import SwiftUI

private let desktopDownloadURL = URL(string: "https://github.com/pebble-ai/bot-hub/releases/latest")!

struct ConnectView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL
    var startScan: Bool
    var isGate = false

    @State private var scanning = false
    @State private var label = ""
    @State private var address = ""
    @State private var token = ""
    @State private var standbyAddress = ""
    @State private var standbyToken = ""
    @State private var err = ""
    @State private var infoHint = ""
    @State private var submitting = false
    @State private var showFormWhileConnecting = false
    @State private var confirm: ConfirmItem?
    @State private var tsAdvOpen = false
    @State private var tsAuthKey = ""
    @State private var tsControlUrl = ""
    @State private var manualOpen = false
    @State private var manualAdvOpen = false

    private var isFreshOnboarding: Bool {
        isGate && hub.servers.isEmpty && hub.activeServer == nil
    }

    private var showFreshHero: Bool {
        isFreshOnboarding && !manualOpen
    }

    private var isConnectingGate: Bool {
        isGate && hub.activeServer != nil && hub.connState == .connecting && hub.bots.isEmpty && !showFormWhileConnecting && !submitting
    }

    var body: some View {
        ZStack {
            if isConnectingGate {
                VStack(spacing: 14) {
                    ProgressView(hub.t("connectingHost"))
                    Button(hub.t("switchHost")) { showFormWhileConnecting = true }
                        .buttonStyle(.bordered)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(p.page)
                .transition(.opacity)
            } else if showFreshHero {
                freshHeroContent
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(p.page)
                    .transition(.asymmetric(
                        insertion: .move(edge: .leading).combined(with: .opacity),
                        removal: .move(edge: .leading).combined(with: .opacity)
                    ))
            } else {
                let inner = formContent
                    .frame(maxWidth: isGate ? 560 : .infinity)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(p.page)
                if isGate {
                    HubSheetScaffold(
                        title: "Pebble",
                        closeTitle: hub.t("backBtn"),
                        onClose: (isFreshOnboarding && manualOpen) ? { withAnimation { manualOpen = false } } : nil,
                        enableCancelShortcut: false,
                        actionTitle: hub.t("connect"),
                        loading: submitting,
                        onAction: { submit() }
                    ) {
                        inner
                    }
                    .transition(.asymmetric(
                        insertion: .move(edge: .trailing).combined(with: .opacity),
                        removal: .move(edge: .trailing).combined(with: .opacity)
                    ))
                } else {
                    HubSheetScaffold(
                        title: hub.t("connectHost"),
                        onClose: { router.sheet = nil },
                        actionTitle: hub.t("connect"),
                        loading: submitting,
                        onAction: { submit() }
                    ) {
                        inner
                    }
                    .transition(.asymmetric(
                        insertion: .move(edge: .trailing).combined(with: .opacity),
                        removal: .move(edge: .trailing).combined(with: .opacity)
                    ))
                }
            }
        }
        .animation(.spring(response: 0.34, dampingFraction: 0.88), value: showFreshHero)
        .animation(.easeOut(duration: 0.22), value: isConnectingGate)
        .confirmAlert($confirm)
        .onAppear {
            if let s = hub.activeServer, hub.connState == .unauthorized || hub.connState == .error {
                if label.isEmpty { label = s.label }
                if address.isEmpty { address = s.address }
            }
            if tsControlUrl.isEmpty { tsControlUrl = hub.tsControlUrl }
            if hub.tsBound && hub.mobileTailscale == nil {
                Task { await hub.refreshMobileTailscale(includePeers: true) }
            }
            if platformSupportsQRScanner && startScan { scanning = true }
        }
        .onChange(of: hub.connState) { _, st in
            if st == .connected {
                submitting = false
                showFormWhileConnecting = false
                if !isGate { router.sheet = nil }
                return
            }
            guard submitting else { return }
            if st == .error || st == .unauthorized {
                submitting = false
                err = hub.connError.isEmpty ? hub.t("connFailed") : hub.connError
            }
        }
    }

    private var freshHeroContent: some View {
        GeometryReader { geo in
            ScrollView {
                VStack(spacing: 0) {
                    Spacer(minLength: 24)

                    VStack(spacing: 16) {
                        BotAvatar(id: "assistant", size: scanning ? 72 : 104, color: pebbleColor, busy: submitting)
                            .animation(.spring(response: 0.32, dampingFraction: 0.86), value: scanning)
                        Text("Pebble")
                            .font(.system(size: AppFont.hero, weight: .bold))
                            .foregroundStyle(p.text)
                        Text(hub.t("onboardHeroSub"))
                            .font(.system(size: AppFont.body))
                            .foregroundStyle(p.text2)
                            .multilineTextAlignment(.center)
                    }

                    Spacer(minLength: 24)

                    if platformSupportsQRScanner && scanning {
                        QRScannerBox(
                            onCode: { onScanned($0) },
                            onMessage: { err = $0 }
                        )
                        .frame(height: 300)
                        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
                        .padding(.bottom, 16)
                        .transition(.opacity.combined(with: .scale(scale: 0.96)))
                    }

                    VStack(spacing: 10) {
                        if platformSupportsQRScanner {
                            Button {
                                err = ""
                                withAnimation(.spring(response: 0.32, dampingFraction: 0.86)) {
                                    scanning.toggle()
                                }
                            } label: {
                                HStack(spacing: 8) {
                                    Image(systemName: "qrcode.viewfinder")
                                        .font(.system(size: AppFont.cardTitle, weight: .semibold))
                                    Text(scanning ? hub.t("collapse") : hub.t("scanConnect"))
                                        .font(.system(size: AppFont.body, weight: .semibold))
                                }
                                .frame(maxWidth: .infinity)
                                .frame(height: 48)
                                .background(p.accent, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                                .foregroundStyle(p.card)
                            }
                            .buttonStyle(.plain)
                        }

                        Button {
                            scanning = false
                            withAnimation(.spring(response: 0.34, dampingFraction: 0.88)) {
                                manualOpen = true
                            }
                        } label: {
                            Text(hub.t("manualPairingBtn"))
                                .font(.system(size: AppFont.body, weight: .medium))
                                .frame(maxWidth: .infinity)
                                .frame(height: 48)
                                .background(p.card, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                                .foregroundStyle(p.text)
                        }
                        .buttonStyle(.plain)
                    }

                    if !err.isEmpty {
                        Text(err)
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(p.danger)
                            .multilineTextAlignment(.center)
                            .padding(.top, 12)
                    } else if !infoHint.isEmpty {
                        Text(infoHint)
                            .font(.system(size: AppFont.secondary))
                            .foregroundStyle(p.sageInk)
                            .multilineTextAlignment(.center)
                            .padding(.top, 12)
                    }

                    VStack(spacing: 6) {
                        Text(hub.t("noDesktopYet"))
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(p.mute)
                        HStack(spacing: 10) {
                            Button(hub.t("downloadDesktop")) { openURL(desktopDownloadURL) }
                            Text("·").foregroundStyle(p.mute)
                            ShareLink(item: desktopDownloadURL) {
                                Text(hub.t("sendToComputer"))
                            }
                        }
                        .font(.system(size: AppFont.caption, weight: .medium))
                        .foregroundStyle(p.text2)
                        .buttonStyle(.plain)
                    }
                    .padding(.top, 24)
                    .padding(.bottom, 20)
                }
                .frame(maxWidth: 360)
                .padding(.horizontal, 24)
                .frame(maxWidth: .infinity, minHeight: geo.size.height)
            }
        }
    }

    private var formContent: some View {
        let norm = normalizeAddress(address)
        return Form {
            if isGate && hub.activeServer != nil && (hub.connState == .unauthorized || hub.connState == .error) {
                Section(hub.connState == .unauthorized ? hub.t("tokenRequired") : hub.t("cantConnectHost")) {
                    Text(hub.connState == .error && !hub.connError.isEmpty ? hub.connError : hub.t("onboardDesc"))
                        .foregroundStyle(p.text2)
                    Button(hub.t("retry")) {
                        err = ""
                        showFormWhileConnecting = false
                        Task { await hub.reconnect() }
                    }
                }
            }
            if platformSupportsQRScanner {
                Section {
                    Button {
                        err = ""
                        withAnimation(.spring(response: 0.32, dampingFraction: 0.86)) {
                            scanning.toggle()
                        }
                    } label: {
                        Label(scanning ? hub.t("collapse") : hub.t("scanConnect"), systemImage: "qrcode.viewfinder")
                    }
                    if scanning {
                        QRScannerBox(
                            onCode: { onScanned($0) },
                            onMessage: { err = $0 }
                        )
                        .frame(height: 300)
                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                        .listRowInsets(EdgeInsets(top: 8, leading: 8, bottom: 8, trailing: 8))
                    }
                } header: {
                    Text(hub.t("scanPairTitle"))
                } footer: {
                    Text(hub.t("scanPairDesc"))
                }
            }
            Section {
                TextField("", text: $address, prompt: Text(hub.t("pairingLinkPh")), axis: .vertical)
                    .monoInput()
                    .lineLimit(3...6)
                    .accessibilityLabel(hub.t("manualPairingBtn"))
                Button {
                    pastePairingLink()
                } label: {
                    Label(hub.t("pasteLinkBtn"), systemImage: "doc.on.clipboard")
                }
                DisclosureGroup(hub.t("moreOptions"), isExpanded: $manualAdvOpen) {
                    TextField(hub.t("labelOptional"), text: $label, prompt: Text(hub.t("labelExamplePh")))
                    SecureField(hub.t("tokenOptionalLbl"), text: $token, prompt: Text(hub.t("tokenFillHint")))
                    TextField(hub.t("standbyAddress"), text: $standbyAddress, prompt: Text("tailcat://… / 192.168.x.x:3789")).monoInput()
                    SecureField(hub.t("standbyToken"), text: $standbyToken, prompt: Text(hub.t("tokenFillHint")))
                }
            } header: {
                Text(hub.t("manualPairingBtn"))
            } footer: {
                Text(!infoHint.isEmpty ? infoHint : !norm.token.isEmpty ? hub.t("tokenRecognized") : hub.t("pasteLinkHint"))
                    .foregroundStyle((!infoHint.isEmpty || !norm.token.isEmpty) ? p.sageInk : .secondary)
            }
            if !err.isEmpty { Section { StatusText(text: err) } }
            tailscaleSection
            if !hub.servers.isEmpty {
                Section(hub.t("savedHosts")) {
                    ForEach(hub.servers) { s in
                        Button {
                            err = ""
                            submitting = true
                            Task { await hub.selectServer(s.id) }
                        } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(s.label).foregroundStyle(.primary)
                                    Text(s.address).font(.system(size: AppFont.secondary, design: .monospaced)).foregroundStyle(.secondary)
                                        .lineLimit(1).truncationMode(.middle)
                                }
                                Spacer()
                                if s.id == hub.activeServerId {
                                    Image(systemName: "checkmark").foregroundStyle(.tint)
                                        .accessibilityLabel(hub.t("current"))
                                }
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .swipeActions {
                            Button(hub.t("removeBtn"), role: .destructive) { askRemove(s) }
                        }
                        .contextMenu {
                            Button(hub.t("removeBtn"), systemImage: "trash", role: .destructive) { askRemove(s) }
                        }
                    }
                }
            }
        }
        .hubForm()
    }

    private func askRemove(_ s: ServerProfile) {
        confirm = ConfirmItem(title: hub.t("removeBtn"), message: s.label) { await hub.removeServer(s.id) }
    }

    private func onScanned(_ raw: String) {
        let s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard isValidPairingQR(s) else {
            err = hub.t("invalidQr")
            return
        }
        triggerScanSuccessHaptic()
        scanning = false
        err = ""
        let norm = normalizeAddress(s)
        address = norm.cleanAddress
        if !norm.token.isEmpty { token = norm.token }
        connect(address: s, token: norm.token.isEmpty ? token : norm.token)
    }

    private func pastePairingLink() {
        err = ""
        let raw = readClipboardString()
            .trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "[\\r\\n]+", with: "", options: .regularExpression)
        guard !raw.isEmpty else {
            infoHint = hub.t("clipboardEmpty")
            return
        }
        infoHint = ""
        address = raw
    }

    private func submit() {
        err = ""
        guard !address.trimmingCharacters(in: .whitespaces).isEmpty else { err = hub.t("enterAddrOrScan"); return }
        connect(address: address, token: token)
    }

    private func connect(address: String, token: String) {
        submitting = true
        Task {
            do {
                try await hub.addAndConnectServer(
                    label: label,
                    address: address,
                    token: token,
                    standbyAddress: standbyAddress,
                    standbyToken: standbyToken
                )
            } catch {
                submitting = false
                err = errText(error, hub.t("connFailed"))
            }
        }
    }

    @ViewBuilder
    private var tailscaleSection: some View {
        let ts = hub.mobileTailscale
        let state = ts?.state ?? "disabled"
        Section(hub.t("tsNetwork")) {
            if state == "running" {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(hub.t("tsBoundReady", ["tailnet": ts?.tailnet ?? "Tailscale"]))
                            .font(.system(size: AppFont.body, weight: .semibold))
                            .foregroundStyle(p.sageInk)
                        if let ip = ts?.ip, !ip.isEmpty {
                            Text(ip).font(.system(size: AppFont.secondary, design: .monospaced)).foregroundStyle(.secondary)
                        }
                    }
                    Spacer()
                    Button(hub.t("tsRefreshPeers")) {
                        Task { await hub.refreshMobileTailscale(includePeers: true) }
                    }
                    .buttonStyle(.bordered).controlSize(.small)
                    Button(hub.t("tsUnlink"), role: .destructive) {
                        confirm = ConfirmItem(title: hub.t("tsUnlinkTitle"), message: hub.t("tsUnlinkDesc")) {
                            await hub.logoutTailscale()
                        }
                    }
                    .buttonStyle(.bordered).controlSize(.small)
                }
                let peers = (ts?.peers ?? []).filter { $0.online }
                if peers.isEmpty {
                    Text(hub.t("tsNoPeers")).font(.system(size: AppFont.secondary)).foregroundStyle(.secondary)
                } else {
                    ForEach(peers) { peer in
                        Button {
                            selectPeer(peer)
                        } label: {
                            HStack {
                                VStack(alignment: .leading, spacing: 2) {
                                    HStack(spacing: 6) {
                                        Text(peer.hostname).foregroundStyle(.primary)
                                        if peer.isBotHub {
                                            Tag(text: hub.t("tsBotHubBadge"), bg: p.lilac, fg: p.lilacInk)
                                        }
                                    }
                                    Text(peer.dnsName ?? peer.ip ?? peer.address)
                                        .font(.system(size: AppFont.caption, design: .monospaced))
                                        .foregroundStyle(.secondary)
                                        .lineLimit(1).truncationMode(.middle)
                                }
                                Spacer()
                                Image(systemName: "arrow.up.right.circle").foregroundStyle(.tint)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                }
            } else if state == "needs_login" {
                HStack {
                    Label(hub.t("tsNeedsLogin"), systemImage: "person.badge.key")
                        .foregroundStyle(p.accent)
                    Spacer()
                    if let auth = ts?.authUrl, let url = URL(string: auth) {
                        Button(hub.t("tsOpenLogin")) { openURL(url) }
                            .buttonStyle(.borderedProminent).controlSize(.small)
                    }
                }
            } else if state == "starting" || hub.tsBinding {
                HStack(spacing: 10) {
                    ProgressView()
                    Text(hub.t("tsBinding")).foregroundStyle(.secondary)
                }
            } else {
                if state == "error", let msg = ts?.error, !msg.isEmpty {
                    StatusText(text: msg)
                }
                Button {
                    Task {
                        await hub.bindTailscale(authKey: tsAuthKey, controlUrl: tsControlUrl)
                        tsAuthKey = ""
                    }
                } label: {
                    Label(hub.t("tsBindAccount"), systemImage: "network")
                }
                DisclosureGroup(hub.t("tsAdvanced"), isExpanded: $tsAdvOpen) {
                    SecureField(hub.t("tsAuthKeyPh"), text: $tsAuthKey).monoInput()
                    TextField(hub.t("tsControlUrlPh"), text: $tsControlUrl).monoInput()
                }
            }
        }
    }

    private func selectPeer(_ peer: TailscalePeer) {
        err = ""
        infoHint = ""
        label = peer.hostname
        address = peer.address
        if let saved = hub.servers.first(where: { $0.address == peer.address && !$0.token.isEmpty }) {
            token = saved.token
            connect(address: peer.address, token: saved.token)
            return
        }
        if !token.trimmingCharacters(in: .whitespaces).isEmpty {
            connect(address: peer.address, token: token)
            return
        }
        manualAdvOpen = true
        infoHint = hub.t("tsPeerSelectedHint")
    }

}
