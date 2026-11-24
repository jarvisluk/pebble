package bothub.tunnel

/**
 * JNI bridge matching the exported C symbol names in cmd/libbothub/jni_android.go:
 * Java_bothub_tunnel_BothubTunnelModule_native*
 */
object BothubTunnelModule {
    init {
        System.loadLibrary("bothub")
    }

    external fun nativeStartTunnel(link: String, token: String): String
    external fun nativeStopTunnel()
    external fun nativeTailscaleStart(
        stateDir: String,
        hostname: String,
        controlUrl: String,
        authKey: String,
    ): String
    external fun nativeTailscaleStatus(includePeers: Boolean): String
    external fun nativeTailscaleLogout(): String
}
