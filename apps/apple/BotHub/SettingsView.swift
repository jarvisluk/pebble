import SwiftUI

struct SettingsSidebarView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    var compact = true

    var body: some View {
        if compact {
            List(SettingsTab.allCases) { tab in
                NavigationLink(value: tab) {
                    tabLabel(tab)
                }
                .listRowBackground(p.card)
            }
            .navigationDestination(for: SettingsTab.self) { tab in
                SettingsView(tab: tab)
            }
            .hubColumnList(compact: true, page: p.page)
            .hubListSectionSpacing(12)
            .contentMargins(.top, 8, for: .scrollContent)
            .navigationTitle(hub.t("settings"))
            .inlineNavTitle()
        } else {
            ScrollView {
                VStack(spacing: 4) {
                    ForEach(SettingsTab.allCases) { tab in
                        let active = (router.settingsTab ?? .general) == tab
                        Button {
                            router.settingsTab = tab
                        } label: {
                            tabLabel(tab)
                                .font(.system(size: AppFont.body, weight: active ? .semibold : .regular))
                                .foregroundStyle(active ? p.text : p.text2)
                                .padding(.vertical, 9)
                                .padding(.horizontal, 12)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(
                                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                                        .fill(active ? Color.primary.opacity(p.dark ? 0.14 : 0.08) : Color.clear)
                                )
                                .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.top, 4)
                .padding(.bottom, 12)
            }
            .safeAreaInset(edge: .top, spacing: 0) {
                HStack {
                    Button {
                        if router.settingsDirty {
                            router.requestLeaveSettings = true
                        } else {
                            router.leaveSettings()
                        }
                    } label: {
                        Image(systemName: "chevron.left")
                            .font(.system(size: AppFont.cardTitle, weight: .medium))
                            .foregroundStyle(p.text)
                            .frame(width: 28, height: 28)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .help(hub.t("backToChat"))
                    Spacer()
                    Text(hub.t("settings"))
                        .font(.system(size: AppFont.cardTitle, weight: .semibold))
                        .foregroundStyle(p.text)
                    Spacer()
                    Color.clear.frame(width: 28, height: 28)
                }
                .padding(.horizontal, 12)
                .padding(.top, 2)
                .padding(.bottom, 8)
            }
            .navigationTitle("")
            .inlineNavTitle()
        }
    }

    private func tabLabel(_ tab: SettingsTab) -> some View {
        Label(hub.t(tab.titleKey), systemImage: tab.icon)
    }
}

struct SettingsView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL

    var tab: SettingsTab = .general

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
    @State private var deskPermsError = ""

    @State private var tcQRData: Data?
    @State private var tcQRVersion: Int = 0
    @State private var tcTokenIn = ""
    @State private var tcTokenVal = ""
    @State private var tcTokenShow = false
    @State private var tsQRData: Data?
    @State private var tsQRVersion: Int = 0
    @State private var tsAdvOpen = false
    @State private var tsHostnameIn = ""
    @State private var tsAuthKeyIn = ""
    @State private var tsControlUrlIn = ""

    private var harnesses: [HarnessInfo] {
        if let h = hub.settings?.harnesses, !h.isEmpty { return h }
        return ["opencode", "codebuddy", "qoder", "codex", "claude", "grok", "trae", "pi", "dsh", "agy"].map { HarnessInfo(id: $0, label: $0) }
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
            switch tab {
            case .general:
                #if os(macOS)
                macDesktopHostSection
                #endif
                appearanceSection
                pushNotificationsSection
                openSourceSection
            case .models:
                magpieSection
                nativeCLISection
                defaultModelSection
                botModelsSection
            case .memory:
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
                    Text("_shared/USER.md").font(.system(size: AppFont.secondary, design: .monospaced))
                }
                memorySection
            case .desktop:
                desktopPermissionsSection
                remoteSection
            case .remote:
                hostSection
            }
        }
        .hubForm()
        .navigationTitle(hub.t(tab.titleKey))
        .inlineNavTitle()
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                if saving {
                    ProgressView().controlSize(.small)
                } else {
                    Button(hub.t("save")) { save() }.disabled(!dirty)
                }
            }
        }
        .alert(hub.t("unsavedTitle"), isPresented: $showUnsavedAlert) {
            Button(hub.t("saveAndLeave")) { save(closeAfter: true) }
            Button(hub.t("discardChanges"), role: .destructive) { router.leaveSettings() }
            Button(hub.t("cancel"), role: .cancel) {}
        } message: {
            Text(hub.t("unsavedDesc"))
        }
        .confirmAlert($confirm)
        .onChange(of: dirty) { _, v in router.settingsDirty = v }
        .onChange(of: router.requestLeaveSettings) { _, req in
            if req {
                router.requestLeaveSettings = false
                showUnsavedAlert = true
            }
        }
        .task(id: "\(tab.rawValue)|\(hub.authToken)") {
            guard !hub.baseURL.isEmpty else { return }
            error = ""
            do {
                _ = try await hub.loadSettings()
                try await hub.refreshBots()
            } catch {
                self.error = errText(error, hub.t("opFailed"))
            }
            if tab == .memory { await loadMem("") }
            if tab == .desktop { await loadPerms() }
            if tab == .remote {
                do {
                    async let n: Void = hub.refreshNodes()
                    async let tc: Void = hub.refreshTailcat()
                    async let ht: Void = hub.refreshHostTailscale()
                    _ = try await (n, tc, ht)
                } catch {
                    self.error = errText(error, hub.t("opFailed"))
                }
                if tsHostnameIn.isEmpty, let hn = hub.hostTailscale?.hostname, !hn.isEmpty { tsHostnameIn = hn }
                if tsControlUrlIn.isEmpty, let cu = hub.hostTailscale?.controlUrl, !cu.isEmpty { tsControlUrlIn = cu }
            }
        }
        .task(id: "\(tab == .models)|\(hub.isConnected)|\(hub.modelsGeneration)|\(harnesses.map(\.id).joined(separator: ","))") {
            guard tab == .models, hub.isConnected else { return }
            await preloadAllModels()
        }
        .task(id: "\(tab == .remote)|\(hub.tailcat?.state ?? "")|\(hub.tailcat?.link ?? "")|\(tcQRVersion)") {
            guard tab == .remote, hub.tailcat?.state == "ready" else {
                tcQRData = nil
                return
            }
            tcQRData = try? await hub.fetchTailcatQR()
        }
        .task(id: "\(tab == .remote)|\(hub.hostTailscale?.state ?? "")|\(hub.hostTailscale?.link ?? "")|\(tsQRVersion)") {
            guard tab == .remote, hub.hostTailscale?.state == "running" else {
                tsQRData = nil
                return
            }
            tsQRData = try? await hub.fetchHostTailscaleQR()
        }
        .task(id: "poll|\(tab == .remote)|\(hub.tailcat?.state ?? "")|\(hub.hostTailscale?.state ?? "")") {
            let tcBusy = hub.tailcat?.state == "connecting"
            let tsBusy = hub.hostTailscale?.state == "starting" || hub.hostTailscale?.state == "needs_login"
            guard tab == .remote, tcBusy || tsBusy else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(1500))
                if Task.isCancelled { break }
                if tcBusy { _ = try? await hub.refreshTailcat() }
                if tsBusy { _ = try? await hub.refreshHostTailscale() }
            }
        }
        .onAppear {
            sync()
            router.settingsDirty = dirty
        }
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
                if closeAfter { router.leaveSettings() }
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
        #if os(macOS)
        if hub.managedMode {
            let label = DesktopHostManager.shared.snapshot.mode == "remote"
                ? hub.t("macModeRemoteTag")
                : hub.t("macModeLocalTag")
            switch hub.connState {
            case .connecting: return "\(label) · \(hub.t("connectingShort"))"
            case .unauthorized: return "\(label) · \(hub.t("needsToken"))"
            case .error: return "\(label) · \(hub.t("connLost"))"
            case .connected: return hub.online ? label : "\(label) · \(hub.t("reconnecting"))"
            case .idle: return hub.t("disconnected")
            }
        }
        #endif
        guard let s = hub.activeServer else { return hub.t("disconnected") }
        switch hub.connState {
        case .connecting: return "\(s.label) · \(hub.t("connectingShort"))"
        case .unauthorized: return "\(s.label) · \(hub.t("needsToken"))"
        case .error: return "\(s.label) · \(hub.t("connLost"))"
        case .connected: return hub.online ? s.label : "\(s.label) · \(hub.t("reconnecting"))"
        case .idle: return hub.t("disconnected")
        }
    }

    private var tailcatStatusView: (text: String, color: Color) {
        guard let t = hub.tailcat else { return (hub.t("tailcatUnknown"), .secondary) }
        switch t.state {
        case "ready": return (hub.t("tailcatReady"), p.sageInk)
        case "connecting": return (hub.t("tailcatConnecting"), p.busy)
        case "disabled": return (hub.t("tailcatDisabled"), .secondary)
        default: return ((t.error ?? "").isEmpty ? hub.t("tailcatErr") : t.error!, p.danger)
        }
    }

    private var hostTailscaleStatusView: (text: String, color: Color) {
        guard let t = hub.hostTailscale else { return (hub.t("tailcatUnknown"), .secondary) }
        switch t.state {
        case "running": return (t.tailnet ?? hub.t("tailcatReady"), p.sageInk)
        case "starting": return (hub.t("tsBinding"), p.busy)
        case "needs_login": return (hub.t("tsNeedsLogin"), p.busy)
        case "disabled": return (hub.t("tailcatDisabled"), .secondary)
        default: return ((t.error ?? "").isEmpty ? hub.t("tailcatErr") : t.error!, p.danger)
        }
    }

    @ViewBuilder
    private var hostSection: some View {
        if !hub.managedMode {
            Section(hub.t("hostSection")) {
                Button {
                    router.sheet = .connect(scan: false)
                } label: {
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

        tailcatPairingSection
        hostTailscaleSection

        Section(hub.t("connectedNodes")) {
            if hub.nodes.isEmpty {
                Text(hub.t("noRemoteNodes")).foregroundStyle(.secondary)
            } else {
                ForEach(hub.nodes) { node in
                    let on = node.connected ?? false
                    let outdated = hub.serverInfo != nil && node.isOutdated
                    HStack(spacing: 8) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(node.name ?? node.id).font(.system(size: AppFont.body, weight: .semibold))
                            Text("\(node.os ?? "")/\(node.arch ?? "")\((node.hostname ?? "").isEmpty ? "" : " · \(node.hostname!)")")
                                .font(.system(size: AppFont.caption, design: .monospaced))
                                .foregroundStyle(.secondary)
                            if outdated {
                                Text(hub.t("nodeOutdatedHint"))
                                    .font(.system(size: AppFont.caption))
                                    .foregroundStyle(.secondary)
                            }
                        }
                        Spacer()
                        if outdated {
                            Tag(text: hub.t("nodeOutdatedTag"), bg: p.pink, fg: p.pinkInk)
                        }
                        Tag(text: on ? hub.t("online") : hub.t("offline"), bg: on ? p.sage : p.s2, fg: on ? p.sageInk : .secondary)
                    }
                }
            }
            if hub.managedMode {
                Button { router.sheet = .devices } label: {
                    let n = hub.nodes.filter { $0.connected == true }.count
                    Label(n > 0 ? "\(hub.t("devicesTunnel")) (\(n))" : hub.t("devicesTunnel"), systemImage: "globe")
                }
            }
        }
    }

    private var tcFooterText: String {
        let st = hub.tailcat?.state ?? "disabled"
        switch st {
        case "ready": return hub.t("remoteHelpReady")
        case "connecting": return hub.t("remoteHelpConnecting")
        case "error": return (hub.tailcat?.error ?? "").isEmpty ? hub.t("tailcatErr") : hub.tailcat!.error!
        default: return hub.t("remoteHelpOff")
        }
    }

    private var tsFooterText: String {
        let st = hub.hostTailscale?.state ?? "disabled"
        switch st {
        case "running": return hub.t("tsHelpReady", ["tailnet": hub.hostTailscale?.tailnet ?? "Tailscale"])
        case "needs_login": return hub.t("tsHelpLogin")
        case "starting": return hub.t("tsHelpStarting")
        case "error": return (hub.hostTailscale?.error ?? "").isEmpty ? hub.t("tailcatErr") : hub.hostTailscale!.error!
        default: return hub.t("tsHelpOff")
        }
    }

    @ViewBuilder
    private func qrCardView(_ data: Data?) -> some View {
        HStack {
            Spacer()
            ZStack {
                if let data, let img = PlatformImage(data: data) {
                    Image(platform: img)
                        .interpolation(.none)
                        .resizable()
                        .scaledToFit()
                } else {
                    ProgressView()
                }
            }
            .frame(width: 164, height: 164)
            .padding(10)
            .background(Color.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            Spacer()
        }
        .padding(.vertical, 4)
    }

    private var tailcatPairingSection: some View {
        let tc = tailcatStatusView
        let state = hub.tailcat?.state ?? "disabled"
        let isOn = state == "ready" || state == "connecting"
        let hasTok = hub.tailcat?.hasToken == true
        let envRef = hub.tailcat?.tokenEnvRef ?? ""

        return Section {
            HStack(spacing: 8) {
                Text(hub.t("tunnelState"))
                Text("·").foregroundStyle(.secondary)
                Text(tc.text).foregroundStyle(tc.color)
                Spacer()
                if busy == "tc:toggle" {
                    ProgressView().controlSize(.small)
                } else if isOn {
                    Button(hub.t("remoteStop"), role: .destructive) {
                        confirm = ConfirmItem(title: hub.t("remoteDisableTitle"), message: hub.t("remoteDisableDesc")) {
                            run("tc:toggle", "opFailed") {
                                _ = try await hub.setTailcat(["enabled": false])
                                tcQRData = nil
                                return nil
                            }
                        }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                } else {
                    Button(state == "error" ? hub.t("remoteRetry") : hub.t("remoteEnable")) {
                        run("tc:toggle", "opFailed") {
                            var body: [String: Any] = ["enabled": true]
                            let tok = tcTokenIn.trimmingCharacters(in: .whitespacesAndNewlines)
                            if !tok.isEmpty { body["apiToken"] = tok }
                            let res = try await hub.setTailcat(body)
                            tcTokenIn = ""
                            if let issued = res.apiToken, !issued.isEmpty {
                                tcTokenVal = issued
                                tcTokenShow = true
                            }
                            tcQRVersion += 1
                            return nil
                        }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                }
            }

            if state == "ready" {
                qrCardView(tcQRData)
                if let link = hub.tailcat?.link, !link.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(link)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(p.text2)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        HStack {
                            Spacer()
                            Button(hub.t("copyPairLink")) {
                                copyToClipboard(link)
                                saved = hub.t("pairLinkCopied")
                            }
                            .buttonStyle(HubPrimaryButtonStyle())
                        }
                    }
                }
            }

            if !isOn && !hasTok {
                TextField(hub.t("accessTokenOptional"), text: $tcTokenIn, prompt: Text(hub.t("accessTokenPh")))
                    .monoInput()
            }

            if hasTok {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 6) {
                        Text(envRef.isEmpty ? hub.t("accessTokenReusable") : hub.t("accessTokenEnv"))
                        Spacer()
                        if envRef.isEmpty {
                            Button(tcTokenShow ? hub.t("hide") : hub.t("show")) {
                                if tcTokenShow {
                                    tcTokenShow = false
                                } else if !tcTokenVal.isEmpty {
                                    tcTokenShow = true
                                } else {
                                    run("tc:reveal", "opFailed") {
                                        tcTokenVal = try await hub.revealTailcatToken()
                                        tcTokenShow = true
                                        return nil
                                    }
                                }
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .disabled(busy == "tc:reveal")

                            Button(hub.t("copy")) {
                                run("tc:copy", "opFailed") {
                                    let tok = tcTokenVal.isEmpty ? try await hub.revealTailcatToken() : tcTokenVal
                                    tcTokenVal = tok
                                    if !tok.isEmpty {
                                        copyToClipboard(tok)
                                        return hub.t("tokenCopied")
                                    }
                                    return nil
                                }
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .disabled(busy == "tc:copy")

                            Button(hub.t("resetToken"), role: .destructive) {
                                confirm = ConfirmItem(title: hub.t("resetTokenTitle"), message: hub.t("resetTokenDesc")) {
                                    run("tc:reset", "opFailed") {
                                        let res = try await hub.setTailcat(["resetToken": true])
                                        if let issued = res.apiToken, !issued.isEmpty {
                                            tcTokenVal = issued
                                            tcTokenShow = true
                                        }
                                        tcQRVersion += 1
                                        return hub.t("tokenResetToast")
                                    }
                                }
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .disabled(busy == "tc:reset")
                        }
                    }

                    if !envRef.isEmpty {
                        Text(hub.t("accessTokenEnvHint", ["ref": envRef]))
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                    } else {
                        Text(tcTokenShow && !tcTokenVal.isEmpty ? tcTokenVal : String(repeating: "•", count: 24))
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(p.text2)
                            .textSelection(.enabled)
                    }
                }
            }
        } header: {
            Text(hub.t("hostRemoteAccess"))
        } footer: {
            Text(tcFooterText)
        }
    }

    private var hostTailscaleSection: some View {
        let ts = hostTailscaleStatusView
        let state = hub.hostTailscale?.state ?? "disabled"
        let isOn = state == "running" || state == "starting" || state == "needs_login"
        let authEnv = hub.hostTailscale?.authKeyEnvRef ?? ""

        return Section {
            HStack(spacing: 8) {
                Text(hub.t("tsNetwork"))
                Text("·").foregroundStyle(.secondary)
                Text(ts.text).foregroundStyle(ts.color)
                Spacer()
                if busy == "ts:toggle" {
                    ProgressView().controlSize(.small)
                } else if isOn {
                    Button(hub.t("tsDisable"), role: .destructive) {
                        run("ts:toggle", "opFailed") {
                            _ = try await hub.setHostTailscale(["enabled": false])
                            tsQRData = nil
                            return nil
                        }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                } else {
                    Button(state == "error" ? hub.t("remoteRetry") : hub.t("tsBind")) {
                        run("ts:toggle", "opFailed") {
                            var body: [String: Any] = ["enabled": true]
                            let hn = tsHostnameIn.trimmingCharacters(in: .whitespacesAndNewlines)
                            let cu = tsControlUrlIn.trimmingCharacters(in: .whitespacesAndNewlines)
                            let ak = tsAuthKeyIn.trimmingCharacters(in: .whitespacesAndNewlines)
                            let tok = tcTokenIn.trimmingCharacters(in: .whitespacesAndNewlines)
                            if !hn.isEmpty { body["hostname"] = hn }
                            if !cu.isEmpty { body["controlUrl"] = cu }
                            if !ak.isEmpty { body["authKey"] = ak }
                            if !tok.isEmpty { body["apiToken"] = tok }
                            let res = try await hub.setHostTailscale(body)
                            tsAuthKeyIn = ""
                            if let issued = res.apiToken, !issued.isEmpty {
                                tcTokenVal = issued
                                tcTokenShow = true
                            }
                            tsQRVersion += 1
                            return nil
                        }
                    }
                    .buttonStyle(HubPrimaryButtonStyle())
                }
            }

            if state == "needs_login", let auth = hub.hostTailscale?.authUrl, !auth.isEmpty {
                HStack(spacing: 8) {
                    if let url = URL(string: auth) {
                        Button(hub.t("tsOpenLogin")) { openExternalURL(url) }
                            .buttonStyle(HubPrimaryButtonStyle())
                    }
                    Button(hub.t("tsCopyLoginUrl")) {
                        copyToClipboard(auth)
                        saved = hub.t("tsLoginUrlCopied")
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }

            if state == "running" {
                qrCardView(tsQRData)
                let hostDesc = hub.hostTailscale?.dnsName?.isEmpty == false ? hub.hostTailscale?.dnsName : hub.hostTailscale?.ip
                if let hostDesc, !hostDesc.isEmpty {
                    let suffix = (hub.hostTailscale?.dnsName?.isEmpty == false && !(hub.hostTailscale?.ip ?? "").isEmpty) ? " (\(hub.hostTailscale!.ip!))" : ""
                    Text(hostDesc + suffix)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
                if let link = hub.hostTailscale?.link, !link.isEmpty {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(link)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(p.text2)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        HStack(spacing: 8) {
                            Spacer()
                            Button(hub.t("tsLogout"), role: .destructive) {
                                confirm = ConfirmItem(title: hub.t("tsLogoutTitle"), message: hub.t("tsLogoutDesc")) {
                                    run("ts:logout", "opFailed") {
                                        _ = try await hub.setHostTailscale(["logout": true])
                                        tsQRData = nil
                                        return nil
                                    }
                                }
                            }
                            .buttonStyle(.bordered)
                            .controlSize(.small)
                            .disabled(busy == "ts:logout")

                            Button(hub.t("tsCopyLink")) {
                                copyToClipboard(link)
                                saved = hub.t("tsAddressCopied")
                            }
                            .buttonStyle(HubPrimaryButtonStyle())
                        }
                    }
                }
            }

            DisclosureGroup(hub.t("tsAdvanced"), isExpanded: $tsAdvOpen) {
                HubFormField(label: hub.t("tsHostname")) {
                    TextField("", text: $tsHostnameIn, prompt: Text(hub.t("tsHostnamePh")))
                        .monoInput()
                }
                HubFormField(label: "Auth Key") {
                    SecureField("", text: $tsAuthKeyIn, prompt: Text(hub.t("tsAuthKeyPh")))
                        .monoInput()
                        .disabled(!authEnv.isEmpty)
                }
                HubFormField(label: "Control URL") {
                    TextField("", text: $tsControlUrlIn, prompt: Text(hub.t("tsControlUrlPh")))
                        .monoInput()
                }
            }
        } header: {
            Text(hub.t("tsHostStatus"))
        } footer: {
            Text(tsFooterText)
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
                    Button {
                        run("magpie:install", "opFailed") {
                            try await hub.installMagpie()
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
                    .disabled(busy != nil)
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
                    let addr = hub.activeServer?.address ?? hub.baseURL
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
                    Text(h.title).font(.system(size: AppFont.body, weight: .semibold))
                    Text(connected ? "Magpie · \((h.magpie?.model ?? "").isEmpty ? hub.t("mpConnected") : h.magpie!.model!)"
                         : hub.t("mpNative") + ((h.magpie?.nativeModel ?? "").isEmpty ? "" : " · \(h.magpie!.nativeModel!)"))
                        .font(.system(size: AppFont.secondary)).foregroundStyle(connected ? p.sageInk : .secondary).lineLimit(1)
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
        let key = "magpie:\(h.id)"
        return VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(h.title).font(.system(size: AppFont.body, weight: .semibold))
                    Text(hub.t("mpNative") + ((h.magpie?.nativeModel ?? "").isEmpty ? "" : " · \(h.magpie!.nativeModel!)"))
                        .font(.system(size: AppFont.secondary)).foregroundStyle(.secondary).lineLimit(1)
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
                modelMenu(for: h.id, current: perHarness[h.id] ?? "") { m in
                    guard m != (perHarness[h.id] ?? "") else { return }
                    perHarness[h.id] = m; perTouched = true; saved = ""
                }
            }
        }
        .padding(.vertical, 2)
    }

    // MARK: 外观

    private var appearanceSection: some View {
        @Bindable var h = hub
        return Section(hub.t("appearance")) {
            HubSegmentedPicker(
                selection: $h.themePref,
                options: [
                    ("system", hub.t("followSystem")),
                    ("light", hub.t("themeLight")),
                    ("dark", hub.t("themeDark")),
                ]
            )
            HubMenuPicker(
                title: hub.t("language"),
                selection: $h.langPref,
                options: [
                    ("system", hub.t("followSystem")),
                    ("zh-CN", "简体中文"),
                    ("en", "English"),
                ]
            )
        }
    }

    private var pushNotificationsSection: some View {
        let pm = PushManager.shared
        #if os(iOS)
        let la = LiveActivityManager.shared
        let title = hub.t("pushSection")
        #else
        let title = hub.t("pushSectionMac")
        #endif
        return Section(title) {
            HStack {
                Text(hub.t("pushSystemPerm"))
                Spacer()
                switch pm.authStatus {
                case .authorized, .provisional, .ephemeral:
                    Text(hub.t("pushGranted")).foregroundStyle(p.sageInk)
                case .denied:
                    Button(hub.t("pushOpenSettings")) {
                        if let u = platformNotificationSettingsURL { openURL(u) }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                default:
                    Button(hub.t("pushEnable")) {
                        Task { await pm.requestPermissionAndRegister() }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }
            #if os(iOS)
            HStack {
                Text(hub.t("pushLiveActivity"))
                Spacer()
                Text(la.activitiesEnabled ? hub.t("pushEnabled") : hub.t("pushDisabled"))
                    .foregroundStyle(la.activitiesEnabled ? Color.green : .secondary)
            }
            HStack {
                Text(hub.t("pushServerChannel"))
                Spacer()
                Text(pushModeLabel(pm.serverStatus.mode))
                    .foregroundStyle(pm.serverStatus.configured ? Color.green : .secondary)
            }
            #endif
        }
    }

    #if os(macOS)
    private var macDesktopHostSection: some View {
        let host = DesktopHostManager.shared
        let snap = host.snapshot
        return Section(hub.t("macHostSection")) {
            LabeledContent(hub.t("macCurrentMode")) {
                HStack(spacing: 6) {
                    if host.daemonOutdated {
                        Tag(text: hub.t("nodeOutdatedTag"), bg: p.pink, fg: p.pinkInk)
                    }
                    Text(snap.mode == "remote" ? hub.t("macModeRemoteTag") : hub.t("macModeLocalTag"))
                        .foregroundStyle(.secondary)
                }
            }
            if !snap.dataDir.isEmpty {
                LabeledContent(hub.t("macDataDir")) {
                    Text(snap.dataDir)
                        .font(.system(size: AppFont.caption, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            if snap.mode == "remote" {
                Toggle(hub.t("macAllowNode"), isOn: Binding(
                    get: { snap.allowNode },
                    set: { next in Task { await host.setLocalNode(enabled: next) } }
                ))
                if snap.allowNode {
                    LabeledContent(hub.t("macNodeStatus")) {
                        let statusText = snap.nodeConnected ? hub.t("macNodeConnected")
                            : snap.nodeStatus == "waiting_auth" ? hub.t("macNodeWaiting")
                            : hub.t("macNodeDisconnected")
                        Text(statusText)
                            .foregroundStyle(snap.nodeConnected ? p.sageInk : .secondary)
                    }
                }
                if snap.canFailover {
                    Button(hub.t("macFailover")) {
                        Task { await host.failoverToLocal() }
                    }
                }
            } else if snap.canFailback {
                Button(hub.t("macFailback")) {
                    Task { await host.failbackToRemote() }
                }
            }
            Button(hub.t("macOpenDataDir")) {
                host.openDataDir()
            }
            Button(hub.t("macSwitchMode")) {
                Task { await host.resetMode() }
            }
        }
    }
    #endif

    private func pushModeLabel(_ mode: String) -> String {
        switch mode {
        case "direct": return hub.t("pushModeDirect")
        case "relay": return hub.t("pushModeRelay")
        default: return hub.t("pushModeDisabled")
        }
    }

    private var openSourceSection: some View {
        Section(hub.t("openSourceTitle")) {
            Text(hub.t("openSourceDesc"))
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(p.text2)
            Link(destination: URL(string: "https://github.com/jarvisluk/bot-hub")!) {
                Label(hub.t("openSourceLinkBtn"), systemImage: "star")
                    .foregroundStyle(p.text)
            }
            .foregroundStyle(p.text)
        }
    }
    // MARK: 模型

    private var orderedGlobalHarnesses: [HarnessInfo] {
        var seen: Set<String> = []
        var out: [HarnessInfo] = []
        let anyInstalled = harnesses.contains { $0.installed == true }
        func appendIfNeeded(_ h: HarnessInfo) {
            guard seen.insert(h.id).inserted else { return }
            out.append(h)
        }
        if let def = harnesses.first(where: { $0.id == defaultHarness }) {
            appendIfNeeded(def)
        }
        for h in harnesses where h.installed == true && h.magpie?.connected != true {
            appendIfNeeded(h)
        }
        for h in harnesses where h.installed == true && h.magpie?.connected == true {
            appendIfNeeded(h)
        }
        if !anyInstalled {
            for h in harnesses {
                appendIfNeeded(h)
            }
        } else {
            for h in harnesses where !(hub.models[h.id] ?? []).isEmpty {
                appendIfNeeded(h)
            }
        }
        return out
    }

    private func harnessFieldLabel(_ h: HarnessInfo) -> String {
        if h.magpie?.connected == true {
            return "\(h.title) · Magpie"
        }
        if hub.settings?.magpie?.installed == true {
            return "\(h.title) · \(hub.t("mpNative"))"
        }
        return h.title
    }

    @ViewBuilder
    private func modelOptionsContent(for harness: String, current: String, onSelect: @escaping (String) -> Void) -> some View {
        let cands = hub.models[harness] ?? []
        let trimmedCurrent = current.trimmingCharacters(in: .whitespaces)
        if cands.isEmpty {
            if loadingModelsFor.contains(harness) {
                Text(hub.t("loading"))
            } else {
                Text(hub.t("noModels"))
                Button {
                    loadCands(for: harness, force: true)
                } label: {
                    Label(hub.t("refresh"), systemImage: "arrow.clockwise")
                }
            }
        } else {
            ForEach(cands, id: \.self) { m in
                Button {
                    onSelect(m)
                } label: {
                    if m == trimmedCurrent {
                        Label(m, systemImage: "checkmark")
                    } else {
                        Text(m)
                    }
                }
            }
            Divider()
            Button {
                loadCands(for: harness, force: true)
            } label: {
                Label(hub.t("refresh"), systemImage: "arrow.clockwise")
            }
        }
    }

    @ViewBuilder
    private func modelMenu(for harness: String, current: String, onSelect: @escaping (String) -> Void) -> some View {
        let cands = hub.models[harness] ?? []
        Menu {
            if !current.trimmingCharacters(in: .whitespaces).isEmpty {
                Button(hub.t("clear")) { onSelect("") }
                Divider()
            }
            modelOptionsContent(for: harness, current: current, onSelect: onSelect)
        } label: {
            Label(hub.t("optionalModels"), systemImage: "list.bullet").labelStyle(.iconOnly)
        }
        .buttonStyle(.plain)
        .hubBorderlessMenu()
        .menuIndicator(.hidden)
        .fixedSize()
        .task(id: "\(harness)|\(hub.isConnected)|\(hub.modelsGeneration)") {
            if cands.isEmpty, hub.isConnected, !harness.isEmpty {
                await fetchCands(for: harness)
            }
        }
    }

    @ViewBuilder
    private func globalModelMenu(current: String, onSelect: @escaping (String) -> Void) -> some View {
        let targets = orderedGlobalHarnesses
        let trimmedCurrent = current.trimmingCharacters(in: .whitespaces)
        Menu {
            if !trimmedCurrent.isEmpty {
                Button(hub.t("clear")) { onSelect("") }
                Divider()
            }
            if targets.count == 1, let only = targets.first {
                modelOptionsContent(for: only.id, current: current, onSelect: onSelect)
            } else {
                ForEach(targets) { h in
                    let label = h.id == defaultHarness
                        ? "\(harnessFieldLabel(h)) (\(hub.t("defaultTag")))"
                        : harnessFieldLabel(h)
                    Menu(label) {
                        modelOptionsContent(for: h.id, current: current, onSelect: onSelect)
                    }
                }
                Divider()
                Button {
                    Task { await preloadAllModels(force: true) }
                } label: {
                    Label(hub.t("refresh"), systemImage: "arrow.clockwise")
                }
            }
        } label: {
            Label(hub.t("optionalModels"), systemImage: "list.bullet").labelStyle(.iconOnly)
        }
        .buttonStyle(.plain)
        .hubBorderlessMenu()
        .menuIndicator(.hidden)
        .fixedSize()
    }

    private var defaultModelSection: some View {
        Section(hub.t("globalDefaultModel")) {
            HubMenuPicker(
                title: hub.t("defaultHarness"),
                selection: Binding(
                    get: { defaultHarness },
                    set: {
                        guard $0 != defaultHarness else { return }
                        defaultHarness = $0; defaultHarnessTouched = true; saved = ""
                        if (hub.models[$0] ?? []).isEmpty, hub.isConnected {
                            loadCands(for: $0)
                        }
                    }
                ),
                options: harnesses.map { ($0.id, $0.title) }
            )
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
                globalModelMenu(current: defaultModel) { m in
                    guard m != defaultModel else { return }
                    defaultModel = m; defaultTouched = true; saved = ""
                }
            }
            DisclosureGroup(hub.t("perHarnessTitle"), isExpanded: $perOpen) {
                VStack(alignment: .leading, spacing: 12) {
                    ForEach(harnesses) { h in
                        HubFormField(label: harnessFieldLabel(h)) {
                            HStack(spacing: 8) {
                                TextField("", text: Binding(
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
                .padding(.top, 4)
            }
        }
    }

    private func fetchCands(for harness: String, force: Bool = false, showError: Bool = false) async {
        guard !harness.isEmpty else { return }
        if !force, let existing = hub.models[harness], !existing.isEmpty { return }
        guard force || !loadingModelsFor.contains(harness) else { return }
        loadingModelsFor.insert(harness)
        defer { loadingModelsFor.remove(harness) }
        do {
            _ = try await hub.loadModels(harness, force: force)
        } catch {
            if showError {
                self.error = errText(error, hub.t("loadModelsFailed"))
            }
        }
    }

    private func preloadAllModels(force: Bool = false) async {
        let ids = harnesses.map(\.id)
        await withTaskGroup(of: Void.self) { group in
            for id in ids {
                group.addTask { @MainActor in
                    await fetchCands(for: id, force: force, showError: false)
                }
            }
        }
    }

    private func loadCands(for harness: String? = nil, force: Bool = false, showError: Bool? = nil) {
        let target = harness ?? primaryHarness
        guard !target.isEmpty else { return }
        let reportErr = showError ?? force
        Task { await fetchCands(for: target, force: force, showError: reportErr) }
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
                            Text(b.displayName).font(.system(size: AppFont.body, weight: .semibold))
                            Text("@\(b.id)").font(.system(size: AppFont.caption, design: .monospaced)).foregroundStyle(.secondary)
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
            Button { router.sheet = .createBot } label: {
                Label(hub.t("newBot"), systemImage: "plus")
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
                        .buttonStyle(.bordered)
                        .controlSize(.small)
                    }
                }
                if sem.active != true {
                    Text(hub.t("memSemanticHintOff"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(.secondary)
                }
            }
            HubSegmentedPicker(
                selection: Binding(
                    get: { memScope },
                    set: { next in
                        memScope = next
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
                options: [("", hub.t("memKindAll"))] + ["fact", "preference", "rule", "reference", "profile", "procedure", "episode"].map { ($0, $0) }
            )
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
                Button {
                    run("magpie:install", "opFailed") {
                        try await hub.installMagpie()
                        await loadMem(memQuery)
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
                .disabled(busy != nil)
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
                HubSegmentedPicker(
                    selection: $memKind,
                    options: ["fact", "preference", "rule", "reference"].map { ($0, $0) }
                )
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
        deskPermsError = ""
        defer { deskPermsLoading = false }
        do {
            let resp = try await hub.fetchDesktopPermissions("local")
            deskPerms = resp.items ?? []
        } catch {
            deskPermsError = errText(error, hub.t("opFailed"))
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
            if !deskPermsError.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    StatusText(text: deskPermsError)
                    Button(hub.t("retry")) {
                        Task { await loadPerms() }
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }
            if deskPerms.isEmpty {
                if deskPermsLoading {
                    ProgressView()
                } else if deskPermsError.isEmpty {
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
                            Text(permText("deskPermName_\(item.id)", item.name)).font(.system(size: AppFont.body, weight: .semibold))
                            Spacer()
                            Tag(text: stLabel, bg: bg, fg: fg)
                        }
                        Text(permText("deskPermDesc_\(item.id)", item.description)).font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
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
                .buttonStyle(.borderless)
                .disabled(deskPermsLoading)
            }
        } footer: {
            Text(hub.t("deskPermsDesc"))
        }
    }

    private var remoteSection: some View {
        Section(hub.t("deskWorkerTitle")) {
            HubMenuPicker(
                title: hub.t("deskWorkerHarnessLabel"),
                selection: Binding(get: { deskHarness }, set: { deskHarness = $0; deskTouched = true }),
                options: [("", hub.t("deskWorkerFollowBot"))] + (hub.settings?.harnesses ?? []).map { ($0.id, $0.title) }
            )
            HStack {
                TextField(hub.t("deskWorkerModelLabel"), text: Binding(get: { deskModel }, set: { deskModel = $0; deskTouched = true }),
                          prompt: Text(hub.t("deskWorkerModelPh")))
                    .monoInput()
                    .labelsHidden()
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
