package ai.pebble.android.data

import android.content.Context
import bothub.tunnel.BothubTunnelModule
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File

data class TunnelEndpoint(
    val url: String,
    val token: String,
)

object PebbleTunnel {
    suspend fun start(link: String, token: String): TunnelEndpoint = withContext(Dispatchers.IO) {
        val raw = BothubTunnelModule.nativeStartTunnel(link, token)
        val parsed = runCatching { HubJson.decodeFromString<TunnelStartResp>(raw) }.getOrNull()
        val err = parsed?.error
        if (!err.isNullOrBlank()) {
            throw HubException(err)
        }
        val url = parsed?.url
        if (url.isNullOrBlank()) {
            throw HubException("connFailed", isKey = true)
        }
        TunnelEndpoint(url = url, token = parsed.token.orEmpty())
    }

    suspend fun stop() = withContext(Dispatchers.IO) {
        runCatching { BothubTunnelModule.nativeStopTunnel() }
    }

    private fun defaultTailscaleDir(context: Context): String {
        val dir = File(context.filesDir, "tsnet")
        if (!dir.exists()) dir.mkdirs()
        return dir.absolutePath
    }

    suspend fun tailscaleStart(
        context: Context,
        hostname: String = "",
        controlUrl: String = "",
        authKey: String = "",
    ): MobileTailscaleStatus = withContext(Dispatchers.IO) {
        val dir = defaultTailscaleDir(context)
        val raw = BothubTunnelModule.nativeTailscaleStart(dir, hostname, controlUrl, authKey)
        runCatching { HubJson.decodeFromString<MobileTailscaleStatus>(raw) }
            .getOrElse { MobileTailscaleStatus(state = "error", error = "decode failed") }
    }

    suspend fun tailscaleStatus(includePeers: Boolean = false): MobileTailscaleStatus = withContext(Dispatchers.IO) {
        val raw = BothubTunnelModule.nativeTailscaleStatus(includePeers)
        runCatching { HubJson.decodeFromString<MobileTailscaleStatus>(raw) }
            .getOrElse { MobileTailscaleStatus(state = "disabled") }
    }

    suspend fun tailscaleLogout(): MobileTailscaleStatus = withContext(Dispatchers.IO) {
        val raw = BothubTunnelModule.nativeTailscaleLogout()
        runCatching { HubJson.decodeFromString<MobileTailscaleStatus>(raw) }
            .getOrElse { MobileTailscaleStatus(state = "disabled") }
    }
}
