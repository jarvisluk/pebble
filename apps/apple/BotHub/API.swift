import Foundation
import Security
import BotHubTunnelC

struct HubError: LocalizedError {
    var message: String
    var unauthorized = false
    var status = 0
    var errorDescription: String? { message }
}

func errText(_ error: Error, _ fallback: String) -> String {
    if let e = error as? HubError { return e.message.isEmpty ? fallback : e.message }
    // 系统网络错误的文案语言跟随系统，不一定与 App 语言一致，统一用自己的文案。
    if let u = error as? URLError {
        switch u.code {
        case .timedOut: return L.t("netTimeout")
        case .notConnectedToInternet, .networkConnectionLost, .cannotConnectToHost, .cannotFindHost, .dnsLookupFailed:
            return L.t("netUnreachable")
        default: return fallback
        }
    }
    return fallback
}

/// 路径段编码（与 encodeURIComponent 一致）。
func enc(_ s: String) -> String {
    var allowed = CharacterSet.alphanumerics
    allowed.insert(charactersIn: "-_.!~*'()")
    return s.addingPercentEncoding(withAllowedCharacters: allowed) ?? s
}

struct NormalizedAddress {
    var cleanAddress: String
    var token: String
    var isTailcat: Bool
}

/// 拆出 `#token=`，tailcat:// 原样保留，其余补 http:// 并去掉末尾斜杠。
func normalizeAddress(_ raw: String) -> NormalizedAddress {
    let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    var base = trimmed
    var token = ""
    if let hash = trimmed.firstIndex(of: "#") {
        base = String(trimmed[..<hash])
        let frag = trimmed[trimmed.index(after: hash)...]
        for part in frag.split(separator: "&") {
            let kv = part.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
            let k = String(kv[0]).removingPercentEncoding ?? String(kv[0])
            if k == "token" {
                let v = kv.count > 1 ? String(kv[1]).replacingOccurrences(of: "+", with: "%20") : ""
                token = (v.removingPercentEncoding ?? v).trimmingCharacters(in: .whitespaces)
                break
            }
        }
    }
    let s = base.trimmingCharacters(in: .whitespacesAndNewlines)
    if s.lowercased().hasPrefix("tailcat://") {
        return NormalizedAddress(cleanAddress: s, token: token, isTailcat: true)
    }
    if s.isEmpty { return NormalizedAddress(cleanAddress: "", token: token, isTailcat: false) }
    var withProto = s.range(of: "^https?://", options: [.regularExpression, .caseInsensitive]) != nil ? s : "http://\(s)"
    while withProto.hasSuffix("/") { withProto.removeLast() }
    return NormalizedAddress(cleanAddress: withProto, token: token, isTailcat: false)
}

struct APIClient {
    var base: String
    var token: String

    func raw(_ path: String, method: String = "GET", body: [String: Any]? = nil) async throws -> Data {
        guard let url = URL(string: base + path) else { throw HubError(message: L.t("enterValidAddr")) }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.timeoutInterval = 30
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        if !token.isEmpty { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        let (data, resp) = try await URLSession.shared.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if code == 401 { throw HubError(message: L.t("invalidTokenErr"), unauthorized: true, status: 401) }
        if !(200..<300).contains(code) {
            let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            throw HubError(message: (obj?["error"] as? String) ?? "HTTP \(code)", status: code)
        }
        return data
    }

    func call<T: Decodable>(_ path: String, method: String = "GET", body: [String: Any]? = nil, as: T.Type = T.self) async throws -> T {
        let data = try await raw(path, method: method, body: body)
        return try JSONDecoder().decode(T.self, from: data.isEmpty ? Data("{}".utf8) : data)
    }
}

/// `/api/events` SSE：ready 事件表示在线，断开后按 1s 起、×1.8、上限 12s 重连，401 停止。
final class EventStream {
    private var task: Task<Void, Never>?

    func start(base: String, token: String,
               onStatus: @escaping @MainActor (Bool) -> Void,
               onUnauthorized: @escaping @MainActor () -> Void,
               onEvent: @escaping @MainActor (String, [String: Any]) -> Void) {
        stop()
        task = Task.detached {
            var delay = 1.0
            while !Task.isCancelled {
                do {
                    guard let url = URL(string: base + "/api/events") else { return }
                    var req = URLRequest(url: url)
                    req.timeoutInterval = 70
                    req.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    if !token.isEmpty { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
                    let (bytes, resp) = try await URLSession.shared.bytes(for: req)
                    let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
                    if code == 401 {
                        await onStatus(false)
                        await onUnauthorized()
                        return
                    }
                    if code != 200 { throw HubError(message: "HTTP \(code)") }
                    var buf: [UInt8] = []
                    var event = "message"
                    var dataLines: [String] = []
                    for try await byte in bytes {
                        if byte != 10 { buf.append(byte); continue }
                        var line = String(decoding: buf, as: UTF8.self)
                        buf.removeAll(keepingCapacity: true)
                        if line.hasSuffix("\r") { line.removeLast() }
                        if line.isEmpty {
                            if event == "ready" {
                                delay = 1
                                await onStatus(true)
                            } else if !dataLines.isEmpty,
                                      let obj = try? JSONSerialization.jsonObject(with: Data(dataLines.joined(separator: "\n").utf8)) as? [String: Any] {
                                let payload = (obj["payload"] as? [String: Any]) ?? obj
                                await onEvent(event, payload)
                            }
                            event = "message"
                            dataLines = []
                        } else if line.hasPrefix(":") {
                            continue
                        } else if line.hasPrefix("event:") {
                            event = line.dropFirst(6).trimmingCharacters(in: .whitespaces)
                        } else if line.hasPrefix("data:") {
                            dataLines.append(String(line.dropFirst(5).drop(while: { $0 == " " })))
                        }
                    }
                } catch {
                    if Task.isCancelled { return }
                }
                await onStatus(false)
                try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
                delay = min(delay * 1.8, 12)
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
    }
}

/// Go Tailcat 隧道（cmd/libbothub c-archive）。
enum Tunnel {
    static func start(link: String, token: String) async throws -> (url: String, token: String) {
        // 先发一个普通请求触发 iOS「无线数据」授权弹窗，否则 Go 侧直连会被系统静默拦截。
        var probe = URLRequest(url: URL(string: "https://captive.apple.com/hotspot-detect.html")!)
        probe.httpMethod = "HEAD"
        probe.timeoutInterval = 5
        _ = try? await URLSession.shared.data(for: probe)
        let json: String = await Task.detached {
            let l = strdup(link), t = strdup(token)
            defer { free(l); free(t) }
            guard let r = BothubStartTunnel(l, t) else { return "" }
            defer { BothubFreeString(r) }
            return String(cString: r)
        }.value
        let obj = (try? JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any]) ?? [:]
        if let err = obj["error"] as? String, !err.isEmpty { throw HubError(message: err) }
        guard let url = obj["url"] as? String, !url.isEmpty else { throw HubError(message: L.t("connFailed")) }
        return (url, (obj["token"] as? String) ?? "")
    }

    static func stop() async {
        await Task.detached { BothubStopTunnel() }.value
    }
}

/// 主机列表含访问口令：iOS 存 Keychain；macOS 为免去 ad-hoc 签名重建后的钥匙串弹窗，存用户目录下 0600 文件。
enum SecureStore {
    private static let service = "local.bothub.apple"

    static func read(_ key: String) -> Data? {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: key, kSecReturnData as String: true]
        var out: AnyObject?
        return SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess ? out as? Data : nil
    }

    static func write(_ key: String, _ data: Data?) {
        let q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: key]
        SecItemDelete(q as CFDictionary)
        guard let data else { return }
        var add = q
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(add as CFDictionary, nil)
    }
}
