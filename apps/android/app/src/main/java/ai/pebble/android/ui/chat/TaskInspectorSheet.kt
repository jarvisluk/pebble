package ai.pebble.android.ui.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.BotTask
import ai.pebble.android.data.BotTaskEvent
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.asDisplayString
import ai.pebble.android.data.errText
import ai.pebble.android.data.formatClockTime
import ai.pebble.android.data.formatDurationMs
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import java.util.Locale

private data class TaskTimelineItem(
    val id: String,
    val eventType: String,
    val summary: String,
    val detail: String?,
    val createdAt: String?,
)

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun TaskInspectorSheet(
    store: PebbleStore,
    botId: String,
    taskId: String,
    onDismiss: () -> Unit,
    onOpenDesktop: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    val botIndex = store.bots.indexOfFirst { it.id == botId }
    val bot = if (botIndex >= 0) store.bots[botIndex] else null
    val pal = p.bot(botId, botIndex.coerceAtLeast(0), bot?.color)

    var fallbackTask by remember(taskId) { mutableStateOf<BotTask?>(null) }
    var followUp by remember(taskId) { mutableStateOf("") }
    var sending by remember(taskId) { mutableStateOf(false) }
    var cancelling by remember(taskId) { mutableStateOf(false) }
    var resolvingTakeover by remember(taskId) { mutableStateOf(false) }
    var err by remember(taskId) { mutableStateOf("") }

    val task = store.tasks[botId]?.firstOrNull { it.id == taskId } ?: fallbackTask
    val matchingApproval = store.approvals.firstOrNull { it.taskId == taskId }
    val matchingQuestion = store.questions.firstOrNull { it.taskId == taskId }
    val matchingTakeover = store.takeovers.firstOrNull { it.taskId == taskId }
        ?: if (task?.status == "waiting_takeover") store.takeovers.firstOrNull { it.botId == botId } else null

    val streamed = store.taskStreamText[taskId].orEmpty()
    val persistedOutput = task?.output.orEmpty()
    val liveOutputText = if (task?.isActive == true) {
        streamed.ifEmpty { persistedOutput }
    } else {
        persistedOutput.ifEmpty { streamed }
    }

    val events = store.taskEvents[taskId].orEmpty()
    val timelineRows = remember(events) { buildTimelineRows(store, events) }

    suspend fun refreshEventsIncremental() {
        val lastSeq = store.taskEvents[taskId]?.lastOrNull()?.seq ?: 0L
        runCatching { store.fetchTaskEvents(taskId, afterSeq = lastSeq) }
    }

    LaunchedEffect(taskId) {
        if (task == null) {
            runCatching { store.api.call<BotTask>("/api/tasks/$taskId") }
                .getOrNull()?.let {
                    fallbackTask = it
                    store.upsertBotTask(it)
                }
        }
        runCatching { store.fetchTaskEvents(taskId) }
    }

    LaunchedEffect(task?.status, store.taskTools[taskId]) {
        refreshEventsIncremental()
    }

    fun sendFollowUp() {
        val msg = followUp.trim()
        if (msg.isEmpty() || sending) return
        followUp = ""
        err = ""
        sending = true
        scope.launch {
            try {
                val updated = store.sendToTask(taskId, msg)
                fallbackTask = updated
                refreshEventsIncremental()
            } catch (e: Throwable) {
                followUp = msg
                err = errText(e, store.t("taskSendFailed"))
            } finally {
                sending = false
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .imePadding()
            .navigationBarsPadding(),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            IconButton(onClick = onDismiss) {
                Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text)
            }
            Text(
                text = task?.displayTitle ?: taskId,
                color = p.text,
                fontSize = PebbleFont.title,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
        }

        Column(
            modifier = Modifier
                .weight(1f, fill = false)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 6.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (err.isNotEmpty()) {
                StatusTextBanner(text = err, error = true)
            }

            if (task != null) {
                val (fg, bg) = taskStatusColors(task.status, p)
                val deviceLabel = taskDeviceLabel(task.deviceId, store.nodes)
                val activeTool = if (task.status == "running") store.taskTools[task.id].orEmpty() else ""
                val dur = formatDurationMs(task.durationMs)
                val details = listOfNotNull(
                    task.model?.takeIf { it.isNotEmpty() },
                    task.permissionMode?.takeIf { it.isNotEmpty() },
                    dur.ifEmpty { null },
                    task.costUsd?.takeIf { it > 0 }?.let { String.format(Locale.US, "$%.4f", it) },
                )

                SectionCard {
                    Column(
                        modifier = Modifier.padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        FlowRow(
                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                            verticalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            TagPill(taskStatusText(task.status, store), bg, fg)
                            TagPill(taskKindText(task.kind, store), p.s1, p.text2)
                            if (deviceLabel != null) {
                                TagPill(deviceLabel, p.lilac, p.lilacInk)
                            } else {
                                TagPill(store.t("taskDeviceLocal"), p.s1, p.text2)
                            }
                            task.harness?.takeIf { it.isNotEmpty() }?.let {
                                TagPill(it, p.s1, p.text2)
                            }
                        }

                        if (details.isNotEmpty()) {
                            Text(
                                text = details.joinToString(" · "),
                                color = p.text2,
                                fontFamily = MonoFont,
                                fontSize = PebbleFont.caption,
                            )
                        }

                        task.workdir?.takeIf { it.isNotEmpty() }?.let { wd ->
                            SelectionContainer {
                                Text(wd, color = p.mute, fontFamily = MonoFont, fontSize = PebbleFont.caption)
                            }
                        }

                        if (activeTool.isNotEmpty()) {
                            Text(
                                text = store.t("callingTool", mapOf("tool" to activeTool)),
                                color = p.busy,
                                fontFamily = MonoFont,
                                fontSize = PebbleFont.secondary,
                            )
                        }

                        if (task.prompt.isNotEmpty()) {
                            SelectionContainer {
                                Text(task.prompt, color = p.text2, fontSize = PebbleFont.secondary)
                            }
                        }

                        task.error?.takeIf { it.isNotEmpty() }?.let { e ->
                            StatusTextBanner(text = e, error = true)
                        }

                        if (task.isActive) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.End,
                            ) {
                                OutlinedButton(
                                    onClick = {
                                        if (!cancelling) {
                                            cancelling = true
                                            err = ""
                                            scope.launch {
                                                runCatching { store.cancelTask(taskId) }
                                                    .onSuccess {
                                                        fallbackTask = it
                                                        refreshEventsIncremental()
                                                    }
                                                    .onFailure { err = errText(it, store.t("taskCancelFailed")) }
                                                cancelling = false
                                            }
                                        }
                                    },
                                    enabled = !cancelling,
                                    contentPadding = PaddingValues(horizontal = 14.dp, vertical = 6.dp),
                                ) {
                                    Text(store.t("taskCancelBtn"), color = p.danger, fontSize = PebbleFont.secondary)
                                }
                            }
                        }
                    }
                }
            }

            if (matchingApproval != null) {
                ApprovalCardView(store = store, approval = matchingApproval, pal = pal)
            }

            if (matchingQuestion != null) {
                QuestionCardView(store = store, question = matchingQuestion, pal = pal)
            }

            if (matchingTakeover != null) {
                SectionCard {
                    Column(
                        modifier = Modifier.padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Text(
                                text = store.t("taskWaitingTakeoverTitle"),
                                color = p.text,
                                fontSize = PebbleFont.cardTitle,
                                fontWeight = FontWeight.Bold,
                                modifier = Modifier.weight(1f),
                            )
                            TagPill(store.t("takeoverWaitingBadge"), p.pink, p.pinkInk)
                        }
                        matchingTakeover.reason?.takeIf { it.isNotEmpty() }?.let { r ->
                            Text(r, color = p.text2, fontSize = PebbleFont.body)
                        }
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End),
                        ) {
                            OutlinedButton(
                                onClick = {
                                    if (!resolvingTakeover) {
                                        resolvingTakeover = true
                                        scope.launch {
                                            runCatching { store.resolveTakeover(matchingTakeover.id) }
                                                .onFailure { err = errText(it, store.t("opFailed")) }
                                            resolvingTakeover = false
                                        }
                                    }
                                },
                                enabled = !resolvingTakeover,
                            ) {
                                Text(store.t("resumeShort"), color = p.text, fontSize = PebbleFont.secondary)
                            }
                            Button(
                                onClick = {
                                    val target = matchingTakeover.target?.takeIf { it.isNotEmpty() } ?: "local"
                                    onDismiss()
                                    onOpenDesktop(target)
                                },
                                colors = ButtonDefaults.buttonColors(containerColor = pal.accent, contentColor = pal.onAccent),
                            ) {
                                Text(store.t("taskOpenTakeover"), fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                            }
                        }
                    }
                }
            }

            // Live output card
            SectionCard {
                Column(
                    modifier = Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(store.t("taskOutputTitle"), color = p.mute, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                    if (liveOutputText.trim().isEmpty()) {
                        Text(store.t("taskOutputEmpty"), color = p.text2, fontSize = PebbleFont.secondary)
                    } else {
                        MarkdownText(text = liveOutputText)
                    }
                }
            }

            // Event timeline card
            SectionCard {
                Column(
                    modifier = Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(store.t("taskTimelineTitle"), color = p.mute, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                    if (timelineRows.isEmpty()) {
                        Text(store.t("taskTimelineEmpty"), color = p.text2, fontSize = PebbleFont.secondary)
                    } else {
                        timelineRows.forEachIndexed { idx, row ->
                            if (idx > 0) CardDivider()
                            Column(verticalArrangement = Arrangement.spacedBy(3.dp)) {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    val key = "taskEvent_${row.eventType}"
                                    val typeLabel = store.t(key).let { if (it == key) row.eventType else it }
                                    Text(
                                        text = typeLabel,
                                        color = if (row.eventType == "error") p.danger else p.text2,
                                        fontSize = PebbleFont.caption,
                                        fontWeight = FontWeight.SemiBold,
                                        modifier = Modifier.weight(1f),
                                    )
                                    val clk = formatClockTime(row.createdAt)
                                    if (clk.isNotEmpty()) {
                                        Text(clk, color = p.mute, fontSize = PebbleFont.caption)
                                    }
                                }
                                SelectionContainer {
                                    Text(
                                        text = row.summary,
                                        color = if (row.eventType == "error") p.danger else p.text,
                                        fontFamily = if (row.eventType == "tool_use") MonoFont else null,
                                        fontSize = PebbleFont.secondary,
                                        maxLines = if (row.eventType == "chunk") 4 else 6,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                                row.detail?.takeIf { it.isNotEmpty() }?.let { d ->
                                    Text(d, color = p.text2, fontSize = PebbleFont.caption, maxLines = 3, overflow = TextOverflow.Ellipsis)
                                }
                            }
                        }
                    }
                }
            }

            Spacer(Modifier.height(6.dp))
        }

        if (task?.status != "cancelled") {
            HorizontalDivider(thickness = 0.5.dp, color = p.s2)
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                OutlinedTextField(
                    value = followUp,
                    onValueChange = { followUp = it },
                    placeholder = { Text(store.t("taskSendPlaceholder"), fontSize = PebbleFont.secondary) },
                    maxLines = 4,
                    textStyle = TextStyle(color = p.text, fontSize = PebbleFont.body),
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.weight(1f),
                )
                Button(
                    onClick = { sendFollowUp() },
                    enabled = !sending && followUp.trim().isNotEmpty(),
                    colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                    contentPadding = PaddingValues(horizontal = 16.dp, vertical = 10.dp),
                ) {
                    if (sending) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.onAccent)
                    } else {
                        Text(store.t("taskSendBtn"), fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                    }
                }
            }
        }
    }
}

private fun eventField(ev: BotTaskEvent, key: String): String? {
    val obj = ev.payload as? JsonObject ?: return null
    return obj[key]?.asDisplayString()
}

private fun buildTimelineRows(store: PebbleStore, events: List<BotTaskEvent>): List<TaskTimelineItem> {
    val rows = mutableListOf<TaskTimelineItem>()
    val chunkBuf = StringBuilder()
    var chunkStartSeq = 0L
    var chunkTime: String? = null

    fun flushChunks() {
        val text = chunkBuf.toString().trim()
        if (text.isNotEmpty()) {
            rows.add(
                TaskTimelineItem(
                    id = "chunk_$chunkStartSeq",
                    eventType = "chunk",
                    summary = text,
                    detail = null,
                    createdAt = chunkTime,
                ),
            )
        }
        chunkBuf.clear()
        chunkStartSeq = 0L
        chunkTime = null
    }

    for (ev in events) {
        if (ev.eventType == "chunk") {
            val piece = eventField(ev, "chunk").orEmpty()
            if (chunkStartSeq == 0L) {
                chunkStartSeq = ev.seq
                chunkTime = ev.createdAt
            }
            chunkBuf.append(piece)
            continue
        }
        flushChunks()
        rows.add(formatTaskEvent(store, ev))
    }
    flushChunks()
    return rows
}

private fun formatTaskEvent(store: PebbleStore, ev: BotTaskEvent): TaskTimelineItem = when (ev.eventType) {
    "status" -> {
        val st = eventField(ev, "status").orEmpty()
        TaskTimelineItem(
            id = "ev_${ev.seq}",
            eventType = ev.eventType,
            summary = taskStatusText(st, store),
            detail = eventField(ev, "error"),
            createdAt = ev.createdAt,
        )
    }
    "tool_use" -> TaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = eventField(ev, "tool").orEmpty(),
        detail = null,
        createdAt = ev.createdAt,
    )
    "message" -> {
        val role = eventField(ev, "role").orEmpty()
        val text = eventField(ev, "text").orEmpty()
        TaskTimelineItem(
            id = "ev_${ev.seq}",
            eventType = ev.eventType,
            summary = if (role.isEmpty()) text else "[$role] $text",
            detail = null,
            createdAt = ev.createdAt,
        )
    }
    "approval" -> TaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = eventField(ev, "toolName") ?: eventField(ev, "title") ?: store.t("approvalTitle"),
        detail = eventField(ev, "summary"),
        createdAt = ev.createdAt,
    )
    "question" -> TaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = eventField(ev, "title") ?: store.t("questionTitle"),
        detail = null,
        createdAt = ev.createdAt,
    )
    "takeover" -> TaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = eventField(ev, "target") ?: "local",
        detail = eventField(ev, "state"),
        createdAt = ev.createdAt,
    )
    "error" -> TaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = eventField(ev, "error") ?: store.t("opFailed"),
        detail = null,
        createdAt = ev.createdAt,
    )
    else -> TaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = ev.eventType,
        detail = null,
        createdAt = ev.createdAt,
    )
}
