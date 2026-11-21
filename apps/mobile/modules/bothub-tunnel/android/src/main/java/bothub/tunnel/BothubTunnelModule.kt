package bothub.tunnel

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

class BothubTunnelModule : Module() {
  companion object {
    init {
      System.loadLibrary("bothub")
    }
  }

  private external fun nativeStartTunnel(link: String, token: String): String
  private external fun nativeStopTunnel()

  override fun definition() = ModuleDefinition {
    Name("BothubTunnel")

    AsyncFunction("startTunnel") { link: String, token: String ->
      nativeStartTunnel(link, token)
    }

    AsyncFunction("stopTunnel") {
      nativeStopTunnel()
    }
  }
}
