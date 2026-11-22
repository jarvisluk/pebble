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
  }
}
