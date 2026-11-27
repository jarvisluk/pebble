package ai.pebble.android.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.MemoryConfig
import ai.pebble.android.data.MemoryIndexDoc
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.MemoryStatusResp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.ui.nav.PebbleRouter
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch

@Composable
fun MemorySettingsTab(
    store: PebbleStore,
    router: PebbleRouter,
    onError: (String) -> Unit,
    onSaved: (String) -> Unit,
    onConfirm: (ConfirmItem) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    // Card 1: _shared/USER.md
    var userMd by remember { mutableStateOf("") }
    var userMdTouched by remember { mutableStateOf(false) }
    var userMdSaving by remember { mutableStateOf(false) }

    // Card 2: Global MEMORY.md
    var indexDoc by remember { mutableStateOf<MemoryIndexDoc?>(null) }
    var indexText by remember { mutableStateOf("") }
    var indexTouched by remember { mutableStateOf(false) }
    var indexSaving by remember { mutableStateOf(false) }

    // Card 3: Settings & Maintenance
    var memStatus by remember { mutableStateOf<MemoryStatusResp?>(null) }
    var cfgCoexistence by remember { mutableStateOf("auto") }
    var cfgDreamEnabled by remember { mutableStateOf(true) }
    var cfgDreamDailyAt by remember { mutableStateOf("03:00") }
    var cfgDreamModel by remember { mutableStateOf("") }
    var cfgEmbEnabled by remember { mutableStateOf(false) }
    var cfgEmbBaseUrl by remember { mutableStateOf("") }
    var cfgEmbModel by remember { mutableStateOf("") }
    var cfgEmbApiKey by remember { mutableStateOf("") }
    var cfgTouched by remember { mutableStateOf(false) }

    // Card 4: Memory Items Browser
    var mem by remember { mutableStateOf<MemoryList?>(null) }
    var memScope by remember { mutableStateOf("global") }
    var memStatusFilter by remember { mutableStateOf("active") }
    var memFilterKind by remember { mutableStateOf("") }
    var memQuery by remember { mutableStateOf("") }
    var memAdd by remember { mutableStateOf(false) }
    var addSeq by remember { mutableIntStateOf(0) }
    var editingId by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf("") }

    fun rtrim(x: String) = x.replace(Regex("\\s+$"), "")

    fun syncUserMd() {
        if (!userMdTouched) {
            userMd = store.settings?.sharedUserMd.orEmpty()
        }
    }

    fun syncConfig(cfg: MemoryConfig?) {
        if (!cfgTouched && cfg != null) {
            cfgCoexistence = cfg.coexistence?.takeIf { it == "auto" || it == "isolated" } ?: "auto"
            cfgDreamEnabled = cfg.dream?.enabled ?: true
            cfgDreamDailyAt = cfg.dream?.dailyAt.orEmpty().ifBlank { "03:00" }
            cfgDreamModel = cfg.dream?.model.orEmpty()
            cfgEmbEnabled = cfg.embedding?.enabled == true
            cfgEmbBaseUrl = cfg.embedding?.baseUrl.orEmpty()
            cfgEmbModel = cfg.embedding?.model.orEmpty()
            cfgEmbApiKey = ""
        }
    }

    fun currentBaseCfg(): MemoryConfig? = store.settings?.memory ?: memStatus?.config

    fun isUserMdDirty(): Boolean {
        val s = store.settings ?: return false
        return userMdTouched && rtrim(userMd) != rtrim(s.sharedUserMd.orEmpty())
    }

    fun isIndexDirty(): Boolean =
        indexTouched && rtrim(indexText) != rtrim(indexDoc?.content.orEmpty())

    fun isConfigDirty(): Boolean {
        val cfg = currentBaseCfg()
        return cfgTouched && (
            cfgCoexistence != (cfg?.coexistence?.takeIf { it == "auto" || it == "isolated" } ?: "auto") ||
                cfgDreamEnabled != (cfg?.dream?.enabled ?: true) ||
                cfgDreamDailyAt.trim() != (cfg?.dream?.dailyAt.orEmpty().ifBlank { "03:00" }) ||
                cfgDreamModel.trim() != cfg?.dream?.model.orEmpty().trim() ||
                cfgEmbEnabled != (cfg?.embedding?.enabled == true) ||
                cfgEmbBaseUrl.trim() != cfg?.embedding?.baseUrl.orEmpty().trim() ||
                cfgEmbModel.trim() != cfg?.embedding?.model.orEmpty().trim() ||
                cfgEmbApiKey.trim().isNotEmpty()
            )
    }

    val userMdDirty = isUserMdDirty()
    val indexDirty = isIndexDirty()
    val configDirty = isConfigDirty()

    suspend fun loadIndex() {
        runCatching { store.fetchMemoryIndex(scope = "global") }.getOrNull()?.let { doc ->
            indexDoc = doc
            if (!indexTouched) indexText = doc.content
        }
    }

    suspend fun loadStatus() {
        runCatching { store.fetchMemoryStatus() }.getOrNull()?.let { st ->
            memStatus = st
            syncConfig(store.settings?.memory ?: st.config)
        }
    }

    suspend fun loadMem(
        q: String = memQuery,
        scopeVal: String = memScope,
        kindVal: String = memFilterKind,
        statusVal: String = memStatusFilter,
    ) {
        try {
            mem = store.fetchMemories(scope = scopeVal, query = q, kind = kindVal, status = statusVal, limit = 50)
        } catch (e: Throwable) {
            onError(errText(e, store.t("opFailed")))
        }
    }

    suspend fun saveUserMd(): Boolean {
        onError("")
        onSaved("")
        userMdSaving = true
        return try {
            store.saveSharedUserMd(userMd)
            userMdTouched = false
            userMd = store.settings?.sharedUserMd.orEmpty()
            onSaved(store.t("settingsSaved"))
            true
        } catch (e: Throwable) {
            onError(errText(e, store.t("saveFailed")))
            false
        } finally {
            userMdSaving = false
        }
    }

    suspend fun saveIndex(): Boolean {
        onError("")
        onSaved("")
        indexSaving = true
        return try {
            val savedDoc = store.saveMemoryIndex(content = indexText, scope = "global")
            indexDoc = savedDoc.copy(content = indexText)
            indexTouched = false
            onSaved(store.t("memIndexSaved"))
            true
        } catch (e: Throwable) {
            onError(errText(e, store.t("saveFailed")))
            false
        } finally {
            indexSaving = false
        }
    }

    suspend fun saveConfig(): Boolean {
        onError("")
        onSaved("")
        busy = "mem:config"
        return try {
            val baseCfg = currentBaseCfg()
            val dreamMap = mutableMapOf<String, Any?>(
                "enabled" to cfgDreamEnabled,
                "dailyAt" to cfgDreamDailyAt.trim().ifEmpty { "03:00" },
                "model" to cfgDreamModel.trim(),
            )
            baseCfg?.dream?.minNewMessages?.let { dreamMap["minNewMessages"] = it }
            val embMap = mutableMapOf<String, Any?>(
                "enabled" to cfgEmbEnabled,
                "baseUrl" to cfgEmbBaseUrl.trim(),
                "model" to cfgEmbModel.trim(),
            )
            if (cfgEmbApiKey.trim().isNotEmpty()) embMap["apiKey"] = cfgEmbApiKey.trim()
            val payload = mutableMapOf<String, Any?>(
                "coexistence" to cfgCoexistence,
                "dream" to dreamMap,
                "embedding" to embMap,
            )
            baseCfg?.enabled?.let { payload["enabled"] = it }
            baseCfg?.snapshotMaxChars?.let { payload["snapshotMaxChars"] = it }
            baseCfg?.disableClaudeAutoMemory?.let { payload["disableClaudeAutoMemory"] = it }
            store.saveMemoryConfig(payload)
            cfgTouched = false
            cfgEmbApiKey = ""
            syncConfig(store.settings?.memory)
            loadStatus()
            onSaved(store.t("memConfigSaved"))
            true
        } catch (e: Throwable) {
            onError(errText(e, store.t("saveFailed")))
            false
        } finally {
            busy = ""
        }
    }

    fun runOp(key: String, failKey: String, op: suspend () -> String?) {
        onError("")
        onSaved("")
        busy = key
        scope.launch {
            try {
                op()?.let(onSaved)
            } catch (e: Throwable) {
                onError(errText(e, store.t(failKey)))
            } finally {
                busy = ""
            }
        }
    }

    LaunchedEffect(userMdDirty, indexDirty, configDirty) {
        router.settingsDirty = userMdDirty || indexDirty || configDirty
    }

    LaunchedEffect(store.settings?.sharedUserMd, store.settings?.memory) {
        syncUserMd()
        syncConfig(store.settings?.memory)
    }

    LaunchedEffect(store.isConnected) {
        if (store.isConnected) {
            runCatching { store.loadSettings() }
            syncUserMd()
            syncConfig(store.settings?.memory)
            launch { loadIndex() }
            launch { loadStatus() }
            launch { loadMem() }
        }
    }

    DisposableEffect(Unit) {
        router.settingsSaveAction = {
            var ok = true
            if (isUserMdDirty()) ok = ok && saveUserMd()
            if (isIndexDirty()) ok = ok && saveIndex()
            if (isConfigDirty()) ok = ok && saveConfig()
            ok
        }
        router.settingsDiscardAction = {
            userMdTouched = false
            userMd = store.settings?.sharedUserMd.orEmpty()
            indexTouched = false
            indexText = indexDoc?.content.orEmpty()
            cfgTouched = false
            syncConfig(currentBaseCfg())
        }
        onDispose {
            router.settingsDirty = false
            router.settingsSaveAction = null
            router.settingsDiscardAction = null
        }
    }

    // Card 1: _shared/USER.md
    SectionHeader(store.t("globalSharedMem"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            OutlinedTextField(
                value = userMd,
                onValueChange = { next ->
                    if (next != userMd) {
                        userMd = next
                        userMdTouched = true
                        onSaved("")
                    }
                },
                placeholder = { Text(store.t("userMdPh")) },
                minLines = 4,
                maxLines = 16,
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text("_shared/USER.md", color = p.mute, fontSize = PebbleFont.secondary, fontFamily = MonoFont, modifier = Modifier.weight(1f))
                Button(
                    onClick = { scope.launch { saveUserMd() } },
                    enabled = userMdDirty && !userMdSaving && busy.isEmpty(),
                    colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                ) {
                    if (userMdSaving) {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.onAccent)
                    } else {
                        Text(store.t("save"), fontSize = PebbleFont.secondary)
                    }
                }
            }
        }
    }

    // Card 2: Global MEMORY.md
    SectionHeader(store.t("memIndexTitle"))
    SectionCard {
        Column(modifier = Modifier.padding(14.dp)) {
            MemoryIndexCollapsible(
                store = store,
                title = store.t("memIndexTitle"),
                pathFallback = "_shared/memory/MEMORY.md",
                doc = indexDoc,
                content = indexText,
                dirty = indexDirty,
                saving = indexSaving,
                accent = p.accent,
                onAccent = p.onAccent,
                onContentChange = { indexText = it; indexTouched = true; onSaved("") },
                onSave = { scope.launch { saveIndex() } },
            )
        }
    }

    // Card 3: Pebble Memory Settings & Maintenance
    val liveBaseCfg = currentBaseCfg()
    val hasSavedEmbeddingApiKey = (liveBaseCfg?.embedding?.hasApiKey == true) || !liveBaseCfg?.embedding?.apiKey.isNullOrBlank()
    MemoryMaintenanceCard(
        store = store,
        status = memStatus,
        magpie = memStatus?.magpie ?: mem?.magpie,
        semantic = memStatus?.semantic ?: mem?.semantic,
        coexistence = cfgCoexistence,
        dreamEnabled = cfgDreamEnabled,
        dreamDailyAt = cfgDreamDailyAt,
        dreamModel = cfgDreamModel,
        embeddingEnabled = cfgEmbEnabled,
        embeddingBaseUrl = cfgEmbBaseUrl,
        embeddingModel = cfgEmbModel,
        embeddingApiKey = cfgEmbApiKey,
        hasSavedEmbeddingApiKey = hasSavedEmbeddingApiKey,
        configDirty = configDirty,
        busy = busy,
        onCoexistenceChange = { cfgCoexistence = it; cfgTouched = true; onSaved("") },
        onDreamEnabledChange = { cfgDreamEnabled = it; cfgTouched = true; onSaved("") },
        onDreamDailyAtChange = { cfgDreamDailyAt = it; cfgTouched = true; onSaved("") },
        onDreamModelChange = { cfgDreamModel = it; cfgTouched = true; onSaved("") },
        onEmbeddingEnabledChange = { cfgEmbEnabled = it; cfgTouched = true; onSaved("") },
        onEmbeddingBaseUrlChange = { cfgEmbBaseUrl = it; cfgTouched = true; onSaved("") },
        onEmbeddingModelChange = { cfgEmbModel = it; cfgTouched = true; onSaved("") },
        onEmbeddingApiKeyChange = { cfgEmbApiKey = it; cfgTouched = true; onSaved("") },
        onSaveConfig = { scope.launch { saveConfig() } },
        onRunDream = {
            runOp("mem:dream", "opFailed") {
                store.runMemoryDream()
                loadStatus()
                loadIndex()
                loadMem()
                store.t("memDreamDone")
            }
        },
        onReindex = {
            runOp("mem:reindex", "opFailed") {
                val r = store.reindexMemory()
                loadStatus()
                loadMem()
                store.t("memReindexDone", mapOf("docs" to r.docsIndexed, "chunks" to r.chunksIndexed))
            }
        },
        onImportMagpie = { mode ->
            runOp("mem:import:$mode", "opFailed") {
                val r = store.importMagpieMemory(mode = mode)
                loadStatus()
                loadMem()
                store.t("memImportDone", mapOf("imported" to r.imported, "skipped" to r.skipped))
            }
        },
    )

    // Card 4: Pebble Native Memory Items Browser
    MemoryBrowserCard(
        store = store,
        mem = mem,
        candidateCount = mem?.counts?.candidate ?: memStatus?.counts?.candidate ?: 0,
        memScope = memScope,
        memStatusFilter = memStatusFilter,
        memFilterKind = memFilterKind,
        memQuery = memQuery,
        memAdd = memAdd,
        addSeq = addSeq,
        editingId = editingId,
        busy = busy,
        onScopeChange = { sc -> memScope = sc; editingId = null; scope.launch { loadMem(scopeVal = sc) } },
        onStatusChange = { st -> memStatusFilter = st; editingId = null; scope.launch { loadMem(statusVal = st) } },
        onKindChange = { k -> memFilterKind = k; scope.launch { loadMem(kindVal = k) } },
        onQueryChange = { memQuery = it },
        onSearch = { q -> scope.launch { loadMem(q = q) } },
        onToggleAdd = { memAdd = !memAdd },
        onStartEdit = { editingId = it },
        onAddSubmit = { form ->
            if (form.text.isEmpty()) {
                onError(store.t("memContentReq"))
                return@MemoryBrowserCard
            }
            val payload = mutableMapOf<String, Any?>(
                "scope" to "global",
                "kind" to form.kind,
                "text" to form.text,
                "tags" to form.tags,
                "pin" to form.pin,
            )
            if (form.title.isNotEmpty()) payload["title"] = form.title
            if (form.why.isNotEmpty()) payload["why"] = form.why
            runOp("mem:save", "saveMemFailed") {
                store.saveMemory(payload)
                addSeq += 1
                memAdd = false
                loadMem()
                loadStatus()
                store.t("globalMemSaved")
            }
        },
        onEditSubmit = { item, form ->
            if (form.text.isEmpty()) {
                onError(store.t("memContentReq"))
                return@MemoryBrowserCard
            }
            runOp("mem:edit:${item.id}", "saveMemFailed") {
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
        onTogglePin = { item ->
            runOp("mem:pin:${item.id}", "updateFailed") {
                store.patchMemory(item.id, mapOf("pin" to (item.pin != true)))
                loadMem()
                store.t("memUpdated")
            }
        },
        onApprove = { item ->
            runOp("mem:approve:${item.id}", "updateFailed") {
                store.patchMemory(item.id, mapOf("status" to "active"))
                loadMem()
                loadStatus()
                store.t("memApproved")
            }
        },
        onDismiss = { item ->
            runOp("mem:dismiss:${item.id}", "delFailed") {
                store.deleteMemory(item.id, dismiss = true)
                loadMem()
                loadStatus()
                store.t("memDismissed")
            }
        },
        onArchive = { item ->
            runOp("mem:archive:${item.id}", "updateFailed") {
                store.deleteMemory(item.id)
                loadMem()
                loadStatus()
                store.t("memArchived")
            }
        },
        onRestore = { item ->
            runOp("mem:restore:${item.id}", "updateFailed") {
                store.patchMemory(item.id, mapOf("status" to "active"))
                loadMem()
                loadStatus()
                store.t("memUpdated")
            }
        },
        onDeleteConfirm = { item ->
            onConfirm(
                ConfirmItem(
                    title = store.t("deleteMemTitle"),
                    message = store.t("deleteMemConfirm"),
                ) {
                    runOp("mem:del:${item.id}", "delFailed") {
                        store.deleteMemory(item.id, hard = true)
                        loadMem()
                        loadStatus()
                        store.t("memDeleted")
                    }
                },
            )
        },
    )
}
