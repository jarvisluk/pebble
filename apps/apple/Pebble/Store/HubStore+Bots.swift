import Foundation

extension HubStore {
    // MARK: Bot 与消息

    func refreshBots() async throws {
        guard !baseURL.isEmpty else { return }
        let d: BotsResp = try await callAPI("/api/bots")
        serverInfo = d.server
        bots = d.bots ?? []
    }

    func loadMessages(_ botId: String) async throws {
        guard !baseURL.isEmpty else { return }
        let d: MessagesResp = try await callAPI("/api/bots/\(enc(botId))/messages?limit=200")
        let list = d.messages ?? []
        msgs[botId] = list
        if let m = list.last { last[botId] = m }
    }

    func loadCollabMessages(_ botId: String, peerId: String, limit: Int = 200) async throws -> [ChatMessage] {
        guard !baseURL.isEmpty else { return [] }
        let d: MessagesResp = try await callAPI("/api/bots/\(enc(botId))/collab/\(enc(peerId))?limit=\(limit)")
        return d.messages ?? []
    }

    func fetchMediaData(_ path: String) async throws -> Data {
        let key = (baseURL + path) as NSString
        if let cached = mediaCache.object(forKey: key) {
            return Data(referencing: cached)
        }
        let data = try await rawAPI(path)
        mediaCache.setObject(data as NSData, forKey: key)
        return data
    }

    func fetchCommands(_ botId: String) async throws -> [SlashCommandItem] {
        let resp: CommandsResp = try await callAPI("/api/bots/\(enc(botId))/commands")
        return resp.commands ?? []
    }

    func fetchMentions(_ botId: String, query: String) async throws -> [MentionCandidate] {
        let q = query.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? query
        let resp: MentionsResp = try await callAPI("/api/bots/\(enc(botId))/mentions?q=\(q)")
        return resp.items ?? []
    }

    func uploadAttachment(_ botId: String, data: Data, filename: String, mimeType: String) async throws -> InboundAttachmentResponse {
        try await performAPI { try await $0.uploadAttachment("/api/bots/\(enc(botId))/attachments", data: data, filename: filename, mimeType: mimeType) }
    }

    func sendMessage(_ botId: String, _ text: String, mediaIds: [String] = []) async throws {
        var body: [String: Any] = ["message": text, "source": "mobile"]
        if !mediaIds.isEmpty {
            body["mediaIds"] = mediaIds
        }
        let d: TextResp = try await callAPI("/api/bots/\(enc(botId))/message", method: "POST", body: body)
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if (["/bots", "/status", "/reset", "/mode"].contains(trimmed) || trimmed.hasPrefix("/mode ")),
           let out = d.text, !out.isEmpty {
            pushIncoming(botId, ChatMessage(id: "cmd_\(Int(Date().timeIntervalSince1970 * 1000))", botId: botId,
                                            role: "system", from: "command", text: out,
                                            createdAt: ISO8601DateFormatter().string(from: Date())))
        }
        if trimmed == "/reset" || trimmed.hasPrefix("/mode ") { try await refreshBots() }
    }

    func interruptBot(_ botId: String) async throws -> Bool {
        let r: InterruptResp = try await callAPI("/api/bots/\(enc(botId))/interrupt", method: "POST")
        return r.interrupted ?? false
    }

    func resetBot(_ botId: String) async throws {
        _ = try await rawAPI("/api/bots/\(enc(botId))/reset", method: "POST")
        try await refreshBots()
    }

    func botDetail(_ botId: String) async throws -> BotStatus {
        try await callAPI("/api/bots/\(enc(botId))")
    }

    func createBot(_ body: [String: Any]) async throws -> BotStatus {
        let st: BotStatus = try await callAPI("/api/bots", method: "POST", body: body)
        if !bots.contains(where: { $0.id == st.id }) { bots.append(st) }
        return st
    }

    @discardableResult
    func updateBot(_ botId: String, _ body: [String: Any]) async throws -> BotStatus {
        let st: BotStatus = try await callAPI("/api/bots/\(enc(botId))", method: "PATCH", body: body)
        if let i = bots.firstIndex(where: { $0.id == st.id }) { bots[i] = st }
        return st
    }

    func deleteBot(_ botId: String) async throws {
        _ = try await rawAPI("/api/bots/\(enc(botId))", method: "DELETE")
        removeBotState(botId)
    }

    // MARK: 记忆

    func fetchMemories(scope: String, botId: String? = nil, query: String = "", kind: String = "", limit: Int = 0) async throws -> MemoryList {
        var q = URLComponents()
        var items = [URLQueryItem(name: "scope", value: scope)]
        if let botId { items.append(URLQueryItem(name: "botId", value: botId)) }
        let qq = query.trimmingCharacters(in: .whitespaces)
        if !qq.isEmpty { items.append(URLQueryItem(name: "query", value: qq)) }
        let kk = kind.trimmingCharacters(in: .whitespaces)
        if !kk.isEmpty { items.append(URLQueryItem(name: "kind", value: kk)) }
        if limit > 0 { items.append(URLQueryItem(name: "limit", value: "\(limit)")) }
        q.queryItems = items
        return try await callAPI("/api/memory?\(q.percentEncodedQuery ?? "")")
    }

    func saveMemory(_ body: [String: Any]) async throws {
        _ = try await rawAPI("/api/memory", method: "POST", body: body)
    }

    func deleteMemory(_ id: String) async throws {
        _ = try await rawAPI("/api/memory/\(enc(id))", method: "DELETE")
    }

    // MARK: 定时任务

    @discardableResult
    func fetchRoutines(_ botId: String) async throws -> [RoutineItem] {
        let r: RoutinesResp = try await callAPI("/api/bots/\(enc(botId))/routines")
        routines[botId] = r.routines ?? []
        return routines[botId]!
    }

    func createRoutine(_ botId: String, _ body: [String: Any]) async throws {
        _ = try await rawAPI("/api/bots/\(enc(botId))/routines", method: "POST", body: body)
        try await fetchRoutines(botId)
    }

    func updateRoutine(_ botId: String, _ rid: String, _ body: [String: Any]) async throws {
        _ = try await rawAPI("/api/bots/\(enc(botId))/routines/\(enc(rid))", method: "PATCH", body: body)
        try await fetchRoutines(botId)
    }

    func deleteRoutine(_ botId: String, _ rid: String) async throws {
        _ = try await rawAPI("/api/bots/\(enc(botId))/routines/\(enc(rid))", method: "DELETE")
        try await fetchRoutines(botId)
    }

    func runRoutine(_ botId: String, _ rid: String) async throws {
        _ = try await rawAPI("/api/bots/\(enc(botId))/routines/\(enc(rid))/run?async=1", method: "POST")
        try await fetchRoutines(botId)
    }

    func fetchRoutineRuns(_ botId: String, _ rid: String, limit: Int = 6) async throws -> [RoutineRun] {
        let r: RunsResp = try await callAPI("/api/bots/\(enc(botId))/routine-runs?routineId=\(enc(rid))&limit=\(limit)")
        return r.runs ?? []
    }

    // MARK: 工作区文件

    func listWorkspaceFiles(botId: String, path: String = "", showHidden: Bool = false) async throws -> WorkspaceListResult {
        try await performAPI { try await $0.listWorkspaceFiles(botId: botId, path: path, showHidden: showHidden) }
    }

    func readWorkspaceFile(botId: String, path: String) async throws -> WorkspaceFileContent {
        try await performAPI { try await $0.readWorkspaceFile(botId: botId, path: path) }
    }

    func workspaceFileImageURL(botId: String, path: String) async throws -> URL? {
        let tk = try await performAPI { try await $0.issueWorkspaceFileTicket(botId: botId, path: path) }
        return api.workspaceFileRawURL(botId: botId, path: path, ticket: tk.ticket, download: false)
    }

    func downloadWorkspaceFileToTemp(botId: String, path: String, fileName: String) async throws -> URL {
        try await performAPI { try await $0.downloadWorkspaceFileToTemp(botId: botId, path: path, fileName: fileName) }
    }

    func insertIntoChat(_ text: String) {
        let nextSeq = (pendingChatInsert?.seq ?? 0) + 1
        pendingChatInsert = PendingChatInsert(seq: nextSeq, text: text)
    }
}
