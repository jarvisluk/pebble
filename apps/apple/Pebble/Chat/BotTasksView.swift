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
    let active = allTasks.filter(\.isActive)
    let recentDone = allTasks.filter(\.isTerminal).prefix(max(0, 5 - active.count))
    return Array((active + recentDone).prefix(6))
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
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
            }
            .background(p.page)
            .overlay(alignment: .bottom) {
                Rectangle().fill(p.s2).frame(height: 0.5)
            }
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
    var onNewTask: () -> Void

    @State private var filter: TaskFilterKind = .all
    @State private var loading = false
    @State private var err = ""

    private var allTasks: [BotTask] { hub.tasks[botId] ?? [] }

    private var filteredTasks: [BotTask] {
        switch filter {
        case .all: return allTasks
        case .worker: return allTasks.filter { $0.kind == "worker" }
        case .routine: return allTasks.filter { $0.kind == "routine" }
        case .desktop: return allTasks.filter { $0.kind == "desktop" }
        }
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

                Button {
                    onNewTask()
                } label: {
                    Label(hub.t("newTask"), systemImage: "plus")
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
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

        return VStack(alignment: .leading, spacing: 6) {
            Button {
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

            if task.isActive {
                HStack(spacing: 8) {
                    Spacer()
                    Button(hub.t("taskCancelBtn"), role: .destructive) {
                        Task {
                            do {
                                _ = try await hub.cancelTask(task.id)
                            } catch {
                                err = errText(error, hub.t("taskCancelFailed"))
                            }
                        }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }
        }
    }

    private func reload() async {
        guard hub.isConnected else { return }
        loading = true
        defer { loading = false }
        do {
            _ = try await hub.fetchBotTasks(botId)
            err = ""
        } catch {
            err = errText(error, hub.t("connFailed"))
        }
    }
}

/// Quick "New Task" sheet for spawning a worker/desktop child task on local or remote agent nodes.
struct NewTaskSheet: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.dismiss) private var dismiss
    @Environment(\.palette) private var p

    var botId: String
    var onCreated: ((BotTask) -> Void)? = nil

    @State private var title = ""
    @State private var prompt = ""
    @State private var deviceId = "local"
    @State private var workdir = ""
    @State private var harness = ""
    @State private var model = ""
    @State private var permissionMode = ""
    @State private var delivery = "receipt"
    @State private var submitting = false
    @State private var err = ""

    private var bot: BotStatus? { hub.bots.first { $0.id == botId } }

    private var agentNodes: [NodeInfo] {
        hub.nodes.filter { ($0.connected ?? false) && ($0.permissions ?? []).contains("agent") }
    }

    private var selectedNode: NodeInfo? {
        guard deviceId != "local" else { return nil }
        return agentNodes.first { $0.id == deviceId }
    }

    private var deviceOptions: [(String, String)] {
        [("local", hub.t("taskDeviceLocal"))] + agentNodes.map { n in
            let label = (n.name ?? "").isEmpty ? n.id : n.name!
            return (n.id, label)
        }
    }

    private var harnessOptions: [(String, String)] {
        var opts: [(String, String)] = [("", hub.t("taskFollowBot"))]
        if let node = selectedNode, let nodeHarnesses = node.harnesses, !nodeHarnesses.isEmpty {
            let installed = nodeHarnesses.filter { $0.installed ?? false }
            let source = installed.isEmpty ? nodeHarnesses : installed
            for h in source {
                opts.append((h.kind, h.title))
            }
            return opts
        }
        let localHarnesses = settingsHarnessList(from: hub)
        let installed = localHarnesses.filter { $0.installed ?? true }
        for h in installed {
            opts.append((h.id, h.title))
        }
        return opts
    }

    private var permissionOptions: [(String, String)] {
        var modes = ["auto", "edit", "ask", "plan"]
        if let maxMode = selectedNode?.maxPermissionMode, !maxMode.isEmpty {
            let rank = ["plan": 0, "ask": 1, "edit": 2, "auto": 3]
            let maxRank = rank[maxMode.lowercased()] ?? 3
            modes = modes.filter { (rank[$0] ?? 0) <= maxRank }
        }
        var opts: [(String, String)] = [("", hub.t("taskFollowBot"))]
        for m in modes {
            let key = "permMode_\(m)"
            opts.append((m, hub.t(key) == key ? m : hub.t(key)))
        }
        return opts
    }

    var body: some View {
        let cardFill = p.dark ? Color(hex: "#1c1c1e") : p.card
        HubSheetScaffold(
            title: hub.t("newTask"),
            closeTitle: hub.t("cancel"),
            onClose: { dismiss() },
            actionTitle: hub.t("taskSpawnBtn"),
            actionDisabled: prompt.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
            loading: submitting,
            onAction: { submit() }
        ) {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    if !err.isEmpty {
                        StatusText(text: err)
                            .padding(.horizontal, 16)
                    }

                    VStack(alignment: .leading, spacing: 12) {
                        HubFormField(label: hub.t("taskTitlePh")) {
                            TextField(hub.t("taskTitlePh"), text: $title)
                                .textFieldStyle(.plain)
                                .font(.system(size: AppFont.body))
                                .padding(.horizontal, 10)
                                .padding(.vertical, 8)
                                .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                        }

                        HubFormField(label: hub.t("taskPromptPh")) {
                            TextField(hub.t("taskPromptPh"), text: $prompt, axis: .vertical)
                                .textFieldStyle(.plain)
                                .font(.system(size: AppFont.body))
                                .lineLimit(3...8)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 8)
                                .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                        }
                    }
                    .padding(16)
                    .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))

                    VStack(alignment: .leading, spacing: 12) {
                        HubMenuPicker(
                            title: hub.t("taskDeviceLabel"),
                            selection: Binding(
                                get: { deviceId },
                                set: { next in
                                    deviceId = next
                                    if next == "local" {
                                        workdir = ""
                                    } else if let ws = agentNodes.first(where: { $0.id == next })?.workspaces?.first {
                                        workdir = ws.path
                                    }
                                    if !harnessOptions.contains(where: { $0.0 == harness }) {
                                        harness = ""
                                    }
                                    if !permissionOptions.contains(where: { $0.0 == permissionMode }) {
                                        permissionMode = ""
                                    }
                                }
                            ),
                            options: deviceOptions
                        )

                        if let node = selectedNode, let wss = node.workspaces, !wss.isEmpty {
                            Divider()
                            HubMenuPicker(
                                title: hub.t("taskWorkdirLabel"),
                                selection: $workdir,
                                options: [("", hub.t("taskWorkdirPh"))] + wss.map { ($0.path, "\($0.name) (\($0.path))") }
                            )
                        } else if deviceId != "local" {
                            Divider()
                            HubFormField(label: hub.t("taskWorkdirLabel")) {
                                TextField(hub.t("taskWorkdirPh"), text: $workdir)
                                    .monoInput()
                                    .padding(.horizontal, 10)
                                    .padding(.vertical, 8)
                                    .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                            }
                        }

                        Divider()

                        HubMenuPicker(
                            title: hub.t("taskHarnessLabel"),
                            selection: $harness,
                            options: harnessOptions
                        )

                        Divider()

                        let effectiveHarness = harness.isEmpty ? (bot?.harness ?? "") : harness
                        HubFormField(label: hub.t("taskModelLabel")) {
                            if !effectiveHarness.isEmpty {
                                ModelFieldRow(
                                    title: hub.t("taskModelLabel"),
                                    harness: effectiveHarness,
                                    text: $model,
                                    placeholder: hub.t("taskModelPh"),
                                    onError: { err = $0 }
                                )
                            } else {
                                TextField(hub.t("taskModelPh"), text: $model)
                                    .monoInput()
                            }
                        }

                        Divider()

                        HubMenuPicker(
                            title: hub.t("permMode"),
                            selection: $permissionMode,
                            options: permissionOptions
                        )

                        Divider()

                        VStack(alignment: .leading, spacing: 6) {
                            Text(hub.t("taskDeliveryLabel"))
                                .font(.system(size: AppFont.caption, weight: .medium))
                                .foregroundStyle(p.mute)
                            HubSegmentedPicker(
                                selection: $delivery,
                                options: [
                                    ("receipt", hub.t("taskDeliveryReceipt")),
                                    ("wake", hub.t("taskDeliveryWake")),
                                    ("silent", hub.t("taskDeliverySilent")),
                                ]
                            )
                        }
                    }
                    .padding(16)
                    .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                }
                .padding(.horizontal, 16)
                .padding(.top, 8)
                .padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
        }
    }

    private func submit() {
        let trimmedPrompt = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedPrompt.isEmpty else {
            err = hub.t("taskPromptRequired")
            return
        }
        err = ""
        submitting = true
        Task {
            defer { submitting = false }
            do {
                let created = try await hub.spawnBotTask(
                    botId,
                    title: title,
                    prompt: trimmedPrompt,
                    kind: "worker",
                    deviceId: deviceId,
                    harness: harness,
                    model: model,
                    permissionMode: permissionMode,
                    workdir: workdir,
                    delivery: delivery
                )
                onCreated?(created)
                dismiss()
            } catch {
                err = errText(error, hub.t("taskSpawnFailed"))
            }
        }
    }
}
