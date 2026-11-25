import ActivityKit
import AppIntents
import SwiftUI
import WidgetKit

struct PebbleLiveActivityWidget: Widget {
    private var isEn: Bool { PebbleSharedStore.isEnglish }

    var body: some WidgetConfiguration {
        ActivityConfiguration(for: PebbleActivityAttributes.self) { context in
            LockScreenView(context: context)
                .widgetURL(deepLinkURL(for: context))
                .activityBackgroundTint(backgroundColor(for: context.state.phase))
                .activitySystemActionForegroundColor(.primary)
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Label {
                        Text(displayTitle(for: context))
                            .font(.subheadline.weight(.semibold))
                            .lineLimit(1)
                    } icon: {
                        Image(systemName: iconName(for: context))
                            .foregroundStyle(accentColor(for: context.state.phase))
                    }
                }
                DynamicIslandExpandedRegion(.trailing) {
                    if context.state.phase == "running" || context.state.phase == "takeover" || context.state.phase == "approval" {
                        Text(timerInterval: context.state.startedDate...Date.distantFuture, countsDown: false)
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.trailing)
                    } else if context.state.stepCount > 0 {
                        Text(stepText(context.state.stepCount))
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(subtitleText(for: context))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)

                        HStack(spacing: 8) {
                            if context.attributes.kind == "desktop" || context.state.phase == "takeover" {
                                if let url = deepLinkURL(for: context) {
                                    Link(destination: url) {
                                        Label(isEn ? "Take Over" : "立即接管", systemImage: "hand.tap.fill")
                                            .font(.caption.weight(.semibold))
                                            .frame(maxWidth: .infinity)
                                    }
                                    .buttonStyle(.borderedProminent)
                                    .tint(.orange)
                                }
                            } else {
                                if context.state.phase == "running" || context.state.phase == "approval",
                                   let botId = context.attributes.botId, !botId.isEmpty {
                                    Button(intent: InterruptBotIntent(botId: botId)) {
                                        Label(isEn ? "Stop" : "中断", systemImage: "stop.fill")
                                            .font(.caption.weight(.semibold))
                                    }
                                    .buttonStyle(.bordered)
                                    .tint(.red)
                                }
                                if let url = deepLinkURL(for: context) {
                                    Link(destination: url) {
                                        Label(isEn ? "Open Chat" : "查看会话", systemImage: "arrow.up.right")
                                            .font(.caption.weight(.semibold))
                                            .frame(maxWidth: .infinity)
                                    }
                                    .buttonStyle(.borderedProminent)
                                    .tint(Color(red: 0.85, green: 0.47, blue: 0.29))
                                }
                            }
                        }
                    }
                    .padding(.top, 4)
                }
            } compactLeading: {
                Image(systemName: iconName(for: context))
                    .foregroundStyle(accentColor(for: context.state.phase))
            } compactTrailing: {
                if context.state.phase == "running" || context.state.phase == "takeover" || context.state.phase == "approval" {
                    Text(timerInterval: context.state.startedDate...Date.distantFuture, countsDown: false)
                        .font(.caption2.monospacedDigit())
                        .frame(maxWidth: 44)
                } else {
                    Image(systemName: context.state.phase == "error" ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                        .foregroundStyle(accentColor(for: context.state.phase))
                }
            } minimal: {
                Image(systemName: iconName(for: context))
                    .foregroundStyle(accentColor(for: context.state.phase))
            }
            .widgetURL(deepLinkURL(for: context))
        }
    }

    private func stepText(_ n: Int) -> String {
        isEn ? (n == 1 ? "1 step" : "\(n) steps") : "\(n) 步"
    }

    private func desktopTitle(_ target: String?) -> String {
        let resolved = (target ?? "").isEmpty ? "local" : target!
        let base = isEn ? "Remote Desktop" : "远程桌面"
        return resolved == "local" ? base : "\(base) · \(resolved)"
    }

    private func displayTitle(for context: ActivityViewContext<PebbleActivityAttributes>) -> String {
        if context.attributes.kind == "desktop" {
            return desktopTitle(context.attributes.target)
        }
        return context.attributes.botName ?? context.attributes.botId ?? "Pebble"
    }

    private func subtitleText(for context: ActivityViewContext<PebbleActivityAttributes>) -> String {
        if context.state.phase == "takeover", let reason = context.state.takeoverReason, !reason.isEmpty {
            return reason
        }
        if context.state.phase == "completed", let summary = context.state.summary, !summary.isEmpty {
            return summary
        }
        if context.state.stepCount > 0 {
            return "\(context.state.statusText) · \(stepText(context.state.stepCount))"
        }
        return context.state.statusText
    }

    private func iconName(for context: ActivityViewContext<PebbleActivityAttributes>) -> String {
        if context.state.phase == "takeover" || context.attributes.kind == "desktop" {
            return "display"
        }
        switch context.state.phase {
        case "approval": return "checkmark.shield.fill"
        case "completed": return "checkmark.circle.fill"
        case "error": return "exclamationmark.triangle.fill"
        default: return "sparkles"
        }
    }

    private func accentColor(for phase: String) -> Color {
        switch phase {
        case "takeover", "approval": return .orange
        case "completed": return .green
        case "error": return .red
        default: return Color(red: 0.85, green: 0.47, blue: 0.29)
        }
    }

    private func backgroundColor(for phase: String) -> Color {
        switch phase {
        case "takeover", "approval": return Color.orange.opacity(0.18)
        case "error": return Color.red.opacity(0.15)
        default: return Color(UIColor.secondarySystemBackground).opacity(0.88)
        }
    }

    private func deepLinkURL(for context: ActivityViewContext<PebbleActivityAttributes>) -> URL? {
        if context.attributes.kind == "desktop" {
            let target = (context.attributes.target ?? "local").addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? "local"
            return URL(string: "pebble://desktop?target=\(target)")
        }
        if let botId = context.attributes.botId, !botId.isEmpty {
            let encId = botId.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? botId
            return URL(string: "pebble://bot/\(encId)")
        }
        return nil
    }
}

private struct LockScreenView: View {
    let context: ActivityViewContext<PebbleActivityAttributes>

    private var isEn: Bool { PebbleSharedStore.isEnglish }

    private var titleText: String {
        if context.attributes.kind == "desktop" {
            let resolved = (context.attributes.target ?? "").isEmpty ? "local" : context.attributes.target!
            let base = isEn ? "Remote Desktop" : "远程桌面"
            return resolved == "local" ? base : "\(base) · \(resolved)"
        }
        return context.attributes.botName ?? context.attributes.botId ?? "Pebble"
    }

    private var accent: Color {
        switch context.state.phase {
        case "takeover", "approval": return .orange
        case "completed": return .green
        case "error": return .red
        default: return Color(red: 0.85, green: 0.47, blue: 0.29)
        }
    }

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: context.attributes.kind == "desktop" || context.state.phase == "takeover"
                  ? "display"
                  : (context.state.phase == "approval" ? "checkmark.shield.fill" : "sparkles"))
                .font(.title3.weight(.semibold))
                .foregroundStyle(accent)
                .frame(width: 34, height: 34)
                .background(accent.opacity(0.14), in: Circle())

            VStack(alignment: .leading, spacing: 3) {
                HStack {
                    Text(titleText)
                        .font(.subheadline.weight(.semibold))
                        .lineLimit(1)
                    Spacer()
                    if context.state.phase == "running" || context.state.phase == "takeover" || context.state.phase == "approval" {
                        Text(timerInterval: context.state.startedDate...Date.distantFuture, countsDown: false)
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                }
                Text(context.state.takeoverReason?.isEmpty == false ? (context.state.takeoverReason ?? "") :
                        (context.state.summary?.isEmpty == false ? (context.state.summary ?? "") : context.state.statusText))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }

            if context.state.phase == "running" || context.state.phase == "approval",
               let botId = context.attributes.botId, !botId.isEmpty {
                Button(intent: InterruptBotIntent(botId: botId)) {
                    Image(systemName: "stop.fill")
                        .font(.caption.weight(.bold))
                        .padding(8)
                }
                .buttonStyle(.bordered)
                .tint(.red)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 14)
    }
}
