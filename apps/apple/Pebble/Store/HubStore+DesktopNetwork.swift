import Foundation

extension HubStore {
    // MARK: 节点与桌面

    func refreshNodes() async throws {
        guard !baseURL.isEmpty else { return }
        let d: NodesResp = try await callAPI("/api/nodes")
        nodes = d.nodes ?? []
    }

    private func applyRotatedAPIToken(_ newTok: String?) {
        guard let tok = newTok, !tok.isEmpty, tok != authToken else { return }
        authToken = tok
        if !managedMode, let id = activeServerId, let i = servers.firstIndex(where: { $0.id == id }) {
            servers[i].token = tok
            persistServers()
        }
        PushManager.shared.configureConnection(base: baseURL, token: tok)
        if !baseURL.isEmpty {
            startStream(baseURL, tok)
        }
        #if os(macOS)
        if managedMode {
            Task { await DesktopHostManager.shared.refreshStatus() }
        }
        #endif
    }

    func refreshTailcat() async throws {
        tailcat = try await callAPI("/api/tailcat")
    }

    func revealTailcatToken() async throws -> String {
        let d: TailcatStatus = try await callAPI("/api/tailcat?reveal=1")
        return d.apiToken ?? ""
    }

    func setTailcat(_ body: [String: Any]) async throws -> TailcatStatus {
        let res: TailcatStatus = try await callAPI("/api/tailcat", method: "POST", body: body)
        tailcat = res
        applyRotatedAPIToken(res.apiToken)
        return res
    }

    func fetchTailcatQR() async throws -> Data {
        try await rawAPI("/api/tailcat/qr.png")
    }

    func refreshHostTailscale() async throws {
        hostTailscale = try await callAPI("/api/tailscale")
    }

    func setHostTailscale(_ body: [String: Any]) async throws -> HostTailscaleStatus {
        let res: HostTailscaleStatus = try await callAPI("/api/tailscale", method: "POST", body: body)
        hostTailscale = res
        applyRotatedAPIToken(res.apiToken)
        return res
    }

    func fetchHostTailscaleQR() async throws -> Data {
        try await rawAPI("/api/tailscale/qr.png")
    }

    func refreshMobileTailscale(includePeers: Bool = true) async {
        let st = await Tunnel.tailscaleStatus(includePeers: includePeers)
        mobileTailscale = st
        if st.state == "running" {
            tsBound = true
        }
    }

    func bindTailscale(authKey: String = "", controlUrl: String = "") async {
        tsBinding = true
        defer { tsBinding = false }
        let ctrl = controlUrl.trimmingCharacters(in: .whitespacesAndNewlines)
        let key = authKey.trimmingCharacters(in: .whitespacesAndNewlines)
        var st = await Tunnel.tailscaleStart(controlUrl: ctrl, authKey: key)
        mobileTailscale = st
        var openedAuth = ""
        if st.state == "needs_login", let auth = st.authUrl, !auth.isEmpty, let u = URL(string: auth) {
            openedAuth = auth
            openExternalURL(u)
        }
        let deadline = Date().addingTimeInterval(180)
        while (st.state == "starting" || st.state == "needs_login") && Date() < deadline {
            try? await Task.sleep(for: .milliseconds(1500))
            st = await Tunnel.tailscaleStatus(includePeers: true)
            mobileTailscale = st
            if st.state == "needs_login", let auth = st.authUrl, !auth.isEmpty, auth != openedAuth, let u = URL(string: auth) {
                openedAuth = auth
                openExternalURL(u)
            }
        }
        if st.state == "running" {
            tsBound = true
            tsControlUrl = ctrl
            if (st.peers ?? []).isEmpty {
                await refreshMobileTailscale(includePeers: true)
            }
        }
    }

    func logoutTailscale() async {
        _ = await Tunnel.tailscaleLogout()
        tsBound = false
        tsControlUrl = ""
        mobileTailscale = nil
    }

    @discardableResult
    func refreshDesktopTargets() async throws -> [DesktopTarget] {
        let d: TargetsResp = try await callAPI("/api/desktop/targets")
        desktopTargets = d.targets ?? []
        if let at = d.activeTarget {
            activeDesktopTarget = at
        }
        takeovers = d.takeovers ?? []
        return desktopTargets
    }

    func setActiveDesktopTarget(_ target: String) async throws {
        let res: ActiveTargetResp = try await callAPI("/api/desktop/target", method: "PUT", body: ["target": target])
        if let at = res.activeTarget {
            activeDesktopTarget = at
        }
    }

    func fetchDesktopState(_ target: String) async throws -> DesktopState {
        let d: DesktopState = try await callAPI("/api/desktop/state?target=\(enc(target))")
        takeovers = d.takeovers ?? []
        return d
    }

    func fetchDesktopPermissions(_ target: String) async throws -> DesktopPermissionsResp {
        try await callAPI("/api/desktop/permissions?target=\(enc(target))")
    }

    func requestDesktopPermission(target: String, permission: String, openSettings: Bool = false) async throws -> DesktopPermissionRequestResp {
        try await callAPI("/api/desktop/permissions/request", method: "POST",
                           body: ["target": target, "permission": permission, "openSettings": openSettings])
    }

    func fetchDesktopFrame(_ target: String) async throws -> DesktopFrame {
        try await callAPI("/api/desktop/frame?target=\(enc(target))&windowId=&maxDim=1280&quality=72")
    }

    func makeDesktopWSRequest(target: String, preset: String? = nil, codec: String = "hevc") -> URLRequest? {
        guard !baseURL.isEmpty, var comps = URLComponents(string: baseURL) else { return nil }
        comps.scheme = (comps.scheme?.lowercased() == "https") ? "wss" : "ws"
        comps.path = "/api/desktop/stream/ws"
        var q = [
            URLQueryItem(name: "target", value: target),
            URLQueryItem(name: "codec", value: codec.isEmpty ? "hevc" : codec),
        ]
        if let preset, !preset.isEmpty {
            q.append(URLQueryItem(name: "preset", value: preset))
        }
        comps.queryItems = q
        guard let url = comps.url else { return nil }
        var req = URLRequest(url: url)
        APIClient.attachClientHeaders(&req)
        if !authToken.isEmpty {
            req.setValue("Bearer \(authToken)", forHTTPHeaderField: "Authorization")
        }
        return req
    }

    func sendDesktopInput(_ body: [String: Any]) async throws {
        _ = try await rawAPI("/api/desktop/input", method: "POST", body: body)
    }

    func resolveTakeover(_ id: String) async throws {
        let r: TakeoversResp = try await callAPI("/api/desktop/takeover/resolve", method: "POST",
                                                  body: ["id": id, "note": L.t("takeoverResolvedNote")])
        takeovers = r.items ?? []
    }

    func replyQuestion(id: String, answers: [String: Any], cancelled: Bool) async throws {
        let r: QuestionsResp = try await callAPI("/api/questions/\(enc(id))/reply", method: "POST",
                                                  body: ["answers": answers, "cancelled": cancelled])
        questions = r.items ?? []
    }

    func replyApproval(id: String, decision: String, optionId: String? = nil, reason: String? = nil) async throws {
        var body: [String: Any] = ["decision": decision]
        if let optionId, !optionId.isEmpty { body["optionId"] = optionId }
        if let reason, !reason.isEmpty { body["reason"] = reason }
        let r: ApprovalsResp = try await api.call("/api/approvals/\(enc(id))/reply", method: "POST", body: body)
        approvals = r.items ?? []
        LiveActivityManager.shared.handleApprovals(approvals, bots: bots)
    }
}
