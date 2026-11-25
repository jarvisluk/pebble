#if os(macOS)
import AppKit
import Foundation
import Observation

struct DesktopHostSnapshot: Codable, Equatable {
    var mode: String = ""
    var link: String = ""
    var dataDir: String = ""
    var activeUrl: String = ""
    var activeToken: String = ""
    var status: String = ""
    var statusState: String = ""
    var statusError: String = ""
    var allowNode: Bool = false
    var nodeConnected: Bool = false
    var nodeStatus: String = "disabled"
    var nodeError: String = ""
    var nodeId: String = ""
    var nodeName: String = ""
    var nodePerms: [String] = []
    var canFailover: Bool = false
    var canFailback: Bool = false
    var tailscaleBound: Bool = false
    var tailscaleControlUrl: String = ""
    var daemonProtocol: Int? = nil
}

private struct DaemonHandshake: Codable {
    var url: String
    var token: String
    var dataDir: String
    var protocolVersion: Int?
    var capabilities: [String]?
    var version: String?
}

@MainActor
@Observable
final class DesktopHostManager {
    static let shared = DesktopHostManager()

    var snapshot = DesktopHostSnapshot()
    var tailscale: MobileTailscaleStatus?
    var ready = false
    var daemonError = ""
    var tsBinding = false
    var daemonProtocol: Int = 0

    var daemonOutdated: Bool {
        let p = snapshot.daemonProtocol ?? daemonProtocol
        return ready && p > 0 && p < ClientProtocol.current
    }

    @ObservationIgnored private var process: Process?
    @ObservationIgnored private var stdinPipe: Pipe?
    @ObservationIgnored private var controlURL = ""
    @ObservationIgnored private var controlToken = ""
    @ObservationIgnored private var sseTask: Task<Void, Never>?
    @ObservationIgnored private var starting = false

    private init() {}

    func startDaemonIfNeeded() {
        guard process == nil, !starting else { return }
        starting = true
        daemonError = ""

        guard let binPath = resolveBinaryPath() else {
            starting = false
            daemonError = "未找到 pebble 可执行文件"
            return
        }

        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: binPath)
        proc.arguments = ["host-daemon"]
        proc.environment = ProcessInfo.processInfo.environment

        let outPipe = Pipe()
        let inPipe = Pipe()
        proc.standardOutput = outPipe
        proc.standardInput = inPipe
        proc.standardError = FileHandle.standardError

        proc.terminationHandler = { [weak self] p in
            Task { @MainActor in
                guard let self else { return }
                let wasRunning = self.process === p
                if wasRunning {
                    self.process = nil
                    self.stdinPipe = nil
                    self.sseTask?.cancel()
                    self.sseTask = nil
                    self.ready = false
                    self.starting = false
                    self.daemonProtocol = 0
                    if p.terminationStatus != 0 {
                        self.daemonError = "host-daemon 异常退出（code \(p.terminationStatus)）"
                    }
                }
            }
        }

        do {
            try proc.run()
        } catch {
            starting = false
            daemonError = error.localizedDescription
            return
        }

        process = proc
        stdinPipe = inPipe

        let handle = outPipe.fileHandleForReading
        Task.detached { [weak self] in
            var buf = Data()
            while true {
                let chunk = handle.availableData
                if chunk.isEmpty { break }
                buf.append(chunk)
                if let nl = buf.firstIndex(of: 0x0A) {
                    let lineData = buf[..<nl]
                    if let hs = try? JSONDecoder().decode(DaemonHandshake.self, from: lineData) {
                        await MainActor.run { [weak self] in
                            self?.onHandshake(hs)
                        }
                        break
                    }
                    buf.removeSubrange(...nl)
                }
            }
        }
    }

    private func onHandshake(_ hs: DaemonHandshake) {
        controlURL = hs.url
        controlToken = hs.token
        snapshot.dataDir = hs.dataDir
        daemonProtocol = hs.protocolVersion ?? 1
        ready = true
        starting = false
        startControlStream()
        Task {
            await refreshStatus()
            if snapshot.tailscaleBound {
                await refreshTailscale(includePeers: true)
            }
        }
    }

    func shutdown() {
        sseTask?.cancel()
        sseTask = nil
        if let pipe = stdinPipe {
            try? pipe.fileHandleForWriting.close()
            stdinPipe = nil
        }
        if let proc = process, proc.isRunning {
            proc.terminate()
        }
        process = nil
        ready = false
        starting = false
        daemonProtocol = 0
    }

    private func resolveBinaryPath() -> String? {
        let env = ProcessInfo.processInfo.environment
        for key in ["PEBBLE_DAEMON_BIN", "BOT_HUB_DAEMON_BIN"] {
            if let custom = env[key], !custom.isEmpty, FileManager.default.isExecutableFile(atPath: custom) {
                return custom
            }
        }
        for rel in ["bin/pebble", "bin/bot-hub"] {
            if let bundled = Bundle.main.resourceURL?.appendingPathComponent(rel).path,
               FileManager.default.isExecutableFile(atPath: bundled) {
                return bundled
            }
        }
        for cand in ["/opt/homebrew/bin/pebble", "/usr/local/bin/pebble", "/opt/homebrew/bin/bot-hub", "/usr/local/bin/bot-hub"] {
            if FileManager.default.isExecutableFile(atPath: cand) {
                return cand
            }
        }
        return nil
    }

    private func controlRequest(_ path: String, method: String = "GET", body: [String: Any]? = nil) async throws -> Data {
        guard !controlURL.isEmpty, let url = URL(string: controlURL + path) else {
            throw HubError(message: "桌面宿主尚未就绪")
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.timeoutInterval = 30
        req.setValue("Bearer \(controlToken)", forHTTPHeaderField: "Authorization")
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let (data, resp) = try await hubAPISession.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(code) else {
            let msg = String(data: data, encoding: .utf8) ?? "HTTP \(code)"
            throw HubError(message: msg, status: code)
        }
        return data
    }

    func refreshStatus() async {
        guard let data = try? await controlRequest("/control/status"),
              let snap = try? JSONDecoder().decode(DesktopHostSnapshot.self, from: data) else { return }
        snapshot = snap
    }

    func startLocal() async {
        daemonError = ""
        do {
            _ = try await controlRequest("/control/start", method: "POST", body: ["mode": "local"])
        } catch {
            daemonError = error.localizedDescription
        }
    }

    func startRemote(link: String, token: String = "") async {
        daemonError = ""
        do {
            _ = try await controlRequest("/control/start", method: "POST", body: [
                "mode": "remote",
                "link": link,
                "remoteToken": token,
            ])
        } catch {
            daemonError = error.localizedDescription
        }
    }

    func resetMode() async {
        do {
            _ = try await controlRequest("/control/reset", method: "POST")
        } catch {
            daemonError = error.localizedDescription
        }
    }

    func setLocalNode(enabled: Bool) async {
        do {
            _ = try await controlRequest("/control/local-node", method: "POST", body: ["enabled": enabled])
            await refreshStatus()
        } catch {
            daemonError = error.localizedDescription
        }
    }

    func failoverToLocal() async {
        do {
            _ = try await controlRequest("/control/failover", method: "POST")
        } catch {
            daemonError = error.localizedDescription
        }
    }

    func failbackToRemote() async {
        do {
            _ = try await controlRequest("/control/failback", method: "POST")
        } catch {
            daemonError = error.localizedDescription
        }
    }

    func openDataDir() {
        let dir = snapshot.dataDir
        guard !dir.isEmpty else { return }
        NSWorkspace.shared.open(URL(fileURLWithPath: dir, isDirectory: true))
    }

    func refreshTailscale(includePeers: Bool = true) async {
        let path = includePeers ? "/control/tailscale?peers=1" : "/control/tailscale"
        if let data = try? await controlRequest(path),
           let st = try? JSONDecoder().decode(MobileTailscaleStatus.self, from: data) {
            tailscale = st
        }
    }

    func bindTailscale(authKey: String = "", controlUrl: String = "") async {
        tsBinding = true
        defer { tsBinding = false }
        if let data = try? await controlRequest("/control/tailscale/start", method: "POST", body: [
            "authKey": authKey,
            "controlUrl": controlUrl,
        ]), let st = try? JSONDecoder().decode(MobileTailscaleStatus.self, from: data) {
            tailscale = st
        }
        let deadline = Date().addingTimeInterval(180)
        while let cur = tailscale, (cur.state == "starting" || cur.state == "needs_login"), Date() < deadline {
            try? await Task.sleep(for: .milliseconds(1500))
            await refreshTailscale(includePeers: true)
        }
    }

    func logoutTailscale() async {
        if let data = try? await controlRequest("/control/tailscale/logout", method: "POST"),
           let st = try? JSONDecoder().decode(MobileTailscaleStatus.self, from: data) {
            tailscale = st
        }
    }

    private func startControlStream() {
        sseTask?.cancel()
        let base = controlURL
        let tok = controlToken
        sseTask = Task.detached { [weak self] in
            while !Task.isCancelled {
                do {
                    guard let url = URL(string: base + "/control/events") else { return }
                    var req = URLRequest(url: url)
                    req.timeoutInterval = 70
                    req.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    req.setValue("Bearer \(tok)", forHTTPHeaderField: "Authorization")
                    let (bytes, resp) = try await hubAPISession.bytes(for: req)
                    let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
                    if code != 200 { throw HubError(message: "HTTP \(code)") }
                    var buf: [UInt8] = []
                    var event = "message"
                    var dataLines: [String] = []
                    for try await byte in bytes {
                        if byte != 10 { buf.append(byte); continue }
                        var line = String(decoding: buf, as: UTF8.self)
                        buf.removeAll(keepingCapacity: true)
                        if line.hasSuffix("\r") { line.removeLast() }
                        if line.isEmpty {
                            let joined = dataLines.joined(separator: "\n")
                            if event == "status", let data = joined.data(using: .utf8),
                               let snap = try? JSONDecoder().decode(DesktopHostSnapshot.self, from: data) {
                                await MainActor.run { [weak self] in
                                    self?.snapshot = snap
                                }
                            } else if event == "open_url", let data = joined.data(using: .utf8),
                                      let obj = try? JSONSerialization.jsonObject(with: data) as? [String: String],
                                      let raw = obj["url"], let u = URL(string: raw) {
                                _ = await MainActor.run {
                                    NSWorkspace.shared.open(u)
                                }
                            }
                            event = "message"
                            dataLines = []
                        } else if line.hasPrefix(":") {
                            continue
                        } else if line.hasPrefix("event:") {
                            event = line.dropFirst(6).trimmingCharacters(in: .whitespaces)
                        } else if line.hasPrefix("data:") {
                            dataLines.append(String(line.dropFirst(5).drop(while: { $0 == " " })))
                        }
                    }
                } catch {
                    if Task.isCancelled { return }
                }
                try? await Task.sleep(for: .milliseconds(600))
            }
        }
    }
}
#endif
