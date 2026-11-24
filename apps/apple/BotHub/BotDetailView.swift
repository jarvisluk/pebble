import SwiftUI

struct BotDetailView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    var botId: String
    @Binding var isPresented: Bool

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
    @State private var saving = false
    @State private var err = ""
    @State private var confirm: ConfirmItem?

    @State private var mem: MemoryList?
    @State private var memLoading = false
    @State private var memText = ""
    @State private var memSaving = false
    @State private var previewStream = H264StreamController()

    private var index: Int { hub.bots.firstIndex { $0.id == botId } ?? -1 }
    private var bot: BotStatus? { index >= 0 ? hub.bots[index] : nil }
    private var active: BotStatus? { detail ?? bot }

    private var dirty: Bool {
        guard let a = active else { return false }
        return editName != (a.name ?? botId) || editDesc != (a.description ?? "")
            || editAvatar != (a.avatar ?? "") || editColor != (a.color ?? "")
            || editPrivate != (a.privateSessions ?? false) || editInst != (detail?.instructions ?? "")
    }

    var body: some View {
        let pal = p.bot(botId, index, custom: editColor.isEmpty ? active?.color : editColor)
        Form { Group {
            if !err.isEmpty { Section { StatusText(text: err) } }
            Section {
                Button {
                    router.openDesktop(hub.takeovers.first?.target ?? "local")
                } label: {
                    ZStack(alignment: .topTrailing) {
                        ZStack {
                            Color.black
                            H264DisplayView(controller: previewStream)
                            if !previewStream.connected {
                                VStack(spacing: 6) {
                                    if !previewStream.error.isEmpty {
                                        Image(systemName: "display")
                                            .font(.system(size: 20))
                                            .foregroundStyle(.secondary)
                                        Text(previewStream.error)
                                            .font(.system(size: 12))
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
                        .aspectRatio(16.0 / 10.0, contentMode: .fit)

                        Image(systemName: "arrow.up.left.and.arrow.down.right")
                            .font(.system(size: 12, weight: .semibold))
                            .foregroundStyle(.white)
                            .padding(7)
                            .background(.black.opacity(0.55), in: Circle())
                            .padding(12)
                    }
                }
                .buttonStyle(.plain)
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.black)
            }
            Section {
                HStack(spacing: 14) {
                    BotAvatar(id: botId, size: 56, index: index, avatar: editAvatar, color: editColor)
                    VStack(alignment: .leading, spacing: 4) {
                        TextField(hub.t("botNamePh"), text: $editName).font(.system(size: 16, weight: .semibold))
                        Text("@\(botId)").font(.system(size: 13, design: .monospaced)).foregroundStyle(.secondary)
                    }
                }
                TextField(hub.t("descPh"), text: $editDesc, axis: .vertical)
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
                            isPresented = false
                            router.openFiles(botId: botId)
                        } label: {
                            HStack(spacing: 4) {
                                Text(active?.workspace ?? "-")
                                    .font(.system(size: 13, design: .monospaced))
                                    .lineLimit(1)
                                    .truncationMode(.middle)
                                Image(systemName: "folder")
                                    .font(.system(size: 12))
                            }
                        }
                        .buttonStyle(.plain)
                    }
                    Toggle(hub.t("privateSessions"), isOn: $editPrivate).tint(pal.accent)
                    VStack(alignment: .leading, spacing: 6) {
                        Text("AGENTS.md").font(.system(size: 15)).foregroundStyle(.secondary)
                        if loading && detail == nil {
                            ProgressView()
                        } else {
                            TextField(hub.t("agentsPh"), text: $editInst, axis: .vertical)
                                .lineLimit(6...24)
                                .font(.system(size: 15, design: .monospaced))
                        }
                    }
                }
            }

            RoutinesSection(botId: botId)

            Section {
                Button(hub.t("resetSessionBtn")) {
                    confirm = ConfirmItem(title: hub.t("resetSessionTitle"), message: hub.t("resetSessionDesc", ["name": bot?.displayName ?? botId])) {
                        do {
                            try await hub.resetBot(botId)
                            isPresented = false
                            try? await hub.loadMessages(botId)
                        } catch { err = errText(error, hub.t("resetFailed")) }
                    }
                }
                if hub.bots.count > 1 {
                    Button(hub.t("deleteBotBtn"), role: .destructive) {
                        confirm = ConfirmItem(title: hub.t("deleteBotTitle", ["name": bot?.displayName ?? botId]), message: hub.t("deleteBotDesc")) {
                            do {
                                try await hub.deleteBot(botId)
                                isPresented = false
                                router.closeBot()
                            } catch { err = errText(error, hub.t("delFailed")) }
                        }
                    }
                }
            }
        }.listRowBackground(p.card) }
        .hubForm()
        .navigationTitle(hub.t("botDetails"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button(hub.t("close")) { isPresented = false }
            }
            ToolbarItem(placement: .confirmationAction) {
                if saving { ProgressView().controlSize(.small) } else {
                    Button(hub.t("save")) { save() }
                        .disabled(!dirty || editName.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
        .confirmAlert($confirm)
        .task(id: botId) {
            if hub.isConnected, let req = hub.makeDesktopWSRequest(target: hub.takeovers.first?.target ?? "local") {
                previewStream.start(request: req)
            }
            await open()
        }
        .onDisappear { previewStream.stop() }
    }

    private func open() async {
        let src = detail ?? bot
        editName = src?.name ?? botId
        editDesc = src?.description ?? ""
        editAvatar = src?.avatar ?? ""
        editColor = src?.color ?? ""
        editInst = detail?.instructions ?? ""
        editPrivate = src?.privateSessions ?? false
        loading = true
        Task { await loadMem() }
        Task { try? await hub.fetchRoutines(botId) }
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
    }

    private func save() {
        let n = editName.trimmingCharacters(in: .whitespaces)
        guard !n.isEmpty else { err = hub.t("nameRequired"); return }
        err = ""
        saving = true
        Task {
            do {
                try await hub.updateBot(botId, ["name": n, "description": editDesc.trimmingCharacters(in: .whitespaces),
                                                "avatar": editAvatar, "color": editColor,
                                                "instructions": editInst, "privateSessions": editPrivate])
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
                HStack(alignment: .bottom) {
                    TextField(hub.t("botMemPh"), text: $memText, axis: .vertical).lineLimit(1...5)
                    if memSaving { ProgressView().controlSize(.small) } else {
                        Button(hub.t("saveMemory")) { addMem() }
                            .buttonStyle(.borderedProminent).tint(pal.accent).controlSize(.small)
                            .disabled(memText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
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
                Text(item.kind ?? "memory").font(.system(size: 13, weight: .semibold)).foregroundStyle(p.sageInk)
                if showScope, let sc = item.scope, !sc.isEmpty {
                    Text(sc == "everywhere" || sc == "global" ? hub.t("memScopeEverywhere") : hub.t("memScopeProject"))
                        .font(.system(size: 12))
                        .foregroundStyle(.secondary)
                }
                if showTags {
                    ForEach((item.tags ?? []).filter { !$0.hasPrefix("bot:") }, id: \.self) { tag in
                        Text("#\(tag)").font(.system(size: 13)).foregroundStyle(.secondary)
                    }
                }
            }
            Text(item.text ?? "").font(.system(size: 15)).textSelection(.enabled)
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
        Section {
            if !err.isEmpty { StatusText(text: err) }
            if list.isEmpty { Text(hub.t("rtEmpty")).foregroundStyle(.secondary) }
            ForEach(list) { rt in row(rt) }
        } header: {
            HStack {
                Text(hub.t("routines"))
                Spacer()
                Button { creating = true } label: { Label(hub.t("addShort"), systemImage: "plus") }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
            }
        }
        .confirmAlert($confirm)
        .sheet(isPresented: $creating) {
            NavigationStack { RoutineEditor(botId: botId, routine: nil) }
        }
        .sheet(item: $editing) { rt in
            NavigationStack { RoutineEditor(botId: botId, routine: rt) }
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
                Text(rt.name ?? rt.id).font(.system(size: 16, weight: .semibold)).lineLimit(1)
                if !status.isEmpty {
                    Text(status).font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(rt.lastRun?.status == "error" && !running ? p.danger : p.sageInk)
                }
                Spacer()
                Toggle("", isOn: Binding(get: { rt.enabled ?? false }, set: { v in
                    Task { do { try await hub.updateRoutine(botId, rt.id, ["enabled": v]) } catch { err = errText(error, hub.t("rtStatusUpdateFailed")) } }
                }))
                .labelsHidden()
            }
            Text(meta).font(.system(size: 13)).foregroundStyle(.secondary)
            Text(rt.prompt ?? "").font(.system(size: 15)).lineLimit(4)
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
                    Text(hub.t("rtRunsEmpty")).font(.system(size: 13)).foregroundStyle(.secondary)
                } else {
                    ForEach(l) { r in
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(statusText(r.status).isEmpty ? hub.t("rtStatusError") : statusText(r.status)) · \(r.trigger ?? "") · \(String(format: "%.1fs", (r.durationMs ?? 0) / 1000))")
                                .font(.system(size: 13)).foregroundStyle(.secondary)
                            if let e = r.error, !e.isEmpty {
                                Text(e).font(.system(size: 13)).foregroundStyle(p.danger)
                            } else if let o = r.output, !o.isEmpty {
                                Text(o).font(.system(size: 13)).lineLimit(3)
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
        Form {
            if !err.isEmpty { Section { StatusText(text: err) } }
            Section {
                TextField(hub.t("rtNamePh"), text: $name)
                Picker(hub.t("routines"), selection: Binding(get: { mode }, set: { mode = $0; if $0 == .webhook { webhookTrigger = true } })) {
                    ForEach(Mode.allCases, id: \.self) { Text(modeLabel($0)).tag($0) }
                }
                .pickerStyle(.segmented)
                switch mode {
                case .interval: TextField(hub.t("rtEveryPh"), text: $every).monoInput()
                    .keyboardType(.numberPad)
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
                Picker(hub.t("rtDeliveryReceipt"), selection: $delivery) {
                    Text(hub.t("rtDeliveryReceipt")).tag("receipt")
                    Text(hub.t("rtDeliveryNotify")).tag("notify")
                    Text(hub.t("rtDeliveryNone")).tag("none")
                }
                .pickerStyle(.segmented)
                TextField(hub.t("rtPromptPh"), text: $prompt, axis: .vertical).lineLimit(3...12)
                Toggle(hub.t("rtEnabled"), isOn: $enabled)
            }
        }
        .hubForm()
        .navigationTitle(routine == nil ? hub.t("routines") : (routine?.name ?? hub.t("routines")))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button(hub.t("cancel")) { dismiss() } }
            ToolbarItem(placement: .confirmationAction) {
                if saving { ProgressView().controlSize(.small) } else { Button(hub.t("rtSaveBtn")) { save() } }
            }
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
