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

// MARK: BotTasks

internal fun PebbleStore.upsertBotTaskImpl(task: BotTask) {
    val list = tasks[task.botId].orEmpty().toMutableList()
    val idx = list.indexOfFirst { it.id == task.id }
    if (idx >= 0) {
        list[idx] = task
    } else {
        list.add(0, task)
    }
    tasks[task.botId] = list
}

internal suspend fun PebbleStore.fetchBotTasksImpl(
    botId: String,
    kind: String? = null,
    status: String? = null,
    limit: Int = 50,
): List<BotTask> {
    val params = mutableListOf("limit=$limit")
    if (!kind.isNullOrBlank()) params.add("kind=${encodeURIComponent(kind)}")
    if (!status.isNullOrBlank()) params.add("status=${encodeURIComponent(status)}")
    val list = api.call<TasksResp>(
        "/api/bots/${encodeURIComponent(botId)}/tasks?${params.joinToString("&")}"
    ).tasks.orEmpty()
    if (kind.isNullOrBlank() && status.isNullOrBlank()) {
        tasks[botId] = list
    }
    return list
}

internal suspend fun PebbleStore.spawnBotTaskImpl(
    botId: String,
    title: String? = null,
    prompt: String,
    kind: String = "worker",
    deviceId: String = "local",
    harness: String? = null,
    model: String? = null,
    permissionMode: String? = null,
    workdir: String? = null,
    delivery: String = "receipt",
): BotTask {
    val body = mutableMapOf<String, Any?>(
        "prompt" to prompt,
        "kind" to kind.ifBlank { "worker" },
        "deviceId" to deviceId.ifBlank { "local" },
        "delivery" to delivery.ifBlank { "receipt" },
    )
    title?.trim()?.takeIf { it.isNotEmpty() }?.let { body["title"] = it }
    harness?.trim()?.takeIf { it.isNotEmpty() }?.let { body["harness"] = it }
    model?.trim()?.takeIf { it.isNotEmpty() }?.let { body["model"] = it }
    permissionMode?.trim()?.takeIf { it.isNotEmpty() }?.let { body["permissionMode"] = it }
    workdir?.trim()?.takeIf { it.isNotEmpty() }?.let { body["workdir"] = it }
    val created = api.call<BotTask>("/api/bots/${encodeURIComponent(botId)}/tasks", method = "POST", body = body)
    upsertBotTask(created)
    return created
}

internal suspend fun PebbleStore.fetchTaskEventsImpl(
    taskId: String,
    afterSeq: Long = 0L,
    limit: Int = 200,
): List<BotTaskEvent> {
    val qs = buildString {
        append("?limit=").append(limit)
        if (afterSeq > 0L) append("&afterSeq=").append(afterSeq)
    }
    val incoming = api.call<TaskEventsResp>("/api/tasks/${encodeURIComponent(taskId)}/events$qs").events.orEmpty()
    if (afterSeq <= 0L) {
        taskEvents[taskId] = incoming
        return incoming
    }
    if (incoming.isNotEmpty()) {
        val existing = taskEvents[taskId].orEmpty().toMutableList()
        val seen = existing.map { it.seq }.toHashSet()
        for (ev in incoming) {
            if (seen.add(ev.seq)) existing.add(ev)
        }
        existing.sortBy { it.seq }
        taskEvents[taskId] = existing
    }
    return taskEvents[taskId].orEmpty()
}

internal suspend fun PebbleStore.sendToTaskImpl(taskId: String, message: String): BotTask {
    val updated = api.call<BotTask>(
        "/api/tasks/${encodeURIComponent(taskId)}/send",
        method = "POST",
        body = mapOf("message" to message),
    )
    upsertBotTask(updated)
    return updated
}

internal suspend fun PebbleStore.cancelTaskImpl(taskId: String): BotTask {
    val updated = api.call<BotTask>(
        "/api/tasks/${encodeURIComponent(taskId)}/cancel",
        method = "POST",
    )
    upsertBotTask(updated)
    taskTools.remove(taskId)
    return updated
}

internal suspend fun PebbleStore.fetchTaskDiffImpl(taskId: String): TaskDiffResult {
    return api.call("/api/tasks/${encodeURIComponent(taskId)}/diff")
}

internal suspend fun PebbleStore.rollbackTaskImpl(
    taskId: String,
    removeWorktree: Boolean = false,
): TaskRollbackResult {
    return api.call(
        "/api/tasks/${encodeURIComponent(taskId)}/rollback",
        method = "POST",
        body = mapOf("removeWorktree" to removeWorktree),
    )
}
