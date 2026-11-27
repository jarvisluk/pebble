package ai.pebble.android.ui.bots

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.MemoryIndexDoc
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.ui.settings.MemoryEditorForm
import ai.pebble.android.ui.settings.MemoryIndexCollapsible
import ai.pebble.android.ui.settings.MemoryItemCardRow
import ai.pebble.android.ui.settings.MemoryStatusFilterRow
import ai.pebble.android.ui.theme.BotAccent
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch

private val BOT_STATUS_FILTERS = listOf("active", "candidate", "all")

@Composable
internal fun BotMemorySection(
    store: PebbleStore,
    botId: String,
    pal: BotAccent,
    onError: (String) -> Unit,
    onConfirm: (ConfirmItem) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var indexDoc by remember(botId) { mutableStateOf<MemoryIndexDoc?>(null) }
    var indexText by remember(botId) { mutableStateOf("") }
    var indexTouched by remember(botId) { mutableStateOf(false) }
    var indexSaving by remember(botId) { mutableStateOf(false) }

    var mem by remember(botId) { mutableStateOf<MemoryList?>(null) }
    var memLoading by remember(botId) { mutableStateOf(false) }
    var statusFilter by remember(botId) { mutableStateOf("active") }
    var searchQuery by remember(botId) { mutableStateOf("") }
    var addOpen by remember(botId) { mutableStateOf(false) }
    var addSeq by remember(botId) { mutableIntStateOf(0) }
    var editingId by remember(botId) { mutableStateOf<String?>(null) }
    var busy by remember(botId) { mutableStateOf("") }
    var notice by remember(botId) { mutableStateOf("") }

    fun rtrim(x: String) = x.replace(Regex("\\s+$"), "")
    val indexDirty = indexTouched && rtrim(indexText) != rtrim(indexDoc?.content.orEmpty())

    suspend fun loadIndex() {
        runCatching { store.fetchMemoryIndex(botId = botId) }.getOrNull()?.let { doc ->
            indexDoc = doc
            if (!indexTouched) indexText = doc.content
        }
    }

    suspend fun loadMem(statusVal: String = statusFilter, q: String = searchQuery) {
        memLoading = true
        runCatching {
            store.fetchMemories(
                scope = "bot",
                botId = botId,
                query = q,
                status = statusVal,
                limit = 50,
            )
        }.onSuccess { mem = it }
            .onFailure { onError(errText(it, store.t("opFailed"))) }
        memLoading = false
    }

    fun runOp(key: String, failKey: String, op: suspend () -> String?) {
        onError("")
        notice = ""
        busy = key
        scope.launch {
            try {
                op()?.let { notice = it }
            } catch (e: Throwable) {
                onError(errText(e, store.t(failKey)))
            } finally {
                busy = ""
            }
        }
    }

    LaunchedEffect(botId) {
        launch { loadIndex() }
        launch { loadMem() }
    }

    SectionHeader(
        title = store.t("botMemory"),
        trailing = {
            TextButton(
                onClick = {
                    runOp("bot:dream", "opFailed") {
                        store.runMemoryDream(botId = botId)
                        loadIndex()
                        loadMem()
                        store.t("memDreamDone")
                    }
                },
                enabled = busy.isEmpty(),
                contentPadding = PaddingValues(horizontal = 8.dp, vertical = 2.dp),
            ) {
                if (busy == "bot:dream") {
                    CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp, color = pal.accent)
                    Spacer(Modifier.width(6.dp))
                    Text(store.t("memDreamRunning"), color = pal.accent, fontSize = PebbleFont.secondary)
                } else {
                    Text(store.t("memRunDream"), color = pal.accent, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                }
            }
        },
    )

    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (notice.isNotEmpty()) {
                StatusTextBanner(text = notice, error = false)
            }

            // Bot MEMORY.md collapsible viewer/editor
            MemoryIndexCollapsible(
                store = store,
                title = store.t("memBotIndexTitle"),
                pathFallback = "$botId/memory/MEMORY.md",
                doc = indexDoc,
                content = indexText,
                dirty = indexDirty,
                saving = indexSaving,
                accent = pal.accent,
                onAccent = pal.onAccent,
                onContentChange = {
                    indexText = it
                    indexTouched = true
                    notice = ""
                },
                onSave = {
                    onError("")
                    notice = ""
                    indexSaving = true
                    scope.launch {
                        try {
                            val savedDoc = store.saveMemoryIndex(content = indexText, botId = botId)
                            indexDoc = savedDoc.copy(content = indexText)
                            indexTouched = false
                            notice = store.t("memIndexSaved")
                        } catch (e: Throwable) {
                            onError(errText(e, store.t("saveFailed")))
                        } finally {
                            indexSaving = false
                        }
                    }
                },
            )

            CardDivider()

            // Status filter (Active / Candidate / All)
            MemoryStatusFilterRow(
                store = store,
                statuses = BOT_STATUS_FILTERS,
                selected = statusFilter,
                candidateCount = mem?.counts?.candidate ?: 0,
                accent = pal.accent,
                onAccent = pal.onAccent,
                onSelect = { next ->
                    statusFilter = next
                    editingId = null
                    scope.launch { loadMem(statusVal = next) }
                },
            )

            // Search input
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                OutlinedTextField(
                    value = searchQuery,
                    onValueChange = { searchQuery = it },
                    placeholder = { Text(store.t("memBotSearchPh"), fontSize = PebbleFont.secondary) },
                    singleLine = true,
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.weight(1f),
                )
                if (searchQuery.isNotEmpty()) {
                    IconButton(onClick = { searchQuery = ""; scope.launch { loadMem(q = "") } }) {
                        Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("clear"), tint = p.mute)
                    }
                }
                IconButton(onClick = { scope.launch { loadMem(q = searchQuery) } }) {
                    Icon(painterResource(PebbleIcons.Search), contentDescription = store.t("search"), tint = pal.accent)
                }
            }

            // Items list
            val m = mem
            when {
                memLoading && m == null -> {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = pal.accent)
                }
                m != null && m.available == false -> {
                    val detailErr = m.error?.takeIf { it.isNotEmpty() }?.let { ": $it" }.orEmpty()
                    Text(store.t("memUnavailable") + detailErr, color = p.danger, fontSize = PebbleFont.secondary)
                }
                else -> {
                    val items = m?.items.orEmpty()
                    if (items.isEmpty()) {
                        val emptyKey = if (searchQuery.isNotBlank() || statusFilter != "active") {
                            "memNoMatching"
                        } else {
                            "noBotMemories"
                        }
                        Text(store.t(emptyKey), color = p.mute, fontSize = PebbleFont.secondary)
                    } else {
                        items.forEachIndexed { idx, item ->
                            if (idx > 0) CardDivider()
                            if (editingId == item.id) {
                                MemoryEditorForm(
                                    store = store,
                                    textPlaceholder = store.t("botMemPh"),
                                    accent = pal.accent,
                                    onAccent = pal.onAccent,
                                    saving = busy == "bot:edit:${item.id}",
                                    initial = item,
                                    onCancel = { editingId = null },
                                    onSubmit = { form ->
                                        if (form.text.isEmpty()) {
                                            onError(store.t("memContentReq"))
                                            return@MemoryEditorForm
                                        }
                                        runOp("bot:edit:${item.id}", "saveBotMemFailed") {
                                            store.patchMemory(
                                                item.id,
                                                mapOf(
                                                    "title" to form.title,
                                                    "kind" to form.kind,
                                                    "text" to form.text,
                                                    "tags" to form.tags,
                                                    "why" to form.why,
                                                    "pin" to form.pin,
                                                ),
                                            )
                                            editingId = null
                                            loadMem()
                                            store.t("memUpdated")
                                        }
                                    },
                                )
                            } else {
                                MemoryItemCardRow(
                                    store = store,
                                    item = item,
                                    accent = pal.accent,
                                    showTags = true,
                                    showScope = false,
                                    busy = busy.isNotEmpty(),
                                    onTogglePin = {
                                        runOp("bot:pin:${item.id}", "updateFailed") {
                                            store.patchMemory(item.id, mapOf("pin" to (item.pin != true)))
                                            loadMem()
                                            store.t("memUpdated")
                                        }
                                    },
                                    onEdit = { editingId = item.id },
                                    onApprove = {
                                        runOp("bot:approve:${item.id}", "updateFailed") {
                                            store.patchMemory(item.id, mapOf("status" to "active"))
                                            loadMem()
                                            store.t("memApproved")
                                        }
                                    },
                                    onDismiss = {
                                        runOp("bot:dismiss:${item.id}", "delFailed") {
                                            store.deleteMemory(item.id, dismiss = true)
                                            loadMem()
                                            store.t("memDismissed")
                                        }
                                    },
                                    onArchive = {
                                        runOp("bot:archive:${item.id}", "updateFailed") {
                                            store.deleteMemory(item.id)
                                            loadMem()
                                            store.t("memArchived")
                                        }
                                    },
                                    onRestore = {
                                        runOp("bot:restore:${item.id}", "updateFailed") {
                                            store.patchMemory(item.id, mapOf("status" to "active"))
                                            loadMem()
                                            store.t("memUpdated")
                                        }
                                    },
                                    onDelete = {
                                        onConfirm(
                                            ConfirmItem(
                                                title = store.t("deleteMemTitle"),
                                                message = store.t("deleteMemConfirm"),
                                            ) {
                                                runOp("bot:del:${item.id}", "delFailed") {
                                                    store.deleteMemory(item.id, hard = true)
                                                    loadMem()
                                                    store.t("memDeleted")
                                                }
                                            },
                                        )
                                    },
                                )
                            }
                        }
                    }
                }
            }

            CardDivider()

            // Expandable + Add form (supports title, kind, text, pin, daily)
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable { addOpen = !addOpen }
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = store.t("addShort"),
                    color = pal.accent,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier.weight(1f),
                )
                Icon(
                    painter = painterResource(if (addOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                    contentDescription = null,
                    tint = p.mute,
                )
            }

            AnimatedVisibility(visible = addOpen) {
                key(addSeq) {
                    MemoryEditorForm(
                        store = store,
                        textPlaceholder = store.t("botMemPh"),
                        accent = pal.accent,
                        onAccent = pal.onAccent,
                        saving = busy == "bot:save",
                        allowDaily = true,
                        onSubmit = { form ->
                            if (form.text.isEmpty()) {
                                onError(store.t("memContentReq"))
                                return@MemoryEditorForm
                            }
                            val payload = mutableMapOf<String, Any?>(
                                "scope" to "bot",
                                "botId" to botId,
                                "text" to form.text,
                            )
                            if (form.daily) {
                                payload["daily"] = true
                            } else {
                                payload["kind"] = form.kind
                                payload["pin"] = form.pin
                                if (form.title.isNotEmpty()) payload["title"] = form.title
                                if (form.tags.isNotEmpty()) payload["tags"] = form.tags
                                if (form.why.isNotEmpty()) payload["why"] = form.why
                            }
                            runOp("bot:save", "saveBotMemFailed") {
                                store.saveMemory(payload)
                                addSeq += 1
                                addOpen = false
                                loadMem()
                                if (form.daily) store.t("memDailySaved") else store.t("opSuccess")
                            }
                        },
                    )
                }
            }
        }
    }
}
