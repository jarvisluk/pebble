package ai.pebble.android.ui.desktop

import android.app.Activity
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
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
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
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.view.WindowCompat
import ai.pebble.android.data.DesktopTarget
import ai.pebble.android.data.HubException
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.StageKind
import ai.pebble.android.data.errText
import ai.pebble.android.ui.chat.ChatScreen
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlin.math.hypot
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

private enum class DesktopStreamMode { WS_HEVC, WS_H264, FRAME_POLLING }

private fun preferredDesktopStreamMode(store: PebbleStore, target: String): DesktopStreamMode = when {
    !PebbleStore.isLocalDesktopTarget(target) || !store.supports("desktop.ws_h264") -> DesktopStreamMode.FRAME_POLLING
    !store.supports("desktop.ws_hevc") -> DesktopStreamMode.WS_H264
    else -> DesktopStreamMode.WS_HEVC
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DesktopScreen(
    store: PebbleStore,
    initialTarget: String,
    initialBotId: String?,
    initialTab: StageKind = StageKind.DESKTOP,
    initialPath: String? = null,
    onClose: () -> Unit,
) {
    val parentPalette = LocalPalette.current
    val view = LocalView.current
    val scope = rememberCoroutineScope()

    val canUseFiles = store.supports("stage.files")
    var activeTab by remember(initialTab, canUseFiles) {
        mutableStateOf(if (!canUseFiles && initialTab == StageKind.FILES) StageKind.DESKTOP else initialTab)
    }
    var target by remember { mutableStateOf(initialTarget.ifEmpty { "local" }) }
    var streaming by remember { mutableStateOf(true) }
    var preset by remember { mutableStateOf("auto") }
    var error by remember { mutableStateOf("") }
    var showText by remember { mutableStateOf(false) }
    var draft by remember { mutableStateOf("") }
    var showChatSidebar by remember { mutableStateOf(false) }
    var chatBotId by remember { mutableStateOf(initialBotId.orEmpty()) }
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

    LaunchedEffect(canUseFiles) {
        if (!canUseFiles && activeTab == StageKind.FILES) {
            activeTab = StageKind.DESKTOP
        }
    }

    var keyMenuOpen by remember { mutableStateOf(false) }
    var settingsMenuOpen by remember { mutableStateOf(false) }

    val stageBg = Color.Black
    val pillBg = Color(0xFF1F1D1B)
    val pillText = Color(0xFFF3F1EE)
    val pillMute = Color(0xFFA6A09A)

    val useDarkStage = activeTab == StageKind.DESKTOP || parentPalette.dark

    DisposableEffect(useDarkStage) {
        val window = (view.context as? Activity)?.window
        val ctrl = window?.let { WindowCompat.getInsetsController(it, view) }
        val prevLightStatus = ctrl?.isAppearanceLightStatusBars ?: false
        val prevLightNav = ctrl?.isAppearanceLightNavigationBars ?: false
        ctrl?.isAppearanceLightStatusBars = !useDarkStage
        ctrl?.isAppearanceLightNavigationBars = !useDarkStage
        onDispose {
            ctrl?.isAppearanceLightStatusBars = prevLightStatus
            ctrl?.isAppearanceLightNavigationBars = prevLightNav
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

    val resolvedBotId = remember(chatBotId, store.bots) {
        when {
            chatBotId.isNotEmpty() && store.bots.any { it.id == chatBotId } -> chatBotId
            else -> store.bots.firstOrNull()?.id
        }
    }

    val hasBoundTarget = remember(resolvedBotId, store.bots) {
        val bid = resolvedBotId
        bid != null && !store.bots.firstOrNull { it.id == bid }?.desktopTarget.isNullOrEmpty()
    }

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

    LaunchedEffect(target, streaming, activeTab, isStarted, streamMode) {
        if (activeTab != StageKind.DESKTOP || !isStarted) return@LaunchedEffect
        delay(80)
        while (isActive && streaming && activeTab == StageKind.DESKTOP && isStarted) {
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

    LaunchedEffect(target, streaming, activeTab, isStarted, streamMode) {
        if (activeTab != StageKind.DESKTOP || !streaming || !isStarted || streamMode != DesktopStreamMode.FRAME_POLLING) {
            return@LaunchedEffect
        }
        var consecErrors = 0
        var windowFrames = 0
        var fpsWindowStartMs = System.currentTimeMillis()
        while (isActive && streaming && activeTab == StageKind.DESKTOP && isStarted && streamMode == DesktopStreamMode.FRAME_POLLING) {
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
                if (!isActive || !streaming || activeTab != StageKind.DESKTOP || !isStarted || streamMode != DesktopStreamMode.FRAME_POLLING) {
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

    PebbleTheme(dark = useDarkStage) {
        val p = LocalPalette.current
        val barContainer = if (activeTab == StageKind.DESKTOP) stageBg else p.page
        val barFg = if (activeTab == StageKind.DESKTOP) pillText else p.text

        BoxWithConstraints(modifier = Modifier.fillMaxSize().background(barContainer)) {
            val isWide = maxWidth >= 840.dp

            if (!isWide && showChatSidebar) {
                ModalBottomSheet(
                    onDismissRequest = { showChatSidebar = false },
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
                                onSelectBot = { chatBotId = it },
                                onCloseSidebar = { showChatSidebar = false },
                            )
                        }
                    }
                }
            }

            Scaffold(
                modifier = Modifier.fillMaxSize(),
                containerColor = barContainer,
                topBar = {
                    TopAppBar(
                        colors = TopAppBarDefaults.topAppBarColors(
                            containerColor = barContainer,
                            titleContentColor = barFg,
                        ),
                        navigationIcon = {
                            Row(
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(4.dp),
                            ) {
                                IconButton(onClick = onClose, modifier = Modifier.size(40.dp)) {
                                    Icon(painterResource(PebbleIcons.ArrowBackIosNew), contentDescription = store.t("close"), tint = barFg, modifier = Modifier.size(20.dp))
                                }
                                IconButton(
                                    onClick = { showChatSidebar = !showChatSidebar },
                                    modifier = Modifier
                                        .size(36.dp)
                                        .clip(CircleShape)
                                        .background(
                                            if (showChatSidebar) {
                                                if (activeTab == StageKind.DESKTOP) pillBg else p.s1
                                            } else {
                                                Color.Transparent
                                            },
                                        ),
                                ) {
                                    Icon(painterResource(PebbleIcons.Chat), contentDescription = store.t("deskChatSidebar"), tint = barFg, modifier = Modifier.size(20.dp))
                                }
                            }
                        },
                        title = {
                            if (canUseFiles) {
                                StageTabSwitcher(
                                    activeTab = activeTab,
                                    filesLabel = store.t("stageFilesTab"),
                                    desktopLabel = store.t("stageDesktopTab"),
                                    isDarkStage = activeTab == StageKind.DESKTOP,
                                    onSelect = { activeTab = it },
                                )
                            } else {
                                Text(
                                    text = store.t("stageDesktopTab"),
                                    color = barFg,
                                    fontSize = PebbleFont.cardTitle,
                                    fontWeight = FontWeight.SemiBold,
                                )
                            }
                        },
                        actions = {
                            if (activeTab == StageKind.FILES) {
                                IconButton(onClick = { showHiddenFiles = !showHiddenFiles }, modifier = Modifier.size(36.dp)) {
                                    Icon(
                                        painter = painterResource(if (showHiddenFiles) PebbleIcons.Visibility else PebbleIcons.VisibilityOff),
                                        contentDescription = store.t("showHiddenFiles"),
                                        tint = barFg,
                                        modifier = Modifier.size(20.dp),
                                    )
                                }
                                IconButton(onClick = { filesRefreshSeq += 1 }, modifier = Modifier.size(36.dp)) {
                                    Icon(painterResource(PebbleIcons.Refresh), contentDescription = store.t("refreshFiles"), tint = barFg, modifier = Modifier.size(20.dp))
                                }
                            } else {
                                IconButton(onClick = { streaming = !streaming }, modifier = Modifier.size(36.dp)) {
                                    Icon(
                                        painter = painterResource(if (streaming) PebbleIcons.Pause else PebbleIcons.PlayArrow),
                                        contentDescription = store.t(if (streaming) "pause" else "resume"),
                                        tint = barFg,
                                        modifier = Modifier.size(20.dp),
                                    )
                                }
                                Box {
                                    IconButton(onClick = { keyMenuOpen = true }, modifier = Modifier.size(36.dp)) {
                                        Icon(painterResource(PebbleIcons.Keyboard), contentDescription = store.t("aaInput"), tint = barFg, modifier = Modifier.size(20.dp))
                                    }
                                    DropdownMenu(
                                        expanded = keyMenuOpen,
                                        onDismissRequest = { keyMenuOpen = false },
                                        containerColor = pillBg,
                                    ) {
                                        DropdownMenuItem(
                                            text = { Text(store.t("aaInput"), color = pillText) },
                                            trailingIcon = {
                                                if (showText) Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pillText, modifier = Modifier.size(16.dp))
                                            },
                                            onClick = {
                                                showText = !showText
                                                keyMenuOpen = false
                                            },
                                        )
                                        HorizontalDivider(color = Color(0xFF333333))
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyEnter"), color = pillText) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Return")) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyBackspace"), color = pillText) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Backspace")) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("hotkeyEsc"), color = pillText) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Escape")) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("hotkeyTab"), color = pillText) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "key", "key" to "Tab")) },
                                        )
                                        HorizontalDivider(color = Color(0xFF333333))
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyScrollUp"), color = pillText) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "scroll", "deltaY" to 8)) },
                                        )
                                        DropdownMenuItem(
                                            text = { Text(store.t("keyScrollDown"), color = pillText) },
                                            onClick = { keyMenuOpen = false; send(mapOf("kind" to "scroll", "deltaY" to -8)) },
                                        )
                                    }
                                }
                                Box {
                                    IconButton(onClick = { settingsMenuOpen = true }, modifier = Modifier.size(36.dp)) {
                                        Icon(painterResource(PebbleIcons.Tune), contentDescription = store.t("settings"), tint = barFg, modifier = Modifier.size(20.dp))
                                    }
                                    DropdownMenu(
                                        expanded = settingsMenuOpen,
                                        onDismissRequest = { settingsMenuOpen = false },
                                        containerColor = pillBg,
                                    ) {
                                        if (streamMode == DesktopStreamMode.FRAME_POLLING && fallbackBitmap != null) {
                                            DropdownMenuItem(
                                                text = {
                                                    Text(
                                                        store.t("deskFrameModeLabel", mapOf("fps" to "$frameFps")),
                                                        color = pillMute,
                                                        fontSize = PebbleFont.secondary,
                                                    )
                                                },
                                                enabled = false,
                                                onClick = {},
                                            )
                                            HorizontalDivider(color = Color(0xFF333333))
                                        } else if (streamConnected) {
                                            val codecLabel = if (streamMode == DesktopStreamMode.WS_HEVC) "HEVC" else "H.264"
                                            DropdownMenuItem(
                                                text = { Text("$codecLabel · $streamFps fps", color = pillMute, fontSize = PebbleFont.secondary) },
                                                enabled = false,
                                                onClick = {},
                                            )
                                            HorizontalDivider(color = Color(0xFF333333))
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
                                                text = { Text(lbl, color = pillText) },
                                                trailingIcon = {
                                                    if (preset == id) Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pillText, modifier = Modifier.size(16.dp))
                                                },
                                                onClick = {
                                                    preset = id
                                                    settingsMenuOpen = false
                                                },
                                            )
                                        }
                                        if (targets.size > 1) {
                                            HorizontalDivider(color = Color(0xFF333333))
                                            targets.forEach { tr ->
                                                DropdownMenuItem(
                                                    text = { Text(tr.name ?: tr.id, color = pillText) },
                                                    trailingIcon = {
                                                        if (target == tr.id) Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pillText, modifier = Modifier.size(16.dp))
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
                        visible = isWide && showChatSidebar && resolvedBotId != null,
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
                                    .padding(
                                        start = 12.dp,
                                        top = 12.dp,
                                        bottom = 12.dp,
                                        end = if (activeTab == StageKind.DESKTOP) 12.dp else 0.dp,
                                    )
                                    .width(340.dp)
                                    .fillMaxHeight(),
                            ) {
                                ChatScreen(
                                    store = store,
                                    botId = resolvedBotId,
                                    compact = false,
                                    inDesktopSidebar = true,
                                    onSelectBot = { chatBotId = it },
                                    onCloseSidebar = null,
                                )
                            }
                        }
                    }

                    Box(
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxHeight(),
                    ) {
                        if (activeTab == StageKind.FILES) {
                            WorkspaceFilesView(
                                store = store,
                                botId = resolvedBotId.orEmpty(),
                                initialPath = initialPath,
                                showHidden = showHiddenFiles,
                                refreshTrigger = filesRefreshSeq,
                                inWideStage = isWide,
                                onQuotePath = { relPath ->
                                    store.insertIntoChat("@$relPath ")
                                    showChatSidebar = true
                                },
                                modifier = Modifier.fillMaxSize(),
                            )
                        } else {
                            Column(modifier = Modifier.fillMaxSize()) {
                                if (store.activeDesktopTarget.temporary) {
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .padding(horizontal = 12.dp, vertical = 4.dp)
                                            .clip(RoundedCornerShape(12.dp))
                                            .background(pillBg)
                                            .padding(horizontal = 12.dp, vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        Text(
                                            text = store.t("tempDesktopBanner", mapOf("name" to (store.activeDesktopTarget.name ?: store.activeDesktopTarget.target))),
                                            color = pillText,
                                            fontSize = PebbleFont.secondary,
                                            fontWeight = FontWeight.SemiBold,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                            modifier = Modifier.weight(1f),
                                        )
                                        OutlinedButton(
                                            onClick = {
                                                target = "local"
                                                scope.launch { runCatching { store.setActiveDesktopTarget("local") } }
                                            },
                                            contentPadding = PaddingValues(horizontal = 10.dp, vertical = 2.dp),
                                        ) {
                                            Text(store.t("switchBackLocal"), color = pillText, fontSize = PebbleFont.caption)
                                        }
                                    }
                                }

                                if (activeTakeover != null) {
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .padding(horizontal = 12.dp, vertical = 4.dp)
                                            .clip(RoundedCornerShape(12.dp))
                                            .background(Color(0xFF2C1616))
                                            .padding(horizontal = 12.dp, vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        Icon(painterResource(PebbleIcons.PanTool), contentDescription = null, tint = Color(0xFFFF6961), modifier = Modifier.size(16.dp))
                                        Text(
                                            text = store.t(
                                                "takeoverBanner",
                                                mapOf("bot" to (activeTakeover.botName ?: activeTakeover.botId ?: "Bot"), "reason" to activeTakeover.reason.orEmpty()),
                                            ),
                                            color = Color(0xFFFF6961),
                                            fontSize = PebbleFont.body,
                                            maxLines = 2,
                                            overflow = TextOverflow.Ellipsis,
                                            modifier = Modifier.weight(1f),
                                        )
                                        Button(
                                            onClick = { scope.launch { runCatching { store.resolveTakeover(activeTakeover.id) } } },
                                            colors = ButtonDefaults.buttonColors(containerColor = pillText, contentColor = stageBg),
                                            contentPadding = PaddingValues(horizontal = 10.dp, vertical = 4.dp),
                                        ) {
                                            Text(store.t("doneResumeAI"), fontSize = PebbleFont.caption, fontWeight = FontWeight.SemiBold)
                                        }
                                    }
                                }

                                BoxWithConstraints(
                                    modifier = Modifier
                                        .weight(1f)
                                        .fillMaxWidth(),
                                ) {
                                    val density = LocalDensity.current
                                    val boxW = with(density) { maxWidth.toPx() }
                                    val boxH = with(density) { maxHeight.toPx() }

                                    fun mapPoint(pos: Offset): Pair<Double, Double>? {
                                        val m = streamMeta ?: return null
                                        if (m.imageWidth <= 0 || m.imageHeight <= 0 || boxW <= 0f || boxH <= 0f) return null
                                        val scale = minOf(boxW / m.imageWidth.toFloat(), boxH / m.imageHeight.toFloat())
                                        val dw = m.imageWidth.toFloat() * scale
                                        val dh = m.imageHeight.toFloat() * scale
                                        val rx = (pos.x - (boxW - dw) / 2f) / dw
                                        val ry = (pos.y - (boxH - dh) / 2f) / dh
                                        if (rx !in 0f..1f || ry !in 0f..1f) return null
                                        return (m.logicalX + rx * m.logicalWidth) to (m.logicalY + ry * m.logicalHeight)
                                    }

                                    Box(
                                        modifier = Modifier
                                            .fillMaxSize()
                                            .pointerInput(streamMeta, target) {
                                                awaitEachGesture {
                                                    val down = awaitFirstDown(requireUnconsumed = false)
                                                    val downTime = System.currentTimeMillis()
                                                    val startPos = down.position
                                                    var isDragging = false
                                                    var lastMoveSent = 0L
                                                    var lastPos = startPos
                                                    var isScroll = false

                                                    while (true) {
                                                        val ev = awaitPointerEvent()
                                                        val pressed = ev.changes.filter { it.pressed }
                                                        if (pressed.isEmpty()) break

                                                        if (pressed.size >= 2) {
                                                            isScroll = true
                                                            val p0 = pressed[0]
                                                            val dy = p0.position.y - p0.previousPosition.y
                                                            if (kotlin.math.abs(dy) > 2f) {
                                                                send(mapOf("kind" to "scroll", "deltaY" to (dy / 12f).toInt().coerceIn(-10, 10)))
                                                            }
                                                        } else {
                                                            val ch = pressed.first()
                                                            lastPos = ch.position
                                                            if (!isScroll && !isDragging && hypot(lastPos.x - startPos.x, lastPos.y - startPos.y) >= 14f) {
                                                                isDragging = true
                                                                mapPoint(startPos)?.let { (lx, ly) ->
                                                                    send(mapOf("kind" to "down", "x" to lx, "y" to ly, "button" to "left"))
                                                                }
                                                            } else if (isDragging) {
                                                                val now = System.currentTimeMillis()
                                                                if (now - lastMoveSent >= 16L) {
                                                                    lastMoveSent = now
                                                                    mapPoint(lastPos)?.let { (lx, ly) ->
                                                                        send(mapOf("kind" to "move", "x" to lx, "y" to ly))
                                                                    }
                                                                }
                                                            }
                                                        }
                                                    }

                                                    if (isDragging) {
                                                        mapPoint(lastPos)?.let { (lx, ly) ->
                                                            send(mapOf("kind" to "up", "x" to lx, "y" to ly, "button" to "left"))
                                                        }
                                                    } else if (!isScroll) {
                                                        val kind = if (System.currentTimeMillis() - downTime > 500L) "right_click" else "click"
                                                        mapPoint(startPos)?.let { (lx, ly) ->
                                                            send(mapOf("kind" to kind, "x" to lx, "y" to ly))
                                                        }
                                                    }
                                                }
                                            },
                                    ) {
                                        if (streamMode == DesktopStreamMode.FRAME_POLLING) {
                                            fallbackBitmap?.let { bmp ->
                                                Image(
                                                    bitmap = bmp,
                                                    contentDescription = null,
                                                    contentScale = ContentScale.Fit,
                                                    modifier = Modifier.fillMaxSize(),
                                                )
                                            }
                                        } else if (wsUrl.isNotEmpty()) {
                                            key(target, streamMode) {
                                                H264SurfaceView(
                                                    url = wsUrl,
                                                    token = store.authToken,
                                                    preset = preset,
                                                    active = streaming && activeTab == StageKind.DESKTOP && isStarted,
                                                    controller = streamCtrl,
                                                    onMeta = { streamMeta = it },
                                                    onStatus = { conn, fps ->
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
                                                    modifier = Modifier.fillMaxSize(),
                                                )
                                            }
                                        }

                                        val isDisplayReady = (streamMode == DesktopStreamMode.FRAME_POLLING && fallbackBitmap != null) || streamConnected
                                        val displayErr = error.ifEmpty { if (streamMode == DesktopStreamMode.FRAME_POLLING) "" else streamErr }
                                        if (!isDisplayReady) {
                                            Column(
                                                modifier = Modifier
                                                    .align(Alignment.Center)
                                                    .padding(20.dp),
                                                horizontalAlignment = Alignment.CenterHorizontally,
                                                verticalArrangement = Arrangement.spacedBy(10.dp),
                                            ) {
                                                CircularProgressIndicator(color = pillText, strokeWidth = 2.dp)
                                                Text(
                                                    text = displayErr.ifEmpty { store.t("deskH264Connecting") },
                                                    color = pillMute,
                                                    fontSize = PebbleFont.body,
                                                    textAlign = TextAlign.Center,
                                                )
                                                if (displayErr.isNotEmpty()) {
                                                    Button(
                                                        onClick = {
                                                            error = ""
                                                            streamErr = ""
                                                            fallbackBitmap = null
                                                            streamMode = preferredDesktopStreamMode(store, target)
                                                            streaming = false
                                                            streaming = true
                                                        },
                                                        colors = ButtonDefaults.buttonColors(containerColor = pillText, contentColor = stageBg),
                                                    ) {
                                                        Text(store.t("retryConnect"), fontSize = PebbleFont.body, fontWeight = FontWeight.SemiBold)
                                                    }
                                                }
                                            }
                                        } else if (displayErr.isNotEmpty()) {
                                            Surface(
                                                color = pillBg,
                                                shape = CircleShape,
                                                modifier = Modifier
                                                    .align(Alignment.BottomCenter)
                                                    .padding(10.dp),
                                            ) {
                                                Text(
                                                    text = displayErr,
                                                    color = pillText,
                                                    fontSize = PebbleFont.secondary,
                                                    modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                                                )
                                            }
                                        }
                                    }
                                }

                                if (showText) {
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .padding(horizontal = 12.dp, vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        Surface(
                                            color = pillBg,
                                            shape = CircleShape,
                                            modifier = Modifier
                                                .size(40.dp)
                                                .clip(CircleShape),
                                        ) {
                                            IconButton(onClick = { showText = false }) {
                                                Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = pillText, modifier = Modifier.size(18.dp))
                                            }
                                        }
                                        Surface(
                                            color = pillBg,
                                            shape = RoundedCornerShape(22.dp),
                                            modifier = Modifier.weight(1f),
                                        ) {
                                            Box(modifier = Modifier.padding(horizontal = 14.dp, vertical = 10.dp)) {
                                                if (draft.isEmpty()) {
                                                    Text(store.t("typeDesktopPh"), color = pillMute, fontSize = PebbleFont.body, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                                }
                                                BasicTextField(
                                                    value = draft,
                                                    onValueChange = { draft = it },
                                                    singleLine = true,
                                                    textStyle = TextStyle(color = pillText, fontSize = PebbleFont.body),
                                                    cursorBrush = SolidColor(pillText),
                                                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                                                    keyboardActions = KeyboardActions(onSend = { sendText() }),
                                                    modifier = Modifier.fillMaxWidth(),
                                                )
                                            }
                                        }
                                        Surface(
                                            color = if (draft.isNotEmpty()) pillText else pillBg,
                                            shape = CircleShape,
                                            modifier = Modifier.size(40.dp),
                                        ) {
                                            IconButton(onClick = { sendText() }, enabled = draft.isNotEmpty()) {
                                                Icon(painterResource( PebbleIcons.ArrowUpward),
                                                    contentDescription = store.t("send"),
                                                    tint = if (draft.isNotEmpty()) stageBg else pillMute,
                                                    modifier = Modifier.size(18.dp),
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
        }
    }
}

@Composable
private fun StageTabSwitcher(
    activeTab: StageKind,
    filesLabel: String,
    desktopLabel: String,
    isDarkStage: Boolean,
    onSelect: (StageKind) -> Unit,
) {
    val p = LocalPalette.current
    val containerBg = if (isDarkStage) Color(0xFF1F1D1B) else p.s1
    val activeBg = if (isDarkStage) Color(0xFF3A3734) else p.card
    val activeFg = if (isDarkStage) Color(0xFFF3F1EE) else p.text
    val inactiveFg = if (isDarkStage) Color(0xFFA6A09A) else p.text2

    Surface(
        color = containerBg,
        shape = CircleShape,
        modifier = Modifier.border(0.5.dp, if (isDarkStage) Color.White.copy(alpha = 0.10f) else p.s2, CircleShape),
    ) {
        Row(
            modifier = Modifier.padding(3.dp),
            horizontalArrangement = Arrangement.spacedBy(2.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            listOf(StageKind.FILES to filesLabel, StageKind.DESKTOP to desktopLabel).forEach { (kind, lbl) ->
                val sel = activeTab == kind
                Box(
                    modifier = Modifier
                        .clip(CircleShape)
                        .background(if (sel) activeBg else Color.Transparent)
                        .clickable { onSelect(kind) }
                        .padding(horizontal = 10.dp, vertical = 5.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = lbl,
                        color = if (sel) activeFg else inactiveFg,
                        fontSize = PebbleFont.secondary,
                        fontWeight = if (sel) FontWeight.SemiBold else FontWeight.Medium,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}
