import SwiftUI

struct SettingsView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL

    struct BotDraft: Equatable { var harness: String; var model: String }

    @State private var defaultModel = ""
    @State private var defaultTouched = false
    @State private var perHarness: [String: String] = [:]
    @State private var perTouched = false
    @State private var perOpen = false
    @State private var userMd = ""
    @State private var userMdTouched = false
    @State private var drafts: [String: BotDraft] = [:]
    @State private var loadingModelsFor = ""
    @State private var saving = false
    @State private var busy = ""
    @State private var error = ""
    @State private var saved = ""
    @State private var confirm: ConfirmItem?

    @State private var mem: MemoryList?
    @State private var memQuery = ""
    @State private var memAdd = false
    @State private var memKind = "fact"
    @State private var memText = ""
    @State private var memTags = ""

    @State private var deskHarness = ""
    @State private var deskModel = ""
    @State private var deskTouched = false

    private var harnesses: [HarnessInfo] {
        if let h = hub.settings?.harnesses, !h.isEmpty { return h }
        return ["opencode", "claude", "pi"].map { HarnessInfo(id: $0, label: $0) }
    }

    private var primaryHarness: String { harnesses.first?.id ?? "opencode" }

    private func clean(_ m: [String: String]?) -> [String: String] {
        (m ?? [:]).compactMapValues { let t = $0.trimmingCharacters(in: .whitespaces); return t.isEmpty ? nil : t }
    }

    private func origModel(_ b: BotStatus) -> String { b.modelInherited == true ? "" : (b.model ?? "") }

    private var botUpdates: [(id: String, harness: String, model: String)] {
        hub.bots.compactMap { b in
            guard let d = drafts[b.id] else { return nil }
            let m = d.model.trimmingCharacters(in: .whitespaces)
            return d.harness != (b.harness ?? "") || m != origModel(b) ? (b.id, d.harness, m) : nil
        }
    }

    private var settingsChanged: Bool {
        let s = hub.settings
        func rtrim(_ x: String) -> String { x.replacingOccurrences(of: "\\s+$", with: "", options: .regularExpression) }
        return defaultModel.trimmingCharacters(in: .whitespaces) != (s?.defaultModel ?? "")
            || clean(perHarness) != clean(s?.defaultModels)
            || rtrim(userMd) != rtrim(s?.sharedUserMd ?? "")
    }

    private var dirty: Bool { settingsChanged || !botUpdates.isEmpty }

    var body: some View {
        Form {
            if !error.isEmpty { Section { StatusText(text: error) } }
            if !saved.isEmpty { Section { StatusText(text: saved, error: false) } }
            hostSection
            magpieSection
            appearanceSection
            defaultModelSection
            botModelsSection
            Section {
                TextField(hub.t("globalSharedMem"), text: Binding(get: { userMd }, set: { userMd = $0; userMdTouched = true; saved = "" }),
                          prompt: Text(hub.t("userMdPh")), axis: .vertical)
                    .lineLimit(4...20)
                    .labelsHidden()
            } header: {
                Text(hub.t("globalSharedMem"))
            } footer: {
                Text("_shared/USER.md").font(.system(size: 13, design: .monospaced))
            }
            memorySection
            remoteSection
        }
        .hubForm()
        .navigationTitle(hub.t("settings"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { SheetToolbar(actionTitle: hub.t("save"), actionDisabled: !dirty, loading: saving) { save() } }
        .confirmAlert($confirm)
        .task {
            _ = try? await hub.loadSettings()
            try? await hub.refreshBots()
            await loadMem("")
        }
        .onAppear { sync() }
        .onChange(of: hub.settings?.defaultModel) { _, _ in sync() }
        .onChange(of: hub.settings?.sharedUserMd) { _, _ in sync() }
        .onChange(of: hub.settings?.desktopModel) { _, _ in sync() }
        .onChange(of: hub.settings?.defaultModels) { _, _ in sync() }
        .onChange(of: hub.bots.map(\.id)) { _, _ in syncDrafts() }
    }

    private func sync() {
        guard let s = hub.settings else { return }
        if !defaultTouched { defaultModel = s.defaultModel ?? "" }
        if !deskTouched { deskHarness = s.desktopHarness ?? ""; deskModel = s.desktopModel ?? "" }
        if !perTouched {
            perHarness = clean(s.defaultModels)
            if !perHarness.isEmpty { perOpen = true }
        }
        if !userMdTouched { userMd = s.sharedUserMd ?? "" }
        syncDrafts()
    }

    private func syncDrafts() {
        for b in hub.bots where drafts[b.id] == nil {
            drafts[b.id] = BotDraft(harness: b.harness ?? "", model: origModel(b))
        }
    }

    private func save() {
        error = ""; saved = ""; saving = true
        Task {
            do {
                try await hub.saveSettings(changed: settingsChanged, defaultModel: defaultModel.trimmingCharacters(in: .whitespaces),
                                           defaultModels: clean(perHarness), sharedUserMd: userMd, botUpdates: botUpdates)
                defaultTouched = false; perTouched = false; userMdTouched = false
                drafts = [:]
                sync()
                saved = hub.t("settingsSaved")
            } catch { self.error = errText(error, hub.t("saveFailed")) }
            saving = false
        }
    }

    private func run(_ key: String, _ fail: String, _ op: @escaping () async throws -> String?) {
        error = ""; saved = ""; busy = key
        Task {
            do { if let msg = try await op() { saved = msg } } catch { self.error = errText(error, hub.t(fail)) }
            busy = ""
        }
    }

    // MARK: 主机

    private var hostColor: Color {
        switch hub.connState {
        case .connected: return hub.online ? p.sageInk : p.busy
        case .connecting: return p.busy
        case .unauthorized, .error: return p.danger
        case .idle: return p.mute
        }
    }

    private var hostText: String {
        guard let s = hub.activeServer else { return hub.t("disconnected") }
        switch hub.connState {
        case .connecting: return "\(s.label) · \(hub.t("connectingShort"))"
        case .unauthorized: return "\(s.label) · \(hub.t("needsToken"))"
        case .error: return "\(s.label) · \(hub.t("connLost"))"
        case .connected: return hub.online ? s.label : "\(s.label) · \(hub.t("reconnecting"))"
        case .idle: return hub.t("disconnected")
        }
    }

    private var hostSection: some View {
        Section(hub.t("hostSection")) {
            Button { router.sheet = .connect(scan: false) } label: {
                HStack(spacing: 8) {
                    Circle().fill(hostColor).frame(width: 8, height: 8)
                    Text(hostText).foregroundStyle(p.text).lineLimit(1)
                    Spacer()
                    Text(hub.t("switchHost")).foregroundStyle(p.mute)
                }
            }
            Button { router.sheet = .desktop(target: hub.takeovers.first?.target ?? "local") } label: {
                Label(hub.takeovers.isEmpty ? hub.t("remoteDesktop") : "\(hub.t("remoteDesktop")) (\(hub.takeovers.count))", systemImage: "display")
            }
            Button { router.sheet = .devices } label: {
                let n = hub.nodes.filter { $0.connected == true }.count
                Label(n > 0 ? "\(hub.t("devicesTunnel")) (\(n))" : hub.t("devicesTunnel"), systemImage: "globe")
            }
        }
    }

    // MARK: Magpie

    private var magpieSection: some View {
        let mp = hub.settings?.magpie
        let installed = mp?.installed == true
        return Group {
            Section {
                if !installed {
                    Text(hub.t("hostNoMagpieGlobal")).foregroundStyle(.secondary)
                } else {
                    ForEach(harnesses) { h in harnessRow(h) }
                }
            } header: {
                HStack {
                    Text(hub.t("magpieTakeover"))
                    Spacer()
                    Tag(text: !installed ? hub.t("notInstalled") : mp?.healthy == true ? hub.t("gatewayOk") : hub.t("gatewayDown"),
                        bg: !installed ? p.s1 : mp?.healthy == true ? p.sage : p.pink,
                        fg: !installed ? .secondary : mp?.healthy == true ? p.sageInk : p.danger)
                        .textCase(nil)
                }
            } footer: {
                if installed { Text(hub.t("magpieHint")) }
            }
            if installed {
                Section {
                    let addr = hub.activeServer?.address ?? ""
                    let local = ["localhost", "127.0.0.1", "[::1]"].contains { addr.contains($0) }
                    if let web = mp?.webUrl, !web.isEmpty, !local, let url = URL(string: web) {
                        Button(hub.t("mpOpenWeb")) { openURL(url) }
                    } else {
                        Button(hub.t("mpOpenApp")) {
                            run("magpie:open", "opFailed") { try await hub.openMagpie(); return nil }
                        }
                    }
                } footer: {
                    Text(hub.t("mpManageInApp"))
                }
            }
        }
    }

    private func harnessRow(_ h: HarnessInfo) -> some View {
        let connected = h.magpie?.connected == true
        let key = "magpie:\(h.id)"
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(h.title).font(.system(size: 16, weight: .semibold))
                    Text(connected ? "Magpie · \((h.magpie?.model ?? "").isEmpty ? hub.t("mpConnected") : h.magpie!.model!)"
                         : hub.t("mpNative") + ((h.magpie?.nativeModel ?? "").isEmpty ? "" : " · \(h.magpie!.nativeModel!)"))
                        .font(.system(size: 13)).foregroundStyle(connected ? p.sageInk : .secondary).lineLimit(1)
                }
                Spacer()
                if busy == key { ProgressView().controlSize(.small) }
                if connected {
                    Button(hub.t("mpRestore"), role: .destructive) {
                        confirm = ConfirmItem(title: hub.t("restoreNativeTitle"), message: hub.t("restoreNativeDesc", ["label": h.id])) {
                            run(key, "restoreFailed") { try await hub.disconnectMagpie(h.id); return hub.t("restoredNative", ["label": h.id]) }
                        }
                    }
                    .buttonStyle(.bordered)
                } else {
                    Button(hub.t("mpConnect")) { connectMagpie(h) }.buttonStyle(.borderedProminent)
                }
            }
            .controlSize(.small)
            .disabled(busy == key)
        }
        .padding(.vertical, 2)
    }

    private func connectMagpie(_ h: HarnessInfo) {
        let dm = defaultModel.trimmingCharacters(in: .whitespaces)
        let target = dm.isEmpty ? (h.defaultModel ?? "") : dm
        guard !target.isEmpty else { error = hub.t("pickMagpieModel"); return }
        confirm = ConfirmItem(title: hub.t("mpConnect"), message: "magpie \(h.id) \(target)", destructive: false) {
            run("magpie:\(h.id)", "connectFailed") {
                try await hub.connectMagpie(h.id, model: target)
                return hub.t("connectedMagpie", ["label": h.id])
            }
        }
    }

    // MARK: 外观

    private var appearanceSection: some View {
        @Bindable var h = hub
        return Section(hub.t("appearance")) {
            Picker(hub.t("appearance"), selection: $h.themePref) {
                Text(hub.t("followSystem")).tag("system")
                Text(hub.t("themeLight")).tag("light")
                Text(hub.t("themeDark")).tag("dark")
            }
            .pickerStyle(.segmented)
            VStack(alignment: .leading, spacing: 10) {
                HStack {
                    Text(hub.t("appAccent"))
                    Spacer()
                    Button(hub.t("resetDefault")) { hub.accentHex = Palette.defaultAccent }
                        .buttonStyle(.borderless)
                        .disabled(hub.accentHex.lowercased() == Palette.defaultAccent)
                }
                HStack(spacing: 14) {
                    ForEach(Palette.accentPresets, id: \.self) { hex in
                        Button { hub.accentHex = hex } label: {
                            Circle().fill(Color(hex: hex)).frame(width: 28, height: 28)
                                .overlay {
                                    if hub.accentHex.lowercased() == hex {
                                        Image(systemName: "checkmark").font(.system(size: 13, weight: .bold)).foregroundStyle(.white)
                                    }
                                }
                        }
                        .buttonStyle(.plain)
                    }
                }
                Text(hub.t("appAccentDesc")).font(.system(size: 13)).foregroundStyle(.secondary)
            }
            .padding(.vertical, 4)
            Picker(hub.t("language"), selection: $h.langPref) {
                Text(hub.t("followSystem")).tag("system")
                Text("简体中文").tag("zh-CN")
                Text("English").tag("en")
            }
        }
    }

    // MARK: 模型

    private var defaultModelSection: some View {
        let cands = hub.models[primaryHarness] ?? []
        return Section(hub.t("globalDefaultModel")) {
            HStack {
                TextField(hub.t("globalDefaultModel"), text: Binding(get: { defaultModel }, set: { defaultModel = $0; defaultTouched = true; saved = "" }),
                          prompt: Text("magpie/claude-sonnet-4"))
                    .monoInput()
                    .labelsHidden()
                Menu {
                    if cands.isEmpty {
                        Button(loadingModelsFor == primaryHarness ? hub.t("loading") : hub.t("optionalModels")) { loadCands() }
                    } else {
                        ForEach(Array(cands.prefix(24)), id: \.self) { m in
                            Button(m) { defaultModel = m; defaultTouched = true; saved = "" }
                        }
                    }
                } label: {
                    Label(hub.t("optionalModels"), systemImage: "list.bullet").labelStyle(.iconOnly)
                }
                .menuIndicator(.hidden)
                .fixedSize()
                .onAppear { if cands.isEmpty, hub.isConnected { loadCands() } }
            }
            DisclosureGroup(hub.t("perHarnessTitle"), isExpanded: $perOpen) {
                ForEach(harnesses) { h in
                    TextField(h.title, text: Binding(get: { perHarness[h.id] ?? "" },
                                                    set: { perHarness[h.id] = $0; perTouched = true; saved = "" }),
                              prompt: Text(hub.t("followGlobalPh")))
                        .monoInput()
                }
            }
        }
    }

    private func loadCands() {
        guard loadingModelsFor.isEmpty else { return }
        loadingModelsFor = primaryHarness
        Task {
            do { try await hub.loadModels(primaryHarness) } catch { self.error = errText(error, hub.t("loadModelsFailed")) }
            loadingModelsFor = ""
        }
    }

    private var botModelsSection: some View {
        Section(hub.t("botModelsSection")) {
            if hub.bots.isEmpty {
                Text(hub.t("noBotsInSettings")).foregroundStyle(.secondary)
            }
            ForEach(Array(hub.bots.enumerated()), id: \.element.id) { i, b in
                let d = drafts[b.id] ?? BotDraft(harness: b.harness ?? "", model: origModel(b))
                let per = (perHarness[d.harness] ?? "").trimmingCharacters(in: .whitespaces)
                let ph = per.isEmpty ? resolveDefaultModel(defaultModel.trimmingCharacters(in: .whitespaces), d.harness) : per
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 10) {
                        BotAvatar(id: b.id, size: 28, index: i, avatar: b.avatar, color: b.color)
                        VStack(alignment: .leading, spacing: 0) {
                            Text(b.displayName).font(.system(size: 16, weight: .semibold))
                            Text("@\(b.id)").font(.system(size: 13, design: .monospaced)).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Picker(hub.t("statHarness"), selection: Binding(get: { d.harness }, set: { var n = d; n.harness = $0; drafts[b.id] = n; saved = "" })) {
                            if !harnesses.contains(where: { $0.id == d.harness }) { Text(d.harness.isEmpty ? "-" : d.harness).tag(d.harness) }
                            ForEach(harnesses) { Text($0.title).tag($0.id) }
                        }
                        .labelsHidden()
                        .fixedSize()
                    }
                    TextField(hub.t("statModel"), text: Binding(get: { d.model }, set: { var n = d; n.model = $0; drafts[b.id] = n; saved = "" }),
                              prompt: Text(ph.isEmpty ? hub.t("inheritDefaultPh") : ph))
                        .monoInput()
                        .labelsHidden()
                }
                .padding(.vertical, 2)
            }
        }
    }

    // MARK: 全局记忆

    private func loadMem(_ q: String) async {
        do { mem = try await hub.fetchMemories(scope: "global", query: q, limit: 30) } catch { self.error = errText(error, hub.t("opFailed")) }
    }

    private var memorySection: some View {
        Section {
            HStack {
                TextField(hub.t("search"), text: $memQuery, prompt: Text(hub.t("searchGlobalMemPh")))
                    .labelsHidden()
                    .onSubmit { Task { await loadMem(memQuery) } }
                Button { Task { await loadMem(memQuery) } } label: { Label(hub.t("search"), systemImage: "magnifyingglass").labelStyle(.iconOnly) }
                    .buttonStyle(.borderless)
            }
            if mem?.installed != true {
                Text(hub.t("hostNoMagpieGlobal")).foregroundStyle(.secondary)
            } else if mem?.available != true {
                Text(hub.t("magpieNoResp") + ((mem?.error ?? "").isEmpty ? "" : ": \(mem!.error!)")).foregroundStyle(p.danger)
            } else if (mem?.items ?? []).isEmpty {
                Text(hub.t("noGlobalMem")).foregroundStyle(.secondary)
            } else {
                ForEach(mem?.items ?? []) { it in
                    MemoryRow(item: it, showTags: true) {
                        confirm = ConfirmItem(title: hub.t("deleteMemTitle"), message: hub.t("deleteMemConfirm")) {
                            do {
                                try await hub.deleteMemory(it.id)
                                await loadMem(memQuery)
                            } catch { self.error = errText(error, hub.t("delFailed")) }
                        }
                    }
                }
            }
            DisclosureGroup(hub.t("addShort"), isExpanded: $memAdd) {
                Picker("Kind", selection: $memKind) {
                    ForEach(["fact", "preference", "rule", "reference"], id: \.self) { Text($0).tag($0) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                TextField(hub.t("magpieGlobalMem"), text: $memText, prompt: Text(hub.t("globalMemPh")), axis: .vertical).lineLimit(2...8)
                TextField(hub.t("tagsPh"), text: $memTags)
                Button(hub.t("saveMemory")) {
                    let txt = memText.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !txt.isEmpty else { error = hub.t("memContentReq"); return }
                    let tags = memTags.components(separatedBy: CharacterSet(charactersIn: ",，"))
                        .map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
                    run("mem:save", "saveMemFailed") {
                        try await hub.saveMemory(["scope": "global", "kind": memKind, "text": txt, "tags": tags])
                        memText = ""; memTags = ""; memAdd = false
                        await loadMem(memQuery)
                        return hub.t("globalMemSaved")
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(busy == "mem:save")
            }
        } header: {
            Text(hub.t("magpieGlobalMem"))
        }
    }

    // MARK: 远程与设备

    private var remoteSection: some View {
        Section(hub.t("deskWorkerTitle")) {
            Picker(hub.t("deskWorkerHarnessLabel"), selection: Binding(get: { deskHarness }, set: { deskHarness = $0; deskTouched = true })) {
                Text(hub.t("deskWorkerFollowBot")).tag("")
                ForEach(hub.settings?.harnesses ?? []) { Text($0.title).tag($0.id) }
            }
            HStack {
                TextField(hub.t("deskWorkerModelLabel"), text: Binding(get: { deskModel }, set: { deskModel = $0; deskTouched = true }),
                          prompt: Text(hub.t("deskWorkerModelPh")))
                    .monoInput()
                Button(hub.t("save")) {
                    run("desk", "saveFailed") {
                        try await hub.setDesktopWorker(harness: deskHarness, model: deskModel)
                        deskTouched = false
                        return hub.t("deskWorkerSaved")
                    }
                }
                .buttonStyle(.bordered)
                .disabled(!deskTouched || busy == "desk")
            }
        }
    }
}
