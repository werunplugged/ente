package io.ente.photos

import android.content.ContentUris
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.provider.MediaStore
import android.util.Log
import io.flutter.embedding.android.FlutterFragmentActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugins.GeneratedPluginRegistrant
import android.Manifest
import android.accounts.AccountManager
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat

class MainActivity : FlutterFragmentActivity() {
    // Channel for receiving account details from LoginActivity
    private val ACCOUNT_CHANNEL_NAME = "com.unplugged.photos/account"
    private var methodChannel: MethodChannel? = null
    private lateinit var methodChannelHandler: MethodChannelHandler

    private var pendingAccountDetails: Map<String, String>? = null
    private lateinit var accountManager: AccountManager

    private var pendingLogoutRestart = false
    private var pendingShouldLogout = false

    private var logoutChannel: MethodChannel? = null

    companion object {
        private const val LOGOUT_CHANNEL_NAME = "ente_logout_channel"
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        Log.d("UpEnte", "configureFlutterEngine called")
        

        GeneratedPluginRegistrant.registerWith(flutterEngine)

        // Initialize MethodChannelHandler with context
        methodChannelHandler = MethodChannelHandler()
        methodChannelHandler.onAttachedToEngine(flutterEngine.dartExecutor.binaryMessenger, this)

        methodChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, ACCOUNT_CHANNEL_NAME)
        logoutChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, LOGOUT_CHANNEL_NAME)

        // Handle method calls from Dart
        methodChannel?.setMethodCallHandler { call, result ->
            when (call.method) {
                "requestAccount" -> {
                    if (pendingAccountDetails != null) {
                        sendAccountDetailsToFlutter(pendingAccountDetails!!)
                        pendingAccountDetails = null
                        result.success(null)
                    } else {
                        Log.d("UpEnte", "No pending account details to send.")
                        result.success(null)
                    }
                }
                else -> {
                    Log.w("UpEnte", "Unknown method from Flutter: ${call.method}")
                    result.notImplemented()
                }
            }
        }

        // Add handler for logoutComplete from Flutter
        val logoutCompleteChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "ente_logout_complete_channel")
        logoutCompleteChannel.setMethodCallHandler { call, result ->
            if (call.method == "logoutComplete") {
                Log.d("UpEnte", "[DEBUG] Received logoutComplete from Flutter, restarting LoginActivity")
                pendingLogoutRestart = false
                val loginIntent = Intent(this, LoginActivity::class.java)
                startActivity(loginIntent)
                finish()
                result.success(true)
            } else {
                result.notImplemented()
            }
        }

        // After engine is ready, process any pending logout
        if (pendingShouldLogout) {
            Log.d("UpEnte", "[DEBUG] Sending pending logout to Flutter after engine ready")
            val logoutChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "ente_logout_channel")
            logoutChannel.invokeMethod("onLogoutRequested", null)
            pendingShouldLogout = false
        }

        // If account details were pending before the methodChannel was initialized, send them now
        pendingAccountDetails?.let { details ->
            sendAccountDetailsToFlutter(details)
            pendingAccountDetails = null
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        // Resolve duplicate/invalid media URIs before calling super to prevent plugin crash
        extractAndClearMediaUri(intent)
        super.onCreate(savedInstanceState)
        overridePendingTransition(0, 0)
        handleIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        // Resolve duplicate/invalid media URIs before calling super to prevent plugin crash
        extractAndClearMediaUri(intent)
        super.onNewIntent(intent)

        Log.d("UpEnte", "[DEBUG] onNewIntent called - MainActivity brought to front")
        setIntent(intent)

        if (intent.getBooleanExtra("from_login", false)) {
            Log.d("UpEnte", "[DEBUG] MainActivity brought to front from login app")
            methodChannel?.invokeMethod("onBroughtToFront", null)
        }

        handleIntent(intent)
    }

    /**
     * Checks if the intent is a media intent (VIEW, SEND, etc. with image/video).
     */
    private fun isMediaIntent(intent: Intent): Boolean {
        val isMediaAction = intent.action in listOf(
            Intent.ACTION_VIEW,
            Intent.ACTION_SEND,
            Intent.ACTION_SEND_MULTIPLE,
            Intent.ACTION_PICK,
            Intent.ACTION_GET_CONTENT
        )
        val hasMediaType = intent.type?.startsWith("image/") == true ||
                intent.type?.startsWith("video/") == true
        val hasMediaData = intent.data?.toString()?.contains("media") == true

        return isMediaAction && (hasMediaType || hasMediaData || intent.data != null)
    }

    /**
     * Checks if the app has permission to read media files (images/videos).
     */
    private fun hasMediaReadPermission(): Boolean {
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            ContextCompat.checkSelfPermission(this, Manifest.permission.READ_MEDIA_VIDEO) == PackageManager.PERMISSION_GRANTED ||
                ContextCompat.checkSelfPermission(this, Manifest.permission.READ_MEDIA_IMAGES) == PackageManager.PERMISSION_GRANTED
        } else {
            ContextCompat.checkSelfPermission(this, Manifest.permission.READ_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED
        }
    }

    /**
     * For media intents, resolves duplicate/invalid URIs to a valid single-item URI.
     * Returns the original URI if resolution is not needed.
     */
    private fun extractAndClearMediaUri(intent: Intent): Uri? {
        if (!isMediaIntent(intent)) return null

        val uri = intent.data ?: return null
        Log.d("UpEnte", "[DEBUG] Processing media URI: $uri")

        // If the URI is a MediaStore URI and we don't have media permissions,
        // clear intent data to prevent SecurityException crash in media_extension plugin.
        // The app will open to the permissions screen instead.
        if (uri.toString().startsWith("content://media/") && !hasMediaReadPermission()) {
            Log.w("UpEnte", "[DEBUG] No media permission for MediaStore URI, clearing intent data to prevent crash")
            intent.data = null
            return null
        }

        // Try to resolve the URI to handle duplicates
        val resolvedUri = resolveToSingleItemUri(uri)
        if (resolvedUri != null && resolvedUri != uri) {
            Log.d("UpEnte", "[DEBUG] Resolved duplicate URI: $uri -> $resolvedUri")
            intent.data = resolvedUri
        }
        return uri
    }

    /**
     * Resolves a content URI to a specific single-item URI.
     * This handles the "Multiple items" error by querying and picking the first item.
     */
    private fun resolveToSingleItemUri(uri: Uri): Uri? {
        try {
            val uriString = uri.toString()
            // Only process media store URIs that might have duplicates
            if (!uriString.startsWith("content://media/external/")) return null

            val projection = arrayOf(MediaStore.MediaColumns._ID)
            contentResolver.query(uri, projection, null, null, "${MediaStore.MediaColumns._ID} LIMIT 1")?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val id = cursor.getLong(cursor.getColumnIndexOrThrow(MediaStore.MediaColumns._ID))
                    val baseUri = when {
                        uriString.contains("/images/") -> MediaStore.Images.Media.EXTERNAL_CONTENT_URI
                        uriString.contains("/video/") -> MediaStore.Video.Media.EXTERNAL_CONTENT_URI
                        else -> return null
                    }
                    return ContentUris.withAppendedId(baseUri, id)
                }
            }
        } catch (e: Exception) {
            Log.e("UpEnte", "[DEBUG] Error resolving URI to single item: ${e.message}")
        }
        return null
    }

    private fun handleIntent(intent: Intent?) {
        if (intent == null) {
            Log.d("UpEnte", "[DEBUG] Intent is null, returning")
            return
        }

        // Only process sensitive credentials from trusted internal source (LoginActivity)
        if (isIntentFromLoginActivity(intent)) {
            Log.d("UpEnte", "[DEBUG] Intent is from LoginActivity, handling login")
            handleLoginIntent(intent)
        } else {
            Log.d("UpEnte", "[DEBUG] Intent is NOT from LoginActivity, handling as external")
            // Handle external intents (login, sharing, deep links) - no credential processing
            handleExternalIntent(intent)
        }
    }
    
    private fun isIntentFromLoginActivity(intent: Intent): Boolean {
        // Check if this intent contains credentials (new login attempt)
        val hasServicePassword = intent.hasExtra("service_password")
        val hasUpToken = intent.hasExtra("up_token") 
        val hasUsername = intent.hasExtra("username")
        val hasCredentials = hasServicePassword && hasUpToken && hasUsername
        
        // Only require shared secret validation for credential-passing intents
        if (hasCredentials) {
            Log.d("UpEnte", "[DEBUG] Intent contains credentials, validating shared secret")
            val providedSecret = intent.getStringExtra("call_secret")
            val prefs = getSharedPreferences("ente_internal", MODE_PRIVATE)
            val expectedSecret = prefs.getString("call_secret", null)
            
            val hasValidSecret = providedSecret != null && 
                                expectedSecret != null && 
                                providedSecret == expectedSecret
            
            // IMPORTANT: Clear the secret immediately after validation if valid
            if (hasValidSecret) {
                prefs.edit().remove("call_secret").apply()
                Log.d("UpEnte", "[DEBUG] Valid secret found and cleared")
            }
            
            return hasValidSecret
        } else {
            // For non-credential intents (re-entry, logout), use existing logic
            Log.d("UpEnte", "[DEBUG] Intent has no credentials, using existing validation")
            return true  // Allow existing LoginActivity intents without credentials
        }
    }
    
    private fun handleLoginIntent(intent: Intent) {
        val servicePassword = intent.getStringExtra("service_password")
        val upToken = intent.getStringExtra("up_token")
        val username = intent.getStringExtra("username")
        
        // Additional validation of credential format
        if (servicePassword.isNullOrBlank() || upToken.isNullOrBlank() || username.isNullOrBlank()) {
            Log.d("UpEnte", "[DEBUG] One or more credentials is null/blank, returning")
            return
        }
        
        // Validate username format (basic sanitization)
        if (!isValidUsername(username)) {
            Log.d("UpEnte", "[DEBUG] Username validation failed, returning")
            return
        }
        
        val accountDetails = mapOf(
            "service_password" to servicePassword,
            "up_token" to upToken,
            "username" to username
        )
        
        Log.d("UpEnte", "[DEBUG] Account details created, checking methodChannel")

        // Check if methodChannel is initialized (meaning configureFlutterEngine has run)
        if (methodChannel != null) {
            Log.d("UpEnte", "[DEBUG] MethodChannel is ready, sending data to Flutter")
            // Flutter engine is configured, send data directly
            sendAccountDetailsToFlutter(accountDetails)
        } else {
            Log.d("UpEnte", "[DEBUG] MethodChannel not ready, storing as pending")
            // Flutter engine not configured yet, or methodChannel not set up.
            // Store data to send when configureFlutterEngine is called.
            pendingAccountDetails = accountDetails
        }
        
        // Handle logout flag from trusted source only (also requires valid token)
        if (intent.getBooleanExtra("shouldLogout", false) && !pendingLogoutRestart) {
            Log.d("UpEnte", "[DEBUG] Logout flag detected")
            pendingLogoutRestart = true
            pendingShouldLogout = true
        }
    }
    
    private fun handleExternalIntent(intent: Intent) {
        // Handle legitimate external intents safely (login, sharing, deep links)
        when (intent.action) {
            Intent.ACTION_VIEW,
            Intent.ACTION_PICK,
            Intent.ACTION_GET_CONTENT,
            Intent.ACTION_SEND,
            Intent.ACTION_SEND_MULTIPLE -> {
                // These are safe - just pass the intent to Flutter for media handling
                // No credential processing allowed from external sources
                notifyFlutterOfExternalIntent(intent.action)
            }
            "es.antonborri.home_widget.action.LAUNCH" -> {
                // Widget launch - safe
                notifyFlutterOfExternalIntent("widget_launch")
            }
            else -> {
                // Unknown external intent - ignore for security
            }
        }
    }
    
    private fun isValidUsername(username: String): Boolean {
        // Basic validation - adjust pattern as needed for your username requirements
        return username.matches(Regex("^[a-zA-Z0-9._@-]+$")) && username.length <= 255
    }
    
    private fun notifyFlutterOfExternalIntent(action: String?) {
        // Notify Flutter of external intent without passing sensitive data
        methodChannel?.invokeMethod("onExternalIntent", mapOf("action" to action))
    }

    private fun sendAccountDetailsToFlutter(accountDetails: Map<String, String>) {
        methodChannel?.invokeMethod("onAccountReceived", accountDetails, object : MethodChannel.Result {
            override fun success(result: Any?) {
                 Log.d("UpEnte", "[DEBUG] Account details sent to Flutter successfully.")
            }

            override fun error(errorCode: String, errorMessage: String?, errorDetails: Any?) {
                 Log.e("UpEnte", "[DEBUG] Failed to send account details: $errorCode $errorMessage")
            }

            override fun notImplemented() {
                 Log.w("UpEnte", "[DEBUG] onAccountReceived not implemented on Dart side.")
            }
        })
    }

    fun handleLogoutFromFlutter() {
        Log.d("UpEnte", "Handling logout from Flutter")
        val sharedPrefs = getSharedPreferences("ente_prefs", MODE_PRIVATE)
        val editor = sharedPrefs.edit()
        editor.remove("username")
        editor.apply()
        Log.d("UpEnte", "Closing app after logout from Flutter")
        finishAndRemoveTask()
    }

    override fun onStart() {
        super.onStart()
        accountManager = AccountManager.get(this)
        val accountType = getString(R.string.account_type)

        val sharedPrefs = getSharedPreferences("ente_prefs", MODE_PRIVATE)
        val savedUsername = sharedPrefs.getString("username", null)
        val accountsInSystem = accountManager.accounts
        // Only check account state, do not trigger logout to Flutter here
        // If needed, start login flow or clear state, but do not send onLogoutRequested
        // All forced logout is now handled via shouldLogout intent
        if (savedUsername != null && savedUsername.isNotEmpty()) {
            if (accountsInSystem.none { it.type == accountType }) {
                Log.d("UpEnte", "[DEBUG] No account found on start, requesting logout in Flutter")
                val logoutChannel = MethodChannel(flutterEngine!!.dartExecutor.binaryMessenger, "ente_logout_channel")
                logoutChannel.invokeMethod("onLogoutRequested", null)
            } else {
                val account = accountsInSystem.firstOrNull { it.type == accountType }
                val username = account?.let { accountManager.getUserData(it, "username") }
                val trimmedSavedUsername = savedUsername?.substringBefore('@')
                if (username != trimmedSavedUsername) {
                    Log.d("UpEnte", "[DEBUG] Username mismatch on start, requesting logout in Flutter")
                    val logoutChannel = MethodChannel(flutterEngine!!.dartExecutor.binaryMessenger, "ente_logout_channel")
                    logoutChannel.invokeMethod("onLogoutRequested", null)
                }
            }
        } else {
            Log.d("UpEnte", "[DEBUG] No saved username, not triggering logout")
        }

        // Register listener for future changes
        accountManager.addOnAccountsUpdatedListener({ accountsInSystem ->
            // Only check account state, do not trigger logout to Flutter here
            // All forced logout is now handled via shouldLogout intent
        }, null, false)
    }

    private fun triggerLogoutToFlutter() {
        Log.d("UpEnte", "[DEBUG] Sending logout request to Flutter via MethodChannel")
        val logoutChannel = MethodChannel(flutterEngine!!.dartExecutor.binaryMessenger, "ente_logout_channel")
        logoutChannel.invokeMethod("onLogoutRequested", null)
        val sharedPrefs = getSharedPreferences("ente_prefs", MODE_PRIVATE)
        sharedPrefs.edit().remove("username").apply()
        Log.d("UpEnte", "[DEBUG] Cleared username from native SharedPreferences")
        finishAndRemoveTask()
    }

    override fun onDestroy() {
        methodChannel = null // Clean up the channel
        if (::methodChannelHandler.isInitialized) {
            methodChannelHandler.onDetachedFromEngine()
        }
        logoutChannel = null // Clean up the logout channel
        super.onDestroy()
    }

    override fun finish() {
        super.finish()
        overridePendingTransition(0, 0)
    }
}
