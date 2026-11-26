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
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.BotTask
import ai.pebble.android.data.NodeInfo
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.data.formatDurationMs
import ai.pebble.android.data.formatShortTime
import ai.pebble.android.data.plainPreview
import ai.pebble.android.ui.settings.ModelInputWithMenu
import ai.pebble.android.ui.settings.settingsHarnessList
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
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
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

internal fun stripTasks(allTasks: List<BotTask>): List<BotTask> {
    val active = allTasks.filter { it.isActive }
    val recentDone = allTasks.filter { it.isTerminal }.take((5 - active.size).coerceAtLeast(0))
    return (active + recentDone).take(6)
}

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

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(p.page),
    ) {
        LazyRow(
            modifier = Modifier.fillMaxWidth(),
            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 8.dp),
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
        HorizontalDivider(thickness = 0.5.dp, color = p.s2)
    }
}

@Composable
internal fun BotTasksBlock(
    store: PebbleStore,
    botId: String,
    onSelectTask: (BotTask) -> Unit,
    onNewTask: () -> Unit,
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
    }

    SectionHeader(
        title = store.t("tasksTitle"),
        trailing = {
            Row(horizontalArrangement = Arrangement.spacedBy(2.dp), verticalAlignment = Alignment.CenterVertically) {
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
                IconButton(onClick = onNewTask, modifier = Modifier.size(28.dp)) {
                    Icon(
                        painterResource(PebbleIcons.Add),
                        contentDescription = store.t("newTask"),
                        tint = p.accent,
                        modifier = Modifier.size(20.dp),
                    )
                }
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
                        if (task.isActive) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.End,
                            ) {
                                OutlinedButton(
                                    onClick = {
                                        scope.launch {
                                            runCatching { store.cancelTask(task.id) }
                                                .onFailure { onError(errText(it, store.t("taskCancelFailed"))) }
                                        }
                                    },
                                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                ) {
                                    Text(store.t("taskCancelBtn"), color = p.danger, fontSize = PebbleFont.secondary)
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
internal fun NewTaskSheet(
    store: PebbleStore,
    botId: String,
    onDismiss: () -> Unit,
    onCreated: (BotTask) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    val bot = store.bots.firstOrNull { it.id == botId }

    var title by remember { mutableStateOf("") }
    var prompt by remember { mutableStateOf("") }
    var deviceId by remember { mutableStateOf("local") }
    var workdir by remember { mutableStateOf("") }
    var harness by remember { mutableStateOf("") }
    var model by remember { mutableStateOf("") }
    var permissionMode by remember { mutableStateOf("") }
    var delivery by remember { mutableStateOf("receipt") }
    var submitting by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf("") }

    var deviceMenuOpen by remember { mutableStateOf(false) }
    var workdirMenuOpen by remember { mutableStateOf(false) }
    var harnessMenuOpen by remember { mutableStateOf(false) }
    var permMenuOpen by remember { mutableStateOf(false) }
    var loadingModels by remember { mutableStateOf(false) }

    val agentNodes = store.nodes.filter { it.connected == true && it.permissions.orEmpty().contains("agent") }
    val selectedNode = if (deviceId == "local") null else agentNodes.firstOrNull { it.id == deviceId }

    val deviceOptions = listOf("local" to store.t("taskDeviceLocal")) + agentNodes.map { n ->
        n.id to (n.name?.takeIf { it.isNotEmpty() } ?: n.id)
    }

    val harnessOptions: List<Pair<String, String>> = buildList {
        add("" to store.t("taskFollowBot"))
        val remoteHarnesses = selectedNode?.harnesses.orEmpty()
        if (remoteHarnesses.isNotEmpty()) {
            val installed = remoteHarnesses.filter { it.installed == true }.ifEmpty { remoteHarnesses }
            installed.forEach { add(it.kind to it.title) }
        } else {
            val local = settingsHarnessList(store)
            local.filter { it.installed ?: true }.forEach { add(it.id to it.title) }
        }
    }

    val permOptions: List<Pair<String, String>> = buildList {
        add("" to store.t("taskFollowBot"))
        val rank = mapOf("plan" to 0, "ask" to 1, "edit" to 2, "auto" to 3)
        val maxRank = selectedNode?.maxPermissionMode?.lowercase()?.let { rank[it] } ?: 3
        listOf("auto", "edit", "ask", "plan").filter { (rank[it] ?: 0) <= maxRank }.forEach { m ->
            val key = "permMode_$m"
            val label = store.t(key).let { if (it == key) m else it }
            add(m to label)
        }
    }

    val effectiveHarness = harness.ifEmpty { bot?.harness.orEmpty() }
    LaunchedEffect(effectiveHarness, store.isConnected, store.modelsGeneration) {
        if (store.isConnected && effectiveHarness.isNotEmpty() && store.models[effectiveHarness].isNullOrEmpty()) {
            loadingModels = true
            runCatching { store.loadModels(effectiveHarness, force = false) }
            loadingModels = false
        }
    }

    fun submit() {
        val trimmedPrompt = prompt.trim()
        if (trimmedPrompt.isEmpty()) {
            err = store.t("taskPromptRequired")
            return
        }
        if (submitting) return
        err = ""
        submitting = true
        scope.launch {
            try {
                val created = store.spawnBotTask(
                    botId = botId,
                    title = title,
                    prompt = trimmedPrompt,
                    kind = "worker",
                    deviceId = deviceId,
                    harness = harness,
                    model = model,
                    permissionMode = permissionMode,
                    workdir = workdir,
                    delivery = delivery,
                )
                onCreated(created)
                onDismiss()
            } catch (e: Throwable) {
                err = errText(e, store.t("taskSpawnFailed"))
            } finally {
                submitting = false
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .imePadding()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onDismiss) { Text(store.t("cancel"), color = p.text2) }
            Text(
                text = store.t("newTask"),
                color = p.text,
                fontSize = PebbleFont.title,
                fontWeight = FontWeight.Bold,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f),
            )
            if (submitting) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
            } else {
                TextButton(
                    onClick = { submit() },
                    enabled = prompt.trim().isNotEmpty(),
                ) {
                    Text(store.t("taskSpawnBtn"), color = p.accent, fontWeight = FontWeight.SemiBold)
                }
            }
        }

        if (err.isNotEmpty()) StatusTextBanner(text = err, error = true)

        OutlinedTextField(
            value = title,
            onValueChange = { title = it },
            placeholder = { Text(store.t("taskTitlePh"), fontSize = PebbleFont.body) },
            singleLine = true,
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        OutlinedTextField(
            value = prompt,
            onValueChange = { prompt = it },
            placeholder = { Text(store.t("taskPromptPh"), fontSize = PebbleFont.body) },
            minLines = 3,
            maxLines = 8,
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        // Device picker row
        Box {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .background(p.s1)
                    .clickable { deviceMenuOpen = true }
                    .padding(horizontal = 14.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(store.t("taskDeviceLabel"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                Text(
                    text = deviceOptions.firstOrNull { it.first == deviceId }?.second ?: deviceId,
                    color = p.text2,
                    fontSize = PebbleFont.secondary,
                )
                Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
            }
            DropdownMenu(expanded = deviceMenuOpen, onDismissRequest = { deviceMenuOpen = false }, containerColor = p.card) {
                deviceOptions.forEach { (id, label) ->
                    DropdownMenuItem(
                        text = { Text(label, color = p.text, fontSize = PebbleFont.body) },
                        onClick = {
                            deviceId = id
                            deviceMenuOpen = false
                            workdir = if (id == "local") {
                                ""
                            } else {
                                agentNodes.firstOrNull { it.id == id }?.workspaces?.firstOrNull()?.path.orEmpty()
                            }
                            if (harnessOptions.none { it.first == harness }) harness = ""
                            if (permOptions.none { it.first == permissionMode }) permissionMode = ""
                        },
                    )
                }
            }
        }

        // Workdir picker / input when remote device selected
        val remoteWorkspaces = selectedNode?.workspaces.orEmpty()
        if (remoteWorkspaces.isNotEmpty()) {
            Box {
                val curWsLabel = remoteWorkspaces.firstOrNull { it.path == workdir }?.let { "${it.name} (${it.path})" }
                    ?: workdir.ifEmpty { store.t("taskWorkdirPh") }
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .background(p.s1)
                        .clickable { workdirMenuOpen = true }
                        .padding(horizontal = 14.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(store.t("taskWorkdirLabel"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                    Text(curWsLabel, color = p.text2, fontSize = PebbleFont.secondary, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.widthIn(max = 200.dp))
                    Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                }
                DropdownMenu(expanded = workdirMenuOpen, onDismissRequest = { workdirMenuOpen = false }, containerColor = p.card) {
                    DropdownMenuItem(
                        text = { Text(store.t("taskWorkdirPh"), color = p.text2) },
                        onClick = { workdir = ""; workdirMenuOpen = false },
                    )
                    remoteWorkspaces.forEach { ws ->
                        DropdownMenuItem(
                            text = { Text("${ws.name} (${ws.path})", color = p.text, fontSize = PebbleFont.secondary) },
                            onClick = { workdir = ws.path; workdirMenuOpen = false },
                        )
                    }
                }
            }
        } else if (deviceId != "local") {
            OutlinedTextField(
                value = workdir,
                onValueChange = { workdir = it },
                label = { Text(store.t("taskWorkdirLabel"), fontSize = PebbleFont.caption) },
                placeholder = { Text(store.t("taskWorkdirPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
        }

        // Harness picker row
        Box {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .background(p.s1)
                    .clickable { harnessMenuOpen = true }
                    .padding(horizontal = 14.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(store.t("taskHarnessLabel"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                Text(
                    text = harnessOptions.firstOrNull { it.first == harness }?.second ?: harness,
                    color = p.text2,
                    fontSize = PebbleFont.secondary,
                )
                Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
            }
            DropdownMenu(expanded = harnessMenuOpen, onDismissRequest = { harnessMenuOpen = false }, containerColor = p.card) {
                harnessOptions.forEach { (id, label) ->
                    DropdownMenuItem(
                        text = { Text(label, color = p.text, fontSize = PebbleFont.body) },
                        onClick = { harness = id; harnessMenuOpen = false },
                    )
                }
            }
        }

        // Model field
        if (effectiveHarness.isNotEmpty()) {
            ModelInputWithMenu(
                value = model,
                placeholder = store.t("taskModelPh"),
                candidates = store.models[effectiveHarness].orEmpty(),
                loading = loadingModels,
                onLoadCandidates = {
                    scope.launch {
                        loadingModels = true
                        runCatching { store.loadModels(effectiveHarness, force = false) }
                        loadingModels = false
                    }
                },
                onRefreshCandidates = {
                    scope.launch {
                        loadingModels = true
                        runCatching { store.loadModels(effectiveHarness, force = true) }
                            .onFailure { err = errText(it, store.t("loadModelsFailed")) }
                        loadingModels = false
                    }
                },
                onValueChange = { model = it },
                store = store,
            )
        } else {
            OutlinedTextField(
                value = model,
                onValueChange = { model = it },
                placeholder = { Text(store.t("taskModelPh"), fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
        }

        // Permission Mode picker row
        Box {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .background(p.s1)
                    .clickable { permMenuOpen = true }
                    .padding(horizontal = 14.dp, vertical = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(store.t("permMode"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                Text(
                    text = permOptions.firstOrNull { it.first == permissionMode }?.second ?: permissionMode,
                    color = p.text2,
                    fontSize = PebbleFont.secondary,
                )
                Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
            }
            DropdownMenu(expanded = permMenuOpen, onDismissRequest = { permMenuOpen = false }, containerColor = p.card) {
                permOptions.forEach { (id, label) ->
                    DropdownMenuItem(
                        text = { Text(label, color = p.text, fontSize = PebbleFont.body) },
                        onClick = { permissionMode = id; permMenuOpen = false },
                    )
                }
            }
        }

        // Delivery picker
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(store.t("taskDeliveryLabel"), color = p.text2, fontSize = PebbleFont.secondary)
            val deliveries = listOf(
                "receipt" to store.t("taskDeliveryReceipt"),
                "wake" to store.t("taskDeliveryWake"),
                "silent" to store.t("taskDeliverySilent"),
            )
            SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                deliveries.forEachIndexed { idx, (key, lbl) ->
                    SegmentedButton(
                        selected = delivery == key,
                        onClick = { delivery = key },
                        shape = SegmentedButtonDefaults.itemShape(idx, deliveries.size),
                        label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                    )
                }
            }
        }

        Spacer(Modifier.height(16.dp))
    }
}
