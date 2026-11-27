import SwiftUI

struct SettingsNetworkTab: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    @Binding var error: String
    @Binding var saved: String
    @Binding var confirm: ConfirmItem?

    @State private var tcQRData: Data?
    @State private var tcQRVersion = 0
    @State private var tcTokenIn = ""
    @State private var tcTokenVal = ""
    @State private var tcTokenShow = false

    @State private var tsQRData: Data?
    @State private var tsQRVersion = 0
    @State private var tsAdvOpen = false
    @State private var tsHostnameIn = ""
    @State private var tsAuthKeyIn = ""
    @State private var tsControlUrlIn = ""
    @State private var busy = ""

    private var hostColor: Color {
        switch hub.connState {
        case .connected: return hub.online ? p.sageInk : p.busy
        case .connecting: return p.busy
        case .unauthorized, .error: return p.danger
        case .idle: return p.mute
        }
    }

    private var hostText: String {
        if let managed = platformManagedHostLabel(hub: hub) {
            return managed
        }
        guard let s = hub.activeServer else { return hub.t("disconnected") }
        switch hub.connState {
        case .connecting: return "\(s.label) · \(hub.t("connectingShort"))"
        case .unauthorized: return "\(s.label) · \(hub.t("needsToken"))"
        case .error: return "\(s.label) · \(hub.t("connLost"))"
        case .connected: return hub.online ? s.label : "\(s.label) · \(hub.t("reconnecting"))"
        case .idle: return hub.t("disconnected")
        }
    }

    private var mobileTailscaleText: String {
        guard let t = hub.mobileTailscale else { return hub.t("tsNotBound") }
        switch t.state {
        case "running": return t.tailnet ?? hub.t("tailcatReady")
        case "starting": return hub.t("tsBinding")
        case "needs_login": return hub.t("tsNeedsLogin")
        case "disabled": return hub.t("tsNotBound")
        default: return (t.error ?? "").isEmpty ? hub.t("tailcatErr") : t.error!
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

    var body: some View {
        connectionOrManagedSection
        tailcatPairingSection
        hostTailscaleSection
        connectedNodesSection
            .task(id: hub.authToken) { await loadAll() }
            .task(id: "\(hub.tailcat?.state ?? "")|\(hub.tailcat?.link ?? "")|\(tcQRVersion)") {
                guard hub.tailcat?.state == "ready" else {
                    tcQRData = nil
                    return
                }
                tcQRData = try? await hub.fetchTailcatQR()
            }
            .task(id: "\(hub.hostTailscale?.state ?? "")|\(hub.hostTailscale?.link ?? "")|\(tsQRVersion)") {
                guard hub.hostTailscale?.state == "running" else {
                    tsQRData = nil
                    return
                }
                tsQRData = try? await hub.fetchHostTailscaleQR()
            }
            .task(id: "poll|\(hub.tailcat?.state ?? "")|\(hub.hostTailscale?.state ?? "")") {
                let tcBusy = hub.tailcat?.state == "connecting"
                let tsBusy = hub.hostTailscale?.state == "starting" || hub.hostTailscale?.state == "needs_login"
                guard tcBusy || tsBusy else { return }
                while !Task.isCancelled {
                    try? await Task.sleep(for: .milliseconds(1500))
                    if Task.isCancelled { break }
                    if tcBusy { _ = try? await hub.refreshTailcat() }
                    if tsBusy { _ = try? await hub.refreshHostTailscale() }
                }
            }
    }

    @ViewBuilder
    private var connectionOrManagedSection: some View {
        if hub.managedMode {
            PlatformDesktopHostModeSection()
        } else {
            Section(hub.t("currentMobileConn")) {
                if let s = hub.activeServer {
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
                    LabeledContent(hub.t("connAddr")) {
                        Text(s.address)
                            .font(.system(size: AppFont.secondary, design: .monospaced))
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                    HStack(spacing: 6) {
                        Tag(
                            text: hub.isTailscale ? hub.t("tailscaleE2E") : hub.isTailcat ? hub.t("tailcatE2E") : hub.t("httpLan"),
                            bg: p.lilac,
                            fg: p.lilacInk
                        )
                        Tag(
                            text: hub.online ? hub.t("streamConnected") : hub.t("reconnecting"),
                            bg: hub.online ? p.sage : p.s2,
                            fg: hub.online ? p.sageInk : .secondary
                        )
                    }
                    if hub.isClientOutdated {
                        Text(hub.t(hub.isClientUpgradeRequired ? "clientUpgradeRequired" : "clientUpdateHint"))
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(hub.isClientUpgradeRequired ? p.pinkInk : .secondary)
                    }
                } else {
                    Text(hub.t("notConnectedHost")).foregroundStyle(.secondary)
                    Button(hub.t("manageConn")) { router.sheet = .connect(scan: false) }
                }

                LabeledContent(hub.t("tsMobileStatus")) {
                    Text(mobileTailscaleText)
                        .foregroundStyle(hub.mobileTailscale?.state == "running" ? p.sageInk : .secondary)
                }

                if hub.activeServer != nil {
                    Button {
                        router.openDesktop(hub.takeovers.first?.target ?? "local")
                    } label: {
                        Label(
                            hub.takeovers.isEmpty ? hub.t("openHostDesktop") : "\(hub.t("openHostDesktop")) (\(hub.takeovers.count))",
                            systemImage: "display"
                        )
                    }
                }
            }
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
                }
                Toggle("", isOn: Binding(
                    get: { isOn },
                    set: { next in
                        if next {
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
                        } else {
                            confirm = ConfirmItem(title: hub.t("remoteDisableTitle"), message: hub.t("remoteDisableDesc")) {
                                run("tc:toggle", "opFailed") {
                                    _ = try await hub.setTailcat(["enabled": false])
                                    tcQRData = nil
                                    return nil
                                }
                            }
                        }
                    }
                ))
                .labelsHidden()
                .toggleStyle(.switch)
                .tint(p.sageInk)
                .controlSize(platformSwitchControlSize)
                .disabled(busy == "tc:toggle")
                .accessibilityLabel(hub.t("hostRemoteAccess"))
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
                }
                Toggle("", isOn: Binding(
                    get: { isOn },
                    set: { next in
                        if next {
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
                        } else {
                            run("ts:toggle", "opFailed") {
                                _ = try await hub.setHostTailscale(["enabled": false])
                                tsQRData = nil
                                return nil
                            }
                        }
                    }
                ))
                .labelsHidden()
                .toggleStyle(.switch)
                .tint(p.sageInk)
                .controlSize(platformSwitchControlSize)
                .disabled(busy == "ts:toggle")
                .accessibilityLabel(hub.t("tsHostStatus"))
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

    private var connectedNodesSection: some View {
        let permLabels = [
            "files": hub.t("permFiles"),
            "exec": hub.t("permExec"),
            "desktop": hub.t("permDesktop"),
            "agent": hub.t("permAgent"),
        ]
        return Section {
            if hub.nodes.isEmpty {
                Text(hub.t("noRemoteNodes")).foregroundStyle(.secondary)
            } else {
                ForEach(hub.nodes) { node in
                    let on = node.connected ?? false
                    let outdated = hub.serverInfo != nil && node.isOutdated
                    let perms = node.permissions ?? []
                    let harnesses = node.harnesses ?? []
                    let workspaces = node.workspaces ?? []
                    let activeCount = node.activeTaskIds?.count ?? 0
                    VStack(alignment: .leading, spacing: 6) {
                        HStack(spacing: 8) {
                            Text(node.name ?? node.id)
                                .font(.system(size: AppFont.body, weight: .semibold))
                            Spacer()
                            if activeCount > 0 {
                                Tag(text: hub.t("nodeActiveTasks", ["n": activeCount]), bg: p.sage, fg: p.sageInk)
                            }
                            if outdated {
                                Tag(text: hub.t("nodeOutdatedTag"), bg: p.pink, fg: p.pinkInk)
                            }
                            Tag(text: on ? hub.t("online") : hub.t("offline"), bg: on ? p.sage : p.s2, fg: on ? p.sageInk : .secondary)
                        }
                        Text("\(node.os ?? "")/\(node.arch ?? "")\((node.hostname ?? "").isEmpty ? "" : " · \(node.hostname!)")")
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                        if let maxMode = node.maxPermissionMode, !maxMode.isEmpty {
                            let modeKey = "permMode_\(maxMode)"
                            let modeLabel = hub.t(modeKey) == modeKey ? maxMode : hub.t(modeKey)
                            Text(hub.t("nodeMaxPermMode", ["mode": modeLabel]))
                                .font(.system(size: AppFont.caption))
                                .foregroundStyle(.secondary)
                        }
                        if outdated {
                            Text(hub.t("nodeOutdatedHint"))
                                .font(.system(size: AppFont.caption))
                                .foregroundStyle(.secondary)
                        }
                        if !perms.isEmpty || (on && perms.contains("desktop")) {
                            HStack(spacing: 6) {
                                FlowLayout(spacing: 6) {
                                    ForEach(perms, id: \.self) { perm in
                                        let isAgent = perm == "agent"
                                        Tag(
                                            text: permLabels[perm] ?? perm,
                                            bg: isAgent ? p.lilac : p.s1,
                                            fg: isAgent ? p.lilacInk : .secondary
                                        )
                                    }
                                }
                                Spacer(minLength: 4)
                                if on && perms.contains("desktop") {
                                    Button(hub.t("openDesktop")) {
                                        router.openDesktop(node.id)
                                    }
                                    .buttonStyle(.bordered)
                                    .controlSize(.small)
                                }
                            }
                        }
                        if !harnesses.isEmpty {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(hub.t("nodeHarnesses"))
                                    .font(.system(size: AppFont.caption, weight: .medium))
                                    .foregroundStyle(.secondary)
                                FlowLayout(spacing: 6) {
                                    ForEach(harnesses) { h in
                                        let installed = h.installed ?? false
                                        let ready = installed && h.authStatus == "ready"
                                        let needsLogin = installed && h.authStatus == "needs_login"
                                        let suffix = !installed
                                            ? hub.t("nodeNotInstalled")
                                            : ready ? hub.t("nodeAuthReady")
                                            : needsLogin ? hub.t("nodeAuthNeedsLogin")
                                            : ""
                                        let label = suffix.isEmpty ? h.title : "\(h.title) · \(suffix)"
                                        Tag(
                                            text: label,
                                            bg: ready ? p.sage : needsLogin ? p.pink : p.s1,
                                            fg: ready ? p.sageInk : needsLogin ? p.pinkInk : .secondary
                                        )
                                    }
                                }
                            }
                            .padding(.top, 2)
                        }
                        if !workspaces.isEmpty {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(hub.t("nodeWorkspaces"))
                                    .font(.system(size: AppFont.caption, weight: .medium))
                                    .foregroundStyle(.secondary)
                                ForEach(workspaces) { ws in
                                    HStack(spacing: 6) {
                                        Text(ws.name.isEmpty ? ws.id : ws.name)
                                            .font(.system(size: AppFont.caption, weight: .semibold))
                                            .foregroundStyle(p.text)
                                        Text(ws.path)
                                            .font(.system(size: AppFont.caption, design: .monospaced))
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                            .truncationMode(.middle)
                                        if ws.isGit == true {
                                            Tag(
                                                text: (ws.defaultBranch ?? "").isEmpty ? "git" : ws.defaultBranch!,
                                                bg: p.s1,
                                                fg: .secondary
                                            )
                                        }
                                    }
                                }
                            }
                            .padding(.top, 2)
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
        } header: {
            HStack {
                Text(hub.t("connectedNodes"))
                Spacer()
                Button {
                    Task { await loadAll() }
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
            }
        }
    }

    private func loadAll() async {
        await hub.refreshMobileTailscale(includePeers: false)
        guard hub.isConnected else { return }
        async let n: Void? = try? hub.refreshNodes()
        async let tc: Void? = try? hub.refreshTailcat()
        async let ht: Void? = try? hub.refreshHostTailscale()
        _ = await (n, tc, ht)
        if tsHostnameIn.isEmpty, let hn = hub.hostTailscale?.hostname, !hn.isEmpty { tsHostnameIn = hn }
        if tsControlUrlIn.isEmpty, let cu = hub.hostTailscale?.controlUrl, !cu.isEmpty { tsControlUrlIn = cu }
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
