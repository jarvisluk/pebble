import Foundation
import Observation

#if os(iOS)
import ActivityKit

@MainActor
@Observable
final class LiveActivityManager {
    static let shared = LiveActivityManager()

    var activitiesEnabled: Bool {
        ActivityAuthorizationInfo().areActivitiesEnabled
    }

    @ObservationIgnored private var botActivities: [String: Activity<PebbleActivityAttributes>] = [:]
    @ObservationIgnored private var botStartedAt: [String: Double] = [:]
    @ObservationIgnored private var botSteps: [String: Int] = [:]
    @ObservationIgnored private var desktopActivities: [String: Activity<PebbleActivityAttributes>] = [:]
    @ObservationIgnored private var tokenTasks: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var startTokenTask: Task<Void, Never>?

    private init() {}

    func startObservingTokens() {
        if startTokenTask == nil, #available(iOS 17.2, *) {
            startTokenTask = Task {
                for await tokenData in Activity<PebbleActivityAttributes>.pushToStartTokenUpdates {
                    let hex = tokenData.map { String(format: "%02x", $0) }.joined()
                    PushManager.shared.setLiveActivityStartToken(hex)
                }
            }
        }
    }

    func handleBotStatus(_ bot: BotStatus) {
        guard activitiesEnabled else { return }
        if bot.isBusy {
            if botActivities[bot.id] != nil { return }
            let now = Date().timeIntervalSince1970
            botStartedAt[bot.id] = now
            botSteps[bot.id] = 0
            let attrs = PebbleActivityAttributes(
                kind: "bot",
                botId: bot.id,
                botName: bot.displayName,
                target: nil
            )
            let state = PebbleActivityAttributes.ContentState(
                phase: "running",
                statusText: L.lang == "en" ? "Thinking…" : "正在思考",
                stepCount: 0,
                startedAt: now,
                summary: nil,
                takeoverReason: nil
            )
            if let act = try? Activity.request(
                attributes: attrs,
                content: .init(state: state, staleDate: nil),
                pushType: .token
            ) {
                botActivities[bot.id] = act
                observeActivityToken(act, key: "bot:\(bot.id)")
            }
        } else if let act = botActivities.removeValue(forKey: bot.id) {
            let started = botStartedAt.removeValue(forKey: bot.id) ?? Date().timeIntervalSince1970
            let steps = botSteps.removeValue(forKey: bot.id) ?? 0
            let finalState = PebbleActivityAttributes.ContentState(
                phase: "completed",
                statusText: L.lang == "en" ? "Completed" : "已完成",
                stepCount: steps,
                startedAt: started,
                summary: L.lang == "en" ? "Task finished" : "任务已完成",
                takeoverReason: nil
            )
            Task {
                await act.end(
                    .init(state: finalState, staleDate: nil),
                    dismissalPolicy: .after(Date().addingTimeInterval(180))
                )
            }
            tokenTasks.removeValue(forKey: "bot:\(bot.id)")?.cancel()
        }
    }

    func handleToolUse(botId: String, tool: String) {
        guard activitiesEnabled, let act = botActivities[botId] else { return }
        let steps = (botSteps[botId] ?? 0) + 1
        botSteps[botId] = steps
        let started = botStartedAt[botId] ?? Date().timeIntervalSince1970
        let status = tool.isEmpty
            ? (L.lang == "en" ? "Running tool" : "正在运行工具")
            : (L.lang == "en" ? "Running \(tool)" : "正在运行 \(tool)")
        let state = PebbleActivityAttributes.ContentState(
            phase: "running",
            statusText: status,
            stepCount: steps,
            startedAt: started,
            summary: nil,
            takeoverReason: nil
        )
        Task {
            await act.update(.init(state: state, staleDate: nil))
        }
    }

    func handleTakeovers(_ items: [TakeoverRequest]) {
        guard activitiesEnabled else { return }
        var activeTargets = Set<String>()
        for item in items {
            let target = (item.target ?? "").isEmpty ? "local" : item.target!
            activeTargets.insert(target)
            let reason = (item.reason ?? "").isEmpty
                ? (L.lang == "en" ? "Manual action required" : "需要人工接管完成操作")
                : item.reason!
            let state = PebbleActivityAttributes.ContentState(
                phase: "takeover",
                statusText: L.lang == "en" ? "Waiting for takeover" : "等待人工接管",
                stepCount: 0,
                startedAt: Date().timeIntervalSince1970,
                summary: nil,
                takeoverReason: reason
            )
            if let existing = desktopActivities[target] {
                Task { await existing.update(.init(state: state, staleDate: nil)) }
            } else {
                let attrs = PebbleActivityAttributes(
                    kind: "desktop",
                    botId: nil,
                    botName: nil,
                    target: target
                )
                if let act = try? Activity.request(
                    attributes: attrs,
                    content: .init(state: state, staleDate: nil),
                    pushType: .token
                ) {
                    desktopActivities[target] = act
                    observeActivityToken(act, key: "desktop:\(target)")
                }
            }
        }

        for (target, act) in desktopActivities where !activeTargets.contains(target) {
            desktopActivities.removeValue(forKey: target)
            tokenTasks.removeValue(forKey: "desktop:\(target)")?.cancel()
            let doneState = PebbleActivityAttributes.ContentState(
                phase: "completed",
                statusText: L.lang == "en" ? "Takeover resolved" : "接管已完成",
                stepCount: 0,
                startedAt: Date().timeIntervalSince1970,
                summary: L.lang == "en" ? "Automation resumed" : "远程桌面已恢复自动执行",
                takeoverReason: nil
            )
            Task {
                await act.end(
                    .init(state: doneState, staleDate: nil),
                    dismissalPolicy: .after(Date().addingTimeInterval(60))
                )
            }
        }
    }

    private func observeActivityToken(_ activity: Activity<PebbleActivityAttributes>, key: String) {
        tokenTasks[key]?.cancel()
        tokenTasks[key] = Task {
            for await data in activity.pushTokenUpdates {
                let hex = data.map { String(format: "%02x", $0) }.joined()
                await PushManager.shared.reportActivityToken(activityKey: key, token: hex)
            }
        }
    }
}

#else

@MainActor
@Observable
final class LiveActivityManager {
    static let shared = LiveActivityManager()
    var activitiesEnabled: Bool { false }
    private init() {}
    func startObservingTokens() {}
    func handleBotStatus(_ bot: BotStatus) {}
    func handleToolUse(botId: String, tool: String) {}
    func handleTakeovers(_ items: [TakeoverRequest]) {}
}

#endif
