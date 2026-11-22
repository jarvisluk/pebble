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
  private external fun nativeTailscaleStart(stateDir: String, hostname: String, controlUrl: String, authKey: String): String
  private external fun nativeTailscaleStatus(includePeers: Boolean): String
  private external fun nativeTailscaleLogout(): String

  override fun definition() = ModuleDefinition {
    Name("BothubTunnel")

    AsyncFunction("startTunnel") { link: String, token: String ->
      nativeStartTunnel(link, token)
    }

    AsyncFunction("stopTunnel") {
      nativeStopTunnel()
    }

    AsyncFunction("tailscaleStart") { stateDir: String, hostname: String, controlUrl: String, authKey: String ->
      val dir = if (stateDir.isNotEmpty()) {
        stateDir
      } else {
        val filesDir = appContext.reactContext?.filesDir?.absolutePath ?: ""
        if (filesDir.isNotEmpty()) "$filesDir/tsnet" else ""
      }
      nativeTailscaleStart(dir, hostname, controlUrl, authKey)
    }

    AsyncFunction("tailscaleStatus") { includePeers: Boolean ->
      nativeTailscaleStatus(includePeers)
    }

    AsyncFunction("tailscaleLogout") {
      nativeTailscaleLogout()
    }

    View(BothubH264View::class) {
      Events("onMeta", "onStatus", "onStreamError")
      Prop("url") { view: BothubH264View, url: String ->
        view.setUrl(url)
      }
      Prop("token") { view: BothubH264View, token: String ->
        view.setToken(token)
      }
      Prop("preset") { view: BothubH264View, preset: String ->
        view.setPreset(preset)
      }
      Prop("active") { view: BothubH264View, active: Boolean ->
        view.setActive(active)
      }
      AsyncFunction("sendInput") { view: BothubH264View, eventJson: String ->
        view.sendInput(eventJson)
      }
    }
  }
}
