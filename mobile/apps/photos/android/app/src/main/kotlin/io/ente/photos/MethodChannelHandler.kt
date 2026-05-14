package io.ente.photos

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.net.Uri
import android.util.Log
import android.provider.MediaStore
import java.io.File
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.MethodChannel.MethodCallHandler
import io.flutter.plugin.common.MethodChannel.Result

class MethodChannelHandler {
    private lateinit var loginChannel: MethodChannel
    private lateinit var supportChannel: MethodChannel
    private lateinit var galleryChannel: MethodChannel
    private lateinit var context: Context

    fun onAttachedToEngine(binaryMessenger: BinaryMessenger, context: Context) {
        this.context = context

        // Login channel handler
        loginChannel = MethodChannel(binaryMessenger, "ente_login_channel")
        loginChannel.setMethodCallHandler(LoginMethodCallHandler())

        // Support channel handler
        supportChannel = MethodChannel(binaryMessenger, "support_channel")
        supportChannel.setMethodCallHandler(SupportMethodCallHandler())

        // Gallery channel handler - for opening external gallery apps
        galleryChannel = MethodChannel(binaryMessenger, "ente_gallery_channel")
        galleryChannel.setMethodCallHandler(GalleryMethodCallHandler())
    }

    fun onDetachedFromEngine() {
        loginChannel.setMethodCallHandler(null)
        supportChannel.setMethodCallHandler(null)
        galleryChannel.setMethodCallHandler(null)
    }

    private fun destroyApp() {
        Log.d("UpEnte", "Destroying app")
        try {
            val mainActivity = context as? android.app.Activity
            if (mainActivity != null) {
                mainActivity.finishAndRemoveTask()
            }
            android.os.Process.killProcess(android.os.Process.myPid())
        } catch (e: Exception) {
            Log.e("UpEnte", "Failed to destroy app", e)
            try {
                android.os.Process.killProcess(android.os.Process.myPid())
            } catch (killException: Exception) {
                Log.e("UpEnte", "Failed to kill process", killException)
            }
        }
    }

    // Login channel handler
    private inner class LoginMethodCallHandler : MethodCallHandler {
        override fun onMethodCall(call: MethodCall, result: Result) {
            when (call.method) {
                "saveUsername" -> {
                    val username = call.argument<String>("username")
                    val sharedPrefs = context.getSharedPreferences("ente_prefs", Context.MODE_PRIVATE)
                    sharedPrefs.edit().putString("username", username).apply()
                    val current = sharedPrefs.getString("username", null)
                    result.success(true)
                }

                "clearUsername" -> {
                    val sharedPrefs = context.getSharedPreferences("ente_prefs", Context.MODE_PRIVATE)
                    sharedPrefs.edit().remove("username").apply()
                    Log.d("UpEnte", "[DEBUG] Cleared username from native SharedPreferences")
                    result.success(true)
                }

                "logout" -> {
                    Log.d("UpEnte", "Received logout request from Flutter")
                    handleLogoutFromFlutter()
                    result.success(true)
                }

                "getCurrentUsername" -> {
                    val username = getCurrentUsernameFromNativePreferences()
                    result.success(username)
                }

                "openAccountApp" -> {
                    val authorityUri = Uri.parse("content://${context.getString(R.string.account_provider_authority)}")
                    try {
                        val providerResult = context.contentResolver.call(authorityUri, "get_service_credentials", "service_1", null)
                        if (providerResult != null) {
                            val servicePassword = providerResult.getString("service_password") ?: ""
                            val upToken = providerResult.getString("up_token") ?: ""
                            val username = providerResult.getString("username") ?: ""
                            if (servicePassword.isNotEmpty()) {
                                result.success(mapOf(
                                    "service_password" to servicePassword,
                                    "up_token" to upToken,
                                    "username" to username
                                ))
                                return
                            }
                        }
                        result.error(
                            "NO_CREDENTIALS",
                            "No credentials available from account app",
                            null
                        )
                    } catch (e: Exception) {
                        Log.e("UpEnte", "ContentProvider call failed", e)
                        result.error(
                            "ACCOUNT_APP_ERROR",
                            "Failed to fetch credentials from account app",
                            e.message
                        )
                    }
                }

                "openAccountAppForSync" -> {
                    val accountPackage = context.getString(R.string.account_intent_package)
                    val storePackage = context.getString(R.string.store_intent_package)

                    val targetPackage = if (isPackageInstalled(accountPackage)) {
                        Log.d("UpEnte", "Account app found for sync: $accountPackage")
                        accountPackage
                    } else if (isPackageInstalled(storePackage)) {
                        Log.d("UpEnte", "Account app not found, using store for sync: $storePackage")
                        storePackage
                    } else {
                        Log.d("UpEnte", "Neither account app nor store found for sync")
                        result.error(
                            "ACCOUNT_APP_ERROR",
                            "Failed to open account app for sync",
                            "Unable to launch account app"
                        )
                        return
                    }

                    // Clear native shared prefs before destroying
                    val sharedPrefs = context.getSharedPreferences("ente_prefs", Context.MODE_PRIVATE)
                    sharedPrefs.edit().clear().apply()
                    openAccountAppForSync(targetPackage)
                    result.success(true)
                    destroyApp()
                }

                "destroyApp" -> {
                    try {
                        Log.d("UpEnte", "Destroying app completely")
                        // First try to finish the activity cleanly
                        val mainActivity = context as? android.app.Activity
                        if (mainActivity != null) {
                            mainActivity.finishAndRemoveTask()
                        }

                        // Always kill the process to ensure complete destruction
                        android.os.Process.killProcess(android.os.Process.myPid())

                        result.success(true)
                    } catch (e: Exception) {
                        Log.e("UpEnte", "Failed to destroy app", e)
                        // Even if there's an error, try to kill the process
                        try {
                            android.os.Process.killProcess(android.os.Process.myPid())
                        } catch (killException: Exception) {
                            Log.e("UpEnte", "Failed to kill process as fallback", killException)
                        }
                        result.error("DESTROY_APP_ERROR", "Failed to destroy app", e.message)
                    }
                }

                else -> {
                    result.notImplemented()
                }
            }
        }

        private fun isPackageInstalled(packageName: String): Boolean {
            return try {
                context.packageManager.getPackageInfo(packageName, 0)
                true
            } catch (e: Exception) {
                false
            }
        }

        private fun openAccountAppForSync(packageName: String): Boolean {
            try {
                Log.d("UpEnte", "Opening account app for sync with package: $packageName")
                val accountIntent = Intent().apply {
                    component = android.content.ComponentName(
                        packageName,
                        "com.unplugged.account.ui.thirdparty.ThirdPartyCredentialsActivity"
                    )
                    putExtra("action", "sync_credentials")
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                }
                context.startActivity(accountIntent)
                Log.d("UpEnte", "Account app opened for sync successfully")
                return true
            } catch (e: Exception) {
                Log.e("UpEnte", "Failed to open account app for sync: ${e.message}", e)
                return false
            }
        }

    }

    // Support channel handler
    private inner class SupportMethodCallHandler : MethodCallHandler {
        override fun onMethodCall(call: MethodCall, result: Result) {
            when (call.method) {
                "openSupportApp" -> {
                    try {
                        Log.d("UpEnte", "Opening support app")
                        val supportIntent =
                            context.packageManager.getLaunchIntentForPackage("com.unplugged.support")
                        if (supportIntent != null) {
                            // Clear native shared prefs before destroying
                            val sharedPrefs = context.getSharedPreferences("ente_prefs", Context.MODE_PRIVATE)
                            sharedPrefs.edit().clear().apply()
                            supportIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                            context.startActivity(supportIntent)
                            result.success(true)
                            destroyApp()
                        } else {
                            Log.e("UpEnte", "Support app not found on device")
                            result.error(
                                "SUPPORT_APP_NOT_FOUND",
                                "Support app not installed",
                                null
                            )
                        }
                    } catch (e: Exception) {
                        Log.e("UpEnte", "Failed to open support app", e)
                        result.error(
                            "SUPPORT_APP_ERROR",
                            "Failed to open support app",
                            e.message
                        )
                    }
                }

                else -> {
                    result.notImplemented()
                }
            }
        }
    }

    // Gallery channel handler
    private inner class GalleryMethodCallHandler : MethodCallHandler {
        override fun onMethodCall(call: MethodCall, result: Result) {
            when (call.method) {
                "openGalleryAppForEdit" -> {
                    try {
                        val photoPath = call.argument<String>("photoPath")
                        if (photoPath.isNullOrEmpty()) {
                            result.error("INVALID_PATH", "Photo path is required", null)
                            return
                        }

                        val photoFile = File(photoPath)
                        if (!photoFile.exists()) {
                            result.error("FILE_NOT_FOUND", "Photo file not found", null)
                            return
                        }

                        // Find MediaStore URI for this specific image
                        val projection = arrayOf(MediaStore.Images.Media._ID)
                        val selection = "${MediaStore.Images.Media.DATA} = ?"
                        val selectionArgs = arrayOf(photoPath)

                        val cursor = context.contentResolver.query(
                            MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
                            projection,
                            selection,
                            selectionArgs,
                            null
                        )

                        val photoUri = if (cursor?.moveToFirst() == true) {
                            val id =
                                cursor.getLong(cursor.getColumnIndexOrThrow(MediaStore.Images.Media._ID))
                            cursor.close()
                            Uri.withAppendedPath(
                                MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
                                id.toString()
                            )
                        } else {
                            cursor?.close()
                            result.error(
                                "IMAGE_NOT_IN_MEDIASTORE",
                                "Image not found in MediaStore",
                                null
                            )
                            return
                        }

                        // Detect MIME type from file extension
                        val mimeType = when (photoFile.extension.lowercase()) {
                            "jpg", "jpeg" -> "image/jpeg"
                            "png" -> "image/png"
                            "webp" -> "image/webp"
                            "gif" -> "image/gif"
                            "bmp" -> "image/bmp"
                            else -> "image/*"
                        }

                        val intent = Intent(Intent.ACTION_EDIT).apply {
                            setDataAndType(photoUri, mimeType)
                            addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
                        }

                        val packageManager = context.packageManager
                        val resolveInfos = packageManager.queryIntentActivities(intent, 0)

                        if (resolveInfos.size == 1) {
                            context.startActivity(intent)
                        } else {
                            val chooserIntent =
                                Intent.createChooser(intent, "Edit image with").apply {
                                    putExtra(
                                        Intent.EXTRA_EXCLUDE_COMPONENTS, arrayOf(
                                            android.content.ComponentName(
                                                "com.unplugged.photos",
                                                "io.ente.photos.MainActivity"
                                            )
                                        )
                                    )
                                }
                            context.startActivity(chooserIntent)
                        }
                        result.success(true)

                    } catch (e: Exception) {
                        result.error("OPEN_IMAGE_ERROR", "Failed to open image", e.message)
                    }
                }

                else -> {
                    result.notImplemented()
                }
            }
        }
    }

    private fun handleLogoutFromFlutter() {
        Log.d("UpEnte", "Handling logout from Flutter")
        val sharedPrefs = context.getSharedPreferences("ente_prefs", Context.MODE_PRIVATE)
        val editor = sharedPrefs.edit()
        editor.remove("username")
        editor.apply()
        Log.d("UpEnte", "Logout completed from Flutter")
    }

    private fun getCurrentUsernameFromNativePreferences(): String? {
        Log.d("UpEnte", "[DEBUG] About to get current username from native SharedPreferences")
        val sharedPrefs: SharedPreferences =
            context.getSharedPreferences("ente_prefs", Context.MODE_PRIVATE)
        val username = sharedPrefs.getString("username", null)
        return username
    }
}