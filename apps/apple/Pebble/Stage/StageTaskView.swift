import SwiftUI

struct StageTaskView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    var botId: String
    var taskId: String
    var inWideStage: Bool = false

    @State private var fallbackTask: BotTask?
    @State private var followUp = ""
    @State private var sending = false
    @State private var cancelling = false
    @State private var resolvingTakeover = false
    @State private var diff: TaskDiffResult?
    @State private var diffLoading = false
    @State private var diffErr = ""
    @State private var rollingBack = false
    @State private var patchOpen = false
    @State private var confirm: ConfirmItem?
    @State private var err = ""

    private var botIndex: Int { hub.bots.firstIndex { $0.id == botId } ?? -1 }
    private var bot: BotStatus? { botIndex >= 0 ? hub.bots[botIndex] : nil }
    private var pal: (accent: Color, text: Color, soft: Color) {
        p.bot(botId, max(0, botIndex), custom: bot?.color)
    }

    private var task: BotTask? {
        hub.tasks[botId]?.first(where: { $0.id == taskId }) ?? fallbackTask
    }

    private var matchingApproval: PendingApproval? {
        hub.approvals.first { $0.taskId == taskId }
    }

    private var matchingQuestion: PendingQuestion? {
        hub.questions.first { $0.taskId == taskId }
    }

    private var matchingTakeover: TakeoverRequest? {
        if let exact = hub.takeovers.first(where: { $0.taskId == taskId }) {
            return exact
        }
        if task?.status == "waiting_takeover" {
            return hub.takeovers.first(where: { $0.botId == botId })
        }
        return nil
    }

    private var hasWorktreeInfo: Bool {
        guard let t = task else { return false }
        return t.isolation == "worktree" || !(t.checkpointRef ?? "").isEmpty || !(t.worktreePath ?? "").isEmpty
    }

    private var liveOutputText: String {
        let streamed = hub.taskStreamText[taskId] ?? ""
        let persisted = task?.output ?? ""
        if let t = task, t.isActive {
            if !streamed.isEmpty { return streamed }
            return persisted
        }
        return !persisted.isEmpty ? persisted : streamed
    }

    private struct TimelineRow: Identifiable {
        let id: String
        let eventType: String
        let summary: String
        let detail: String?
        let createdAt: String?
    }

    private var timelineRows: [TimelineRow] {
        let events = hub.taskEvents[taskId] ?? []
        var rows: [TimelineRow] = []
        var chunkBuffer = ""
        var chunkStartSeq: Int64 = 0
        var chunkTime: String?

        func flushChunks() {
            let trimmed = chunkBuffer.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty {
                rows.append(TimelineRow(
                    id: "chunk_\(chunkStartSeq)",
                    eventType: "chunk",
                    summary: trimmed,
                    detail: nil,
                    createdAt: chunkTime
                ))
            }
            chunkBuffer = ""
            chunkStartSeq = 0
            chunkTime = nil
        }

        for ev in events {
            if ev.eventType == "chunk" {
                let piece = eventField(ev.payload, "chunk") ?? ""
                if chunkStartSeq == 0 {
                    chunkStartSeq = ev.seq
                    chunkTime = ev.createdAt
                }
                chunkBuffer += piece
                continue
            }
            flushChunks()
            rows.append(formatEventRow(ev))
        }
        flushChunks()
        return rows
    }

    var body: some View {
        let t = task
        let sectionFill = inWideStage ? p.s1 : p.card
        let cardContent = VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    HStack(alignment: .center, spacing: 10) {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(t?.displayTitle ?? taskId)
                                .font(.system(size: AppFont.title, weight: .semibold))
                                .foregroundStyle(p.text)
                                .lineLimit(2)
                            if let t {
                                let dev = taskDeviceLabel(t.deviceId, nodes: hub.nodes) ?? hub.t("taskDeviceLocal")
                                let meta = [taskKindText(t.kind, hub: hub), dev, t.harness]
                                    .compactMap { $0 }
                                    .filter { !$0.isEmpty }
                                    .joined(separator: " · ")
                                if !meta.isEmpty {
                                    Text(meta)
                                        .font(.system(size: AppFont.caption))
                                        .foregroundStyle(.secondary)
                                        .lineLimit(1)
                                }
                            }
                        }

                        Spacer(minLength: 8)

                        if let t {
                            let vis = taskStatusVisual(t.status, palette: p)
                            Tag(text: taskStatusText(t.status, hub: hub), bg: vis.bg, fg: vis.fg)
                            if t.isActive {
                                Button(hub.t("taskCancelBtn"), role: .destructive) {
                                    cancelCurrentTask()
                                }
                                .buttonStyle(.bordered)
                                .controlSize(.small)
                                .disabled(cancelling)
                            }
                        }
                    }
                    .padding(.bottom, 2)

                    if !err.isEmpty {
                        StatusText(text: err)
                    }

                    if let t {
                        summaryCard(t, fill: sectionFill)
                    }

                    if let apr = matchingApproval {
                        ApprovalCardView(approval: apr, pal: pal)
                    }

                    if let q = matchingQuestion {
                        QuestionCardView(question: q, pal: pal)
                    }

                    if let to = matchingTakeover {
                        takeoverActionCard(to, fill: sectionFill)
                    }

                    outputCard(fill: sectionFill)

                    if hasWorktreeInfo, let t {
                        diffCard(t, fill: sectionFill)
                    }

                    timelineCard(fill: sectionFill)
                }
                .frame(maxWidth: 760, alignment: .leading)
                .padding(16)
                .frame(maxWidth: .infinity)
            }
            .scrollDismissesKeyboard(.interactively)

            if t?.status != "cancelled" {
                followUpBar
            }
        }

        Group {
            if inWideStage {
                cardContent
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                    .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                    .padding(12)
            } else {
                cardContent
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(p.page)
        .confirmAlert($confirm)
        .task(id: taskId) {
            await loadInspectorData()
        }
        .onChange(of: task?.status) { _, _ in
            Task {
                await refreshEventsIncremental()
                if hasWorktreeInfo && (task?.isTerminal ?? false) {
                    await loadDiff()
                }
            }
        }
        .onChange(of: hub.taskTools[taskId]) { _, _ in
            Task { await refreshEventsIncremental() }
        }
    }

    private func summaryCard(_ t: BotTask, fill: Color) -> some View {
        let activeTool = t.status == "running" ? (hub.taskTools[t.id] ?? "") : ""
        let dur = formatDurationMs(t.durationMs)
        let details = [
            (t.model ?? "").isEmpty ? nil : t.model!,
            (t.permissionMode ?? "").isEmpty ? nil : t.permissionMode!,
            (t.isolation ?? "").isEmpty ? nil : t.isolation!,
            (diff?.branch ?? t.branch ?? "").isEmpty ? nil : (diff?.branch ?? t.branch!),
            dur.isEmpty ? nil : dur,
            (t.costUsd ?? 0) > 0 ? String(format: "$%.4f", t.costUsd!) : nil,
        ].compactMap { $0 }

        return VStack(alignment: .leading, spacing: 8) {
            if !details.isEmpty {
                Text(details.joined(separator: " · "))
                    .font(.system(size: AppFont.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
            }

            if let wd = t.workdir, !wd.isEmpty {
                Text(wd)
                    .font(.system(size: AppFont.caption, design: .monospaced))
                    .foregroundStyle(p.mute)
                    .lineLimit(2)
                    .textSelection(.enabled)
            }

            if !activeTool.isEmpty {
                Text(hub.t("callingTool", ["tool": activeTool]))
                    .font(.system(size: AppFont.secondary, design: .monospaced))
                    .foregroundStyle(p.busy)
            }

            if !t.prompt.isEmpty {
                Text(t.prompt)
                    .font(.system(size: AppFont.secondary))
                    .foregroundStyle(p.text2)
                    .textSelection(.enabled)
            }

            if let e = t.error, !e.isEmpty {
                StatusText(text: e)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(fill, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func takeoverActionCard(_ to: TakeoverRequest, fill: Color) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text(hub.t("taskWaitingTakeoverTitle"))
                    .font(.system(size: AppFont.cardTitle, weight: .bold))
                    .foregroundStyle(p.text)
                Spacer()
                Tag(text: hub.t("takeoverWaitingBadge"), bg: p.pink, fg: p.pinkInk)
            }

            if let reason = to.reason, !reason.isEmpty {
                Text(reason)
                    .font(.system(size: AppFont.body))
                    .foregroundStyle(p.text2)
            }

            HStack(spacing: 8) {
                Spacer()
                Button(hub.t("resumeShort")) {
                    guard !resolvingTakeover else { return }
                    resolvingTakeover = true
                    Task {
                        defer { resolvingTakeover = false }
                        do {
                            try await hub.resolveTakeover(to.id)
                        } catch {
                            err = errText(error, hub.t("opFailed"))
                        }
                    }
                }
                .buttonStyle(.bordered)
                .disabled(resolvingTakeover)

                Button(hub.t("taskOpenTakeover")) {
                    let target = (to.target ?? "").isEmpty ? "local" : to.target!
                    router.openDesktop(target, botId: botId)
                }
                .buttonStyle(.borderedProminent)
                .tint(pal.accent)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(fill, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func outputCard(fill: Color) -> some View {
        let out = liveOutputText
        return VStack(alignment: .leading, spacing: 8) {
            Text(hub.t("taskOutputTitle"))
                .font(.system(size: AppFont.secondary, weight: .semibold))
                .foregroundStyle(p.mute)

            if out.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Text(hub.t("taskOutputEmpty"))
                    .font(.system(size: AppFont.secondary))
                    .foregroundStyle(.secondary)
            } else {
                MarkdownText(text: out)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(fill, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func diffCard(_ t: BotTask, fill: Color) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Text(hub.t("taskDiffTitle"))
                    .font(.system(size: AppFont.secondary, weight: .semibold))
                    .foregroundStyle(p.mute)
                Spacer()
                Button(hub.t("taskDiffRefresh")) {
                    Task { await loadDiff() }
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .disabled(diffLoading)

                if t.isTerminal, !(diff?.checkpointRef ?? t.checkpointRef ?? "").isEmpty {
                    Button(hub.t("taskRollbackBtn"), role: .destructive) {
                        confirm = ConfirmItem(
                            title: hub.t("taskRollbackConfirmTitle", ["title": t.displayTitle]),
                            message: hub.t("taskRollbackConfirmDesc")
                        ) {
                            await performRollback()
                        }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .disabled(rollingBack)
                }
            }

            if !diffErr.isEmpty {
                StatusText(text: diffErr)
            } else if diffLoading && diff == nil {
                ProgressView().controlSize(.small)
            } else if let diff, !diff.files.isEmpty || !diff.patch.isEmpty {
                ForEach(diff.files) { file in
                    HStack(spacing: 8) {
                        Text(file.status ?? "M")
                            .font(.system(size: AppFont.caption, weight: .bold, design: .monospaced))
                            .foregroundStyle(p.text2)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(inWideStage ? p.card : p.s1, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
                        Text(file.path)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(p.text)
                            .lineLimit(1)
                            .truncationMode(.middle)
                        Spacer()
                        let adds = file.additions ?? 0
                        let dels = file.deletions ?? 0
                        if adds > 0 || dels > 0 {
                            Text("+\(adds) -\(dels)")
                                .font(.system(size: AppFont.caption, design: .monospaced))
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                if !diff.patch.isEmpty {
                    Button(hub.t(patchOpen ? "taskDiffHidePatch" : "taskDiffShowPatch")) {
                        patchOpen.toggle()
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)

                    if patchOpen {
                        ScrollView(.horizontal, showsIndicators: true) {
                            Text(diff.patch)
                                .font(.system(size: AppFont.caption, design: .monospaced))
                                .foregroundStyle(p.text)
                                .textSelection(.enabled)
                                .padding(10)
                        }
                        .background(inWideStage ? p.card : p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    }
                }
            } else {
                Text(hub.t("taskDiffEmpty"))
                    .font(.system(size: AppFont.secondary))
                    .foregroundStyle(.secondary)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(fill, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func timelineCard(fill: Color) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(hub.t("taskTimelineTitle"))
                .font(.system(size: AppFont.secondary, weight: .semibold))
                .foregroundStyle(p.mute)

            if timelineRows.isEmpty {
                Text(hub.t("taskTimelineEmpty"))
                    .font(.system(size: AppFont.secondary))
                    .foregroundStyle(.secondary)
            } else {
                ForEach(Array(timelineRows.enumerated()), id: \.element.id) { idx, row in
                    if idx > 0 {
                        Rectangle()
                            .fill(Color.primary.opacity(p.dark ? 0.10 : 0.08))
                            .frame(height: 0.5)
                    }
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(spacing: 6) {
                            Text(eventTypeLabel(row.eventType))
                                .font(.system(size: AppFont.caption, weight: .semibold))
                                .foregroundStyle(row.eventType == "error" ? p.danger : p.text2)
                            Spacer()
                            let clk = formatClockTime(row.createdAt)
                            if !clk.isEmpty {
                                Text(clk)
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(p.mute)
                            }
                        }
                        Text(row.summary)
                            .font(.system(size: AppFont.secondary, design: row.eventType == "tool_use" ? .monospaced : .default))
                            .foregroundStyle(row.eventType == "error" ? p.danger : p.text)
                            .lineLimit(row.eventType == "chunk" ? 4 : 6)
                            .textSelection(.enabled)
                        if let detail = row.detail, !detail.isEmpty {
                            Text(detail)
                                .font(.system(size: AppFont.caption))
                                .foregroundStyle(.secondary)
                                .lineLimit(3)
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(fill, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private var followUpBar: some View {
        HStack(spacing: 8) {
            TextField(hub.t("taskSendPlaceholder"), text: $followUp, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.body))
                .lineLimit(1...4)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(inWideStage ? p.s1 : p.card, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
                .hubSubmitOnReturn(disabled: sending || followUp.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty) {
                    sendFollowUp()
                }

            Button {
                sendFollowUp()
            } label: {
                if sending {
                    ProgressView().controlSize(.small)
                } else {
                    Text(hub.t("taskSendBtn"))
                }
            }
            .buttonStyle(HubPrimaryButtonStyle())
            .disabled(sending || followUp.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .overlay(alignment: .top) {
            Rectangle().fill(p.s2).frame(height: 0.5)
        }
    }

    private func loadInspectorData() async {
        fallbackTask = nil
        diff = nil
        diffErr = ""
        patchOpen = false
        err = ""
        if let fetched: BotTask = try? await hub.callAPI("/api/tasks/\(enc(taskId))") {
            fallbackTask = fetched
            hub.upsertBotTask(fetched)
        }
        _ = try? await hub.fetchTaskEvents(taskId)
        if hasWorktreeInfo {
            await loadDiff()
        }
    }

    private func loadDiff() async {
        diffLoading = true
        defer { diffLoading = false }
        do {
            diff = try await hub.fetchTaskDiff(taskId)
            diffErr = ""
        } catch {
            diffErr = errText(error, hub.t("opFailed"))
        }
    }

    private func performRollback() async {
        rollingBack = true
        defer { rollingBack = false }
        do {
            _ = try await hub.rollbackTask(taskId, removeWorktree: false)
            diffErr = ""
            await loadDiff()
        } catch {
            diffErr = errText(error, hub.t("opFailed"))
        }
    }

    private func refreshEventsIncremental() async {
        let lastSeq = hub.taskEvents[taskId]?.last?.seq ?? 0
        _ = try? await hub.fetchTaskEvents(taskId, afterSeq: lastSeq)
    }

    private func sendFollowUp() {
        let msg = followUp.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !msg.isEmpty, !sending else { return }
        followUp = ""
        err = ""
        sending = true
        Task {
            defer { sending = false }
            do {
                let updated = try await hub.sendToTask(taskId, message: msg)
                fallbackTask = updated
                await refreshEventsIncremental()
            } catch {
                followUp = msg
                err = errText(error, hub.t("taskSendFailed"))
            }
        }
    }

    private func cancelCurrentTask() {
        guard !cancelling else { return }
        cancelling = true
        err = ""
        Task {
            defer { cancelling = false }
            do {
                let updated = try await hub.cancelTask(taskId)
                fallbackTask = updated
                await refreshEventsIncremental()
            } catch {
                err = errText(error, hub.t("taskCancelFailed"))
            }
        }
    }

    private func eventTypeLabel(_ type: String) -> String {
        let key = "taskEvent_\(type)"
        let val = hub.t(key)
        return val == key ? type : val
    }

    private func eventField(_ payload: JSONValue?, _ key: String) -> String? {
        guard case let .object(dict) = payload else { return nil }
        return dict[key]?.stringValue
    }

    private func formatEventRow(_ ev: BotTaskEvent) -> TimelineRow {
        switch ev.eventType {
        case "status":
            let st = eventField(ev.payload, "status") ?? ""
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: taskStatusText(st, hub: hub),
                detail: eventField(ev.payload, "error"),
                createdAt: ev.createdAt
            )
        case "tool_use":
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: eventField(ev.payload, "tool") ?? "",
                detail: nil,
                createdAt: ev.createdAt
            )
        case "message":
            let role = eventField(ev.payload, "role") ?? ""
            let text = eventField(ev.payload, "text") ?? ""
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: role.isEmpty ? text : "[\(role)] \(text)",
                detail: nil,
                createdAt: ev.createdAt
            )
        case "approval":
            let tool = eventField(ev.payload, "toolName") ?? eventField(ev.payload, "title") ?? ""
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: tool.isEmpty ? hub.t("approvalTitle") : tool,
                detail: eventField(ev.payload, "summary"),
                createdAt: ev.createdAt
            )
        case "question":
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: eventField(ev.payload, "title") ?? hub.t("questionTitle"),
                detail: nil,
                createdAt: ev.createdAt
            )
        case "takeover":
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: eventField(ev.payload, "target") ?? "local",
                detail: eventField(ev.payload, "state"),
                createdAt: ev.createdAt
            )
        case "error":
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: eventField(ev.payload, "error") ?? hub.t("opFailed"),
                detail: nil,
                createdAt: ev.createdAt
            )
        default:
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: ev.eventType,
                detail: nil,
                createdAt: ev.createdAt
            )
        }
    }
}
