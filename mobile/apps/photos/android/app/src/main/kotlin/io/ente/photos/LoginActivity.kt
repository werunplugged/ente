package io.ente.photos

import android.content.ComponentName
import android.content.Intent
import android.content.SharedPreferences
import android.net.Uri
import android.os.Bundle
import android.util.Log
import androidx.appcompat.app.AppCompatActivity
import android.accounts.AccountManager
import androidx.appcompat.app.AlertDialog

class LoginActivity : AppCompatActivity() {

    private var account: AccountModel? = null

    companion object {
        private const val ACCOUNT_ACTIVITY_CLASS_NAME =
            "com.unplugged.account.ui.thirdparty.ThirdPartyCredentialsActivity"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        overridePendingTransition(0, 0)
        // Prevent white screen flash
        window.setFlags(
            android.view.WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS,
            android.view.WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS
        )
        val sharedPrefs: SharedPreferences = getSharedPreferences("ente_prefs", MODE_PRIVATE)
        val savedUsername = sharedPrefs.getString("username", null)

        val accountType = getString(R.string.account_type)

        val accountManager = AccountManager.get(this)
        val account = accountManager.getAccountsByType(accountType).firstOrNull()
        val accountUsername = account?.let { accountManager.getUserData(it, "username") }

        if (savedUsername.isNullOrEmpty()) {
            if (account == null) {
                Log.d("UpEnte", "[DEBUG] No saved username and no UP account on device")
                openNotConnectedDialog()
                return
            }
            Log.d("UpEnte", "[DEBUG] No saved username, fetching credentials via ContentProvider")
            fetchCredentialsFromProvider()
            return
        }

        val trimmedSavedUsername = savedUsername.substringBefore('@')
        if (accountUsername.isNullOrEmpty() || trimmedSavedUsername != accountUsername) {
            // Username mismatch or missing in AccountManager, trigger forced logout
            Log.d(
                "UpEnte",
                "[DEBUG] Username missing or mismatch, triggering forced logout via MainActivity"
            )
            val callSecret = generateCallSecret()
            val openFlutterIntent = Intent(this, MainActivity::class.java).apply {
                putExtra("shouldLogout", true)
                putExtra("call_secret", callSecret)
                addFlags(Intent.FLAG_ACTIVITY_REORDER_TO_FRONT)
                addFlags(Intent.FLAG_ACTIVITY_NO_ANIMATION)
            }
            startActivity(openFlutterIntent)
            finish()
            return
        } else {
            // If both usernames exist and match, go to MainActivity (no secret needed for re-entry)
            Log.d("UpEnte", "[DEBUG] Usernames match, proceeding to MainActivity")
            val openFlutterIntent = Intent(this, MainActivity::class.java).apply {
                putExtra("username", savedUsername)
                putExtra("from_login", true)
                addFlags(Intent.FLAG_ACTIVITY_REORDER_TO_FRONT)
                addFlags(Intent.FLAG_ACTIVITY_NO_ANIMATION)
            }
            startActivity(openFlutterIntent)
            finish()
        }
    }

    private fun fetchCredentialsFromProvider() {
        val authorityUri = Uri.parse("content://${getString(R.string.account_provider_authority)}")
        try {
            val result = contentResolver.call(authorityUri, "get_service_credentials", "service_1", null)
            if (result != null) {
                val servicePassword = result.getString("service_password") ?: ""
                val upToken = result.getString("up_token") ?: ""
                val username = result.getString("username") ?: ""
                if (servicePassword.isNotEmpty()) {
                    account = AccountModel(servicePassword, upToken, username)
                    handleAccountLoginResponse(account)
                    return
                }
            }
            // No credentials available — launch generate flow
            launchGenerateCredentials()
        } catch (e: Exception) {
            // Provider not found (account app not installed) or error
            Log.e("UpEnte", "ContentProvider call failed", e)
            openErrorDialog()
        }
    }

    private fun launchGenerateCredentials() {
        val accountPackage = getString(R.string.account_intent_package)
        val storePackage = getString(R.string.store_intent_package)

        val targetPackage = if (isPackageInstalled(accountPackage)) {
            accountPackage
        } else if (isPackageInstalled(storePackage)) {
            storePackage
        } else {
            Log.d("UpEnte", "Neither account app nor store found for credential generation")
            finish()
            return
        }

        val generateCredentialsIntent = Intent().apply {
            component = ComponentName(targetPackage, ACCOUNT_ACTIVITY_CLASS_NAME)
            putExtra("action", "generate_credentials")
        }
        startActivity(generateCredentialsIntent)
        finish()
    }

    override fun finish() {
        super.finish()
        overridePendingTransition(0, 0)
    }

    private fun handleAccountLoginResponse(retrievedAccount: AccountModel? = null) {
        var loginSuccess = false

        if (retrievedAccount != null && retrievedAccount.servicePassword.isNotEmpty()) {
            loginSuccess = true
        } else {
            Log.d("UpEnte", "Login failed: Account details null")
            loginSuccess = false
        }

        if (loginSuccess) {
            Log.d("UpEnte", "Proceeding to MainActivity.")
            val callSecret = generateCallSecret()
            val openFlutterIntent = Intent(this, MainActivity::class.java).apply {
                putExtra("service_password", account?.servicePassword)
                putExtra("up_token", account?.upToken)
                putExtra("username", account?.username)
                putExtra("call_secret", callSecret)
                putExtra("from_login", true)
                addFlags(Intent.FLAG_ACTIVITY_REORDER_TO_FRONT)
                addFlags(Intent.FLAG_ACTIVITY_NO_ANIMATION)
            }
            startActivity(openFlutterIntent)
            finish()
        } else {
            openErrorDialog()
        }
    }

    private fun openErrorDialog() {
        AlertDialog.Builder(this)
            .setTitle("Login Error")
            .setMessage("An error occurred while trying to login. Please contact support if the problem persists.")
            .setPositiveButton("Contact Support") { _, _ ->
                openSupportApp()
                finish()
            }
            .setNegativeButton("Exit") { _, _ ->
                val sharedPrefs = getSharedPreferences("ente_prefs", MODE_PRIVATE)
                sharedPrefs.edit().clear().apply()
                val flutterPrefs = getSharedPreferences("FlutterSharedPreferences", MODE_PRIVATE)
                flutterPrefs.edit().clear().apply()
                finishAndRemoveTask()
            }
            .setCancelable(false)
            .show()
    }

    // Hidden for now - keeping logic for future use with account app
    @Suppress("unused")
    private fun openErrorDialogWithRetry() {
        AlertDialog.Builder(this)
            .setTitle("Login Error")
            .setMessage("An error occurred while trying to login. Please try again or contact support if the problem persists.")
            .setPositiveButton("Try Again") { _, _ ->
                openAccountAppForSync()
                finish()
            }
            .setNegativeButton("Contact Support") { _, _ ->
                openSupportApp()
                finish()
            }
            .setCancelable(false)
            .show()
    }

    private fun openNotConnectedDialog() {
        AlertDialog.Builder(this)
            .setTitle("Not Connected")
            .setMessage("You are not connected to any user. Please connect a user and try again.")
            .setPositiveButton("Exit") { _, _ ->
                val sharedPrefs = getSharedPreferences("ente_prefs", MODE_PRIVATE)
                sharedPrefs.edit().clear().apply()
                val flutterPrefs = getSharedPreferences("FlutterSharedPreferences", MODE_PRIVATE)
                flutterPrefs.edit().clear().apply()
                finishAndRemoveTask()
            }
            .setCancelable(false)
            .show()
    }

    private fun openAccountAppForSync() {
        val sharedPrefs = getSharedPreferences("ente_prefs", MODE_PRIVATE)
        sharedPrefs.edit().clear().apply()

        val accountPackage = getString(R.string.account_intent_package)
        val storePackage = getString(R.string.store_intent_package)

        val targetPackage = if (isPackageInstalled(accountPackage)) {
            accountPackage
        } else if (isPackageInstalled(storePackage)) {
            storePackage
        } else {
            Log.d("UpEnte", "Neither account app nor store found")
            return
        }

        try {
            val intent = Intent().apply {
                component = ComponentName(targetPackage, ACCOUNT_ACTIVITY_CLASS_NAME)
                putExtra("action", "sync_credentials")
            }
            startActivity(intent)
        } catch (e: Exception) {
            Log.e("UpEnte", "Failed to open account app for sync", e)
        }
    }

    private fun openSupportApp() {
        val sharedPrefs = getSharedPreferences("ente_prefs", MODE_PRIVATE)
        sharedPrefs.edit().clear().apply()

        try {
            val supportIntent = packageManager.getLaunchIntentForPackage("com.unplugged.support")
            if (supportIntent != null) {
                supportIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                startActivity(supportIntent)
            } else {
                Log.e("UpEnte", "Support app not found on device")
            }
        } catch (e: Exception) {
            Log.e("UpEnte", "Failed to open support app", e)
        }
    }

    private fun isPackageInstalled(packageName: String): Boolean {
        return try {
            packageManager.getPackageInfo(packageName, 0)
            true
        } catch (e: Exception) {
            false
        }
    }

    private fun generateCallSecret(): String {
        val secret = java.util.UUID.randomUUID().toString()
        getSharedPreferences("ente_internal", MODE_PRIVATE)
            .edit().putString("call_secret", secret).apply()
        Log.d("UpEnte", "[DEBUG] Generated call secret for MainActivity")
        return secret
    }

    override fun onDestroy() {
        super.onDestroy()
        Log.d("UpEnte", "onDestroy: ")
    }
}
