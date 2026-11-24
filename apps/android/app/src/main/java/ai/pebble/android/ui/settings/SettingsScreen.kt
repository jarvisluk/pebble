package ai.pebble.android.ui.settings

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
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
import androidx.compose.ui.graphics.Color
import androidx.annotation.DrawableRes
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.ConnState
import ai.pebble.android.data.DesktopPermissionItem
import ai.pebble.android.data.HarnessInfo
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.errText
import ai.pebble.android.data.resolveDefaultModel
import ai.pebble.android.notify.NotificationHelper
import ai.pebble.android.ui.bots.MemoryItemRow
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionFooter
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.TagPill
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.launch
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

enum class SettingsTab(val titleKey: String, @DrawableRes val icon: Int) {
    GENERAL("tabGeneral", PebbleIcons.Settings),
    MODELS("tabModels", PebbleIcons.Memory),
    MEMORY("tabMemory", PebbleIcons.Psychology),
    DESKTOP("tabDesktop", PebbleIcons.DesktopWindows),
    REMOTE("tabRemote", PebbleIcons.Devices),
}

private data class BotDraft(val harness: String, val model: String)

@Composable
fun SettingsSidebar(
    store: PebbleStore,
    compact: Boolean,
    selectedTab: SettingsTab?,
    onSelectTab: (SettingsTab) -> Unit,
    onBack: () -> Unit,
) {
    val p = LocalPalette.current

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(if (compact) p.page else p.card),
    ) {
        @OptIn(ExperimentalMaterial3Api::class)
        TopAppBar(
            colors = TopAppBarDefaults.topAppBarColors(
                containerColor = if (compact) p.page else p.card,
                titleContentColor = p.text,
            ),
            navigationIcon = {
                IconButton(onClick = onBack) {
                    Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = store.t("backToChat"), tint = p.text)
                }
            },
            title = {
                Text(store.t("settings"), fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
            },
        )

        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            SectionCard {
                SettingsTab.entries.forEachIndexed { idx, tab ->
                    if (idx > 0) CardDivider()
                    val active = !compact && selectedTab == tab
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .background(if (active) p.sel else p.card)
                            .clickable { onSelectTab(tab) }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Icon(painterResource(tab.icon), contentDescription = null, tint = p.text, modifier = Modifier.size(20.dp))
                        Text(
                            text = store.t(tab.titleKey),
                            color = p.text,
                            fontSize = PebbleFont.cardTitle,
                            fontWeight = FontWeight.Medium,
                            modifier = Modifier.weight(1f),
                        )
                        if (compact) {
                            Icon(painterResource(PebbleIcons.ChevronRight), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsDetailScreen(
    store: PebbleStore,
    tab: SettingsTab,
    compact: Boolean,
    requestLeave: Boolean,
    onClearRequestLeave: () -> Unit,
    onDirtyChanged: (Boolean) -> Unit,
    onBack: () -> Unit,
    onLeaveSettings: () -> Unit,
    onOpenConnect: () -> Unit,
    onOpenDevices: () -> Unit,
    onOpenCreateBot: () -> Unit,
    onOpenDesktop: (String) -> Unit,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()

    val harnesses = remember(store.settings?.harnesses) {
        val list = store.settings?.harnesses.orEmpty()
        list.ifEmpty {
            listOf("opencode", "codebuddy", "qoder", "codex", "claude", "grok", "trae", "pi", "dsh", "agy").map { HarnessInfo(id = it, label = it) }
        }
    }
    val primaryHarness = harnesses.firstOrNull()?.id ?: "opencode"
    val baseDefaultHarness = store.settings?.defaultHarness?.takeIf { it.isNotEmpty() } ?: primaryHarness

    var defaultHarness by remember { mutableStateOf(baseDefaultHarness) }
    var defaultHarnessTouched by remember { mutableStateOf(false) }
    var defaultModel by remember { mutableStateOf("") }
    var defaultTouched by remember { mutableStateOf(false) }
    val perHarness = remember { mutableStateMapOf<String, String>() }
    var perTouched by remember { mutableStateOf(false) }
    var perOpen by remember { mutableStateOf(false) }
    var userMd by remember { mutableStateOf("") }
    var userMdTouched by remember { mutableStateOf(false) }
    val drafts = remember { mutableStateMapOf<String, BotDraft>() }
    var touchedBots by remember { mutableStateOf(setOf<String>()) }
    var loadingModelsFor by remember { mutableStateOf(setOf<String>()) }

    var saving by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf("") }
    var error by remember { mutableStateOf("") }
    var saved by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf<ConfirmItem?>(null) }
    var showUnsavedAlert by remember { mutableStateOf(false) }

    var mem by remember { mutableStateOf<MemoryList?>(null) }
    var memScope by remember { mutableStateOf("global") }
    var memFilterKind by remember { mutableStateOf("") }
    var memQuery by remember { mutableStateOf("") }
    var memAdd by remember { mutableStateOf(false) }
    var memKind by remember { mutableStateOf("fact") }
    var memText by remember { mutableStateOf("") }
    var memTags by remember { mutableStateOf("") }

    var deskHarness by remember { mutableStateOf("") }
    var deskModel by remember { mutableStateOf("") }
    var deskTouched by remember { mutableStateOf(false) }
    var deskPerms by remember { mutableStateOf<List<DesktopPermissionItem>>(emptyList()) }
    var deskPermsLoading by remember { mutableStateOf(false) }

    var notifGranted by remember { mutableStateOf(NotificationHelper.hasNotificationPermission(context)) }
    val notifLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        notifGranted = granted || NotificationHelper.hasNotificationPermission(context)
    }

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
                drafts[b.id] = BotDraft(b.harness.orEmpty(), origModel(b))
            }
        }
    }

    fun sync() {
        val s = store.settings ?: return
        if (!defaultHarnessTouched) defaultHarness = baseDefaultHarness
        if (!defaultTouched) defaultModel = s.defaultModel.orEmpty().trim()
        if (!deskTouched) {
            deskHarness = s.desktopHarness.orEmpty()
            deskModel = s.desktopModel.orEmpty()
        }
        if (!perTouched) {
            perHarness.clear()
            val cleaned = cleanMap(s.defaultModels)
            perHarness.putAll(cleaned)
            if (cleaned.isNotEmpty()) perOpen = true
        }
        if (!userMdTouched) userMd = s.sharedUserMd.orEmpty()
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
        fun rtrim(x: String) = x.replace(Regex("\\s+$"), "")
        (defaultHarnessTouched && defaultHarness != baseDefaultHarness) ||
            (defaultTouched && defaultModel.trim() != s.defaultModel.orEmpty().trim()) ||
            (perTouched && cleanMap(perHarness) != cleanMap(s.defaultModels)) ||
            (userMdTouched && rtrim(userMd) != rtrim(s.sharedUserMd.orEmpty()))
    }
    val dirty = settingsChanged || botUpdates.isNotEmpty()

    LaunchedEffect(dirty) { onDirtyChanged(dirty) }
    DisposableEffect(Unit) {
        onDispose { onDirtyChanged(false) }
    }
    LaunchedEffect(requestLeave) {
        if (requestLeave) {
            onClearRequestLeave()
            showUnsavedAlert = true
        }
    }

    suspend fun loadMem(q: String, scopeVal: String = memScope, kindVal: String = memFilterKind) {
        try {
            mem = store.fetchMemories(scope = scopeVal, query = q, kind = kindVal, limit = 30)
        } catch (e: Throwable) {
            error = errText(e, store.t("opFailed"))
        }
    }

    suspend fun loadPerms() {
        deskPermsLoading = true
        runCatching { store.fetchDesktopPermissions("local") }.getOrNull()?.items?.let { deskPerms = it }
        deskPermsLoading = false
    }

    LaunchedEffect(Unit) {
        runCatching { store.loadSettings() }
        runCatching { store.refreshBots() }
        sync()
        loadMem("")
        loadPerms()
    }

    LaunchedEffect(store.settings, store.bots) {
        sync()
    }

    fun save(closeAfter: Boolean = false) {
        error = ""; saved = ""; saving = true
        scope.launch {
            try {
                store.saveSettings(
                    changed = settingsChanged,
                    defaultHarness = defaultHarness,
                    defaultModel = defaultModel.trim(),
                    defaultModels = cleanMap(perHarness),
                    sharedUserMd = userMd,
                    botUpdates = botUpdates,
                )
                defaultHarnessTouched = false; defaultTouched = false; perTouched = false; userMdTouched = false
                touchedBots = emptySet()
                drafts.clear()
                sync()
                saved = store.t("settingsSaved")
                if (closeAfter) onLeaveSettings()
            } catch (e: Throwable) {
                error = errText(e, store.t("saveFailed"))
            } finally {
                saving = false
            }
        }
    }

    fun runOp(key: String, failKey: String, op: suspend () -> String?) {
        error = ""; saved = ""; busy = key
        scope.launch {
            try {
                op()?.let { saved = it }
            } catch (e: Throwable) {
                error = errText(e, store.t(failKey))
            } finally {
                busy = ""
            }
        }
    }

    fun loadCands(harnessId: String, force: Boolean = false) {
        if (harnessId.isEmpty()) return
        if (!force && !store.models[harnessId].isNullOrEmpty()) return
        if (!force && harnessId in loadingModelsFor) return
        loadingModelsFor = loadingModelsFor + harnessId
        scope.launch {
            try {
                store.loadModels(harnessId, force = force)
            } catch (e: Throwable) {
                if (force) error = errText(e, store.t("loadModelsFailed"))
            } finally {
                loadingModelsFor = loadingModelsFor - harnessId
            }
        }
    }

    fun preloadAllModels(force: Boolean = false) {
        harnesses.forEach { h -> loadCands(h.id, force = force) }
    }

    fun harnessFieldLabel(h: HarnessInfo): String = when {
        h.magpie?.connected == true -> "${h.title} · Magpie"
        store.settings?.magpie?.installed == true -> "${h.title} · ${store.t("mpNative")}"
        else -> h.title
    }

    LaunchedEffect(tab, store.isConnected, store.modelsGeneration, harnesses.map { it.id }) {
        if (tab == SettingsTab.MODELS && store.isConnected) {
            preloadAllModels()
        }
    }

    fun connectMagpie(h: HarnessInfo) {
        val dm = defaultModel.trim()
        val target = dm.ifEmpty { h.defaultModel.orEmpty() }
        if (target.isEmpty()) {
            error = store.t("pickMagpieModel")
            return
        }
        confirm = ConfirmItem(
            title = store.t("mpConnect"),
            message = "magpie ${h.id} $target",
            destructive = false,
        ) {
            runOp("magpie:${h.id}", "connectFailed") {
                store.connectMagpie(h.id, target)
                store.t("connectedMagpie", mapOf("label" to h.id))
            }
        }
    }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    if (showUnsavedAlert) {
        AlertDialog(
            onDismissRequest = { showUnsavedAlert = false },
            containerColor = p.card,
            title = { Text(store.t("unsavedTitle"), color = p.text, fontWeight = FontWeight.Bold) },
            text = { Text(store.t("unsavedDesc"), color = p.text2) },
            confirmButton = {
                TextButton(onClick = { showUnsavedAlert = false; save(closeAfter = true) }) {
                    Text(store.t("saveAndLeave"), color = p.accent, fontWeight = FontWeight.SemiBold)
                }
            },
            dismissButton = {
                Row {
                    TextButton(onClick = { showUnsavedAlert = false; onLeaveSettings() }) {
                        Text(store.t("discardChanges"), color = p.danger)
                    }
                    TextButton(onClick = { showUnsavedAlert = false }) {
                        Text(store.t("cancel"), color = p.text2)
                    }
                }
            },
        )
    }

    Scaffold(
        containerColor = p.page,
        topBar = {
            TopAppBar(
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.page,
                    titleContentColor = p.text,
                ),
                navigationIcon = {
                    if (compact) {
                        IconButton(onClick = onBack) {
                            Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = store.t("back"), tint = p.text)
                        }
                    }
                },
                title = {
                    Text(store.t(tab.titleKey), fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
                },
                actions = {
                    if (saving) {
                        CircularProgressIndicator(modifier = Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp, color = p.accent)
                    } else {
                        TextButton(onClick = { save() }, enabled = dirty) {
                            Text(
                                text = store.t("save"),
                                color = if (dirty) p.accent else p.mute,
                                fontSize = PebbleFont.body,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                    }
                },
            )
        },
    ) { innerPadding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .imePadding(),
            contentAlignment = Alignment.TopCenter,
        ) {
            Column(
                modifier = Modifier
                    .widthIn(max = 680.dp)
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
                    .navigationBarsPadding()
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                if (error.isNotEmpty()) {
                    SectionCard {
                        Box(modifier = Modifier.padding(14.dp)) { StatusTextBanner(error, error = true) }
                    }
                }
                if (saved.isNotEmpty()) {
                    SectionCard {
                        Box(modifier = Modifier.padding(14.dp)) { StatusTextBanner(saved, error = false) }
                    }
                }

                when (tab) {
                    SettingsTab.GENERAL -> {
                        SectionHeader(store.t("appearance"))
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(12.dp),
                            ) {
                                val themes = listOf(
                                    "system" to store.t("followSystem"),
                                    "light" to store.t("themeLight"),
                                    "dark" to store.t("themeDark"),
                                )
                                SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                                    themes.forEachIndexed { idx, (key, lbl) ->
                                        SegmentedButton(
                                            selected = store.themePref == key,
                                            onClick = { store.updateThemePref(key) },
                                            shape = SegmentedButtonDefaults.itemShape(idx, themes.size),
                                            label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                                        )
                                    }
                                }

                                val langs = listOf(
                                    "system" to store.t("followSystem"),
                                    "zh-CN" to "简体中文",
                                    "en" to "English",
                                )
                                Text(store.t("language"), color = p.text2, fontSize = PebbleFont.secondary)
                                SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                                    langs.forEachIndexed { idx, (key, lbl) ->
                                        SegmentedButton(
                                            selected = store.langPref == key,
                                            onClick = { store.updateLangPref(key) },
                                            shape = SegmentedButtonDefaults.itemShape(idx, langs.size),
                                            label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                                        )
                                    }
                                }
                            }
                        }

                        SectionHeader(store.t("pushSection"))
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                    Text(store.t("pushSystemPerm"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                    if (notifGranted) {
                                        Text(store.t("pushGranted"), color = p.sageInk, fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold)
                                    } else {
                                        TextButton(
                                            onClick = {
                                                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                                                    notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                                                } else {
                                                    runCatching {
                                                        context.startActivity(
                                                            Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                                                                .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                                                        )
                                                    }
                                                }
                                            },
                                            contentPadding = PaddingValues(horizontal = 8.dp),
                                        ) {
                                            Text(store.t("pushEnable"), color = p.accent, fontSize = PebbleFont.body)
                                        }
                                    }
                                }
                                CardDivider()
                                Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                    Text(store.t("pushAndroidChannel"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                                    Text(
                                        text = if (notifGranted) store.t("pushEnabled") else store.t("pushDisabled"),
                                        color = if (notifGranted) p.sageInk else p.mute,
                                        fontSize = PebbleFont.secondary,
                                    )
                                }
                            }
                        }

                        SectionHeader(store.t("openSourceTitle"))
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Text(store.t("openSourceDesc"), color = p.text2, fontSize = PebbleFont.secondary)
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable {
                                            runCatching {
                                                context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://github.com/jarvisluk/bot-hub")))
                                            }
                                        }
                                        .padding(vertical = 6.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                                ) {
                                    Icon(painterResource(PebbleIcons.Star), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                    Text(store.t("openSourceLinkBtn"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                                }
                            }
                        }
                    }

                    SettingsTab.MODELS -> {
                        val mp = store.settings?.magpie
                        val installed = mp?.installed == true
                        val connectedHarnesses = harnesses.filter { it.magpie?.connected == true }
                        val nativeHarnesses = harnesses.filter { it.magpie?.connected != true }

                        SectionHeader(
                            title = store.t("magpieTakeover"),
                            trailing = {
                                TagPill(
                                    text = when {
                                        !installed -> store.t("notInstalled")
                                        mp.healthy == true -> store.t("gatewayOk")
                                        else -> store.t("gatewayDown")
                                    },
                                    bg = if (!installed) p.s1 else if (mp.healthy == true) p.sage else p.pink,
                                    fg = if (!installed) p.text2 else if (mp.healthy == true) p.sageInk else p.danger,
                                )
                            },
                        )
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                when {
                                    !installed -> {
                                        Text(store.t("hostNoMagpieGlobal"), color = p.text2, fontSize = PebbleFont.body)
                                        OutlinedButton(
                                            onClick = {
                                                runOp("magpie:install", "opFailed") {
                                                    store.installMagpie()
                                                    store.t("mpInstallDone")
                                                }
                                            },
                                            enabled = busy.isEmpty(),
                                        ) {
                                            if (busy == "magpie:install") {
                                                CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = p.accent)
                                                Spacer(Modifier.width(8.dp))
                                                Text(store.t("mpInstalling"), color = p.text)
                                            } else {
                                                Text(store.t("mpInstallOnHost"), color = p.text)
                                            }
                                        }
                                    }
                                    connectedHarnesses.isEmpty() -> {
                                        Text(store.t("mpNoConnected"), color = p.text2, fontSize = PebbleFont.body)
                                    }
                                    else -> {
                                        connectedHarnesses.forEachIndexed { idx, h ->
                                            if (idx > 0) CardDivider()
                                            val key = "magpie:${h.id}"
                                            Row(
                                                modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                                                verticalAlignment = Alignment.CenterVertically,
                                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                                            ) {
                                                Column(modifier = Modifier.weight(1f)) {
                                                    Text(h.title, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold)
                                                    val m = h.magpie?.model?.takeIf { it.isNotEmpty() } ?: store.t("mpConnected")
                                                    Text("Magpie · $m", color = p.sageInk, fontSize = PebbleFont.secondary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                                }
                                                OutlinedButton(
                                                    onClick = {
                                                        confirm = ConfirmItem(
                                                            title = store.t("restoreNativeTitle"),
                                                            message = store.t("restoreNativeDesc", mapOf("label" to h.id)),
                                                        ) {
                                                            runOp(key, "restoreFailed") {
                                                                store.disconnectMagpie(h.id)
                                                                store.t("restoredNative", mapOf("label" to h.id))
                                                            }
                                                        }
                                                    },
                                                    enabled = busy != key,
                                                    contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                                ) {
                                                    Text(store.t("mpRestore"), color = p.danger, fontSize = PebbleFont.secondary)
                                                }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                        if (installed) {
                            SectionFooter(store.t("magpieHint"))
                            SectionCard {
                                val addr = store.activeServer?.address.orEmpty()
                                val local = listOf("localhost", "127.0.0.1", "[::1]").any { addr.contains(it) }
                                val webUrl = mp.webUrl.orEmpty()
                                Text(
                                    text = if (webUrl.isNotEmpty() && !local) store.t("mpOpenWeb") else store.t("mpOpenApp"),
                                    color = p.accent,
                                    fontSize = PebbleFont.body,
                                    fontWeight = FontWeight.Medium,
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clickable {
                                            if (webUrl.isNotEmpty() && !local) {
                                                runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(webUrl))) }
                                            } else {
                                                runOp("magpie:open", "opFailed") { store.openMagpie(); null }
                                            }
                                        }
                                        .padding(16.dp),
                                )
                            }
                            SectionFooter(store.t("mpManageInApp"))
                        }

                        if (nativeHarnesses.isNotEmpty()) {
                            SectionHeader(store.t("nativeClisTitle"))
                            SectionCard {
                                Column(
                                    modifier = Modifier.padding(14.dp),
                                    verticalArrangement = Arrangement.spacedBy(10.dp),
                                ) {
                                    nativeHarnesses.forEachIndexed { idx, h ->
                                        if (idx > 0) CardDivider()
                                        val cands = store.models[h.id].orEmpty()
                                        val key = "magpie:${h.id}"
                                        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                                Column(modifier = Modifier.weight(1f)) {
                                                    Text(h.title, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold)
                                                    val nm = h.magpie?.nativeModel?.takeIf { it.isNotEmpty() }?.let { " · $it" }.orEmpty()
                                                    Text(store.t("mpNative") + nm, color = p.text2, fontSize = PebbleFont.secondary, maxLines = 1)
                                                }
                                                if (installed) {
                                                    OutlinedButton(
                                                        onClick = { connectMagpie(h) },
                                                        enabled = busy != key,
                                                        contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                                    ) {
                                                        Text(store.t("mpConnectAction"), color = p.accent, fontSize = PebbleFont.secondary)
                                                    }
                                                }
                                            }
                                            ModelInputWithMenu(
                                                value = perHarness[h.id].orEmpty(),
                                                placeholder = store.t("nativeModelPh"),
                                                candidates = cands,
                                                loading = h.id in loadingModelsFor,
                                                onLoadCandidates = { loadCands(h.id) },
                                                onRefreshCandidates = { loadCands(h.id, force = true) },
                                                onValueChange = { next ->
                                                    if (next != perHarness[h.id].orEmpty()) {
                                                        perHarness[h.id] = next
                                                        perTouched = true
                                                        saved = ""
                                                    }
                                                },
                                                store = store,
                                            )
                                        }
                                    }
                                }
                            }
                            SectionFooter(store.t("nativeClisIntro"))
                        }

                        // Global default model
                        val orderedGlobalHarnesses = buildList {
                            val seen = mutableSetOf<String>()
                            val anyInstalled = harnesses.any { it.installed == true }
                            fun addUnique(h: HarnessInfo) {
                                if (seen.add(h.id)) add(h)
                            }
                            harnesses.firstOrNull { it.id == defaultHarness }?.let(::addUnique)
                            harnesses.filter { it.installed == true && it.magpie?.connected != true }.forEach(::addUnique)
                            harnesses.filter { it.installed == true && it.magpie?.connected == true }.forEach(::addUnique)
                            if (!anyInstalled) {
                                harnesses.forEach(::addUnique)
                            } else {
                                harnesses.filter { !store.models[it.id].isNullOrEmpty() }.forEach(::addUnique)
                            }
                        }

                        SectionHeader(store.t("globalDefaultModel"))
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
                                                text = { Text(h.title, color = p.text) },
                                                onClick = {
                                                    if (h.id != defaultHarness) {
                                                        defaultHarness = h.id
                                                        defaultHarnessTouched = true
                                                        saved = ""
                                                        if (store.models[h.id].isNullOrEmpty() && store.isConnected) {
                                                            loadCands(h.id)
                                                        }
                                                    }
                                                    defHarnessOpen = false
                                                },
                                            )
                                        }
                                    }
                                }
                                GlobalModelInputWithMenu(
                                    value = defaultModel,
                                    placeholder = "magpie/claude-sonnet-4",
                                    harnesses = orderedGlobalHarnesses,
                                    defaultHarness = defaultHarness,
                                    harnessLabel = ::harnessFieldLabel,
                                    modelsByHarness = store.models,
                                    loadingHarnesses = loadingModelsFor,
                                    onLoadHarness = { hid -> loadCands(hid) },
                                    onRefreshHarness = { hid -> loadCands(hid, force = true) },
                                    onRefreshAll = { preloadAllModels(force = true) },
                                    onValueChange = { next ->
                                        if (next != defaultModel) {
                                            defaultModel = next
                                            defaultTouched = true
                                            saved = ""
                                        }
                                    },
                                    store = store,
                                )

                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable { perOpen = !perOpen }
                                        .padding(vertical = 6.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Text(store.t("perHarnessTitle"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                                    Icon(painterResource(if (perOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
                                }
                                AnimatedVisibility(visible = perOpen) {
                                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                        harnesses.forEach { h ->
                                            Text(harnessFieldLabel(h), color = p.text2, fontSize = PebbleFont.secondary)
                                            ModelInputWithMenu(
                                                value = perHarness[h.id].orEmpty(),
                                                placeholder = store.t("followGlobalPh"),
                                                candidates = store.models[h.id].orEmpty(),
                                                loading = h.id in loadingModelsFor,
                                                onLoadCandidates = { loadCands(h.id) },
                                                onRefreshCandidates = { loadCands(h.id, force = true) },
                                                onValueChange = { next ->
                                                    if (next != perHarness[h.id].orEmpty()) {
                                                        perHarness[h.id] = next
                                                        perTouched = true
                                                        saved = ""
                                                    }
                                                },
                                                store = store,
                                            )
                                        }
                                    }
                                }
                            }
                        }

                        // Per-bot overrides
                        SectionHeader(store.t("botModelsSection"))
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                if (store.bots.isEmpty()) {
                                    Text(store.t("noBotsInSettings"), color = p.mute, fontSize = PebbleFont.body)
                                } else {
                                    store.bots.forEachIndexed { idx, b ->
                                        if (idx > 0) CardDivider()
                                        val d = drafts[b.id] ?: BotDraft(b.harness.orEmpty(), origModel(b))
                                        val per = perHarness[d.harness].orEmpty().trim()
                                        val ph = per.ifEmpty { resolveDefaultModel(defaultModel.trim(), d.harness) }
                                        var botHarnessMenu by remember { mutableStateOf(false) }

                                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                                                BotAvatar(id = b.id, size = 28.dp, index = idx, avatar = b.avatar, color = b.color)
                                                Column(modifier = Modifier.weight(1f)) {
                                                    Text(b.displayName, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold)
                                                    Text("@${b.id}", color = p.text2, fontSize = PebbleFont.caption, fontFamily = MonoFont)
                                                }
                                                Box {
                                                    TextButton(onClick = { botHarnessMenu = true }) {
                                                        Text(d.harness.ifEmpty { "-" }, color = p.accent, fontSize = PebbleFont.secondary)
                                                        Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                                    }
                                                    DropdownMenu(expanded = botHarnessMenu, onDismissRequest = { botHarnessMenu = false }, containerColor = p.card) {
                                                        harnesses.forEach { h ->
                                                            DropdownMenuItem(
                                                                text = { Text(h.title, color = p.text) },
                                                                onClick = {
                                                                    if (h.id != d.harness) {
                                                                        drafts[b.id] = d.copy(harness = h.id)
                                                                        touchedBots = touchedBots + b.id
                                                                        saved = ""
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
                                                        saved = ""
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
                    }

                    SettingsTab.MEMORY -> {
                        SectionHeader(store.t("globalSharedMem"))
                        SectionCard {
                            OutlinedTextField(
                                value = userMd,
                                onValueChange = { next ->
                                    if (next != userMd) {
                                        userMd = next
                                        userMdTouched = true
                                        saved = ""
                                    }
                                },
                                placeholder = { Text(store.t("userMdPh")) },
                                minLines = 4,
                                maxLines = 16,
                                shape = PebbleDimens.rowShape,
                                colors = pebbleFieldColors(),
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(14.dp),
                            )
                        }
                        SectionFooter("_shared/USER.md")

                        SectionHeader(store.t("magpieGlobalMem"))
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                val sem = mem?.semantic
                                if (mem?.installed == true && sem != null) {
                                    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                        TagPill(
                                            text = store.t(if (sem.active == true) "memSemanticOn" else "memSemanticOff"),
                                            bg = if (sem.active == true) p.sage else p.s1,
                                            fg = if (sem.active == true) p.sageInk else p.text2,
                                        )
                                        Spacer(Modifier.weight(1f))
                                        if (sem.active != true) {
                                            TextButton(onClick = { runOp("magpie:open", "opFailed") { store.openMagpie(); null } }) {
                                                Text(store.t("openMagpie"), color = p.accent, fontSize = PebbleFont.secondary)
                                            }
                                        }
                                    }
                                    if (sem.active != true) {
                                        Text(store.t("memSemanticHintOff"), color = p.text2, fontSize = PebbleFont.secondary)
                                    }
                                }

                                val scopes = listOf("global" to store.t("memScopeGlobal"), "all" to store.t("memScopeAll"))
                                SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                                    scopes.forEachIndexed { idx, (sc, lbl) ->
                                        SegmentedButton(
                                            selected = memScope == sc,
                                            onClick = {
                                                memScope = sc
                                                scope.launch { loadMem(memQuery, scopeVal = sc) }
                                            },
                                            shape = SegmentedButtonDefaults.itemShape(idx, scopes.size),
                                            label = { Text(lbl, fontSize = PebbleFont.secondary) },
                                        )
                                    }
                                }

                                var kindMenuOpen by remember { mutableStateOf(false) }
                                Box {
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clip(PebbleDimens.rowShape)
                                            .clickable { kindMenuOpen = true }
                                            .padding(vertical = 6.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                    ) {
                                        Text(store.t("memType"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                                        Text(memFilterKind.ifEmpty { store.t("memKindAll") }, color = p.text, fontSize = PebbleFont.secondary)
                                        Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                                    }
                                    DropdownMenu(expanded = kindMenuOpen, onDismissRequest = { kindMenuOpen = false }, containerColor = p.card) {
                                        (listOf("") + listOf("fact", "preference", "rule", "reference", "profile", "procedure", "episode")).forEach { k ->
                                            DropdownMenuItem(
                                                text = { Text(k.ifEmpty { store.t("memKindAll") }, color = p.text) },
                                                onClick = {
                                                    memFilterKind = k
                                                    kindMenuOpen = false
                                                    scope.launch { loadMem(memQuery, kindVal = k) }
                                                },
                                            )
                                        }
                                    }
                                }

                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    OutlinedTextField(
                                        value = memQuery,
                                        onValueChange = { memQuery = it },
                                        placeholder = {
                                            Text(store.t(if (mem?.semantic?.active == true) "searchSemanticMemPh" else "searchGlobalMemPh"))
                                        },
                                        singleLine = true,
                                        shape = PebbleDimens.rowShape,
                                        colors = pebbleFieldColors(),
                                        modifier = Modifier.weight(1f),
                                    )
                                    if (memQuery.isNotEmpty()) {
                                        IconButton(onClick = { memQuery = ""; scope.launch { loadMem("") } }) {
                                            Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("clear"), tint = p.mute)
                                        }
                                    }
                                    IconButton(onClick = { scope.launch { loadMem(memQuery) } }) {
                                        Icon(painterResource(PebbleIcons.Search), contentDescription = store.t("search"), tint = p.accent)
                                    }
                                }

                                val m = mem
                                when {
                                    m?.installed != true -> {
                                        Text(store.t("hostNoMagpieGlobal"), color = p.text2, fontSize = PebbleFont.secondary)
                                        OutlinedButton(
                                            onClick = {
                                                runOp("magpie:install", "opFailed") {
                                                    store.installMagpie()
                                                    loadMem(memQuery)
                                                    store.t("mpInstallDone")
                                                }
                                            },
                                            enabled = busy.isEmpty(),
                                        ) {
                                            Text(if (busy == "magpie:install") store.t("mpInstalling") else store.t("mpInstallOnHost"), color = p.text)
                                        }
                                    }
                                    m.available != true -> {
                                        val detailErr = m.error?.takeIf { it.isNotEmpty() }?.let { ": $it" }.orEmpty()
                                        Text(store.t("magpieNoResp") + detailErr, color = p.danger, fontSize = PebbleFont.secondary)
                                    }
                                    m.items.isNullOrEmpty() -> {
                                        Text(store.t("noGlobalMem"), color = p.mute, fontSize = PebbleFont.secondary)
                                    }
                                    else -> {
                                        m.items.forEachIndexed { idx, it ->
                                            if (idx > 0) CardDivider()
                                            MemoryItemRow(
                                                store = store,
                                                item = it,
                                                showTags = true,
                                                showScope = memScope == "all",
                                                onDelete = {
                                                    confirm = ConfirmItem(
                                                        title = store.t("deleteMemTitle"),
                                                        message = store.t("deleteMemConfirm"),
                                                    ) {
                                                        scope.launch {
                                                            runCatching {
                                                                store.deleteMemory(it.id)
                                                                loadMem(memQuery)
                                                            }.onFailure { err -> error = errText(err, store.t("delFailed")) }
                                                        }
                                                    }
                                                },
                                            )
                                        }
                                    }
                                }

                                CardDivider()
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable { memAdd = !memAdd }
                                        .padding(vertical = 6.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Text(store.t("addShort"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium, modifier = Modifier.weight(1f))
                                    Icon(painterResource(if (memAdd) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
                                }
                                AnimatedVisibility(visible = memAdd) {
                                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                        val kinds = listOf("fact", "preference", "rule", "reference")
                                        SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
                                            kinds.forEachIndexed { idx, k ->
                                                SegmentedButton(
                                                    selected = memKind == k,
                                                    onClick = { memKind = k },
                                                    shape = SegmentedButtonDefaults.itemShape(idx, kinds.size),
                                                    label = { Text(k, fontSize = PebbleFont.caption, maxLines = 1) },
                                                )
                                            }
                                        }
                                        OutlinedTextField(
                                            value = memText,
                                            onValueChange = { memText = it },
                                            placeholder = { Text(store.t("globalMemPh")) },
                                            minLines = 2,
                                            maxLines = 6,
                                            shape = PebbleDimens.rowShape,
                                            colors = pebbleFieldColors(),
                                            modifier = Modifier.fillMaxWidth(),
                                        )
                                        OutlinedTextField(
                                            value = memTags,
                                            onValueChange = { memTags = it },
                                            placeholder = { Text(store.t("tagsPh")) },
                                            singleLine = true,
                                            shape = PebbleDimens.rowShape,
                                            colors = pebbleFieldColors(),
                                            modifier = Modifier.fillMaxWidth(),
                                        )
                                        Button(
                                            onClick = {
                                                val txt = memText.trim()
                                                if (txt.isEmpty()) {
                                                    error = store.t("memContentReq")
                                                    return@Button
                                                }
                                                val tags = memTags.split(',', '，').map { it.trim() }.filter { it.isNotEmpty() }
                                                runOp("mem:save", "saveMemFailed") {
                                                    store.saveMemory(mapOf("scope" to "global", "kind" to memKind, "text" to txt, "tags" to tags))
                                                    memText = ""; memTags = ""; memAdd = false
                                                    loadMem(memQuery)
                                                    store.t("globalMemSaved")
                                                }
                                            },
                                            enabled = busy != "mem:save",
                                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                                        ) {
                                            Text(store.t("saveMemory"))
                                        }
                                    }
                                }
                            }
                        }
                    }

                    SettingsTab.DESKTOP -> {
                        SectionHeader(
                            title = store.t("deskPermsTitle"),
                            trailing = {
                                IconButton(onClick = { scope.launch { loadPerms() } }, enabled = !deskPermsLoading, modifier = Modifier.size(28.dp)) {
                                    Icon(painterResource(PebbleIcons.Refresh), contentDescription = store.t("refresh"), tint = p.accent, modifier = Modifier.size(18.dp))
                                }
                            },
                        )
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                if (deskPerms.isEmpty()) {
                                    if (deskPermsLoading) {
                                        CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
                                    } else {
                                        Text(store.t("deskPermsDesc"), color = p.text2, fontSize = PebbleFont.secondary)
                                    }
                                } else {
                                    deskPerms.forEachIndexed { idx, item ->
                                        if (idx > 0) CardDivider()
                                        val stLabel = when (item.status) {
                                            "granted" -> store.t("deskPermGranted")
                                            "denied" -> store.t("deskPermDenied")
                                            "unsupported" -> store.t("deskPermUnsupported")
                                            else -> store.t("deskPermNotDetermined")
                                        }
                                        val bg = when (item.status) {
                                            "granted" -> p.sage
                                            "denied" -> p.pink
                                            else -> p.s1
                                        }
                                        val fg = when (item.status) {
                                            "granted" -> p.sageInk
                                            "denied" -> p.danger
                                            else -> p.text2
                                        }
                                        val permName = store.t("deskPermName_${item.id}").let { if (it == "deskPermName_${item.id}") item.name else it }
                                        val permDesc = store.t("deskPermDesc_${item.id}").let { if (it == "deskPermDesc_${item.id}") item.description else it }
                                        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                                                Text(permName, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                                                TagPill(stLabel, bg, fg)
                                            }
                                            Text(permDesc, color = p.text2, fontSize = PebbleFont.secondary)
                                            if ((item.canPrompt == true && item.status != "granted") || item.canOpenSettings == true) {
                                                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                                    if (item.canPrompt == true && item.status != "granted") {
                                                        Button(
                                                            onClick = {
                                                                runOp("perm:${item.id}", "opFailed") {
                                                                    val res = store.requestDesktopPermission("local", item.id, openSettings = false)
                                                                    res.items?.let { deskPerms = it }
                                                                    if (res.openedSettings == true) store.t("deskPermOpened")
                                                                    else if (res.prompted == true) store.t("deskPermPrompted") else null
                                                                }
                                                            },
                                                            enabled = busy != "perm:${item.id}",
                                                            colors = ButtonDefaults.buttonColors(containerColor = p.accent, contentColor = p.onAccent),
                                                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                                        ) {
                                                            Text(store.t("deskPermRequest"), fontSize = PebbleFont.secondary)
                                                        }
                                                    }
                                                    if (item.canOpenSettings == true) {
                                                        OutlinedButton(
                                                            onClick = {
                                                                runOp("perm:${item.id}", "opFailed") {
                                                                    val res = store.requestDesktopPermission("local", item.id, openSettings = true)
                                                                    res.items?.let { deskPerms = it }
                                                                    if (res.openedSettings == true) store.t("deskPermOpened") else null
                                                                }
                                                            },
                                                            enabled = busy != "perm:${item.id}",
                                                            contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                                                        ) {
                                                            Text(store.t("deskPermOpenSettings"), fontSize = PebbleFont.secondary, color = p.text)
                                                        }
                                                    }
                                                }
                                            }
                                        }
                                    }
                                }
                                CardDivider()
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable { onOpenDesktop("local") }
                                        .padding(vertical = 6.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                                ) {
                                    Icon(painterResource(PebbleIcons.DesktopWindows), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                    Text(store.t("openDesktop"), color = p.accent, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
                                }
                            }
                        }
                        SectionFooter(store.t("deskPermsDesc"))

                        SectionHeader(store.t("deskWorkerTitle"))
                        SectionCard {
                            Column(
                                modifier = Modifier.padding(14.dp),
                                verticalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                var dwHarnessMenu by remember { mutableStateOf(false) }
                                Box {
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clip(PebbleDimens.rowShape)
                                            .clickable { dwHarnessMenu = true }
                                            .padding(vertical = 6.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                    ) {
                                        Text(store.t("deskWorkerHarnessLabel"), color = p.text2, fontSize = PebbleFont.secondary, modifier = Modifier.weight(1f))
                                        Text(
                                            text = deskHarness.ifEmpty { store.t("deskWorkerFollowBot") },
                                            color = p.text,
                                            fontSize = PebbleFont.secondary,
                                            fontWeight = FontWeight.Medium,
                                        )
                                        Icon(painterResource(PebbleIcons.ArrowDropDown), contentDescription = null, tint = p.mute)
                                    }
                                    DropdownMenu(expanded = dwHarnessMenu, onDismissRequest = { dwHarnessMenu = false }, containerColor = p.card) {
                                        DropdownMenuItem(
                                            text = { Text(store.t("deskWorkerFollowBot"), color = p.text) },
                                            onClick = { deskHarness = ""; deskTouched = true; dwHarnessMenu = false },
                                        )
                                        harnesses.forEach { h ->
                                            DropdownMenuItem(
                                                text = { Text(h.title, color = p.text) },
                                                onClick = { deskHarness = h.id; deskTouched = true; dwHarnessMenu = false },
                                            )
                                        }
                                    }
                                }
                                Row(
                                    modifier = Modifier.fillMaxWidth(),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                                ) {
                                    OutlinedTextField(
                                        value = deskModel,
                                        onValueChange = { deskModel = it; deskTouched = true },
                                        label = { Text(store.t("deskWorkerModelLabel"), fontSize = PebbleFont.caption) },
                                        placeholder = { Text(store.t("deskWorkerModelPh"), fontFamily = MonoFont) },
                                        singleLine = true,
                                        textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                                        shape = PebbleDimens.rowShape,
                                        colors = pebbleFieldColors(),
                                        modifier = Modifier.weight(1f),
                                    )
                                    OutlinedButton(
                                        onClick = {
                                            runOp("desk", "saveFailed") {
                                                store.setDesktopWorker(deskHarness, deskModel)
                                                deskTouched = false
                                                store.t("deskWorkerSaved")
                                            }
                                        },
                                        enabled = deskTouched && busy != "desk",
                                    ) {
                                        Text(store.t("save"), color = p.text)
                                    }
                                }
                            }
                        }
                    }

                    SettingsTab.REMOTE -> {
                        val hostColor = when (store.connState) {
                            ConnState.CONNECTED -> if (store.online) p.sageInk else p.busy
                            ConnState.CONNECTING -> p.busy
                            ConnState.UNAUTHORIZED, ConnState.ERROR -> p.danger
                            ConnState.IDLE -> p.mute
                        }
                        val hostText = when (val s = store.activeServer) {
                            null -> store.t("disconnected")
                            else -> when (store.connState) {
                                ConnState.CONNECTING -> "${s.label} · ${store.t("connectingShort")}"
                                ConnState.UNAUTHORIZED -> "${s.label} · ${store.t("needsToken")}"
                                ConnState.ERROR -> "${s.label} · ${store.t("connLost")}"
                                ConnState.CONNECTED -> if (store.online) s.label else "${s.label} · ${store.t("reconnecting")}"
                                ConnState.IDLE -> store.t("disconnected")
                            }
                        }
                        SectionHeader(store.t("hostSection"))
                        SectionCard {
                            Column(modifier = Modifier.fillMaxWidth()) {
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clickable(onClick = onOpenConnect)
                                        .padding(horizontal = 16.dp, vertical = 14.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                                ) {
                                    Box(modifier = Modifier.size(8.dp).clip(CircleShape).background(hostColor))
                                    Text(hostText, color = p.text, fontSize = PebbleFont.body, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                                    Text(store.t("switchHost"), color = p.mute, fontSize = PebbleFont.secondary)
                                }
                                CardDivider()
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clickable { onOpenDesktop(store.takeovers.firstOrNull()?.target ?: "local") }
                                        .padding(horizontal = 16.dp, vertical = 14.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                                ) {
                                    Icon(painterResource(PebbleIcons.DesktopWindows), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                    val lbl = if (store.takeovers.isEmpty()) store.t("remoteDesktop") else "${store.t("remoteDesktop")} (${store.takeovers.size})"
                                    Text(lbl, color = p.text, fontSize = PebbleFont.body)
                                }
                                CardDivider()
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clickable(onClick = onOpenDevices)
                                        .padding(horizontal = 16.dp, vertical = 14.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                                ) {
                                    Icon(painterResource(PebbleIcons.Devices), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp))
                                    val n = store.nodes.count { it.connected == true }
                                    val lbl = if (n > 0) "${store.t("devicesTunnel")} ($n)" else store.t("devicesTunnel")
                                    Text(lbl, color = p.text, fontSize = PebbleFont.body)
                                }
                            }
                        }
                    }
                }

                Spacer(Modifier.height(16.dp))
            }
        }
    }
}

@Composable
private fun ModelInputWithMenu(
    value: String,
    placeholder: String,
    candidates: List<String>,
    loading: Boolean,
    onLoadCandidates: () -> Unit,
    onRefreshCandidates: () -> Unit = onLoadCandidates,
    onValueChange: (String) -> Unit,
    store: PebbleStore,
) {
    val p = LocalPalette.current
    var menuOpen by remember { mutableStateOf(false) }
    val trimmedValue = value.trim()

    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            placeholder = { Text(placeholder, fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
            singleLine = true,
            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.weight(1f),
        )
        Box {
            IconButton(onClick = {
                menuOpen = true
                if (candidates.isEmpty() && !loading) onLoadCandidates()
            }) {
                Icon(painterResource(PebbleIcons.List), contentDescription = store.t("optionalModels"), tint = p.accent)
            }
            DropdownMenu(
                expanded = menuOpen,
                onDismissRequest = { menuOpen = false },
                containerColor = p.card,
                modifier = Modifier.heightIn(max = 360.dp),
            ) {
                if (trimmedValue.isNotEmpty()) {
                    DropdownMenuItem(
                        text = { Text(store.t("clear"), color = p.danger) },
                        onClick = { onValueChange(""); menuOpen = false },
                    )
                    HorizontalDivider(color = p.s2)
                }
                if (candidates.isEmpty()) {
                    DropdownMenuItem(
                        text = { Text(if (loading) store.t("loading") else store.t("noModels"), color = p.mute, fontSize = PebbleFont.secondary) },
                        enabled = false,
                        onClick = {},
                    )
                    if (!loading) {
                        DropdownMenuItem(
                            text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                            leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                            onClick = onRefreshCandidates,
                        )
                    }
                } else {
                    candidates.forEach { m ->
                        val selected = m == trimmedValue
                        DropdownMenuItem(
                            text = {
                                Text(
                                    text = m,
                                    fontFamily = MonoFont,
                                    fontSize = PebbleFont.secondary,
                                    fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                                    color = if (selected) p.accent else p.text,
                                )
                            },
                            trailingIcon = if (selected) {
                                { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp)) }
                            } else {
                                null
                            },
                            onClick = { onValueChange(m); menuOpen = false },
                        )
                    }
                    HorizontalDivider(color = p.s2)
                    DropdownMenuItem(
                        text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                        leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                        onClick = onRefreshCandidates,
                    )
                }
            }
        }
    }
}

@Composable
private fun GlobalModelInputWithMenu(
    value: String,
    placeholder: String,
    harnesses: List<HarnessInfo>,
    defaultHarness: String,
    harnessLabel: (HarnessInfo) -> String,
    modelsByHarness: Map<String, List<String>>,
    loadingHarnesses: Set<String>,
    onLoadHarness: (String) -> Unit,
    onRefreshHarness: (String) -> Unit,
    onRefreshAll: () -> Unit,
    onValueChange: (String) -> Unit,
    store: PebbleStore,
) {
    val p = LocalPalette.current
    var menuOpen by remember { mutableStateOf(false) }
    var activeHarnessId by remember { mutableStateOf<String?>(null) }
    val trimmedValue = value.trim()

    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            placeholder = { Text(placeholder, fontFamily = MonoFont, fontSize = PebbleFont.secondary) },
            singleLine = true,
            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.weight(1f),
        )
        Box {
            IconButton(onClick = {
                val singleId = if (harnesses.size == 1) harnesses.first().id else null
                activeHarnessId = singleId
                menuOpen = true
                if (singleId != null && modelsByHarness[singleId].isNullOrEmpty() && singleId !in loadingHarnesses) {
                    onLoadHarness(singleId)
                }
            }) {
                Icon(painterResource(PebbleIcons.List), contentDescription = store.t("optionalModels"), tint = p.accent)
            }
            DropdownMenu(
                expanded = menuOpen,
                onDismissRequest = { menuOpen = false; activeHarnessId = null },
                containerColor = p.card,
                modifier = Modifier.heightIn(max = 360.dp),
            ) {
                if (trimmedValue.isNotEmpty()) {
                    DropdownMenuItem(
                        text = { Text(store.t("clear"), color = p.danger) },
                        onClick = { onValueChange(""); menuOpen = false; activeHarnessId = null },
                    )
                    HorizontalDivider(color = p.s2)
                }
                val selectedId = activeHarnessId
                if (selectedId == null) {
                    harnesses.forEach { h ->
                        val count = modelsByHarness[h.id]?.size ?: 0
                        val isLoading = h.id in loadingHarnesses
                        val lbl = if (h.id == defaultHarness) "${harnessLabel(h)} (${store.t("defaultTag")})" else harnessLabel(h)
                        DropdownMenuItem(
                            text = { Text(lbl, color = p.text, fontSize = PebbleFont.secondary) },
                            trailingIcon = {
                                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                                    Text(
                                        text = if (isLoading && count == 0) store.t("loading") else "$count",
                                        color = p.mute,
                                        fontSize = PebbleFont.caption,
                                    )
                                    Icon(painterResource(PebbleIcons.ChevronRight), contentDescription = null, tint = p.mute, modifier = Modifier.size(16.dp))
                                }
                            },
                            onClick = {
                                activeHarnessId = h.id
                                if (modelsByHarness[h.id].isNullOrEmpty() && h.id !in loadingHarnesses) {
                                    onLoadHarness(h.id)
                                }
                            },
                        )
                    }
                    HorizontalDivider(color = p.s2)
                    DropdownMenuItem(
                        text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                        leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                        onClick = onRefreshAll,
                    )
                } else {
                    val activeInfo = harnesses.firstOrNull { it.id == selectedId }
                    if (harnesses.size > 1) {
                        DropdownMenuItem(
                            text = { Text(activeInfo?.let(harnessLabel) ?: selectedId, color = p.text, fontWeight = FontWeight.SemiBold, fontSize = PebbleFont.secondary) },
                            leadingIcon = { Icon(painterResource(PebbleIcons.ArrowBack), contentDescription = null, tint = p.text, modifier = Modifier.size(18.dp)) },
                            onClick = { activeHarnessId = null },
                        )
                        HorizontalDivider(color = p.s2)
                    }
                    val cands = modelsByHarness[selectedId].orEmpty()
                    val isLoading = selectedId in loadingHarnesses
                    if (cands.isEmpty()) {
                        DropdownMenuItem(
                            text = { Text(if (isLoading) store.t("loading") else store.t("noModels"), color = p.mute, fontSize = PebbleFont.secondary) },
                            enabled = false,
                            onClick = {},
                        )
                        if (!isLoading) {
                            DropdownMenuItem(
                                text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                                leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                                onClick = { onRefreshHarness(selectedId) },
                            )
                        }
                    } else {
                        cands.forEach { m ->
                            val selected = m == trimmedValue
                            DropdownMenuItem(
                                text = {
                                    Text(
                                        text = m,
                                        fontFamily = MonoFont,
                                        fontSize = PebbleFont.secondary,
                                        fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
                                        color = if (selected) p.accent else p.text,
                                    )
                                },
                                trailingIcon = if (selected) {
                                    { Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.accent, modifier = Modifier.size(16.dp)) }
                                } else {
                                    null
                                },
                                onClick = { onValueChange(m); menuOpen = false; activeHarnessId = null },
                            )
                        }
                        HorizontalDivider(color = p.s2)
                        DropdownMenuItem(
                            text = { Text(store.t("refresh"), color = p.accent, fontSize = PebbleFont.secondary) },
                            leadingIcon = { Icon(painterResource(PebbleIcons.Refresh), contentDescription = null, tint = p.accent, modifier = Modifier.size(18.dp)) },
                            onClick = { onRefreshHarness(selectedId) },
                        )
                    }
                }
            }
        }
    }
}
