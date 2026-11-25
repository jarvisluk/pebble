package ai.pebble.android.ui.settings

import androidx.annotation.DrawableRes
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
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
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.ui.nav.PebbleRouter
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import kotlinx.coroutines.launch

enum class SettingsGroup(val titleKey: String, val tabs: List<SettingsTab>) {
    APP("settingsGroupApp", listOf(SettingsTab.GENERAL)),
    HOST("settingsGroupHost", listOf(SettingsTab.MODELS, SettingsTab.MEMORY, SettingsTab.DESKTOP, SettingsTab.REMOTE)),
}

enum class SettingsTab(val titleKey: String, @DrawableRes val icon: Int) {
    GENERAL("tabGeneral", PebbleIcons.Settings),
    MODELS("tabModels", PebbleIcons.Memory),
    MEMORY("tabMemory", PebbleIcons.Psychology),
    DESKTOP("tabDesktop", PebbleIcons.DesktopWindows),
    REMOTE("tabRemote", PebbleIcons.Devices),
}

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
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            SettingsGroup.entries.forEach { group ->
                SectionHeader(store.t(group.titleKey))
                SectionCard {
                    group.tabs.forEachIndexed { idx, tab ->
                        if (idx > 0) CardDivider()
                        val active = !compact && selectedTab == tab
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(PebbleDimens.rowShape)
                                .background(if (active) p.sel else p.card)
                                .clickable { onSelectTab(tab) }
                                .padding(horizontal = 16.dp, vertical = 14.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Icon(painterResource(tab.icon), contentDescription = null, tint = if (active) p.text else p.text2, modifier = Modifier.size(20.dp))
                            Text(
                                text = store.t(tab.titleKey),
                                color = if (active) p.text else p.text2,
                                fontSize = PebbleFont.cardTitle,
                                fontWeight = if (active) FontWeight.SemiBold else FontWeight.Medium,
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
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsDetailScreen(
    store: PebbleStore,
    router: PebbleRouter,
    tab: SettingsTab,
    compact: Boolean,
    onBack: () -> Unit,
    onOpenConnect: () -> Unit,
    onOpenCreateBot: () -> Unit,
    onOpenDesktop: (String) -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var saving by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf("") }
    var saved by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf<ConfirmItem?>(null) }

    var showUnsavedAlert by remember { mutableStateOf(false) }
    var alertTargetTab by remember { mutableStateOf<SettingsTab?>(null) }
    var alertLeaveRequested by remember { mutableStateOf(false) }
    var alertToSidebarOnly by remember { mutableStateOf(false) }

    fun clearUnsavedPrompt() {
        showUnsavedAlert = false
        alertTargetTab = null
        alertLeaveRequested = false
        alertToSidebarOnly = false
        router.requestLeaveSettings = false
        router.pendingSettingsTab = null
        router.pendingBackToSidebar = false
    }

    LaunchedEffect(tab) {
        error = ""
        saved = ""
    }

    DisposableEffect(Unit) {
        onDispose { router.clearSettingsDirty() }
    }

    LaunchedEffect(router.requestLeaveSettings) {
        if (router.requestLeaveSettings) {
            alertToSidebarOnly = router.pendingBackToSidebar
            alertLeaveRequested = true
            alertTargetTab = null
            router.requestLeaveSettings = false
            router.pendingBackToSidebar = false
            showUnsavedAlert = true
        }
    }

    LaunchedEffect(router.pendingSettingsTab) {
        val next = router.pendingSettingsTab ?: return@LaunchedEffect
        alertTargetTab = next
        alertLeaveRequested = false
        alertToSidebarOnly = false
        router.pendingSettingsTab = null
        showUnsavedAlert = true
    }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    if (showUnsavedAlert) {
        AlertDialog(
            onDismissRequest = { clearUnsavedPrompt() },
            containerColor = p.card,
            title = { Text(store.t("unsavedTitle"), color = p.text, fontSize = PebbleFont.title, fontWeight = FontWeight.Bold) },
            text = { Text(store.t("unsavedDesc"), color = p.text2, fontSize = PebbleFont.body) },
            confirmButton = {
                val target = alertTargetTab
                val leaving = alertLeaveRequested
                val sidebarOnly = alertToSidebarOnly
                TextButton(onClick = {
                    clearUnsavedPrompt()
                    scope.launch {
                        saving = true
                        val ok = router.settingsSaveAction?.invoke() ?: true
                        saving = false
                        if (!ok) return@launch
                        if (target != null) {
                            router.settingsTab = target
                        } else if (leaving) {
                            if (sidebarOnly) {
                                router.settingsDirty = false
                                router.settingsTab = null
                            } else {
                                router.leaveSettings()
                            }
                        }
                    }
                }) {
                    Text(
                        text = store.t(if (target != null) "saveAndSwitch" else "saveAndLeave"),
                        color = p.accent,
                        fontSize = PebbleFont.body,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
            },
            dismissButton = {
                val target = alertTargetTab
                val leaving = alertLeaveRequested
                val sidebarOnly = alertToSidebarOnly
                Row {
                    TextButton(onClick = {
                        clearUnsavedPrompt()
                        router.settingsDiscardAction?.invoke()
                        router.settingsDirty = false
                        if (target != null) {
                            router.settingsTab = target
                        } else if (leaving) {
                            if (sidebarOnly) {
                                router.settingsTab = null
                            } else {
                                router.leaveSettings()
                            }
                        }
                    }) {
                        Text(store.t("discardChanges"), color = p.danger, fontSize = PebbleFont.body)
                    }
                    TextButton(onClick = { clearUnsavedPrompt() }) {
                        Text(store.t("cancel"), color = p.text2, fontSize = PebbleFont.body)
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
                    if (tab == SettingsTab.MODELS && store.isConnected) {
                        if (saving) {
                            CircularProgressIndicator(modifier = Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp, color = p.accent)
                        } else {
                            TextButton(
                                onClick = {
                                    scope.launch {
                                        saving = true
                                        router.settingsSaveAction?.invoke()
                                        saving = false
                                    }
                                },
                                enabled = router.settingsDirty,
                            ) {
                                Text(
                                    text = store.t("save"),
                                    color = if (router.settingsDirty) p.accent else p.mute,
                                    fontSize = PebbleFont.body,
                                    fontWeight = FontWeight.SemiBold,
                                )
                            }
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

                key(tab) {
                    when (tab) {
                        SettingsTab.GENERAL -> GeneralSettingsTab(store = store)
                        SettingsTab.MODELS -> ModelsSettingsTab(
                            store = store,
                            router = router,
                            onOpenCreateBot = onOpenCreateBot,
                            onError = { error = it },
                            onSaved = { saved = it },
                            onConfirm = { confirm = it },
                        )
                        SettingsTab.MEMORY -> MemorySettingsTab(
                            store = store,
                            router = router,
                            onError = { error = it },
                            onSaved = { saved = it },
                            onConfirm = { confirm = it },
                        )
                        SettingsTab.DESKTOP -> DesktopSettingsTab(
                            store = store,
                            router = router,
                            onOpenDesktop = onOpenDesktop,
                            onError = { error = it },
                            onSaved = { saved = it },
                        )
                        SettingsTab.REMOTE -> NetworkSettingsTab(
                            store = store,
                            onOpenConnect = onOpenConnect,
                            onOpenDesktop = onOpenDesktop,
                            onError = { error = it },
                            onSaved = { saved = it },
                            onConfirm = { confirm = it },
                        )
                    }
                }

                Spacer(Modifier.height(16.dp))
            }
        }
    }
}
