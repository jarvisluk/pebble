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
import ai.pebble.android.data.BotStatus
import ai.pebble.android.data.BotTask
import ai.pebble.android.data.HubException
import ai.pebble.android.data.MemoryItem
import ai.pebble.android.data.MemoryList
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.RoutineItem
import ai.pebble.android.data.errText
import ai.pebble.android.data.resolveDefaultModel
import ai.pebble.android.ui.settings.ModelInputWithMenu
import ai.pebble.android.ui.settings.localizedMemoryKind
import ai.pebble.android.ui.settings.settingsHarnessList
import ai.pebble.android.ui.chat.BotTasksBlock
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
    onOpenTask: (BotTask) -> Unit,
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
        runCatching { store.fetchBotTasks(botId) }
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

            // Tasks & Activity
            BotTasksBlock(
                store = store,
                botId = botId,
                onSelectTask = onOpenTask,
                onError = { err = it },
            )

            Spacer(Modifier.height(16.dp))
        }
    }
}
