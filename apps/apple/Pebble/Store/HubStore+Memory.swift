import Foundation

extension HubStore {
    // MARK: Pebble Native Memory

    func fetchMemories(
        scope: String,
        botId: String? = nil,
        query: String = "",
        kind: String = "",
        tags: String = "",
        status: String = "",
        limit: Int = 50
    ) async throws -> MemoryList {
        var q = URLComponents()
        var items = [URLQueryItem(name: "scope", value: scope)]
        if let botId, !botId.isEmpty {
            items.append(URLQueryItem(name: "botId", value: botId))
        }
        let qq = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if !qq.isEmpty { items.append(URLQueryItem(name: "query", value: qq)) }
        let kk = kind.trimmingCharacters(in: .whitespacesAndNewlines)
        if !kk.isEmpty { items.append(URLQueryItem(name: "kind", value: kk)) }
        let tt = tags.trimmingCharacters(in: .whitespacesAndNewlines)
        if !tt.isEmpty { items.append(URLQueryItem(name: "tags", value: tt)) }
        let ss = status.trimmingCharacters(in: .whitespacesAndNewlines)
        if !ss.isEmpty { items.append(URLQueryItem(name: "status", value: ss)) }
        if limit > 0 { items.append(URLQueryItem(name: "limit", value: "\(limit)")) }
        q.queryItems = items
        return try await callAPI("/api/memory?\(q.percentEncodedQuery ?? "")")
    }

    func saveMemory(_ body: [String: Any]) async throws {
        _ = try await rawAPI("/api/memory", method: "POST", body: body)
    }

    func patchMemory(_ id: String, _ body: [String: Any]) async throws {
        _ = try await rawAPI("/api/memory/\(enc(id))", method: "PATCH", body: body)
    }

    func deleteMemory(_ id: String, hard: Bool = false, dismiss: Bool = false) async throws {
        var params: [String] = []
        if hard { params.append("hard=1") }
        if dismiss { params.append("dismiss=1") }
        let suffix = params.isEmpty ? "" : "?\(params.joined(separator: "&"))"
        _ = try await rawAPI("/api/memory/\(enc(id))\(suffix)", method: "DELETE")
    }

    func fetchMemoryIndex(scope: String = "global", botId: String? = nil) async throws -> MemoryIndexDoc {
        var q = URLComponents()
        var items: [URLQueryItem] = []
        if let botId, !botId.isEmpty {
            items.append(URLQueryItem(name: "scope", value: "bot"))
            items.append(URLQueryItem(name: "botId", value: botId))
        } else {
            items.append(URLQueryItem(name: "scope", value: scope))
        }
        q.queryItems = items
        return try await callAPI("/api/memory/index?\(q.percentEncodedQuery ?? "")")
    }

    @discardableResult
    func saveMemoryIndex(scope: String = "global", botId: String? = nil, content: String) async throws -> MemoryIndexDoc {
        var body: [String: Any] = ["content": content]
        if let botId, !botId.isEmpty {
            body["scope"] = "bot"
            body["botId"] = botId
        } else {
            body["scope"] = scope
        }
        return try await callAPI("/api/memory/index", method: "PUT", body: body)
    }

    func fetchMemoryStatus(botId: String? = nil) async throws -> MemoryStatusResp {
        if let botId, !botId.isEmpty {
            return try await callAPI("/api/memory/status?botId=\(enc(botId))")
        }
        return try await callAPI("/api/memory/status")
    }

    @discardableResult
    func triggerMemoryDream(botId: String? = nil) async throws -> MemoryDreamResp {
        var body: [String: Any] = [:]
        if let botId, !botId.isEmpty { body["botId"] = botId }
        return try await callAPI("/api/memory/dream", method: "POST", body: body)
    }

    @discardableResult
    func triggerMemoryReindex() async throws -> MemoryReindexResp {
        try await callAPI("/api/memory/reindex", method: "POST", body: [:])
    }

    @discardableResult
    func importMemoryFromMagpie(mode: String, botId: String? = nil) async throws -> MemoryImportResp {
        var body: [String: Any] = ["mode": mode]
        if let botId, !botId.isEmpty { body["botId"] = botId }
        return try await callAPI("/api/memory/import/magpie", method: "POST", body: body)
    }

    func saveMemoryConfig(_ memory: [String: Any]) async throws {
        settings = try await callAPI("/api/settings", method: "PUT", body: ["memory": memory])
    }
}
