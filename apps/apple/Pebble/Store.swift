import Foundation
import Observation
import SwiftUI
#if os(macOS)
import AppKit
#endif

@MainActor
@Observable
final class HubStore {
    // 偏好
    var themePref: String {
        didSet {
            let normalized = normalizeThemePref(themePref)
            if themePref != normalized {
                themePref = normalized
                return
            }
            UserDefaults.standard.set(themePref, forKey: "bothub.theme")
            PlatformAppearance.shared.apply(themePref: themePref)
        }
    }
    var langPref: String {
        didSet {
            UserDefaults.standard.set(langPref, forKey: "bothub.lang")
            L.lang = lang
            #if os(iOS)
            PebbleSharedStore.setPreferredLang(lang)
            #endif
        }
    }
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
    var modelsGeneration = 0
    var nodes: [NodeInfo] = []
    var tailcat: TailcatStatus?
    var mobileTailscale: MobileTailscaleStatus?
    var hostTailscale: HostTailscaleStatus?
    var tsBinding = false
    var takeovers: [TakeoverRequest] = []
    var questions: [PendingQuestion] = []
    var approvals: [PendingApproval] = []
    var desktopTargets: [DesktopTarget] = []
    var activeDesktopTarget = ActiveDesktopTarget(target: "local", name: nil, temporary: false, expiresAt: nil)
    var usingStandby = false
    var managedMode = false
    var serverInfo: ServerInfo?
    var unsupportedCaps: Set<String> = []
    var dismissedCompatProto = 0
    var updateStatus: UpdateStatus?
    var updateChecking = false
    var dismissedUpdateVersion = ""
    var routines: [String: [RoutineItem]] = [:]
    var pendingStageOpen: StageOpenEvent?
    var pendingChatInsert: PendingChatInsert?

    @ObservationIgnored var activeChat: String?
    @ObservationIgnored var connSeq = 0
    @ObservationIgnored var managedIsRemote = false
    @ObservationIgnored let stream = EventStream()
    @ObservationIgnored var modelLoadTasks: [String: (UUID, Task<[String], Error>)] = [:]
    @ObservationIgnored let mediaCache = NSCache<NSString, NSData>()

    func supports(_ cap: String) -> Bool {
        if unsupportedCaps.contains(cap) { return false }
        if let info = serverInfo {
            return info.capabilities?.contains(cap) ?? false
        }
        return true
    }

    func markUnsupported(_ cap: String) {
        unsupportedCaps.insert(cap)
    }

    var isClientOutdated: Bool {
        (serverInfo?.protocolVersion ?? 0) > ClientProtocol.current
    }

    var isClientUpgradeRequired: Bool {
        (serverInfo?.minClientProtocol ?? 1) > ClientProtocol.current
    }

    var showCompatBanner: Bool {
        guard let info = serverInfo, info.protocolVersion > ClientProtocol.current else { return false }
        return isClientUpgradeRequired || dismissedCompatProto != info.protocolVersion
    }

    func dismissCompatBanner() {
        if let proto = serverInfo?.protocolVersion {
            dismissedCompatProto = proto
        }
    }

    var effectiveHasUpdate: Bool {
        guard let st = updateStatus else { return false }
        if st.hasUpdate { return true }
        if let ver = st.latest?.version, !ver.isEmpty {
            return isSemVerNewer(ver, than: platformCurrentShortVersion)
        }
        return false
    }

    var showUpdateBanner: Bool {
        guard !isClientUpgradeRequired,
              effectiveHasUpdate,
              let ver = updateStatus?.latest?.version, !ver.isEmpty else { return false }
        return dismissedUpdateVersion != ver
    }

    func dismissUpdateBanner() {
        guard let ver = updateStatus?.latest?.version, !ver.isEmpty else { return }
        dismissedUpdateVersion = ver
        UserDefaults.standard.set(ver, forKey: "pebble.dismissedUpdate")
    }

    static func isLocalDesktopTarget(_ target: String) -> Bool {
        target.isEmpty || target == "local" || target.hasPrefix("virtual:") || target.hasPrefix("local:virtual:")
    }

    init(autoConnect: Bool = true) {
        let d = UserDefaults.standard
        themePref = normalizeThemePref(d.string(forKey: "bothub.theme"))
        langPref = d.string(forKey: "bothub.lang") ?? "system"
        tsBound = d.bool(forKey: "bothub.tsBound")
        tsControlUrl = d.string(forKey: "bothub.tsControlUrl") ?? ""
        dismissedUpdateVersion = d.string(forKey: "pebble.dismissedUpdate") ?? ""
        PlatformAppearance.shared.apply(themePref: themePref)
        L.lang = lang
        #if os(iOS)
        PebbleSharedStore.setPreferredLang(lang)
        #endif
        if autoConnect {
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

    func normalizeEndpointURL(_ raw: String) -> String {
        raw.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "/+$", with: "", options: .regularExpression)
    }

    func performAPI<T>(_ op: (APIClient) async throws -> T) async throws -> T {
        let usedClient = api
        do {
            return try await op(usedClient)
        } catch let err as HubError where err.unauthorized {
            #if os(macOS)
            if managedMode {
                await DesktopHostManager.shared.refreshStatus()
                let snap = DesktopHostManager.shared.snapshot
                let snapURL = normalizeEndpointURL(snap.activeUrl)
                if !snapURL.isEmpty, snapURL == baseURL, snap.activeToken != usedClient.token {
                    if authToken != snap.activeToken {
                        authToken = snap.activeToken
                        PushManager.shared.configureConnection(base: baseURL, token: snap.activeToken)
                        startStream(baseURL, snap.activeToken)
                    }
                    do {
                        return try await op(api)
                    } catch let retryErr as HubError where retryErr.unauthorized {
                        online = false
                        connState = .unauthorized
                        connError = L.t("needsToken")
                        stream.stop()
                        throw retryErr
                    }
                }
            }
            #endif
            online = false
            connState = .unauthorized
            connError = L.t("needsToken")
            stream.stop()
            throw err
        }
    }

    func callAPI<T: Decodable>(_ path: String, method: String = "GET", body: [String: Any]? = nil, as: T.Type = T.self) async throws -> T {
        try await performAPI { try await $0.call(path, method: method, body: body, as: T.self) }
    }

    func rawAPI(_ path: String, method: String = "GET", body: [String: Any]? = nil) async throws -> Data {
        try await performAPI { try await $0.raw(path, method: method, body: body) }
    }

    func persistServers() {
        SecureStore.write("servers", try? JSONEncoder().encode(servers))
        UserDefaults.standard.set(activeServerId, forKey: "bothub.active")
    }
}
