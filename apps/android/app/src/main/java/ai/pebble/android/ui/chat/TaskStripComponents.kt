package ai.pebble.android.ui.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.BotTask
import ai.pebble.android.data.NodeInfo
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.data.formatDurationMs
import ai.pebble.android.data.formatShortTime
import ai.pebble.android.data.plainPreview
import ai.pebble.android.ui.theme.BotAccent
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.PebblePalette
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import kotlinx.coroutines.launch
import java.util.Locale

internal enum class TaskFilterKind { ALL, WORKER, ROUTINE, DESKTOP }

internal fun taskDeviceLabel(deviceId: String?, nodes: List<NodeInfo>): String? {
    val clean = deviceId.orEmpty().trim()
    if (clean.isEmpty() || clean == "local") return null
    val match = nodes.firstOrNull { it.id == clean }
    return match?.name?.takeIf { it.isNotEmpty() } ?: clean
}

internal fun taskStatusColors(status: String, p: PebblePalette): Pair<Color, Color> = when (status) {
    "running" -> p.sageInk to p.sage
    "queued" -> p.text2 to p.s1
    "waiting_approval", "waiting_question" -> p.busy to p.busy.copy(alpha = 0.16f)
    "waiting_takeover" -> p.pinkInk to p.pink
    "completed" -> p.sageInk to p.sage
    "failed" -> p.pinkInk to p.pink
    "cancelled" -> p.mute to p.s1
    else -> p.text2 to p.s1
}

internal fun taskStatusText(status: String, store: PebbleStore): String {
    val key = "taskStatus_$status"
    val value = store.t(key)
    return if (value == key) status else value
}

internal fun taskKindText(kind: String, store: PebbleStore): String {
    val key = "taskKind_$kind"
    val value = store.t(key)
    return if (value == key) kind else value
}

internal fun stripTasks(allTasks: List<BotTask>): List<BotTask> = allTasks.filter { it.isActive }.take(6)

@Composable
fun BotChatTaskStrip(
    store: PebbleStore,
    botId: String,
    pal: BotAccent,
    onSelectTask: (BotTask) -> Unit,
) {
    val p = LocalPalette.current
    val items = stripTasks(store.tasks[botId].orEmpty())
    if (items.isEmpty()) return

    LazyRow(
        modifier = Modifier.fillMaxWidth(),
        contentPadding = PaddingValues(horizontal = 4.dp, vertical = 2.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(items, key = { it.id }) { task ->
            val (fg, bg) = taskStatusColors(task.status, p)
            val deviceLabel = taskDeviceLabel(task.deviceId, store.nodes)
            val activeTool = if (task.status == "running") store.taskTools[task.id].orEmpty() else ""
            val harness = task.harness.orEmpty().trim()
            val borderColor = if (task.isWaiting) fg.copy(alpha = 0.45f) else p.s2

            Surface(
                color = p.card,
                shape = CircleShape,
                modifier = Modifier
                    .clip(CircleShape)
                    .border(0.75.dp, borderColor, CircleShape)
                    .clickable { onSelectTask(task) },
            ) {
                Row(
                    modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Box(
                        modifier = Modifier
                            .size(7.dp)
                            .clip(CircleShape)
                            .background(if (task.isActive) fg else p.mute),
                    )
                    Text(
                        text = task.displayTitle,
                        color = p.text,
                        fontSize = PebbleFont.caption,
                        fontWeight = FontWeight.SemiBold,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.widthIn(max = 160.dp),
                    )
                    if (deviceLabel != null) {
                        Surface(color = p.lilac, shape = CircleShape) {
                            Text(
                                text = deviceLabel,
                                color = p.lilacInk,
                                fontSize = PebbleFont.caption,
                                fontWeight = FontWeight.Medium,
                                maxLines = 1,
                                modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                            )
                        }
                    }
                    if (harness.isNotEmpty()) {
                        Text(
                            text = harness,
                            color = p.text2,
                            fontFamily = MonoFont,
                            fontSize = PebbleFont.caption,
                            maxLines = 1,
                        )
                    }
                    if (activeTool.isNotEmpty()) {
                        Text(
                            text = activeTool,
                            color = pal.accent,
                            fontFamily = MonoFont,
                            fontSize = PebbleFont.caption,
                            maxLines = 1,
                        )
                    }
                    Surface(color = bg, shape = CircleShape) {
                        Text(
                            text = taskStatusText(task.status, store),
                            color = fg,
                            fontSize = PebbleFont.caption,
                            fontWeight = FontWeight.SemiBold,
                            maxLines = 1,
                            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
internal fun BotTasksBlock(
    store: PebbleStore,
    botId: String,
    onSelectTask: (BotTask) -> Unit,
    onError: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    var filter by remember(botId) { mutableStateOf(TaskFilterKind.ALL) }
    var loading by remember(botId) { mutableStateOf(false) }

    val allTasks = store.tasks[botId].orEmpty()
    val filtered = when (filter) {
        TaskFilterKind.ALL -> allTasks
        TaskFilterKind.WORKER -> allTasks.filter { it.kind == "worker" }
        TaskFilterKind.ROUTINE -> allTasks.filter { it.kind == "routine" }
        TaskFilterKind.DESKTOP -> allTasks.filter { it.kind == "desktop" }
    }.take(6)

    SectionHeader(
        title = store.t("tasksTitle"),
        trailing = {
            IconButton(
                onClick = {
                    if (!loading) {
                        loading = true
                        scope.launch {
                            runCatching { store.fetchBotTasks(botId) }
                                .onFailure { onError(errText(it, store.t("connFailed"))) }
                            loading = false
                        }
                    }
                },
                modifier = Modifier.size(28.dp),
            ) {
                Icon(
                    painterResource(PebbleIcons.Refresh),
                    contentDescription = store.t("refresh"),
                    tint = p.text2,
                    modifier = Modifier.size(18.dp),
                )
            }
        },
    )

    val filters = listOf(
        TaskFilterKind.ALL to store.t("taskFilterAll"),
        TaskFilterKind.WORKER to store.t("taskFilterWorkers"),
        TaskFilterKind.ROUTINE to store.t("taskFilterRoutines"),
        TaskFilterKind.DESKTOP to store.t("taskFilterDesktop"),
    )
    SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
        filters.forEachIndexed { idx, (kind, label) ->
            SegmentedButton(
                selected = filter == kind,
                onClick = { filter = kind },
                shape = SegmentedButtonDefaults.itemShape(idx, filters.size),
                label = { Text(label, fontSize = PebbleFont.caption, maxLines = 1, overflow = TextOverflow.Ellipsis) },
            )
        }
    }

    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (filtered.isEmpty()) {
                Text(store.t("taskEmpty"), color = p.mute, fontSize = PebbleFont.secondary)
            } else {
                filtered.forEachIndexed { idx, task ->
                    if (idx > 0) CardDivider()
                    val (fg, bg) = taskStatusColors(task.status, p)
                    val deviceLabel = taskDeviceLabel(task.deviceId, store.nodes)
                    val activeTool = if (task.status == "running") store.taskTools[task.id].orEmpty() else ""
                    val dur = formatDurationMs(task.durationMs)
                    val stamp = formatShortTime(task.startedAt ?: task.createdAt)
                    val meta = listOfNotNull(
                        taskKindText(task.kind, store),
                        task.harness?.takeIf { it.isNotEmpty() },
                        task.model?.takeIf { it.isNotEmpty() },
                        dur.ifEmpty { null },
                        task.costUsd?.takeIf { it > 0 }?.let { String.format(Locale.US, "$%.4f", it) },
                        stamp.ifEmpty { null },
                    ).joinToString(" · ")

                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { onSelectTask(task) }
                            .padding(vertical = 2.dp),
                        verticalArrangement = Arrangement.spacedBy(5.dp),
                    ) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            Text(
                                text = task.displayTitle,
                                color = p.text,
                                fontSize = PebbleFont.cardTitle,
                                fontWeight = FontWeight.SemiBold,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.weight(1f),
                            )
                            if (deviceLabel != null) {
                                TagPill(deviceLabel, p.lilac, p.lilacInk)
                            }
                            TagPill(taskStatusText(task.status, store), bg, fg)
                        }
                        if (meta.isNotEmpty()) {
                            Text(meta, color = p.text2, fontSize = PebbleFont.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        when {
                            activeTool.isNotEmpty() -> {
                                Text(
                                    text = store.t("callingTool", mapOf("tool" to activeTool)),
                                    color = p.busy,
                                    fontFamily = MonoFont,
                                    fontSize = PebbleFont.caption,
                                    maxLines = 1,
                                )
                            }
                            !task.error.isNullOrEmpty() -> {
                                Text(task.error, color = p.danger, fontSize = PebbleFont.secondary, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            }
                            !task.output.isNullOrBlank() -> {
                                Text(plainPreview(task.output.trim()), color = p.text2, fontSize = PebbleFont.secondary, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            }
                            task.prompt.isNotEmpty() -> {
                                Text(task.prompt, color = p.mute, fontSize = PebbleFont.secondary, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            }
                        }
                    }
                }
            }
        }
    }
}

