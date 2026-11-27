package ai.pebble.android.ui.chat

import android.content.Intent
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
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
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.FileProvider
import ai.pebble.android.data.ChatMessage
import ai.pebble.android.data.MessageMedia
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.countToolCalls
import ai.pebble.android.data.formatClockTime
import ai.pebble.android.data.formatDurationMs
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebbleDimens
import android.graphics.BitmapFactory
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File
import java.util.Locale
import androidx.compose.ui.res.painterResource
import ai.pebble.android.data.StageRef
import ai.pebble.android.ui.theme.PebbleIcons

@Composable
fun MessageBubble(
    store: PebbleStore,
    msg: ChatMessage,
    soft: Color,
    onPreviewMedia: (MessageMedia) -> Unit,
    modifier: Modifier = Modifier,
    showCollabHeader: Boolean = true,
    bubbleColor: Color? = null,
    botId: String = "",
    inSidebar: Boolean = false,
    onOpenStageRef: ((StageRef) -> Unit)? = null,
) {
    val p = LocalPalette.current
    val bubbleShape = if (inSidebar) RoundedCornerShape(16.dp) else PebbleDimens.bubbleShape
    val hPad = if (inSidebar) 12.dp else 16.dp
    val vPad = if (inSidebar) 9.dp else 10.dp

    when (msg.role) {
        "user" -> {
            Row(
                modifier = modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.End,
            ) {
                Spacer(Modifier.weight(if (inSidebar) 0.1f else 0.15f, fill = false))
                Surface(
                    color = if (inSidebar && p.dark) p.s2 else soft,
                    shape = bubbleShape,
                    modifier = Modifier.widthIn(max = 560.dp),
                ) {
                    Column(
                        modifier = Modifier.padding(horizontal = hPad, vertical = vPad),
                        horizontalAlignment = Alignment.End,
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        if (msg.body.isNotEmpty()) {
                            SelectionContainer {
                                Text(
                                    text = msg.body,
                                    color = p.text,
                                    fontSize = PebbleFont.body,
                                    lineHeight = 22.sp,
                                )
                            }
                        }
                        MediaGrid(store = store, media = msg.media.orEmpty(), onPreviewMedia = onPreviewMedia)
                    }
                }
            }
        }
        "assistant" -> {
            val clock = formatClockTime(msg.createdAt)
            val toolCalls = countToolCalls(msg.tools)
            val dur = formatDurationMs(msg.durationMs)
            val meta = buildList {
                if (clock.isNotEmpty()) add(clock)
                if (toolCalls > 0) add(store.t("msgToolCalls", mapOf("count" to toolCalls)))
                if (dur.isNotEmpty()) add(store.t("msgDuration", mapOf("dur" to dur)))
                val cost = msg.costUsd ?: 0.0
                if (cost > 0.0) add(String.format(Locale.US, "$%.4f", cost))
            }

            Surface(
                modifier = modifier
                    .padding(end = if (inSidebar) 8.dp else 24.dp)
                    .widthIn(max = 680.dp),
                color = if (inSidebar) p.s1 else p.card,
                shape = bubbleShape,
            ) {
                Column(
                    modifier = Modifier.padding(horizontal = hPad, vertical = vPad),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    val from = msg.from.orEmpty()
                    if (from.startsWith("routine:")) {
                        Text(
                            text = store.t("routineReceiptFrom", mapOf("name" to from.removePrefix("routine:"))),
                            color = p.accent,
                            fontSize = PebbleFont.secondary,
                            fontWeight = FontWeight.SemiBold,
                        )
                    }
                    if (msg.body.isNotEmpty()) {
                        MarkdownText(text = msg.body)
                    }
                    MediaGrid(store = store, media = msg.media.orEmpty(), onPreviewMedia = onPreviewMedia)
                    if (meta.isNotEmpty()) {
                        Text(
                            text = meta.joinToString(" · "),
                            color = p.mute,
                            fontSize = PebbleFont.secondary,
                        )
                    }
                }
            }
        }
        "bot_out", "bot_in" -> {
            val isOut = msg.role == "bot_out"
            val rawPeer = (if (isOut) msg.to else msg.from).orEmpty().removePrefix("bot:")
            val senderId = if (isOut) botId.ifEmpty { rawPeer } else rawPeer
            val senderIdx = store.bots.indexOfFirst { it.id == senderId }.coerceAtLeast(0)
            val senderBot = store.bots.getOrNull(senderIdx)
            val senderPal = p.bot(senderId, senderIdx, senderBot?.color)
            Surface(
                modifier = modifier
                    .padding(
                        start = if (isOut) 24.dp else 0.dp,
                        end = if (isOut) 0.dp else 24.dp,
                    )
                    .widthIn(max = 680.dp),
                color = bubbleColor ?: senderPal.collabBg,
                shape = PebbleDimens.bubbleShape,
            ) {
                Column(
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    if (showCollabHeader) {
                        val peerDisplay = store.bots.firstOrNull { it.id == rawPeer }?.displayName ?: rawPeer
                        val label = if (isOut) {
                            store.t("delegateTo", mapOf("peer" to peerDisplay))
                        } else {
                            store.t("fromPeer", mapOf("peer" to peerDisplay))
                        }
                        Text(
                            text = label,
                            color = senderPal.collabInk,
                            fontSize = PebbleFont.secondary,
                            fontWeight = FontWeight.SemiBold,
                        )
                    }
                    if (msg.body.isNotEmpty()) {
                        MarkdownText(text = msg.body)
                    }
                    MediaGrid(store = store, media = msg.media.orEmpty(), onPreviewMedia = onPreviewMedia)
                }
            }
        }
        "stage" -> {
            val st = msg.stage
            if (st != null) {
                Row(
                    modifier = modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.Start,
                ) {
                    StageJumpCard(
                        store = store,
                        ref = st,
                        inSidebar = inSidebar,
                        onOpen = { onOpenStageRef?.invoke(st) },
                    )
                }
            }
        }
        else -> {
            if (msg.from == "command") {
                Surface(
                    modifier = modifier
                        .padding(end = if (inSidebar) 8.dp else 24.dp)
                        .widthIn(max = 680.dp),
                    color = if (inSidebar) p.s1 else p.card,
                    shape = bubbleShape,
                ) {
                    SelectionContainer {
                        Text(
                            text = msg.body,
                            fontFamily = MonoFont,
                            fontSize = PebbleFont.body,
                            color = p.text,
                            modifier = Modifier.padding(horizontal = hPad, vertical = vPad),
                        )
                    }
                }
            } else {
                SelectionContainer {
                    Text(
                        text = msg.body,
                        color = if (msg.from == "error") p.danger else p.mute,
                        fontSize = PebbleFont.secondary,
                        textAlign = TextAlign.Center,
                        modifier = modifier.fillMaxWidth(),
                    )
                }
            }
        }
    }
}

@Composable
private fun MediaGrid(
    store: PebbleStore,
    media: List<MessageMedia>,
    onPreviewMedia: (MessageMedia) -> Unit,
) {
    if (media.isEmpty()) return
    val images = media.filter { it.isImage }
    val files = media.filterNot { it.isImage }

    if (images.isNotEmpty()) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            images.chunked(2).forEach { row ->
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    row.forEach { img ->
                        AuthMediaImage(
                            store = store,
                            path = img.url,
                            modifier = Modifier
                                .then(
                                    if (images.size == 1) Modifier.widthIn(max = 280.dp)
                                    else Modifier.weight(1f)
                                )
                                .height(180.dp)
                                .clip(RoundedCornerShape(12.dp))
                                .clickable { onPreviewMedia(img) },
                        )
                    }
                }
            }
        }
    }

    if (files.isNotEmpty()) {
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            files.forEach { file ->
                FileAttachmentRow(store = store, file = file)
            }
        }
    }
}

@Composable
fun AuthMediaImage(
    store: PebbleStore,
    path: String,
    modifier: Modifier = Modifier,
    contentScale: ContentScale = ContentScale.Crop,
) {
    val p = LocalPalette.current
    var bitmap by remember(path) { mutableStateOf<ImageBitmap?>(null) }

    LaunchedEffect(path) {
        bitmap = withContext(Dispatchers.IO) {
            runCatching {
                val bytes = store.fetchMediaBytes(path)
                BitmapFactory.decodeByteArray(bytes, 0, bytes.size)?.asImageBitmap()
            }.getOrNull()
        }
    }

    val loaded = bitmap
    if (loaded != null) {
        Image(
            bitmap = loaded,
            contentDescription = null,
            contentScale = contentScale,
            modifier = modifier,
        )
    } else {
        Box(
            modifier = modifier.background(p.s1),
            contentAlignment = Alignment.Center,
        ) {
            CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.mute)
        }
    }
}

@Composable
private fun FileAttachmentRow(
    store: PebbleStore,
    file: MessageMedia,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var loading by remember { mutableStateOf(false) }
    var errMsg by remember { mutableStateOf("") }

    val displayName = file.name?.trim()?.takeIf { it.isNotEmpty() } ?: file.id
    val sizeLabel = remember(file.size) {
        val bytes = file.size ?: 0L
        when {
            bytes <= 0L -> ""
            bytes < 1024L -> "$bytes B"
            bytes < 1024L * 1024L -> String.format(Locale.US, "%.1f KB", bytes / 1024.0)
            else -> String.format(Locale.US, "%.1f MB", bytes / (1024.0 * 1024.0))
        }
    }

    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(p.s1)
                .clickable(enabled = !loading) {
                    loading = true
                    errMsg = ""
                    scope.launch {
                        try {
                            val data = store.fetchMediaBytes(file.url)
                            val uri = withContext(Dispatchers.IO) {
                                val dir = File(context.cacheDir, "pebble-files-${file.id}").apply { mkdirs() }
                                val safeName = File(displayName).name.ifEmpty { file.id }
                                val dst = File(dir, safeName)
                                dst.writeBytes(data)
                                FileProvider.getUriForFile(context, "${context.packageName}.fileprovider", dst)
                            }
                            val share = Intent(Intent.ACTION_SEND).apply {
                                type = file.mimeType ?: "*/*"
                                putExtra(Intent.EXTRA_STREAM, uri)
                                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                            }
                            context.startActivity(Intent.createChooser(share, displayName))
                        } catch (_: Throwable) {
                            errMsg = store.t("downloadFailed")
                        } finally {
                            loading = false
                        }
                    }
                }
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(painterResource(PebbleIcons.Description), contentDescription = null, tint = p.text2, modifier = Modifier.size(18.dp))
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(1.dp)) {
                Text(
                    text = displayName,
                    color = p.text,
                    fontSize = PebbleFont.secondary,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (sizeLabel.isNotEmpty()) {
                    Text(sizeLabel, color = p.mute, fontSize = PebbleFont.caption)
                }
            }
            if (loading) {
                CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = p.mute)
            } else {
                Icon(painterResource(PebbleIcons.ArrowCircleDown), contentDescription = null, tint = p.mute, modifier = Modifier.size(20.dp))
            }
        }
        if (errMsg.isNotEmpty()) {
            Text(errMsg, color = p.danger, fontSize = PebbleFont.caption)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MediaPreviewDialog(
    store: PebbleStore,
    media: MessageMedia,
    onDismiss: () -> Unit,
) {
    var scale by remember { mutableFloatStateOf(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }

    Dialog(
        onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Scaffold(
            containerColor = Color.Black,
            topBar = {
                TopAppBar(
                    colors = TopAppBarDefaults.topAppBarColors(
                        containerColor = Color.Black.copy(alpha = 0.7f),
                        titleContentColor = Color.White,
                    ),
                    title = {
                        Text(
                            text = media.name ?: store.t("imageAltFallback"),
                            fontSize = PebbleFont.body,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    },
                    navigationIcon = {
                        IconButton(onClick = onDismiss) {
                            Icon(painterResource(PebbleIcons.Close), contentDescription = store.t("close"), tint = Color.White)
                        }
                    },
                )
            },
        ) { inner ->
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(inner)
                    .background(Color.Black)
                    .pointerInput(Unit) {
                        detectTransformGestures { _, pan, zoom, _ ->
                            val nextScale = (scale * zoom).coerceIn(1f, 5f)
                            scale = nextScale
                            offset = if (nextScale > 1f) offset + pan else Offset.Zero
                        }
                    },
                contentAlignment = Alignment.Center,
            ) {
                AuthMediaImage(
                    store = store,
                    path = media.url,
                    contentScale = ContentScale.Fit,
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(12.dp)
                        .graphicsLayer(
                            scaleX = scale,
                            scaleY = scale,
                            translationX = offset.x,
                            translationY = offset.y,
                        ),
                )
            }
        }
    }
}
