import SwiftUI

struct SettingsMemoryTab: View {
    @Environment(HubStore.self) var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) var p

    @Binding var error: String
    @Binding var saved: String
    @Binding var confirm: ConfirmItem?

    // _shared/USER.md
    @State private var userMd = ""
    @State private var userMdTouched = false
    @State private var userMdSaving = false

    // Global MEMORY.md index
    @State private var indexDoc: MemoryIndexDoc?
    @State private var indexDraft = ""
    @State private var indexTouched = false
    @State private var indexExpanded = false
    @State private var indexLoading = false
    @State private var indexSaving = false

    // Memory settings & status (shared with SettingsMemoryConfigSection.swift)
    @State var memStatus: MemoryStatusResp?
    @State var cfgCoexistence = "auto"
    @State var cfgDisableClaudeAuto = false
    @State var cfgDreamEnabled = true
    @State var cfgDreamDailyAt = "03:00"
    @State var cfgDreamModel = ""
    @State var cfgDreamMinMsgs = ""
    @State var cfgEmbedEnabled = false
    @State var cfgEmbedBaseUrl = ""
    @State var cfgEmbedApiKey = ""
    @State var cfgEmbedApiKeyTouched = false
    @State var cfgEmbedModel = ""
    @State var cfgTouched = false
    @State var cfgSaving = false
    @State var embedExpanded = false

    // Memory items browser
    @State var mem: MemoryList?
    @State private var memLoading = false
    @State private var memStatusFilter = "active"
    @State private var memScope = "global"
    @State private var memFilterKind = ""
    @State private var memQuery = ""
    @State private var initialLoaded = false

    // Add memory form
    @State private var memAdd = false
    @State private var addTitle = ""
    @State private var addKind = "fact"
    @State private var addText = ""
    @State private var addWhy = ""
    @State private var addTags = ""
    @State private var addPin = false

    // Inline edit form
    @State private var editingId: String? = nil
    @State private var editTitle = ""
    @State private var editKind = "fact"
    @State private var editText = ""
    @State private var editWhy = ""
    @State private var editTags = ""
    @State private var editPin = false

    @State var busy = ""

    private static let statusFilters = ["active", "candidate", "superseded", "archived", "all"]

    private var userMdDirty: Bool {
        guard let s = hub.settings else { return false }
        return userMdTouched && MemoryKindConstants.rtrim(userMd) != MemoryKindConstants.rtrim(s.sharedUserMd ?? "")
    }

    private var indexDirty: Bool {
        indexTouched && MemoryKindConstants.rtrim(indexDraft) != MemoryKindConstants.rtrim(indexDoc?.content ?? "")
    }

    private var anyDirty: Bool {
        userMdDirty || indexDirty || cfgTouched
    }

    var effectiveMagpie: MemoryMagpieStatus? {
        memStatus?.magpie ?? mem?.magpie
    }

    var effectiveSemantic: MemorySemanticStatus? {
        memStatus?.semantic ?? mem?.semantic
    }

    private var candidateCount: Int {
        mem?.counts?.candidate ?? memStatus?.counts?.candidate ?? 0
    }

    var body: some View {
        sharedUserSection
        globalIndexSection
        settingsMaintenanceSection
        memoryItemsSection
            .task(id: hub.isConnected) {
                guard hub.isConnected else { return }
                _ = try? await hub.loadSettings()
                syncUserMd()
                syncMemoryConfig()
                await reloadAll()
                initialLoaded = true
            }
            .task(id: memQuery) {
                guard initialLoaded, hub.isConnected else { return }
                try? await Task.sleep(nanoseconds: 280_000_000)
                if Task.isCancelled { return }
                await loadMem(memQuery)
            }
            .onAppear {
                syncUserMd()
                syncMemoryConfig()
                registerHandlers()
            }
            .onDisappear {
                router.settingsDirty = false
                router.settingsSaveAction = nil
                router.settingsDiscardAction = nil
            }
            .onChange(of: hub.settings?.sharedUserMd) { _, _ in
                syncUserMd()
            }
            .onChange(of: hub.settings?.memory) { _, _ in
                syncMemoryConfig()
            }
            .onChange(of: anyDirty) { _, dirty in
                router.settingsDirty = dirty
            }
    }

    // MARK: - Section 1: _shared/USER.md

    private var sharedUserSection: some View {
        Section {
            TextField(
                hub.t("globalSharedMem"),
                text: Binding(
                    get: { userMd },
                    set: {
                        guard $0 != userMd else { return }
                        userMd = $0
                        userMdTouched = true
                        saved = ""
                    }
                ),
                prompt: Text(hub.t("userMdPh")),
                axis: .vertical
            )
            .lineLimit(4...20)
            .labelsHidden()

            HStack {
                Text("_shared/USER.md")
                    .font(.system(size: AppFont.secondary, design: .monospaced))
                    .foregroundStyle(.secondary)
                Spacer()
                Button {
                    Task { _ = await saveSharedUserMd() }
                } label: {
                    HubLoadingLabel(loading: userMdSaving) { Text(hub.t("save")) }
                }
                .buttonStyle(HubPrimaryButtonStyle())
                .disabled(!userMdDirty || !busy.isEmpty || userMdSaving)
            }
        } header: {
            Text(hub.t("globalSharedMem"))
        }
    }

    // MARK: - Section 2: Global MEMORY.md

    private var globalIndexSection: some View {
        Section {
            MemoryIndexEditorBlock(
                title: hub.t("memIndexTitle"),
                doc: indexDoc,
                draft: $indexDraft,
                touched: $indexTouched,
                expanded: $indexExpanded,
                loading: indexLoading,
                saving: indexSaving,
                defaultMaxLines: 100,
                onReload: { Task { await loadGlobalIndex(forceReset: true) } },
                onSave: { Task { _ = await saveGlobalIndex() } }
            )
        }
    }

    // MARK: - Section 4: Memory Items

    private var memoryItemsSection: some View {
        let kindFilterOptions: [(String, String)] = [("", hub.t("memKindAll"))] + MemoryKindConstants.all.map { ($0, localizedMemoryKind($0, hub: hub)) }
        let items = mem?.items ?? []

        return Section {
            MemoryStatusFilterBar(
                selection: memStatusFilter,
                statuses: Self.statusFilters,
                candidateCount: candidateCount
            ) { next in
                memStatusFilter = next
                editingId = nil
                Task { await loadMem(memQuery, status: next) }
            }

            HubSegmentedPicker(
                selection: Binding(
                    get: { memScope },
                    set: { next in
                        memScope = next
                        editingId = nil
                        Task { await loadMem(memQuery, scope: next) }
                    }
                ),
                options: [
                    ("global", hub.t("memScopeGlobal")),
                    ("all", hub.t("memScopeAll")),
                ]
            )

            HubMenuPicker(
                title: hub.t("memType"),
                selection: Binding(
                    get: { memFilterKind },
                    set: { next in
                        memFilterKind = next
                        Task { await loadMem(memQuery, kind: next) }
                    }
                ),
                options: kindFilterOptions
            )

            HubSearchBar(
                text: $memQuery,
                placeholder: hub.t(effectiveSemantic?.active == true ? "searchSemanticMemPh" : "searchGlobalMemPh")
            )
            .onSubmit {
                Task { await loadMem(memQuery) }
            }

            if memLoading && mem == nil {
                ProgressView().controlSize(.small)
            } else if mem?.available == false {
                let detail = (mem?.error ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                Text(detail.isEmpty ? hub.t("memUnavailable") : "\(hub.t("memUnavailable")): \(detail)")
                    .foregroundStyle(p.danger)
            } else if items.isEmpty {
                Text(hub.t(memStatusFilter == "active" && memQuery.isEmpty && memFilterKind.isEmpty ? "noGlobalMem" : "memNoItems"))
                    .foregroundStyle(.secondary)
            } else {
                ForEach(items) { it in
                    if editingId == it.id {
                        inlineEditRow(it)
                    } else {
                        MemoryRow(
                            item: it,
                            showTags: true,
                            showScope: memScope == "all",
                            busy: !busy.isEmpty,
                            onTogglePin: {
                                run("mem:pin:\(it.id)", "opFailed") {
                                    try await hub.patchMemory(it.id, ["pin": !it.isPinned])
                                    await loadMem(memQuery)
                                    return nil
                                }
                            },
                            onApprove: {
                                run("mem:approve:\(it.id)", "opFailed") {
                                    try await hub.patchMemory(it.id, ["status": "active"])
                                    await loadMem(memQuery)
                                    return hub.t("memApproved")
                                }
                            },
                            onDismiss: {
                                run("mem:dismiss:\(it.id)", "opFailed") {
                                    try await hub.deleteMemory(it.id, dismiss: true)
                                    await loadMem(memQuery)
                                    return hub.t("memDismissed")
                                }
                            },
                            onEdit: { beginEdit(it) },
                            onArchive: {
                                run("mem:archive:\(it.id)", "opFailed") {
                                    try await hub.deleteMemory(it.id)
                                    await loadMem(memQuery)
                                    return hub.t("memArchived")
                                }
                            },
                            onRestore: {
                                run("mem:restore:\(it.id)", "opFailed") {
                                    try await hub.patchMemory(it.id, ["status": "active"])
                                    await loadMem(memQuery)
                                    return hub.t("memUpdated")
                                }
                            },
                            onDelete: {
                                confirm = ConfirmItem(title: hub.t("deleteMemTitle"), message: hub.t("deleteMemConfirm")) {
                                    do {
                                        try await hub.deleteMemory(it.id, hard: true)
                                        await loadMem(memQuery)
                                    } catch {
                                        self.error = errText(error, hub.t("delFailed"))
                                    }
                                }
                            }
                        )
                    }
                }
            }

            DisclosureGroup(hub.t("addShort"), isExpanded: $memAdd) {
                VStack(alignment: .leading, spacing: 10) {
                    MemoryEditorFormFields(
                        title: $addTitle,
                        kind: $addKind,
                        text: $addText,
                        tags: $addTags,
                        why: $addWhy,
                        pin: $addPin,
                        contentPlaceholder: hub.t("globalMemPh")
                    )
                    HStack {
                        Spacer()
                        Button(hub.t("saveMemory")) { saveNewMemory() }
                            .buttonStyle(HubPrimaryButtonStyle())
                            .disabled(busy == "mem:save")
                    }
                }
                .padding(.top, 8)
            }
        } header: {
            Text(hub.t("magpieGlobalMem"))
        }
    }

    private func inlineEditRow(_ item: MemoryItem) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(hub.t("memEditTitle"))
                .font(.system(size: AppFont.secondary, weight: .semibold))
                .foregroundStyle(p.text)
            MemoryEditorFormFields(
                title: $editTitle,
                kind: $editKind,
                text: $editText,
                tags: $editTags,
                why: $editWhy,
                pin: $editPin,
                contentPlaceholder: hub.t("globalMemPh")
            )
            HStack(spacing: 8) {
                Spacer()
                MemoryChipButton(title: hub.t("cancel"), disabled: busy == "mem:edit") {
                    editingId = nil
                }
                Button(hub.t("save")) { saveEditedMemory(item.id) }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(busy == "mem:edit")
            }
        }
    }

    // MARK: - Actions & Sync

    private func syncUserMd() {
        guard !userMdTouched, let s = hub.settings else { return }
        userMd = s.sharedUserMd ?? ""
    }

    private func registerHandlers() {
        router.settingsDirty = anyDirty
        router.settingsSaveAction = {
            var ok = true
            if userMdDirty { ok = await saveSharedUserMd() && ok }
            if indexDirty { ok = await saveGlobalIndex() && ok }
            if cfgTouched { ok = await saveMemorySettings() && ok }
            return ok
        }
        router.settingsDiscardAction = {
            userMdTouched = false
            userMd = hub.settings?.sharedUserMd ?? ""
            indexTouched = false
            indexDraft = indexDoc?.content ?? ""
            cfgTouched = false
            syncMemoryConfig()
        }
    }

    func reloadAll() async {
        async let statusTask: Void = loadStatus()
        async let indexTask: Void = loadGlobalIndex(forceReset: false)
        async let memTask: Void = loadMem(memQuery)
        _ = await (statusTask, indexTask, memTask)
    }

    func loadStatus() async {
        if let st = try? await hub.fetchMemoryStatus() {
            memStatus = st
            syncMemoryConfig()
        }
    }

    private func loadGlobalIndex(forceReset: Bool) async {
        indexLoading = true
        defer { indexLoading = false }
        if let doc = try? await hub.fetchMemoryIndex(scope: "global") {
            indexDoc = doc
            if forceReset || !indexTouched {
                indexDraft = doc.content ?? ""
                indexTouched = false
            }
        }
    }

    private func loadMem(_ q: String, scope: String? = nil, kind: String? = nil, status: String? = nil) async {
        let sc = scope ?? memScope
        let kd = kind ?? memFilterKind
        let st = status ?? memStatusFilter
        memLoading = true
        defer { memLoading = false }
        do {
            mem = try await hub.fetchMemories(scope: sc, query: q, kind: kd, status: st, limit: 50)
        } catch {
            self.error = errText(error, hub.t("opFailed"))
        }
    }

    private func saveSharedUserMd() async -> Bool {
        error = ""
        saved = ""
        userMdSaving = true
        defer { userMdSaving = false }
        do {
            try await hub.saveSharedUserMd(userMd)
            userMdTouched = false
            userMd = hub.settings?.sharedUserMd ?? ""
            saved = hub.t("settingsSaved")
            return true
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
            return false
        }
    }

    private func saveGlobalIndex() async -> Bool {
        error = ""
        saved = ""
        indexSaving = true
        defer { indexSaving = false }
        do {
            var savedDoc = try await hub.saveMemoryIndex(scope: "global", content: indexDraft)
            if savedDoc.content == nil {
                savedDoc.content = indexDraft
            }
            indexDoc = savedDoc
            indexDraft = savedDoc.content ?? indexDraft
            indexTouched = false
            saved = hub.t("memIndexSaved")
            return true
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
            return false
        }
    }

    private func saveNewMemory() {
        let txt = addText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !txt.isEmpty else {
            error = hub.t("memContentReq")
            return
        }
        var body: [String: Any] = [
            "scope": "global",
            "kind": addKind,
            "text": txt,
            "tags": MemoryKindConstants.parseTags(addTags),
            "pin": addPin,
        ]
        let title = addTitle.trimmingCharacters(in: .whitespacesAndNewlines)
        if !title.isEmpty { body["title"] = title }
        let why = addWhy.trimmingCharacters(in: .whitespacesAndNewlines)
        if !why.isEmpty { body["why"] = why }

        run("mem:save", "saveMemFailed") {
            try await hub.saveMemory(body)
            addTitle = ""
            addText = ""
            addWhy = ""
            addTags = ""
            addPin = false
            memAdd = false
            await loadMem(memQuery)
            await loadStatus()
            return hub.t("globalMemSaved")
        }
    }

    private func beginEdit(_ item: MemoryItem) {
        editingId = item.id
        editTitle = item.title ?? ""
        editKind = MemoryKindConstants.all.contains(item.kind ?? "") ? (item.kind ?? "fact") : "fact"
        editText = item.text ?? item.displayBody
        editWhy = item.why ?? ""
        editTags = item.visibleTags.joined(separator: ", ")
        editPin = item.isPinned
    }

    private func saveEditedMemory(_ id: String) {
        let txt = editText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !txt.isEmpty else {
            error = hub.t("memContentReq")
            return
        }
        let body: [String: Any] = [
            "title": editTitle.trimmingCharacters(in: .whitespacesAndNewlines),
            "kind": editKind,
            "text": txt,
            "why": editWhy.trimmingCharacters(in: .whitespacesAndNewlines),
            "tags": MemoryKindConstants.parseTags(editTags),
            "pin": editPin,
        ]
        run("mem:edit", "saveMemFailed") {
            try await hub.patchMemory(id, body)
            editingId = nil
            await loadMem(memQuery)
            return hub.t("memUpdated")
        }
    }

    func run(_ key: String, _ fail: String, _ op: @escaping () async throws -> String?) {
        error = ""
        saved = ""
        busy = key
        Task {
            do {
                if let msg = try await op() { saved = msg }
            } catch {
                self.error = errText(error, hub.t(fail))
            }
            busy = ""
        }
    }
}
