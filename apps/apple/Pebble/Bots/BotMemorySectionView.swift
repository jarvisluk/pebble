import SwiftUI

/// Bot Detail native Pebble Memory section (safe down to 320pt narrow width).
struct BotMemorySectionView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var botId: String
    var accent: Color
    @Binding var error: String
    @Binding var confirm: ConfirmItem?

    @State private var mem: MemoryList?
    @State private var memLoading = false
    @State private var statusFilter = "active"
    @State private var notice = ""

    @State private var indexDoc: MemoryIndexDoc?
    @State private var indexDraft = ""
    @State private var indexTouched = false
    @State private var indexExpanded = false
    @State private var indexLoading = false
    @State private var indexSaving = false

    @State private var addExpanded = false
    @State private var addTitle = ""
    @State private var addKind = "fact"
    @State private var addText = ""
    @State private var addPin = false
    @State private var addDaily = false

    @State private var busy = ""

    private static let botStatuses = ["active", "candidate", "all"]

    var body: some View {
        let items = mem?.items ?? []
        Section {
            if !notice.isEmpty {
                StatusText(text: notice, error: false)
            }

            MemoryIndexEditorBlock(
                title: hub.t("memBotIndexTitle"),
                doc: indexDoc,
                draft: $indexDraft,
                touched: $indexTouched,
                expanded: $indexExpanded,
                loading: indexLoading,
                saving: indexSaving,
                defaultMaxLines: 150,
                onReload: { Task { await loadIndex(forceReset: true) } },
                onSave: { Task { await saveIndex() } }
            )

            VStack(alignment: .leading, spacing: 8) {
                MemoryStatusFilterBar(
                    selection: statusFilter,
                    statuses: Self.botStatuses,
                    candidateCount: mem?.counts?.candidate ?? 0
                ) { next in
                    statusFilter = next
                    Task { await loadMem(status: next) }
                }

                FlowLayout(spacing: 8) {
                    MemoryChipButton(
                        title: busy == "dream" ? hub.t("memDreamRunning") : hub.t("memDreamRun"),
                        systemImage: "sparkles",
                        disabled: !busy.isEmpty,
                        action: { runDream() }
                    )
                    MemoryChipButton(
                        title: hub.t("refresh"),
                        systemImage: "arrow.clockwise",
                        disabled: memLoading || !busy.isEmpty,
                        action: { Task { await reloadAll() } }
                    )
                }
            }

            if memLoading && mem == nil {
                ProgressView().controlSize(.small)
            } else if mem?.available == false {
                let detail = (mem?.error ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                Text(detail.isEmpty ? hub.t("memUnavailable") : "\(hub.t("memUnavailable")): \(detail)")
                    .foregroundStyle(p.danger)
            } else if items.isEmpty {
                Text(hub.t(statusFilter == "active" ? "noBotMemories" : "memNoItems"))
                    .foregroundStyle(.secondary)
            } else {
                ForEach(items) { it in
                    MemoryRow(
                        item: it,
                        showTags: true,
                        showScope: false,
                        busy: !busy.isEmpty,
                        onTogglePin: {
                            run("pin:\(it.id)", "opFailed") {
                                try await hub.patchMemory(it.id, ["pin": !it.isPinned])
                                await loadMem()
                                return nil
                            }
                        },
                        onApprove: {
                            run("approve:\(it.id)", "opFailed") {
                                try await hub.patchMemory(it.id, ["status": "active"])
                                await loadMem()
                                return hub.t("memApproved")
                            }
                        },
                        onDismiss: {
                            run("dismiss:\(it.id)", "opFailed") {
                                try await hub.deleteMemory(it.id, dismiss: true)
                                await loadMem()
                                return hub.t("memDismissed")
                            }
                        },
                        onArchive: {
                            run("archive:\(it.id)", "opFailed") {
                                try await hub.deleteMemory(it.id)
                                await loadMem()
                                return hub.t("memArchived")
                            }
                        },
                        onRestore: {
                            run("restore:\(it.id)", "opFailed") {
                                try await hub.patchMemory(it.id, ["status": "active"])
                                await loadMem()
                                return hub.t("memUpdated")
                            }
                        },
                        onDelete: { askDelete(it) }
                    )
                }
            }

            DisclosureGroup(hub.t("addShort"), isExpanded: $addExpanded) {
                VStack(alignment: .leading, spacing: 10) {
                    MemoryEditorFormFields(
                        title: $addTitle,
                        kind: $addKind,
                        text: $addText,
                        pin: $addPin,
                        daily: $addDaily,
                        contentPlaceholder: hub.t("botMemPh"),
                        toggleTint: accent
                    )
                    HStack {
                        Spacer()
                        if busy == "save" {
                            ProgressView().controlSize(.small)
                        } else {
                            Button(hub.t("saveMemory")) { addMem() }
                                .buttonStyle(HubPrimaryButtonStyle())
                                .disabled(addText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !busy.isEmpty)
                        }
                    }
                }
                .padding(.top, 8)
            }
        } header: {
            Text(hub.t("botMemory"))
        }
        .task(id: botId) {
            await reloadAll()
        }
    }

    private func reloadAll() async {
        async let idxTask: Void = loadIndex(forceReset: false)
        async let memTask: Void = loadMem()
        _ = await (idxTask, memTask)
    }

    private func loadIndex(forceReset: Bool) async {
        indexLoading = true
        defer { indexLoading = false }
        if let doc = try? await hub.fetchMemoryIndex(scope: "bot", botId: botId) {
            indexDoc = doc
            if forceReset || !indexTouched {
                indexDraft = doc.content ?? ""
                indexTouched = false
            }
        }
    }

    private func saveIndex() async {
        error = ""
        notice = ""
        indexSaving = true
        defer { indexSaving = false }
        do {
            var savedDoc = try await hub.saveMemoryIndex(scope: "bot", botId: botId, content: indexDraft)
            if savedDoc.content == nil {
                savedDoc.content = indexDraft
            }
            indexDoc = savedDoc
            indexDraft = savedDoc.content ?? indexDraft
            indexTouched = false
            notice = hub.t("memIndexSaved")
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
        }
    }

    private func loadMem(status: String? = nil) async {
        let st = status ?? statusFilter
        memLoading = true
        defer { memLoading = false }
        if let m = try? await hub.fetchMemories(scope: "bot", botId: botId, status: st, limit: 50) {
            mem = m
        }
    }

    private func addMem() {
        let txt = addText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !txt.isEmpty else {
            error = hub.t("memContentReq")
            return
        }
        var body: [String: Any] = [
            "scope": "bot",
            "botId": botId,
            "text": txt,
        ]
        if addDaily {
            body["daily"] = true
        } else {
            body["kind"] = addKind
            body["pin"] = addPin
            let title = addTitle.trimmingCharacters(in: .whitespacesAndNewlines)
            if !title.isEmpty { body["title"] = title }
        }
        let wasDaily = addDaily
        run("save", "saveBotMemFailed") {
            try await hub.saveMemory(body)
            addTitle = ""
            addText = ""
            addPin = false
            addDaily = false
            addExpanded = false
            await loadMem()
            return wasDaily ? hub.t("memDailySaved") : nil
        }
    }

    private func runDream() {
        run("dream", "opFailed") {
            _ = try await hub.triggerMemoryDream(botId: botId)
            await reloadAll()
            return hub.t("memDreamDone")
        }
    }

    private func askDelete(_ it: MemoryItem) {
        confirm = ConfirmItem(title: hub.t("deleteMemTitle"), message: hub.t("deleteMemConfirm")) {
            do {
                try await hub.deleteMemory(it.id, hard: true)
                await loadMem()
            } catch {
                self.error = errText(error, hub.t("delFailed"))
            }
        }
    }

    private func run(_ key: String, _ fail: String, _ op: @escaping () async throws -> String?) {
        error = ""
        notice = ""
        busy = key
        Task {
            do {
                if let msg = try await op() { notice = msg }
            } catch {
                self.error = errText(error, hub.t(fail))
            }
            busy = ""
        }
    }
}
