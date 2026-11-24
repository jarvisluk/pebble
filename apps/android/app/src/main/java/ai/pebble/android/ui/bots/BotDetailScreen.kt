package ai.pebble.android.ui.bots

import android.graphics.BitmapFactory
import android.util.Base64
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.HubException
import ai.pebble.android.data.MemoryItem
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.RoutineItem
import ai.pebble.android.data.RoutineRun
import ai.pebble.android.data.errText
import ai.pebble.android.ui.desktop.H264SurfaceView
import ai.pebble.android.ui.theme.BotAppearancePicker
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.BotAvatar
import ai.pebble.android.ui.theme.CardDivider
import ai.pebble.android.ui.theme.ConfirmAlertDialog
import ai.pebble.android.ui.theme.ConfirmItem
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import ai.pebble.android.ui.theme.SectionCard
import ai.pebble.android.ui.theme.SectionHeader
import ai.pebble.android.ui.theme.StatusTextBanner
import ai.pebble.android.ui.theme.copyToClipboard
import ai.pebble.android.ui.theme.pebbleFieldColors
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Locale
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BotDetailScreen(
    store: PebbleStore,
    botId: String,
    onClose: () -> Unit,
    onOpenDesktop: (String) -> Unit,
) {
    val p = LocalPalette.current

    var err by remember(botId) { mutableStateOf("") }
    var previewConnected by remember(botId) { mutableStateOf(false) }
    var previewError by remember(botId) { mutableStateOf("") }
    var previewUseFrame by remember(botId) { mutableStateOf(false) }
    var previewBitmap by remember(botId) { mutableStateOf<ImageBitmap?>(null) }
    var previewFrameError by remember(botId) { mutableStateOf("") }
    var editingRoutine by remember(botId) { mutableStateOf<RoutineItem?>(null) }
    var creatingRoutine by remember(botId) { mutableStateOf(false) }

    LaunchedEffect(botId) {
        if (store.desktopTargets.isEmpty()) {
            runCatching { store.refreshDesktopTargets() }
        }
        runCatching { store.fetchRoutines(botId) }
    }

    if (creatingRoutine || editingRoutine != null) {
        ModalBottomSheet(
            onDismissRequest = {
                creatingRoutine = false
                editingRoutine = null
            },
            sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
            containerColor = p.page,
        ) {
            RoutineEditorSheet(
                store = store,
                botId = botId,
                routine = editingRoutine,
                onDismiss = {
                    creatingRoutine = false
                    editingRoutine = null
                },
            )
        }
    }

    val previewTarget = store.takeovers.firstOrNull { it.botId == botId }?.target?.takeIf { it.isNotEmpty() }
        ?: store.bots.firstOrNull { it.id == botId }?.desktopTarget?.takeIf { it.isNotEmpty() }
        ?: store.activeDesktopTarget.target.takeIf { it.isNotEmpty() }
        ?: "local"
    val previewBadgeLabel = store.desktopTargets.firstOrNull { it.id == previewTarget }?.name?.takeIf { it.isNotEmpty() }
        ?: previewTarget
    val shouldUsePreviewFrame = !PebbleStore.isLocalDesktopTarget(previewTarget) || !store.supports("desktop.ws_h264") || previewUseFrame
    val previewCodec = if (store.supports("desktop.ws_hevc")) "hevc" else "h264"
    val previewWsUrl = store.makeDesktopWsUrl(previewTarget, preset = "preview", codec = previewCodec).orEmpty()
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

    LaunchedEffect(previewTarget) {
        previewUseFrame = false
        previewConnected = false
        previewError = ""
        previewBitmap = null
        previewFrameError = ""
    }

    LaunchedEffect(botId, previewTarget, store.isConnected, isStarted, shouldUsePreviewFrame) {
        if (!store.isConnected || !isStarted || !shouldUsePreviewFrame) return@LaunchedEffect
        while (isActive && store.isConnected && isStarted && shouldUsePreviewFrame) {
            runCatching {
                val fr = store.fetchDesktopFrame(previewTarget)
                withContext(Dispatchers.IO) {
                    if (fr.jpegBase64.isEmpty()) return@withContext null
                    val raw = Base64.decode(fr.jpegBase64, Base64.DEFAULT)
                    BitmapFactory.decodeByteArray(raw, 0, raw.size)?.asImageBitmap()
                }
            }.onSuccess { bmp ->
                if (!isActive || !isStarted) return@onSuccess
                if (bmp != null) {
                    previewBitmap = bmp
                    if (previewFrameError.isNotEmpty()) previewFrameError = ""
                }
            }.onFailure { err ->
                if (!isActive) return@onFailure
                if (err is HubException && err.code == "screen_recording_denied") {
                    previewFrameError = store.t("deskScreenRecordingDenied")
                    return@LaunchedEffect
                }
                if (previewBitmap == null) {
                    previewFrameError = errText(err, store.t("connFailed"))
                }
            }
            delay(2500L)
        }
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
                    IconButton(onClick = onClose) {
                        Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text)
                    }
                },
                title = {
                    Text(store.t("rightPanelTitle"), fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
                },
            )
        },
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .imePadding()
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (err.isNotEmpty()) {
                SectionCard {
                    Box(modifier = Modifier.padding(14.dp)) {
                        StatusTextBanner(text = err, error = true)
                    }
                }
            }

            // Borderless Live Remote Desktop Thumbnail Preview
            if (store.isConnected && (shouldUsePreviewFrame || previewWsUrl.isNotEmpty())) {
                val hasPreview = previewConnected || (shouldUsePreviewFrame && previewBitmap != null)
                val displayPreviewErr = if (shouldUsePreviewFrame) previewFrameError else previewError
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .aspectRatio(16f / 10f)
                        .clip(PebbleDimens.cardShape)
                        .background(Color.Black)
                        .clickable { onOpenDesktop(previewTarget) },
                ) {
                    if (shouldUsePreviewFrame) {
                        previewBitmap?.let { bmp ->
                            Image(
                                bitmap = bmp,
                                contentDescription = null,
                                contentScale = ContentScale.Fit,
                                modifier = Modifier.fillMaxSize(),
                            )
                        }
                    } else {
                        key(previewTarget, previewCodec) {
                            H264SurfaceView(
                                url = previewWsUrl,
                                token = store.authToken,
                                preset = "preview",
                                active = isStarted,
                                trackFps = false,
                                onStatus = { connected, _ -> previewConnected = connected },
                                onStreamError = { code, msg ->
                                    if (!previewConnected && code != "screen_recording_denied") {
                                        previewUseFrame = true
                                    } else {
                                        previewError = msg
                                    }
                                },
                                modifier = Modifier.fillMaxSize(),
                            )
                        }
                    }
                    if (!hasPreview) {
                        Column(
                            modifier = Modifier
                                .align(Alignment.Center)
                                .padding(16.dp),
                            horizontalAlignment = Alignment.CenterHorizontally,
                            verticalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            if (displayPreviewErr.isNotEmpty()) {
                                Icon(painterResource(PebbleIcons.DesktopWindows), contentDescription = null, tint = Color.White.copy(alpha = 0.6f))
                                Text(displayPreviewErr, color = Color.White.copy(alpha = 0.7f), fontSize = PebbleFont.caption, textAlign = TextAlign.Center, maxLines = 2)
                            } else {
                                CircularProgressIndicator(modifier = Modifier.size(22.dp), strokeWidth = 2.dp, color = Color.White)
                            }
                        }
                    }
                    if (previewTarget != "local") {
                        Text(
                            text = previewBadgeLabel,
                            color = Color.White,
                            fontSize = PebbleFont.caption,
                            fontWeight = FontWeight.Medium,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier
                                .align(Alignment.BottomStart)
                                .padding(12.dp)
                                .clip(CircleShape)
                                .background(Color.Black.copy(alpha = 0.60f))
                                .padding(horizontal = 9.dp, vertical = 4.dp),
                        )
                    }
                    Box(
                        modifier = Modifier
                            .align(Alignment.TopEnd)
                            .padding(12.dp)
                            .size(30.dp)
                            .clip(CircleShape)
                            .background(Color.Black.copy(alpha = 0.55f)),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(painterResource(PebbleIcons.OpenInFull), contentDescription = null, tint = Color.White, modifier = Modifier.size(15.dp))
                    }
                }
            }

            // Routines
            RoutinesBlock(
                store = store,
                botId = botId,
                onCreate = { creatingRoutine = true },
                onEdit = { editingRoutine = it },
                onError = { err = it },
            )

            Spacer(Modifier.height(16.dp))
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BotInfoSheet(
    store: PebbleStore,
    botId: String,
    onClose: () -> Unit,
    onDeletedBot: () -> Unit,
    onOpenFiles: (String) -> Unit = {},
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    var detail by remember(botId) { mutableStateOf<BotStatus?>(null) }
    var loading by remember(botId) { mutableStateOf(false) }
    var techOpen by remember(botId) { mutableStateOf(false) }
    var appearOpen by remember(botId) { mutableStateOf(false) }
    var editName by remember(botId) { mutableStateOf("") }
    var editDesc by remember(botId) { mutableStateOf("") }
    var editAvatar by remember(botId) { mutableStateOf("") }
    var editColor by remember(botId) { mutableStateOf("") }
    var editInst by remember(botId) { mutableStateOf("") }
    var editPrivate by remember(botId) { mutableStateOf(false) }
    var editDesktopTarget by remember(botId) { mutableStateOf("") }
    var editBrowserMode by remember(botId) { mutableStateOf("") }
    var screenMenuOpen by remember(botId) { mutableStateOf(false) }
    var browserMenuOpen by remember(botId) { mutableStateOf(false) }
    var saving by remember(botId) { mutableStateOf(false) }
    var err by remember(botId) { mutableStateOf("") }
    var confirm by remember(botId) { mutableStateOf<ConfirmItem?>(null) }

    var mem by remember(botId) { mutableStateOf<MemoryList?>(null) }
    var memLoading by remember(botId) { mutableStateOf(false) }
    var memText by remember(botId) { mutableStateOf("") }
    var memSaving by remember(botId) { mutableStateOf(false) }

    val index = store.bots.indexOfFirst { it.id == botId }
    val bot = if (index >= 0) store.bots[index] else null
    val active = detail ?: bot
    val pal = p.bot(botId, index, editColor.ifEmpty { active?.color })

    val canBindDesktop = store.supports("bot.desktop_binding")
    val canIsolateBrowser = store.supports("bot.browser_isolation")

    val dirty = remember(active, detail, editName, editDesc, editAvatar, editColor, editPrivate, editDesktopTarget, editBrowserMode, editInst, canBindDesktop, canIsolateBrowser) {
        active != null && (
            editName != (active.name ?: botId) ||
                editDesc != active.description.orEmpty() ||
                editAvatar != active.avatar.orEmpty() ||
                editColor != active.color.orEmpty() ||
                editPrivate != (active.privateSessions == true) ||
                (canBindDesktop && editDesktopTarget != active.desktopTarget.orEmpty()) ||
                (canIsolateBrowser && editBrowserMode != active.browserMode.orEmpty()) ||
                editInst != detail?.instructions.orEmpty()
            )
    }

    val desktopTargetOptions = remember(store.desktopTargets, botId, editDesktopTarget) {
        val opts = mutableListOf(
            "" to store.t("deskBindDefault"),
            "local" to store.t("localDesktopName"),
        )
        val seen = linkedSetOf("", "local")
        if (store.desktopTargets.firstOrNull { it.id == "local" }?.virtualSupported == true) {
            val vid = "virtual:$botId"
            seen.add(vid)
            opts.add(vid to store.t("deskBindLocalVirtual"))
        }
        store.desktopTargets.filter { it.id != "local" }.forEach { tr ->
            if (seen.add(tr.id)) {
                opts.add(tr.id to (tr.name ?: tr.id))
            }
            if (tr.virtual != true && tr.virtualSupported == true) {
                val rvid = "${tr.id}:virtual:$botId"
                if (seen.add(rvid)) {
                    opts.add(rvid to store.t("deskBindRemoteVirtual", mapOf("name" to (tr.name ?: tr.id))))
                }
            }
        }
        if (editDesktopTarget.isNotEmpty() && seen.add(editDesktopTarget)) {
            opts.add(editDesktopTarget to store.t("deskBindOfflineTag", mapOf("id" to editDesktopTarget)))
        }
        opts
    }

    val browserModeOptions = remember(store.langPref) {
        listOf(
            "" to store.t("browserModeAuto"),
            "isolated" to store.t("browserModeIsolated"),
            "attach" to store.t("browserModeAttach"),
        )
    }

    fun apply(d: BotStatus) {
        detail = d
        editName = d.name ?: botId
        editDesc = d.description.orEmpty()
        editAvatar = d.avatar.orEmpty()
        editColor = d.color.orEmpty()
        editInst = d.instructions.orEmpty()
        editPrivate = d.privateSessions == true
        editDesktopTarget = d.desktopTarget.orEmpty()
        editBrowserMode = d.browserMode.orEmpty()
    }

    suspend fun loadMem() {
        memLoading = true
        runCatching { store.fetchMemories(scope = "project", botId = botId, limit = 30) }
            .getOrNull()?.let { mem = it }
        memLoading = false
    }

    LaunchedEffect(botId) {
        val src = detail ?: bot
        editName = src?.name ?: botId
        editDesc = src?.description.orEmpty()
        editAvatar = src?.avatar.orEmpty()
        editColor = src?.color.orEmpty()
        editInst = detail?.instructions.orEmpty()
        editPrivate = src?.privateSessions == true
        editDesktopTarget = src?.desktopTarget.orEmpty()
        editBrowserMode = src?.browserMode.orEmpty()
        loading = true
        scope.launch { loadMem() }
        scope.launch { runCatching { store.refreshDesktopTargets() } }
        runCatching { store.botDetail(botId) }.getOrNull()?.let { apply(it) }
        loading = false
    }

    fun save() {
        val n = editName.trim()
        if (n.isEmpty()) {
            err = store.t("nameRequired")
            return
        }
        err = ""
        saving = true
        scope.launch {
            try {
                val payload = mutableMapOf<String, Any?>(
                    "name" to n,
                    "description" to editDesc.trim(),
                    "avatar" to editAvatar,
                    "color" to editColor,
                    "instructions" to editInst,
                    "privateSessions" to editPrivate,
                )
                if (canBindDesktop) payload["desktopTarget"] = editDesktopTarget
                if (canIsolateBrowser) payload["browserMode"] = editBrowserMode
                store.updateBot(botId, payload)
                apply(store.botDetail(botId))
            } catch (e: Throwable) {
                err = errText(e, store.t("saveFailed"))
            } finally {
                saving = false
            }
        }
    }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    Scaffold(
        containerColor = p.page,
        topBar = {
            TopAppBar(
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = p.page,
                    titleContentColor = p.text,
                ),
                navigationIcon = {
                    IconButton(onClick = onClose) {
                        Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = p.text)
                    }
                },
                title = {
                    Text(store.t("botDetails"), fontSize = PebbleFont.title, fontWeight = FontWeight.Bold)
                },
                actions = {
                    if (saving) {
                        CircularProgressIndicator(modifier = Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp, color = pal.accent)
                    } else {
                        TextButton(
                            onClick = { save() },
                            enabled = dirty && editName.trim().isNotEmpty(),
                        ) {
                            Text(
                                text = store.t("save"),
                                color = if (dirty && editName.trim().isNotEmpty()) pal.accent else p.mute,
                                fontSize = PebbleFont.body,
                                fontWeight = FontWeight.SemiBold,
                            )
                        }
                    }
                },
            )
        },
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .imePadding()
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (err.isNotEmpty()) {
                SectionCard {
                    Box(modifier = Modifier.padding(14.dp)) {
                        StatusTextBanner(text = err, error = true)
                    }
                }
            }

            // Profile + Stats Card
            SectionCard {
                Column(
                    modifier = Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                    ) {
                        BotAvatar(
                            id = botId,
                            size = 56.dp,
                            index = index,
                            avatar = editAvatar,
                            color = editColor,
                            interactive = true,
                        )
                        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            OutlinedTextField(
                                value = editName,
                                onValueChange = { editName = it },
                                placeholder = { Text(store.t("botNamePh")) },
                                singleLine = true,
                                textStyle = TextStyle(fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold),
                                shape = PebbleDimens.rowShape,
                                colors = pebbleFieldColors(),
                                modifier = Modifier.fillMaxWidth(),
                            )
                            Text("@$botId", color = p.mute, fontSize = PebbleFont.secondary, fontFamily = MonoFont)
                        }
                    }

                    OutlinedTextField(
                        value = editDesc,
                        onValueChange = { editDesc = it },
                        placeholder = { Text(store.t("descPh")) },
                        minLines = 1,
                        maxLines = 4,
                        shape = PebbleDimens.rowShape,
                        colors = pebbleFieldColors(),
                        modifier = Modifier.fillMaxWidth(),
                    )

                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { appearOpen = !appearOpen }
                            .padding(horizontal = 4.dp, vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(store.t("appearance"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                        Icon(painterResource(if (appearOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore),
                            contentDescription = null,
                            tint = p.mute,
                        )
                    }

                    AnimatedVisibility(visible = appearOpen) {
                        BotAppearancePicker(
                            botId = botId,
                            index = index,
                            avatar = editAvatar,
                            color = editColor,
                            accent = pal.accent,
                            onAvatarChange = { editAvatar = it },
                            onColorChange = { editColor = it },
                        )
                    }

                    CardDivider()
                    DetailRow(store.t("statTurns"), "${active?.session?.turnCount ?: 0}")
                    CardDivider()
                    DetailRow(store.t("statCost"), String.format(Locale.US, "$%.4f", active?.session?.totalCostUsd ?: 0.0))
                }
            }

            // Bot Memory
            SectionHeader(store.t("botMemory"))
            SectionCard {
                Column(
                    modifier = Modifier.padding(14.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    val m = mem
                    when {
                        memLoading && m == null -> CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
                        m?.installed != true -> {
                            Text(store.t("hostNoMagpie"), color = p.text2, fontSize = PebbleFont.body)
                            TextButton(
                                onClick = {
                                    scope.launch {
                                        runCatching {
                                            store.installMagpie()
                                            loadMem()
                                        }.onFailure { err = errText(it, store.t("opFailed")) }
                                    }
                                },
                                contentPadding = PaddingValues(0.dp),
                            ) {
                                Text(store.t("mpInstallOnHost"), color = pal.accent, fontSize = PebbleFont.body)
                            }
                        }
                        m.available != true -> {
                            Text(store.t("magpieNoResp"), color = p.danger, fontSize = PebbleFont.body)
                        }
                        else -> {
                            val items = m.items.orEmpty()
                            if (items.isEmpty()) {
                                Text(store.t("noBotMemories"), color = p.mute, fontSize = PebbleFont.secondary)
                            } else {
                                items.forEachIndexed { idx, item ->
                                    if (idx > 0) CardDivider()
                                    MemoryItemRow(
                                        store = store,
                                        item = item,
                                        onDelete = {
                                            confirm = ConfirmItem(
                                                title = store.t("deleteMemTitle"),
                                                message = store.t("deleteMemConfirm"),
                                            ) {
                                                scope.launch {
                                                    runCatching { store.deleteMemory(item.id) }
                                                    loadMem()
                                                }
                                            }
                                        },
                                    )
                                }
                            }
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.Bottom,
                                horizontalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                OutlinedTextField(
                                    value = memText,
                                    onValueChange = { memText = it },
                                    placeholder = { Text(store.t("botMemPh")) },
                                    minLines = 1,
                                    maxLines = 4,
                                    shape = PebbleDimens.rowShape,
                                    colors = pebbleFieldColors(),
                                    modifier = Modifier.weight(1f),
                                )
                                Button(
                                    onClick = {
                                        val txt = memText.trim()
                                        if (txt.isEmpty()) return@Button
                                        memSaving = true
                                        scope.launch {
                                            try {
                                                store.saveMemory(
                                                    mapOf(
                                                        "scope" to "project",
                                                        "botId" to botId,
                                                        "kind" to "fact",
                                                        "text" to txt,
                                                    ),
                                                )
                                                memText = ""
                                                loadMem()
                                            } catch (e: Throwable) {
                                                err = errText(e, store.t("saveBotMemFailed"))
                                            } finally {
                                                memSaving = false
                                            }
                                        }
                                    },
                                    enabled = !memSaving && memText.trim().isNotEmpty(),
                                    colors = ButtonDefaults.buttonColors(containerColor = pal.accent, contentColor = pal.onAccent),
                                ) {
                                    Text(store.t("saveMemory"), fontSize = PebbleFont.secondary)
                                }
                            }
                        }
                    }
                }
            }

            // Tech Details & AGENTS.md
            SectionCard {
                Column(modifier = Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(PebbleDimens.rowShape)
                            .clickable { techOpen = !techOpen }
                            .padding(horizontal = 4.dp, vertical = 6.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(store.t("techDetails"), color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium, modifier = Modifier.weight(1f))
                        Icon(painterResource(if (techOpen) PebbleIcons.ExpandLess else PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute)
                    }
                    AnimatedVisibility(visible = techOpen) {
                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            DetailRow(store.t("statHarness"), active?.harness ?: "-")
                            DetailRow(
                                store.t("statModel"),
                                active?.model?.takeIf { it.isNotEmpty() } ?: store.t("defaultTag"),
                            )
                            val archivedText = store.t("archivedCount", mapOf("n" to (active?.archivedSessions ?: 0))) +
                                if (active?.privateSessions == true) store.t("privateTag") else ""
                            DetailRow(store.t("statArchived"), archivedText)
                            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                Text(store.t("workspaceDir"), color = p.text2, fontSize = PebbleFont.secondary)
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .clip(PebbleDimens.rowShape)
                                        .clickable {
                                            onClose()
                                            onOpenFiles(botId)
                                        }
                                        .padding(vertical = 4.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                                ) {
                                    Text(
                                        text = active?.workspace ?: "-",
                                        color = pal.accent,
                                        fontSize = PebbleFont.secondary,
                                        fontFamily = MonoFont,
                                        maxLines = 1,
                                        overflow = TextOverflow.MiddleEllipsis,
                                        modifier = Modifier.weight(1f),
                                    )
                                    Icon(
                                        painter = painterResource( PebbleIcons.Folder),
                                        contentDescription = store.t("workspaceFiles"),
                                        tint = pal.accent,
                                        modifier = Modifier.size(16.dp),
                                    )
                                }
                            }
                            if (canBindDesktop) {
                                Box(modifier = Modifier.fillMaxWidth()) {
                                    val currentScreenLabel = desktopTargetOptions.firstOrNull { it.first == editDesktopTarget }?.second ?: editDesktopTarget
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clip(PebbleDimens.rowShape)
                                            .clickable { screenMenuOpen = true }
                                            .padding(vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        Text(store.t("botDesktopTarget"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                        Text(
                                            text = currentScreenLabel,
                                            color = p.text2,
                                            fontSize = PebbleFont.secondary,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                            modifier = Modifier.widthIn(max = 170.dp),
                                        )
                                        Icon(painterResource(PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                                    }
                                    DropdownMenu(
                                        expanded = screenMenuOpen,
                                        onDismissRequest = { screenMenuOpen = false },
                                        containerColor = p.card,
                                    ) {
                                        desktopTargetOptions.forEach { (id, label) ->
                                            DropdownMenuItem(
                                                text = { Text(label, color = p.text, fontSize = PebbleFont.body) },
                                                trailingIcon = {
                                                    if (editDesktopTarget == id) {
                                                        Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pal.accent, modifier = Modifier.size(16.dp))
                                                    }
                                                },
                                                onClick = {
                                                    editDesktopTarget = id
                                                    screenMenuOpen = false
                                                },
                                            )
                                        }
                                    }
                                }
                            }
                            if (canIsolateBrowser) {
                                Box(modifier = Modifier.fillMaxWidth()) {
                                    val currentBrowserLabel = browserModeOptions.firstOrNull { it.first == editBrowserMode }?.second ?: editBrowserMode
                                    Row(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .clip(PebbleDimens.rowShape)
                                            .clickable { browserMenuOpen = true }
                                            .padding(vertical = 8.dp),
                                        verticalAlignment = Alignment.CenterVertically,
                                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                                    ) {
                                        Text(store.t("botBrowserMode"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                        Text(
                                            text = currentBrowserLabel,
                                            color = p.text2,
                                            fontSize = PebbleFont.secondary,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis,
                                            modifier = Modifier.widthIn(max = 170.dp),
                                        )
                                        Icon(painterResource(PebbleIcons.ExpandMore), contentDescription = null, tint = p.mute, modifier = Modifier.size(18.dp))
                                    }
                                    DropdownMenu(
                                        expanded = browserMenuOpen,
                                        onDismissRequest = { browserMenuOpen = false },
                                        containerColor = p.card,
                                    ) {
                                        browserModeOptions.forEach { (id, label) ->
                                            DropdownMenuItem(
                                                text = { Text(label, color = p.text, fontSize = PebbleFont.body) },
                                                trailingIcon = {
                                                    if (editBrowserMode == id) {
                                                        Icon(painterResource(PebbleIcons.Check), contentDescription = null, tint = pal.accent, modifier = Modifier.size(16.dp))
                                                    }
                                                },
                                                onClick = {
                                                    editBrowserMode = id
                                                    browserMenuOpen = false
                                                },
                                            )
                                        }
                                    }
                                }
                            }
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Text(store.t("privateSessions"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                                Switch(checked = editPrivate, onCheckedChange = { editPrivate = it })
                            }
                            Text("AGENTS.md", color = p.text2, fontSize = PebbleFont.secondary)
                            if (loading && detail == null) {
                                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
                            } else {
                                OutlinedTextField(
                                    value = editInst,
                                    onValueChange = { editInst = it },
                                    placeholder = { Text(store.t("agentsPh"), fontFamily = MonoFont) },
                                    textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
                                    minLines = 5,
                                    maxLines = 16,
                                    shape = PebbleDimens.rowShape,
                                    colors = pebbleFieldColors(),
                                    modifier = Modifier.fillMaxWidth(),
                                )
                            }
                        }
                    }
                }
            }

            // Danger actions
            SectionCard {
                Column(modifier = Modifier.fillMaxWidth()) {
                    Text(
                        text = store.t("resetSessionBtn"),
                        color = p.text,
                        fontSize = PebbleFont.cardTitle,
                        fontWeight = FontWeight.Medium,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable {
                                confirm = ConfirmItem(
                                    title = store.t("resetSessionTitle"),
                                    message = store.t("resetSessionDesc", mapOf("name" to (bot?.displayName ?: botId))),
                                ) {
                                    scope.launch {
                                        try {
                                            store.resetBot(botId)
                                            onClose()
                                            runCatching { store.loadMessages(botId) }
                                        } catch (e: Throwable) {
                                            err = errText(e, store.t("resetFailed"))
                                        }
                                    }
                                }
                            }
                            .padding(horizontal = 16.dp, vertical = 14.dp),
                    )
                    if (store.bots.size > 1) {
                        CardDivider()
                        Text(
                            text = store.t("deleteBotBtn"),
                            color = p.danger,
                            fontSize = PebbleFont.cardTitle,
                            fontWeight = FontWeight.Medium,
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    confirm = ConfirmItem(
                                        title = store.t("deleteBotTitle", mapOf("name" to (bot?.displayName ?: botId))),
                                        message = store.t("deleteBotDesc"),
                                    ) {
                                        scope.launch {
                                            try {
                                                store.deleteBot(botId)
                                                onClose()
                                                onDeletedBot()
                                            } catch (e: Throwable) {
                                                err = errText(e, store.t("delFailed"))
                                            }
                                        }
                                    }
                                }
                                .padding(horizontal = 16.dp, vertical = 14.dp),
                        )
                    }
                }
            }

            Spacer(Modifier.height(16.dp))
        }
    }
}

@Composable
private fun DetailRow(label: String, value: String) {
    val p = LocalPalette.current
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, color = p.text2, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
        Text(value, color = p.text, fontSize = PebbleFont.body, fontWeight = FontWeight.Medium)
    }
}

@Composable
fun MemoryItemRow(
    store: PebbleStore,
    item: MemoryItem,
    showTags: Boolean = false,
    showScope: Boolean = false,
    onDelete: () -> Unit,
) {
    val p = LocalPalette.current
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp),
        verticalAlignment = Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(item.kind ?: "memory", color = p.sageInk, fontSize = PebbleFont.secondary, fontWeight = FontWeight.SemiBold)
                if (showScope && !item.scope.isNullOrEmpty()) {
                    Text(
                        text = if (item.scope == "everywhere" || item.scope == "global") store.t("memScopeEverywhere") else store.t("memScopeProject"),
                        color = p.mute,
                        fontSize = PebbleFont.caption,
                    )
                }
                if (showTags) {
                    item.tags.orEmpty().filterNot { it.startsWith("bot:") }.forEach { tag ->
                        Text("#$tag", color = p.mute, fontSize = PebbleFont.caption)
                    }
                }
            }
            SelectionContainer {
                Text(item.text.orEmpty(), color = p.text, fontSize = PebbleFont.body)
            }
        }
        TextButton(
            onClick = onDelete,
            contentPadding = PaddingValues(horizontal = 6.dp, vertical = 2.dp),
        ) {
            Text(store.t("delete"), color = p.danger, fontSize = PebbleFont.secondary)
        }
    }
}

@Composable
private fun RoutinesBlock(
    store: PebbleStore,
    botId: String,
    onCreate: () -> Unit,
    onEdit: (RoutineItem) -> Unit,
    onError: (String) -> Unit,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val list = store.routines[botId].orEmpty()
    var runsOpen by remember(botId) { mutableStateOf("") }
    val runsMap = remember(botId) { mutableStateMapOf<String, List<RoutineRun>>() }
    var confirm by remember(botId) { mutableStateOf<ConfirmItem?>(null) }

    ConfirmAlertDialog(item = confirm, onDismiss = { confirm = null })

    SectionHeader(
        title = store.t("routines"),
        trailing = {
            IconButton(onClick = onCreate, modifier = Modifier.size(28.dp)) {
                Icon(painterResource(PebbleIcons.Add), contentDescription = store.t("addShort"), tint = p.accent, modifier = Modifier.size(20.dp))
            }
        },
    )

    SectionCard {
        Column(
            modifier = Modifier.padding(14.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (list.isEmpty()) {
                Text(store.t("rtEmpty"), color = p.mute, fontSize = PebbleFont.secondary)
            } else {
                list.forEachIndexed { idx, rt ->
                    if (idx > 0) CardDivider()
                    val trig = listOfNotNull(
                        (rt.everyMinutes ?: 0).takeIf { it > 0 }?.let { store.t("rtEveryShort", mapOf("n" to it)) },
                        rt.dailyAt?.takeIf { it.isNotEmpty() }?.let { store.t("rtDailyShort", mapOf("time" to it)) },
                        rt.cron?.takeIf { it.isNotEmpty() }?.let { "Cron $it" },
                        if (rt.webhookTrigger == true) store.t("rtWebhookTag") else null,
                    ).joinToString(" · ")
                    val deliv = when (rt.mainDelivery) {
                        "notify" -> store.t("rtDeliveryNotify")
                        "none" -> store.t("rtDeliveryNone")
                        else -> store.t("rtDeliveryReceipt")
                    }
                    val running = rt.running == true
                    val statusLabel = when {
                        running -> store.t("rtStatusRunning")
                        rt.lastRun?.status == "ok" -> store.t("rtStatusOk")
                        rt.lastRun?.status == "silent" -> store.t("rtStatusSilent")
                        rt.lastRun?.status == "error" -> store.t("rtStatusError")
                        else -> ""
                    }
                    val meta = listOfNotNull(
                        trig.ifEmpty { null },
                        deliv,
                        if (!rt.webhookUrl.isNullOrEmpty()) store.t("rtWebhookPushTag") else null,
                        rt.model?.takeIf { it.isNotEmpty() },
                    ).joinToString(" · ")
                    val isOpen = runsOpen == rt.id
                    var menuOpen by remember { mutableStateOf(false) }

                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                                    Text(rt.name ?: rt.id, color = p.text, fontSize = PebbleFont.cardTitle, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    if (statusLabel.isNotEmpty()) {
                                        Text(
                                            text = statusLabel,
                                            color = if (rt.lastRun?.status == "error" && !running) p.danger else p.sageInk,
                                            fontSize = PebbleFont.secondary,
                                            fontWeight = FontWeight.SemiBold,
                                        )
                                    }
                                }
                                if (meta.isNotEmpty()) {
                                    Text(meta, color = p.text2, fontSize = PebbleFont.secondary)
                                }
                            }
                            Switch(
                                checked = rt.enabled == true,
                                onCheckedChange = { enabled ->
                                    scope.launch {
                                        runCatching { store.updateRoutine(botId, rt.id, mapOf("enabled" to enabled)) }
                                            .onFailure { onError(errText(it, store.t("rtStatusUpdateFailed"))) }
                                    }
                                },
                            )
                        }
                        Text(rt.prompt.orEmpty(), color = p.text, fontSize = PebbleFont.body, maxLines = 4, overflow = TextOverflow.Ellipsis)
                        Row(
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            OutlinedButton(
                                onClick = {
                                    scope.launch {
                                        runCatching { store.runRoutine(botId, rt.id) }
                                            .onFailure { onError(errText(it, store.t("rtRunFailed"))) }
                                    }
                                },
                                enabled = !running,
                                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                            ) {
                                Text(if (running) store.t("rtRunningBtn") else store.t("rtRunNow"), fontSize = PebbleFont.secondary, color = p.text)
                            }
                            OutlinedButton(
                                onClick = {
                                    if (isOpen) {
                                        runsOpen = ""
                                    } else {
                                        runsOpen = rt.id
                                        scope.launch {
                                            runCatching { store.fetchRoutineRuns(botId, rt.id, limit = 6) }
                                                .getOrNull()?.let { runsMap[rt.id] = it }
                                        }
                                    }
                                },
                                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
                            ) {
                                Text(if (isOpen) store.t("rtRunsHide") else store.t("rtRunsBtn"), fontSize = PebbleFont.secondary, color = p.text)
                            }
                            Box {
                                IconButton(onClick = { menuOpen = true }, modifier = Modifier.size(32.dp)) {
                                    Icon(painterResource(PebbleIcons.MoreVert), contentDescription = store.t("moreActions"), tint = p.text2)
                                }
                                DropdownMenu(
                                    expanded = menuOpen,
                                    onDismissRequest = { menuOpen = false },
                                    containerColor = p.card,
                                ) {
                                    DropdownMenuItem(
                                        text = { Text(store.t("edit"), color = p.text) },
                                        onClick = {
                                            menuOpen = false
                                            onEdit(rt)
                                        },
                                    )
                                    val path = rt.webhookPath
                                    if (rt.webhookTrigger == true && !path.isNullOrEmpty()) {
                                        DropdownMenuItem(
                                            text = { Text(store.t("rtCopyWebhook"), color = p.text) },
                                            onClick = {
                                                menuOpen = false
                                                val base = store.baseURL.trimEnd('/')
                                                copyToClipboard(context, "$base$path")
                                            },
                                        )
                                    }
                                    HorizontalDivider(color = p.s2)
                                    DropdownMenuItem(
                                        text = { Text(store.t("delete"), color = p.danger) },
                                        onClick = {
                                            menuOpen = false
                                            confirm = ConfirmItem(
                                                title = store.t("rtDeleteTitle", mapOf("name" to (rt.name ?: rt.id))),
                                                message = store.t("rtDeleteDesc"),
                                            ) {
                                                scope.launch { runCatching { store.deleteRoutine(botId, rt.id) } }
                                            }
                                        },
                                    )
                                }
                            }
                        }

                        if (isOpen) {
                            val listRuns = runsMap[rt.id].orEmpty()
                            if (listRuns.isEmpty()) {
                                Text(store.t("rtRunsEmpty"), color = p.mute, fontSize = PebbleFont.secondary)
                            } else {
                                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                    listRuns.forEach { r ->
                                        val rs = when (r.status) {
                                            "ok" -> store.t("rtStatusOk")
                                            "silent" -> store.t("rtStatusSilent")
                                            else -> store.t("rtStatusError")
                                        }
                                        Column(
                                            modifier = Modifier
                                                .fillMaxWidth()
                                                .clip(RoundedCornerShape(10.dp))
                                                .background(p.s1)
                                                .padding(10.dp),
                                            verticalArrangement = Arrangement.spacedBy(2.dp),
                                        ) {
                                            Text(
                                                text = "$rs · ${r.trigger.orEmpty()} · ${String.format(Locale.US, "%.1fs", (r.durationMs ?: 0.0) / 1000.0)}",
                                                color = p.text2,
                                                fontSize = PebbleFont.caption,
                                            )
                                            if (!r.error.isNullOrEmpty()) {
                                                Text(r.error, color = p.danger, fontSize = PebbleFont.secondary)
                                            } else if (!r.output.isNullOrEmpty()) {
                                                Text(r.output, color = p.text, fontSize = PebbleFont.secondary, maxLines = 3, overflow = TextOverflow.Ellipsis)
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

private enum class RoutineMode { INTERVAL, DAILY, CRON, WEBHOOK }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RoutineEditorSheet(
    store: PebbleStore,
    botId: String,
    routine: RoutineItem?,
    onDismiss: () -> Unit,
) {
    val p = LocalPalette.current
    val scope = rememberCoroutineScope()

    val hasEvery = (routine?.everyMinutes ?: 0) > 0
    val hasDaily = !routine?.dailyAt.isNullOrEmpty()
    val hasCron = !routine?.cron.isNullOrEmpty()
    val initialMode = when {
        hasCron -> RoutineMode.CRON
        hasDaily && !hasEvery -> RoutineMode.DAILY
        !hasEvery && !hasDaily && routine?.webhookTrigger == true -> RoutineMode.WEBHOOK
        else -> RoutineMode.INTERVAL
    }

    var name by remember { mutableStateOf(routine?.name.orEmpty()) }
    var mode by remember { mutableStateOf(initialMode) }
    var every by remember { mutableStateOf(if (hasEvery) "${routine?.everyMinutes}" else "60") }
    var daily by remember { mutableStateOf(if (hasDaily) routine.dailyAt.orEmpty() else "09:00") }
    var cron by remember { mutableStateOf(if (hasCron) routine.cron.orEmpty() else "0 9 * * 1-5") }
    var webhookTrigger by remember { mutableStateOf(routine?.webhookTrigger == true || initialMode == RoutineMode.WEBHOOK) }
    var secret by remember { mutableStateOf("") }
    val secretSet = routine?.webhookSecretSet == true
    var clearSecret by remember { mutableStateOf(false) }
    var webhookUrl by remember { mutableStateOf(routine?.webhookUrl.orEmpty()) }
    var delivery by remember { mutableStateOf(routine?.mainDelivery ?: "receipt") }
    var prompt by remember { mutableStateOf(routine?.prompt.orEmpty()) }
    var enabled by remember { mutableStateOf(routine?.enabled ?: true) }
    var saving by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf("") }

    fun save() {
        val n = name.trim()
        val pr = prompt.trim()
        if (n.isEmpty()) { err = store.t("rtNameRequired"); return }
        if (pr.isEmpty()) { err = store.t("rtPromptRequired"); return }
        val everyMinutes = if (mode == RoutineMode.INTERVAL) (every.trim().toIntOrNull() ?: 0) else 0
        val dailyAt = if (mode == RoutineMode.DAILY) daily.trim() else ""
        val cronExpr = if (mode == RoutineMode.CRON) cron.trim() else ""
        if (mode == RoutineMode.INTERVAL && everyMinutes <= 0) { err = store.t("rtEveryPositive"); return }
        if (mode == RoutineMode.DAILY && !Regex("^\\d{2}:\\d{2}$").matches(dailyAt)) { err = store.t("rtTimeFormatErr"); return }
        if (mode == RoutineMode.CRON && cronExpr.isEmpty()) { err = store.t("rtCronRequired"); return }

        err = ""
        val body = mutableMapOf<String, Any?>(
            "name" to n,
            "everyMinutes" to everyMinutes,
            "dailyAt" to dailyAt,
            "cron" to cronExpr,
            "webhookTrigger" to (mode == RoutineMode.WEBHOOK || webhookTrigger),
            "webhookUrl" to webhookUrl.trim(),
            "prompt" to pr,
            "mainDelivery" to delivery,
            "enabled" to enabled,
        )
        val sec = secret.trim()
        if (clearSecret) body["clearWebhookSecret"] = true else if (sec.isNotEmpty()) body["webhookSecret"] = sec

        saving = true
        scope.launch {
            try {
                if (routine != null) store.updateRoutine(botId, routine.id, body)
                else store.createRoutine(botId, body)
                onDismiss()
            } catch (e: Throwable) {
                err = errText(e, store.t("rtSaveFailed"))
            } finally {
                saving = false
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
                text = routine?.name ?: store.t("routines"),
                color = p.text,
                fontSize = PebbleFont.title,
                fontWeight = FontWeight.Bold,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f),
            )
            if (saving) {
                CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.accent)
            } else {
                TextButton(onClick = { save() }) {
                    Text(store.t("rtSaveBtn"), color = p.accent, fontWeight = FontWeight.SemiBold)
                }
            }
        }

        if (err.isNotEmpty()) StatusTextBanner(text = err, error = true)

        OutlinedTextField(
            value = name,
            onValueChange = { name = it },
            placeholder = { Text(store.t("rtNamePh")) },
            singleLine = true,
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
            RoutineMode.entries.forEachIndexed { idx, m ->
                val lbl = when (m) {
                    RoutineMode.INTERVAL -> store.t("rtModeInterval")
                    RoutineMode.DAILY -> store.t("rtModeDaily")
                    RoutineMode.CRON -> store.t("rtModeCron")
                    RoutineMode.WEBHOOK -> store.t("rtModeWebhook")
                }
                SegmentedButton(
                    selected = mode == m,
                    onClick = {
                        mode = m
                        if (m == RoutineMode.WEBHOOK) webhookTrigger = true
                    },
                    shape = SegmentedButtonDefaults.itemShape(idx, RoutineMode.entries.size),
                    label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                )
            }
        }

        when (mode) {
            RoutineMode.INTERVAL -> OutlinedTextField(
                value = every,
                onValueChange = { every = it },
                placeholder = { Text(store.t("rtEveryPh")) },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            RoutineMode.DAILY -> OutlinedTextField(
                value = daily,
                onValueChange = { daily = it },
                placeholder = { Text(store.t("rtDailyPh")) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            RoutineMode.CRON -> OutlinedTextField(
                value = cron,
                onValueChange = { cron = it },
                placeholder = { Text(store.t("rtCronPh")) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.body),
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            RoutineMode.WEBHOOK -> Unit
        }

        if (mode != RoutineMode.WEBHOOK) {
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(store.t("rtAllowWebhook"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                Switch(checked = webhookTrigger, onCheckedChange = { webhookTrigger = it })
            }
        }

        if (mode == RoutineMode.WEBHOOK || webhookTrigger) {
            OutlinedTextField(
                value = secret,
                onValueChange = { secret = it },
                enabled = !clearSecret,
                placeholder = {
                    Text(if (secretSet && !clearSecret) store.t("rtWebhookSecretSetPh") else store.t("rtWebhookSecretPh"))
                },
                visualTransformation = PasswordVisualTransformation(),
                singleLine = true,
                shape = PebbleDimens.rowShape,
                colors = pebbleFieldColors(),
                modifier = Modifier.fillMaxWidth(),
            )
            if (secretSet) {
                Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text(store.t("rtWebhookClearSecret"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
                    Switch(checked = clearSecret, onCheckedChange = { clearSecret = it; if (it) secret = "" })
                }
            }
        }

        OutlinedTextField(
            value = webhookUrl,
            onValueChange = { webhookUrl = it },
            placeholder = { Text(store.t("rtWebhookUrlPh"), fontFamily = MonoFont) },
            singleLine = true,
            textStyle = TextStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary),
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        val delivs = listOf(
            "receipt" to store.t("rtDeliveryReceipt"),
            "notify" to store.t("rtDeliveryNotify"),
            "none" to store.t("rtDeliveryNone"),
        )
        SingleChoiceSegmentedButtonRow(modifier = Modifier.fillMaxWidth()) {
            delivs.forEachIndexed { idx, (key, lbl) ->
                SegmentedButton(
                    selected = delivery == key,
                    onClick = { delivery = key },
                    shape = SegmentedButtonDefaults.itemShape(idx, delivs.size),
                    label = { Text(lbl, fontSize = PebbleFont.secondary, maxLines = 1) },
                )
            }
        }

        OutlinedTextField(
            value = prompt,
            onValueChange = { prompt = it },
            placeholder = { Text(store.t("rtPromptPh")) },
            minLines = 3,
            maxLines = 10,
            shape = PebbleDimens.rowShape,
            colors = pebbleFieldColors(),
            modifier = Modifier.fillMaxWidth(),
        )

        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(store.t("rtEnabled"), color = p.text, fontSize = PebbleFont.body, modifier = Modifier.weight(1f))
            Switch(checked = enabled, onCheckedChange = { enabled = it })
        }

        Spacer(Modifier.height(16.dp))
    }
}
