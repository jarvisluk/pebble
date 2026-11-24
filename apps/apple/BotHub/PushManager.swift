import Foundation
import Observation
import UIKit
import UserNotifications

struct PushStatusInfo: Codable, Equatable {
    var configured: Bool = false
    var mode: String = "disabled"
    var deviceCount: Int = 0
}

@MainActor
@Observable
final class PushManager {
    static let shared = PushManager()

    var authStatus: UNAuthorizationStatus = .notDetermined
    var pushToken: String = ""
    var liveActivityStartToken: String = ""
    var serverStatus = PushStatusInfo()
    var lastError = ""

    @ObservationIgnored var onDeepLink: ((URL) -> Void)?
    @ObservationIgnored var activeChatBotId: (() -> String?)?
    @ObservationIgnored private var apiBase = ""
    @ObservationIgnored private var apiToken = ""

    let deviceId: String

    private init() {
        let key = "bothub.push.deviceId"
        if let saved = UserDefaults.standard.string(forKey: key), !saved.isEmpty {
            deviceId = saved
        } else {
            let fresh = UUID().uuidString.lowercased()
            UserDefaults.standard.set(fresh, forKey: key)
            deviceId = fresh
        }
        Task { await refreshAuthorizationStatus() }
    }

    func refreshAuthorizationStatus() async {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        authStatus = settings.authorizationStatus
        if settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional {
            UIApplication.shared.registerForRemoteNotifications()
        }
    }

    func requestPermissionAndRegister() async {
        do {
            let granted = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
            await refreshAuthorizationStatus()
            if granted {
                UIApplication.shared.registerForRemoteNotifications()
                await syncRegistration()
            }
        } catch {
            lastError = error.localizedDescription
        }
    }

    func configureConnection(base: String, token: String) {
        apiBase = base
        apiToken = token
        Task {
            await refreshAuthorizationStatus()
            await syncRegistration()
            await fetchServerStatus()
        }
    }

    func setPushToken(_ tokenData: Data) {
        let hex = tokenData.map { String(format: "%02x", $0) }.joined()
        guard !hex.isEmpty, hex != pushToken else { return }
        pushToken = hex
        Task { await syncRegistration() }
    }

    func setLiveActivityStartToken(_ token: String) {
        guard !token.isEmpty, token != liveActivityStartToken else { return }
        liveActivityStartToken = token
        Task { await syncRegistration() }
    }

    func reportActivityToken(activityKey: String, token: String) async {
        guard !apiBase.isEmpty, !activityKey.isEmpty, !token.isEmpty else { return }
        let body: [String: Any] = [
            "deviceId": deviceId,
            "activityKey": activityKey,
            "token": token,
        ]
        _ = try? await APIClient(base: apiBase, token: apiToken).raw("/api/push/activity-token", method: "POST", body: body)
    }

    func fetchServerStatus() async {
        guard !apiBase.isEmpty else { return }
        if let st: PushStatusInfo = try? await APIClient(base: apiBase, token: apiToken).call("/api/push/status") {
            serverStatus = st
        }
    }

    func syncRegistration() async {
        guard !apiBase.isEmpty else { return }
        guard !pushToken.isEmpty || !liveActivityStartToken.isEmpty else { return }
        #if DEBUG
        let env = "sandbox"
        #else
        let env = "production"
        #endif
        let bundleId = Bundle.main.bundleIdentifier ?? "local.bothub.mobile"
        let body: [String: Any] = [
            "deviceId": deviceId,
            "platform": "ios",
            "env": env,
            "bundleId": bundleId,
            "pushToken": pushToken,
            "liveActivityStartToken": liveActivityStartToken,
            "enabled": true,
        ]
        _ = try? await APIClient(base: apiBase, token: apiToken).raw("/api/push/register", method: "POST", body: body)
        await fetchServerStatus()
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Task { @MainActor in
            PushManager.shared.setPushToken(deviceToken)
        }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        Task { @MainActor in
            PushManager.shared.lastError = error.localizedDescription
        }
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        let userInfo = notification.request.content.userInfo
        let botId = userInfo["botId"] as? String
        let suppress = await MainActor.run {
            if let botId, !botId.isEmpty, PushManager.shared.activeChatBotId?() == botId {
                return true
            }
            return false
        }
        return suppress ? [] : [.banner, .sound, .list]
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        let userInfo = response.notification.request.content.userInfo
        if let raw = userInfo["deepLink"] as? String, let url = URL(string: raw) {
            await MainActor.run {
                PushManager.shared.onDeepLink?(url)
            }
        }
    }
}
