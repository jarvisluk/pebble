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
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import ai.pebble.android.data.StageRef
import ai.pebble.android.data.StageTab
import ai.pebble.android.ui.desktop.BotStageState
import ai.pebble.android.ui.desktop.StageStateOps
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
import ai.pebble.android.ui.bots.BotInfoSheet
import ai.pebble.android.ui.bots.BotListScreen
import ai.pebble.android.ui.bots.CreateBotSheet
import ai.pebble.android.ui.chat.ChatScreen
import ai.pebble.android.ui.connect.ConnectScreen
import ai.pebble.android.ui.desktop.DesktopScreen
import ai.pebble.android.ui.settings.SettingsDetailScreen
import ai.pebble.android.ui.settings.SettingsSidebar
import ai.pebble.android.ui.settings.SettingsTab
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

sealed interface SheetRoute {
    data class Connect(val scan: Boolean = false) : SheetRoute
    data object CreateBot : SheetRoute
    data class BotInfo(val botId: String) : SheetRoute
}

data class DesktopPresentation(
    val target: String,
    val botId: String? = null,
)

class PebbleRouter {
    var selectedBotId by mutableStateOf<String?>(null)
    var sheet by mutableStateOf<SheetRoute?>(null)
    var detailOpen by mutableStateOf(false)
    var desktop by mutableStateOf<DesktopPresentation?>(null)
    var botStages by mutableStateOf<Map<String, BotStageState>>(emptyMap())
    var translate: (String) -> String = { it }
    var inSettings by mutableStateOf(false)
    var settingsTab by mutableStateOf<SettingsTab?>(null)
    var settingsDirty by mutableStateOf(false)
    var requestLeaveSettings by mutableStateOf(false)
    var pendingSettingsTab by mutableStateOf<SettingsTab?>(null)
    var pendingBackToSidebar by mutableStateOf(false)
    var settingsSaveAction: (suspend () -> Boolean)? = null
    var settingsDiscardAction: (() -> Unit)? = null

    fun openBot(id: String) {
        if (sheet is SheetRoute.BotInfo) sheet = null
        leaveSettings()
        selectedBotId = id
        if (desktop != null && desktop?.botId != id) {
            desktop = desktop?.copy(botId = id)
        }
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

    fun requestSelectSettingsTab(target: SettingsTab) {
        if (target == settingsTab) return
        if (settingsDirty) {
            pendingSettingsTab = target
        } else {
            settingsTab = target
        }
    }

    fun requestLeaveSettings(toSidebarOnly: Boolean = false) {
        if (settingsDirty) {
            pendingBackToSidebar = toSidebarOnly
            requestLeaveSettings = true
        } else if (toSidebarOnly) {
            leaveSettingsTab()
        } else {
            leaveSettings()
        }
    }

    fun clearSettingsDirty() {
        settingsDirty = false
        requestLeaveSettings = false
        pendingSettingsTab = null
        pendingBackToSidebar = false
        settingsSaveAction = null
        settingsDiscardAction = null
    }

    fun leaveSettingsTab() {
        settingsTab = null
        clearSettingsDirty()
    }

    fun requestSettingsBack(compact: Boolean) {
        requestLeaveSettings(toSidebarOnly = compact && settingsTab != null)
    }

    fun leaveSettings() {
        inSettings = false
        clearSettingsDirty()
    }

    fun hasUnreadLatestStage(botId: String): Boolean =
        !botStages[botId]?.unreadLatestStageId.isNullOrEmpty()

    fun selectStageTab(botId: String, tabId: String) {
        val st = StageStateOps.ensureNonEmpty(botStages[botId] ?: BotStageState(), newTabTitle = translate("stageNewTab"))
        botStages = botStages + (botId to st.copy(activeTabId = tabId, unreadLatestStageId = null))
    }

    fun closeStageTab(botId: String, tabId: String) {
        val cur = botStages[botId] ?: BotStageState()
        botStages = botStages + (botId to StageStateOps.close(cur, tabId = tabId, newTabTitle = translate("stageNewTab")))
    }

    fun openNewStageTab(botId: String) {
        val cur = botStages[botId] ?: BotStageState()
        val tab = StageStateOps.newTab(title = translate("stageNewTab"))
        botStages = botStages + (botId to StageStateOps.upsert(cur, tab, activate = true))
    }

    fun replaceNewOrOpenStageTab(botId: String, tab: StageTab) {
        val cur = botStages[botId] ?: BotStageState()
        val next = StageStateOps.replaceNewOrOpen(cur, tab).copy(unreadLatestStageId = null)
        botStages = botStages + (botId to next)
    }

    fun stripFileTabs() {
        val updated = mutableMapOf<String, BotStageState>()
        botStages.forEach { (bid, st) ->
            val filtered = st.tabs.filter { it.kind != StageKind.FILES && it.kind != StageKind.FILE }
            if (filtered.size != st.tabs.size) {
                updated[bid] = StageStateOps.ensureNonEmpty(st.copy(tabs = filtered), newTabTitle = translate("stageNewTab"))
            } else {
                updated[bid] = st
            }
        }
        botStages = updated
    }

    fun openStage(botId: String?, tab: StageTab, replaceNew: Boolean = false) {
        val bid = (botId ?: selectedBotId.orEmpty()).trim()
        sheet = null
        leaveSettings()
        detailOpen = false
        if (bid.isNotEmpty() && bid != "__global__") {
            val cur = botStages[bid] ?: BotStageState()
            val next = (if (replaceNew) StageStateOps.replaceNewOrOpen(cur, tab) else StageStateOps.upsert(cur, tab, activate = true))
                .copy(unreadLatestStageId = null)
            botStages = botStages + (bid to next)
            if (selectedBotId != bid) {
                selectedBotId = bid
            }
        } else if (bid == "__global__") {
            val cur = botStages[bid] ?: BotStageState()
            val next = if (replaceNew) StageStateOps.replaceNewOrOpen(cur, tab) else StageStateOps.upsert(cur, tab, activate = true)
            botStages = botStages + (bid to next)
        }
        val target = if (tab.kind == StageKind.DESKTOP) (tab.target?.ifBlank { "local" } ?: "local") else "local"
        val presBotId = bid.takeIf { it.isNotEmpty() && it != "__global__" }
        if (desktop == null || desktop?.botId != presBotId || tab.kind == StageKind.DESKTOP) {
            desktop = DesktopPresentation(target = target, botId = presBotId)
        }
    }

    fun openStageRef(botId: String?, ref: StageRef) {
        val tab = StageStateOps.tabFromRef(
            ref = ref,
            botId = botId,
            filesTitle = translate("stageFilesTab"),
            desktopTitle = translate("stageDesktopTab"),
            newTabTitle = translate("stageNewTab"),
        )
        openStage(botId = botId, tab = tab, replaceNew = false)
    }

    fun openLatestStage(botId: String, latestStage: StageRef?) {
        if (latestStage != null) {
            openStageRef(botId = botId, ref = latestStage)
            return
        }
        val existing = botStages[botId]
        if (existing != null && existing.tabs.isNotEmpty()) {
            botStages = botStages + (botId to existing.copy(unreadLatestStageId = null))
            sheet = null
            leaveSettings()
            detailOpen = false
            if (desktop == null || desktop?.botId != botId) {
                desktop = DesktopPresentation(target = existing.activeTab?.target ?: "local", botId = botId)
            }
            return
        }
        openStage(botId = botId, tab = StageStateOps.newTab(title = translate("stageNewTab")), replaceNew = false)
    }

    fun openDesktop(target: String = "local") {
        val resolved = target.trim().ifEmpty { "local" }
        val tab = StageStateOps.desktopTab(target = resolved, title = translate("stageDesktopTab"))
        openStage(botId = selectedBotId, tab = tab, replaceNew = false)
    }

    fun openFiles(botId: String, path: String? = null) {
        val clean = path.orEmpty().trim()
        val tab = if (clean.isEmpty()) {
            StageStateOps.filesTab(title = translate("stageFilesTab"))
        } else {
            StageStateOps.fileTab(path = clean)
        }
        openStage(botId = botId, tab = tab, replaceNew = false)
    }

    fun handleStageOpen(ev: StageOpenEvent) {
        val bid = ev.botId?.takeIf { it.isNotBlank() } ?: selectedBotId.orEmpty()
        val tab = StageStateOps.tabFromOpenEvent(
            ev = ev,
            filesTitle = translate("stageFilesTab"),
            desktopTitle = translate("stageDesktopTab"),
            newTabTitle = translate("stageNewTab"),
        )
        val inBotStage = desktop != null && (desktop?.botId == bid || bid.isEmpty())
        val shouldActivate = inBotStage || ev.autoOpen == true
        if (bid.isNotEmpty()) {
            val cur = botStages[bid] ?: BotStageState()
            val next = StageStateOps.upsert(cur, tab, activate = shouldActivate)
                .copy(unreadLatestStageId = if (shouldActivate) null else tab.id)
            botStages = botStages + (bid to next)
        }
        if (inBotStage) {
            if (tab.kind == StageKind.DESKTOP) {
                desktop = DesktopPresentation(target = tab.target ?: "local", botId = bid.ifEmpty { null })
            }
        } else if (ev.autoOpen == true) {
            openStage(botId = bid.ifEmpty { null }, tab = tab, replaceNew = false)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RootScaffold(
    store: PebbleStore,
    router: PebbleRouter,
) {
    val p = LocalPalette.current
    SideEffect { router.translate = store::t }
    val canUseFiles = store.supports("stage.files")
    LaunchedEffect(canUseFiles) {
        if (!canUseFiles) router.stripFileTabs()
    }

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
                router.inSettings -> router.requestSettingsBack(compact)
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
                            onBack = { router.requestSettingsBack(compact = true) },
                        )
                    } else {
                        SettingsDetailScreen(
                            store = store,
                            router = router,
                            tab = tab,
                            compact = true,
                            onBack = { router.requestSettingsBack(compact = true) },
                            onOpenConnect = { router.sheet = SheetRoute.Connect(false) },
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
                            onOpenDesktop = { router.openDesktop(it) },
                        )
                    } else {
                        ChatScreen(
                            store = store,
                            botId = bid,
                            compact = true,
                            onBack = { router.closeBot() },
                            onOpenDesktop = { router.openDesktop(it) },
                            onOpenStage = { router.openLatestStage(it, store.bots.firstOrNull { b -> b.id == it }?.latestStage) },
                            onOpenStageRef = { id, ref -> router.openStageRef(id, ref) },
                            hasUnreadLatestStage = router.hasUnreadLatestStage(bid),
                            onToggleDetail = { router.detailOpen = true },
                            onOpenBotInfo = { router.sheet = SheetRoute.BotInfo(bid) },
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
                            onSelectTab = { router.requestSelectSettingsTab(it) },
                            onBack = { router.requestSettingsBack(compact = false) },
                        )
                    }
                    Box(modifier = Modifier.width(0.5.dp).fillMaxHeight().background(p.s2))
                    Box(modifier = Modifier.weight(1f).fillMaxHeight()) {
                        SettingsDetailScreen(
                            store = store,
                            router = router,
                            tab = router.settingsTab ?: SettingsTab.GENERAL,
                            compact = false,
                            onBack = { router.requestSettingsBack(compact = false) },
                            onOpenConnect = { router.sheet = SheetRoute.Connect(false) },
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
                            onCreateBot = { router.sheet = SheetRoute.CreateBot },
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
                                onOpenStage = { router.openLatestStage(it, store.bots.firstOrNull { b -> b.id == it }?.latestStage) },
                                onOpenStageRef = { id, ref -> router.openStageRef(id, ref) },
                                hasUnreadLatestStage = router.hasUnreadLatestStage(selId),
                                onToggleDetail = { router.detailOpen = !router.detailOpen },
                                onOpenBotInfo = { router.sheet = SheetRoute.BotInfo(selId) },
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
                                    Icon(painterResource( PebbleIcons.Forum),
                                        contentDescription = null,
                                        tint = p.mute,
                                        modifier = Modifier.size(44.dp),
                                    )
                                    Text(
                                        text = store.t(if (store.isConnected) "selectBotHint" else "connectHost"),
                                        color = p.text2,
                                        fontSize = PebbleFont.cardTitle,
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
                                onOpenDesktop = { router.openDesktop(it) },
                            )
                        }
                    }
                }

                // Medium tablet (600..839dp): workspace panel opens in a ModalBottomSheet so chat stays legible
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
                            onOpenDesktop = { router.openDesktop(it) },
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
            is SheetRoute.BotInfo -> {
                ModalBottomSheet(
                    onDismissRequest = { router.sheet = null },
                    sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
                    containerColor = p.page,
                ) {
                    Box(modifier = Modifier.fillMaxHeight(0.92f)) {
                        BotInfoSheet(
                            store = store,
                            botId = sheet.botId,
                            onClose = { router.sheet = null },
                            onDeletedBot = {
                                router.sheet = null
                                router.closeBot()
                            },
                            onOpenFiles = {
                                router.sheet = null
                                router.openFiles(it)
                            },
                        )
                    }
                }
            }
            null -> Unit
        }

        // Fullscreen Stage Cover (Side Tabs + Collapsible Bot Chat)
        router.desktop?.let { pres ->
            DesktopScreen(
                store = store,
                router = router,
                initialTarget = pres.target,
                initialBotId = pres.botId ?: router.selectedBotId,
                onClose = { router.desktop = null },
            )
        }
    }
}
