package ai.pebble.android.ui.desktop

import android.graphics.BitmapFactory
import android.util.Base64
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandHorizontally
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkHorizontally
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.rememberModalBottomSheetState
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import ai.pebble.android.data.DesktopTarget
import ai.pebble.android.data.HubException
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.StageKind
import ai.pebble.android.data.errText
import ai.pebble.android.ui.chat.ChatScreen
import ai.pebble.android.ui.nav.PebbleRouter
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

internal enum class DesktopStreamMode { WS_HEVC, WS_H264, FRAME_POLLING }

private fun preferredDesktopStreamMode(store: PebbleStore, target: String): DesktopStreamMode = when {
    !PebbleStore.isLocalDesktopTarget(target) || !store.supports("desktop.ws_h264") -> DesktopStreamMode.FRAME_POLLING
    !store.supports("desktop.ws_hevc") -> DesktopStreamMode.WS_H264
    else -> DesktopStreamMode.WS_HEVC
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DesktopScreen(
    store: PebbleStore,
    router: PebbleRouter,
    initialTarget: String,
    initialBotId: String?,
    onClose: () -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()
    val canUseFiles = store.supports("stage.files")

    var chatBotId by remember(initialBotId) { mutableStateOf(initialBotId.orEmpty()) }
    val resolvedBotId = remember(chatBotId, store.bots) {
        when {
            chatBotId.isNotEmpty() && store.bots.any { it.id == chatBotId } -> chatBotId
            else -> store.bots.firstOrNull()?.id
        }
    }
    val stageBotKey = resolvedBotId.orEmpty().ifEmpty { "__global__" }
    val currentBot = remember(resolvedBotId, store.bots) {
        resolvedBotId?.let { bid -> store.bots.firstOrNull { it.id == bid } }
    }

    val stageState = remember(router.botStages, stageBotKey, store.langRevision, canUseFiles) {
        val raw = router.botStages[stageBotKey] ?: BotStageState()
        val filtered = if (canUseFiles) raw else raw.copy(tabs = raw.tabs.filter { it.kind != StageKind.FILES && it.kind != StageKind.FILE })
        StageStateOps.ensureNonEmpty(filtered, newTabTitle = store.t("stageNewTab"))
    }
    val activeTab = stageState.activeTab
    val activeKind = activeTab?.kind ?: StageKind.NEW

    var target by remember { mutableStateOf(initialTarget.ifEmpty { "local" }) }
    var streaming by remember { mutableStateOf(true) }
    var preset by remember { mutableStateOf("auto") }
    var error by remember { mutableStateOf("") }
    var showText by remember { mutableStateOf(false) }
    var draft by remember { mutableStateOf("") }
    var showChatSidebarWide by remember { mutableStateOf(true) }
    var showChatSidebarCompact by remember { mutableStateOf(false) }
    var showHiddenFiles by remember { mutableStateOf(false) }
    var filesRefreshSeq by remember { mutableIntStateOf(0) }

    var streamMode by remember { mutableStateOf(preferredDesktopStreamMode(store, target)) }
    var fallbackBitmap by remember { mutableStateOf<ImageBitmap?>(null) }
    var frameFps by remember { mutableIntStateOf(0) }
    var streamMeta by remember { mutableStateOf<DesktopStreamMeta?>(null) }
    var streamConnected by remember { mutableStateOf(false) }
    var streamFps by remember { mutableIntStateOf(0) }
    var streamErr by remember { mutableStateOf("") }
    val streamCtrl = remember { H264StreamController() }

    var keyMenuOpen by remember { mutableStateOf(false) }
    var settingsMenuOpen by remember { mutableStateOf(false) }

    LaunchedEffect(activeTab?.id, activeTab?.target) {
        if (activeKind == StageKind.DESKTOP) {
            val tabTarget = activeTab?.target?.trim().orEmpty().ifEmpty { initialTarget.ifEmpty { "local" } }
            if (target != tabTarget) {
                target = tabTarget
            }
        }
    }

    val targets = remember(store.desktopTargets, target) {
        val base = store.desktopTargets.ifEmpty {
            listOf(DesktopTarget(id = "local", name = store.t("localDesktopName"), local = true, supported = true))
        }
        if (target.isNotEmpty() && base.none { it.id == target }) {
            base + DesktopTarget(id = target, name = target, local = !target.contains(":virtual:") || target.startsWith("virtual:"), supported = true)
        } else {
            base
        }
    }

    val activeTakeover = remember(store.takeovers, target) {
        store.takeovers.firstOrNull { it.target == target } ?: store.takeovers.firstOrNull()
    }

    val hasBoundTarget = !currentBot?.desktopTarget.isNullOrEmpty()

    LaunchedEffect(Unit) {
        runCatching { store.refreshDesktopTargets() }
    }

    LaunchedEffect(store.activeDesktopTarget.target, hasBoundTarget) {
        if (hasBoundTarget) return@LaunchedEffect
        val next = store.activeDesktopTarget.target.ifEmpty { "local" }
        if (target != next) target = next
    }

    LaunchedEffect(target) {
        val next = target.ifEmpty { "local" }
        streamMode = preferredDesktopStreamMode(store, next)
        fallbackBitmap = null
        frameFps = 0
        streamConnected = false
        streamErr = ""
        error = ""
        if (store.activeDesktopTarget.target != next) {
            runCatching { store.setActiveDesktopTarget(next) }
        }
    }

    LaunchedEffect(store.serverInfo, store.unsupportedCaps, target) {
        if ((!PebbleStore.isLocalDesktopTarget(target) || !store.supports("desktop.ws_h264")) && streamMode != DesktopStreamMode.FRAME_POLLING) {
            streamMode = DesktopStreamMode.FRAME_POLLING
        }
    }

    val lifecycleOwner = LocalLifecycleOwner.current
    var isStarted by remember(lifecycleOwner) {
        mutableStateOf(lifecycleOwner.lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED))
    }
    DisposableEffect(lifecycleOwner) {
        val obs = LifecycleEventObserver { _, _ ->
            isStarted = lifecycleOwner.lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)
        }
        lifecycleOwner.lifecycle.addObserver(obs)
        onDispose { lifecycleOwner.lifecycle.removeObserver(obs) }
    }

    LaunchedEffect(target, streaming, activeKind, isStarted, streamMode) {
        if (activeKind != StageKind.DESKTOP || !isStarted) return@LaunchedEffect
        delay(80)
        while (isActive && streaming && activeKind == StageKind.DESKTOP && isStarted) {
            runCatching { store.fetchDesktopState(target) }.getOrNull()?.let { st ->
                if (st.status?.screenRecording == "denied") {
                    streaming = false
                    error = store.t("deskScreenRecordingDenied")
                } else if (error == store.t("deskScreenRecordingDenied")) {
                    error = ""
                }
            }
            val ready = streamConnected || (streamMode == DesktopStreamMode.FRAME_POLLING && fallbackBitmap != null)
            delay(if (ready) 10_000 else 2000)
        }
    }

    LaunchedEffect(target, streaming, activeKind, isStarted, streamMode) {
        if (activeKind != StageKind.DESKTOP || !streaming || !isStarted || streamMode != DesktopStreamMode.FRAME_POLLING) {
            return@LaunchedEffect
        }
        var consecErrors = 0
        var windowFrames = 0
        var fpsWindowStartMs = System.currentTimeMillis()
        while (isActive && streaming && activeKind == StageKind.DESKTOP && isStarted && streamMode == DesktopStreamMode.FRAME_POLLING) {
            val t0 = System.currentTimeMillis()
            var sleepMs = 350L
            runCatching {
                val fr = store.fetchDesktopFrame(target)
                val bmp = withContext(Dispatchers.IO) {
                    if (fr.jpegBase64.isEmpty()) return@withContext null
                    val raw = Base64.decode(fr.jpegBase64, Base64.DEFAULT)
                    BitmapFactory.decodeByteArray(raw, 0, raw.size)?.asImageBitmap()
                }
                fr to bmp
            }.onSuccess { (fr, bmp) ->
                if (!isActive || !streaming || activeKind != StageKind.DESKTOP || !isStarted || streamMode != DesktopStreamMode.FRAME_POLLING) {
                    return@onSuccess
                }
                if (bmp != null) {
                    val iw = (fr.imageWidth ?: 0.0).let { if (it > 0) it else bmp.width.toDouble() }
                    val ih = (fr.imageHeight ?: 0.0).let { if (it > 0) it else bmp.height.toDouble() }
                    streamMeta = DesktopStreamMeta(
                        imageWidth = iw,
                        imageHeight = ih,
                        logicalX = fr.logicalX ?: 0.0,
                        logicalY = fr.logicalY ?: 0.0,
                        logicalWidth = (fr.logicalWidth ?: 0.0).let { if (it > 0) it else 1440.0 },
                        logicalHeight = (fr.logicalHeight ?: 0.0).let { if (it > 0) it else 900.0 },
                        preset = "frame",
                        bitrateKbps = 0,
                    )
                    fallbackBitmap = bmp
                    consecErrors = 0
                    windowFrames += 1
                    val now = System.currentTimeMillis()
                    val elapsedWin = now - fpsWindowStartMs
                    if (elapsedWin >= 1000L) {
                        frameFps = maxOf(1, (windowFrames * 1000L / elapsedWin).toInt())
                        windowFrames = 0
                        fpsWindowStartMs = now
                    }
                    if (error.isNotEmpty() && error != store.t("deskScreenRecordingDenied")) error = ""
                }
                val elapsed = System.currentTimeMillis() - t0
                sleepMs = maxOf(120L, 350L - elapsed)
            }.onFailure { err ->
                if (!isActive) return@onFailure
                if (err is HubException && err.code == "screen_recording_denied") {
                    streaming = false
                    error = store.t("deskScreenRecordingDenied")
                    return@LaunchedEffect
                }
                consecErrors += 1
                if (fallbackBitmap == null && consecErrors >= 3) {
                    error = errText(err, store.t("connFailed"))
                }
                sleepMs = 1200L
            }
            delay(sleepMs)
        }
    }

    fun send(body: Map<String, Any?>) {
        if (streamConnected) {
            streamCtrl.sendInput(body)
            return
        }
        val payload = body.toMutableMap()
        payload["target"] = target
        scope.launch {
            runCatching { store.sendDesktopInput(payload) }
                .onFailure { error = errText(it, store.t("opFailed")) }
        }
    }

    fun sendText() {
        if (draft.isEmpty()) return
        val txt = draft
        draft = ""
        send(mapOf("kind" to "type", "text" to txt))
    }

    val wsCodec = if (streamMode == DesktopStreamMode.WS_HEVC) "hevc" else "h264"
    val wsUrl = if (streamMode == DesktopStreamMode.FRAME_POLLING) "" else store.makeDesktopWsUrl(target, codec = wsCodec).orEmpty()
    val displayErr = error.ifEmpty { if (streamMode == DesktopStreamMode.FRAME_POLLING) "" else streamErr }

    BoxWithConstraints(
        modifier = Modifier
            .fillMaxSize()
            .background(p.page),
    ) {
        val isWide = maxWidth >= 840.dp
        val showChatSidebar = if (isWide) showChatSidebarWide else showChatSidebarCompact

        if (!isWide && showChatSidebarCompact) {
            ModalBottomSheet(
                onDismissRequest = { showChatSidebarCompact = false },
                sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
                containerColor = p.card,
            ) {
                Box(modifier = Modifier.heightIn(min = 380.dp, max = 660.dp)) {
                    if (resolvedBotId != null) {
                        ChatScreen(
                            store = store,
                            botId = resolvedBotId,
                            compact = true,
                            inDesktopSidebar = true,
                            onSelectBot = {
                                chatBotId = it
                                router.openBot(it)
                            },
                            onOpenStageRef = { bid, ref ->
                                router.openStageRef(bid, ref)
                                showChatSidebarCompact = false
                            },
                            onCloseSidebar = { showChatSidebarCompact = false },
                        )
                    }
                }
            }
        }

        Scaffold(
            modifier = Modifier.fillMaxSize(),
            containerColor = p.page,
            topBar = {
                TopAppBar(
                    colors = TopAppBarDefaults.topAppBarColors(
                        containerColor = p.page,
                        titleContentColor = p.text,
                    ),
                    navigationIcon = {
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(4.dp),
                        ) {
                            IconButton(onClick = onClose, modifier = Modifier.size(40.dp)) {
                                Icon(painterResource(PebbleIcons.ArrowBackIosNew), contentDescription = store.t("close"), tint = p.text, modifier = Modifier.size(20.dp))
                            }
                            IconButton(
                                onClick = {
                                    if (isWide) showChatSidebarWide = !showChatSidebarWide
                                    else showChatSidebarCompact = !showChatSidebarCompact
                                },
                                modifier = Modifier
                                    .size(36.dp)
                                    .clip(CircleShape)
                                    .background(if (showChatSidebar) p.s1 else Color.Transparent),
                            ) {
                                Icon(painterResource(PebbleIcons.Chat), contentDescription = store.t("deskChatSidebar"), tint = p.text, modifier = Modifier.size(20.dp))
                            }
                        }
                    },
                    title = {
                        Text(
                            text = currentBot?.displayName ?: store.t("stageLatestTab"),
                            color = p.text,
                            fontSize = PebbleFont.cardTitle,
                            fontWeight = FontWeight.SemiBold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    },
                    actions = {
                        when (activeKind) {
                            StageKind.FILES -> {
                                IconButton(onClick = { showHiddenFiles = !showHiddenFiles }, modifier = Modifier.size(36.dp)) {
                                    Icon(
                                        painter = painterResource(if (showHiddenFiles) PebbleIcons.Visibility else PebbleIcons.VisibilityOff),
                                        contentDescription = store.t("showHiddenFiles"),
                                        tint = p.text,
                                        modifier = Modifier.size(20.dp),
                                    )
                                }
                                IconButton(onClick = { filesRefreshSeq += 1 }, modifier = Modifier.size(36.dp)) {
                                    Icon(painterResource(PebbleIcons.Refresh), contentDescription = store.t("refreshFiles"), tint = p.text, modifier = Modifier.size(20.dp))
                                }
                            }
                            StageKind.FILE -> {
                                IconButton(onClick = { filesRefreshSeq += 1 }, modifier = Modifier.size(36.dp)) {
                                    Icon(painterResource(PebbleIcons.Refresh), contentDescription = store.t("refreshFiles"), tint = p.text, modifier = Modifier.size(20.dp))
                                }
                            }
                            StageKind.DESKTOP -> {
                                IconButton(onClick = { streaming = !streaming }, modifier = Modifier.size(36.dp)) {
                                    Icon(
                                        painter = painterResource(if (streaming) PebbleIcons.Pause else PebbleIcons.PlayArrow),
                                        contentDescription = store.t(if (streaming) "pause" else "resume"),
                                        tint = p.text,
                                        modifier = Modifier.size(20.dp),
                                    )
                                }
                                Box {
                                    IconButton(onClick = { keyMenuOpen = true }, modifier = Modifier.size(36.dp)) {
                                        Icon(painterResource(PebbleIcons.Keyboard), contentDescription = store.t("aaInput"), tint = p.text, modifier = Modifier.size(20.dp))
                                    }
                                    DropdownMenu(
                                        expanded = keyMenuOpen,
                                        onDismissRequest = { keyMenuOpen = false },
                                        containerColor = p.card,
                                    ) {
                                        DropdownMenuItem(
                                            text = { Text(store.t("aaInput"), color = p.text) },
                                            trailingIcon = {
                                                if (showText) Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.text, modifier = Modifier.size(16.dp))
                                            },
                                            onClick = {
                                                showText = !showText
                                                keyMenuOpen = false
                                            },
                                        )
                                        HorizontalDivider(color = p.s2)
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyEnter"), color = p.text) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Return")) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyBackspace"), color = p.text) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Backspace")) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("hotkeyEsc"), color = p.text) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Escape")) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("hotkeyTab"), color = p.text) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Tab")) },
                                        )
                                        HorizontalDivider(color = p.s2)
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyScrollUp"), color = p.text) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "scroll", "deltaY" to 8)) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyScrollDown"), color = p.text) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "scroll", "deltaY" to -8)) },
                                        )
                                    }
                                }
                                Box {
                                    IconButton(onClick = { settingsMenuOpen = true }, modifier = Modifier.size(36.dp)) {
                                        Icon(painterResource(PebbleIcons.Tune), contentDescription = store.t("settings"), tint = p.text, modifier = Modifier.size(20.dp))
                                    }
                                    DropdownMenu(
                                        expanded = settingsMenuOpen,
                                        onDismissRequest = { settingsMenuOpen = false },
                                        containerColor = p.card,
                                    ) {
                                        if (streamMode == DesktopStreamMode.FRAME_POLLING && fallbackBitmap != null) {
                                            DropdownMenuItem(
                                                text = { Text(store.t("deskFrameModeLabel", mapOf("fps" to "$frameFps")), color = p.mute, fontSize = PebbleFont.secondary) },
                                                enabled = false,
                                                onClick = {},
                                            )
                                            HorizontalDivider(color = p.s2)
                                        } else if (streamConnected) {
                                            val codecLabel = if (streamMode == DesktopStreamMode.WS_HEVC) "HEVC" else "H.264"
                                            DropdownMenuItem(
                                                text = { Text("$codecLabel · $streamFps fps", color = p.mute, fontSize = PebbleFont.secondary) },
                                                enabled = false,
                                                onClick = {},
                                            )
                                            HorizontalDivider(color = p.s2)
                                        }
                                        val presets = listOf(
                                            "auto" to store.t("deskPresetAuto"),
                                            "ultra" to store.t("deskPresetUltra"),
                                            "high" to store.t("deskPresetHigh"),
                                            "medium" to store.t("deskPresetMedium"),
                                            "low" to store.t("deskPresetLow"),
                                        )
                                        presets.forEach { (id, lbl) ->
                                            DropdownMenuItem(
                                                text = { Text(lbl, color = p.text) },
                                                trailingIcon = {
                                                    if (preset == id) Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.text, modifier = Modifier.size(16.dp))
                                                },
                                                onClick = {
                                                    preset = id
                                                    settingsMenuOpen = false
                                                },
                                            )
                                        }
                                        if (targets.size > 1) {
                                            HorizontalDivider(color = p.s2)
                                            targets.forEach { tr ->
                                                DropdownMenuItem(
                                                    text = { Text(tr.name ?: tr.id, color = p.text) },
                                                    trailingIcon = {
                                                        if (target == tr.id) Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = p.text, modifier = Modifier.size(16.dp))
                                                    },
                                                    onClick = {
                                                        target = tr.id
                                                        settingsMenuOpen = false
                                                    },
                                                )
                                            }
                                        }
                                    }
                                }
                            }
                            else -> Unit
                        }
                    },
                )
            },
        ) { innerPadding ->
            Row(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .imePadding()
                    .navigationBarsPadding(),
            ) {
                AnimatedVisibility(
                    visible = isWide && showChatSidebarWide && resolvedBotId != null,
                    enter = expandHorizontally(tween(240, easing = FastOutSlowInEasing), expandFrom = Alignment.Start) +
                        slideInHorizontally(tween(240, easing = FastOutSlowInEasing)) { -it / 3 } +
                        fadeIn(tween(200)),
                    exit = shrinkHorizontally(tween(220, easing = FastOutSlowInEasing), shrinkTowards = Alignment.Start) +
                        slideOutHorizontally(tween(220, easing = FastOutSlowInEasing)) { -it / 3 } +
                        fadeOut(tween(160)),
                ) {
                    if (resolvedBotId != null) {
                        Surface(
                            color = p.card,
                            shape = RoundedCornerShape(20.dp),
                            modifier = Modifier
                                .padding(start = 12.dp, top = 12.dp, bottom = 12.dp)
                                .width(340.dp)
                                .fillMaxHeight(),
                        ) {
                            ChatScreen(
                                store = store,
                                botId = resolvedBotId,
                                compact = false,
                                inDesktopSidebar = true,
                                onSelectBot = {
                                    chatBotId = it
                                    router.openBot(it)
                                },
                                onOpenStageRef = { bid, ref ->
                                    router.openStageRef(bid, ref)
                                },
                                onCloseSidebar = null,
                            )
                        }
                    }
                }

                Column(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxHeight(),
                ) {
                    StageTabBar(
                        store = store,
                        tabs = stageState.tabs,
                        activeTabId = activeTab?.id.orEmpty(),
                        onSelect = { tabId -> router.selectStageTab(stageBotKey, tabId) },
                        onClose = { tabId -> router.closeStageTab(stageBotKey, tabId) },
                        onNewTab = { router.openNewStageTab(stageBotKey) },
                    )

                    Box(
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxSize(),
                    ) {
                        when (activeKind) {
                            StageKind.NEW -> StageNewTabView(
                                store = store,
                                botId = resolvedBotId.orEmpty(),
                                canUseFiles = canUseFiles,
                                inWideStage = isWide,
                                onOpenFiles = {
                                    router.replaceNewOrOpenStageTab(
                                        stageBotKey,
                                        StageStateOps.filesTab(title = store.t("stageFilesTab")),
                                    )
                                },
                                onOpenDesktop = {
                                    val preferredTarget = currentBot?.desktopTarget?.takeIf { it.isNotBlank() } ?: target
                                    router.replaceNewOrOpenStageTab(
                                        stageBotKey,
                                        StageStateOps.desktopTab(target = preferredTarget, title = store.t("stageDesktopTab")),
                                    )
                                },
                                onOpenTask = { task ->
                                    router.replaceNewOrOpenStageTab(
                                        stageBotKey,
                                        StageStateOps.taskTab(task),
                                    )
                                },
                            )
                            StageKind.FILES -> WorkspaceFilesView(
                                store = store,
                                botId = resolvedBotId.orEmpty(),
                                showHidden = showHiddenFiles,
                                refreshTrigger = filesRefreshSeq,
                                inWideStage = isWide,
                                onOpenFile = { relPath ->
                                    router.openStage(
                                        botId = stageBotKey,
                                        tab = StageStateOps.fileTab(path = relPath),
                                        replaceNew = false,
                                    )
                                },
                            )
                            StageKind.FILE -> StageFilePreviewView(
                                store = store,
                                botId = resolvedBotId.orEmpty(),
                                path = activeTab?.path.orEmpty(),
                                refreshTrigger = filesRefreshSeq,
                                inWideStage = isWide,
                                onQuotePath = { relPath ->
                                    store.insertIntoChat("@$relPath ")
                                    if (isWide) showChatSidebarWide = true else showChatSidebarCompact = true
                                },
                                onCloseTab = {
                                    activeTab?.id?.let { router.closeStageTab(stageBotKey, it) }
                                },
                            )
                            StageKind.DESKTOP -> StageDesktopCanvas(
                                store = store,
                                target = target,
                                preset = preset,
                                streaming = streaming,
                                isStarted = isStarted,
                                streamMode = streamMode,
                                wsUrl = wsUrl,
                                fallbackBitmap = fallbackBitmap,
                                streamMeta = streamMeta,
                                streamConnected = streamConnected,
                                streamCtrl = streamCtrl,
                                displayErr = displayErr,
                                activeTakeover = activeTakeover,
                                showText = showText,
                                draft = draft,
                                inWideStage = isWide,
                                onDraftChange = { draft = it },
                                onCloseText = { showText = false },
                                onSendText = ::sendText,
                                onSendInput = ::send,
                                onSwitchBackLocal = {
                                    target = "local"
                                    scope.launch { runCatching { store.setActiveDesktopTarget("local") } }
                                },
                                onResolveTakeover = { id ->
                                    scope.launch { runCatching { store.resolveTakeover(id) } }
                                },
                                onMeta = { streamMeta = it },
                                onStreamStatus = { conn, fps ->
                                    streamConnected = conn
                                    streamFps = fps
                                    if (conn) streamErr = ""
                                },
                                onStreamError = { code, msg ->
                                    if (code == "screen_recording_denied") {
                                        streaming = false
                                        error = store.t("deskScreenRecordingDenied")
                                    } else if (!streamConnected) {
                                        streamErr = ""
                                        streamMode = when (streamMode) {
                                            DesktopStreamMode.WS_HEVC -> DesktopStreamMode.WS_H264
                                            DesktopStreamMode.WS_H264 -> DesktopStreamMode.FRAME_POLLING
                                            DesktopStreamMode.FRAME_POLLING -> DesktopStreamMode.FRAME_POLLING
                                        }
                                    } else {
                                        streamErr = msg
                                    }
                                },
                                onRetryConnect = {
                                    error = ""
                                    streamErr = ""
                                    fallbackBitmap = null
                                    streamMode = preferredDesktopStreamMode(store, target)
                                    streaming = false
                                    streaming = true
                                },
                            )
                            StageKind.TASK -> StageTaskView(
                                store = store,
                                botId = resolvedBotId.orEmpty(),
                                taskId = activeTab?.taskId.orEmpty().ifEmpty { activeTab?.id?.removePrefix("task:").orEmpty() },
                                inWideStage = isWide,
                                onOpenDesktop = { dest ->
                                    target = dest
                                    router.openDesktop(dest)
                                },
                            )
                            StageKind.BROWSER, StageKind.ARTIFACT, StageKind.MCP_APP -> {
                                if (activeTab != null) {
                                    StageExtensionPane(
                                        store = store,
                                        tab = activeTab,
                                        inWideStage = isWide,
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
