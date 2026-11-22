import SwiftUI
import AVFoundation

struct ConnectView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    var startScan: Bool

    @State private var scanning = false
    @State private var label = ""
    @State private var address = ""
    @State private var token = ""
    @State private var err = ""
    @State private var submitting = false
    @State private var confirm: ConfirmItem?

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
                TextField(hub.t("connAddr"), text: $address, prompt: Text("tailcat://… / 192.168.1.10:3789")).monoInput()
                SecureField(hub.t("tokenOptionalLbl"), text: $token, prompt: Text(hub.t("tokenFillHint")))
            } header: {
                Text(hub.t("manualAddrTitle"))
            } footer: {
                Text(!norm.token.isEmpty ? hub.t("tokenRecognized") : hub.t("tokenAutoHint"))
                    .foregroundStyle(!norm.token.isEmpty ? p.sageInk : .secondary)
            }
            if !err.isEmpty { Section { StatusText(text: err) } }
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
        guard lower.hasPrefix("tailcat://") || lower.hasPrefix("http://") || lower.hasPrefix("https://") else {
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
            if lower.hasPrefix("tailcat://") || lower.hasPrefix("http://") || lower.hasPrefix("https://") { locked = true }
            onCode(code)
        }
    }
}
