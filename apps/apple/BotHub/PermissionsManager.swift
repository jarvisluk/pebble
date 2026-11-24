import Foundation
import Network
import Observation
import UserNotifications
#if os(iOS)
import AVFoundation
#endif

enum UnboxPermStatus: String, Equatable {
    case granted
    case denied
    case notDetermined
    case requested
    case unsupported
}

struct UnboxPermissionItem: Identifiable, Equatable {
    let id: String
    let titleKey: String
    let fallbackTitle: String
    let descKey: String
    let fallbackDesc: String
    var status: UnboxPermStatus
    var canPrompt: Bool
    var canOpenSettings: Bool
    var optional: Bool = false
}

@MainActor
@Observable
final class PermissionsManager {
    static let shared = PermissionsManager()

    private static let doneKey = "bothub.unboxPermsDone"
    private static let networkPrimedKey = "bothub.unboxNetworkPrimed"

    var items: [UnboxPermissionItem] = []
    var loading = false
    var requesting = false
    var busyItemId = ""
    var feedback = ""
    var unboxDone: Bool = UserDefaults.standard.bool(forKey: PermissionsManager.doneKey)

    @ObservationIgnored private var networkPrimed: Bool = UserDefaults.standard.bool(forKey: PermissionsManager.networkPrimedKey)
    @ObservationIgnored private var followUpTask: Task<Void, Never>?

    var autoAdvanceOnGrantAll: Bool {
        #if os(macOS)
        return false
        #else
        return true
        #endif
    }

    var subtitleKey: String {
        #if os(macOS)
        return "ubPermSubMac"
        #else
        return "ubPermSubMobile"
        #endif
    }

    var coreItems: [UnboxPermissionItem] {
        let filtered = items.filter { !$0.optional }
        return filtered.isEmpty ? items : filtered
    }

    var grantedCoreCount: Int {
        coreItems.filter { $0.status == .granted || $0.status == .requested }.count
    }

    var allCoreGranted: Bool {
        !coreItems.isEmpty && grantedCoreCount == coreItems.count
    }

    var hasUngrantedCore: Bool {
        #if os(macOS)
        return coreItems.contains { $0.status != .granted && $0.status != .requested }
        #else
        // Local network & wireless data authorization cannot be queried without triggering a prompt,
        // so the gate check looks at verifiable permissions (notifications & camera).
        return coreItems.contains { $0.id != "network" && $0.status != .granted }
        #endif
    }

    var hasPromptableUngranted: Bool {
        items.contains { $0.canPrompt && $0.status != .granted && $0.status != .requested }
    }

    private init() {}

    func markUnboxDone() {
        unboxDone = true
        UserDefaults.standard.set(true, forKey: Self.doneKey)
    }

    private func markNetworkPrimed() {
        networkPrimed = true
        UserDefaults.standard.set(true, forKey: Self.networkPrimedKey)
    }

    func refresh() async {
        loading = true
        defer { loading = false }

        await PushManager.shared.refreshAuthorizationStatus()
        let notifItem = mapNotificationItem(PushManager.shared.authStatus)

        #if os(macOS)
        var built = [notifItem]
        if DesktopHostManager.shared.ready {
            let hostPerms = await DesktopHostManager.shared.fetchHostPermissions()
            for hp in hostPerms where hp.status != "unsupported" {
                built.append(UnboxPermissionItem(
                    id: hp.id,
                    titleKey: "deskPermName_\(hp.id)",
                    fallbackTitle: hp.name,
                    descKey: "deskPermDesc_\(hp.id)",
                    fallbackDesc: hp.description,
                    status: mapHostStatus(hp.status),
                    canPrompt: hp.canPrompt ?? false,
                    canOpenSettings: hp.canOpenSettings ?? true,
                    optional: hp.id == "full_disk_access"
                ))
            }
        }
        items = built
        #elseif os(iOS)
        let camAuth = AVCaptureDevice.authorizationStatus(for: .video)
        let camStatus: UnboxPermStatus = switch camAuth {
        case .authorized: .granted
        case .denied, .restricted: .denied
        default: .notDetermined
        }
        let camItem = UnboxPermissionItem(
            id: "camera",
            titleKey: "ubPermCameraName",
            fallbackTitle: "Camera",
            descKey: "ubPermCameraDesc",
            fallbackDesc: "Scan pairing QR code.",
            status: camStatus,
            canPrompt: camAuth == .notDetermined,
            canOpenSettings: true,
            optional: false
        )
        let netItem = UnboxPermissionItem(
            id: "network",
            titleKey: "ubPermNetworkName",
            fallbackTitle: "Wireless Data & Local Network",
            descKey: "ubPermNetworkDesc",
            fallbackDesc: "Connect to your host over LAN or Tailcat.",
            status: networkPrimed ? .requested : .notDetermined,
            canPrompt: !networkPrimed,
            canOpenSettings: true,
            optional: false
        )
        items = [notifItem, camItem, netItem]
        #endif
    }

    func requestAll() async {
        guard !requesting else { return }
        requesting = true
        busyItemId = "all"
        feedback = ""
        defer {
            requesting = false
            busyItemId = ""
        }

        #if os(macOS)
        await PushManager.shared.refreshAuthorizationStatus()
        if PushManager.shared.authStatus == .notDetermined {
            await PushManager.shared.requestPermissionAndRegister()
        }
        await primeLocalNetwork(duration: .milliseconds(600))
        if DesktopHostManager.shared.ready {
            let hostPerms = await DesktopHostManager.shared.fetchHostPermissions()
            if hostPerms.contains(where: { ($0.canPrompt ?? false) && $0.status != "granted" }) {
                if let res = await DesktopHostManager.shared.requestHostPermission("all", openSettings: false),
                   res.prompted == true {
                    feedback = L.t("deskPermPrompted")
                }
            }
        }
        await refresh()
        scheduleFollowUpRefresh()
        #elseif os(iOS)
        await primeWirelessData()
        await PushManager.shared.refreshAuthorizationStatus()
        if PushManager.shared.authStatus == .notDetermined {
            await PushManager.shared.requestPermissionAndRegister()
        }
        if AVCaptureDevice.authorizationStatus(for: .video) == .notDetermined {
            _ = await AVCaptureDevice.requestAccess(for: .video)
        }
        await primeLocalNetwork(duration: .milliseconds(800))
        markNetworkPrimed()
        await refresh()
        #endif
    }

    func requestItem(_ item: UnboxPermissionItem, openSettings: Bool) async {
        guard busyItemId.isEmpty else { return }
        busyItemId = item.id
        feedback = ""
        defer { busyItemId = "" }

        if item.id == "notifications" {
            if openSettings || item.status == .denied {
                if let url = platformNotificationSettingsURL {
                    openExternalURL(url)
                    feedback = L.t("deskPermOpened")
                }
            } else {
                await PushManager.shared.requestPermissionAndRegister()
                await refresh()
            }
            return
        }

        #if os(iOS)
        if item.id == "camera" {
            if openSettings || item.status == .denied {
                if let url = platformAppSettingsURL {
                    openExternalURL(url)
                    feedback = L.t("deskPermOpened")
                }
            } else {
                _ = await AVCaptureDevice.requestAccess(for: .video)
                await refresh()
            }
            return
        }
        if item.id == "network" {
            if openSettings {
                if let url = platformAppSettingsURL {
                    openExternalURL(url)
                    feedback = L.t("deskPermOpened")
                }
            } else {
                await primeWirelessData()
                await primeLocalNetwork(duration: .milliseconds(800))
                markNetworkPrimed()
                await refresh()
            }
            return
        }
        #endif

        #if os(macOS)
        if DesktopHostManager.shared.ready {
            if let res = await DesktopHostManager.shared.requestHostPermission(item.id, openSettings: openSettings) {
                if res.openedSettings == true {
                    feedback = L.t("deskPermOpened")
                } else if res.prompted == true {
                    feedback = L.t("deskPermPrompted")
                }
            }
            await refresh()
            scheduleFollowUpRefresh()
        }
        #endif
    }

    func primeSilentNetworkIfNeeded() async {
        #if os(iOS)
        guard !networkPrimed else { return }
        await primeWirelessData()
        await primeLocalNetwork(duration: .milliseconds(600))
        markNetworkPrimed()
        #endif
    }

    private func scheduleFollowUpRefresh() {
        followUpTask?.cancel()
        followUpTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(1500))
            guard !Task.isCancelled, let self else { return }
            await self.refresh()
        }
    }

    private func primeLocalNetwork(duration: Duration) async {
        let browser = NWBrowser(for: .bonjour(type: "_bothub._tcp", domain: nil), using: .tcp)
        browser.start(queue: DispatchQueue(label: "local.bothub.netprobe"))
        try? await Task.sleep(for: duration)
        browser.cancel()
    }

    private func primeWirelessData() async {
        guard let url = URL(string: "https://captive.apple.com/hotspot-detect.html") else { return }
        var req = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 4)
        req.httpMethod = "HEAD"
        _ = try? await URLSession.shared.data(for: req)
    }

    private func mapNotificationItem(_ auth: UNAuthorizationStatus) -> UnboxPermissionItem {
        let status: UnboxPermStatus = switch auth {
        case .authorized, .provisional, .ephemeral: .granted
        case .denied: .denied
        default: .notDetermined
        }
        return UnboxPermissionItem(
            id: "notifications",
            titleKey: "ubPermNotifName",
            fallbackTitle: "Notifications",
            descKey: "ubPermNotifDesc",
            fallbackDesc: "Receive task completion and takeover alerts.",
            status: status,
            canPrompt: auth == .notDetermined,
            canOpenSettings: true,
            optional: false
        )
    }

    private func mapHostStatus(_ raw: String) -> UnboxPermStatus {
        switch raw {
        case "granted": return .granted
        case "denied": return .denied
        case "unsupported": return .unsupported
        default: return .notDetermined
        }
    }
}
