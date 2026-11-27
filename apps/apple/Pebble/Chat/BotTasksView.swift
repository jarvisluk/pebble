import SwiftUI

enum TaskFilterKind: String, CaseIterable, Hashable {
    case all
    case worker
    case routine
    case desktop
}

@MainActor
func taskDeviceLabel(_ deviceId: String?, nodes: [NodeInfo]) -> String? {
    let clean = (deviceId ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    guard !clean.isEmpty, clean != "local" else { return nil }
    if let match = nodes.first(where: { $0.id == clean }), let name = match.name, !name.isEmpty {
        return name
    }
    return clean
}

@MainActor
func taskStatusVisual(_ status: String, palette p: Palette) -> (fg: Color, bg: Color) {
    switch status {
    case "running":
        return (p.sageInk, p.sage)
    case "queued":
        return (p.text2, p.s1)
    case "waiting_approval", "waiting_question":
        return (p.busy, p.busy.opacity(0.16))
    case "waiting_takeover":
        return (p.pinkInk, p.pink)
    case "completed":
        return (p.sageInk, p.sage)
    case "failed":
        return (p.pinkInk, p.pink)
    case "cancelled":
        return (p.mute, p.s1)
    default:
        return (p.text2, p.s1)
    }
}

@MainActor
func taskStatusText(_ status: String, hub: HubStore) -> String {
    let key = "taskStatus_\(status)"
    let val = hub.t(key)
    return val == key ? status : val
}

@MainActor
func taskKindText(_ kind: String, hub: HubStore) -> String {
    let key = "taskKind_\(kind)"
    let val = hub.t(key)
    return val == key ? kind : val
}

private func stripTasks(from allTasks: [BotTask]) -> [BotTask] {
    Array(allTasks.filter(\.isActive).prefix(6))
}

/// Compact horizontal strip displayed in active Bot Chat for running & recent child tasks.
struct BotChatTaskStrip: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var botId: String
    var pal: (accent: Color, text: Color, soft: Color)
    var onSelectTask: (BotTask) -> Void

    private var items: [BotTask] {
        stripTasks(from: hub.tasks[botId] ?? [])
    }

    var body: some View {
        if !items.isEmpty {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(items) { task in
                        taskCapsule(task)
                    }
                }
                .padding(.horizontal, 16)
                .padding(.top, 4)
                .padding(.bottom, 2)
            }
            .frame(maxWidth: 840)
            .frame(maxWidth: .infinity)
        }
    }

    private func taskCapsule(_ task: BotTask) -> some View {
        let vis = taskStatusVisual(task.status, palette: p)
        let deviceLabel = taskDeviceLabel(task.deviceId, nodes: hub.nodes)
        let activeTool = task.status == "running" ? (hub.taskTools[task.id] ?? "") : ""
        let harness = (task.harness ?? "").trimmingCharacters(in: .whitespacesAndNewlines)

        return Button {
            onSelectTask(task)
        } label: {
            HStack(spacing: 6) {
                Circle()
                    .fill(task.isActive ? vis.fg : p.mute)
                    .frame(width: 7, height: 7)

                Text(task.displayTitle)
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                    .frame(maxWidth: 160, alignment: .leading)

                if let deviceLabel {
                    Text(deviceLabel)
                        .font(.system(size: AppFont.caption, weight: .medium))
                        .foregroundStyle(p.lilacInk)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(p.lilac, in: Capsule())
                        .lineLimit(1)
                }

                if !harness.isEmpty {
                    Text(harness)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(p.text2)
                        .lineLimit(1)
                }

                if !activeTool.isEmpty {
                    Text(activeTool)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(pal.accent)
                        .lineLimit(1)
                }

                Text(taskStatusText(task.status, hub: hub))
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(vis.fg)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(vis.bg, in: Capsule())
                    .lineLimit(1)
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(p.card, in: Capsule())
            .overlay(Capsule().stroke(task.isWaiting ? vis.fg.opacity(0.45) : p.s2, lineWidth: 0.75))
        }
        .buttonStyle(PressStyle())
    }
}

/// Unified Tasks / Activity section inside Bot Detail Inspector.
struct BotTasksSection: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var botId: String
    var onSelectTask: (BotTask) -> Void

    @State private var filter: TaskFilterKind = .all
    @State private var loading = false
    @State private var err = ""

    private var allTasks: [BotTask] { hub.tasks[botId] ?? [] }

    private var filteredTasks: [BotTask] {
        let base: [BotTask] = switch filter {
        case .all: allTasks
        case .worker: allTasks.filter { $0.kind == "worker" }
        case .routine: allTasks.filter { $0.kind == "routine" }
        case .desktop: allTasks.filter { $0.kind == "desktop" }
        }
        return Array(base.prefix(6))
    }

    var body: some View {
        let cardFill = p.dark ? Color(hex: "#1c1c1e") : p.card
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Text(hub.t("tasksTitle"))
                Spacer()
                Button {
                    Task { await reload() }
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
                .disabled(loading)
            }
            .font(.system(size: AppFont.secondary))
            .foregroundStyle(p.mute)
            .padding(.horizontal, 16)

            HubSegmentedPicker(
                selection: $filter,
                options: [
                    (.all, hub.t("taskFilterAll")),
                    (.worker, hub.t("taskFilterWorkers")),
                    (.routine, hub.t("taskFilterRoutines")),
                    (.desktop, hub.t("taskFilterDesktop")),
                ]
            )

            VStack(alignment: .leading, spacing: 0) {
                if !err.isEmpty {
                    StatusText(text: err)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                }
                if filteredTasks.isEmpty {
                    Text(hub.t("taskEmpty"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 14)
                } else {
                    ForEach(Array(filteredTasks.enumerated()), id: \.element.id) { idx, task in
                        if idx > 0 {
                            Rectangle()
                                .fill(Color.primary.opacity(p.dark ? 0.10 : 0.08))
                                .frame(height: 0.5)
                                .padding(.leading, 16)
                        }
                        taskRow(task)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                    }
                }
            }
            .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
            .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        }
        .task(id: botId) {
            await reload()
        }
    }

    private func taskRow(_ task: BotTask) -> some View {
        let vis = taskStatusVisual(task.status, palette: p)
        let deviceLabel = taskDeviceLabel(task.deviceId, nodes: hub.nodes)
        let activeTool = task.status == "running" ? (hub.taskTools[task.id] ?? "") : ""
        let dur = formatDurationMs(task.durationMs)
        let stamp = formatShortTime(task.startedAt ?? task.createdAt)
        let meta = [
            taskKindText(task.kind, hub: hub),
            (task.harness ?? "").isEmpty ? nil : task.harness,
            (task.model ?? "").isEmpty ? nil : task.model,
            dur.isEmpty ? nil : dur,
            (task.costUsd ?? 0) > 0 ? String(format: "$%.4f", task.costUsd!) : nil,
            stamp.isEmpty ? nil : stamp,
        ].compactMap { $0 }.joined(separator: " · ")

        return Button {
            onSelectTask(task)
        } label: {
            VStack(alignment: .leading, spacing: 5) {
                HStack(spacing: 6) {
                    Text(task.displayTitle)
                        .font(.system(size: AppFont.body, weight: .semibold))
                        .foregroundStyle(p.text)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if let deviceLabel {
                        Tag(text: deviceLabel, bg: p.lilac, fg: p.lilacInk)
                    }
                    Tag(text: taskStatusText(task.status, hub: hub), bg: vis.bg, fg: vis.fg)
                }
                if !meta.isEmpty {
                    Text(meta)
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                if !activeTool.isEmpty {
                    Text(hub.t("callingTool", ["tool": activeTool]))
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(p.busy)
                        .lineLimit(1)
                } else if let e = task.error, !e.isEmpty {
                    Text(e)
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.danger)
                        .lineLimit(2)
                } else if let out = task.output?.trimmingCharacters(in: .whitespacesAndNewlines), !out.isEmpty {
                    Text(plainPreview(out))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.text2)
                        .lineLimit(2)
                } else if !task.prompt.isEmpty {
                    Text(task.prompt)
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func reload() async {
        guard hub.isConnected else { return }
        loading = true
        defer { loading = false }
        do {
            _ = try await hub.fetchBotTasks(botId, limit: 30)
            err = ""
        } catch {
            err = errText(error, hub.t("connFailed"))
        }
    }
}
