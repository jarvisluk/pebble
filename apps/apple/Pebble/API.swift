import Foundation
import Security
#if os(iOS)
import PebbleTunnelC
#endif

struct HubError: LocalizedError {
    var message: String
    var unauthorized = false
    var status = 0
    var code = ""
    var isUnsupportedCapability: Bool {
        status == 404 || status == 405 || status == 501 || code == "capability_unsupported"
    }
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
    var isTailscale: Bool
}

/// 拆出 `#token=`，tailcat:// 与 tailscale:// 原样保留，其余补 http:// 并去掉末尾斜杠。
func normalizeAddress(_ raw: String) -> NormalizedAddress {
    let trimmed = raw
        .trimmingCharacters(in: .whitespacesAndNewlines)
        .replacingOccurrences(of: "[\\r\\n]+", with: "", options: .regularExpression)
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
    let lower = s.lowercased()
    if lower.hasPrefix("tailcat://") {
        return NormalizedAddress(cleanAddress: s, token: token, isTailcat: true, isTailscale: false)
    }
    if lower.hasPrefix("tailscale://") {
        return NormalizedAddress(cleanAddress: s, token: token, isTailcat: false, isTailscale: true)
    }
    if s.isEmpty { return NormalizedAddress(cleanAddress: "", token: token, isTailcat: false, isTailscale: false) }
    var withProto = s.range(of: "^https?://", options: [.regularExpression, .caseInsensitive]) != nil ? s : "http://\(s)"
    while withProto.hasSuffix("/") { withProto.removeLast() }
    return NormalizedAddress(cleanAddress: withProto, token: token, isTailcat: false, isTailscale: false)
}

/// 独立于系统共享 Cookie 存储的 HTTP 会话，避免本机多端口或模式切换时残留的 `bot_hub_token` Cookie 污染请求。
let hubAPISession: URLSession = {
    let cfg = URLSessionConfiguration.default
    cfg.httpShouldSetCookies = false
    cfg.httpCookieAcceptPolicy = .never
    cfg.httpCookieStorage = nil
    return URLSession(configuration: cfg)
}()

struct APIClient {
    var base: String
    var token: String

    private static var appVersion: String {
        (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String) ?? "0.1.0"
    }

    static func attachClientHeaders(_ req: inout URLRequest) {
        req.setValue("\(ClientProtocol.current)", forHTTPHeaderField: "X-Pebble-Protocol")
        req.setValue("apple", forHTTPHeaderField: "X-Pebble-Client")
        req.setValue(appVersion, forHTTPHeaderField: "X-Pebble-Version")
    }

    func raw(_ path: String, method: String = "GET", body: [String: Any]? = nil) async throws -> Data {
        guard let url = URL(string: base + path) else { throw HubError(message: L.t("enterValidAddr")) }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.timeoutInterval = 30
        Self.attachClientHeaders(&req)
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        if !token.isEmpty { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        let (data, resp) = try await hubAPISession.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if code == 401 { throw HubError(message: L.t("invalidTokenErr"), unauthorized: true, status: 401) }
        if !(200..<300).contains(code) {
            let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
            throw HubError(
                message: (obj?["error"] as? String) ?? "HTTP \(code)",
                status: code,
                code: (obj?["code"] as? String) ?? ""
            )
        }
        return data
    }

    func call<T: Decodable>(_ path: String, method: String = "GET", body: [String: Any]? = nil, as: T.Type = T.self) async throws -> T {
        let data = try await raw(path, method: method, body: body)
        return try JSONDecoder().decode(T.self, from: data.isEmpty ? Data("{}".utf8) : data)
    }

    func uploadAttachment(_ path: String, data: Data, filename: String, mimeType: String) async throws -> InboundAttachmentResponse {
        guard let url = URL(string: base + path) else { throw HubError(message: L.t("enterValidAddr")) }
        let boundary = "Boundary-\(UUID().uuidString)"
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.timeoutInterval = 60
        Self.attachClientHeaders(&req)
        req.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        if !token.isEmpty { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }

        var body = Data()
        body.append("--\(boundary)\r\n".data(using: .utf8)!)
        let safeFilename = filename.replacingOccurrences(of: "\"", with: "_")
        body.append("Content-Disposition: form-data; name=\"file\"; filename=\"\(safeFilename)\"\r\n".data(using: .utf8)!)
        body.append("Content-Type: \(mimeType)\r\n\r\n".data(using: .utf8)!)
        body.append(data)
        body.append("\r\n--\(boundary)--\r\n".data(using: .utf8)!)
        req.httpBody = body

        let (respData, resp) = try await hubAPISession.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if code == 401 { throw HubError(message: L.t("invalidTokenErr"), unauthorized: true, status: 401) }
        if !(200..<300).contains(code) {
            let obj = try? JSONSerialization.jsonObject(with: respData) as? [String: Any]
            throw HubError(
                message: (obj?["error"] as? String) ?? "HTTP \(code)",
                status: code,
                code: (obj?["code"] as? String) ?? ""
            )
        }
        return try JSONDecoder().decode(InboundAttachmentResponse.self, from: respData)
    }

    func listWorkspaceFiles(botId: String, path: String = "", showHidden: Bool = false) async throws -> WorkspaceListResult {
        var comps = [String]()
        if !path.isEmpty { comps.append("path=\(enc(path))") }
        if showHidden { comps.append("showHidden=1") }
        let qs = comps.isEmpty ? "" : "?" + comps.joined(separator: "&")
        return try await call("/api/bots/\(enc(botId))/files\(qs)")
    }

    func readWorkspaceFile(botId: String, path: String) async throws -> WorkspaceFileContent {
        return try await call("/api/bots/\(enc(botId))/files/content?path=\(enc(path))")
    }

    func issueWorkspaceFileTicket(botId: String, path: String) async throws -> WorkspaceFileTicketResp {
        return try await call("/api/bots/\(enc(botId))/files/ticket", method: "POST", body: ["path": path])
    }

    func workspaceFileRawURL(botId: String, path: String, ticket: String, download: Bool = false) -> URL? {
        var qs = "path=\(enc(path))&ticket=\(enc(ticket))"
        if download { qs += "&download=1" }
        return URL(string: "\(base)/api/bots/\(enc(botId))/files/raw?\(qs)")
    }

    func downloadWorkspaceFileToTemp(botId: String, path: String, fileName: String) async throws -> URL {
        let tk = try await issueWorkspaceFileTicket(botId: botId, path: path)
        guard let url = workspaceFileRawURL(botId: botId, path: path, ticket: tk.ticket, download: true) else {
            throw HubError(message: L.t("downloadFailed"))
        }
        let (tmpURL, resp) = try await hubAPISession.download(from: url)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        if !(200..<300).contains(code) {
            throw HubError(message: "HTTP \(code)", status: code)
        }
        let destDir = FileManager.default.temporaryDirectory.appendingPathComponent("pebble-files", isDirectory: true)
        try FileManager.default.createDirectory(at: destDir, withIntermediateDirectories: true)
        let safeName = fileName.replacingOccurrences(of: "/", with: "_")
        let dest = destDir.appendingPathComponent(safeName)
        if FileManager.default.fileExists(atPath: dest.path) {
            try? FileManager.default.removeItem(at: dest)
        }
        try FileManager.default.moveItem(at: tmpURL, to: dest)
        return dest
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
                    APIClient.attachClientHeaders(&req)
                    req.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    if !token.isEmpty { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
                    let (bytes, resp) = try await hubAPISession.bytes(for: req)
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

/// Go Tailcat 隧道（iOS 链接 cmd/libbothub c-archive；macOS 由 sidecar 管理）。
enum Tunnel {
    #if os(iOS)
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

    private static var defaultTailscaleDir: String {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("tsnet", isDirectory: true).path
    }

    static func tailscaleStart(hostname: String = "", controlUrl: String = "", authKey: String = "") async -> MobileTailscaleStatus {
        var probe = URLRequest(url: URL(string: "https://captive.apple.com/hotspot-detect.html")!)
        probe.httpMethod = "HEAD"
        probe.timeoutInterval = 5
        _ = try? await URLSession.shared.data(for: probe)
        let dir = defaultTailscaleDir
        let json: String = await Task.detached {
            let d = strdup(dir), h = strdup(hostname), c = strdup(controlUrl), k = strdup(authKey)
            defer { free(d); free(h); free(c); free(k) }
            guard let r = BothubTailscaleStart(d, h, c, k) else { return "{\"state\":\"error\",\"error\":\"null\"}" }
            defer { BothubFreeString(r) }
            return String(cString: r)
        }.value
        return (try? JSONDecoder().decode(MobileTailscaleStatus.self, from: Data(json.utf8)))
            ?? MobileTailscaleStatus(state: "error", error: "decode failed")
    }

    static func tailscaleStatus(includePeers: Bool = false) async -> MobileTailscaleStatus {
        let json: String = await Task.detached {
            guard let r = BothubTailscaleStatus(includePeers ? 1 : 0) else { return "{\"state\":\"disabled\"}" }
            defer { BothubFreeString(r) }
            return String(cString: r)
        }.value
        return (try? JSONDecoder().decode(MobileTailscaleStatus.self, from: Data(json.utf8)))
            ?? MobileTailscaleStatus(state: "disabled")
    }

    static func tailscaleLogout() async -> MobileTailscaleStatus {
        let json: String = await Task.detached {
            guard let r = BothubTailscaleLogout() else { return "{\"state\":\"disabled\"}" }
            defer { BothubFreeString(r) }
            return String(cString: r)
        }.value
        return (try? JSONDecoder().decode(MobileTailscaleStatus.self, from: Data(json.utf8)))
            ?? MobileTailscaleStatus(state: "disabled")
    }
    #else
    static func start(link: String, token: String) async throws -> (url: String, token: String) {
        throw HubError(message: L.t("connFailed"))
    }

    static func stop() async {}

    static func tailscaleStart(hostname: String = "", controlUrl: String = "", authKey: String = "") async -> MobileTailscaleStatus {
        MobileTailscaleStatus(state: "disabled")
    }

    static func tailscaleStatus(includePeers: Bool = false) async -> MobileTailscaleStatus {
        MobileTailscaleStatus(state: "disabled")
    }

    static func tailscaleLogout() async -> MobileTailscaleStatus {
        MobileTailscaleStatus(state: "disabled")
    }
    #endif
}

/// 主机列表含访问口令：iOS 存 Keychain；macOS 存用户支持目录下 0600 文件，避免 ad-hoc 重建后钥匙串反复弹窗。
enum SecureStore {
    #if os(iOS)
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
    #else
    private static func fileURL(_ key: String) -> URL? {
        guard let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first else {
            return nil
        }
        let pebbleDir = base.appendingPathComponent("pebble", isDirectory: true)
        let legacyDir = base.appendingPathComponent("bot-hub", isDirectory: true)
        var isDir: ObjCBool = false
        let dir: URL
        if FileManager.default.fileExists(atPath: pebbleDir.path, isDirectory: &isDir), isDir.boolValue {
            dir = pebbleDir
        } else if FileManager.default.fileExists(atPath: legacyDir.path, isDirectory: &isDir), isDir.boolValue {
            dir = legacyDir
        } else {
            dir = pebbleDir
        }
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let safeKey = key.replacingOccurrences(of: "[^a-zA-Z0-9._-]", with: "_", options: .regularExpression)
        return dir.appendingPathComponent("apple-\(safeKey).json")
    }

    static func read(_ key: String) -> Data? {
        guard let url = fileURL(key) else { return nil }
        return try? Data(contentsOf: url)
    }

    static func write(_ key: String, _ data: Data?) {
        guard let url = fileURL(key) else { return }
        if let data {
            try? data.write(to: url, options: .atomic)
            try? FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
        } else {
            try? FileManager.default.removeItem(at: url)
        }
    }
    #endif
}
