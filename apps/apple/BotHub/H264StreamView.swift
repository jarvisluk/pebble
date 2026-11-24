import AVFoundation
import CoreMedia
import Foundation
import Observation
import SwiftUI
#if os(iOS)
import UIKit
#elseif os(macOS)
import AppKit
#endif

struct H264StreamMeta {
    var imageWidth: Double = 0
    var imageHeight: Double = 0
    var logicalX: Double = 0
    var logicalY: Double = 0
    var logicalWidth: Double = 1440
    var logicalHeight: Double = 900
    var fps: Int = 60
    var codec: String = "h264"
    var preset: String = ""
    var bitrateKbps: Int = 0
}

#if os(iOS)
final class SampleBufferCanvasView: UIView {
    override class var layerClass: AnyClass { AVSampleBufferDisplayLayer.self }
    var displayLayer: AVSampleBufferDisplayLayer { layer as! AVSampleBufferDisplayLayer }

    override init(frame: CGRect) {
        super.init(frame: frame)
        displayLayer.videoGravity = .resizeAspect
        backgroundColor = .black
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
}

struct H264DisplayView: UIViewRepresentable {
    let controller: H264StreamController

    func makeUIView(context: Context) -> SampleBufferCanvasView {
        let v = SampleBufferCanvasView()
        controller.attach(layer: v.displayLayer)
        return v
    }

    func updateUIView(_ uiView: SampleBufferCanvasView, context: Context) {
        controller.attach(layer: uiView.displayLayer)
    }
}
#elseif os(macOS)
final class SampleBufferCanvasNSView: NSView {
    let displayLayer = AVSampleBufferDisplayLayer()
    var mapPoint: ((CGPoint, CGSize) -> CGPoint?)?
    var onInput: (([String: Any]) -> Void)?
    private var trackingAreaRef: NSTrackingArea?
    private var lastMoveSent = Date.distantPast

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.backgroundColor = NSColor.black.cgColor
        displayLayer.videoGravity = .resizeAspect
        layer?.addSublayer(displayLayer)
        setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        setContentCompressionResistancePriority(.defaultLow, for: .vertical)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override var isFlipped: Bool { true }
    override var acceptsFirstResponder: Bool { true }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        displayLayer.frame = bounds
        CATransaction.commit()
    }

    override func updateTrackingAreas() {
        super.updateTrackingAreas()
        if let trackingAreaRef { removeTrackingArea(trackingAreaRef) }
        let area = NSTrackingArea(
            rect: bounds,
            options: [.activeInKeyWindow, .mouseMoved, .inVisibleRect],
            owner: self,
            userInfo: nil
        )
        addTrackingArea(area)
        trackingAreaRef = area
    }

    private func mapped(_ event: NSEvent) -> CGPoint? {
        let pt = convert(event.locationInWindow, from: nil)
        return mapPoint?(pt, bounds.size)
    }

    override func mouseMoved(with event: NSEvent) {
        guard Date().timeIntervalSince(lastMoveSent) >= 0.016, let pt = mapped(event) else { return }
        lastMoveSent = Date()
        onInput?(["kind": "move", "x": pt.x, "y": pt.y])
    }

    override func mouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        guard let pt = mapped(event) else { return }
        onInput?(["kind": "down", "x": pt.x, "y": pt.y, "button": "left"])
    }

    override func mouseDragged(with event: NSEvent) {
        guard Date().timeIntervalSince(lastMoveSent) >= 0.016, let pt = mapped(event) else { return }
        lastMoveSent = Date()
        onInput?(["kind": "move", "x": pt.x, "y": pt.y])
    }

    override func mouseUp(with event: NSEvent) {
        guard let pt = mapped(event) else { return }
        onInput?(["kind": "up", "x": pt.x, "y": pt.y, "button": "left"])
    }

    override func rightMouseDown(with event: NSEvent) {
        window?.makeFirstResponder(self)
        guard let pt = mapped(event) else { return }
        onInput?(["kind": "down", "x": pt.x, "y": pt.y, "button": "right"])
    }

    override func rightMouseUp(with event: NSEvent) {
        guard let pt = mapped(event) else { return }
        onInput?(["kind": "up", "x": pt.x, "y": pt.y, "button": "right"])
    }

    override func scrollWheel(with event: NSEvent) {
        let dy = Int(event.scrollingDeltaY.rounded())
        let dx = Int(event.scrollingDeltaX.rounded())
        guard dy != 0 || dx != 0 else { return }
        onInput?(["kind": "scroll", "deltaX": dx, "deltaY": dy])
    }

    override func keyDown(with event: NSEvent) {
        let keyName: String? = switch event.keyCode {
        case 36, 76: "Return"
        case 51, 117: "Backspace"
        case 53: "Escape"
        case 48: "Tab"
        case 123: "Left"
        case 124: "Right"
        case 125: "Down"
        case 126: "Up"
        default: nil
        }
        if let keyName {
            onInput?(["kind": "key", "key": keyName])
            return
        }
        if let chars = event.characters, !chars.isEmpty, !event.modifierFlags.contains(.command) {
            onInput?(["kind": "type", "text": chars])
        }
    }
}

struct H264DisplayView: NSViewRepresentable {
    let controller: H264StreamController
    var mapPoint: ((CGPoint, CGSize) -> CGPoint?)? = nil
    var onInput: (([String: Any]) -> Void)? = nil

    func makeNSView(context: Context) -> SampleBufferCanvasNSView {
        let v = SampleBufferCanvasNSView()
        v.mapPoint = mapPoint
        v.onInput = onInput
        controller.attach(layer: v.displayLayer)
        return v
    }

    func updateNSView(_ nsView: SampleBufferCanvasNSView, context: Context) {
        nsView.mapPoint = mapPoint
        nsView.onInput = onInput
        controller.attach(layer: nsView.displayLayer)
    }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView: SampleBufferCanvasNSView, context: Context) -> CGSize? {
        CGSize(width: proposal.width ?? 240, height: proposal.height ?? 150)
    }
}
#endif

@MainActor
@Observable
final class H264StreamController {
    var meta: H264StreamMeta?
    var fps: Int = 0
    var connected: Bool = false
    var error: String = ""
    var screenRecordingDenied: Bool = false

    @ObservationIgnored private let decoder = H264DecoderWorker()
    @ObservationIgnored private var task: URLSessionWebSocketTask?
    @ObservationIgnored private var receiveLoop: Task<Void, Never>?
    @ObservationIgnored private var fpsTimer: Task<Void, Never>?
    @ObservationIgnored private var generation: Int = 0

    func attach(layer: AVSampleBufferDisplayLayer) {
        decoder.attach(layer: layer)
    }

    func start(request: URLRequest, trackFPS: Bool = true) {
        stop()
        generation += 1
        let gen = generation
        error = ""
        screenRecordingDenied = false
        decoder.reset(generation: gen, fpsScale: 60)
        let ws = URLSession.shared.webSocketTask(with: request)
        task = ws
        ws.resume()

        if trackFPS {
            fpsTimer = Task { [weak self] in
                while !Task.isCancelled {
                    try? await Task.sleep(nanoseconds: 1_000_000_000)
                    guard let self, !Task.isCancelled, self.generation == gen else { break }
                    let nextFps = self.decoder.drainFrameCount()
                    if self.fps != nextFps {
                        self.fps = nextFps
                    }
                }
            }
        }

        receiveLoop = Task.detached { [weak self] in
            await self?.runReceive(ws: ws, gen: gen)
        }
    }

    func stop() {
        generation += 1
        receiveLoop?.cancel()
        receiveLoop = nil
        fpsTimer?.cancel()
        fpsTimer = nil
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
        if connected { connected = false }
        if fps != 0 { fps = 0 }
        decoder.stopAndFlush(generation: generation)
    }

    func setPreset(_ preset: String) {
        guard let ws = task else { return }
        if let data = try? JSONSerialization.data(withJSONObject: ["type": "profile", "preset": preset]),
           let txt = String(data: data, encoding: .utf8) {
            ws.send(.string(txt)) { _ in }
        }
    }

    func requestKeyframe() {
        decoder.requestKeyframe()
        guard let ws = task else { return }
        ws.send(.string("{\"type\":\"keyframe\"}")) { _ in }
    }

    func sendInput(_ event: [String: Any]) {
        guard let ws = task else { return }
        if let data = try? JSONSerialization.data(withJSONObject: ["type": "input", "event": event]),
           let txt = String(data: data, encoding: .utf8) {
            ws.send(.string(txt)) { _ in }
        }
    }

    private nonisolated func runReceive(ws: URLSessionWebSocketTask, gen: Int) async {
        while !Task.isCancelled {
            do {
                let msg = try await ws.receive()
                if Task.isCancelled { break }
                switch msg {
                case let .string(text):
                    await handleText(text, gen: gen)
                case let .data(data):
                    decoder.enqueuePacket(
                        data,
                        generation: gen,
                        onFirstFrame: { [weak self] in
                            Task { @MainActor [weak self] in
                                guard let self, self.generation == gen, !self.connected else { return }
                                self.connected = true
                            }
                        },
                        onNeedKeyframe: { [weak self] in
                            Task { @MainActor [weak self] in
                                guard let self, self.generation == gen else { return }
                                self.requestKeyframe()
                            }
                        }
                    )
                @unknown default:
                    break
                }
            } catch {
                if !Task.isCancelled {
                    let msg = error.localizedDescription
                    await MainActor.run { [weak self] in
                        guard let self, self.generation == gen, self.task === ws else { return }
                        if self.connected { self.connected = false }
                        if self.error.isEmpty { self.error = msg }
                    }
                }
                break
            }
        }
    }

    private func handleText(_ text: String, gen: Int) {
        guard gen == generation,
              let data = text.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = obj["type"] as? String else { return }
        if type == "meta" {
            let newMeta = H264StreamMeta(
                imageWidth: (obj["imageWidth"] as? NSNumber)?.doubleValue ?? 0,
                imageHeight: (obj["imageHeight"] as? NSNumber)?.doubleValue ?? 0,
                logicalX: (obj["logicalX"] as? NSNumber)?.doubleValue ?? 0,
                logicalY: (obj["logicalY"] as? NSNumber)?.doubleValue ?? 0,
                logicalWidth: (obj["logicalWidth"] as? NSNumber)?.doubleValue ?? 1440,
                logicalHeight: (obj["logicalHeight"] as? NSNumber)?.doubleValue ?? 900,
                fps: max(1, (obj["fps"] as? NSNumber)?.intValue ?? 60),
                codec: obj["codec"] as? String ?? "h264",
                preset: obj["preset"] as? String ?? "",
                bitrateKbps: (obj["bitrateKbps"] as? NSNumber)?.intValue ?? 0
            )
            meta = newMeta
            decoder.updateFPSScale(Int32(newMeta.fps))
            decoder.requestKeyframe()
            if !error.isEmpty { error = "" }
        } else if type == "error" {
            let code = obj["code"] as? String ?? ""
            let msg = obj["error"] as? String ?? ""
            if code == "screen_recording_denied" && !screenRecordingDenied {
                screenRecordingDenied = true
            }
            if error != msg { error = msg }
            if connected { connected = false }
        }
    }
}

private final class H264DecoderWorker: @unchecked Sendable {
    private let queue = DispatchQueue(label: "bothub.h264.decode", qos: .userInteractive)
    private let counterLock = NSLock()
    private var frameCounter = 0

    private weak var displayLayer: AVSampleBufferDisplayLayer?
    private var activeGeneration = 0
    private var formatDesc: CMVideoFormatDescription?
    private var vpsData: Data?
    private var spsData: Data?
    private var ppsData: Data?
    private var needKeyframe = true
    private var reportedConnected = false
    private var ptsCounter: Int64 = 0
    private var fpsScale: Int32 = 60

    func attach(layer: AVSampleBufferDisplayLayer) {
        queue.async { [weak self] in
            self?.displayLayer = layer
        }
    }

    func reset(generation: Int, fpsScale: Int32) {
        counterLock.lock()
        frameCounter = 0
        counterLock.unlock()
        queue.async { [weak self] in
            guard let self else { return }
            self.activeGeneration = generation
            self.fpsScale = max(1, fpsScale)
            self.formatDesc = nil
            self.vpsData = nil
            self.spsData = nil
            self.ppsData = nil
            self.needKeyframe = true
            self.reportedConnected = false
            self.ptsCounter = 0
        }
    }

    func stopAndFlush(generation: Int) {
        counterLock.lock()
        frameCounter = 0
        counterLock.unlock()
        queue.async { [weak self] in
            guard let self else { return }
            self.activeGeneration = generation
            self.formatDesc = nil
            self.vpsData = nil
            self.spsData = nil
            self.ppsData = nil
            self.needKeyframe = true
            self.reportedConnected = false
            self.displayLayer?.sampleBufferRenderer.flush(removingDisplayedImage: true, completionHandler: nil)
        }
    }

    func updateFPSScale(_ fps: Int32) {
        queue.async { [weak self] in
            self?.fpsScale = max(1, fps)
        }
    }

    func requestKeyframe() {
        queue.async { [weak self] in
            self?.needKeyframe = true
        }
    }

    func drainFrameCount() -> Int {
        counterLock.lock()
        let c = frameCounter
        frameCounter = 0
        counterLock.unlock()
        return c
    }

    func enqueuePacket(
        _ packet: Data,
        generation: Int,
        onFirstFrame: @escaping @Sendable () -> Void,
        onNeedKeyframe: @escaping @Sendable () -> Void
    ) {
        queue.async { [weak self] in
            guard let self, self.activeGeneration == generation else { return }
            self.handleBinary(packet, onFirstFrame: onFirstFrame, onNeedKeyframe: onNeedKeyframe)
        }
    }

    private func handleBinary(
        _ packet: Data,
        onFirstFrame: @escaping @Sendable () -> Void,
        onNeedKeyframe: @escaping @Sendable () -> Void
    ) {
        guard packet.count > 2 else { return }
        let kind = packet[packet.startIndex]
        guard kind == 0x02 || kind == 0x03 || kind == 0x04 || kind == 0x05 else { return }
        let isKey = (kind == 0x02 || kind == 0x04)
        let isHEVC = (kind == 0x04 || kind == 0x05)
        if needKeyframe && !isKey { return }

        let payload = packet.dropFirst()
        let naluRanges = parseAnnexBNALURanges(payload)
        if naluRanges.isEmpty { return }

        var sliceRanges: [Range<Data.Index>] = []
        sliceRanges.reserveCapacity(naluRanges.count)
        var vpsChanged = false
        var spsChanged = false
        var ppsChanged = false

        for range in naluRanges {
            guard !range.isEmpty else { continue }
            let first = payload[range.lowerBound]
            if isHEVC {
                let nalType = (first >> 1) & 0x3F
                switch nalType {
                case 32:
                    let nalu = payload[range]
                    if vpsData != nalu { vpsData = Data(nalu); vpsChanged = true }
                case 33:
                    let nalu = payload[range]
                    if spsData != nalu { spsData = Data(nalu); spsChanged = true }
                case 34:
                    let nalu = payload[range]
                    if ppsData != nalu { ppsData = Data(nalu); ppsChanged = true }
                case 0...9, 16...21:
                    sliceRanges.append(range)
                default:
                    break
                }
            } else {
                let nalType = first & 0x1F
                switch nalType {
                case 7:
                    let nalu = payload[range]
                    if spsData != nalu { spsData = Data(nalu); spsChanged = true }
                case 8:
                    let nalu = payload[range]
                    if ppsData != nalu { ppsData = Data(nalu); ppsChanged = true }
                case 5, 1:
                    sliceRanges.append(range)
                default:
                    break
                }
            }
        }

        if isHEVC {
            if isKey, (formatDesc == nil || vpsChanged || spsChanged || ppsChanged),
               let vps = vpsData, let sps = spsData, let pps = ppsData {
                if formatDesc != nil {
                    displayLayer?.sampleBufferRenderer.flush()
                }
                rebuildHEVCFormatDesc(vps: vps, sps: sps, pps: pps)
            }
        } else {
            if isKey, (formatDesc == nil || spsChanged || ppsChanged),
               let sps = spsData, let pps = ppsData {
                if formatDesc != nil {
                    displayLayer?.sampleBufferRenderer.flush()
                }
                rebuildFormatDesc(sps: sps, pps: pps)
            }
        }

        guard !sliceRanges.isEmpty, let fmt = formatDesc else { return }
        if enqueueAVCC(payload: payload, ranges: sliceRanges, formatDesc: fmt, isKey: isKey, onNeedKeyframe: onNeedKeyframe) {
            needKeyframe = false
            counterLock.lock()
            frameCounter += 1
            counterLock.unlock()
            if !reportedConnected {
                reportedConnected = true
                onFirstFrame()
            }
        }
    }

    private func rebuildHEVCFormatDesc(vps: Data, sps: Data, pps: Data) {
        var newDesc: CMVideoFormatDescription?
        let status: OSStatus = vps.withUnsafeBytes { vBuf in
            sps.withUnsafeBytes { sBuf in
                pps.withUnsafeBytes { pBuf in
                    guard let vPtr = vBuf.bindMemory(to: UInt8.self).baseAddress,
                          let sPtr = sBuf.bindMemory(to: UInt8.self).baseAddress,
                          let pPtr = pBuf.bindMemory(to: UInt8.self).baseAddress else {
                        return -1
                    }
                    let ptrs = [vPtr, sPtr, pPtr]
                    let sizes = [vps.count, sps.count, pps.count]
                    return ptrs.withUnsafeBufferPointer { ptrBuf in
                        sizes.withUnsafeBufferPointer { sizeBuf in
                            CMVideoFormatDescriptionCreateFromHEVCParameterSets(
                                allocator: kCFAllocatorDefault,
                                parameterSetCount: 3,
                                parameterSetPointers: ptrBuf.baseAddress!,
                                parameterSetSizes: sizeBuf.baseAddress!,
                                nalUnitHeaderLength: 4,
                                extensions: nil,
                                formatDescriptionOut: &newDesc
                            )
                        }
                    }
                }
            }
        }
        if status == noErr, let newDesc {
            formatDesc = newDesc
        }
    }

    private func rebuildFormatDesc(sps: Data, pps: Data) {
        var newDesc: CMVideoFormatDescription?
        let status: OSStatus = sps.withUnsafeBytes { spsBuf in
            pps.withUnsafeBytes { ppsBuf in
                guard let spsPtr = spsBuf.bindMemory(to: UInt8.self).baseAddress,
                      let ppsPtr = ppsBuf.bindMemory(to: UInt8.self).baseAddress else {
                    return -1
                }
                let ptrs = [spsPtr, ppsPtr]
                let sizes = [sps.count, pps.count]
                return ptrs.withUnsafeBufferPointer { ptrBuf in
                    sizes.withUnsafeBufferPointer { sizeBuf in
                        CMVideoFormatDescriptionCreateFromH264ParameterSets(
                            allocator: kCFAllocatorDefault,
                            parameterSetCount: 2,
                            parameterSetPointers: ptrBuf.baseAddress!,
                            parameterSetSizes: sizeBuf.baseAddress!,
                            nalUnitHeaderLength: 4,
                            formatDescriptionOut: &newDesc
                        )
                    }
                }
            }
        }
        if status == noErr, let newDesc {
            formatDesc = newDesc
        }
    }

    private func enqueueAVCC(
        payload: Data,
        ranges: [Range<Data.Index>],
        formatDesc: CMVideoFormatDescription,
        isKey: Bool,
        onNeedKeyframe: @escaping @Sendable () -> Void
    ) -> Bool {
        var totalLen = 0
        for r in ranges { totalLen += 4 + r.count }

        var avcc = Data(capacity: totalLen)
        for r in ranges {
            var lenBE = UInt32(r.count).bigEndian
            withUnsafeBytes(of: &lenBE) { avcc.append(contentsOf: $0) }
            avcc.append(payload[r])
        }

        var blockBuf: CMBlockBuffer?
        let bStatus = avcc.withUnsafeMutableBytes { raw -> OSStatus in
            var buf: CMBlockBuffer?
            let s1 = CMBlockBufferCreateWithMemoryBlock(
                allocator: kCFAllocatorDefault,
                memoryBlock: nil,
                blockLength: totalLen,
                blockAllocator: kCFAllocatorDefault,
                customBlockSource: nil,
                offsetToData: 0,
                dataLength: totalLen,
                flags: 0,
                blockBufferOut: &buf
            )
            guard s1 == noErr, let buf else { return s1 }
            let s2 = CMBlockBufferReplaceDataBytes(
                with: raw.baseAddress!,
                blockBuffer: buf,
                offsetIntoDestination: 0,
                dataLength: totalLen
            )
            blockBuf = buf
            return s2
        }
        guard bStatus == noErr, let blockBuf else { return false }

        ptsCounter += 1
        var timing = CMSampleTimingInfo(
            duration: CMTime(value: 1, timescale: fpsScale),
            presentationTimeStamp: CMTime(value: ptsCounter, timescale: fpsScale),
            decodeTimeStamp: .invalid
        )
        var sampleSize = totalLen
        var sampleBuf: CMSampleBuffer?
        let sStatus = CMSampleBufferCreateReady(
            allocator: kCFAllocatorDefault,
            dataBuffer: blockBuf,
            formatDescription: formatDesc,
            sampleCount: 1,
            sampleTimingEntryCount: 1,
            sampleTimingArray: &timing,
            sampleSizeEntryCount: 1,
            sampleSizeArray: &sampleSize,
            sampleBufferOut: &sampleBuf
        )
        guard sStatus == noErr, let sampleBuf else { return false }

        if let attachments = CMSampleBufferGetSampleAttachmentsArray(sampleBuf, createIfNecessary: true) {
            let dict = unsafeBitCast(CFArrayGetValueAtIndex(attachments, 0), to: CFMutableDictionary.self)
            CFDictionarySetValue(
                dict,
                Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(),
                Unmanaged.passUnretained(kCFBooleanTrue).toOpaque()
            )
            if !isKey {
                CFDictionarySetValue(
                    dict,
                    Unmanaged.passUnretained(kCMSampleAttachmentKey_NotSync).toOpaque(),
                    Unmanaged.passUnretained(kCFBooleanTrue).toOpaque()
                )
            }
        }

        guard let layer = displayLayer else { return false }
        let renderer = layer.sampleBufferRenderer
        if renderer.status == .failed {
            renderer.flush()
            needKeyframe = true
            onNeedKeyframe()
            return false
        }
        renderer.enqueue(sampleBuf)
        return true
    }
}

private func parseAnnexBNALURanges(_ data: Data) -> [Range<Data.Index>] {
    var out: [Range<Data.Index>] = []
    let base = data.startIndex
    data.withUnsafeBytes { raw in
        guard let ptr = raw.bindMemory(to: UInt8.self).baseAddress else { return }
        let n = raw.count
        var i = 0
        var naluStart = -1
        while i < n {
            var scLen = 0
            if i + 3 < n && ptr[i] == 0 && ptr[i + 1] == 0 && ptr[i + 2] == 0 && ptr[i + 3] == 1 {
                scLen = 4
            } else if i + 2 < n && ptr[i] == 0 && ptr[i + 1] == 0 && ptr[i + 2] == 1 {
                scLen = 3
            }
            if scLen > 0 {
                if naluStart >= 0 && i > naluStart {
                    out.append((base + naluStart)..<(base + i))
                }
                i += scLen
                naluStart = i
            } else {
                i += 1
            }
        }
        if naluStart >= 0 && n > naluStart {
            out.append((base + naluStart)..<(base + n))
        }
    }
    return out
}
