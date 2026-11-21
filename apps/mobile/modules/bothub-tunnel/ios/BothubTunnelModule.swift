import ExpoModulesCore

public class BothubTunnelModule: Module {
  public func definition() -> ModuleDefinition {
    Name("BothubTunnel")

    AsyncFunction("startTunnel") { (link: String, token: String) -> String in
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
