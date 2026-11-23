import SwiftUI

struct SettingsView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL

    struct BotDraft: Equatable { var harness: String; var model: String }

    @State private var defaultHarness = "opencode"
    @State private var defaultHarnessTouched = false
    @State private var defaultModel = ""
    @State private var defaultTouched = false
    @State private var perHarness: [String: String] = [:]
    @State private var perTouched = false
    @State private var perOpen = false
    @State private var userMd = ""
    @State private var userMdTouched = false
    @State private var drafts: [String: BotDraft] = [:]
    @State private var touchedBots: Set<String> = []
    @State private var loadingModelsFor: Set<String> = []
    @State private var saving = false
    @State private var busy = ""
    @State private var error = ""
    @State private var saved = ""
    @State private var confirm: ConfirmItem?
    @State private var showUnsavedAlert = false

    @State private var mem: MemoryList?
    @State private var memScope = "global"
    @State private var memFilterKind = ""
    @State private var memQuery = ""
    @State private var memAdd = false
    @State private var memKind = "fact"
    @State private var memText = ""
    @State private var memTags = ""

    @State private var deskHarness = ""
    @State private var deskModel = ""
    @State private var deskTouched = false
    @State private var deskPerms: [DesktopPermissionItem] = []
    @State private var deskPermsLoading = false

    private var harnesses: [HarnessInfo] {
        if let h = hub.settings?.harnesses, !h.isEmpty { return h }
        return ["opencode", "claude", "pi"].map { HarnessInfo(id: $0, label: $0) }
    }

    private var primaryHarness: String { harnesses.first?.id ?? "opencode" }

    private var botSnapshotKeys: [String] {
        hub.bots.map { "\($0.id)|\($0.harness ?? "")|\($0.model ?? "")|\($0.modelInherited ?? false)" }
    }

    private var baseDefaultHarness: String {
        if let dh = hub.settings?.defaultHarness, !dh.isEmpty { return dh }
        return primaryHarness
    }

    private func clean(_ m: [String: String]?) -> [String: String] {
        (m ?? [:]).compactMapValues { let t = $0.trimmingCharacters(in: .whitespaces); return t.isEmpty ? nil : t }
    }

    private func origModel(_ b: BotStatus) -> String {
        (b.modelInherited == true ? "" : (b.model ?? "")).trimmingCharacters(in: .whitespaces)
    }

    private var botUpdates: [(id: String, harness: String, model: String)] {
        hub.bots.compactMap { b in
            guard touchedBots.contains(b.id), let d = drafts[b.id] else { return nil }
            let m = d.model.trimmingCharacters(in: .whitespaces)
            return d.harness != (b.harness ?? "") || m != origModel(b) ? (b.id, d.harness, m) : nil
        }
    }

    private var settingsChanged: Bool {
        guard let s = hub.settings else { return false }
        func rtrim(_ x: String) -> String { x.replacingOccurrences(of: "\\s+$", with: "", options: .regularExpression) }
        return (defaultHarnessTouched && defaultHarness != baseDefaultHarness)
            || (defaultTouched && defaultModel.trimmingCharacters(in: .whitespaces) != (s.defaultModel ?? "").trimmingCharacters(in: .whitespaces))
            || (perTouched && clean(perHarness) != clean(s.defaultModels))
            || (userMdTouched && rtrim(userMd) != rtrim(s.sharedUserMd ?? ""))
    }

    private var dirty: Bool { settingsChanged || !botUpdates.isEmpty }

    var body: some View {
        Form {
            if !error.isEmpty { Section { StatusText(text: error) } }
            if !saved.isEmpty { Section { StatusText(text: saved, error: false) } }
            appearanceSection
            hostSection
            magpieSection
            nativeCLISection
            defaultModelSection
            botModelsSection
            Section {
                TextField(hub.t("globalSharedMem"), text: Binding(
                    get: { userMd },
                    set: {
                        guard $0 != userMd else { return }
                        userMd = $0; userMdTouched = true; saved = ""
                    }
                ),
                          prompt: Text(hub.t("userMdPh")), axis: .vertical)
                    .lineLimit(4...20)
                    .labelsHidden()
            } header: {
                Text(hub.t("globalSharedMem"))
            } footer: {
                Text("_shared/USER.md").font(.system(size: 13, design: .monospaced))
            }
            memorySection
            desktopPermissionsSection
            remoteSection
        }
        .hubForm()
        .interactiveDismissDisabled(dirty)
        .navigationTitle(hub.t("settings"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            SheetToolbar(
                actionTitle: hub.t("save"),
                actionDisabled: !dirty,
                loading: saving,
                onClose: {
                    if dirty { showUnsavedAlert = true } else { router.sheet = nil }
                }
            ) { save() }
        }
        .alert(hub.t("unsavedTitle"), isPresented: $showUnsavedAlert) {
            Button(hub.t("saveAndLeave")) { save(closeAfter: true) }
            Button(hub.t("discardChanges"), role: .destructive) { router.sheet = nil }
            Button(hub.t("cancel"), role: .cancel) {}
        } message: {
            Text(hub.t("unsavedDesc"))
        }
        .confirmAlert($confirm)
        .task {
            _ = try? await hub.loadSettings()
            try? await hub.refreshBots()
            await loadMem("")
            await loadPerms()
        }
        .onAppear { sync() }
        .modifier(SettingsSyncModifier(
            defaultHarness: hub.settings?.defaultHarness,
            harnessIds: (hub.settings?.harnesses ?? []).map(\.id),
            defaultModel: hub.settings?.defaultModel,
            sharedUserMd: hub.settings?.sharedUserMd,
            desktopModel: hub.settings?.desktopModel,
            defaultModels: hub.settings?.defaultModels,
            botSnapshotKeys: botSnapshotKeys,
            onSync: sync,
            onSyncDrafts: syncDrafts
        ))
    }

    private func sync() {
        guard let s = hub.settings else { return }
        if !defaultHarnessTouched { defaultHarness = baseDefaultHarness }
        if !defaultTouched { defaultModel = (s.defaultModel ?? "").trimmingCharacters(in: .whitespaces) }
        if !deskTouched { deskHarness = s.desktopHarness ?? ""; deskModel = s.desktopModel ?? "" }
        if !perTouched {
            perHarness = clean(s.defaultModels)
            if !perHarness.isEmpty { perOpen = true }
        }
        if !userMdTouched { userMd = s.sharedUserMd ?? "" }
        syncDrafts()
    }

    private func syncDrafts() {
        let validIds = Set(hub.bots.map(\.id))
        for id in drafts.keys where !validIds.contains(id) {
            drafts.removeValue(forKey: id)
        }
        for b in hub.bots where !touchedBots.contains(b.id) {
            drafts[b.id] = BotDraft(harness: b.harness ?? "", model: origModel(b))
        }
    }

    private func save(closeAfter: Bool = false) {
        error = ""; saved = ""; saving = true
        Task {
            do {
                try await hub.saveSettings(changed: settingsChanged, defaultHarness: defaultHarness, defaultModel: defaultModel.trimmingCharacters(in: .whitespaces),
                                           defaultModels: clean(perHarness), sharedUserMd: userMd, botUpdates: botUpdates)
                defaultHarnessTouched = false; defaultTouched = false; perTouched = false; userMdTouched = false
                touchedBots = []
                drafts = [:]
                sync()
                saved = hub.t("settingsSaved")
                if closeAfter { router.sheet = nil }
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
            Button { router.openDesktop(hub.takeovers.first?.target ?? "local") } label: {
                Label(hub.takeovers.isEmpty ? hub.t("remoteDesktop") : "\(hub.t("remoteDesktop")) (\(hub.takeovers.count))", systemImage: "display")
            }
            Button { router.sheet = .devices } label: {
                let n = hub.nodes.filter { $0.connected == true }.count
                Label(n > 0 ? "\(hub.t("devicesTunnel")) (\(n))" : hub.t("devicesTunnel"), systemImage: "globe")
            }
        }
    }

    // MARK: Magpie

    private var connectedHarnesses: [HarnessInfo] {
        harnesses.filter { $0.magpie?.connected == true }
    }

    private var nativeHarnesses: [HarnessInfo] {
        harnesses.filter { $0.magpie?.connected != true }
    }

    private var magpieSection: some View {
        let mp = hub.settings?.magpie
        let installed = mp?.installed == true
        return Group {
            Section {
                if !installed {
                    Text(hub.t("hostNoMagpieGlobal")).foregroundStyle(.secondary)
                } else if connectedHarnesses.isEmpty {
                    Text(hub.t("mpNoConnected")).foregroundStyle(.secondary)
                } else {
                    ForEach(connectedHarnesses) { h in harnessRow(h) }
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

    private var nativeCLISection: some View {
        let installed = hub.settings?.magpie?.installed == true
        return Group {
            if !nativeHarnesses.isEmpty {
                Section {
                    ForEach(nativeHarnesses) { h in nativeHarnessRow(h, magpieInstalled: installed) }
                } header: {
                    Text(hub.t("nativeClisTitle"))
                } footer: {
                    Text(hub.t("nativeClisIntro"))
                }
            }
        }
    }

    private func nativeHarnessRow(_ h: HarnessInfo, magpieInstalled: Bool) -> some View {
        let cands = hub.models[h.id] ?? []
        let key = "magpie:\(h.id)"
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(h.title).font(.system(size: 16, weight: .semibold))
                    Text(hub.t("mpNative") + ((h.magpie?.nativeModel ?? "").isEmpty ? "" : " · \(h.magpie!.nativeModel!)"))
                        .font(.system(size: 13)).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer()
                if busy == key { ProgressView().controlSize(.small) }
                if magpieInstalled {
                    Button(hub.t("mpConnectAction")) { connectMagpie(h) }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                        .disabled(busy == key)
                }
            }
            HStack {
                TextField(h.title, text: Binding(
                    get: { perHarness[h.id] ?? "" },
                    set: {
                        guard $0 != (perHarness[h.id] ?? "") else { return }
                        perHarness[h.id] = $0; perTouched = true; saved = ""
                    }
                ), prompt: Text(hub.t("nativeModelPh")))
                    .monoInput()
                    .labelsHidden()
                Menu {
                    if cands.isEmpty {
                        Button(loadingModelsFor.contains(h.id) ? hub.t("loading") : hub.t("optionalModels")) {
                            loadCands(for: h.id)
                        }
                    } else {
                        ForEach(Array(cands.prefix(40)), id: \.self) { m in
                            Button(m) {
                                guard m != (perHarness[h.id] ?? "") else { return }
                                perHarness[h.id] = m; perTouched = true; saved = ""
                            }
                        }
                    }
                } label: {
                    Label(hub.t("optionalModels"), systemImage: "list.bullet").labelStyle(.iconOnly)
                }
                .menuIndicator(.hidden)
                .fixedSize()
                .onAppear { if cands.isEmpty, hub.isConnected { loadCands(for: h.id) } }
            }
        }
        .padding(.vertical, 2)
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
            Picker(hub.t("language"), selection: $h.langPref) {
                Text(hub.t("followSystem")).tag("system")
                Text("简体中文").tag("zh-CN")
                Text("English").tag("en")
            }
        }
    }

    // MARK: 模型

    @ViewBuilder
    private func modelMenu(for harness: String, current: String, onSelect: @escaping (String) -> Void) -> some View {
        let cands = hub.models[harness] ?? []
        Menu {
            if !current.trimmingCharacters(in: .whitespaces).isEmpty {
                Button(hub.t("clear")) { onSelect("") }
                Divider()
            }
            if cands.isEmpty {
                Button(loadingModelsFor.contains(harness) ? hub.t("loading") : hub.t("optionalModels")) {
                    loadCands(for: harness)
                }
                .disabled(loadingModelsFor.contains(harness))
            } else {
                ForEach(Array(cands.prefix(40)), id: \.self) { m in
                    Button(m) { onSelect(m) }
                }
            }
        } label: {
            Label(hub.t("optionalModels"), systemImage: "list.bullet").labelStyle(.iconOnly)
        }
        .menuIndicator(.hidden)
        .fixedSize()
        .onAppear {
            if cands.isEmpty, hub.isConnected, !harness.isEmpty {
                loadCands(for: harness)
            }
        }
    }

    private var defaultModelSection: some View {
        Section(hub.t("globalDefaultModel")) {
            Picker(hub.t("defaultHarness"), selection: Binding(
                get: { defaultHarness },
                set: {
                    guard $0 != defaultHarness else { return }
                    defaultHarness = $0; defaultHarnessTouched = true; saved = ""
                }
            )) {
                ForEach(harnesses) { Text($0.title).tag($0.id) }
            }
            HStack {
                TextField(hub.t("globalDefaultModel"), text: Binding(
                    get: { defaultModel },
                    set: {
                        guard $0 != defaultModel else { return }
                        defaultModel = $0; defaultTouched = true; saved = ""
                    }
                ),
                          prompt: Text("magpie/claude-sonnet-4"))
                    .monoInput()
                    .labelsHidden()
                modelMenu(for: primaryHarness, current: defaultModel) { m in
                    guard m != defaultModel else { return }
                    defaultModel = m; defaultTouched = true; saved = ""
                }
            }
            DisclosureGroup(hub.t("perHarnessTitle"), isExpanded: $perOpen) {
                ForEach(harnesses) { h in
                    HStack {
                        TextField(h.title, text: Binding(
                            get: { perHarness[h.id] ?? "" },
                            set: {
                                guard $0 != (perHarness[h.id] ?? "") else { return }
                                perHarness[h.id] = $0; perTouched = true; saved = ""
                            }
                        ),
                                  prompt: Text(hub.t("followGlobalPh")))
                            .monoInput()
                        modelMenu(for: h.id, current: perHarness[h.id] ?? "") { m in
                            guard m != (perHarness[h.id] ?? "") else { return }
                            perHarness[h.id] = m; perTouched = true; saved = ""
                        }
                    }
                }
            }
        }
    }

    private func loadCands(for harness: String? = nil) {
        let target = harness ?? primaryHarness
        guard !target.isEmpty, !loadingModelsFor.contains(target) else { return }
        loadingModelsFor.insert(target)
        Task {
            do { _ = try await hub.loadModels(target) } catch { self.error = errText(error, hub.t("loadModelsFailed")) }
            loadingModelsFor.remove(target)
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
                        Picker(hub.t("statHarness"), selection: Binding(get: { d.harness }, set: {
                            guard $0 != d.harness else { return }
                            var n = d; n.harness = $0; drafts[b.id] = n; touchedBots.insert(b.id); saved = ""
                            if (hub.models[$0] ?? []).isEmpty, hub.isConnected { loadCands(for: $0) }
                        })) {
                            if !harnesses.contains(where: { $0.id == d.harness }) { Text(d.harness.isEmpty ? "-" : d.harness).tag(d.harness) }
                            ForEach(harnesses) { Text($0.title).tag($0.id) }
                        }
                        .labelsHidden()
                        .fixedSize()
                    }
                    HStack {
                        TextField(hub.t("statModel"), text: Binding(get: { d.model }, set: {
                            guard $0 != d.model else { return }
                            var n = d; n.model = $0; drafts[b.id] = n; touchedBots.insert(b.id); saved = ""
                        }),
                                  prompt: Text(ph.isEmpty ? hub.t("inheritDefaultPh") : ph))
                            .monoInput()
                            .labelsHidden()
                        modelMenu(for: d.harness, current: d.model) { m in
                            guard m != d.model else { return }
                            var n = d; n.model = m; drafts[b.id] = n; touchedBots.insert(b.id); saved = ""
                        }
                    }
                }
                .padding(.vertical, 2)
            }
        }
    }

    // MARK: 全局记忆

    private func permText(_ key: String, _ fallback: String) -> String {
        let val = hub.t(key)
        return val == key ? fallback : val
    }

    private func loadMem(_ q: String, scope: String? = nil, kind: String? = nil) async {
        let sc = scope ?? memScope
        let kd = kind ?? memFilterKind
        do { mem = try await hub.fetchMemories(scope: sc, query: q, kind: kd, limit: 30) } catch { self.error = errText(error, hub.t("opFailed")) }
    }

    private var memorySection: some View {
        Section {
            if mem?.installed == true, let sem = mem?.semantic {
                HStack(spacing: 8) {
                    Tag(
                        text: hub.t(sem.active == true ? "memSemanticOn" : "memSemanticOff"),
                        bg: sem.active == true ? p.sage : p.s1,
                        fg: sem.active == true ? p.sageInk : .secondary
                    )
                    Spacer()
                    if sem.active != true {
                        Button(hub.t("openMagpie")) {
                            run("magpie:open", "opFailed") { try await hub.openMagpie(); return nil }
                        }
                        .font(.system(size: 13, weight: .medium))
                    }
                }
                if sem.active != true {
                    Text(hub.t("memSemanticHintOff"))
                        .font(.system(size: 13))
                        .foregroundStyle(.secondary)
                }
            }
            Picker("Scope", selection: Binding(
                get: { memScope },
                set: { next in
                    memScope = next
                    Task { await loadMem(memQuery, scope: next) }
                }
            )) {
                Text(hub.t("memScopeGlobal")).tag("global")
                Text(hub.t("memScopeAll")).tag("all")
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            Picker(hub.t("memType"), selection: Binding(
                get: { memFilterKind },
                set: { next in
                    memFilterKind = next
                    Task { await loadMem(memQuery, kind: next) }
                }
            )) {
                Text(hub.t("memKindAll")).tag("")
                ForEach(["fact", "preference", "rule", "reference", "profile", "procedure", "episode"], id: \.self) {
                    Text($0).tag($0)
                }
            }
            HStack {
                TextField(
                    hub.t("search"),
                    text: $memQuery,
                    prompt: Text(hub.t(mem?.semantic?.active == true ? "searchSemanticMemPh" : "searchGlobalMemPh"))
                )
                    .labelsHidden()
                    .onSubmit { Task { await loadMem(memQuery) } }
                if !memQuery.isEmpty {
                    Button {
                        memQuery = ""
                        Task { await loadMem("") }
                    } label: {
                        Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
                    }
                    .buttonStyle(.borderless)
                }
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
                    MemoryRow(item: it, showTags: true, showScope: memScope == "all") {
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

    private func loadPerms() async {
        deskPermsLoading = true
        defer { deskPermsLoading = false }
        if let resp = try? await hub.fetchDesktopPermissions("local") {
            deskPerms = resp.items ?? []
        }
    }

    private func reqPerm(_ id: String, openSettings: Bool) {
        run("perm:\(id)", "opFailed") {
            let res = try await hub.requestDesktopPermission(target: "local", permission: id, openSettings: openSettings)
            if let items = res.items { deskPerms = items }
            return res.openedSettings == true ? hub.t("deskPermOpened") : (res.prompted == true ? hub.t("deskPermPrompted") : nil)
        }
    }

    private var desktopPermissionsSection: some View {
        Section {
            if deskPerms.isEmpty {
                if deskPermsLoading {
                    ProgressView()
                } else {
                    Text(hub.t("deskPermsDesc")).foregroundStyle(.secondary)
                }
            } else {
                ForEach(deskPerms) { item in
                    let stLabel = item.status == "granted" ? hub.t("deskPermGranted")
                        : item.status == "denied" ? hub.t("deskPermDenied")
                        : item.status == "unsupported" ? hub.t("deskPermUnsupported")
                        : hub.t("deskPermNotDetermined")
                    let bg = item.status == "granted" ? p.sage : item.status == "denied" ? p.pink : p.s1
                    let fg: Color = item.status == "granted" ? p.sageInk : item.status == "denied" ? p.danger : .secondary
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(spacing: 8) {
                            Text(permText("deskPermName_\(item.id)", item.name)).font(.system(size: 16, weight: .semibold))
                            Spacer()
                            Tag(text: stLabel, bg: bg, fg: fg)
                        }
                        Text(permText("deskPermDesc_\(item.id)", item.description)).font(.system(size: 13)).foregroundStyle(.secondary)
                        if (item.canPrompt == true && item.status != "granted") || item.canOpenSettings == true {
                            HStack(spacing: 8) {
                                if item.canPrompt == true && item.status != "granted" {
                                    Button(hub.t("deskPermRequest")) { reqPerm(item.id, openSettings: false) }
                                        .buttonStyle(.borderedProminent).controlSize(.small)
                                        .disabled(busy == "perm:\(item.id)")
                                }
                                if item.canOpenSettings == true {
                                    Button(hub.t("deskPermOpenSettings")) { reqPerm(item.id, openSettings: true) }
                                        .buttonStyle(.bordered).controlSize(.small)
                                        .disabled(busy == "perm:\(item.id)")
                                }
                            }
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
            Button { router.openDesktop("local") } label: {
                Label(hub.t("openDesktop"), systemImage: "display")
            }
        } header: {
            HStack {
                Text(hub.t("deskPermsTitle"))
                Spacer()
                Button { Task { await loadPerms() } } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .disabled(deskPermsLoading)
            }
        } footer: {
            Text(hub.t("deskPermsDesc"))
        }
    }

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

private struct SettingsSyncModifier: ViewModifier {
    let defaultHarness: String?
    let harnessIds: [String]?
    let defaultModel: String?
    let sharedUserMd: String?
    let desktopModel: String?
    let defaultModels: [String: String]?
    let botSnapshotKeys: [String]
    let onSync: () -> Void
    let onSyncDrafts: () -> Void

    func body(content: Content) -> some View {
        content
            .onChange(of: defaultHarness) { _, _ in onSync() }
            .onChange(of: harnessIds) { _, _ in onSync() }
            .onChange(of: defaultModel) { _, _ in onSync() }
            .onChange(of: sharedUserMd) { _, _ in onSync() }
            .onChange(of: desktopModel) { _, _ in onSync() }
            .onChange(of: defaultModels) { _, _ in onSync() }
            .onChange(of: botSnapshotKeys) { _, _ in onSyncDrafts() }
    }
}
