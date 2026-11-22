import SwiftUI
import AVFoundation

struct ConnectView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.openURL) private var openURL
    var startScan: Bool

    @State private var scanning = false
    @State private var label = ""
    @State private var address = ""
    @State private var token = ""
    @State private var err = ""
    @State private var infoHint = ""
    @State private var submitting = false
    @State private var confirm: ConfirmItem?
    @State private var tsAdvOpen = false
    @State private var tsAuthKey = ""
    @State private var tsControlUrl = ""

    var body: some View {
        let norm = normalizeAddress(address)
        Form {
            Section {
                Button {
                    toggleScanner()
                } label: {
                    Label(scanning ? hub.t("collapse") : hub.t("scanConnect"), systemImage: "qrcode.viewfinder")
                }
                if scanning {
                    QRScanner { onScanned($0) }
                        .frame(height: 240)
                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                        .listRowInsets(EdgeInsets(top: 8, leading: 8, bottom: 8, trailing: 8))
                }
            } header: {
                Text(hub.t("scanPairTitle"))
            } footer: {
                Text(hub.t("scanPairDesc"))
            }
            Section {
                TextField(hub.t("labelOptional"), text: $label, prompt: Text(hub.t("labelExamplePh")))
                TextField(hub.t("connAddr"), text: $address, prompt: Text("tailcat://… / tailscale://… / 192.168.1.10:3789")).monoInput()
                SecureField(hub.t("tokenOptionalLbl"), text: $token, prompt: Text(hub.t("tokenFillHint")))
            } header: {
                Text(hub.t("manualAddrTitle"))
            } footer: {
                Text(!infoHint.isEmpty ? infoHint : !norm.token.isEmpty ? hub.t("tokenRecognized") : hub.t("tokenAutoHint"))
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
                                    Text(s.address).font(.system(size: 13, design: .monospaced)).foregroundStyle(.secondary)
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
        .navigationTitle(hub.t("connectHost"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { SheetToolbar(actionTitle: hub.t("connect"), loading: submitting) { submit() } }
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
            if startScan { toggleScanner() }
        }
        .onChange(of: hub.connState) { _, st in
            guard submitting else { return }
            if st == .connected {
                submitting = false
                router.sheet = nil
            } else if st == .error || st == .unauthorized {
                submitting = false
                err = hub.connError.isEmpty ? hub.t("connFailed") : hub.connError
            }
        }
    }

    private func askRemove(_ s: ServerProfile) {
        confirm = ConfirmItem(title: hub.t("removeBtn"), message: s.label) { await hub.removeServer(s.id) }
    }

    private func toggleScanner() {
        err = ""
        if scanning { scanning = false; return }
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: scanning = true
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { ok in
                DispatchQueue.main.async { if ok { scanning = true } else { err = hub.t("camPermReq") } }
            }
        default: err = hub.t("camPermReq")
        }
    }

    private func onScanned(_ raw: String) {
        let s = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        let lower = s.lowercased()
        guard lower.hasPrefix("tailcat://") || lower.hasPrefix("tailscale://") || lower.hasPrefix("http://") || lower.hasPrefix("https://") else {
            err = hub.t("invalidQr")
            return
        }
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        scanning = false
        let norm = normalizeAddress(s)
        address = norm.cleanAddress
        if !norm.token.isEmpty { token = norm.token }
        connect(address: s, token: norm.token.isEmpty ? token : norm.token)
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
                try await hub.addAndConnectServer(label: label, address: address, token: token)
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
                            .font(.system(size: 15, weight: .semibold))
                            .foregroundStyle(p.sageInk)
                        if let ip = ts?.ip, !ip.isEmpty {
                            Text(ip).font(.system(size: 13, design: .monospaced)).foregroundStyle(.secondary)
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
                    Text(hub.t("tsNoPeers")).font(.system(size: 14)).foregroundStyle(.secondary)
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
                                        .font(.system(size: 12, design: .monospaced))
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
        infoHint = hub.t("tsPeerSelectedHint")
    }

}

/// AVFoundation 二维码扫描。
struct QRScanner: UIViewRepresentable {
    var onCode: (String) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onCode: onCode) }

    func makeUIView(context: Context) -> PreviewView {
        let v = PreviewView()
        v.backgroundColor = .black
        let session = AVCaptureSession()
        if let dev = AVCaptureDevice.default(for: .video), let input = try? AVCaptureDeviceInput(device: dev), session.canAddInput(input) {
            session.addInput(input)
            let out = AVCaptureMetadataOutput()
            if session.canAddOutput(out) {
                session.addOutput(out)
                out.setMetadataObjectsDelegate(context.coordinator, queue: .main)
                out.metadataObjectTypes = [.qr]
            }
        }
        v.previewLayer.session = session
        v.previewLayer.videoGravity = .resizeAspectFill
        context.coordinator.session = session
        DispatchQueue.global(qos: .userInitiated).async { session.startRunning() }
        return v
    }

    func updateUIView(_ uiView: PreviewView, context: Context) {}

    static func dismantleUIView(_ uiView: PreviewView, coordinator: Coordinator) {
        let s = coordinator.session
        DispatchQueue.global(qos: .userInitiated).async { s?.stopRunning() }
    }

    final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var previewLayer: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    }

    final class Coordinator: NSObject, AVCaptureMetadataOutputObjectsDelegate {
        var onCode: (String) -> Void
        var session: AVCaptureSession?
        var locked = false
        init(onCode: @escaping (String) -> Void) { self.onCode = onCode }

        func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
            guard !locked, let code = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue, !code.isEmpty else { return }
            let lower = code.lowercased()
            if lower.hasPrefix("tailcat://") || lower.hasPrefix("tailscale://") || lower.hasPrefix("http://") || lower.hasPrefix("https://") { locked = true }
            onCode(code)
        }
    }
}
