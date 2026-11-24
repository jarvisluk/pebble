package ai.pebble.android

import android.app.Application
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.core.view.WindowCompat
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.nav.PebbleRouter
import ai.pebble.android.ui.nav.RootScaffold
import ai.pebble.android.ui.theme.PebbleTheme
import kotlinx.coroutines.launch

class PebbleApplication : Application() {
    val store: PebbleStore by lazy { PebbleStore(this) }
}

class MainActivity : ComponentActivity() {
    private val appStore: PebbleStore
        get() = (application as PebbleApplication).store

    private var routerRef: PebbleRouter? = null
    private var pendingDeepLink: Uri? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        pendingDeepLink = intent?.data

        appStore.onOpenExternalUrl = { rawUrl ->
            runCatching {
                startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(rawUrl)))
            }
        }

        setContent {
            val sysDark = isSystemInDarkTheme()
            val dark = when (appStore.themePref) {
                "dark" -> true
                "light" -> false
                else -> sysDark
            }
            val router = remember { PebbleRouter() }
            routerRef = router
            val scope = rememberCoroutineScope()

            DisposableEffect(dark) {
                enableEdgeToEdge(
                    statusBarStyle = if (dark) {
                        SystemBarStyle.dark(Color.TRANSPARENT)
                    } else {
                        SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT)
                    },
                    navigationBarStyle = if (dark) {
                        SystemBarStyle.dark(Color.TRANSPARENT)
                    } else {
                        SystemBarStyle.light(Color.TRANSPARENT, Color.TRANSPARENT)
                    },
                )
                onDispose {}
            }

            LaunchedEffect(Unit) {
                pendingDeepLink?.let { uri ->
                    pendingDeepLink = null
                    scope.launch { handleDeepLink(uri, router) }
                }
            }

            PebbleTheme(dark = dark) {
                RootScaffold(store = appStore, router = router)
            }
        }
    }

    override fun onStart() {
        super.onStart()
        appStore.onForegroundChanged(true)
    }

    override fun onStop() {
        super.onStop()
        appStore.onForegroundChanged(false)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val uri = intent.data ?: return
        val r = routerRef
        if (r != null) {
            handleDeepLink(uri, r)
        } else {
            pendingDeepLink = uri
        }
    }

    private fun handleDeepLink(uri: Uri, router: PebbleRouter) {
        val scheme = uri.scheme?.lowercase() ?: return
        if (scheme != "pebble" && scheme != "bothub") return
        val host = uri.host?.lowercase().orEmpty()
        val parts = uri.pathSegments.orEmpty()
        when (host) {
            "connect" -> {
                val link = uri.getQueryParameter("link")?.trim().orEmpty()
                if (link.isNotEmpty()) {
                    kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.Main.immediate).launch {
                        runCatching { appStore.addAndConnectServer(label = "", address = link, token = "") }
                    }
                }
            }
            "bot" -> {
                val botId = parts.firstOrNull().orEmpty()
                if (botId.isNotEmpty()) {
                    router.openBot(botId)
                }
            }
            "desktop" -> {
                val target = uri.getQueryParameter("target").orEmpty().ifBlank { "local" }
                router.openDesktop(target)
            }
            "interrupt" -> {
                val botId = parts.firstOrNull().orEmpty()
                if (botId.isNotEmpty()) {
                    kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.Dispatchers.Main.immediate).launch {
                        runCatching { appStore.interruptBot(botId) }
                    }
                }
            }
        }
    }
}
