package ai.pebble.android.data

// MARK: Pebble Native Memory

internal suspend fun PebbleStore.fetchMemoriesImpl(
    scope: String,
    botId: String? = null,
    query: String = "",
    kind: String = "",
    tags: String = "",
    status: String = "",
    limit: Int = 50,
): MemoryList {
    val params = mutableListOf("scope=${encodeURIComponent(scope)}")
    if (!botId.isNullOrBlank()) params.add("botId=${encodeURIComponent(botId)}")
    if (query.trim().isNotEmpty()) params.add("query=${encodeURIComponent(query.trim())}")
    if (kind.trim().isNotEmpty()) params.add("kind=${encodeURIComponent(kind.trim())}")
    if (tags.trim().isNotEmpty()) params.add("tags=${encodeURIComponent(tags.trim())}")
    if (status.trim().isNotEmpty()) params.add("status=${encodeURIComponent(status.trim())}")
    if (limit > 0) params.add("limit=$limit")
    return api.call("/api/memory?${params.joinToString("&")}")
}

internal suspend fun PebbleStore.saveMemoryImpl(body: Map<String, Any?>): MemorySaveResp =
    api.call("/api/memory", method = "POST", body = body)

internal suspend fun PebbleStore.patchMemoryImpl(id: String, body: Map<String, Any?>): MemoryPatchResp =
    api.call("/api/memory/${encodeURIComponent(id)}", method = "PATCH", body = body)

internal suspend fun PebbleStore.deleteMemoryImpl(
    id: String,
    hard: Boolean = false,
    dismiss: Boolean = false,
) {
    val params = mutableListOf<String>()
    if (hard) params.add("hard=1")
    if (dismiss) params.add("dismiss=1")
    val qs = if (params.isEmpty()) "" else "?" + params.joinToString("&")
    api.rawText("/api/memory/${encodeURIComponent(id)}$qs", method = "DELETE")
}

internal suspend fun PebbleStore.fetchMemoryIndexImpl(
    scope: String? = null,
    botId: String? = null,
): MemoryIndexDoc {
    val params = mutableListOf<String>()
    if (!botId.isNullOrBlank()) {
        params.add("botId=${encodeURIComponent(botId)}")
    } else {
        params.add("scope=${encodeURIComponent(scope?.ifBlank { "global" } ?: "global")}")
    }
    return api.call("/api/memory/index?${params.joinToString("&")}")
}

internal suspend fun PebbleStore.saveMemoryIndexImpl(
    content: String,
    scope: String? = null,
    botId: String? = null,
): MemoryIndexDoc {
    val body = mutableMapOf<String, Any?>("content" to content)
    if (!botId.isNullOrBlank()) {
        body["botId"] = botId
    } else {
        body["scope"] = scope?.ifBlank { "global" } ?: "global"
    }
    return api.call("/api/memory/index", method = "PUT", body = body)
}

internal suspend fun PebbleStore.fetchMemoryStatusImpl(botId: String? = null): MemoryStatusResp {
    val qs = if (!botId.isNullOrBlank()) "?botId=${encodeURIComponent(botId)}" else ""
    return api.call("/api/memory/status$qs")
}

internal suspend fun PebbleStore.runMemoryDreamImpl(botId: String? = null): MemoryDreamRunResp {
    val body = if (!botId.isNullOrBlank()) mapOf<String, Any?>("botId" to botId) else emptyMap()
    return api.call("/api/memory/dream", method = "POST", body = body)
}

internal suspend fun PebbleStore.reindexMemoryImpl(): MemoryReindexResp =
    api.call("/api/memory/reindex", method = "POST", body = emptyMap())

internal suspend fun PebbleStore.importMagpieMemoryImpl(
    mode: String,
    botId: String? = null,
): MemoryImportResp {
    val body = mutableMapOf<String, Any?>("mode" to mode)
    if (!botId.isNullOrBlank()) body["botId"] = botId
    return api.call("/api/memory/import/magpie", method = "POST", body = body)
}

internal suspend fun PebbleStore.saveMemoryConfigImpl(memory: Map<String, Any?>): SettingsData {
    val updated = api.call<SettingsData>("/api/settings", method = "PUT", body = mapOf("memory" to memory))
    settings = updated
    return updated
}
