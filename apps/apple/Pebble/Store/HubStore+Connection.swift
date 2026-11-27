import Foundation

extension HubStore {
    // MARK: 连接

    func connectProfile(_ profile: ServerProfile) async {
        connSeq += 1
        let seq = connSeq
        stream.stop()
        online = false
        usingStandby = false
        connState = .connecting
        connError = ""
        serverInfo = nil
        unsupportedCaps = []
        dismissedCompatProto = 0
        updateStatus = nil
        updateChecking = false

        do {
            try await connectEndpoint(profile: profile, rawAddress: profile.address, rawToken: profile.token, isStandby: false, seq: seq)
        } catch {
            guard seq == connSeq else { return }
            if (error as? HubError)?.unauthorized == true {
                handleConnectError(error)
                return
            }
            var candidates: [(String, String)] = []
            let sbAddr = (profile.standbyAddress ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if !sbAddr.isEmpty {
                candidates.append((sbAddr, profile.standbyToken ?? ""))
            }
            for fb in (profile.fallbackHosts ?? []) {
                let addr = fb.address.trimmingCharacters(in: .whitespacesAndNewlines)
                if !addr.isEmpty && addr != sbAddr {
                    candidates.append((addr, fb.token ?? ""))
                }
            }
            var lastErr: Error = error
            for (addr, tok) in candidates {
                do {
                    try await connectEndpoint(profile: profile, rawAddress: addr, rawToken: tok, isStandby: true, seq: seq)
                    return
                } catch {
                    guard seq == connSeq else { return }
                    lastErr = error
                    if (error as? HubError)?.unauthorized == true {
                        break
                    }
                }
            }
            handleConnectError(lastErr)
        }
    }

    private func handleConnectError(_ error: Error) {
        usingStandby = false
        if let e = error as? HubError, e.unauthorized {
            connState = .unauthorized
            connError = e.message
        } else {
            connState = .error
            connError = errText(error, L.t("connFailed"))
        }
    }

    private func connectEndpoint(profile: ServerProfile, rawAddress: String, rawToken: String, isStandby: Bool, seq: Int) async throws {
        let norm = normalizeAddress(rawAddress)
        var tok = rawToken.isEmpty ? norm.token : rawToken
        var url = norm.cleanAddress
        do {
            if norm.isTailscale {
                if mobileTailscale?.state != "running" {
                    let st = await Tunnel.tailscaleStart(controlUrl: tsControlUrl)
                    mobileTailscale = st
                    if st.state == "needs_login", let auth = st.authUrl, let u = URL(string: auth) {
                        openExternalURL(u)
                    }
                }
                let r = try await Tunnel.start(link: norm.cleanAddress, token: tok)
                guard seq == connSeq else { return }
                url = r.url
                if tok.isEmpty, !r.token.isEmpty { tok = r.token }
            } else if norm.isTailcat {
                let r = try await Tunnel.start(link: norm.cleanAddress, token: tok)
                guard seq == connSeq else { return }
                url = r.url
                if tok.isEmpty, !r.token.isEmpty { tok = r.token }
            } else {
                await Tunnel.stop()
            }
            guard seq == connSeq else { return }
            if !isStandby, tok != profile.token || norm.cleanAddress != profile.address,
               let i = servers.firstIndex(where: { $0.id == profile.id }) {
                servers[i].address = norm.cleanAddress
                servers[i].token = tok
                persistServers()
            }
            isTailcat = norm.isTailcat
            isTailscale = norm.isTailscale
            baseURL = url
            authToken = tok

            let client = APIClient(base: url, token: tok)
            let data: BotsResp = try await client.call("/api/bots")
            guard seq == connSeq else { return }
            serverInfo = data.server
            unsupportedCaps = []
            let list = data.bots ?? []
            bots = list
            msgs = [:]; last = [:]; unread = [:]; tools = [:]; routines = [:]
            tasks = [:]; taskEvents = [:]; taskTools = [:]; taskStreamText = [:]
            resetModelsCache()
            usingStandby = isStandby
            connState = .connected
            connError = ""

            for b in list {
                Task {
                    if let r: MessagesResp = try? await client.call("/api/bots/\(enc(b.id))/messages?limit=1"),
                       let m = r.messages?.last, seq == connSeq { last[b.id] = m }
                }
                Task {
                    if let t: TasksResp = try? await client.call("/api/bots/\(enc(b.id))/tasks?limit=50"),
                       seq == connSeq {
                        let taskList = t.tasks ?? []
                        tasks[b.id] = taskList
                        LiveActivityManager.shared.handleTasks(botId: b.id, tasks: taskList)
                    }
                }
            }
            Task { if let s: SettingsData = try? await client.call("/api/settings"), seq == connSeq { settings = s } }
            Task { if let n: NodesResp = try? await client.call("/api/nodes"), seq == connSeq { nodes = n.nodes ?? [] } }
            Task { if let t: TailcatStatus = try? await client.call("/api/tailcat"), seq == connSeq { tailcat = t } }
            Task { if let ts: HostTailscaleStatus = try? await client.call("/api/tailscale"), seq == connSeq { hostTailscale = ts } }
            Task { if let t: TakeoversResp = try? await client.call("/api/desktop/takeovers"), seq == connSeq { takeovers = t.items ?? [] } }
            Task {
                if let d: TargetsResp = try? await client.call("/api/desktop/targets"), seq == connSeq {
                    desktopTargets = d.targets ?? []
                    if let at = d.activeTarget { activeDesktopTarget = at }
                }
            }
            Task { if let q: QuestionsResp = try? await client.call("/api/questions"), seq == connSeq { questions = q.items ?? [] } }
            Task { if let a: ApprovalsResp = try? await client.call("/api/approvals"), seq == connSeq { approvals = a.items ?? [] } }
            Task { if let u: UpdateStatus = try? await client.call("/api/update"), seq == connSeq { updateStatus = u } }
            PushManager.shared.activeChatBotId = { [weak self] in self?.activeChat }
            PushManager.shared.configureConnection(base: url, token: tok)
            LiveActivityManager.shared.startObservingTokens()
            startStream(url, tok)
        } catch {
            guard seq == connSeq else { throw error }
            baseURL = url
            authToken = tok
            isTailcat = norm.isTailcat
            isTailscale = norm.isTailscale
            throw error
        }
    }

    /// 回到前台：刷新列表并重启 SSE，失败则整体重连。
    func resume() async {
        guard let cur = activeServer, !baseURL.isEmpty else { return }
        let url = baseURL, tok = authToken
        do {
            let d: BotsResp = try await api.call("/api/bots")
            serverInfo = d.server
            bots = d.bots ?? []
            startStream(url, tok)
            if let bid = activeChat {
                let m: MessagesResp = try await api.call("/api/bots/\(enc(bid))/messages?limit=200")
                msgs[bid] = m.messages ?? []
            }
        } catch {
            await connectProfile(cur)
        }
    }

    func addAndConnectServer(label: String, address: String, token: String, standbyAddress: String? = nil, standbyToken: String? = nil) async throws {
        let norm = normalizeAddress(address)
        if norm.cleanAddress.isEmpty { throw HubError(message: L.t("enterValidAddr")) }
        let tok = (token.isEmpty ? norm.token : token).trimmingCharacters(in: .whitespaces)
        let lbl = label.trimmingCharacters(in: .whitespaces)
        let sbTokTrim = (standbyToken ?? "").trimmingCharacters(in: .whitespaces)
        let sbParts = (standbyAddress ?? "")
            .split { $0 == "," || $0 == "\n" }
            .map { String($0).trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        var fallbacks: [FallbackHost] = []
        for part in sbParts {
            let fn = normalizeAddress(part)
            if !fn.cleanAddress.isEmpty {
                let ft = !sbTokTrim.isEmpty ? sbTokTrim : (fn.token.isEmpty ? nil : fn.token)
                fallbacks.append(FallbackHost(label: nil, address: fn.cleanAddress, token: ft))
            }
        }
        let sbAddr = fallbacks.first?.address
        let sbTok = fallbacks.first?.token

        var target: ServerProfile
        if let i = servers.firstIndex(where: { $0.address == norm.cleanAddress }) {
            target = servers[i]
            if !lbl.isEmpty { target.label = lbl }
            if !tok.isEmpty { target.token = tok }
            if let sbAddr, !sbAddr.isEmpty { target.standbyAddress = sbAddr }
            if let sbTok, !sbTok.isEmpty { target.standbyToken = sbTok }
            if !fallbacks.isEmpty { target.fallbackHosts = fallbacks }
            servers[i] = target
        } else {
            let def = norm.isTailcat ? L.t("remoteHostDefault")
                : norm.cleanAddress.replacingOccurrences(of: "^https?://", with: "", options: .regularExpression)
            target = ServerProfile(id: "\(Int(Date().timeIntervalSince1970 * 1000))", label: lbl.isEmpty ? def : lbl,
                                   address: norm.cleanAddress, token: tok,
                                   standbyAddress: (sbAddr?.isEmpty == false ? sbAddr : nil),
                                   standbyToken: sbTok,
                                   fallbackHosts: fallbacks.isEmpty ? nil : fallbacks)
            servers.insert(target, at: 0)
        }
        activeServerId = target.id
        persistServers()
        await connectProfile(target)
    }

    func selectServer(_ id: String) async {
        guard let target = servers.first(where: { $0.id == id }) else { return }
        activeServerId = id
        persistServers()
        await connectProfile(target)
    }

    func removeServer(_ id: String) async {
        servers.removeAll { $0.id == id }
        let removingActive = activeServerId == id
        if removingActive { activeServerId = servers.first?.id }
        persistServers()
        guard removingActive else { return }
        if let next = servers.first {
            await connectProfile(next)
        } else {
            connSeq += 1
            stream.stop()
            await Tunnel.stop()
            baseURL = ""; authToken = ""
            bots = []; msgs = [:]; last = [:]
            serverInfo = nil; unsupportedCaps = []; dismissedCompatProto = 0
            connState = .idle
            connError = ""
        }
    }

    /// 由 macOS Sidecar 宿主管理连接时调用，直接接入本地或代理后的 HTTP/SSE 端点。
    func connectManagedEndpoint(url rawURL: String, token: String, isRemote: Bool) async {
        let cleanURL = normalizeEndpointURL(rawURL)
        guard !cleanURL.isEmpty else {
            disconnectManagedEndpoint()
            return
        }
        if managedMode && connState == .connected && cleanURL == baseURL && managedIsRemote == isRemote {
            if token != authToken {
                let seq = connSeq
                authToken = token
                PushManager.shared.configureConnection(base: cleanURL, token: token)
                startStream(cleanURL, token)
                Task { try? await self.refreshBots() }
                Task { if let s: SettingsData = try? await self.callAPI("/api/settings"), seq == self.connSeq { self.settings = s } }
                Task { if let n: NodesResp = try? await self.callAPI("/api/nodes"), seq == self.connSeq { self.nodes = n.nodes ?? [] } }
                Task { if let t: TailcatStatus = try? await self.callAPI("/api/tailcat"), seq == self.connSeq { self.tailcat = t } }
                Task { if let ts: HostTailscaleStatus = try? await self.callAPI("/api/tailscale"), seq == self.connSeq { self.hostTailscale = ts } }
                Task { if let t: TakeoversResp = try? await self.callAPI("/api/desktop/takeovers"), seq == self.connSeq { self.takeovers = t.items ?? [] } }
                Task {
                    if let d: TargetsResp = try? await self.callAPI("/api/desktop/targets"), seq == self.connSeq {
                        self.desktopTargets = d.targets ?? []
                        if let at = d.activeTarget { self.activeDesktopTarget = at }
                    }
                }
                Task { if let q: QuestionsResp = try? await self.callAPI("/api/questions"), seq == self.connSeq { self.questions = q.items ?? [] } }
                Task { if let u: UpdateStatus = try? await self.callAPI("/api/update"), seq == self.connSeq { self.updateStatus = u } }
            }
            return
        }
        connSeq += 1
        let seq = connSeq
        stream.stop()
        managedMode = true
        managedIsRemote = isRemote
        online = false
        usingStandby = false
        isTailcat = false
        isTailscale = false
        connState = .connecting
        connError = ""
        baseURL = cleanURL
        authToken = token

        do {
            let client = APIClient(base: cleanURL, token: token)
            let data: BotsResp = try await client.call("/api/bots")
            guard seq == connSeq else { return }
            serverInfo = data.server
            unsupportedCaps = []
            let list = data.bots ?? []
            bots = list
            msgs = [:]; last = [:]; unread = [:]; tools = [:]; routines = [:]
            tasks = [:]; taskEvents = [:]; taskTools = [:]; taskStreamText = [:]
            resetModelsCache()
            connState = .connected
            connError = ""

            for b in list {
                Task {
                    if let r: MessagesResp = try? await client.call("/api/bots/\(enc(b.id))/messages?limit=1"),
                       let m = r.messages?.last, seq == connSeq { last[b.id] = m }
                }
                Task {
                    if let t: TasksResp = try? await client.call("/api/bots/\(enc(b.id))/tasks?limit=50"),
                       seq == connSeq {
                        let taskList = t.tasks ?? []
                        tasks[b.id] = taskList
                        LiveActivityManager.shared.handleTasks(botId: b.id, tasks: taskList)
                    }
                }
            }
            Task { if let s: SettingsData = try? await client.call("/api/settings"), seq == connSeq { settings = s } }
            Task { if let n: NodesResp = try? await client.call("/api/nodes"), seq == connSeq { nodes = n.nodes ?? [] } }
            Task { if let t: TailcatStatus = try? await client.call("/api/tailcat"), seq == connSeq { tailcat = t } }
            Task { if let ts: HostTailscaleStatus = try? await client.call("/api/tailscale"), seq == connSeq { hostTailscale = ts } }
            Task { if let t: TakeoversResp = try? await client.call("/api/desktop/takeovers"), seq == connSeq { takeovers = t.items ?? [] } }
            Task {
                if let d: TargetsResp = try? await client.call("/api/desktop/targets"), seq == connSeq {
                    desktopTargets = d.targets ?? []
                    if let at = d.activeTarget { activeDesktopTarget = at }
                }
            }
            Task { if let q: QuestionsResp = try? await client.call("/api/questions"), seq == connSeq { questions = q.items ?? [] } }
            Task { if let a: ApprovalsResp = try? await client.call("/api/approvals"), seq == connSeq { approvals = a.items ?? [] } }
            Task { if let u: UpdateStatus = try? await client.call("/api/update"), seq == connSeq { updateStatus = u } }
            PushManager.shared.activeChatBotId = { [weak self] in self?.activeChat }
            PushManager.shared.configureConnection(base: cleanURL, token: token)
            startStream(cleanURL, token)
        } catch {
            guard seq == connSeq else { return }
            handleConnectError(error)
        }
    }

    func disconnectManagedEndpoint() {
        connSeq += 1
        stream.stop()
        online = false
        managedIsRemote = false
        baseURL = ""
        authToken = ""
        bots = []
        msgs = [:]
        last = [:]
        unread = [:]
        tools = [:]
        models = [:]
        routines = [:]
        tasks = [:]
        taskEvents = [:]
        taskTools = [:]
        taskStreamText = [:]
        takeovers = []
        questions = []
        approvals = []
        settings = nil
        serverInfo = nil
        unsupportedCaps = []
        dismissedCompatProto = 0
        updateStatus = nil
        updateChecking = false
        connState = .idle
        connError = ""
    }

    func reconnect() async {
        if managedMode, !baseURL.isEmpty {
            await connectManagedEndpoint(url: baseURL, token: authToken, isRemote: managedIsRemote)
            return
        }
        if let s = activeServer { await connectProfile(s) }
    }
}
