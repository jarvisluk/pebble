import SwiftUI

struct TaskInspectorSheet: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.dismiss) private var dismiss
    @Environment(\.palette) private var p

    var botId: String
    var taskId: String
    var onOpenDesktop: (String) -> Void

    @State private var fallbackTask: BotTask?
    @State private var followUp = ""
    @State private var sending = false
    @State private var cancelling = false
    @State private var resolvingTakeover = false
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
        let cardFill = p.dark ? Color(hex: "#1c1c1e") : p.card
        let t = task
        HubSheetScaffold(
            title: t?.displayTitle ?? taskId,
            closeTitle: hub.t("close"),
            onClose: { dismiss() }
        ) {
            VStack(spacing: 0) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 14) {
                        if !err.isEmpty {
                            StatusText(text: err)
                        }

                        if let t {
                            summaryCard(t, cardFill: cardFill)
                        }

                        if let apr = matchingApproval {
                            ApprovalCardView(approval: apr, pal: pal)
                        }

                        if let q = matchingQuestion {
                            QuestionCardView(question: q, pal: pal)
                        }

                        if let to = matchingTakeover {
                            takeoverActionCard(to, cardFill: cardFill)
                        }

                        outputCard(cardFill: cardFill)

                        timelineCard(cardFill: cardFill)
                    }
                    .padding(.horizontal, 16)
                    .padding(.top, 8)
                    .padding(.bottom, 20)
                }
                .scrollDismissesKeyboard(.interactively)

                if t?.status != "cancelled" {
                    followUpBar
                }
            }
        }
        .task(id: taskId) {
            await loadInspectorData()
        }
        .onChange(of: task?.status) { _, _ in
            Task { await refreshEventsIncremental() }
        }
        .onChange(of: hub.taskTools[taskId]) { _, _ in
            Task { await refreshEventsIncremental() }
        }
    }

    private func summaryCard(_ t: BotTask, cardFill: Color) -> some View {
        let vis = taskStatusVisual(t.status, palette: p)
        let deviceLabel = taskDeviceLabel(t.deviceId, nodes: hub.nodes)
        let activeTool = t.status == "running" ? (hub.taskTools[t.id] ?? "") : ""
        let dur = formatDurationMs(t.durationMs)

        return VStack(alignment: .leading, spacing: 10) {
            FlowLayout(spacing: 6) {
                Tag(text: taskStatusText(t.status, hub: hub), bg: vis.bg, fg: vis.fg)
                Tag(text: taskKindText(t.kind, hub: hub), bg: p.s1, fg: .secondary)
                if let deviceLabel {
                    Tag(text: deviceLabel, bg: p.lilac, fg: p.lilacInk)
                } else {
                    Tag(text: hub.t("taskDeviceLocal"), bg: p.s1, fg: .secondary)
                }
                if let h = t.harness, !h.isEmpty {
                    Tag(text: h, bg: p.s1, fg: .secondary)
                }
            }

            let details = [
                (t.model ?? "").isEmpty ? nil : t.model!,
                (t.permissionMode ?? "").isEmpty ? nil : t.permissionMode!,
                dur.isEmpty ? nil : dur,
                (t.costUsd ?? 0) > 0 ? String(format: "$%.4f", t.costUsd!) : nil,
            ].compactMap { $0 }

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

            if t.isActive {
                HStack {
                    Spacer()
                    Button(hub.t("taskCancelBtn"), role: .destructive) {
                        cancelCurrentTask()
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                    .disabled(cancelling)
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    private func takeoverActionCard(_ to: TakeoverRequest, cardFill: Color) -> some View {
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
                    dismiss()
                    onOpenDesktop(target)
                }
                .buttonStyle(.borderedProminent)
                .tint(pal.accent)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    private func outputCard(cardFill: Color) -> some View {
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
        .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    private func timelineCard(cardFill: Color) -> some View {
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
        .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
    }

    private var followUpBar: some View {
        HStack(spacing: 8) {
            TextField(hub.t("taskSendPlaceholder"), text: $followUp, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.body))
                .lineLimit(1...4)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
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
        .background(p.page)
        .overlay(alignment: .top) {
            Rectangle().fill(p.s2).frame(height: 0.5)
        }
    }

    private func loadInspectorData() async {
        if task == nil {
            if let fetched: BotTask = try? await hub.callAPI("/api/tasks/\(enc(taskId))") {
                fallbackTask = fetched
                hub.upsertBotTask(fetched)
            }
        }
        _ = try? await hub.fetchTaskEvents(taskId)
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
            let e = eventField(ev.payload, "error")
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: taskStatusText(st, hub: hub),
                detail: e,
                createdAt: ev.createdAt
            )
        case "tool_use":
            let tool = eventField(ev.payload, "tool") ?? ""
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: tool,
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
            let sum = eventField(ev.payload, "summary")
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: tool.isEmpty ? hub.t("approvalTitle") : tool,
                detail: sum,
                createdAt: ev.createdAt
            )
        case "question":
            let title = eventField(ev.payload, "title") ?? hub.t("questionTitle")
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: title,
                detail: nil,
                createdAt: ev.createdAt
            )
        case "takeover":
            let target = eventField(ev.payload, "target") ?? "local"
            return TimelineRow(
                id: "ev_\(ev.seq)",
                eventType: ev.eventType,
                summary: target,
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
