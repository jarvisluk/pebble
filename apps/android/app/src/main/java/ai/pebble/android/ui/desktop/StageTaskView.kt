package ai.pebble.android.ui.desktop

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.BotTask
import ai.pebble.android.data.BotTaskEvent
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.TaskDiffResult
import ai.pebble.android.data.asDisplayString
import ai.pebble.android.data.errText
import ai.pebble.android.data.formatClockTime
import ai.pebble.android.data.formatDurationMs
import ai.pebble.android.ui.chat.ApprovalCardView
import ai.pebble.android.ui.chat.MarkdownText
import ai.pebble.android.ui.chat.QuestionCardView
import ai.pebble.android.ui.chat.taskDeviceLabel
import ai.pebble.android.ui.chat.taskKindText
import ai.pebble.android.ui.chat.taskStatusColors
import ai.pebble.android.ui.chat.taskStatusText
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonObject
import java.util.Locale

private data class StageTaskTimelineItem(
    val id: String,
    val eventType: String,
    val summary: String,
    val detail: String?,
    val createdAt: String?,
)

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun StageTaskView(
    store: PebbleStore,
    botId: String,
    taskId: String,
    inWideStage: Boolean,
    onOpenDesktop: (String) -> Unit,
    modifier: Modifier = Modifier,
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
    var diff by remember(taskId) { mutableStateOf<TaskDiffResult?>(null) }
    var diffLoading by remember(taskId) { mutableStateOf(false) }
    var diffErr by remember(taskId) { mutableStateOf("") }
    var rollingBack by remember(taskId) { mutableStateOf(false) }
    var patchOpen by remember(taskId) { mutableStateOf(false) }
    var confirm by remember(taskId) { mutableStateOf<ConfirmItem?>(null) }
    var err by remember(taskId) { mutableStateOf("") }

    val task = store.tasks[botId]?.firstOrNull { it.id == taskId } ?: fallbackTask
    val resolvedBotId = botId.ifEmpty { task?.botId.orEmpty() }
    val matchingApproval = store.approvals.firstOrNull { it.taskId == taskId }
    val matchingQuestion = store.questions.firstOrNull { it.taskId == taskId }
    val matchingTakeover = store.takeovers.firstOrNull { it.taskId == taskId }
        ?: if (task?.status == "waiting_takeover") store.takeovers.firstOrNull { it.botId == resolvedBotId } else null

    val hasWorktreeInfo = remember(task?.isolation, task?.checkpointRef, task?.worktreePath) {
        val t = task ?: return@remember false
        t.isolation == "worktree" || !t.checkpointRef.isNullOrEmpty() || !t.worktreePath.isNullOrEmpty()
    }

    val streamed = store.taskStreamText[taskId].orEmpty()
    val persistedOutput = task?.output.orEmpty()
    val liveOutputText = if (task?.isActive == true) {
        streamed.ifEmpty { persistedOutput }
    } else {
        persistedOutput.ifEmpty { streamed }
    }

    val events = store.taskEvents[taskId].orEmpty()
    val timelineRows = remember(events) { buildStageTaskTimelineRows(store, events) }

    suspend fun refreshEventsIncremental() {
        val lastSeq = store.taskEvents[taskId]?.lastOrNull()?.seq ?: 0L
        runCatching { store.fetchTaskEvents(taskId, afterSeq = lastSeq) }
    }

    suspend fun loadDiff() {
        diffLoading = true
        runCatching { store.fetchTaskDiff(taskId) }
            .onSuccess {
                diff = it
                diffErr = ""
            }
            .onFailure {
                diffErr = errText(it, store.t("opFailed"))
            }
        diffLoading = false
    }

    LaunchedEffect(taskId) {
        if (taskId.isBlank()) return@LaunchedEffect
        diff = null
        diffErr = ""
        patchOpen = false
        err = ""
        val loadedTask = task ?: runCatching { store.api.call<BotTask>("/api/tasks/$taskId") }
            .getOrNull()?.also {
                fallbackTask = it
                store.upsertBotTask(it)
            }
        runCatching { store.fetchTaskEvents(taskId) }
        val wt = loadedTask?.let {
            it.isolation == "worktree" || !it.checkpointRef.isNullOrEmpty() || !it.worktreePath.isNullOrEmpty()
        } ?: false
        if (wt) {
            loadDiff()
        }
    }

    LaunchedEffect(task?.status, store.taskTools[taskId]) {
        if (taskId.isBlank()) return@LaunchedEffect
        refreshEventsIncremental()
        if (hasWorktreeInfo && task?.isTerminal == true) {
            loadDiff()
        }
    }

    fun sendFollowUp() {
        val msg = followUp.trim()
        if (msg.isEmpty() || sending || taskId.isBlank()) return
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

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    val sectionBg = if (inWideStage) p.s1 else p.card
    val nestedBg = if (inWideStage) p.card else p.s1
    val sectionShape = RoundedCornerShape(16.dp)

    val stageBody: @Composable () -> Unit = {
        Column(modifier = Modifier.fillMaxSize()) {
            // Scrollable centered content
            Box(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
                    .padding(16.dp),
                contentAlignment = Alignment.TopCenter,
            ) {
                Column(
                    modifier = Modifier
                        .widthIn(max = 760.dp)
                        .fillMaxWidth(),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(bottom = 2.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                            Text(
                                text = task?.displayTitle ?: taskId,
                                color = p.text,
                                fontSize = PebbleFont.title,
                                fontWeight = FontWeight.SemiBold,
                                maxLines = 2,
                                overflow = TextOverflow.Ellipsis,
                            )
                            if (task != null) {
                                val dev = taskDeviceLabel(task.deviceId, store.nodes) ?: store.t("taskDeviceLocal")
                                val meta = listOfNotNull(
                                    taskKindText(task.kind, store),
                                    dev,
                                    task.harness?.takeIf { it.isNotEmpty() },
                                ).joinToString(" · ")
                                if (meta.isNotEmpty()) {
                                    Text(
                                        text = meta,
                                        color = p.text2,
                                        fontSize = PebbleFont.caption,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                            }
                        }

                        if (task != null) {
                            val (fg, bg) = taskStatusColors(task.status, p)
                            TagPill(taskStatusText(task.status, store), bg, fg)
                            if (task.isActive) {
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
                                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                ) {
                                    Text(store.t("taskCancelBtn"), color = p.danger, fontSize = PebbleFont.secondary)
                                }
                            }
                        }
                    }

                    if (err.isNotEmpty()) {
                        StatusTextBanner(text = err, error = true)
                    }

                    if (task != null) {
                        val activeTool = if (task.status == "running") store.taskTools[task.id].orEmpty() else ""
                        val dur = formatDurationMs(task.durationMs)
                        val branch = diff?.branch?.takeIf { it.isNotEmpty() } ?: task.branch?.takeIf { it.isNotEmpty() }
                        val details = listOfNotNull(
                            task.model?.takeIf { it.isNotEmpty() },
                            task.permissionMode?.takeIf { it.isNotEmpty() },
                            task.isolation?.takeIf { it.isNotEmpty() },
                            branch,
                            dur.ifEmpty { null },
                            task.costUsd?.takeIf { it > 0 }?.let { String.format(Locale.US, "$%.4f", it) },
                        )

                        Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(sectionShape)
                                .background(sectionBg)
                                .padding(14.dp),
                            verticalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
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
                        }
                    }

                    if (matchingApproval != null) {
                        ApprovalCardView(store = store, approval = matchingApproval, pal = pal)
                    }

                    if (matchingQuestion != null) {
                        QuestionCardView(store = store, question = matchingQuestion, pal = pal)
                    }

                    if (matchingTakeover != null) {
                        Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(sectionShape)
                                .background(sectionBg)
                                .padding(14.dp),
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
                                        val dest = matchingTakeover.target?.takeIf { it.isNotEmpty() } ?: "local"
                                        onOpenDesktop(dest)
                                    },
                                    colors = ButtonDefaults.buttonColors(containerColor = pal.accent, contentColor = pal.onAccent),
                                ) {
                                    Text(store.t("taskOpenTakeover"), fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                                }
                            }
                        }
                    }

                    // Live output card
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(sectionShape)
                            .background(sectionBg)
                            .padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Text(store.t("taskOutputTitle"), color = p.mute, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                        if (liveOutputText.trim().isEmpty()) {
                            Text(store.t("taskOutputEmpty"), color = p.text2, fontSize = PebbleFont.secondary)
                        } else {
                            MarkdownText(text = liveOutputText)
                        }
                    }

                    // Worktree Diff & Rollback card
                    if (hasWorktreeInfo && task != null) {
                        val checkpoint = diff?.checkpointRef?.takeIf { it.isNotEmpty() } ?: task.checkpointRef.orEmpty()
                        Column(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(sectionShape)
                                .background(sectionBg)
                                .padding(14.dp),
                            verticalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Text(
                                    text = store.t("taskDiffTitle"),
                                    color = p.mute,
                                    fontSize = PebbleFont.secondary,
                                    fontWeight = FontWeight.SemiBold,
                                    modifier = Modifier.weight(1f),
                                )
                                OutlinedButton(
                                    onClick = {
                                        if (!diffLoading) {
                                            scope.launch { loadDiff() }
                                        }
                                    },
                                    enabled = !diffLoading,
                                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                ) {
                                    Text(store.t("taskDiffRefresh"), color = p.text, fontSize = PebbleFont.caption)
                                }
                                if (task.isTerminal && checkpoint.isNotEmpty()) {
                                    OutlinedButton(
                                        onClick = {
                                            confirm = ConfirmItem(
                                                title = store.t("taskRollbackConfirmTitle", mapOf("title" to task.displayTitle)),
                                                message = store.t("taskRollbackConfirmDesc"),
                                            ) {
                                                if (!rollingBack) {
                                                    rollingBack = true
                                                    scope.launch {
                                                        runCatching { store.rollbackTask(taskId, removeWorktree = false) }
                                                            .onSuccess {
                                                                diffErr = ""
                                                                loadDiff()
                                                            }
                                                            .onFailure { diffErr = errText(it, store.t("opFailed")) }
                                                        rollingBack = false
                                                    }
                                                }
                                            }
                                        },
                                        enabled = !rollingBack,
                                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                    ) {
                                        Text(store.t("taskRollbackBtn"), color = p.danger, fontSize = PebbleFont.caption)
                                    }
                                }
                            }

                            when {
                                diffErr.isNotEmpty() -> StatusTextBanner(text = diffErr, error = true)
                                diffLoading && diff == null -> CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
                                diff != null && (diff!!.files.isNotEmpty() || diff!!.patch.isNotEmpty()) -> {
                                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                        diff!!.files.forEach { file ->
                                            Row(
                                                modifier = Modifier.fillMaxWidth(),
                                                verticalAlignment = Alignment.CenterVertically,
                                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                                            ) {
                                                Text(
                                                    text = file.status ?: "M",
                                                    color = p.text2,
                                                    fontFamily = MonoFont,
                                                    fontSize = PebbleFont.caption,
                                                    fontWeight = FontWeight.Bold,
                                                    modifier = Modifier
                                                        .clip(RoundedCornerShape(6.dp))
                                                        .background(nestedBg)
                                                        .padding(horizontal = 6.dp, vertical = 2.dp),
                                                )
                                                Text(
                                                    text = file.path,
                                                    color = p.text,
                                                    fontFamily = MonoFont,
                                                    fontSize = PebbleFont.caption,
                                                    maxLines = 1,
                                                    overflow = TextOverflow.Ellipsis,
                                                    modifier = Modifier.weight(1f),
                                                )
                                                val adds = file.additions ?: 0
                                                val dels = file.deletions ?: 0
                                                if (adds > 0 || dels > 0) {
                                                    Text(
                                                        text = "+$adds -$dels",
                                                        color = p.text2,
                                                        fontFamily = MonoFont,
                                                        fontSize = PebbleFont.caption,
                                                    )
                                                }
                                            }
                                        }
                                        if (diff!!.patch.isNotEmpty()) {
                                            OutlinedButton(
                                                onClick = { patchOpen = !patchOpen },
                                                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                            ) {
                                                Text(
                                                    text = store.t(if (patchOpen) "taskDiffHidePatch" else "taskDiffShowPatch"),
                                                    color = p.text,
                                                    fontSize = PebbleFont.caption,
                                                )
                                            }
                                            if (patchOpen) {
                                                Box(
                                                    modifier = Modifier
                                                        .fillMaxWidth()
                                                        .clip(RoundedCornerShape(10.dp))
                                                        .background(nestedBg)
                                                        .horizontalScroll(rememberScrollState())
                                                        .padding(10.dp),
                                                ) {
                                                    SelectionContainer {
                                                        Text(
                                                            text = diff!!.patch,
                                                            color = p.text,
                                                            fontFamily = MonoFont,
                                                            fontSize = PebbleFont.caption,
                                                        )
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                                else -> Text(store.t("taskDiffEmpty"), color = p.text2, fontSize = PebbleFont.secondary)
                            }
                        }
                    }

                    // Event timeline card
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(sectionShape)
                            .background(sectionBg)
                            .padding(14.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Text(store.t("taskTimelineTitle"), color = p.mute, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                        if (timelineRows.isEmpty()) {
                            Text(store.t("taskTimelineEmpty"), color = p.text2, fontSize = PebbleFont.secondary)
                        } else {
                            timelineRows.forEachIndexed { idx, row ->
                                if (idx > 0) HorizontalDivider(thickness = 0.5.dp, color = p.s2)
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

                    Spacer(Modifier.height(4.dp))
                }
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

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(if (inWideStage) p.card else p.page),
    ) {
        stageBody()
    }
}

private fun stageEventField(ev: BotTaskEvent, key: String): String? {
    val obj = ev.payload as? JsonObject ?: return null
    return obj[key]?.asDisplayString()
}

private fun buildStageTaskTimelineRows(store: PebbleStore, events: List<BotTaskEvent>): List<StageTaskTimelineItem> {
    val rows = mutableListOf<StageTaskTimelineItem>()
    val chunkBuf = StringBuilder()
    var chunkStartSeq = 0L
    var chunkTime: String? = null

    fun flushChunks() {
        val text = chunkBuf.toString().trim()
        if (text.isNotEmpty()) {
            rows.add(
                StageTaskTimelineItem(
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
            val piece = stageEventField(ev, "chunk").orEmpty()
            if (chunkStartSeq == 0L) {
                chunkStartSeq = ev.seq
                chunkTime = ev.createdAt
            }
            chunkBuf.append(piece)
            continue
        }
        flushChunks()
        rows.add(formatStageTaskEvent(store, ev))
    }
    flushChunks()
    return rows
}

private fun formatStageTaskEvent(store: PebbleStore, ev: BotTaskEvent): StageTaskTimelineItem = when (ev.eventType) {
    "status" -> {
        val st = stageEventField(ev, "status").orEmpty()
        StageTaskTimelineItem(
            id = "ev_${ev.seq}",
            eventType = ev.eventType,
            summary = taskStatusText(st, store),
            detail = stageEventField(ev, "error"),
            createdAt = ev.createdAt,
        )
    }
    "tool_use" -> StageTaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = stageEventField(ev, "tool").orEmpty(),
        detail = null,
        createdAt = ev.createdAt,
    )
    "message" -> {
        val role = stageEventField(ev, "role").orEmpty()
        val text = stageEventField(ev, "text").orEmpty()
        StageTaskTimelineItem(
            id = "ev_${ev.seq}",
            eventType = ev.eventType,
            summary = if (role.isEmpty()) text else "[$role] $text",
            detail = null,
            createdAt = ev.createdAt,
        )
    }
    "approval" -> StageTaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = stageEventField(ev, "toolName") ?: stageEventField(ev, "title") ?: store.t("approvalTitle"),
        detail = stageEventField(ev, "summary"),
        createdAt = ev.createdAt,
    )
    "question" -> StageTaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = stageEventField(ev, "title") ?: store.t("questionTitle"),
        detail = null,
        createdAt = ev.createdAt,
    )
    "takeover" -> StageTaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = stageEventField(ev, "target") ?: "local",
        detail = stageEventField(ev, "state"),
        createdAt = ev.createdAt,
    )
    "error" -> StageTaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = stageEventField(ev, "error") ?: store.t("opFailed"),
        detail = null,
        createdAt = ev.createdAt,
    )
    else -> StageTaskTimelineItem(
        id = "ev_${ev.seq}",
        eventType = ev.eventType,
        summary = ev.eventType,
        detail = null,
        createdAt = ev.createdAt,
    )
}
