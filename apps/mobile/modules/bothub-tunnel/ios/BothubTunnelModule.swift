import ExpoModulesCore
import Foundation

public class BothubTunnelModule: Module {
  private func primeWirelessDataPermission() {
    guard let url = URL(string: "https://captive.apple.com/hotspot-detect.html") else {
      return
    }
    var req = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 5)
    req.httpMethod = "HEAD"
    let sem = DispatchSemaphore(value: 0)
    URLSession.shared.dataTask(with: req) { _, _, _ in
      sem.signal()
    }.resume()
    _ = sem.wait(timeout: .now() + 5)
  }

  public func definition() -> ModuleDefinition {
    Name("BothubTunnel")

    AsyncFunction("startTunnel") { (link: String, token: String) -> String in
      // Trigger iOS wireless data permission prompt via URLSession before Go opens raw BSD sockets.
      self.primeWirelessDataPermission()

      let raw: String = link.withCString { cLink in
        token.withCString { cToken in
          guard let ptr = BothubStartTunnel(
            UnsafeMutablePointer(mutating: cLink),
            UnsafeMutablePointer(mutating: cToken)
          ) else {
            return "{\"error\":\"tunnel returned null\"}"
          }
          defer { BothubFreeString(ptr) }
          return String(cString: ptr)
        }
      }
      return raw
    }

    AsyncFunction("stopTunnel") {
      BothubStopTunnel()
    }

    AsyncFunction("tailscaleStart") { (stateDir: String, hostname: String, controlUrl: String, authKey: String) -> String in
      self.primeWirelessDataPermission()
      let dir = stateDir.isEmpty ? self.defaultTailscaleDir() : stateDir
      return dir.withCString { cDir in
        hostname.withCString { cHost in
          controlUrl.withCString { cCtrl in
            authKey.withCString { cKey in
              guard let ptr = BothubTailscaleStart(
                UnsafeMutablePointer(mutating: cDir),
                UnsafeMutablePointer(mutating: cHost),
                UnsafeMutablePointer(mutating: cCtrl),
                UnsafeMutablePointer(mutating: cKey)
              ) else {
                return "{\"state\":\"error\",\"error\":\"tailscale returned null\"}"
              }
              defer { BothubFreeString(ptr) }
              return String(cString: ptr)
            }
          }
        }
      }
    }

    AsyncFunction("tailscaleStatus") { (includePeers: Bool) -> String in
      guard let ptr = BothubTailscaleStatus(includePeers ? 1 : 0) else {
        return "{\"state\":\"disabled\"}"
      }
      defer { BothubFreeString(ptr) }
      return String(cString: ptr)
    }

    AsyncFunction("tailscaleLogout") { () -> String in
      guard let ptr = BothubTailscaleLogout() else {
        return "{\"state\":\"disabled\"}"
      }
      defer { BothubFreeString(ptr) }
      return String(cString: ptr)
    }

    View(BothubH264View.self) {
      Events("onMeta", "onStatus", "onStreamError")
      Prop("url") { (view: BothubH264View, url: String) in
        view.setUrl(url)
      }
      Prop("token") { (view: BothubH264View, token: String) in
        view.setToken(token)
      }
      Prop("preset") { (view: BothubH264View, preset: String) in
        view.setPreset(preset)
      }
      Prop("active") { (view: BothubH264View, active: Bool) in
        view.setActive(active)
      }
      AsyncFunction("sendInput") { (view: BothubH264View, eventJson: String) in
        view.sendInput(eventJson)
      }
    }
  }

  private func defaultTailscaleDir() -> String {
    let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
      ?? FileManager.default.temporaryDirectory
    return base.appendingPathComponent("tsnet", isDirectory: true).path
  }
}
