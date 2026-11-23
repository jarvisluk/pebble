import Foundation
import Observation
import SwiftUI
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

enum L {
    /// "zh-CN" 或 "en"，由 HubStore 根据偏好同步。
    static var lang = L.systemLang

    static var systemLang: String {
        (Bundle.main.preferredLocalizations.first ?? "en").lowercased().hasPrefix("zh") ? "zh-CN" : "en"
    }

    static func t(_ key: String, _ vars: [String: Any] = [:]) -> String {
        var s = (lang == "en" ? enStrings[key] : zhStrings[key]) ?? zhStrings[key] ?? key
        for (k, v) in vars { s = s.replacingOccurrences(of: "{\(k)}", with: "\(v)") }
        return s
    }
}

enum ConnState { case idle, connecting, connected, unauthorized, error }

func resolveDefaultModel(_ g: String, _ h: String) -> String {
    if g.isEmpty { return "" }
    if g.hasPrefix("magpie/") { return h == "claude" ? String(g.dropFirst(7)) : g }
    if !g.contains("/") { return g }
    return h.isEmpty || h == "opencode" || h == "pi" ? g : ""
}

func parseISO(_ s: String?) -> Date? {
    guard let s, !s.isEmpty else { return nil }
    let f = ISO8601DateFormatter()
    f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    if let d = f.date(from: s) { return d }
    f.formatOptions = [.withInternetDateTime]
    return f.date(from: s)
}

private func hhmm(_ d: Date) -> String {
    let f = DateFormatter()
    f.locale = Locale(identifier: L.lang == "en" ? "en_US" : "zh_CN")
    f.dateFormat = L.lang == "en" ? "h:mm a" : "HH:mm"
    return f.string(from: d)
}

func formatShortTime(_ iso: String?) -> String {
    guard let d = parseISO(iso) else { return "" }
    let cal = Calendar.current
    if cal.isDateInToday(d) { return hhmm(d) }
    if cal.isDateInYesterday(d) { return L.t("yesterday") }
    let c = cal.dateComponents([.month, .day], from: d)
    return "\(c.month ?? 0)/\(c.day ?? 0)"
}

func formatStamp(_ iso: String?) -> String {
    guard let d = parseISO(iso) else { return "" }
    let cal = Calendar.current
    let t = hhmm(d)
    if cal.isDateInToday(d) { return "\(L.t("today")) \(t)" }
    if cal.isDateInYesterday(d) { return "\(L.t("yesterday")) \(t)" }
    let c = cal.dateComponents([.month, .day], from: d)
    return L.lang == "en" ? "\(c.month ?? 0)/\(c.day ?? 0) \(t)" : "\(c.month ?? 0)月\(c.day ?? 0)日 \(t)"
}

func plainPreview(_ s: String) -> String {
    var out = s.replacingOccurrences(of: "```[\\s\\S]*?```", with: L.t("codeTag"), options: .regularExpression)
    out = out.replacingOccurrences(of: "[#>*`_\\[\\]]", with: "", options: .regularExpression)
    out = out.replacingOccurrences(of: "\\s+", with: " ", options: .regularExpression)
    return out.trimmingCharacters(in: .whitespaces)
}

@MainActor
@Observable
final class HubStore {
    // 偏好
    var themePref: String { didSet { UserDefaults.standard.set(themePref, forKey: "bothub.theme") } }
    var langPref: String { didSet { UserDefaults.standard.set(langPref, forKey: "bothub.lang"); L.lang = lang } }
    var accentHex: String { didSet { UserDefaults.standard.set(accentHex, forKey: "bothub.accent") } }
    var tsBound: Bool { didSet { UserDefaults.standard.set(tsBound, forKey: "bothub.tsBound") } }
    var tsControlUrl: String { didSet { UserDefaults.standard.set(tsControlUrl, forKey: "bothub.tsControlUrl") } }

    // 连接
    var servers: [ServerProfile] = []
    var activeServerId: String?
    var connState: ConnState = .idle
    var connError = ""
    var baseURL = ""
    var authToken = ""
    var isTailcat = false
    var isTailscale = false
    var online = false

    // 数据
    var bots: [BotStatus] = []
    var msgs: [String: [ChatMessage]] = [:]
    var last: [String: ChatMessage] = [:]
    var unread: [String: Int] = [:]
    var tools: [String: String] = [:]
    var settings: SettingsData?
    var models: [String: [String]] = [:]
    var nodes: [NodeInfo] = []
    var tailcat: TailcatStatus?
    var mobileTailscale: MobileTailscaleStatus?
    var hostTailscale: HostTailscaleStatus?
    var tsBinding = false
    var takeovers: [TakeoverRequest] = []
    var questions: [PendingQuestion] = []
    var desktopTargets: [DesktopTarget] = []
    var routines: [String: [RoutineItem]] = [:]

    @ObservationIgnored private var activeChat: String?
    @ObservationIgnored private var connSeq = 0
    @ObservationIgnored private let stream = EventStream()

    init() {
        let d = UserDefaults.standard
        themePref = d.string(forKey: "bothub.theme") ?? "system"
        langPref = d.string(forKey: "bothub.lang") ?? "system"
        accentHex = d.string(forKey: "bothub.accent") ?? Palette.defaultAccent
        tsBound = d.bool(forKey: "bothub.tsBound")
        tsControlUrl = d.string(forKey: "bothub.tsControlUrl") ?? ""
        L.lang = lang
        if let data = SecureStore.read("servers"), let list = try? JSONDecoder().decode([ServerProfile].self, from: data) {
            servers = list
        }
        let savedActive = UserDefaults.standard.string(forKey: "bothub.active")
        let target = servers.first { $0.id == savedActive } ?? servers.first
        activeServerId = target?.id
        if tsBound {
            Task {
                _ = await Tunnel.tailscaleStart(controlUrl: tsControlUrl)
                await refreshMobileTailscale(includePeers: true)
            }
        }
        if let target { Task { await connectProfile(target) } }
    }

    var lang: String { langPref == "zh-CN" || langPref == "en" ? langPref : L.systemLang }

    /// 视图内取文案时读取 lang，以便切换语言时自动刷新。
    func t(_ key: String, _ vars: [String: Any] = [:]) -> String {
        _ = lang
        return L.t(key, vars)
    }

    var activeServer: ServerProfile? { servers.first { $0.id == activeServerId } }
    var api: APIClient { APIClient(base: baseURL, token: authToken) }
    var isConnected: Bool { connState == .connected }

    private func persistServers() {
        SecureStore.write("servers", try? JSONEncoder().encode(servers))
        UserDefaults.standard.set(activeServerId, forKey: "bothub.active")
    }

    // MARK: 连接

    func setActiveChat(_ botId: String?) {
        activeChat = botId
        if let botId { unread[botId] = nil }
    }

    private func removeBotState(_ id: String) {
        bots.removeAll { $0.id == id }
        msgs[id] = nil
        last[id] = nil
        unread[id] = nil
        tools[id] = nil
    }

    private func pushIncoming(_ botId: String, _ msg: ChatMessage) {
        var list = msgs[botId] ?? []
        if list.contains(where: { $0.id == msg.id }) { return }
        list.append(msg)
        msgs[botId] = list
        if msg.role != "system" {
            last[botId] = msg
            if activeChat != botId { unread[botId, default: 0] += 1 }
        }
    }

    private func upsertBot(_ st: BotStatus) {
        if let i = bots.firstIndex(where: { $0.id == st.id }) { bots[i] = st } else { bots.append(st) }
    }

    private func startStream(_ url: String, _ tok: String) {
        stream.start(base: url, token: tok,
                     onStatus: { [weak self] ok in self?.online = ok },
                     onUnauthorized: { [weak self] in
                         self?.connState = .unauthorized
                         self?.connError = L.t("needsToken")
                     },
                     onEvent: { [weak self] type, p in self?.handleEvent(type, p, url: url, tok: tok) })
    }

    private func handleEvent(_ type: String, _ p: [String: Any], url: String, tok: String) {
        switch type {
        case "message":
            if let bid = p["botId"] as? String, let m = decodeAny(p, as: ChatMessage.self) { pushIncoming(bid, m) }
        case "bot_status":
            if let st = decodeAny(p, as: BotStatus.self) {
                upsertBot(st)
                if !st.isBusy { tools[st.id] = nil }
            }
        case "tool_use":
            if let bid = p["botId"] as? String { tools[bid] = (p["tool"] as? String) ?? "" }
        case "bot_created":
            if let st = decodeAny(p, as: BotStatus.self), !bots.contains(where: { $0.id == st.id }) { bots.append(st) }
        case "bot_deleted":
            if let bid = (p["id"] as? String) ?? (p["botId"] as? String) { removeBotState(bid) }
        case "nodes_changed":
            if let arr = p["nodes"], let list = decodeAny(arr, as: [NodeInfo].self) { nodes = list }
        case "takeover":
            if let arr = p["items"], let list = decodeAny(arr, as: [TakeoverRequest].self) { takeovers = list }
        case "questions":
            if let arr = p["items"], let list = decodeAny(arr, as: [PendingQuestion].self) { questions = list }
        case "routine_updated", "routine_run":
            if let bid = p["botId"] as? String {
                Task {
                    if let r: RoutinesResp = try? await APIClient(base: url, token: tok).call("/api/bots/\(enc(bid))/routines") {
                        routines[bid] = r.routines ?? []
                    }
                }
            }
        default: break
        }
    }

    func connectProfile(_ profile: ServerProfile) async {
        connSeq += 1
        let seq = connSeq
        stream.stop()
        online = false
        connState = .connecting
        connError = ""

        let norm = normalizeAddress(profile.address)
        var tok = profile.token.isEmpty ? norm.token : profile.token
        var url = norm.cleanAddress
        do {
            if norm.isTailscale {
                if mobileTailscale?.state != "running" {
                    let st = await Tunnel.tailscaleStart(controlUrl: tsControlUrl)
                    mobileTailscale = st
                    if st.state == "needs_login", let auth = st.authUrl, let u = URL(string: auth) {
                        openExternalURL(u)
                    }
                }
                let r = try await Tunnel.start(link: norm.cleanAddress, token: tok)
                guard seq == connSeq else { return }
                url = r.url
                if tok.isEmpty, !r.token.isEmpty { tok = r.token }
            } else if norm.isTailcat {
                let r = try await Tunnel.start(link: norm.cleanAddress, token: tok)
                guard seq == connSeq else { return }
                url = r.url
                if tok.isEmpty, !r.token.isEmpty { tok = r.token }
            } else {
                await Tunnel.stop()
            }
            guard seq == connSeq else { return }
            if tok != profile.token || norm.cleanAddress != profile.address,
               let i = servers.firstIndex(where: { $0.id == profile.id }) {
                servers[i].address = norm.cleanAddress
                servers[i].token = tok
                persistServers()
            }
            isTailcat = norm.isTailcat
            isTailscale = norm.isTailscale
            baseURL = url
            authToken = tok

            let client = APIClient(base: url, token: tok)
            let data: BotsResp = try await client.call("/api/bots")
            guard seq == connSeq else { return }
            let list = data.bots ?? []
            bots = list
            msgs = [:]; last = [:]; unread = [:]; tools = [:]; models = [:]; routines = [:]
            connState = .connected
            connError = ""

            for b in list {
                Task {
                    if let r: MessagesResp = try? await client.call("/api/bots/\(enc(b.id))/messages?limit=1"),
                       let m = r.messages?.last, seq == connSeq { last[b.id] = m }
                }
            }
            Task { if let s: SettingsData = try? await client.call("/api/settings"), seq == connSeq { settings = s } }
            Task { if let n: NodesResp = try? await client.call("/api/nodes"), seq == connSeq { nodes = n.nodes ?? [] } }
            Task { if let t: TailcatStatus = try? await client.call("/api/tailcat"), seq == connSeq { tailcat = t } }
            Task { if let ts: HostTailscaleStatus = try? await client.call("/api/tailscale"), seq == connSeq { hostTailscale = ts } }
            Task { if let t: TakeoversResp = try? await client.call("/api/desktop/takeovers"), seq == connSeq { takeovers = t.items ?? [] } }
            Task { if let q: QuestionsResp = try? await client.call("/api/questions"), seq == connSeq { questions = q.items ?? [] } }
            startStream(url, tok)
        } catch {
            guard seq == connSeq else { return }
            baseURL = url
            authToken = tok
            isTailcat = norm.isTailcat
            isTailscale = norm.isTailscale
            if let e = error as? HubError, e.unauthorized {
                connState = .unauthorized
                connError = e.message
            } else {
                connState = .error
                connError = errText(error, L.t("connFailed"))
            }
        }
    }

    /// 回到前台：刷新列表并重启 SSE，失败则整体重连。
    func resume() async {
        guard let cur = activeServer, !baseURL.isEmpty else { return }
        let url = baseURL, tok = authToken
        do {
            let d: BotsResp = try await api.call("/api/bots")
            bots = d.bots ?? []
            startStream(url, tok)
            if let bid = activeChat {
                let m: MessagesResp = try await api.call("/api/bots/\(enc(bid))/messages?limit=200")
                msgs[bid] = m.messages ?? []
            }
        } catch {
            await connectProfile(cur)
        }
    }

    func addAndConnectServer(label: String, address: String, token: String) async throws {
        let norm = normalizeAddress(address)
        if norm.cleanAddress.isEmpty { throw HubError(message: L.t("enterValidAddr")) }
        let tok = (token.isEmpty ? norm.token : token).trimmingCharacters(in: .whitespaces)
        let lbl = label.trimmingCharacters(in: .whitespaces)
        var target: ServerProfile
        if let i = servers.firstIndex(where: { $0.address == norm.cleanAddress }) {
            target = servers[i]
            if !lbl.isEmpty { target.label = lbl }
            if !tok.isEmpty { target.token = tok }
            servers[i] = target
        } else {
            let def = norm.isTailcat ? L.t("remoteHostDefault")
                : norm.cleanAddress.replacingOccurrences(of: "^https?://", with: "", options: .regularExpression)
            target = ServerProfile(id: "\(Int(Date().timeIntervalSince1970 * 1000))", label: lbl.isEmpty ? def : lbl,
                                   address: norm.cleanAddress, token: tok)
            servers.insert(target, at: 0)
        }
        activeServerId = target.id
        persistServers()
        await connectProfile(target)
    }

    func selectServer(_ id: String) async {
        guard let target = servers.first(where: { $0.id == id }) else { return }
        activeServerId = id
        persistServers()
        await connectProfile(target)
    }

    func removeServer(_ id: String) async {
        servers.removeAll { $0.id == id }
        let removingActive = activeServerId == id
        if removingActive { activeServerId = servers.first?.id }
        persistServers()
        guard removingActive else { return }
        if let next = servers.first {
            await connectProfile(next)
        } else {
            connSeq += 1
            stream.stop()
            await Tunnel.stop()
            baseURL = ""; authToken = ""
            bots = []; msgs = [:]; last = [:]
            connState = .idle
            connError = ""
        }
    }

    func reconnect() async {
        if let s = activeServer { await connectProfile(s) }
    }

    // MARK: Bot 与消息

    func refreshBots() async throws {
        guard !baseURL.isEmpty else { return }
        let d: BotsResp = try await api.call("/api/bots")
        bots = d.bots ?? []
    }

    func loadMessages(_ botId: String) async throws {
        guard !baseURL.isEmpty else { return }
        let d: MessagesResp = try await api.call("/api/bots/\(enc(botId))/messages?limit=200")
        let list = d.messages ?? []
        msgs[botId] = list
        if let m = list.last { last[botId] = m }
    }

    func loadCollabMessages(_ botId: String, peerId: String, limit: Int = 200) async throws -> [ChatMessage] {
        guard !baseURL.isEmpty else { return [] }
        let d: MessagesResp = try await api.call("/api/bots/\(enc(botId))/collab/\(enc(peerId))?limit=\(limit)")
        return d.messages ?? []
    }

    private let mediaCache = NSCache<NSString, NSData>()

    func fetchMediaData(_ path: String) async throws -> Data {
        let key = (baseURL + path) as NSString
        if let cached = mediaCache.object(forKey: key) {
            return Data(referencing: cached)
        }
        let data = try await api.raw(path)
        mediaCache.setObject(data as NSData, forKey: key)
        return data
    }

    func sendMessage(_ botId: String, _ text: String) async throws {
        let d: TextResp = try await api.call("/api/bots/\(enc(botId))/message", method: "POST",
                                             body: ["message": text, "source": "mobile"])
        if ["/bots", "/status", "/reset"].contains(text), let out = d.text, !out.isEmpty {
            pushIncoming(botId, ChatMessage(id: "cmd_\(Int(Date().timeIntervalSince1970 * 1000))", botId: botId,
                                            role: "system", from: "command", text: out,
                                            createdAt: ISO8601DateFormatter().string(from: Date())))
        }
        if text == "/reset" { try await refreshBots() }
    }

    func interruptBot(_ botId: String) async throws -> Bool {
        let r: InterruptResp = try await api.call("/api/bots/\(enc(botId))/interrupt", method: "POST")
        return r.interrupted ?? false
    }

    func resetBot(_ botId: String) async throws {
        _ = try await api.raw("/api/bots/\(enc(botId))/reset", method: "POST")
        try await refreshBots()
    }

    func botDetail(_ botId: String) async throws -> BotStatus {
        try await api.call("/api/bots/\(enc(botId))")
    }

    func createBot(_ body: [String: Any]) async throws -> BotStatus {
        let st: BotStatus = try await api.call("/api/bots", method: "POST", body: body)
        if !bots.contains(where: { $0.id == st.id }) { bots.append(st) }
        return st
    }

    @discardableResult
    func updateBot(_ botId: String, _ body: [String: Any]) async throws -> BotStatus {
        let st: BotStatus = try await api.call("/api/bots/\(enc(botId))", method: "PATCH", body: body)
        if let i = bots.firstIndex(where: { $0.id == st.id }) { bots[i] = st }
        return st
    }

    func deleteBot(_ botId: String) async throws {
        _ = try await api.raw("/api/bots/\(enc(botId))", method: "DELETE")
        removeBotState(botId)
    }

    // MARK: 记忆

    func fetchMemories(scope: String, botId: String? = nil, query: String = "", limit: Int = 0) async throws -> MemoryList {
        var q = URLComponents()
        var items = [URLQueryItem(name: "scope", value: scope)]
        if let botId { items.append(URLQueryItem(name: "botId", value: botId)) }
        let qq = query.trimmingCharacters(in: .whitespaces)
        if !qq.isEmpty { items.append(URLQueryItem(name: "query", value: qq)) }
        if limit > 0 { items.append(URLQueryItem(name: "limit", value: "\(limit)")) }
        q.queryItems = items
        return try await api.call("/api/memory?\(q.percentEncodedQuery ?? "")")
    }

    func saveMemory(_ body: [String: Any]) async throws {
        _ = try await api.raw("/api/memory", method: "POST", body: body)
    }

    func deleteMemory(_ id: String) async throws {
        _ = try await api.raw("/api/memory/\(enc(id))", method: "DELETE")
    }

    // MARK: 定时任务

    @discardableResult
    func fetchRoutines(_ botId: String) async throws -> [RoutineItem] {
        let r: RoutinesResp = try await api.call("/api/bots/\(enc(botId))/routines")
        routines[botId] = r.routines ?? []
        return routines[botId]!
    }

    func createRoutine(_ botId: String, _ body: [String: Any]) async throws {
        _ = try await api.raw("/api/bots/\(enc(botId))/routines", method: "POST", body: body)
        try await fetchRoutines(botId)
    }

    func updateRoutine(_ botId: String, _ rid: String, _ body: [String: Any]) async throws {
        _ = try await api.raw("/api/bots/\(enc(botId))/routines/\(enc(rid))", method: "PATCH", body: body)
        try await fetchRoutines(botId)
    }

    func deleteRoutine(_ botId: String, _ rid: String) async throws {
        _ = try await api.raw("/api/bots/\(enc(botId))/routines/\(enc(rid))", method: "DELETE")
        try await fetchRoutines(botId)
    }

    func runRoutine(_ botId: String, _ rid: String) async throws {
        _ = try await api.raw("/api/bots/\(enc(botId))/routines/\(enc(rid))/run?async=1", method: "POST")
        try await fetchRoutines(botId)
    }

    func fetchRoutineRuns(_ botId: String, _ rid: String, limit: Int = 6) async throws -> [RoutineRun] {
        let r: RunsResp = try await api.call("/api/bots/\(enc(botId))/routine-runs?routineId=\(enc(rid))&limit=\(limit)")
        return r.runs ?? []
    }

    // MARK: 设置

    @discardableResult
    func loadSettings() async throws -> SettingsData {
        let s: SettingsData = try await api.call("/api/settings")
        settings = s
        return s
    }

    @discardableResult
    func loadModels(_ harness: String) async throws -> [String] {
        let d: ModelsResp = try await api.call("/api/models?harness=\(enc(harness))")
        models[harness] = d.models ?? []
        return models[harness]!
    }

    func saveSettings(changed: Bool, defaultModel: String, defaultModels: [String: String], sharedUserMd: String,
                      botUpdates: [(id: String, harness: String, model: String)]) async throws {
        if changed {
            settings = try await api.call("/api/settings", method: "PUT",
                                          body: ["defaultModel": defaultModel, "defaultModels": defaultModels, "sharedUserMd": sharedUserMd])
        }
        for u in botUpdates { try await updateBot(u.id, ["harness": u.harness, "model": u.model]) }
        try await loadSettings()
        try await refreshBots()
    }

    func connectMagpie(_ harness: String, model: String) async throws {
        settings = try await api.call("/api/harnesses/\(enc(harness))/magpie", method: "POST", body: ["action": "connect", "model": model])
        models = [:]
    }

    func disconnectMagpie(_ harness: String) async throws {
        settings = try await api.call("/api/harnesses/\(enc(harness))/magpie", method: "POST", body: ["action": "disconnect"])
        models = [:]
    }

    func openMagpie() async throws {
        let _: OkResp = try await api.call("/api/magpie/open", method: "POST", body: [:])
    }

    func setDesktopWorker(harness: String, model: String) async throws {
        settings = try await api.call("/api/settings", method: "PUT",
                                      body: ["desktopHarness": harness.trimmingCharacters(in: .whitespaces),
                                             "desktopModel": model.trimmingCharacters(in: .whitespaces)])
    }

    // MARK: 节点与桌面

    func refreshNodes() async throws {
        guard !baseURL.isEmpty else { return }
        let d: NodesResp = try await api.call("/api/nodes")
        nodes = d.nodes ?? []
    }

    func refreshTailcat() async throws {
        tailcat = try await api.call("/api/tailcat")
    }

    func refreshHostTailscale() async throws {
        hostTailscale = try await api.call("/api/tailscale")
    }

    func refreshMobileTailscale(includePeers: Bool = true) async {
        let st = await Tunnel.tailscaleStatus(includePeers: includePeers)
        mobileTailscale = st
        if st.state == "running" {
            tsBound = true
        }
    }

    func bindTailscale(authKey: String = "", controlUrl: String = "") async {
        tsBinding = true
        defer { tsBinding = false }
        let ctrl = controlUrl.trimmingCharacters(in: .whitespacesAndNewlines)
        let key = authKey.trimmingCharacters(in: .whitespacesAndNewlines)
        var st = await Tunnel.tailscaleStart(controlUrl: ctrl, authKey: key)
        mobileTailscale = st
        var openedAuth = ""
        if st.state == "needs_login", let auth = st.authUrl, !auth.isEmpty, let u = URL(string: auth) {
            openedAuth = auth
            openExternalURL(u)
        }
        let deadline = Date().addingTimeInterval(180)
        while (st.state == "starting" || st.state == "needs_login") && Date() < deadline {
            try? await Task.sleep(for: .milliseconds(1500))
            st = await Tunnel.tailscaleStatus(includePeers: true)
            mobileTailscale = st
            if st.state == "needs_login", let auth = st.authUrl, !auth.isEmpty, auth != openedAuth, let u = URL(string: auth) {
                openedAuth = auth
                openExternalURL(u)
            }
        }
        if st.state == "running" {
            tsBound = true
            tsControlUrl = ctrl
            if (st.peers ?? []).isEmpty {
                await refreshMobileTailscale(includePeers: true)
            }
        }
    }

    func logoutTailscale() async {
        _ = await Tunnel.tailscaleLogout()
        tsBound = false
        tsControlUrl = ""
        mobileTailscale = nil
    }

    private func openExternalURL(_ url: URL) {
        #if canImport(UIKit)
        UIApplication.shared.open(url)
        #elseif canImport(AppKit)
        NSWorkspace.shared.open(url)
        #endif
    }

    @discardableResult
    func refreshDesktopTargets() async throws -> [DesktopTarget] {
        let d: TargetsResp = try await api.call("/api/desktop/targets")
        desktopTargets = d.targets ?? []
        takeovers = d.takeovers ?? []
        return desktopTargets
    }

    func fetchDesktopState(_ target: String) async throws -> DesktopState {
        let d: DesktopState = try await api.call("/api/desktop/state?target=\(enc(target))")
        takeovers = d.takeovers ?? []
        return d
    }

    func fetchDesktopFrame(_ target: String) async throws -> DesktopFrame {
        try await api.call("/api/desktop/frame?target=\(enc(target))&windowId=&maxDim=1280&quality=72")
    }

    func makeDesktopWSRequest(target: String, preset: String) -> URLRequest? {
        guard var comps = URLComponents(string: baseURL) else { return nil }
        comps.scheme = (comps.scheme?.lowercased() == "https") ? "wss" : "ws"
        comps.path = "/api/desktop/stream/ws"
        comps.queryItems = [
            URLQueryItem(name: "target", value: target),
            URLQueryItem(name: "preset", value: preset),
        ]
        guard let url = comps.url else { return nil }
        var req = URLRequest(url: url)
        if !authToken.isEmpty {
            req.setValue("Bearer \(authToken)", forHTTPHeaderField: "Authorization")
        }
        return req
    }

    func sendDesktopInput(_ body: [String: Any]) async throws {
        _ = try await api.raw("/api/desktop/input", method: "POST", body: body)
    }

    func resolveTakeover(_ id: String) async throws {
        let r: TakeoversResp = try await api.call("/api/desktop/takeover/resolve", method: "POST",
                                                  body: ["id": id, "note": L.t("takeoverResolvedNote")])
        takeovers = r.items ?? []
    }

    func replyQuestion(id: String, answers: [String: Any], cancelled: Bool) async throws {
        let r: QuestionsResp = try await api.call("/api/questions/\(enc(id))/reply", method: "POST",
                                                  body: ["answers": answers, "cancelled": cancelled])
        questions = r.items ?? []
    }
}
