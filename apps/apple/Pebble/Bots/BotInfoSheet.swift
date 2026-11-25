import SwiftUI

/// 顶栏中间 Bot 名称点开的信息与编辑表单 Sheet。
struct BotInfoSheet: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    var botId: String

    @State private var detail: BotStatus?
    @State private var loading = false
    @State private var techOpen = false
    @State private var editName = ""
    @State private var editDesc = ""
    @State private var editAvatar = ""
    @State private var editColor = ""
    @State private var appearOpen = false
    @State private var editHarness = ""
    @State private var editModel = ""
    @State private var editInst = ""
    @State private var editPrivate = false
    @State private var editDesktopTarget = ""
    @State private var editBrowserMode = ""
    @State private var editPermMode = "auto"
    @State private var saving = false
    @State private var err = ""
    @State private var confirm: ConfirmItem?

    @State private var mem: MemoryList?
    @State private var memLoading = false
    @State private var memText = ""
    @State private var memSaving = false

    private var index: Int { hub.bots.firstIndex { $0.id == botId } ?? -1 }
    private var bot: BotStatus? { index >= 0 ? hub.bots[index] : nil }
    private var active: BotStatus? { detail ?? bot }

    private var canBindDesktop: Bool { hub.supports("bot.desktop_binding") }
    private var canIsolateBrowser: Bool { hub.supports("bot.browser_isolation") }

    private var harnessOptions: [(String, String)] {
        var opts = settingsHarnessList(from: hub).map { ($0.id, $0.title) }
        if !editHarness.isEmpty && !opts.contains(where: { $0.0 == editHarness }) {
            opts.insert((editHarness, editHarness), at: 0)
        }
        return opts
    }

    private var effectiveModelPlaceholder: String {
        let per = (hub.settings?.defaultModels?[editHarness] ?? "").trimmingCharacters(in: .whitespaces)
        let ph = per.isEmpty ? resolveDefaultModel((hub.settings?.defaultModel ?? "").trimmingCharacters(in: .whitespaces), editHarness) : per
        return ph.isEmpty ? hub.t("inheritDefaultPh") : ph
    }

    private var dirty: Bool {
        guard let a = active else { return false }
        let activePerm = hub.clampPermissionMode(a.permissionMode, harnessId: a.harness)
        let baseModel = (a.modelInherited == true ? "" : (a.model ?? "")).trimmingCharacters(in: .whitespaces)
        return editName != (a.name ?? botId) || editDesc != (a.description ?? "")
            || editAvatar != (a.avatar ?? "") || editColor != (a.color ?? "")
            || editHarness != (a.harness ?? "")
            || editModel.trimmingCharacters(in: .whitespaces) != baseModel
            || editPrivate != (a.privateSessions ?? false)
            || (canBindDesktop && editDesktopTarget != (a.desktopTarget ?? ""))
            || (canIsolateBrowser && editBrowserMode != (a.browserMode ?? ""))
            || editPermMode != activePerm
            || editInst != (detail?.instructions ?? "")
    }

    private var desktopTargetOptions: [(String, String)] {
        var opts: [(String, String)] = [
            ("", hub.t("deskBindDefault")),
            ("local", hub.t("localDesktopName")),
        ]
        var seen: Set<String> = ["", "local"]
        if hub.desktopTargets.first(where: { $0.id == "local" })?.virtualSupported == true {
            let vid = "virtual:\(botId)"
            seen.insert(vid)
            opts.append((vid, hub.t("deskBindLocalVirtual")))
        }
        for tr in hub.desktopTargets where tr.id != "local" {
            if !seen.contains(tr.id) {
                seen.insert(tr.id)
                opts.append((tr.id, tr.name ?? tr.id))
            }
            if tr.virtual != true, tr.virtualSupported == true {
                let rvid = "\(tr.id):virtual:\(botId)"
                if !seen.contains(rvid) {
                    seen.insert(rvid)
                    opts.append((rvid, hub.t("deskBindRemoteVirtual", ["name": tr.name ?? tr.id])))
                }
            }
        }
        if !editDesktopTarget.isEmpty, !seen.contains(editDesktopTarget) {
            opts.append((editDesktopTarget, hub.t("deskBindOfflineTag", ["id": editDesktopTarget])))
        }
        return opts
    }

    private var browserModeOptions: [(String, String)] {
        [
            ("", hub.t("browserModeAuto")),
            ("isolated", hub.t("browserModeIsolated")),
            ("attach", hub.t("browserModeAttach")),
        ]
    }

    var body: some View {
        let pal = p.bot(botId, index, custom: editColor.isEmpty ? active?.color : editColor)
        let formContent = Form {
            Group {
                if !err.isEmpty { Section { StatusText(text: err) } }
                Section {
                    HStack(spacing: 14) {
                        BotAvatar(id: botId, size: 56, index: index, avatar: editAvatar, color: editColor)
                        VStack(alignment: .leading, spacing: 4) {
                            TextField("", text: $editName, prompt: Text(hub.t("botNamePh")))
                                .labelsHidden()
                                .font(.system(size: AppFont.body, weight: .semibold))
                            Text("@\(botId)").font(.system(size: AppFont.caption, design: .monospaced)).foregroundStyle(.secondary)
                        }
                    }
                    TextField("", text: $editDesc, prompt: Text(hub.t("descPh")), axis: .vertical)
                        .labelsHidden()
                    DisclosureGroup(hub.t("appearance"), isExpanded: $appearOpen) {
                        BotAppearancePicker(botId: botId, index: index, avatar: $editAvatar, color: $editColor, accent: pal.accent)
                    }
                    LabeledContent(hub.t("statTurns"), value: "\(active?.session?.turnCount ?? 0)")
                    LabeledContent(hub.t("statCost"), value: String(format: "$%.4f", active?.session?.totalCostUsd ?? 0))
                }

                memorySection(pal)

                Section {
                    DisclosureGroup(hub.t("techDetails"), isExpanded: $techOpen) {
                        HubMenuPicker(
                            title: hub.t("statHarness"),
                            selection: Binding(
                                get: { editHarness },
                                set: { next in
                                    guard next != editHarness else { return }
                                    editHarness = next
                                    editModel = ""
                                    Task { _ = try? await hub.loadModels(next) }
                                }
                            ),
                            options: harnessOptions
                        )
                        HubFormField(label: hub.t("statModel")) {
                            ModelFieldRow(
                                title: hub.t("statModel"),
                                harness: editHarness,
                                text: $editModel,
                                placeholder: effectiveModelPlaceholder,
                                onError: { err = $0 }
                            )
                        }
                        LabeledContent(hub.t("statArchived"), value: hub.t("archivedCount", ["n": active?.archivedSessions ?? 0])
                            + ((active?.privateSessions ?? false) ? hub.t("privateTag") : ""))
                        LabeledContent(hub.t("workspaceDir")) {
                            Button {
                                router.sheet = nil
                                router.openFiles(botId: botId)
                            } label: {
                                HStack(spacing: 4) {
                                    Text(active?.workspace ?? "-")
                                        .font(.system(size: AppFont.secondary, design: .monospaced))
                                        .foregroundStyle(p.text2)
                                        .lineLimit(1)
                                        .truncationMode(.middle)
                                    Image(systemName: "folder")
                                        .font(.system(size: AppFont.caption))
                                        .foregroundStyle(p.text2)
                                }
                            }
                            .buttonStyle(.plain)
                        }
                        if canBindDesktop {
                            HubMenuPicker(title: hub.t("botDesktopTarget"), selection: $editDesktopTarget, options: desktopTargetOptions)
                        }
                        if canIsolateBrowser {
                            HubMenuPicker(title: hub.t("botBrowserMode"), selection: $editBrowserMode, options: browserModeOptions)
                        }
                        let permModes = hub.supportedPermissionModes(for: active?.harness)
                        if permModes.count <= 1 {
                            let onlyMode = permModes.first ?? "auto"
                            VStack(alignment: .leading, spacing: 3) {
                                LabeledContent(hub.t("permMode")) {
                                    Text(hub.t("permMode_\(onlyMode)")).foregroundStyle(p.text2)
                                }
                                Text(hub.t("permModeUnsupported", ["mode": hub.t("permMode_\(onlyMode)")]))
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(p.mute)
                            }
                        } else {
                            VStack(alignment: .leading, spacing: 3) {
                                HubMenuPicker(
                                    title: hub.t("permMode"),
                                    selection: $editPermMode,
                                    options: permModes.map { ($0, hub.t("permMode_\($0)")) }
                                )
                                Text(hub.t("permDesc_\(editPermMode)"))
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(p.mute)
                            }
                        }
                        Toggle(hub.t("privateSessions"), isOn: $editPrivate).tint(pal.accent)
                        VStack(alignment: .leading, spacing: 6) {
                            Text("AGENTS.md").font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
                            if loading && detail == nil {
                                ProgressView()
                            } else {
                                TextField(hub.t("agentsPh"), text: $editInst, axis: .vertical)
                                    .lineLimit(6...24)
                                    .font(.system(size: AppFont.secondary, design: .monospaced))
                            }
                        }
                    }
                }

                Section {
                    Button(hub.t("resetSessionBtn")) {
                        confirm = ConfirmItem(title: hub.t("resetSessionTitle"), message: hub.t("resetSessionDesc", ["name": bot?.displayName ?? botId])) {
                            do {
                                try await hub.resetBot(botId)
                                router.sheet = nil
                                try? await hub.loadMessages(botId)
                            } catch { err = errText(error, hub.t("resetFailed")) }
                        }
                    }
                    if hub.bots.count > 1 {
                        Button(hub.t("deleteBotBtn"), role: .destructive) {
                            confirm = ConfirmItem(title: hub.t("deleteBotTitle", ["name": bot?.displayName ?? botId]), message: hub.t("deleteBotDesc")) {
                                do {
                                    try await hub.deleteBot(botId)
                                    router.sheet = nil
                                    router.closeBot()
                                } catch { err = errText(error, hub.t("delFailed")) }
                            }
                        }
                    }
                }
            }
        }
        .hubForm()

        HubSheetScaffold(
            title: hub.t("botDetails"),
            onClose: { router.sheet = nil },
            actionTitle: hub.t("save"),
            actionDisabled: !dirty || editName.trimmingCharacters(in: .whitespaces).isEmpty,
            loading: saving,
            onAction: { save() }
        ) {
            formContent
        }
        .hubClearInitialFocus()
        .confirmAlert($confirm)
        .task(id: botId) {
            await open()
        }
    }

    private func open() async {
        let src = detail ?? bot
        editName = src?.name ?? botId
        editDesc = src?.description ?? ""
        editAvatar = src?.avatar ?? ""
        editColor = src?.color ?? ""
        editHarness = src?.harness ?? ""
        editModel = (src?.modelInherited == true ? "" : (src?.model ?? "")).trimmingCharacters(in: .whitespaces)
        editInst = detail?.instructions ?? ""
        editPrivate = src?.privateSessions ?? false
        editDesktopTarget = src?.desktopTarget ?? ""
        editBrowserMode = src?.browserMode ?? ""
        editPermMode = hub.clampPermissionMode(src?.permissionMode, harnessId: src?.harness)
        loading = true
        Task { await loadMem() }
        Task { _ = try? await hub.refreshDesktopTargets() }
        Task {
            if hub.settings == nil { _ = try? await hub.loadSettings() }
            if !editHarness.isEmpty { _ = try? await hub.loadModels(editHarness) }
        }
        if let d = try? await hub.botDetail(botId) { apply(d) }
        loading = false
    }

    private func apply(_ d: BotStatus) {
        detail = d
        editName = d.name ?? botId
        editDesc = d.description ?? ""
        editAvatar = d.avatar ?? ""
        editColor = d.color ?? ""
        editHarness = d.harness ?? ""
        editModel = (d.modelInherited == true ? "" : (d.model ?? "")).trimmingCharacters(in: .whitespaces)
        editInst = d.instructions ?? ""
        editPrivate = d.privateSessions ?? false
        editDesktopTarget = d.desktopTarget ?? ""
        editBrowserMode = d.browserMode ?? ""
        editPermMode = hub.clampPermissionMode(d.permissionMode, harnessId: d.harness)
        if !editHarness.isEmpty {
            Task { _ = try? await hub.loadModels(editHarness) }
        }
    }

    private func save() {
        let n = editName.trimmingCharacters(in: .whitespaces)
        guard !n.isEmpty else { err = hub.t("nameRequired"); return }
        err = ""
        saving = true
        Task {
            do {
                var body: [String: Any] = [
                    "name": n,
                    "description": editDesc.trimmingCharacters(in: .whitespaces),
                    "avatar": editAvatar,
                    "color": editColor,
                    "harness": editHarness,
                    "model": editModel.trimmingCharacters(in: .whitespaces),
                    "instructions": editInst,
                    "privateSessions": editPrivate,
                    "permissionMode": editPermMode,
                ]
                if canBindDesktop { body["desktopTarget"] = editDesktopTarget }
                if canIsolateBrowser { body["browserMode"] = editBrowserMode }
                try await hub.updateBot(botId, body)
                apply(try await hub.botDetail(botId))
            } catch { err = errText(error, hub.t("saveFailed")) }
            saving = false
        }
    }

    private func loadMem() async {
        memLoading = true
        if let m = try? await hub.fetchMemories(scope: "project", botId: botId, limit: 30) { mem = m }
        memLoading = false
    }

    private func memorySection(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        Section(hub.t("botMemory")) {
            if memLoading && mem == nil {
                ProgressView()
            } else if mem?.installed != true {
                Text(hub.t("hostNoMagpie")).foregroundStyle(.secondary)
                Button {
                    Task {
                        do {
                            try await hub.installMagpie()
                            await loadMem()
                        } catch {
                            err = errText(error, hub.t("opFailed"))
                        }
                    }
                } label: {
                    Text(hub.t("mpInstallOnHost"))
                }
            } else if mem?.available != true {
                Text(hub.t("magpieNoResp")).foregroundStyle(p.danger)
            } else {
                if (mem?.items ?? []).isEmpty {
                    Text(hub.t("noBotMemories")).foregroundStyle(.secondary)
                }
                ForEach(mem?.items ?? []) { it in
                    MemoryRow(item: it) { askDelete(it) }
                }
                HStack(alignment: .center, spacing: 8) {
                    TextField("", text: $memText, prompt: Text(hub.t("botMemPh")), axis: .vertical)
                        .labelsHidden()
                        .lineLimit(1...5)
                    let memReady = !memText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                    if memSaving {
                        ProgressView().controlSize(.small)
                    } else if memReady {
                        Button(hub.t("saveMemory")) { addMem() }
                            .buttonStyle(HubPrimaryButtonStyle())
                    } else {
                        Button(hub.t("saveMemory")) { addMem() }
                            .buttonStyle(.bordered).controlSize(.small)
                            .disabled(true)
                    }
                }
            }
        }
    }

    private func askDelete(_ it: MemoryItem) {
        confirm = ConfirmItem(title: hub.t("deleteMemTitle"), message: hub.t("deleteMemConfirm")) {
            try? await hub.deleteMemory(it.id)
            await loadMem()
        }
    }

    private func addMem() {
        let txt = memText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !txt.isEmpty else { return }
        memSaving = true
        Task {
            do {
                try await hub.saveMemory(["scope": "project", "botId": botId, "kind": "fact", "text": txt])
                memText = ""
                await loadMem()
            } catch { err = errText(error, hub.t("saveBotMemFailed")) }
            memSaving = false
        }
    }
}

/// 记忆条目：右滑或右键删除。
struct MemoryRow: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var item: MemoryItem
    var showTags = false
    var showScope = false
    var onDelete: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(localizedMemoryKind(item.kind, hub: hub)).font(.system(size: AppFont.secondary, weight: .semibold)).foregroundStyle(p.sageInk)
                if showScope, let sc = item.scope, !sc.isEmpty {
                    Text(sc == "everywhere" || sc == "global" ? hub.t("memScopeEverywhere") : hub.t("memScopeProject"))
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(.secondary)
                }
                if showTags {
                    ForEach((item.tags ?? []).filter { !$0.hasPrefix("bot:") }, id: \.self) { tag in
                        Text("#\(tag)").font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
                    }
                }
            }
            Text(item.text ?? "").font(.system(size: AppFont.body)).textSelection(.enabled)
        }
        .swipeActions { Button(hub.t("delete"), role: .destructive, action: onDelete) }
        .contextMenu { Button(hub.t("delete"), systemImage: "trash", role: .destructive, action: onDelete) }
    }
}
