package ai.pebble.android.ui.nav

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Forum
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.ConnState
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.StageKind
import ai.pebble.android.data.StageOpenEvent
import ai.pebble.android.ui.bots.BotDetailScreen
import ai.pebble.android.ui.bots.BotListScreen
import ai.pebble.android.ui.bots.CreateBotSheet
import ai.pebble.android.ui.chat.ChatScreen
import ai.pebble.android.ui.connect.ConnectScreen
import ai.pebble.android.ui.desktop.DesktopScreen
import ai.pebble.android.ui.devices.DevicesSheet
import ai.pebble.android.ui.settings.SettingsDetailScreen
import ai.pebble.android.ui.settings.SettingsSidebar
import ai.pebble.android.ui.settings.SettingsTab
import ai.pebble.android.ui.theme.LocalPalette

sealed interface SheetRoute {
    data class Connect(val scan: Boolean = false) : SheetRoute
    data object CreateBot : SheetRoute
    data object Devices : SheetRoute
}

data class DesktopPresentation(
    val target: String,
    val initialTab: StageKind = StageKind.DESKTOP,
    val botId: String? = null,
    val initialPath: String? = null,
)

class PebbleRouter {
    var selectedBotId by mutableStateOf<String?>(null)
    var sheet by mutableStateOf<SheetRoute?>(null)
    var detailOpen by mutableStateOf(false)
    var desktop by mutableStateOf<DesktopPresentation?>(null)
    var inSettings by mutableStateOf(false)
    var settingsTab by mutableStateOf<SettingsTab?>(null)
    var settingsDirty by mutableStateOf(false)
    var requestLeaveSettings by mutableStateOf(false)

    fun openBot(id: String) {
        inSettings = false
        selectedBotId = id
    }

    fun closeBot() {
        detailOpen = false
        selectedBotId = null
    }

    fun openSettings(compact: Boolean) {
        detailOpen = false
        inSettings = true
        settingsTab = if (compact) null else SettingsTab.GENERAL
    }

    fun leaveSettings() {
        inSettings = false
        settingsDirty = false
        requestLeaveSettings = false
    }

    fun openDesktop(target: String = "local") {
        val resolved = target.trim().ifEmpty { "local" }
        sheet = null
        desktop = DesktopPresentation(target = resolved, initialTab = StageKind.DESKTOP, botId = selectedBotId)
    }

    fun openFiles(botId: String, path: String? = null) {
        sheet = null
        desktop = DesktopPresentation(target = "local", initialTab = StageKind.FILES, botId = botId, initialPath = path)
    }

    fun handleStageOpen(ev: StageOpenEvent) {
        val targetBot = ev.botId?.takeIf { it.isNotEmpty() } ?: selectedBotId
        if (targetBot != null && targetBot != selectedBotId) {
            openBot(targetBot)
        }
        val kind = StageKind.fromRaw(ev.kind)
        sheet = null
        desktop = DesktopPresentation(target = "local", initialTab = kind, botId = targetBot, initialPath = ev.path)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RootScaffold(
    store: PebbleStore,
    router: PebbleRouter,
) {
    val p = LocalPalette.current

    val showGate = store.activeServer == null ||
        store.connState == ConnState.UNAUTHORIZED ||
        (store.connState == ConnState.ERROR && store.bots.isEmpty()) ||
        (store.connState == ConnState.CONNECTING && store.bots.isEmpty())

    LaunchedEffect(showGate) {
        if (showGate) {
            router.sheet = null
            router.desktop = null
            router.closeBot()
        }
    }

    LaunchedEffect(store.pendingStageOpen) {
        val ev = store.pendingStageOpen ?: return@LaunchedEffect
        router.handleStageOpen(ev)
        store.consumePendingStageOpen()
    }

    LaunchedEffect(store.bots) {
        val sel = router.selectedBotId
        if (sel != null && store.bots.isNotEmpty() && store.bots.none { it.id == sel }) {
            router.closeBot()
        }
    }

    BoxWithConstraints(
        modifier = Modifier
            .fillMaxSize()
            .background(p.page),
    ) {
        val compact = maxWidth < 600.dp
        val hasInspectorSpace = maxWidth >= 1000.dp
        val sidebarWidth = if (maxWidth >= 1000.dp) 280.dp else 260.dp

        BackHandler(
            enabled = !showGate && (
                router.desktop != null ||
                    router.sheet != null ||
                    router.detailOpen ||
                    router.inSettings ||
                    (compact && router.selectedBotId != null)
                ),
        ) {
            when {
                router.desktop != null -> router.desktop = null
                router.sheet != null -> router.sheet = null
                router.detailOpen -> router.detailOpen = false
                router.inSettings -> {
                    if (compact && router.settingsTab != null) {
                        if (router.settingsDirty) router.requestLeaveSettings = true
                        else router.settingsTab = null
                    } else {
                        if (router.settingsDirty) router.requestLeaveSettings = true
                        else router.leaveSettings()
                    }
                }
                compact && router.selectedBotId != null -> router.closeBot()
            }
        }

        if (showGate) {
            ConnectScreen(
                store = store,
                isGate = true,
                startScan = false,
            )
        } else if (compact) {
            // Compact (<600dp): single-column stack
            when {
                router.inSettings -> {
                    val tab = router.settingsTab
                    if (tab == null) {
                        SettingsSidebar(
                            store = store,
                            compact = true,
                            selectedTab = null,
                            onSelectTab = { router.settingsTab = it },
                            onBack = { router.leaveSettings() },
                        )
                    } else {
                        SettingsDetailScreen(
                            store = store,
                            tab = tab,
                            compact = true,
                            requestLeave = router.requestLeaveSettings,
                            onClearRequestLeave = { router.requestLeaveSettings = false },
                            onDirtyChanged = { router.settingsDirty = it },
                            onBack = {
                                if (router.settingsDirty) router.requestLeaveSettings = true
                                else router.settingsTab = null
                            },
                            onLeaveSettings = {
                                router.leaveSettings()
                                router.settingsTab = null
                            },
                            onOpenConnect = { router.sheet = SheetRoute.Connect(false) },
                            onOpenDevices = { router.sheet = SheetRoute.Devices },
                            onOpenCreateBot = { router.sheet = SheetRoute.CreateBot },
                            onOpenDesktop = { router.openDesktop(it) },
                        )
                    }
                }
                router.selectedBotId != null -> {
                    val bid = router.selectedBotId!!
                    if (router.detailOpen) {
                        BotDetailScreen(
                            store = store,
                            botId = bid,
                            onClose = { router.detailOpen = false },
                            onDeletedBot = { router.closeBot() },
                            onOpenDesktop = { router.openDesktop(it) },
                            onOpenFiles = { router.openFiles(it) },
                        )
                    } else {
                        ChatScreen(
                            store = store,
                            botId = bid,
                            compact = true,
                            onBack = { router.closeBot() },
                            onOpenDesktop = { router.openDesktop(it) },
                            onOpenFiles = { router.openFiles(it) },
                            onToggleDetail = { router.detailOpen = true },
                        )
                    }
                }
                else -> {
                    BotListScreen(
                        store = store,
                        selectedBotId = router.selectedBotId,
                        compact = true,
                        onSelectBot = { router.openBot(it) },
                        onDeletedBot = { if (router.selectedBotId == it) router.closeBot() },
                        onOpenSettings = { router.openSettings(compact = true) },
                        onOpenDesktop = { router.openDesktop(it) },
                    )
                }
            }
        } else {
            // Wide (>=600dp): Sidebar + Main (+ Inspector when >=840dp)
            if (router.inSettings) {
                Row(modifier = Modifier.fillMaxSize()) {
                    Box(modifier = Modifier.width(sidebarWidth).fillMaxHeight()) {
                        SettingsSidebar(
                            store = store,
                            compact = false,
                            selectedTab = router.settingsTab ?: SettingsTab.GENERAL,
                            onSelectTab = { router.settingsTab = it },
                            onBack = {
                                if (router.settingsDirty) router.requestLeaveSettings = true
                                else router.leaveSettings()
                            },
                        )
                    }
                    Box(modifier = Modifier.width(0.5.dp).fillMaxHeight().background(p.s2))
                    Box(modifier = Modifier.weight(1f).fillMaxHeight()) {
                        SettingsDetailScreen(
                            store = store,
                            tab = router.settingsTab ?: SettingsTab.GENERAL,
                            compact = false,
                            requestLeave = router.requestLeaveSettings,
                            onClearRequestLeave = { router.requestLeaveSettings = false },
                            onDirtyChanged = { router.settingsDirty = it },
                            onBack = {
                                if (router.settingsDirty) router.requestLeaveSettings = true
                                else router.leaveSettings()
                            },
                            onLeaveSettings = { router.leaveSettings() },
                            onOpenConnect = { router.sheet = SheetRoute.Connect(false) },
                            onOpenDevices = { router.sheet = SheetRoute.Devices },
                            onOpenCreateBot = { router.sheet = SheetRoute.CreateBot },
                            onOpenDesktop = { router.openDesktop(it) },
                        )
                    }
                }
            } else {
                Row(modifier = Modifier.fillMaxSize()) {
                    Box(modifier = Modifier.width(sidebarWidth).fillMaxHeight()) {
                        BotListScreen(
                            store = store,
                            selectedBotId = router.selectedBotId,
                            compact = false,
                            onSelectBot = { router.openBot(it) },
                            onDeletedBot = { if (router.selectedBotId == it) router.closeBot() },
                            onOpenSettings = { router.openSettings(compact = false) },
                            onOpenDesktop = { router.openDesktop(it) },
                        )
                    }
                    Box(modifier = Modifier.width(0.5.dp).fillMaxHeight().background(p.s2))
                    Box(modifier = Modifier.weight(1f).fillMaxHeight()) {
                        val selId = router.selectedBotId
                        if (selId != null && store.bots.any { it.id == selId }) {
                            ChatScreen(
                                store = store,
                                botId = selId,
                                compact = false,
                                onOpenDesktop = { router.openDesktop(it) },
                                onOpenFiles = { router.openFiles(it) },
                                onToggleDetail = { router.detailOpen = !router.detailOpen },
                            )
                        } else {
                            Box(
                                modifier = Modifier
                                    .fillMaxSize()
                                    .background(p.page),
                                contentAlignment = Alignment.Center,
                            ) {
                                Column(
                                    horizontalAlignment = Alignment.CenterHorizontally,
                                    verticalArrangement = Arrangement.spacedBy(10.dp),
                                ) {
                                    Icon(
                                        Icons.Rounded.Forum,
                                        contentDescription = null,
                                        tint = p.mute,
                                        modifier = Modifier.size(44.dp),
                                    )
                                    Text(
                                        text = store.t(if (store.isConnected) "selectBotHint" else "connectHost"),
                                        color = p.text2,
                                        fontSize = 16.sp,
                                        fontWeight = FontWeight.Medium,
                                    )
                                }
                            }
                        }
                    }

                    val selId = router.selectedBotId
                    if (hasInspectorSpace && router.detailOpen && selId != null && store.bots.any { it.id == selId }) {
                        Box(modifier = Modifier.width(0.5.dp).fillMaxHeight().background(p.s2))
                        Box(modifier = Modifier.width(340.dp).fillMaxHeight()) {
                            BotDetailScreen(
                                store = store,
                                botId = selId,
                                onClose = { router.detailOpen = false },
                                onDeletedBot = { router.closeBot() },
                                onOpenDesktop = { router.openDesktop(it) },
                                onOpenFiles = { router.openFiles(it) },
                            )
                        }
                    }
                }

                // Medium tablet (600..839dp): detail opens in a ModalBottomSheet so chat stays legible
                val selId = router.selectedBotId
                if (!hasInspectorSpace && router.detailOpen && selId != null && store.bots.any { it.id == selId }) {
                    ModalBottomSheet(
                        onDismissRequest = { router.detailOpen = false },
                        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
                        containerColor = p.page,
                    ) {
                        BotDetailScreen(
                            store = store,
                            botId = selId,
                            onClose = { router.detailOpen = false },
                            onDeletedBot = { router.closeBot() },
                            onOpenDesktop = { router.openDesktop(it) },
                            onOpenFiles = { router.openFiles(it) },
                        )
                    }
                }
            }
        }

        // Modal Sheets (Connect / CreateBot / Devices)
        when (val sheet = router.sheet) {
            is SheetRoute.Connect -> {
                ModalBottomSheet(
                    onDismissRequest = { router.sheet = null },
                    sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
                    containerColor = p.page,
                ) {
                    ConnectScreen(
                        store = store,
                        isGate = false,
                        startScan = sheet.scan,
                        onClose = { router.sheet = null },
                    )
                }
            }
            SheetRoute.CreateBot -> {
                ModalBottomSheet(
                    onDismissRequest = { router.sheet = null },
                    sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
                    containerColor = p.page,
                ) {
                    CreateBotSheet(
                        store = store,
                        onClose = { router.sheet = null },
                        onCreated = { newId ->
                            router.sheet = null
                            router.openBot(newId)
                        },
                    )
                }
            }
            SheetRoute.Devices -> {
                ModalBottomSheet(
                    onDismissRequest = { router.sheet = null },
                    sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
                    containerColor = p.page,
                ) {
                    DevicesSheet(
                        store = store,
                        onClose = { router.sheet = null },
                        onManageConnections = { router.sheet = SheetRoute.Connect(false) },
                        onOpenDesktop = { router.openDesktop(it) },
                    )
                }
            }
            null -> Unit
        }

        // Fullscreen Stage Cover (Remote Desktop + Workspace Files)
        router.desktop?.let { pres ->
            DesktopScreen(
                store = store,
                initialTarget = pres.target,
                initialBotId = pres.botId ?: router.selectedBotId,
                initialTab = pres.initialTab,
                initialPath = pres.initialPath,
                onClose = { router.desktop = null },
            )
        }
    }
}
