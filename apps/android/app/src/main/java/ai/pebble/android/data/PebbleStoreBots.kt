package ai.pebble.android.data

import kotlinx.coroutines.launch
import java.time.Instant

// MARK: Bots & Messages

internal suspend fun PebbleStore.refreshBotsImpl() {
    if (baseURL.isEmpty()) return
    val d = api.call<BotsResp>("/api/bots")
    serverInfo = d.server
    bots = d.bots.orEmpty()
}

internal suspend fun PebbleStore.loadMessagesImpl(botId: String) {
    if (baseURL.isEmpty()) return
    val list = api.call<MessagesResp>("/api/bots/${encodeURIComponent(botId)}/messages?limit=200").messages.orEmpty()
    msgs[botId] = list
    list.lastOrNull()?.let { last[botId] = it }
}

internal suspend fun PebbleStore.loadCollabMessagesImpl(botId: String, peerId: String, limit: Int = 200): List<ChatMessage> {
    if (baseURL.isEmpty()) return emptyList()
    return api.call<MessagesResp>(
        "/api/bots/${encodeURIComponent(botId)}/collab/${encodeURIComponent(peerId)}?limit=$limit"
    ).messages.orEmpty()
}

internal suspend fun PebbleStore.fetchMediaBytesImpl(path: String): ByteArray {
    val key = "$baseURL$path"
    synchronized(mediaCache) {
        mediaCache[key]?.let { return it }
    }
    val bytes = api.rawBytes(path)
    val maxEntries = 64
    val maxBytes = 32L * 1024L * 1024L
    synchronized(mediaCache) {
        mediaCache.remove(key)?.let { mediaCacheBytes -= it.size }
        while (mediaCache.isNotEmpty() && (mediaCache.size >= maxEntries || mediaCacheBytes + bytes.size > maxBytes)) {
            val it = mediaCache.entries.iterator()
            val eldest = it.next()
            mediaCacheBytes -= eldest.value.size
            it.remove()
        }
        mediaCache[key] = bytes
        mediaCacheBytes += bytes.size
    }
    return bytes
}

internal suspend fun PebbleStore.fetchCommandsImpl(botId: String): List<SlashCommandItem> =
    api.call<CommandsResp>("/api/bots/${encodeURIComponent(botId)}/commands").commands.orEmpty()

internal suspend fun PebbleStore.fetchMentionsImpl(botId: String, query: String): List<MentionCandidate> =
    api.call<MentionsResp>(
        "/api/bots/${encodeURIComponent(botId)}/mentions?q=${encodeURIComponent(query)}"
    ).items.orEmpty()

internal suspend fun PebbleStore.uploadAttachmentImpl(
    botId: String,
    bytes: ByteArray,
    filename: String,
    mimeType: String,
): InboundAttachmentResponse =
    api.uploadAttachment("/api/bots/${encodeURIComponent(botId)}/attachments", bytes, filename, mimeType)

internal suspend fun PebbleStore.sendMessageImpl(botId: String, text: String, mediaIds: List<String> = emptyList()) {
    val body = mutableMapOf<String, Any?>("message" to text, "source" to "mobile")
    if (mediaIds.isNotEmpty()) body["mediaIds"] = mediaIds
    val d = api.call<TextResp>("/api/bots/${encodeURIComponent(botId)}/message", method = "POST", body = body)
    val trimmed = text.trim()
    if ((trimmed in listOf("/bots", "/status", "/reset", "/mode") || trimmed.startsWith("/mode ")) && !d.text.isNullOrEmpty()) {
        pushIncoming(
            botId,
            ChatMessage(
                id = "cmd_${System.currentTimeMillis()}",
                botId = botId,
                role = "system",
                from = "command",
                text = d.text,
                createdAt = Instant.now().toString(),
            ),
        )
    }
    if (trimmed == "/reset" || trimmed.startsWith("/mode ")) refreshBots()
}

internal suspend fun PebbleStore.interruptBotImpl(botId: String): Boolean =
    api.call<InterruptResp>("/api/bots/${encodeURIComponent(botId)}/interrupt", method = "POST").interrupted == true

internal suspend fun PebbleStore.resetBotImpl(botId: String) {
    api.rawText("/api/bots/${encodeURIComponent(botId)}/reset", method = "POST")
    refreshBots()
}

internal suspend fun PebbleStore.botDetailImpl(botId: String): BotStatus =
    api.call("/api/bots/${encodeURIComponent(botId)}")

internal suspend fun PebbleStore.createBotImpl(body: Map<String, Any?>): BotStatus {
    val st = api.call<BotStatus>("/api/bots", method = "POST", body = body)
    if (bots.none { it.id == st.id }) bots = bots + st
    return st
}

internal suspend fun PebbleStore.updateBotImpl(botId: String, body: Map<String, Any?>): BotStatus {
    val st = api.call<BotStatus>("/api/bots/${encodeURIComponent(botId)}", method = "PATCH", body = body)
    val list = bots.toMutableList()
    val idx = list.indexOfFirst { it.id == st.id }
    if (idx >= 0) list[idx] = st
    bots = list
    return st
}

internal suspend fun PebbleStore.deleteBotImpl(botId: String) {
    api.rawText("/api/bots/${encodeURIComponent(botId)}", method = "DELETE")
    removeBotState(botId)
}

// MARK: Routines

internal suspend fun PebbleStore.fetchRoutinesImpl(botId: String): List<RoutineItem> {
    val list = api.call<RoutinesResp>("/api/bots/${encodeURIComponent(botId)}/routines").routines.orEmpty()
    routines[botId] = list
    return list
}

internal suspend fun PebbleStore.createRoutineImpl(botId: String, body: Map<String, Any?>) {
    api.rawText("/api/bots/${encodeURIComponent(botId)}/routines", method = "POST", body = body)
    fetchRoutines(botId)
}

internal suspend fun PebbleStore.updateRoutineImpl(botId: String, rid: String, body: Map<String, Any?>) {
    api.rawText("/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rid)}", method = "PATCH", body = body)
    fetchRoutines(botId)
}

internal suspend fun PebbleStore.deleteRoutineImpl(botId: String, rid: String) {
    api.rawText("/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rid)}", method = "DELETE")
    fetchRoutines(botId)
}

internal suspend fun PebbleStore.runRoutineImpl(botId: String, rid: String) {
    api.rawText("/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rid)}/run?async=1", method = "POST")
    fetchRoutines(botId)
}

internal suspend fun PebbleStore.fetchRoutineRunsImpl(botId: String, rid: String, limit: Int = 6): List<RoutineRun> =
    api.call<RunsResp>(
        "/api/bots/${encodeURIComponent(botId)}/routine-runs?routineId=${encodeURIComponent(rid)}&limit=$limit"
    ).runs.orEmpty()
