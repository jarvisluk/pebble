import Foundation

extension HubStore {
    // MARK: 事件流与会话状态

    func setActiveChat(_ botId: String?) {
        activeChat = botId
        if let botId { unread[botId] = nil }
    }

    func toggleUnread(_ id: String) {
        if (unread[id] ?? 0) > 0 {
            unread[id] = nil
        } else {
            unread[id] = 1
        }
    }

    func removeBotState(_ id: String) {
        bots.removeAll { $0.id == id }
        msgs[id] = nil
        last[id] = nil
        unread[id] = nil
        tools[id] = nil
    }

    func pushIncoming(_ botId: String, _ msg: ChatMessage) {
        var list = msgs[botId] ?? []
        if list.contains(where: { $0.id == msg.id }) { return }
        list.append(msg)
        msgs[botId] = list
        if msg.role != "system" {
            last[botId] = msg
            if activeChat != botId { unread[botId, default: 0] += 1 }
            #if os(macOS)
            if msg.role != "user" {
                let botName = bots.first(where: { $0.id == botId })?.displayName ?? botId
                let preview = plainPreview(msg.body)
                PushManager.shared.postLocalNotification(
                    id: "msg-\(msg.id)",
                    botId: botId,
                    title: botName,
                    body: preview.isEmpty ? L.t("newMessage") : preview,
                    deepLink: "pebble://bot/\(botId)"
                )
            }
            #endif
        }
    }

    private func updateIncoming(_ botId: String, _ msg: ChatMessage) {
        if var list = msgs[botId], let idx = list.firstIndex(where: { $0.id == msg.id }) {
            list[idx] = msg
            msgs[botId] = list
        }
        if last[botId]?.id == msg.id {
            last[botId] = msg
        }
    }

    private func upsertBot(_ st: BotStatus) {
        if let i = bots.firstIndex(where: { $0.id == st.id }) { bots[i] = st } else { bots.append(st) }
    }

    func startStream(_ url: String, _ tok: String) {
        stream.start(base: url, token: tok,
                     onStatus: { [weak self] ok in
                         if self?.online != ok { self?.online = ok }
                     },
                     onUnauthorized: { [weak self] in
                         guard let self else { return }
                         #if os(macOS)
                         if self.managedMode {
                             Task { @MainActor [weak self] in
                                 guard let self, self.managedMode, self.baseURL == url else { return }
                                 await DesktopHostManager.shared.refreshStatus()
                                 let snap = DesktopHostManager.shared.snapshot
                                 let snapURL = self.normalizeEndpointURL(snap.activeUrl)
                                 if !snapURL.isEmpty, snapURL == self.baseURL, snap.activeToken != tok {
                                     if self.authToken != snap.activeToken {
                                         self.authToken = snap.activeToken
                                         PushManager.shared.configureConnection(base: self.baseURL, token: snap.activeToken)
                                     }
                                     self.connState = .connected
                                     self.connError = ""
                                     self.startStream(self.baseURL, snap.activeToken)
                                     return
                                 }
                                 self.online = false
                                 self.connState = .unauthorized
                                 self.connError = L.t("needsToken")
                             }
                             return
                         }
                         #endif
                         self.online = false
                         self.connState = .unauthorized
                         self.connError = L.t("needsToken")
                     },
                     onEvent: { [weak self] type, p in self?.handleEvent(type, p, url: url, tok: tok) })
    }

    private func handleEvent(_ type: String, _ p: [String: Any], url: String, tok: String) {
        switch type {
        case "message":
            if let bid = p["botId"] as? String, let m = decodeAny(p, as: ChatMessage.self) { pushIncoming(bid, m) }
        case "message_updated":
            if let bid = p["botId"] as? String, let m = decodeAny(p, as: ChatMessage.self) { updateIncoming(bid, m) }
        case "bot_status":
            if let st = decodeAny(p, as: BotStatus.self) {
                upsertBot(st)
                if !st.isBusy && tools[st.id] != nil { tools[st.id] = nil }
                LiveActivityManager.shared.handleBotStatus(st)
            }
        case "tool_use":
            if let bid = p["botId"] as? String {
                let tool = (p["tool"] as? String) ?? ""
                if tools[bid] != tool { tools[bid] = tool }
                LiveActivityManager.shared.handleToolUse(botId: bid, tool: tool)
            }
        case "bot_created":
            if let st = decodeAny(p, as: BotStatus.self), !bots.contains(where: { $0.id == st.id }) { bots.append(st) }
        case "bot_deleted":
            if let bid = (p["id"] as? String) ?? (p["botId"] as? String) { removeBotState(bid) }
        case "nodes_changed":
            if let arr = p["nodes"], let list = decodeAny(arr, as: [NodeInfo].self) { nodes = list }
        case "desktop_target":
            if let at = decodeAny(p, as: ActiveDesktopTarget.self) { activeDesktopTarget = at }
        case "stage_open":
            if let ev = decodeAny(p, as: StageOpenEvent.self) {
                pendingStageOpen = ev
            }
        case "takeover":
            if let arr = p["items"], let list = decodeAny(arr, as: [TakeoverRequest].self) {
                #if os(macOS)
                let oldIDs = Set(takeovers.map(\.id))
                for item in list where !oldIDs.contains(item.id) {
                    let botName = item.botName ?? item.botId ?? "Pebble"
                    let reason = (item.reason ?? "").isEmpty ? L.t("remoteDesktop") : (item.reason ?? "")
                    let tgt = (item.target ?? "").isEmpty ? "local" : (item.target ?? "local")
                    PushManager.shared.postLocalNotification(
                        id: "takeover-\(item.id)",
                        botId: nil,
                        title: botName,
                        body: reason,
                        deepLink: "pebble://desktop?target=\(enc(tgt))"
                    )
                }
                #endif
                takeovers = list
                LiveActivityManager.shared.handleTakeovers(list)
            }
        case "questions":
            if let arr = p["items"], let list = decodeAny(arr, as: [PendingQuestion].self) { questions = list }
        case "approvals":
            if let arr = p["items"], let list = decodeAny(arr, as: [PendingApproval].self) {
                #if os(macOS)
                let oldIDs = Set(approvals.map(\.id))
                for item in list where !oldIDs.contains(item.id) {
                    let botName = item.botName ?? bots.first(where: { $0.id == item.botId })?.displayName ?? item.botId
                    let label = item.toolName.isEmpty ? (item.title ?? L.t("approvalTitle")) : item.toolName
                    let summary = (item.summary ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    let body = summary.isEmpty ? label : "\(label): \(summary)"
                    PushManager.shared.postLocalNotification(
                        id: "approval-\(item.id)",
                        botId: item.botId,
                        title: "\(botName) · \(L.t("approvalWaitingBadge"))",
                        body: body,
                        deepLink: "pebble://bot/\(enc(item.botId))"
                    )
                }
                #endif
                approvals = list
                LiveActivityManager.shared.handleApprovals(list, bots: bots)
            }
        case "routine_updated", "routine_run":
            if let bid = p["botId"] as? String {
                Task { [weak self] in
                    guard let self else { return }
                    if let r: RoutinesResp = try? await self.callAPI("/api/bots/\(enc(bid))/routines") {
                        self.routines[bid] = r.routines ?? []
                    }
                }
            }
        case "update_status":
            if let st = decodeAny(p, as: UpdateStatus.self) {
                updateStatus = st
            }
        default: break
        }
    }
}
