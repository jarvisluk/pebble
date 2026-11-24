import AVFoundation
import CoreMedia
import Foundation
import Observation
import SwiftUI
import UIKit

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

@MainActor
@Observable
final class H264StreamController {
    var meta: H264StreamMeta?
    var fps: Int = 0
    var connected: Bool = false
    var error: String = ""
    var screenRecordingDenied: Bool = false

    @ObservationIgnored private weak var displayLayer: AVSampleBufferDisplayLayer?
    @ObservationIgnored private var task: URLSessionWebSocketTask?
    @ObservationIgnored private var receiveLoop: Task<Void, Never>?
    @ObservationIgnored private var fpsTimer: Task<Void, Never>?
    @ObservationIgnored private var frameCounter = 0
    @ObservationIgnored private var formatDesc: CMVideoFormatDescription?
    @ObservationIgnored private var vpsData: Data?
    @ObservationIgnored private var spsData: Data?
    @ObservationIgnored private var ppsData: Data?
    @ObservationIgnored private var needKeyframe = true
    @ObservationIgnored private var ptsCounter: Int64 = 0
    @ObservationIgnored private var generation: Int = 0

    func attach(layer: AVSampleBufferDisplayLayer) {
        displayLayer = layer
    }

    func start(request: URLRequest) {
        stop()
        generation += 1
        let gen = generation
        error = ""
        screenRecordingDenied = false
        needKeyframe = true
        let ws = URLSession.shared.webSocketTask(with: request)
        task = ws
        ws.resume()

        fpsTimer = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 1_000_000_000)
                guard let self, !Task.isCancelled, self.generation == gen else { break }
                self.fps = self.frameCounter
                self.frameCounter = 0
            }
        }

        receiveLoop = Task { [weak self] in
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
        connected = false
        fps = 0
        frameCounter = 0
        formatDesc = nil
        vpsData = nil
        spsData = nil
        ppsData = nil
        needKeyframe = true
        displayLayer?.sampleBufferRenderer.flush(removingDisplayedImage: true, completionHandler: nil)
    }

    func setPreset(_ preset: String) {
        guard let ws = task else { return }
        if let data = try? JSONSerialization.data(withJSONObject: ["type": "profile", "preset": preset]),
           let txt = String(data: data, encoding: .utf8) {
            ws.send(.string(txt)) { _ in }
        }
    }

    func requestKeyframe() {
        needKeyframe = true
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

    private func runReceive(ws: URLSessionWebSocketTask, gen: Int) async {
        while !Task.isCancelled && gen == generation {
            do {
                let msg = try await ws.receive()
                if Task.isCancelled || gen != generation { break }
                switch msg {
                case let .string(text):
                    handleText(text)
                case let .data(data):
                    handleBinary(data)
                @unknown default:
                    break
                }
            } catch {
                if !Task.isCancelled && gen == generation && task === ws {
                    connected = false
                    if self.error.isEmpty {
                        self.error = error.localizedDescription
                    }
                }
                break
            }
        }
    }

    private func handleText(_ text: String) {
        guard let data = text.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let type = obj["type"] as? String else { return }
        if type == "meta" {
            meta = H264StreamMeta(
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
            needKeyframe = true
            error = ""
        } else if type == "error" {
            let code = obj["code"] as? String ?? ""
            let msg = obj["error"] as? String ?? ""
            if code == "screen_recording_denied" {
                screenRecordingDenied = true
            }
            error = msg
            connected = false
        }
    }

    private func handleBinary(_ packet: Data) {
        guard packet.count > 2 else { return }
        let kind = packet[packet.startIndex]
        guard kind == 0x02 || kind == 0x03 || kind == 0x04 || kind == 0x05 else { return }
        let isKey = (kind == 0x02 || kind == 0x04)
        let isHEVC = (kind == 0x04 || kind == 0x05)
        if needKeyframe && !isKey { return }

        let nalus = parseAnnexBNALUs(packet.dropFirst())
        if nalus.isEmpty { return }

        var sliceNALUs: [Data] = []
        var vpsChanged = false
        var spsChanged = false
        var ppsChanged = false
        for nalu in nalus {
            guard let first = nalu.first else { continue }
            if isHEVC {
                let nalType = (first >> 1) & 0x3F
                switch nalType {
                case 32:
                    if vpsData != nalu { vpsData = nalu; vpsChanged = true }
                case 33:
                    if spsData != nalu { spsData = nalu; spsChanged = true }
                case 34:
                    if ppsData != nalu { ppsData = nalu; ppsChanged = true }
                case 0...9, 16...21:
                    sliceNALUs.append(nalu)
                default:
                    break
                }
            } else {
                let nalType = first & 0x1F
                switch nalType {
                case 7:
                    if spsData != nalu { spsData = nalu; spsChanged = true }
                case 8:
                    if ppsData != nalu { ppsData = nalu; ppsChanged = true }
                case 5, 1:
                    sliceNALUs.append(nalu)
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
            if isKey, (formatDesc == nil || spsChanged || ppsChanged), let sps = spsData, let pps = ppsData {
                if formatDesc != nil {
                    displayLayer?.sampleBufferRenderer.flush()
                }
                rebuildFormatDesc(sps: sps, pps: pps)
            }
        }
        guard !sliceNALUs.isEmpty, let fmt = formatDesc else { return }
        if enqueueAVCC(nalus: sliceNALUs, formatDesc: fmt, isKey: isKey) {
            needKeyframe = false
            connected = true
            frameCounter += 1
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

    private func enqueueAVCC(nalus: [Data], formatDesc: CMVideoFormatDescription, isKey: Bool) -> Bool {
        var avcc = Data()
        for nalu in nalus {
            var lenBE = UInt32(nalu.count).bigEndian
            withUnsafeBytes(of: &lenBE) { avcc.append(contentsOf: $0) }
            avcc.append(nalu)
        }
        var blockBuf: CMBlockBuffer?
        let totalLen = avcc.count
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
        let fpsScale = Int32(max(1, meta?.fps ?? 60))
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
            requestKeyframe()
            return false
        }
        renderer.enqueue(sampleBuf)
        return true
    }
}

private func parseAnnexBNALUs(_ data: Data) -> [Data] {
    var out: [Data] = []
    let bytes = [UInt8](data)
    let n = bytes.count
    var i = 0
    var naluStart = -1

    while i < n {
        var scLen = 0
        if i + 3 < n && bytes[i] == 0 && bytes[i + 1] == 0 && bytes[i + 2] == 0 && bytes[i + 3] == 1 {
            scLen = 4
        } else if i + 2 < n && bytes[i] == 0 && bytes[i + 1] == 0 && bytes[i + 2] == 1 {
            scLen = 3
        }
        if scLen > 0 {
            if naluStart >= 0 && i > naluStart {
                out.append(Data(bytes[naluStart..<i]))
            }
            i += scLen
            naluStart = i
        } else {
            i += 1
        }
    }
    if naluStart >= 0 && n > naluStart {
        out.append(Data(bytes[naluStart..<n]))
    }
    return out
}
