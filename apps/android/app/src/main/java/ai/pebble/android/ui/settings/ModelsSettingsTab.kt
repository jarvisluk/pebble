package ai.pebble.android.ui.settings

import android.content.Intent
import android.net.Uri
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.HarnessInfo
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.SetupTaskStatus
import ai.pebble.android.data.errText
import ai.pebble.android.data.resolveDefaultModel
import ai.pebble.android.ui.nav.PebbleRouter
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionFooter
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.copyToClipboard
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private data class BotModelDraft(val harness: String, val model: String)

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ModelsSettingsTab(
    store: PebbleStore,
    router: PebbleRouter,
    onOpenCreateBot: () -> Unit,
    onError: (String) -> Unit,
    onSaved: (String) -> Unit,
    onConfirm: (ConfirmItem) -> Unit,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    val harnesses = settingsHarnessList(store)
    val primaryHarness = harnesses.firstOrNull()?.id ?: "opencode"
    val baseDefaultHarness = store.settings?.defaultHarness?.takeIf { it.isNotEmpty() } ?: primaryHarness
    val baseDefaultPermMode = store.settings?.defaultPermissionMode?.takeIf { it.isNotEmpty() } ?: "auto"

    var defaultHarness by remember { mutableStateOf(baseDefaultHarness) }
    var defaultHarnessTouched by remember { mutableStateOf(false) }
    var defaultPermMode by remember { mutableStateOf(baseDefaultPermMode) }
    var defaultPermModeTouched by remember { mutableStateOf(false) }
    var defaultModel by remember { mutableStateOf("") }
    var defaultTouched by remember { mutableStateOf(false) }
    val perHarness = remember { mutableStateMapOf<String, String>() }
    var perTouched by remember { mutableStateOf(false) }
    var mpWebUrl by remember { mutableStateOf("") }
    var mpWebUrlTouched by remember { mutableStateOf(false) }
    val drafts = remember { mutableStateMapOf<String, BotModelDraft>() }
    var touchedBots by remember { mutableStateOf(setOf<String>()) }
    var loadingModelsFor by remember { mutableStateOf(setOf<String>()) }

    var moreEnginesOpen by remember { mutableStateOf(false) }
    var expandedInstall by remember { mutableStateOf(setOf<String>()) }
    val installTasks = remember { mutableStateMapOf<String, SetupTaskStatus>() }
    val loginTasks = remember { mutableStateMapOf<String, SetupTaskStatus>() }
    val pollJobs = remember { mutableMapOf<String, Job>() }
    var copiedKey by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf("") }

    fun cleanMap(m: Map<String, String>?): Map<String, String> =
        m.orEmpty().mapNotNull { (k, v) ->
            val t = v.trim()
            if (t.isEmpty()) null else k to t
        }.toMap()

    fun origModel(b: BotStatus): String =
        (if (b.modelInherited == true) "" else b.model.orEmpty()).trim()

    fun syncDrafts() {
        val validIds = store.bots.map { it.id }.toSet()
        drafts.keys.toList().filterNot { it in validIds }.forEach { drafts.remove(it) }
        for (b in store.bots) {
            if (b.id !in touchedBots) {
                drafts[b.id] = BotModelDraft(b.harness.orEmpty(), origModel(b))
            }
        }
    }

    fun sync() {
        val s = store.settings ?: return
        if (!defaultHarnessTouched) defaultHarness = baseDefaultHarness
        if (!defaultPermModeTouched) defaultPermMode = baseDefaultPermMode
        if (!defaultTouched) defaultModel = s.defaultModel.orEmpty().trim()
        if (!perTouched) {
            perHarness.clear()
            perHarness.putAll(cleanMap(s.defaultModels))
        }
        if (!mpWebUrlTouched) mpWebUrl = s.magpie?.webUrl.orEmpty().trim()
        syncDrafts()
    }

    val botUpdates = store.bots.mapNotNull { b ->
        if (b.id !in touchedBots) return@mapNotNull null
        val d = drafts[b.id] ?: return@mapNotNull null
        val m = d.model.trim()
        if (d.harness != b.harness.orEmpty() || m != origModel(b)) Triple(b.id, d.harness, m) else null
    }

    val settingsChanged = run {
        val s = store.settings ?: return@run false
        val webDirty = mpWebUrlTouched && mpWebUrl.trim() != s.magpie?.webUrl.orEmpty().trim()
        (defaultHarnessTouched && defaultHarness != baseDefaultHarness) ||
            (defaultPermModeTouched && defaultPermMode != baseDefaultPermMode) ||
            (defaultTouched && defaultModel.trim() != s.defaultModel.orEmpty().trim()) ||
            (perTouched && cleanMap(perHarness) != cleanMap(s.defaultModels)) ||
            webDirty
    }
    val dirty = settingsChanged || botUpdates.isNotEmpty()

    fun loadCands(harnessId: String, force: Boolean = false) {
        if (harnessId.isEmpty()) return
        if (!force && !store.models[harnessId].isNullOrEmpty()) return
        if (!force && harnessId in loadingModelsFor) return
        loadingModelsFor = loadingModelsFor + harnessId
        scope.launch {
            try {
                store.loadModels(harnessId, force = force)
            } catch (e: Throwable) {
                if (force) onError(errText(e, store.t("loadModelsFailed")))
            } finally {
                loadingModelsFor = loadingModelsFor - harnessId
            }
        }
    }

    fun preloadAllModels(force: Boolean = false) {
        harnesses.filter { it.installed != false }.forEach { h -> loadCands(h.id, force = force) }
    }

    suspend fun saveAll(): Boolean {
        onError("")
        onSaved("")
        return try {
            store.saveModelsSettings(
                changed = settingsChanged,
                defaultHarness = defaultHarness,
                defaultPermissionMode = defaultPermMode,
                defaultModel = defaultModel.trim(),
                defaultModels = cleanMap(perHarness),
                magpieWebUrl = if (mpWebUrlTouched) mpWebUrl.trim() else null,
                botUpdates = botUpdates,
            )
            defaultHarnessTouched = false
            defaultPermModeTouched = false
            defaultTouched = false
            perTouched = false
            mpWebUrlTouched = false
            touchedBots = emptySet()
            drafts.clear()
            sync()
            router.settingsDirty = false
            onSaved(store.t("settingsSaved"))
            true
        } catch (e: Throwable) {
            onError(errText(e, store.t("saveFailed")))
            false
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

    fun startHarnessLogin(id: String) {
        onError("")
        onSaved("")
        pollJobs["login:$id"]?.cancel()
        pollJobs["login:$id"] = scope.launch {
            try {
                var st = store.startHarnessLogin(id)
                loginTasks[id] = st
                st.loginUrl?.takeIf { it.isNotEmpty() }?.let { url ->
                    runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) }
                }
                while (st.state == "running" && st.authStatus != "ready") {
                    delay(1400)
                    st = store.getHarnessLoginStatus(id)
                    loginTasks[id] = st
                }
                if (st.state == "done" || st.authStatus == "ready") {
                    store.resetModelsCache()
                    runCatching { store.loadSettings(fresh = true) }
                    preloadAllModels(force = true)
                    onSaved(store.t("engineLoginConfirmed"))
                } else if (st.state == "error" && !st.error.isNullOrEmpty()) {
                    onError(st.error)
                }
            } catch (e: Throwable) {
                if (e !is CancellationException) {
                    onError(errText(e, store.t("opFailed")))
                }
            }
        }
    }

    fun startHarnessInstall(id: String) {
        onError("")
        onSaved("")
        pollJobs["install:$id"]?.cancel()
        pollJobs["install:$id"] = scope.launch {
            try {
                var st = store.startHarnessInstall(id)
                installTasks[id] = st
                while (st.state == "running") {
                    delay(1200)
                    st = store.getHarnessInstallStatus(id)
                    installTasks[id] = st
                }
                if (st.state == "done") {
                    store.resetModelsCache()
                    val fresh = store.loadSettings(fresh = true)
                    preloadAllModels(force = true)
                    val h = fresh.harnesses?.firstOrNull { it.id == id }
                    onSaved(store.t("engineInstallDone", mapOf("label" to (h?.title ?: id))))
                    if (h?.requiresLogin == true && h.authStatus == "needs_login") {
                        startHarnessLogin(id)
                    }
                } else if (st.state == "error") {
                    expandedInstall = expandedInstall + id
                }
            } catch (e: Throwable) {
                if (e !is CancellationException) {
                    expandedInstall = expandedInstall + id
                    onError(errText(e, store.t("opFailed")))
                }
            }
        }
    }

    LaunchedEffect(dirty) {
        router.settingsDirty = dirty
    }

    LaunchedEffect(store.settings, store.bots) {
        sync()
    }

    LaunchedEffect(store.isConnected) {
        if (store.isConnected) {
            runCatching { store.loadSettings() }
            runCatching { store.refreshBots() }
            sync()
            preloadAllModels()
        }
    }

    LaunchedEffect(store.modelsGeneration, harnesses.map { it.id }) {
        if (store.isConnected) {
            preloadAllModels()
        }
    }

    DisposableEffect(Unit) {
        router.settingsSaveAction = { saveAll() }
        router.settingsDiscardAction = {
            defaultHarnessTouched = false
            defaultPermModeTouched = false
            defaultTouched = false
            perTouched = false
            mpWebUrlTouched = false
            touchedBots = emptySet()
            drafts.clear()
            sync()
        }
        onDispose {
            pollJobs.values.forEach { it.cancel() }
            pollJobs.clear()
            router.settingsDirty = false
            router.settingsSaveAction = null
            router.settingsDiscardAction = null
        }
    }

    val mp = store.settings?.magpie
    val mpInstalled = mp?.installed == true
    val mpHealthy = mp?.healthy == true
    val installedHarnesses = harnesses.filter { it.installed == true }
    val uninstalledHarnesses = harnesses.filter { it.installed != true }

    // Card 3: Defaults & Bot Overrides (without duplicate perOpen disclosure!)
    val orderedGlobal = orderedGlobalHarnesses(harnesses, defaultHarness, store.models)
    SectionHeader(store.t("globalAndBotModels"))
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            var defHarnessOpen by remember { mutableStateOf(false) }
            Box {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable { defHarnessOpen = true }
                        .padding(vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(store.t("defaultHarness"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                    Text(harnesses.firstOrNull { it.id == defaultHarness }?.title ?: defaultHarness, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                    Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                }
                DropdownMenu(expanded = defHarnessOpen, onDismissRequest = { defHarnessOpen = false }, containerColor = p.card) {
                    harnesses.forEach { h ->
                        DropdownMenuItem(
                            text = { Text(h.title, color = p.text, fontSize = PebbleFont.body) },
                            onClick = {
                                if (h.id != defaultHarness) {
                                    defaultHarness = h.id
                                    defaultHarnessTouched = true
                                    onSaved("")
                                    if (store.models[h.id].isNullOrEmpty() && store.isConnected) loadCands(h.id)
                                }
                                defHarnessOpen = false
                            },
                        )
                    }
                }
            }

            var defPermOpen by remember { mutableStateOf(false) }
            val permChoices = listOf("auto", "edit", "ask", "plan")
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Box {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { defPermOpen = true }
                            .padding(vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(store.t("permDefaultMode"), color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                        Text(store.t("permMode_$defaultPermMode"), color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                        Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                    }
                    DropdownMenu(expanded = defPermOpen, onDismissRequest = { defPermOpen = false }, containerColor = p.card) {
                        permChoices.forEach { m ->
                            DropdownMenuItem(
                                text = { Text(store.t("permMode_$m"), color = p.text, fontSize = PebbleFont.body) },
                                onClick = {
                                    if (m != defaultPermMode) {
                                        defaultPermMode = m
                                        defaultPermModeTouched = true
                                        onSaved("")
                                    }
                                    defPermOpen = false
                                },
                            )
                        }
                    }
                }
                Text(store.t("permDefaultModeDesc"), color = p.mute, fontSize = PebbleFont.caption)
            }

            GlobalModelInputWithMenu(
                value = defaultModel,
                placeholder = "magpie/claude-sonnet-4",
                harnesses = orderedGlobal,
                defaultHarness = defaultHarness,
                harnessLabel = { harnessDisplayLabel(it, store) },
                modelsByHarness = store.models,
                loadingHarnesses = loadingModelsFor,
                onLoadHarness = { loadCands(it) },
                onRefreshHarness = { loadCands(it, force = true) },
                onRefreshAll = { preloadAllModels(force = true) },
                onValueChange = { next ->
                    if (next != defaultModel) {
                        defaultModel = next
                        defaultTouched = true
                        onSaved("")
                    }
                },
                store = store,
            )

            if (installedHarnesses.isNotEmpty()) {
                var perOverridesOpen by remember { mutableStateOf(false) }
                val overrideCount = installedHarnesses.count { !perHarness[it.id].isNullOrBlank() }
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable { perOverridesOpen = !perOverridesOpen }
                        .padding(vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = if (perOverridesOpen) {
                            store.t("collapse")
                        } else if (overrideCount > 0) {
                            store.t("perHarnessOverridesCount", mapOf("n" to overrideCount))
                        } else {
                            store.t("perHarnessOverrides")
                        },
                        color = p.accent,
                        fontSize = PebbleFont.secondary,
                        fontWeight = FontWeight.Medium,
                        modifier = Modifier.weight(1f),
                    )
                    Icon(painterResource(if (perOverridesOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
                }
                AnimatedVisibility(visible = perOverridesOpen) {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        installedHarnesses.forEach { h ->
                            val enginePh = defaultModel.trim().takeIf { it.isNotEmpty() }?.let { resolveDefaultModel(it, h.id) }.orEmpty()
                            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                Text(h.title, color = p.text2, fontSize = PebbleFont.caption, fontWeight = FontWeight.Medium)
                                ModelInputWithMenu(
                                    value = perHarness[h.id].orEmpty(),
                                    placeholder = enginePh.ifEmpty { store.t("engineModelPh") },
                                    candidates = store.models[h.id].orEmpty(),
                                    loading = h.id in loadingModelsFor,
                                    onLoadCandidates = { loadCands(h.id) },
                                    onRefreshCandidates = { loadCands(h.id, force = true) },
                                    onValueChange = { next ->
                                        if (next != perHarness[h.id].orEmpty()) {
                                            perHarness[h.id] = next
                                            perTouched = true
                                            onSaved("")
                                        }
                                    },
                                    store = store,
                                )
                            }
                        }
                    }
                }
            }

            CardDivider()
            if (store.bots.isEmpty()) {
                Text(store.t("noBotsInSettings"), color = p.mute, fontSize = PebbleFont.body)
            } else {
                store.bots.forEachIndexed { idx, b ->
                    if (idx > 0) CardDivider()
                    val d = drafts[b.id] ?: BotModelDraft(b.harness.orEmpty(), origModel(b))
                    val per = perHarness[d.harness].orEmpty().trim()
                    val ph = per.ifEmpty { resolveDefaultModel(defaultModel.trim(), d.harness) }
                    var botHarnessMenu by remember { mutableStateOf(false) }

                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            BotAvatar(id = b.id, size = 28.dp, index = idx, avatar = b.avatar, color = b.color)
                            Column(modifier = Modifier.weight(1f)) {
                                Text(b.displayName, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold)
                                Text("@${b.id}", color = p.text2, fontSize = PebbleFont.caption, fontFamily = MonoFont)
                            }
                            Box {
                                TextButton(onClick = { botHarnessMenu = true }) {
                                    Text(harnesses.firstOrNull { it.id == d.harness }?.title ?: d.harness.ifEmpty { "-" }, color = p.accent, fontSize = PebbleFont.secondary)
                                    Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                }
                                DropdownMenu(expanded = botHarnessMenu, onDismissRequest = { botHarnessMenu = false }, containerColor = p.card) {
                                    harnesses.forEach { h ->
                                        DropdownMenuItem(
                                            text = { Text(h.title, color = p.text, fontSize = PebbleFont.body) },
                                            onClick = {
                                                if (h.id != d.harness) {
                                                    drafts[b.id] = BotModelDraft(harness = h.id, model = "")
                                                    touchedBots = touchedBots + b.id
                                                    onSaved("")
                                                    if (store.models[h.id].isNullOrEmpty() && store.isConnected) loadCands(h.id)
                                                }
                                                botHarnessMenu = false
                                            },
                                        )
                                    }
                                }
                            }
                        }
                        ModelInputWithMenu(
                            value = d.model,
                            placeholder = ph.ifEmpty { store.t("inheritDefaultPh") },
                            candidates = store.models[d.harness].orEmpty(),
                            loading = d.harness in loadingModelsFor,
                            onLoadCandidates = { loadCands(d.harness) },
                            onRefreshCandidates = { loadCands(d.harness, force = true) },
                            onValueChange = { next ->
                                if (next != d.model) {
                                    drafts[b.id] = d.copy(model = next)
                                    touchedBots = touchedBots + b.id
                                    onSaved("")
                                }
                            },
                            store = store,
                        )
                    }
                }
            }

            CardDivider()
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(PebbleDimens.rowShape)
                    .clickable(onClick = onOpenCreateBot)
                    .padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Icon(painterResource(PebbleIcons.Add), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                Text(store.t("newBot"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
            }
        }
    }

    // Card 2: Execution Engines
    SectionHeader(
        title = store.t("enginesTitle"),
        trailing = {
            if (busy == "recheck") {
                CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.accent)
            } else {
                TextButton(
                    onClick = {
                        runOp("recheck", "opFailed") {
                            store.loadSettings(fresh = true)
                            store.resetModelsCache()
                            preloadAllModels(force = true)
                            store.t("ubRecheckDone")
                        }
                    },
                    contentPadding = PaddingValues(horizontal = 8.dp, vertical = 2.dp),
                ) {
                    Text(store.t("enginesRecheck"), color = p.accent, fontSize = PebbleFont.secondary)
                }
            }
        },
    )
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (installedHarnesses.isEmpty()) {
                Text(store.t("engineNoInstalled"), color = p.mute, fontSize = PebbleFont.secondary)
            } else {
                val connectedHarnesses = installedHarnesses.filter { it.magpie?.connected == true }
                val nativeHarnesses = installedHarnesses.filter { it.magpie?.connected != true }
                val groups = buildList {
                    if (connectedHarnesses.isNotEmpty()) {
                        add(Triple("${store.t("harnessGroupConnected")} (${connectedHarnesses.size})", true, connectedHarnesses))
                    }
                    if (nativeHarnesses.isNotEmpty()) {
                        add(Triple("${store.t("harnessGroupNative")} (${nativeHarnesses.size})", false, nativeHarnesses))
                    }
                }
                groups.forEachIndexed { gIdx, (groupTitle, isConnectedGroup, list) ->
                    if (gIdx > 0) CardDivider()
                    Text(
                        text = groupTitle,
                        color = if (isConnectedGroup) p.sageInk else p.mute,
                        fontSize = PebbleFont.caption,
                        fontWeight = FontWeight.SemiBold,
                    )
                    list.forEachIndexed { idx, h ->
                        if (idx > 0) CardDivider()
                        val mpConnected = h.magpie?.connected == true
                        val loginTask = loginTasks[h.id]
                        val loggingIn = loginTask?.state == "running"
                        val needsLogin = h.requiresLogin == true && h.authStatus != "ready"
                        val authReady = h.authStatus == "ready"

                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                Box(
                                    modifier = Modifier
                                        .size(7.dp)
                                        .clip(CircleShape)
                                        .background(if (mpConnected) p.sageInk else if (needsLogin) p.danger else p.mute.copy(alpha = 0.45f)),
                                )
                                Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                                        Text(h.title, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                        if (needsLogin) {
                                            TagPill(store.t("engineNeedsLogin"), p.pink, p.pinkInk)
                                        }
                                    }
                                    val subText = when {
                                        mpConnected -> h.magpie?.model?.takeIf { it.isNotEmpty() } ?: store.t("mpConnected")
                                        !h.magpie?.nativeModel.isNullOrEmpty() -> h.magpie?.nativeModel.orEmpty()
                                        !h.summary.isNullOrEmpty() -> h.summary.orEmpty()
                                        else -> if (authReady) store.t("engineReady") else h.id
                                    }
                                    Text(
                                        text = subText,
                                        color = if (mpConnected) p.sageInk else p.text2,
                                        fontSize = PebbleFont.caption,
                                        fontFamily = if (mpConnected || !h.magpie?.nativeModel.isNullOrEmpty()) MonoFont else FontFamily.Default,
                                        maxLines = 1,
                                        overflow = TextOverflow.Ellipsis,
                                    )
                                }
                                if (needsLogin) {
                                    Button(
                                        onClick = { startHarnessLogin(h.id) },
                                        enabled = !loggingIn,
                                        colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                    ) {
                                        if (loggingIn) {
                                            CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp, color = p.onAccent)
                                        } else {
                                            Text(store.t("engineLoginBtn"), fontSize = PebbleFont.secondary)
                                        }
                                    }
                                }
                            }

                            if (loggingIn) {
                                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    if (!loginTask?.loginUrl.isNullOrEmpty()) {
                                        OutlinedButton(
                                            onClick = {
                                                runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(loginTask?.loginUrl))) }
                                            },
                                            contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                        ) {
                                            Text(store.t("engineOpenLogin"), fontSize = PebbleFont.secondary, color = p.text)
                                        }
                                    }
                                    OutlinedButton(
                                        onClick = {
                                            scope.launch {
                                                runCatching { store.getHarnessLoginStatus(h.id) }.getOrNull()?.let { st ->
                                                    loginTasks[h.id] = st
                                                    if (st.state == "done" || st.authStatus == "ready") {
                                                        store.resetModelsCache()
                                                        runCatching { store.loadSettings(fresh = true) }
                                                        preloadAllModels(force = true)
                                                        onSaved(store.t("engineLoginConfirmed"))
                                                    }
                                                }
                                            }
                                        },
                                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                    ) {
                                        Text(store.t("engineCheckLogin"), fontSize = PebbleFont.secondary, color = p.text)
                                    }
                                }
                            }

                            val detailText = loginTask?.authDetail ?: h.authDetail
                            if (!detailText.isNullOrEmpty() && (needsLogin || loginTask?.state == "error")) {
                                Text(detailText, color = if (loginTask?.state == "error") p.danger else p.text2, fontSize = PebbleFont.caption)
                            }
                            if (loginTask?.state == "error" && !loginTask.error.isNullOrEmpty()) {
                                Text(loginTask.error, color = p.danger, fontSize = PebbleFont.caption)
                            }
                        }
                    }
                }
            }

            if (uninstalledHarnesses.isNotEmpty()) {
                if (installedHarnesses.isNotEmpty()) {
                    CardDivider()
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { moreEnginesOpen = !moreEnginesOpen }
                            .padding(vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = if (moreEnginesOpen) store.t("collapse") else store.t("enginesMoreAvailable", mapOf("n" to uninstalledHarnesses.size)),
                            color = p.accent,
                            fontSize = PebbleFont.secondary,
                            fontWeight = FontWeight.Medium,
                            modifier = Modifier.weight(1f),
                        )
                        Icon(painterResource(if (moreEnginesOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
                    }
                }
                AnimatedVisibility(visible = installedHarnesses.isEmpty() || moreEnginesOpen) {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        uninstalledHarnesses.forEachIndexed { idx, h ->
                            if (idx > 0) CardDivider()
                            val task = installTasks[h.id]
                            val installing = task?.state == "running"
                            val cmd = h.install?.command.orEmpty().trim()
                            val urlStr = h.install?.url.orEmpty().trim()
                            val isExpanded = h.id in expandedInstall || task?.state == "error"

                            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                                ) {
                                    Column(modifier = Modifier.weight(1f)) {
                                        Text(h.title, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold)
                                        h.summary?.takeIf { it.isNotEmpty() }?.let {
                                            Text(it, color = p.text2, fontSize = PebbleFont.caption)
                                        }
                                    }
                                    if (h.canAutoInstall == true) {
                                        Button(
                                            onClick = { startHarnessInstall(h.id) },
                                            enabled = !installing,
                                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                        ) {
                                            if (installing) {
                                                CircularProgressIndicator(modifier = Modifier.size(14.dp), strokeWidth = 2.dp, color = p.onAccent)
                                                Spacer(Modifier.width(6.dp))
                                                Text(store.t("engineInstalling"), fontSize = PebbleFont.secondary)
                                            } else {
                                                Text(store.t("engineInstallBtn"), fontSize = PebbleFont.secondary)
                                            }
                                        }
                                    } else {
                                        OutlinedButton(
                                            onClick = {
                                                expandedInstall = if (h.id in expandedInstall) expandedInstall - h.id else expandedInstall + h.id
                                            },
                                            contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                        ) {
                                            Text(store.t(if (isExpanded) "collapse" else "engineManualInstall"), fontSize = PebbleFont.secondary, color = p.text)
                                        }
                                    }
                                }

                                if (isExpanded) {
                                    if (task?.state == "error" && !task.error.isNullOrEmpty()) {
                                        Text(task.error, color = p.danger, fontSize = PebbleFont.caption)
                                    }
                                    val shownCmd = cmd.ifEmpty { store.t("engineManualHint", mapOf("name" to h.title)) }
                                    Row(
                                        modifier = Modifier.fillMaxWidth(),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        Text(shownCmd, color = p.text2, fontSize = PebbleFont.caption, fontFamily = MonoFont, modifier = Modifier.weight(1f))
                                        if (cmd.isNotEmpty()) {
                                            OutlinedButton(
                                                onClick = {
                                                    copyToClipboard(context, cmd)
                                                    copiedKey = "eng-inst-${h.id}"
                                                    scope.launch {
                                                        delay(1200)
                                                        if (copiedKey == "eng-inst-${h.id}") copiedKey = ""
                                                    }
                                                },
                                                contentPadding = PaddingValues(horizontal = 10.dp, vertical = 2.dp),
                                            ) {
                                                Text(
                                                    text = store.t(if (copiedKey == "eng-inst-${h.id}") "copied" else "copy"),
                                                    fontSize = PebbleFont.caption,
                                                    color = p.text,
                                                )
                                            }
                                        }
                                    }
                                    if (urlStr.isNotEmpty()) {
                                        TextButton(
                                            onClick = {
                                                runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(urlStr))) }
                                            },
                                            contentPadding = PaddingValues(0.dp),
                                        ) {
                                            Text(store.t("engineViewDocs"), color = p.accent, fontSize = PebbleFont.secondary)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    SectionFooter(store.t("enginesIntro"))

    // Card 1: Magpie Gateway
    SectionHeader(
        title = store.t("magpieGatewayTitle"),
        trailing = {
            TagPill(
                text = when {
                    !mpInstalled -> store.t("notInstalled")
                    mpHealthy -> store.t("gatewayOk")
                    else -> store.t("gatewayDown")
                },
                bg = if (!mpInstalled) p.s1 else if (mpHealthy) p.sage else p.pink,
                fg = if (!mpInstalled) p.text2 else if (mpHealthy) p.sageInk else p.danger,
            )
        },
    )
    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (!mpInstalled) {
                Text(store.t("hostNoMagpieGlobal"), color = p.text2, fontSize = PebbleFont.body)
                OutlinedButton(
                    onClick = {
                        runOp("magpie:install", "opFailed") {
                            store.installMagpie()
                            preloadAllModels(force = true)
                            store.t("mpInstallDone")
                        }
                    },
                    enabled = busy.isEmpty(),
                ) {
                    if (busy == "magpie:install") {
                        CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.accent)
                        Spacer(Modifier.width(8.dp))
                        Text(store.t("mpInstalling"), color = p.text, fontSize = PebbleFont.secondary)
                    } else {
                        Text(store.t("mpInstallOnHost"), color = p.text, fontSize = PebbleFont.secondary)
                    }
                }
            } else {
                Text(store.t("mpWebUrl"), color = p.text2, fontSize = PebbleFont.secondary)
                OutlinedTextField(
                    value = mpWebUrl,
                    onValueChange = { next ->
                        if (next != mpWebUrl) {
                            mpWebUrl = next
                            mpWebUrlTouched = true
                            onSaved("")
                        }
                    },
                    placeholder = { Text("https://magpie.example.com", fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
                    singleLine = true,
                    textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                    shape = PebbleDimens.rowShape,
                    colors = pebbleFieldColors(),
                    modifier = Modifier.fillMaxWidth(),
                )
                Text(store.t("mpWebUrlHint"), color = p.mute, fontSize = PebbleFont.caption)

                val providers = mp?.providers.orEmpty()
                val presets = mp?.presets.orEmpty()
                val configuredIds = providers.map { it.id }.toSet()
                val extraPresets = presets.filter { it.id !in configuredIds }
                if (providers.isNotEmpty()) {
                    CardDivider()
                    Text(
                        text = store.t("providersTitle", mapOf("n" to providers.size)),
                        color = p.text2,
                        fontSize = PebbleFont.secondary,
                        fontWeight = FontWeight.Medium,
                    )
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        providers.forEach { prov ->
                            TagPill(
                                text = prov.name?.takeIf { it.isNotEmpty() } ?: prov.id,
                                bg = if (prov.enabled == false) p.s1 else p.sage,
                                fg = if (prov.enabled == false) p.mute else p.sageInk,
                            )
                        }
                    }
                } else if (extraPresets.isNotEmpty()) {
                    CardDivider()
                    Text(
                        text = store.t("mpPresetsTitle"),
                        color = p.text2,
                        fontSize = PebbleFont.secondary,
                        fontWeight = FontWeight.Medium,
                    )
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        extraPresets.forEach { pre ->
                            TagPill(
                                text = pre.name?.takeIf { it.isNotEmpty() } ?: pre.id,
                                bg = p.s1,
                                fg = p.text2,
                            )
                        }
                    }
                }

                CardDivider()
                val addr = store.activeServer?.address.orEmpty()
                val local = listOf("localhost", "127.0.0.1", "[::1]").any { addr.contains(it) }
                val webUrl = mp?.webUrl.orEmpty()
                Text(
                    text = if (webUrl.isNotEmpty() && !local) store.t("mpOpenWeb") else store.t("mpOpenApp"),
                    color = p.accent,
                    fontSize = PebbleFont.body,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(PebbleDimens.rowShape)
                        .clickable {
                            if (webUrl.isNotEmpty() && !local) {
                                runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(webUrl))) }
                            } else {
                                runOp("magpie:open", "opFailed") { store.openMagpie(); null }
                            }
                        }
                        .padding(vertical = 6.dp),
                )
            }
        }
    }
    if (mpInstalled) SectionFooter(store.t("mpManageInApp"))
}
