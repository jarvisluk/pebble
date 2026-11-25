import ActivityKit
import AppIntents
import Foundation

struct PebbleActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var phase: String            // "running" | "takeover" | "completed" | "error"
        var statusText: String
        var stepCount: Int
        var startedAt: Double        // Unix epoch seconds
        var summary: String?
        var takeoverReason: String?

        var startedDate: Date {
            Date(timeIntervalSince1970: startedAt)
        }
    }

    var kind: String                 // "bot" | "desktop"
    var botId: String?
    var botName: String?
    var target: String?
}

enum PebbleSharedStore {
    static var appGroupID: String {
        let bundleID = Bundle.main.bundleIdentifier ?? "local.pebble.mobile"
        if bundleID.hasSuffix(".LiveActivity") {
            let mainID = String(bundleID.dropLast(".LiveActivity".count))
            return "group.\(mainID)"
        }
        return "group.\(bundleID)"
    }

    private static var defaults: UserDefaults? {
        UserDefaults(suiteName: appGroupID)
    }

    static func setPendingInterrupt(botId: String?) {
        defaults?.set(botId, forKey: "pendingInterruptBotId")
    }

    static func consumePendingInterrupt() -> String? {
        guard let id = defaults?.string(forKey: "pendingInterruptBotId"), !id.isEmpty else {
            return nil
        }
        defaults?.removeObject(forKey: "pendingInterruptBotId")
        return id
    }
}

struct InterruptBotIntent: LiveActivityIntent {
    static var title: LocalizedStringResource = "中断任务"
    static var openAppWhenRun: Bool = true

    @Parameter(title: "Bot ID")
    var botId: String

    init() {
        self.botId = ""
    }

    init(botId: String) {
        self.botId = botId
    }

    func perform() async throws -> some IntentResult {
        if !botId.isEmpty {
            PebbleSharedStore.setPendingInterrupt(botId: botId)
        }
        return .result()
    }
}
