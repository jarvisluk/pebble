import SwiftUI
#if os(iOS)
import UIKit
import AVFoundation
import PhotosUI
import Vision
import CoreImage
#endif

var platformSupportsQRScanner: Bool {
    #if os(iOS)
    return true
    #else
    return false
    #endif
}

func isValidPairingQR(_ raw: String) -> Bool {
    let lower = raw.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    return lower.hasPrefix("tailcat://")
        || lower.hasPrefix("tailscale://")
        || lower.hasPrefix("http://")
        || lower.hasPrefix("https://")
}

@MainActor
func triggerScanSuccessHaptic() {
    #if os(iOS)
    UINotificationFeedbackGenerator().notificationOccurred(.success)
    #endif
}

#if os(iOS)
struct QRScannerBox: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    @Environment(\.scenePhase) private var scenePhase

    var onCode: (String) -> Void
    var onMessage: (String) -> Void

    @State private var authStatus: AVAuthorizationStatus = AVCaptureDevice.authorizationStatus(for: .video)
    @State private var hasCameraHardware: Bool = AVCaptureDevice.default(for: .video) != nil
    @State private var hasTorch = false
    @State private var torchOn = false
    @State private var focusPoint: CGPoint? = nil
    @State private var focusHideTask: Task<Void, Never>? = nil
    @State private var bottomBarHeight: CGFloat = 88
    @State private var showPhotoPicker = false
    @State private var selectedPhotoItem: PhotosPickerItem? = nil
    @State private var decodingPhoto = false
    @State private var didRequestInitialPerm = false
    @State private var handledCode = false

    var body: some View {
        ZStack {
            if hasCameraHardware && authStatus == .authorized {
                Color.black

                QRCameraPreview(
                    torchOn: torchOn,
                    onHasTorch: { hasTorch = $0 },
                    onFocusPoint: { showFocusIndicator(at: $0) },
                    onCode: { code in
                        guard !handledCode else { return }
                        handledCode = true
                        torchOn = false
                        onCode(code)
                    },
                    onMessage: { onMessage($0) }
                )

                viewfinderOverlay

                bottomBar
            } else {
                (p.dark ? p.s1 : Color.black)

                fallbackView
            }
        }
        .onAppear {
            refreshCameraState()
            if hasCameraHardware && authStatus == .notDetermined && !didRequestInitialPerm {
                didRequestInitialPerm = true
                requestCameraPermission()
            }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active {
                refreshCameraState()
            }
        }
        .onDisappear {
            focusHideTask?.cancel()
            focusHideTask = nil
            torchOn = false
        }
        .photosPicker(isPresented: $showPhotoPicker, selection: $selectedPhotoItem, matching: .images)
        .onChange(of: selectedPhotoItem) { _, item in
            guard let item else { return }
            selectedPhotoItem = nil
            handlePickedPhoto(item)
        }
    }

    private var viewfinderOverlay: some View {
        Canvas { ctx, size in
            let topPad: CGFloat = 16
            let gapAboveBar: CGFloat = 10
            let effectiveBottomBar = max(bottomBarHeight, 88)
            let availHeight = max(size.height - effectiveBottomBar - topPad - gapAboveBar, 64)
            let availWidth = max(size.width - 32, 64)
            let frameSide = min(max(min(availWidth * 0.70, availHeight * 0.90), 72), 196)
            let left = (size.width - frameSide) / 2
            let top = topPad + (availHeight - frameSide) / 2
            let rect = CGRect(x: left, y: top, width: frameSide, height: frameSide)
            let cutout = Path(roundedRect: rect, cornerRadius: 16, style: .continuous)

            var overlay = Path(CGRect(origin: .zero, size: size))
            overlay.addPath(cutout)
            ctx.fill(overlay, with: .color(Color.black.opacity(0.45)), style: FillStyle(eoFill: true))
            ctx.stroke(cutout, with: .color(Color.white.opacity(0.85)), lineWidth: 2)

            if let pt = focusPoint {
                let ring = Path(ellipseIn: CGRect(x: pt.x - 22, y: pt.y - 22, width: 44, height: 44))
                ctx.stroke(ring, with: .color(Color.white.opacity(0.9)), lineWidth: 2)
            }
        }
        .allowsHitTesting(false)
    }

    private var bottomBar: some View {
        VStack {
            Spacer()
            VStack(spacing: 8) {
                Text(hub.t("scanHint"))
                    .font(.system(size: AppFont.caption))
                    .foregroundStyle(Color.white.opacity(0.90))
                    .lineLimit(2)
                    .multilineTextAlignment(.center)

                HStack(spacing: 10) {
                    if hasTorch {
                        ScannerPillButton(
                            systemImage: torchOn ? "flashlight.on.fill" : "flashlight.off.fill",
                            label: hub.t("scanTorch")
                        ) {
                            torchOn.toggle()
                        }
                    }
                    ScannerPillButton(
                        systemImage: "photo.on.rectangle.angled",
                        label: hub.t("scanPickPhoto"),
                        loading: decodingPhoto
                    ) {
                        torchOn = false
                        showPhotoPicker = true
                    }
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bottomBarHeight = $0 }
        }
    }

    private var fallbackView: some View {
        VStack(spacing: 14) {
            Text(hub.t("scanAlbumFallback"))
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(Color.white.opacity(0.85))
                .multilineTextAlignment(.center)

            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) {
                    fallbackButtons(fixedWidth: true)
                }
                VStack(spacing: 10) {
                    fallbackButtons(fixedWidth: false)
                }
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    @ViewBuilder
    private func fallbackButtons(fixedWidth: Bool) -> some View {
        if hasCameraHardware {
            Button {
                if authStatus == .notDetermined {
                    requestCameraPermission()
                } else if let url = URL(string: UIApplication.openSettingsURLString) {
                    openExternalURL(url)
                }
            } label: {
                Text(hub.t("scanGrantCamera"))
                    .font(.system(size: AppFont.body, weight: .semibold))
                    .foregroundStyle(p.dark ? p.card : p.text)
                    .lineLimit(1)
                    .fixedSize(horizontal: fixedWidth, vertical: false)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    .background(p.dark ? p.accent : p.card, in: Capsule())
            }
            .buttonStyle(.plain)
        }

        Button {
            showPhotoPicker = true
        } label: {
            HStack(spacing: 6) {
                if decodingPhoto {
                    ProgressView()
                        .controlSize(.small)
                        .tint(.white)
                }
                Text(hub.t("scanPickPhoto"))
                    .font(.system(size: AppFont.body, weight: .semibold))
                    .foregroundStyle(.white)
                    .lineLimit(1)
                    .fixedSize(horizontal: fixedWidth, vertical: false)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(Color.white.opacity(0.20), in: Capsule())
        }
        .buttonStyle(.plain)
        .disabled(decodingPhoto)
    }

    private func refreshCameraState() {
        hasCameraHardware = AVCaptureDevice.default(for: .video) != nil
        authStatus = AVCaptureDevice.authorizationStatus(for: .video)
    }

    private func requestCameraPermission() {
        AVCaptureDevice.requestAccess(for: .video) { granted in
            DispatchQueue.main.async {
                authStatus = AVCaptureDevice.authorizationStatus(for: .video)
                if !granted {
                    onMessage(hub.t("camPermReq"))
                }
            }
        }
    }

    private func showFocusIndicator(at point: CGPoint) {
        focusPoint = point
        focusHideTask?.cancel()
        focusHideTask = Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(800))
            guard !Task.isCancelled else { return }
            focusPoint = nil
        }
    }

    private func handlePickedPhoto(_ item: PhotosPickerItem) {
        onMessage("")
        decodingPhoto = true
        Task {
            let data = try? await item.loadTransferable(type: Data.self)
            let payloads = await Task.detached(priority: .userInitiated) {
                decodeQRPayloads(from: data)
            }.value
            decodingPhoto = false
            if let valid = payloads.first(where: { isValidPairingQR($0) }) {
                guard !handledCode else { return }
                handledCode = true
                torchOn = false
                onCode(valid)
            } else if !payloads.isEmpty {
                onMessage(hub.t("invalidQr"))
            } else {
                onMessage(hub.t("scanNoQrFound"))
            }
        }
    }
}

private struct ScannerPillButton: View {
    var systemImage: String
    var label: String
    var loading = false
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if loading {
                    ProgressView()
                        .controlSize(.small)
                        .tint(.white)
                } else {
                    Image(systemName: systemImage)
                        .font(.system(size: AppFont.secondary, weight: .medium))
                        .foregroundStyle(.white)
                }
                Text(label)
                    .font(.system(size: AppFont.secondary, weight: .medium))
                    .foregroundStyle(.white)
                    .lineLimit(1)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .background(Color.black.opacity(0.65), in: Capsule())
            .overlay(Capsule().stroke(Color.white.opacity(0.22), lineWidth: 1))
        }
        .buttonStyle(.plain)
        .disabled(loading)
    }
}

private func decodeQRPayloads(from data: Data?) -> [String] {
    guard let data, !data.isEmpty else { return [] }
    var results: [String] = []
    var seen = Set<String>()

    func append(_ raw: String?) {
        guard let trimmed = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else { return }
        if seen.insert(trimmed).inserted {
            results.append(trimmed)
        }
    }

    // 1. Primary: Vision barcode detection
    let request = VNDetectBarcodesRequest()
    request.symbologies = [.qr]
    let handler = VNImageRequestHandler(data: data, options: [:])
    if (try? handler.perform([request])) != nil, let observations = request.results {
        for obs in observations {
            append(obs.payloadStringValue)
        }
    }
    if !results.isEmpty {
        return results
    }

    // 2. Fallback: CoreImage CIDetector
    guard let ciImage = CIImage(data: data, options: [.applyOrientationProperty: true]),
          let detector = CIDetector(
              ofType: CIDetectorTypeQRCode,
              context: nil,
              options: [CIDetectorAccuracy: CIDetectorAccuracyHigh]
          ) else {
        return results
    }

    for feature in detector.features(in: ciImage) {
        if let qr = feature as? CIQRCodeFeature {
            append(qr.messageString)
        }
    }
    if !results.isEmpty {
        return results
    }

    // 3. Transparent PNG fallback: composite over white background
    let white = CIImage(color: .white).cropped(to: ciImage.extent)
    let composited = ciImage.composited(over: white)
    for feature in detector.features(in: composited) {
        if let qr = feature as? CIQRCodeFeature {
            append(qr.messageString)
        }
    }
    return results
}

private struct QRCameraPreview: UIViewRepresentable {
    var torchOn: Bool
    var onHasTorch: (Bool) -> Void
    var onFocusPoint: (CGPoint) -> Void
    var onCode: (String) -> Void
    var onMessage: (String) -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(
            onHasTorch: onHasTorch,
            onFocusPoint: onFocusPoint,
            onCode: onCode,
            onMessage: onMessage
        )
    }

    func makeUIView(context: Context) -> PreviewView {
        let v = PreviewView()
        v.backgroundColor = .black
        let coordinator = context.coordinator
        v.previewLayer.session = coordinator.session
        v.previewLayer.videoGravity = .resizeAspectFill
        coordinator.previewLayer = v.previewLayer

        let pinch = UIPinchGestureRecognizer(target: coordinator, action: #selector(Coordinator.handlePinch(_:)))
        let tap = UITapGestureRecognizer(target: coordinator, action: #selector(Coordinator.handleTap(_:)))
        v.addGestureRecognizer(pinch)
        v.addGestureRecognizer(tap)

        if let dev = AVCaptureDevice.default(for: .video) {
            coordinator.device = dev
            let hasTorch = dev.hasTorch
            DispatchQueue.main.async {
                coordinator.onHasTorch(hasTorch)
            }

            let rc = AVCaptureDevice.RotationCoordinator(device: dev, previewLayer: v.previewLayer)
            coordinator.rotationCoordinator = rc
            coordinator.rotationObs = rc.observe(
                \.videoRotationAngleForHorizonLevelPreview,
                options: [.initial, .new]
            ) { [weak layer = v.previewLayer] _, change in
                guard let angle = change.newValue else { return }
                DispatchQueue.main.async {
                    if let conn = layer?.connection, conn.isVideoRotationAngleSupported(angle) {
                        conn.videoRotationAngle = angle
                    }
                }
            }

            coordinator.sessionQueue.async {
                guard !coordinator.disposed else { return }
                let session = coordinator.session
                session.beginConfiguration()
                if let input = try? AVCaptureDeviceInput(device: dev), session.canAddInput(input) {
                    session.addInput(input)
                }
                let out = AVCaptureMetadataOutput()
                if session.canAddOutput(out) {
                    session.addOutput(out)
                    out.setMetadataObjectsDelegate(coordinator, queue: .main)
                    if out.availableMetadataObjectTypes.contains(.qr) {
                        out.metadataObjectTypes = [.qr]
                    }
                }
                session.commitConfiguration()
                if !coordinator.disposed && !session.isRunning {
                    session.startRunning()
                }
            }
        } else {
            DispatchQueue.main.async {
                coordinator.onHasTorch(false)
            }
        }

        return v
    }

    func updateUIView(_ uiView: PreviewView, context: Context) {
        let coordinator = context.coordinator
        coordinator.onHasTorch = onHasTorch
        coordinator.onFocusPoint = onFocusPoint
        coordinator.onCode = onCode
        coordinator.onMessage = onMessage
        if coordinator.lastAppliedTorch != torchOn {
            coordinator.lastAppliedTorch = torchOn
            coordinator.setTorch(torchOn)
        }
    }

    static func dismantleUIView(_ uiView: PreviewView, coordinator: Coordinator) {
        coordinator.disposed = true
        coordinator.rotationObs?.invalidate()
        coordinator.rotationObs = nil
        coordinator.rotationCoordinator = nil
        let session = coordinator.session
        let device = coordinator.device
        coordinator.sessionQueue.async {
            if let device, device.hasTorch, device.torchMode != .off {
                if (try? device.lockForConfiguration()) != nil {
                    device.torchMode = .off
                    device.unlockForConfiguration()
                }
            }
            if session.isRunning {
                session.stopRunning()
            }
        }
    }

    final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var previewLayer: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    }

    final class Coordinator: NSObject, AVCaptureMetadataOutputObjectsDelegate {
        var onHasTorch: (Bool) -> Void
        var onFocusPoint: (CGPoint) -> Void
        var onCode: (String) -> Void
        var onMessage: (String) -> Void

        let sessionQueue = DispatchQueue(label: "ai.pebble.qrscanner.session", qos: .userInitiated)
        let session = AVCaptureSession()
        weak var previewLayer: AVCaptureVideoPreviewLayer?
        var device: AVCaptureDevice?
        var rotationCoordinator: AVCaptureDevice.RotationCoordinator?
        var rotationObs: NSKeyValueObservation?
        var locked = false
        var disposed = false
        var lastAppliedTorch = false
        private var pinchStartZoom: CGFloat = 1.0
        private var lastInvalidMessageAt = Date.distantPast

        init(
            onHasTorch: @escaping (Bool) -> Void,
            onFocusPoint: @escaping (CGPoint) -> Void,
            onCode: @escaping (String) -> Void,
            onMessage: @escaping (String) -> Void
        ) {
            self.onHasTorch = onHasTorch
            self.onFocusPoint = onFocusPoint
            self.onCode = onCode
            self.onMessage = onMessage
        }

        func setTorch(_ on: Bool) {
            let device = self.device
            sessionQueue.async { [weak self] in
                guard let self, !self.disposed, let device, device.hasTorch else { return }
                guard (try? device.lockForConfiguration()) != nil else { return }
                defer { device.unlockForConfiguration() }
                if on && device.isTorchModeSupported(.on) {
                    device.torchMode = .on
                } else if device.isTorchModeSupported(.off) {
                    device.torchMode = .off
                }
            }
        }

        @objc func handlePinch(_ recognizer: UIPinchGestureRecognizer) {
            guard let device else { return }
            if recognizer.state == .began {
                pinchStartZoom = device.videoZoomFactor
            } else if recognizer.state == .changed {
                let maxZoom = min(device.activeFormat.videoMaxZoomFactor, 6.0)
                let target = min(max(pinchStartZoom * recognizer.scale, 1.0), maxZoom)
                sessionQueue.async { [weak self] in
                    guard let self, !self.disposed else { return }
                    guard (try? device.lockForConfiguration()) != nil else { return }
                    defer { device.unlockForConfiguration() }
                    device.videoZoomFactor = target
                }
            }
        }

        @objc func handleTap(_ recognizer: UITapGestureRecognizer) {
            guard recognizer.state == .ended,
                  let view = recognizer.view,
                  let layer = previewLayer,
                  let device else { return }
            let pt = recognizer.location(in: view)
            let devicePt = layer.captureDevicePointConverted(fromLayerPoint: pt)
            onFocusPoint(pt)
            sessionQueue.async { [weak self] in
                guard let self, !self.disposed else { return }
                guard (try? device.lockForConfiguration()) != nil else { return }
                defer { device.unlockForConfiguration() }
                if device.isFocusPointOfInterestSupported {
                    device.focusPointOfInterest = devicePt
                    if device.isFocusModeSupported(.continuousAutoFocus) {
                        device.focusMode = .continuousAutoFocus
                    } else if device.isFocusModeSupported(.autoFocus) {
                        device.focusMode = .autoFocus
                    }
                }
                if device.isExposurePointOfInterestSupported {
                    device.exposurePointOfInterest = devicePt
                    if device.isExposureModeSupported(.continuousAutoExposure) {
                        device.exposureMode = .continuousAutoExposure
                    }
                }
            }
        }

        func metadataOutput(
            _ output: AVCaptureMetadataOutput,
            didOutput objects: [AVMetadataObject],
            from connection: AVCaptureConnection
        ) {
            guard !locked, !disposed else { return }
            guard let raw = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue?
                .trimmingCharacters(in: .whitespacesAndNewlines),
                !raw.isEmpty else {
                return
            }
            if isValidPairingQR(raw) {
                locked = true
                lastAppliedTorch = false
                let session = self.session
                let device = self.device
                sessionQueue.async {
                    if let device, device.hasTorch, device.torchMode != .off {
                        if (try? device.lockForConfiguration()) != nil {
                            device.torchMode = .off
                            device.unlockForConfiguration()
                        }
                    }
                    if session.isRunning {
                        session.stopRunning()
                    }
                }
                onCode(raw)
            } else {
                let now = Date()
                if now.timeIntervalSince(lastInvalidMessageAt) >= 1.0 {
                    lastInvalidMessageAt = now
                    onMessage(L.t("invalidQr"))
                }
            }
        }
    }
}
#else
struct QRScannerBox: View {
    var onCode: (String) -> Void
    var onMessage: (String) -> Void

    var body: some View { EmptyView() }
}
#endif
