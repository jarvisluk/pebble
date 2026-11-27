import SwiftUI

struct SettingsModelsTab: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL

    @Binding var error: String
    @Binding var saved: String
    @Binding var confirm: ConfirmItem?

    struct BotDraft: Equatable {
        var harness: String
        var model: String
    }

    @State private var defaultHarness = ""
    @State private var defaultHarnessTouched = false
    @State private var defaultPermMode = "auto"
    @State private var defaultPermModeTouched = false
    @State private var defaultModel = ""
    @State private var defaultTouched = false
    @State private var mpWebUrl = ""
    @State private var mpWebUrlTouched = false
    @State private var drafts: [String: BotDraft] = [:]
    @State private var touchedBots: Set<String> = []

    @State private var moreEnginesOpen = false
    @State private var expandedInstall: Set<String> = []
    @State private var installTasks: [String: SetupTaskStatus] = [:]
    @State private var loginTasks: [String: SetupTaskStatus] = [:]
    @State private var pollTasks: [String: Task<Void, Never>] = [:]
    @State private var copiedKey = ""
    @State private var busy = ""

    private var harnesses: [HarnessInfo] { settingsHarnessList(from: hub) }
    private var installedHarnesses: [HarnessInfo] { installedHarnessList(from: hub) }
    private var uninstalledHarnesses: [HarnessInfo] { harnesses.filter { $0.installed != true } }

    private var baseDefaultHarness: String {
        if let dh = hub.settings?.defaultHarness, !dh.isEmpty { return dh }
        return installedHarnesses.first?.id ?? ""
    }

    private var baseDefaultModel: String {
        let override = (hub.settings?.defaultModels?[baseDefaultHarness] ?? "").trimmingCharacters(in: .whitespaces)
        if !override.isEmpty { return override }
        return (hub.settings?.defaultModel ?? "").trimmingCharacters(in: .whitespaces)
    }

    private var baseDefaultPermMode: String {
        let m = (hub.settings?.defaultPermissionMode ?? "").trimmingCharacters(in: .whitespaces)
        return m.isEmpty ? "auto" : m
    }

    private func origModel(_ b: BotStatus) -> String {
        (b.modelInherited == true ? "" : (b.model ?? "")).trimmingCharacters(in: .whitespaces)
    }

    private var botSnapshotKeys: [String] {
        hub.bots.map { "\($0.id)|\($0.harness ?? "")|\($0.model ?? "")|\($0.modelInherited ?? false)" }
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
        let webDirty = mpWebUrlTouched && mpWebUrl.trimmingCharacters(in: .whitespaces) != (s.magpie?.webUrl ?? "").trimmingCharacters(in: .whitespaces)
        return (defaultHarnessTouched && defaultHarness != baseDefaultHarness)
            || (defaultPermModeTouched && defaultPermMode != baseDefaultPermMode)
            || (defaultTouched && defaultModel.trimmingCharacters(in: .whitespaces) != baseDefaultModel)
            || webDirty
    }

    private var dirty: Bool { settingsChanged || !botUpdates.isEmpty }

    var body: some View {
        defaultsAndBotsSection
        enginesSection
        magpieGatewaySection
            .task(id: hub.isConnected) {
                guard hub.isConnected else { return }
                _ = try? await hub.loadSettings()
                try? await hub.refreshBots()
                sync()
            }
            .task(id: "\(hub.isConnected)|\(hub.modelsGeneration)|\(installedHarnesses.map(\.id).joined(separator: ","))") {
                guard hub.isConnected else { return }
                await preloadAllModels(force: false)
            }
            .onAppear {
                sync()
                registerHandlers()
            }
            .onDisappear {
                for (_, t) in pollTasks { t.cancel() }
                pollTasks.removeAll()
                router.settingsDirty = false
                router.settingsSaveAction = nil
                router.settingsDiscardAction = nil
            }
            .onChange(of: dirty) { _, v in router.settingsDirty = v }
            .onChange(of: hub.settings?.defaultHarness) { _, _ in sync() }
            .onChange(of: hub.settings?.defaultPermissionMode) { _, _ in sync() }
            .onChange(of: hub.settings?.defaultModel) { _, _ in sync() }
            .onChange(of: hub.settings?.defaultModels) { _, _ in sync() }
            .onChange(of: hub.settings?.magpie?.webUrl) { _, _ in sync() }
            .onChange(of: hub.settings?.harnesses) { _, _ in sync() }
            .onChange(of: botSnapshotKeys) { _, _ in syncDrafts() }
    }

    // MARK: Card 1 - Magpie Gateway

    private var magpieGatewaySection: some View {
        let mp = hub.settings?.magpie
        let installed = mp?.installed == true
        let providers = mp?.providers ?? []
        let presets = mp?.presets ?? []

        return Section {
            if !installed {
                Text(hub.t("hostNoMagpieGlobal")).foregroundStyle(.secondary)
                Button {
                    run("magpie:install", "opFailed") {
                        try await hub.installMagpie()
                        await preloadAllModels(force: true)
                        return hub.t("mpInstallDone")
                    }
                } label: {
                    if busy == "magpie:install" {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.small)
                            Text(hub.t("mpInstalling"))
                        }
                    } else {
                        Text(hub.t("mpInstallOnHost"))
                    }
                }
                .disabled(!busy.isEmpty)
            } else {
                HubFormField(label: hub.t("mpWebUrl")) {
                    TextField(
                        hub.t("mpWebUrl"),
                        text: Binding(
                            get: { mpWebUrl },
                            set: {
                                guard $0 != mpWebUrl else { return }
                                mpWebUrl = $0
                                mpWebUrlTouched = true
                                saved = ""
                            }
                        ),
                        prompt: Text("https://magpie.example.com")
                    )
                    .monoInput()
                    Text(hub.t("mpWebUrlHint"))
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(.secondary)
                }

                let configuredIds = Set(providers.map(\.id))
                let extraPresets = presets.filter { !configuredIds.contains($0.id) }
                if !providers.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(hub.t("providersTitle", ["n": providers.count]))
                            .font(.system(size: AppFont.secondary, weight: .medium))
                            .foregroundStyle(.secondary)
                        FlowLayout(spacing: 6) {
                            ForEach(providers) { prov in
                                Tag(
                                    text: (prov.name ?? "").isEmpty ? prov.id : prov.name!,
                                    bg: prov.enabled == false ? p.s1 : p.sage,
                                    fg: prov.enabled == false ? .secondary : p.sageInk
                                )
                            }
                        }
                    }
                } else if !extraPresets.isEmpty {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(hub.t("mpPresetsTitle"))
                            .font(.system(size: AppFont.secondary, weight: .medium))
                            .foregroundStyle(.secondary)
                        FlowLayout(spacing: 6) {
                            ForEach(extraPresets) { pre in
                                Tag(text: (pre.name ?? "").isEmpty ? pre.id : pre.name!, bg: p.s1, fg: p.text2)
                            }
                        }
                    }
                }

                let addr = hub.activeServer?.address ?? hub.baseURL
                let local = ["localhost", "127.0.0.1", "[::1]"].contains { addr.contains($0) }
                if let web = mp?.webUrl, !web.isEmpty, !local, let url = URL(string: web) {
                    Button(hub.t("mpOpenWeb")) { openURL(url) }
                } else {
                    Button(hub.t("mpOpenApp")) {
                        run("magpie:open", "opFailed") { try await hub.openMagpie(); return nil }
                    }
                }
            }
        } header: {
            HStack {
                Text(hub.t("magpieGatewayTitle"))
                Spacer()
                Tag(
                    text: !installed ? hub.t("notInstalled") : mp?.healthy == true ? hub.t("gatewayOk") : hub.t("gatewayDown"),
                    bg: !installed ? p.s1 : mp?.healthy == true ? p.sage : p.pink,
                    fg: !installed ? .secondary : mp?.healthy == true ? p.sageInk : p.danger
                )
                .textCase(nil)
            }
        } footer: {
            if installed {
                Text(hub.t("mpManageInApp"))
            }
        }
    }

    // MARK: Card 2 - Harnesses

    @ViewBuilder
    private var recheckHeaderButton: some View {
        if busy == "recheck" {
            ProgressView().controlSize(.small)
        } else {
            Button(hub.t("enginesRecheck")) {
                recheckEnvironment()
            }
            .buttonStyle(.borderless)
            .font(.system(size: AppFont.secondary))
            .textCase(nil)
        }
    }

    @ViewBuilder
    private var enginesSection: some View {
        let installed = installedHarnesses
        let connected = installed.filter { $0.magpie?.connected == true }
        let native = installed.filter { $0.magpie?.connected != true }
        let available = uninstalledHarnesses

        if installed.isEmpty {
            Section {
                Text(hub.t("engineNoInstalled")).foregroundStyle(.secondary)
                ForEach(available) { h in
                    uninstalledEngineRow(h)
                }
            } header: {
                HStack {
                    Text(hub.t("enginesTitle"))
                    Spacer()
                    recheckHeaderButton
                }
            } footer: {
                Text(hub.t("enginesIntro"))
            }
        } else {
            if !connected.isEmpty {
                Section {
                    ForEach(connected) { h in
                        installedEngineRow(h)
                    }
                } header: {
                    HStack {
                        Text("\(hub.t("harnessGroupConnected")) (\(connected.count))")
                            .foregroundStyle(p.sageInk)
                        Spacer()
                        recheckHeaderButton
                    }
                } footer: {
                    if native.isEmpty {
                        Text(hub.t("enginesIntro"))
                    }
                }
            }

            if !native.isEmpty || !available.isEmpty {
                Section {
                    ForEach(native) { h in
                        installedEngineRow(h)
                    }
                    if !available.isEmpty {
                        DisclosureGroup(hub.t("enginesMoreAvailable", ["n": available.count]), isExpanded: $moreEnginesOpen) {
                            ForEach(available) { h in
                                uninstalledEngineRow(h)
                            }
                        }
                    }
                } header: {
                    HStack {
                        Text("\(hub.t("harnessGroupNative")) (\(native.count))")
                        Spacer()
                        if connected.isEmpty {
                            recheckHeaderButton
                        }
                    }
                } footer: {
                    Text(hub.t("enginesIntro"))
                }
            }
        }
    }

    @ViewBuilder
    private func installedEngineRow(_ h: HarnessInfo) -> some View {
        let mpConnected = h.magpie?.connected == true
        let loginTask = loginTasks[h.id]
        let loggingIn = loginTask?.state == "running"
        let needsLogin = h.requiresLogin == true && h.authStatus != "ready"

        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 6) {
                        Circle()
                            .fill(mpConnected ? p.sageInk : needsLogin ? p.danger : p.mute.opacity(0.45))
                            .frame(width: 7, height: 7)
                        Text(h.title)
                            .font(.system(size: AppFont.body, weight: .semibold))
                            .lineLimit(1)
                    }
                    if mpConnected {
                        let modelDesc = (h.magpie?.model ?? "").isEmpty ? hub.t("mpConnected") : h.magpie!.model!
                        Text(modelDesc)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(p.sageInk)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    } else if let nativeModel = h.magpie?.nativeModel, !nativeModel.isEmpty {
                        Text(nativeModel)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    } else if let sum = h.summary, !sum.isEmpty {
                        Text(sum)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    } else {
                        Text(h.authStatus == "ready" ? hub.t("engineReady") : h.id)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                Spacer(minLength: 8)
                if needsLogin {
                    Button {
                        startHarnessLogin(h.id)
                    } label: {
                        if loggingIn {
                            HStack(spacing: 6) {
                                ProgressView().controlSize(.small)
                                Text(hub.t("engineLoggingIn"))
                            }
                        } else {
                            Text(hub.t("engineLoginBtn"))
                        }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(loggingIn)
                }
            }

            if loggingIn {
                HStack(spacing: 8) {
                    if let urlStr = loginTask?.loginUrl, let u = URL(string: urlStr) {
                        Button(hub.t("engineOpenLogin")) { openExternalURL(u) }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                    }
                    Button(hub.t("engineCheckLogin")) { pollHarnessLoginNow(h.id) }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                }
            }

            if needsLogin || (loginTask?.state == "error" && !(loginTask?.authDetail ?? h.authDetail ?? "").isEmpty) {
                HStack(spacing: 6) {
                    if needsLogin {
                        Tag(text: hub.t("engineNeedsLogin"), bg: p.pink, fg: p.pinkInk)
                    }
                    if let detail = loginTask?.authDetail ?? h.authDetail, !detail.isEmpty {
                        Text(detail)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(loginTask?.state == "error" ? p.danger : .secondary)
                            .lineLimit(2)
                    }
                }
            }

            if let loginErr = loginTask?.error, !loginErr.isEmpty, loginTask?.state == "error" {
                Text(loginErr)
                    .font(.system(size: AppFont.caption))
                    .foregroundStyle(p.danger)
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder
    private func uninstalledEngineRow(_ h: HarnessInfo) -> some View {
        let task = installTasks[h.id]
        let installing = task?.state == "running"
        let cmd = (h.install?.command ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let urlStr = (h.install?.url ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let isExpanded = expandedInstall.contains(h.id)

        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(h.title)
                        .font(.system(size: AppFont.body, weight: .semibold))
                    if let sum = h.summary, !sum.isEmpty {
                        Text(sum)
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if h.canAutoInstall == true {
                    Button {
                        startHarnessInstall(h.id)
                    } label: {
                        if installing {
                            HStack(spacing: 6) {
                                ProgressView().controlSize(.small)
                                Text(hub.t("engineInstalling"))
                            }
                        } else {
                            Text(hub.t("engineInstallBtn"))
                        }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                    .disabled(installing)
                } else {
                    Button(isExpanded ? hub.t("collapse") : hub.t("engineManualInstall")) {
                        if isExpanded { expandedInstall.remove(h.id) } else { expandedInstall.insert(h.id) }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }

            if isExpanded || task?.state == "error" {
                if let instErr = task?.error, !instErr.isEmpty {
                    Text(instErr)
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(p.danger)
                }
                let shownCmd = cmd.isEmpty ? hub.t("engineManualHint", ["name": h.title]) : cmd
                HStack(spacing: 8) {
                    Text(shownCmd)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(p.text2)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if !cmd.isEmpty {
                        Button(copiedKey == "eng-inst-\(h.id)" ? hub.t("copied") : hub.t("copy")) {
                            copyToClipboard(cmd)
                            copiedKey = "eng-inst-\(h.id)"
                            Task {
                                try? await Task.sleep(for: .milliseconds(1200))
                                if copiedKey == "eng-inst-\(h.id)" { copiedKey = "" }
                            }
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                    }
                }
                if !urlStr.isEmpty, let u = URL(string: urlStr) {
                    Button(hub.t("engineViewDocs")) { openExternalURL(u) }
                        .buttonStyle(.borderless)
                        .font(.system(size: AppFont.secondary))
                }
            }
        }
        .padding(.vertical, 4)
    }

    // MARK: Card 3 - Defaults & Bot Overrides

    private var defaultsAndBotsSection: some View {
        Section(hub.t("globalAndBotModels")) {
            VStack(alignment: .leading, spacing: 3) {
                HubMenuPicker(
                    title: hub.t("defaultHarness"),
                    selection: Binding(
                        get: { defaultHarness },
                        set: { next in
                            guard next != defaultHarness else { return }
                            defaultHarness = next
                            defaultHarnessTouched = true
                            if !defaultModel.trimmingCharacters(in: .whitespaces).isEmpty {
                                defaultModel = ""
                                defaultTouched = true
                            }
                            saved = ""
                            Task { _ = try? await hub.loadModels(next) }
                        }
                    ),
                    options: installedHarnesses.map { ($0.id, $0.title) },
                    fallbackLabel: harnessLabelForSelection(defaultHarness, from: hub),
                    disabled: installedHarnesses.isEmpty
                )
                if installedHarnesses.isEmpty {
                    Text(hub.t("engineNoInstalled"))
                        .font(.system(size: AppFont.caption))
                        .foregroundStyle(p.mute)
                }
            }

            HubFormField(label: hub.t("defaultModel")) {
                ModelFieldRow(
                    title: hub.t("defaultModel"),
                    harness: defaultHarness,
                    text: Binding(
                        get: { defaultModel },
                        set: {
                            guard $0 != defaultModel else { return }
                            defaultModel = $0
                            defaultTouched = true
                            saved = ""
                        }
                    ),
                    placeholder: hub.t("globalDefaultPh"),
                    onError: { error = $0 }
                )
            }

            VStack(alignment: .leading, spacing: 3) {
                HubMenuPicker(
                    title: hub.t("permDefaultMode"),
                    selection: Binding(
                        get: { defaultPermMode },
                        set: {
                            guard $0 != defaultPermMode else { return }
                            defaultPermMode = $0
                            defaultPermModeTouched = true
                            saved = ""
                        }
                    ),
                    options: ["auto", "edit", "ask", "plan"].map { ($0, hub.t("permMode_\($0)")) }
                )
                Text(hub.t("permDefaultModeDesc"))
                    .font(.system(size: AppFont.caption))
                    .foregroundStyle(p.mute)
            }

            if hub.bots.isEmpty {
                Text(hub.t("noBotsInSettings")).foregroundStyle(.secondary)
            } else {
                ForEach(Array(hub.bots.enumerated()), id: \.element.id) { i, b in
                    let d = drafts[b.id] ?? BotDraft(harness: b.harness ?? "", model: origModel(b))
                    let per = (hub.settings?.defaultModels?[d.harness] ?? "").trimmingCharacters(in: .whitespaces)
                    let ph = per.isEmpty ? resolveDefaultModel(defaultModel.trimmingCharacters(in: .whitespaces), d.harness) : per
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(spacing: 10) {
                            BotAvatar(id: b.id, size: 28, index: i, avatar: b.avatar, color: b.color)
                            VStack(alignment: .leading, spacing: 0) {
                                Text(b.displayName).font(.system(size: AppFont.body, weight: .semibold))
                                Text("@\(b.id)").font(.system(size: AppFont.caption, design: .monospaced)).foregroundStyle(.secondary)
                            }
                            Spacer()
                            let botHarnessLbl = harnessLabelForSelection(d.harness, from: hub)
                            if installedHarnesses.isEmpty {
                                Text(botHarnessLbl)
                                    .foregroundStyle(p.mute)
                            } else {
                                Menu {
                                    ForEach(installedHarnesses) { h in
                                        Button {
                                            guard h.id != d.harness else { return }
                                            drafts[b.id] = BotDraft(harness: h.id, model: "")
                                            touchedBots.insert(b.id)
                                            saved = ""
                                            Task { _ = try? await hub.loadModels(h.id) }
                                        } label: {
                                            if h.id == d.harness {
                                                Label(h.title, systemImage: "checkmark")
                                            } else {
                                                Text(h.title)
                                            }
                                        }
                                    }
                                } label: {
                                    (Text(botHarnessLbl).foregroundStyle(p.text2)
                                        + Text(" ")
                                        + Text(Image(systemName: "chevron.up.chevron.down"))
                                            .font(.system(size: AppFont.caption - 1, weight: .medium))
                                            .foregroundStyle(p.mute))
                                }
                                .buttonStyle(.plain)
                                .hubBorderlessMenu()
                                .menuIndicator(.hidden)
                                .fixedSize()
                            }
                        }
                        ModelFieldRow(
                            title: hub.t("statModel"),
                            harness: d.harness,
                            text: Binding(
                                get: { d.model },
                                set: {
                                    guard $0 != d.model else { return }
                                    var n = d
                                    n.model = $0
                                    drafts[b.id] = n
                                    touchedBots.insert(b.id)
                                    saved = ""
                                }
                            ),
                            placeholder: ph.isEmpty ? hub.t("inheritDefaultPh") : ph,
                            onError: { error = $0 }
                        )
                    }
                    .padding(.vertical, 2)
                }
            }

            Button { router.sheet = .createBot } label: {
                Label(hub.t("newBot"), systemImage: "plus")
            }
        }
    }

    // MARK: Actions & Sync

    private func sync() {
        guard let s = hub.settings else { return }
        if !defaultHarnessTouched { defaultHarness = baseDefaultHarness }
        if !defaultPermModeTouched { defaultPermMode = baseDefaultPermMode }
        if !defaultTouched { defaultModel = baseDefaultModel }
        if !mpWebUrlTouched { mpWebUrl = (s.magpie?.webUrl ?? "").trimmingCharacters(in: .whitespaces) }
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

    private func registerHandlers() {
        router.settingsDirty = dirty
        router.settingsSaveAction = { await saveAll() }
        router.settingsDiscardAction = {
            defaultHarnessTouched = false
            defaultPermModeTouched = false
            defaultTouched = false
            mpWebUrlTouched = false
            touchedBots = []
            drafts = [:]
            sync()
        }
    }

    private func saveAll() async -> Bool {
        error = ""
        saved = ""
        do {
            let hasLegacyOverride = !(hub.settings?.defaultModels?[defaultHarness] ?? "").trimmingCharacters(in: .whitespaces).isEmpty
            let patch: [String: String]? = (defaultTouched || defaultHarnessTouched) && hasLegacyOverride
                ? [defaultHarness: ""]
                : nil
            try await hub.saveModelsSettings(
                changed: settingsChanged,
                defaultHarness: defaultHarness,
                defaultPermissionMode: defaultPermMode,
                defaultModel: defaultModel.trimmingCharacters(in: .whitespaces),
                defaultModelsPatch: patch,
                magpieWebUrl: mpWebUrlTouched ? mpWebUrl.trimmingCharacters(in: .whitespaces) : nil,
                botUpdates: botUpdates
            )
            defaultHarnessTouched = false
            defaultPermModeTouched = false
            defaultTouched = false
            mpWebUrlTouched = false
            touchedBots = []
            drafts = [:]
            sync()
            router.settingsDirty = false
            saved = hub.t("settingsSaved")
            return true
        } catch {
            self.error = errText(error, hub.t("saveFailed"))
            return false
        }
    }

    private func preloadAllModels(force: Bool) async {
        let ids = installedHarnesses.map(\.id)
        await withTaskGroup(of: Void.self) { group in
            for id in ids {
                group.addTask { @MainActor in
                    _ = try? await hub.loadModels(id, force: force)
                }
            }
        }
    }

    private func recheckEnvironment() {
        run("recheck", "opFailed") {
            _ = try await hub.loadSettings(fresh: true)
            hub.resetModelsCache()
            await preloadAllModels(force: true)
            return hub.t("ubRecheckDone")
        }
    }

    private func startHarnessInstall(_ id: String) {
        error = ""
        saved = ""
        pollTasks["install:\(id)"]?.cancel()
        pollTasks["install:\(id)"] = Task {
            do {
                var st = try await hub.startHarnessInstall(id)
                installTasks[id] = st
                while !Task.isCancelled && st.state == "running" {
                    try await Task.sleep(for: .milliseconds(1200))
                    if Task.isCancelled { return }
                    st = try await hub.harnessInstallStatus(id)
                    installTasks[id] = st
                }
                if Task.isCancelled { return }
                if st.state == "done" {
                    hub.resetModelsCache()
                    let fresh = try await hub.loadSettings(fresh: true)
                    await preloadAllModels(force: true)
                    let h = fresh.harnesses?.first(where: { $0.id == id })
                    saved = hub.t("engineInstallDone", ["label": h?.title ?? id])
                    if h?.requiresLogin == true && h?.authStatus == "needs_login" {
                        startHarnessLogin(id)
                    }
                } else if st.state == "error" {
                    expandedInstall.insert(id)
                }
            } catch {
                expandedInstall.insert(id)
                if !(error is CancellationError) {
                    self.error = errText(error, hub.t("opFailed"))
                }
            }
        }
    }

    private func startHarnessLogin(_ id: String) {
        error = ""
        saved = ""
        pollTasks["login:\(id)"]?.cancel()
        pollTasks["login:\(id)"] = Task {
            do {
                var st = try await hub.startHarnessLogin(id)
                loginTasks[id] = st
                if let urlStr = st.loginUrl, !urlStr.isEmpty, let u = URL(string: urlStr) {
                    openExternalURL(u)
                }
                while !Task.isCancelled && st.state == "running" && st.authStatus != "ready" {
                    try await Task.sleep(for: .milliseconds(1400))
                    if Task.isCancelled { return }
                    st = try await hub.harnessLoginStatus(id)
                    loginTasks[id] = st
                }
                if Task.isCancelled { return }
                if st.state == "done" || st.authStatus == "ready" {
                    hub.resetModelsCache()
                    _ = try? await hub.loadSettings(fresh: true)
                    await preloadAllModels(force: true)
                    saved = hub.t("engineLoginConfirmed")
                } else if st.state == "error", let msg = st.error, !msg.isEmpty {
                    error = msg
                }
            } catch {
                if !(error is CancellationError) {
                    self.error = errText(error, hub.t("opFailed"))
                }
            }
        }
    }

    private func pollHarnessLoginNow(_ id: String) {
        Task {
            if let st = try? await hub.harnessLoginStatus(id) {
                loginTasks[id] = st
                if st.state == "done" || st.authStatus == "ready" {
                    hub.resetModelsCache()
                    _ = try? await hub.loadSettings(fresh: true)
                    await preloadAllModels(force: true)
                    saved = hub.t("engineLoginConfirmed")
                }
            }
        }
    }

    private func run(_ key: String, _ fail: String, _ op: @escaping () async throws -> String?) {
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
