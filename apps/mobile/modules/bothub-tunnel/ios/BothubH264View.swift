import AVFoundation
import CoreMedia
import ExpoModulesCore
import UIKit

public class BothubH264View: ExpoView {
  let onMeta = EventDispatcher()
  let onStatus = EventDispatcher()
  let onStreamError = EventDispatcher()

  private let displayLayer = AVSampleBufferDisplayLayer()
  private var wsTask: URLSessionWebSocketTask?
  private var receiveTask: Task<Void, Never>?
  private var fpsTimer: Timer?
  private var frameCounter = 0

  private var streamURL: String = ""
  private var authToken: String = ""
  private var preset: String = "auto"
  private var isActive: Bool = false

  private var formatDesc: CMVideoFormatDescription?
  private var spsData: Data?
  private var ppsData: Data?
  private var needKeyframe = true
  private var ptsCounter: Int64 = 0

  required init(appContext: AppContext? = nil) {
    super.init(appContext: appContext)
    clipsToBounds = true
    backgroundColor = UIColor(red: 17 / 255, green: 17 / 255, blue: 16 / 255, alpha: 1)
    displayLayer.videoGravity = .resizeAspect
    layer.addSublayer(displayLayer)
  }

  public override func layoutSubviews() {
    super.layoutSubviews()
    displayLayer.frame = bounds
  }

  func setUrl(_ value: String) {
    if streamURL != value {
      streamURL = value
      reconnectIfNeeded()
    }
  }

  func setToken(_ value: String) {
    if authToken != value {
      authToken = value
      reconnectIfNeeded()
    }
  }

  func setPreset(_ value: String) {
    let next = value.isEmpty ? "auto" : value
    if preset != next {
      preset = next
      if let ws = wsTask,
         let data = try? JSONSerialization.data(withJSONObject: ["type": "profile", "preset": next]),
         let txt = String(data: data, encoding: .utf8) {
        ws.send(.string(txt)) { _ in }
      }
    }
  }

  func setActive(_ value: Bool) {
    if isActive != value {
      isActive = value
      reconnectIfNeeded()
    }
  }

  func sendInput(_ eventJson: String) {
    guard let ws = wsTask,
          let evData = eventJson.data(using: .utf8),
          let evObj = try? JSONSerialization.jsonObject(with: evData),
          let data = try? JSONSerialization.data(withJSONObject: ["type": "input", "event": evObj]),
          let txt = String(data: data, encoding: .utf8) else { return }
    ws.send(.string(txt)) { _ in }
  }

  private func reconnectIfNeeded() {
    disconnect()
    guard isActive, !streamURL.isEmpty, let url = URL(string: streamURL) else { return }

    var req = URLRequest(url: url)
    if !authToken.isEmpty {
      req.setValue("Bearer \(authToken)", forHTTPHeaderField: "Authorization")
    }
    let ws = URLSession.shared.webSocketTask(with: req)
    wsTask = ws
    needKeyframe = true
    frameCounter = 0
    ws.resume()

    DispatchQueue.main.async { [weak self] in
      self?.fpsTimer = Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { [weak self] _ in
        guard let self else { return }
        let f = self.frameCounter
        self.frameCounter = 0
        self.onStatus(["connected": !self.needKeyframe, "fps": f])
      }
    }

    receiveTask = Task { [weak self] in
      while !Task.isCancelled {
        do {
          let msg = try await ws.receive()
          guard let self, self.wsTask === ws else { break }
          switch msg {
          case let .string(txt):
            self.handleText(txt)
          case let .data(data):
            self.handleBinary(data)
          @unknown default:
            break
          }
        } catch {
          if !Task.isCancelled, let self, self.wsTask === ws {
            DispatchQueue.main.async {
              self.onStatus(["connected": false, "fps": 0])
              self.onStreamError(["code": "", "error": error.localizedDescription])
            }
          }
          break
        }
      }
    }
  }

  private func disconnect() {
    fpsTimer?.invalidate()
    fpsTimer = nil
    receiveTask?.cancel()
    receiveTask = nil
    wsTask?.cancel(with: .goingAway, reason: nil)
    wsTask = nil
    formatDesc = nil
    spsData = nil
    ppsData = nil
    needKeyframe = true
    DispatchQueue.main.async { [weak self] in
      self?.displayLayer.sampleBufferRenderer.flush(removingDisplayedImage: true, completionHandler: nil)
    }
  }

  private func handleText(_ text: String) {
    guard let data = text.data(using: .utf8),
          let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
          let type = obj["type"] as? String else { return }
    if type == "meta" {
      needKeyframe = true
      DispatchQueue.main.async { [weak self] in
        self?.onMeta([
          "imageWidth": obj["imageWidth"] ?? 0,
          "imageHeight": obj["imageHeight"] ?? 0,
          "logicalX": obj["logicalX"] ?? 0,
          "logicalY": obj["logicalY"] ?? 0,
          "logicalWidth": obj["logicalWidth"] ?? 1440,
          "logicalHeight": obj["logicalHeight"] ?? 900,
          "preset": obj["preset"] ?? "",
          "bitrateKbps": obj["bitrateKbps"] ?? 0,
        ])
      }
    } else if type == "error" {
      DispatchQueue.main.async { [weak self] in
        self?.onStreamError([
          "code": obj["code"] ?? "",
          "error": obj["error"] ?? "stream error",
        ])
      }
    }
  }

  private func handleBinary(_ packet: Data) {
    guard packet.count > 2 else { return }
    let kind = packet[packet.startIndex]
    guard kind == 0x02 || kind == 0x03 else { return }
    let isKey = (kind == 0x02)
    if needKeyframe && !isKey { return }

    let nalus = splitAnnexB(packet.dropFirst())
    var slices: [Data] = []
    for n in nalus {
      guard let f = n.first else { continue }
      let t = f & 0x1F
      if t == 7 { spsData = n }
      else if t == 8 { ppsData = n }
      else if t == 5 || t == 1 { slices.append(n) }
    }
    if isKey, let sps = spsData, let pps = ppsData {
      var desc: CMVideoFormatDescription?
      _ = sps.withUnsafeBytes { sBuf in
        pps.withUnsafeBytes { pBuf in
          let ptrs = [sBuf.bindMemory(to: UInt8.self).baseAddress!, pBuf.bindMemory(to: UInt8.self).baseAddress!]
          let sizes = [sps.count, pps.count]
          return CMVideoFormatDescriptionCreateFromH264ParameterSets(
            allocator: kCFAllocatorDefault,
            parameterSetCount: 2,
            parameterSetPointers: ptrs,
            parameterSetSizes: sizes,
            nalUnitHeaderLength: 4,
            formatDescriptionOut: &desc
          )
        }
      }
      if let desc { formatDesc = desc }
    }
    guard !slices.isEmpty, let fmt = formatDesc else { return }
    var avcc = Data()
    for n in slices {
      var len = UInt32(n.count).bigEndian
      withUnsafeBytes(of: &len) { avcc.append(contentsOf: $0) }
      avcc.append(n)
    }
    var block: CMBlockBuffer?
    let total = avcc.count
    let st = avcc.withUnsafeMutableBytes { raw -> OSStatus in
      var b: CMBlockBuffer?
      let s1 = CMBlockBufferCreateWithMemoryBlock(allocator: kCFAllocatorDefault, memoryBlock: nil, blockLength: total, blockAllocator: kCFAllocatorDefault, customBlockSource: nil, offsetToData: 0, dataLength: total, flags: 0, blockBufferOut: &b)
      guard s1 == noErr, let b else { return s1 }
      let s2 = CMBlockBufferReplaceDataBytes(with: raw.baseAddress!, blockBuffer: b, offsetIntoDestination: 0, dataLength: total)
      block = b
      return s2
    }
    guard st == noErr, let block else { return }
    ptsCounter += 1
    var timing = CMSampleTimingInfo(duration: CMTime(value: 1, timescale: 30), presentationTimeStamp: CMTime(value: ptsCounter, timescale: 30), decodeTimeStamp: .invalid)
    var sz = total
    var sample: CMSampleBuffer?
    guard CMSampleBufferCreateReady(allocator: kCFAllocatorDefault, dataBuffer: block, formatDescription: fmt, sampleCount: 1, sampleTimingEntryCount: 1, sampleTimingArray: &timing, sampleSizeEntryCount: 1, sampleSizeArray: &sz, sampleBufferOut: &sample) == noErr, let sample else { return }
    if let arr = CMSampleBufferGetSampleAttachmentsArray(sample, createIfNecessary: true) {
      let dict = unsafeBitCast(CFArrayGetValueAtIndex(arr, 0), to: CFMutableDictionary.self)
      CFDictionarySetValue(dict, Unmanaged.passUnretained(kCMSampleAttachmentKey_DisplayImmediately).toOpaque(), Unmanaged.passUnretained(kCFBooleanTrue).toOpaque())
    }
    DispatchQueue.main.async { [weak self] in
      guard let self else { return }
      let r = self.displayLayer.sampleBufferRenderer
      if r.status == .failed { r.flush() }
      r.enqueue(sample)
      if self.needKeyframe {
        self.needKeyframe = false
        self.onStatus(["connected": true, "fps": self.frameCounter])
      }
      self.frameCounter += 1
    }
  }

  private func splitAnnexB(_ data: Data) -> [Data] {
    var out: [Data] = []
    let bytes = [UInt8](data)
    var i = 0
    var start = -1
    while i < bytes.count {
      let sc = (i + 3 < bytes.count && bytes[i] == 0 && bytes[i+1] == 0 && bytes[i+2] == 0 && bytes[i+3] == 1) ? 4 :
        (i + 2 < bytes.count && bytes[i] == 0 && bytes[i+1] == 0 && bytes[i+2] == 1) ? 3 : 0
      if sc > 0 {
        if start >= 0 && i > start { out.append(Data(bytes[start..<i])) }
        i += sc
        start = i
      } else { i += 1 }
    }
    if start >= 0 && bytes.count > start { out.append(Data(bytes[start..<bytes.count])) }
    return out
  }
}
