import SwiftUI

/// 右侧工作面板 Inspector：仅保留无外框电脑屏幕实时预览与定时任务（Routines）。
struct BotDetailView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.hubWindowVisible) private var windowVisible
    var botId: String
    @Binding var isPresented: Bool

    @State private var previewStream = H264StreamController()
    @State private var previewUseFrame = false
    @State private var previewFrameImage: PlatformImage?
    @State private var previewFrameError = ""

    private var effectiveTarget: String {
        if let to = hub.takeovers.first(where: { $0.botId == botId }), let t = to.target, !t.isEmpty {
            return t
        }
        if let bound = hub.bots.first(where: { $0.id == botId })?.desktopTarget, !bound.isEmpty {
            return bound
        }
        let active = hub.activeDesktopTarget.target
        return active.isEmpty ? "local" : active
    }

    private var shouldUsePreviewFrame: Bool {
        !HubStore.isLocalDesktopTarget(effectiveTarget) || !hub.supports("desktop.ws_h264") || previewUseFrame
    }

    private var targetBadgeLabel: String {
        if let matched = hub.desktopTargets.first(where: { $0.id == effectiveTarget }), let name = matched.name, !name.isEmpty {
            return name
        }
        return effectiveTarget
    }

    var body: some View {
        HubSheetScaffold(
            title: hub.t("rightPanelTitle"),
            onClose: { isPresented = false },
            enableCancelShortcut: false
        ) {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    desktopPreviewCard
                    RoutinesSection(botId: botId)
                }
                .padding(.horizontal, 16)
                .padding(.top, 6)
                .padding(.bottom, 24)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(p.page.ignoresSafeArea())
        }
        .task(id: botId) {
            if hub.isConnected && hub.desktopTargets.isEmpty {
                _ = try? await hub.refreshDesktopTargets()
            }
            try? await hub.fetchRoutines(botId)
        }
        .onChange(of: effectiveTarget) { _, _ in
            previewUseFrame = false
            previewFrameImage = nil
            previewFrameError = ""
        }
        .onChange(of: previewStream.preFirstFrameFailureSeq) { _, _ in
            if windowVisible && hub.isConnected {
                previewUseFrame = true
            }
        }
        .task(id: "\(botId)-\(hub.isConnected)-\(windowVisible)-\(effectiveTarget)-\(shouldUsePreviewFrame)") {
            guard windowVisible, hub.isConnected else {
                previewStream.stop()
                return
            }
            if shouldUsePreviewFrame {
                previewStream.stop()
                await pollPreviewFrames()
                return
            }
            previewFrameImage = nil
            previewFrameError = ""
            let codec = hub.supports("desktop.ws_hevc") ? "hevc" : "h264"
            guard let req = hub.makeDesktopWSRequest(target: effectiveTarget, preset: "preview", codec: codec) else {
                previewStream.stop()
                return
            }
            previewStream.start(request: req, trackFPS: false)
        }
        .onDisappear { previewStream.stop() }
    }

    private func pollPreviewFrames() async {
        while !Task.isCancelled && windowVisible && hub.isConnected && shouldUsePreviewFrame {
            do {
                let fr = try await hub.fetchDesktopFrame(effectiveTarget)
                if Task.isCancelled || !windowVisible { break }
                let img: PlatformImage? = await Task.detached(priority: .utility) {
                    guard !fr.jpegBase64.isEmpty, let data = Data(base64Encoded: fr.jpegBase64) else { return nil }
                    return PlatformImage(data: data)
                }.value
                if Task.isCancelled || !windowVisible { break }
                if let img {
                    previewFrameImage = img
                    if !previewFrameError.isEmpty { previewFrameError = "" }
                }
            } catch {
                if Task.isCancelled { break }
                if previewFrameImage == nil {
                    previewFrameError = errText(error, hub.t("connFailed"))
                }
            }
            try? await Task.sleep(nanoseconds: 2_500_000_000)
        }
    }

    private var desktopPreviewCard: some View {
        let hasFrame = shouldUsePreviewFrame && previewFrameImage != nil
        let isReady = hasFrame || previewStream.connected
        let displayErr = shouldUsePreviewFrame ? previewFrameError : previewStream.error
        return Button {
            router.openDesktop(effectiveTarget)
        } label: {
            ZStack(alignment: .topTrailing) {
                ZStack {
                    Color.black
                    H264DisplayView(controller: previewStream)
                    if let img = previewFrameImage, shouldUsePreviewFrame {
                        Image(platform: img)
                            .resizable()
                            .aspectRatio(contentMode: .fit)
                    }
                    if !isReady {
                        VStack(spacing: 6) {
                            if !displayErr.isEmpty {
                                Image(systemName: "display")
                                    .font(.system(size: AppFont.hero))
                                    .foregroundStyle(.secondary)
                                Text(displayErr)
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(.secondary)
                                    .multilineTextAlignment(.center)
                                    .lineLimit(2)
                                    .padding(.horizontal, 12)
                            } else {
                                ProgressView()
                                    .tint(.white)
                            }
                        }
                    }
                }
                .frame(minWidth: 0, maxWidth: .infinity)
                .aspectRatio(16.0 / 10.0, contentMode: .fit)
                .clipped()

                if effectiveTarget != "local" {
                    Text(targetBadgeLabel)
                        .font(.system(size: AppFont.caption, weight: .medium))
                        .foregroundStyle(.white)
                        .lineLimit(1)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(.black.opacity(0.60), in: Capsule())
                        .padding(10)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomLeading)
                }

                Image(systemName: "arrow.up.left.and.arrow.down.right")
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(.white)
                    .padding(7)
                    .background(.black.opacity(0.55), in: Circle())
                    .padding(10)
            }
            .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .buttonStyle(.plain)
    }
}

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
    @State private var editInst = ""
    @State private var editPrivate = false
    @State private var editDesktopTarget = ""
    @State private var editBrowserMode = ""
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

    private var dirty: Bool {
        guard let a = active else { return false }
        return editName != (a.name ?? botId) || editDesc != (a.description ?? "")
            || editAvatar != (a.avatar ?? "") || editColor != (a.color ?? "")
            || editPrivate != (a.privateSessions ?? false)
            || (canBindDesktop && editDesktopTarget != (a.desktopTarget ?? ""))
            || (canIsolateBrowser && editBrowserMode != (a.browserMode ?? ""))
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
                        LabeledContent(hub.t("statHarness"), value: active?.harness ?? "-")
                        LabeledContent(hub.t("statModel"), value: (active?.model ?? "").isEmpty ? hub.t("defaultTag") : active!.model!)
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
        editInst = detail?.instructions ?? ""
        editPrivate = src?.privateSessions ?? false
        editDesktopTarget = src?.desktopTarget ?? ""
        editBrowserMode = src?.browserMode ?? ""
        loading = true
        Task { await loadMem() }
        Task { _ = try? await hub.refreshDesktopTargets() }
        if let d = try? await hub.botDetail(botId) { apply(d) }
        loading = false
    }

    private func apply(_ d: BotStatus) {
        detail = d
        editName = d.name ?? botId
        editDesc = d.description ?? ""
        editAvatar = d.avatar ?? ""
        editColor = d.color ?? ""
        editInst = d.instructions ?? ""
        editPrivate = d.privateSessions ?? false
        editDesktopTarget = d.desktopTarget ?? ""
        editBrowserMode = d.browserMode ?? ""
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
                    "instructions": editInst,
                    "privateSessions": editPrivate,
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
                            .buttonStyle(.borderedProminent).tint(pal.accent).controlSize(.small)
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
                Text(item.kind ?? "memory").font(.system(size: AppFont.secondary, weight: .semibold)).foregroundStyle(p.sageInk)
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

struct RoutinesSection: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var botId: String

    @State private var editing: RoutineItem?
    @State private var creating = false
    @State private var err = ""
    @State private var runsOpen = ""
    @State private var runs: [String: [RoutineRun]] = [:]
    @State private var confirm: ConfirmItem?

    private var list: [RoutineItem] { hub.routines[botId] ?? [] }

    var body: some View {
        let cardFill = p.dark ? Color(hex: "#1c1c1e") : p.card
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(hub.t("routines"))
                Spacer()
                Button { creating = true } label: { Label(hub.t("addShort"), systemImage: "plus") }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
            }
            .font(.system(size: AppFont.secondary))
            .foregroundStyle(p.mute)
            .padding(.horizontal, 16)

            VStack(alignment: .leading, spacing: 0) {
                if !err.isEmpty {
                    StatusText(text: err)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                }
                if list.isEmpty {
                    Text(hub.t("rtEmpty"))
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 12)
                } else {
                    ForEach(Array(list.enumerated()), id: \.element.id) { idx, rt in
                        if idx > 0 {
                            Rectangle()
                                .fill(Color.primary.opacity(p.dark ? 0.10 : 0.08))
                                .frame(height: 0.5)
                                .padding(.leading, 16)
                        }
                        row(rt)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                    }
                }
            }
            .frame(minWidth: 0, maxWidth: .infinity, alignment: .leading)
            .background(cardFill, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        }
        .confirmAlert($confirm)
        .sheet(isPresented: $creating) {
            RoutineEditor(botId: botId, routine: nil)
                .hubSheetFrame(width: 480, height: 560)
        }
        .sheet(item: $editing) { rt in
            RoutineEditor(botId: botId, routine: rt)
                .hubSheetFrame(width: 480, height: 560)
        }
    }

    private func statusText(_ s: String?) -> String {
        switch s {
        case "ok": return hub.t("rtStatusOk")
        case "silent": return hub.t("rtStatusSilent")
        case "error": return hub.t("rtStatusError")
        default: return ""
        }
    }

    private func row(_ rt: RoutineItem) -> some View {
        let trig = [
            (rt.everyMinutes ?? 0) > 0 ? hub.t("rtEveryShort", ["n": rt.everyMinutes!]) : "",
            (rt.dailyAt ?? "").isEmpty ? "" : hub.t("rtDailyShort", ["time": rt.dailyAt!]),
            (rt.cron ?? "").isEmpty ? "" : "Cron \(rt.cron!)",
            rt.webhookTrigger == true ? hub.t("rtWebhookTag") : "",
        ].filter { !$0.isEmpty }.joined(separator: " · ")
        let deliv = rt.mainDelivery == "notify" ? hub.t("rtDeliveryNotify") : rt.mainDelivery == "none" ? hub.t("rtDeliveryNone") : hub.t("rtDeliveryReceipt")
        let running = rt.running ?? false
        let status = running ? hub.t("rtStatusRunning") : statusText(rt.lastRun?.status)
        let open = runsOpen == rt.id
        let meta = [trig, deliv, (rt.webhookUrl ?? "").isEmpty ? "" : hub.t("rtWebhookPushTag"), rt.model ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
        return VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(rt.name ?? rt.id).font(.system(size: AppFont.body, weight: .semibold)).lineLimit(1)
                if !status.isEmpty {
                    Text(status).font(.system(size: AppFont.caption, weight: .semibold))
                        .foregroundStyle(rt.lastRun?.status == "error" && !running ? p.danger : p.sageInk)
                }
                Spacer()
                Toggle("", isOn: Binding(get: { rt.enabled ?? false }, set: { v in
                    Task { do { try await hub.updateRoutine(botId, rt.id, ["enabled": v]) } catch { err = errText(error, hub.t("rtStatusUpdateFailed")) } }
                }))
                .labelsHidden()
                .toggleStyle(.switch)
                .controlSize(.small)
            }
            Text(meta).font(.system(size: AppFont.caption)).foregroundStyle(.secondary)
            Text(rt.prompt ?? "").font(.system(size: AppFont.secondary)).lineLimit(4)
            HStack(spacing: 8) {
                Button(running ? hub.t("rtRunningBtn") : hub.t("rtRunNow")) {
                    err = ""
                    Task { do { try await hub.runRoutine(botId, rt.id) } catch { err = errText(error, hub.t("rtRunFailed")) } }
                }
                .disabled(running)
                Button(open ? hub.t("rtRunsHide") : hub.t("rtRunsBtn")) {
                    if open { runsOpen = ""; return }
                    runsOpen = rt.id
                    Task { if let l = try? await hub.fetchRoutineRuns(botId, rt.id, limit: 6) { runs[rt.id] = l } }
                }
                Menu {
                    Button(hub.t("edit"), systemImage: "pencil") { editing = rt }
                    if rt.webhookTrigger == true, let path = rt.webhookPath, !path.isEmpty {
                        Button(hub.t("rtCopyWebhook"), systemImage: "doc.on.doc") {
                            var base = hub.baseURL
                            if base.hasSuffix("/") { base.removeLast() }
                            copyToClipboard(base + path)
                        }
                    }
                    Divider()
                    Button(hub.t("delete"), systemImage: "trash", role: .destructive) {
                        confirm = ConfirmItem(title: hub.t("rtDeleteTitle", ["name": rt.name ?? rt.id]), message: hub.t("rtDeleteDesc")) {
                            try? await hub.deleteRoutine(botId, rt.id)
                        }
                    }
                } label: {
                    Label(hub.t("edit"), systemImage: "ellipsis.circle").labelStyle(.iconOnly)
                }
                .menuIndicator(.hidden)
                .fixedSize()
            }
            .buttonStyle(.bordered)
            .controlSize(.small)
            if open {
                let l = runs[rt.id] ?? []
                if l.isEmpty {
                    Text(hub.t("rtRunsEmpty")).font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
                } else {
                    ForEach(l) { r in
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(statusText(r.status).isEmpty ? hub.t("rtStatusError") : statusText(r.status)) · \(r.trigger ?? "") · \(String(format: "%.1fs", (r.durationMs ?? 0) / 1000))")
                                .font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
                            if let e = r.error, !e.isEmpty {
                                Text(e).font(.system(size: AppFont.secondary)).foregroundStyle(p.danger)
                            } else if let o = r.output, !o.isEmpty {
                                Text(o).font(.system(size: AppFont.secondary)).lineLimit(3)
                            }
                        }
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }
}

struct RoutineEditor: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.dismiss) private var dismiss
    var botId: String
    var routine: RoutineItem?

    enum Mode: String, CaseIterable { case interval, daily, cron, webhook }

    @State private var name = ""
    @State private var mode: Mode = .interval
    @State private var every = "60"
    @State private var daily = "09:00"
    @State private var cron = "0 9 * * 1-5"
    @State private var webhookTrigger = false
    @State private var secret = ""
    @State private var secretSet = false
    @State private var clearSecret = false
    @State private var webhookUrl = ""
    @State private var delivery = "receipt"
    @State private var prompt = ""
    @State private var enabled = true
    @State private var saving = false
    @State private var err = ""

    private func modeLabel(_ m: Mode) -> String {
        switch m {
        case .interval: return hub.t("rtModeInterval")
        case .daily: return hub.t("rtModeDaily")
        case .cron: return hub.t("rtModeCron")
        case .webhook: return hub.t("rtModeWebhook")
        }
    }

    var body: some View {
        HubSheetScaffold(
            title: routine == nil ? hub.t("routines") : (routine?.name ?? hub.t("routines")),
            closeTitle: hub.t("cancel"),
            onClose: { dismiss() },
            actionTitle: hub.t("rtSaveBtn"),
            loading: saving,
            onAction: { save() }
        ) {
            Form {
                if !err.isEmpty { Section { StatusText(text: err) } }
                Section {
                    TextField(hub.t("rtNamePh"), text: $name)
                    HubSegmentedPicker(
                        selection: Binding(get: { mode }, set: { mode = $0; if $0 == .webhook { webhookTrigger = true } }),
                        options: Mode.allCases.map { ($0, modeLabel($0)) }
                    )
                    switch mode {
                    case .interval: TextField(hub.t("rtEveryPh"), text: $every).monoInput()
                        .numberPadKeyboard()
                    case .daily: TextField(hub.t("rtDailyPh"), text: $daily).monoInput()
                    case .cron: TextField(hub.t("rtCronPh"), text: $cron).monoInput()
                    case .webhook: EmptyView()
                    }
                    if mode != .webhook { Toggle(hub.t("rtAllowWebhook"), isOn: $webhookTrigger) }
                }
                if mode == .webhook || webhookTrigger {
                    Section("Webhook") {
                        SecureField(secretSet && !clearSecret ? hub.t("rtWebhookSecretSetPh") : hub.t("rtWebhookSecretPh"), text: $secret)
                            .disabled(clearSecret)
                        if secretSet {
                            Toggle(hub.t("rtWebhookClearSecret"), isOn: Binding(get: { clearSecret }, set: { clearSecret = $0; if $0 { secret = "" } }))
                        }
                    }
                }
                Section {
                    TextField(hub.t("rtWebhookUrlPh"), text: $webhookUrl).monoInput()
                    HubSegmentedPicker(
                        selection: $delivery,
                        options: [
                            ("receipt", hub.t("rtDeliveryReceipt")),
                            ("notify", hub.t("rtDeliveryNotify")),
                            ("none", hub.t("rtDeliveryNone")),
                        ]
                    )
                    TextField(hub.t("rtPromptPh"), text: $prompt, axis: .vertical).lineLimit(3...12)
                    Toggle(hub.t("rtEnabled"), isOn: $enabled)
                }
            }
            .hubForm()
        }
        .onAppear(perform: load)
    }

    private func load() {
        let rt = routine
        name = rt?.name ?? ""
        let hasEvery = (rt?.everyMinutes ?? 0) > 0
        let hasDaily = !(rt?.dailyAt ?? "").isEmpty
        let hasCron = !(rt?.cron ?? "").isEmpty
        mode = hasCron ? .cron : (hasDaily && !hasEvery) ? .daily : (!hasEvery && !hasDaily && rt?.webhookTrigger == true) ? .webhook : .interval
        every = hasEvery ? "\(rt!.everyMinutes!)" : "60"
        daily = hasDaily ? rt!.dailyAt! : "09:00"
        cron = hasCron ? rt!.cron! : "0 9 * * 1-5"
        webhookTrigger = rt?.webhookTrigger == true || mode == .webhook
        secretSet = rt?.webhookSecretSet ?? false
        webhookUrl = rt?.webhookUrl ?? ""
        delivery = rt?.mainDelivery ?? "receipt"
        prompt = rt?.prompt ?? ""
        enabled = rt?.enabled ?? true
    }

    private func save() {
        let n = name.trimmingCharacters(in: .whitespaces)
        let pr = prompt.trimmingCharacters(in: .whitespacesAndNewlines)
        if n.isEmpty { err = hub.t("rtNameRequired"); return }
        if pr.isEmpty { err = hub.t("rtPromptRequired"); return }
        let everyMinutes = mode == .interval ? (Int(every.trimmingCharacters(in: .whitespaces)) ?? 0) : 0
        let dailyAt = mode == .daily ? daily.trimmingCharacters(in: .whitespaces) : ""
        let cronExpr = mode == .cron ? cron.trimmingCharacters(in: .whitespaces) : ""
        if mode == .interval && everyMinutes <= 0 { err = hub.t("rtEveryPositive"); return }
        if mode == .daily && dailyAt.range(of: "^\\d{2}:\\d{2}$", options: .regularExpression) == nil { err = hub.t("rtTimeFormatErr"); return }
        if mode == .cron && cronExpr.isEmpty { err = hub.t("rtCronRequired"); return }
        err = ""
        var body: [String: Any] = [
            "name": n, "everyMinutes": everyMinutes, "dailyAt": dailyAt, "cron": cronExpr,
            "webhookTrigger": mode == .webhook ? true : webhookTrigger,
            "webhookUrl": webhookUrl.trimmingCharacters(in: .whitespaces),
            "prompt": pr, "mainDelivery": delivery, "enabled": enabled,
        ]
        let sec = secret.trimmingCharacters(in: .whitespaces)
        if clearSecret { body["clearWebhookSecret"] = true } else if !sec.isEmpty { body["webhookSecret"] = sec }
        saving = true
        Task {
            do {
                if let id = routine?.id { try await hub.updateRoutine(botId, id, body) } else { try await hub.createRoutine(botId, body) }
                dismiss()
            } catch { err = errText(error, hub.t("rtSaveFailed")) }
            saving = false
        }
    }
}
