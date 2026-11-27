package ai.pebble.android.ui.desktop

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ai.pebble.android.data.PebbleStore
import ai.pebble.android.data.StageKind
import ai.pebble.android.data.StageTab
import ai.pebble.android.data.TakeoverRequest
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.PebbleIcons
import kotlin.math.abs
import kotlin.math.hypot

@Composable
internal fun StageDesktopCanvas(
    store: PebbleStore,
    target: String,
    preset: String,
    streaming: Boolean,
    isStarted: Boolean,
    streamMode: DesktopStreamMode,
    wsUrl: String,
    fallbackBitmap: ImageBitmap?,
    streamMeta: DesktopStreamMeta?,
    streamConnected: Boolean,
    streamCtrl: H264StreamController,
    displayErr: String,
    activeTakeover: TakeoverRequest?,
    showText: Boolean,
    draft: String,
    onDraftChange: (String) -> Unit,
    onCloseText: () -> Unit,
    onSendText: () -> Unit,
    onSendInput: (Map<String, Any?>) -> Unit,
    onSwitchBackLocal: () -> Unit,
    onResolveTakeover: (String) -> Unit,
    onMeta: (DesktopStreamMeta) -> Unit,
    onStreamStatus: (Boolean, Int) -> Unit,
    onStreamError: (String, String) -> Unit,
    onRetryConnect: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val stageBg = Color.Black
    val pillBg = Color(0xFF1F1D1B)
    val pillText = Color(0xFFF3F1EE)
    val pillMute = Color(0xFFA6A09A)

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(stageBg),
    ) {
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
                    onClick = onSwitchBackLocal,
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
                    onClick = { onResolveTakeover(activeTakeover.id) },
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
                                    if (abs(dy) > 2f) {
                                        onSendInput(mapOf("kind" to "scroll", "deltaY" to (dy / 12f).toInt().coerceIn(-10, 10)))
                                    }
                                } else {
                                    val ch = pressed.first()
                                    lastPos = ch.position
                                    if (!isScroll && !isDragging && hypot(lastPos.x - startPos.x, lastPos.y - startPos.y) >= 14f) {
                                        isDragging = true
                                        mapPoint(startPos)?.let { (lx, ly) ->
                                            onSendInput(mapOf("kind" to "down", "x" to lx, "y" to ly, "button" to "left"))
                                        }
                                    } else if (isDragging) {
                                        val now = System.currentTimeMillis()
                                        if (now - lastMoveSent >= 16L) {
                                            lastMoveSent = now
                                            mapPoint(lastPos)?.let { (lx, ly) ->
                                                onSendInput(mapOf("kind" to "move", "x" to lx, "y" to ly))
                                            }
                                        }
                                    }
                                }
                            }

                            if (isDragging) {
                                mapPoint(lastPos)?.let { (lx, ly) ->
                                    onSendInput(mapOf("kind" to "up", "x" to lx, "y" to ly, "button" to "left"))
                                }
                            } else if (!isScroll) {
                                val kind = if (System.currentTimeMillis() - downTime > 500L) "right_click" else "click"
                                mapPoint(startPos)?.let { (lx, ly) ->
                                    onSendInput(mapOf("kind" to kind, "x" to lx, "y" to ly))
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
                            active = streaming && isStarted,
                            controller = streamCtrl,
                            onMeta = onMeta,
                            onStatus = onStreamStatus,
                            onStreamError = onStreamError,
                            modifier = Modifier.fillMaxSize(),
                        )
                    }
                }

                val isReady = if (streamMode == DesktopStreamMode.FRAME_POLLING) fallbackBitmap != null else streamConnected
                if (!isReady) {
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
                                onClick = onRetryConnect,
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
                    IconButton(onClick = onCloseText) {
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
                            onValueChange = onDraftChange,
                            singleLine = true,
                            textStyle = TextStyle(color = pillText, fontSize = PebbleFont.body),
                            cursorBrush = SolidColor(pillText),
                            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                            keyboardActions = KeyboardActions(onSend = { onSendText() }),
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                }
                Surface(
                    color = if (draft.isNotEmpty()) pillText else pillBg,
                    shape = CircleShape,
                    modifier = Modifier.size(40.dp),
                ) {
                    IconButton(onClick = onSendText, enabled = draft.isNotEmpty()) {
                        Icon(
                            painter = painterResource(PebbleIcons.ArrowUpward),
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

@Composable
internal fun StageExtensionPane(
    store: PebbleStore,
    tab: StageTab,
    inWideStage: Boolean,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val kindName = when (tab.kind) {
        StageKind.BROWSER -> store.t("stageExtensionKind_browser")
        StageKind.ARTIFACT -> store.t("stageExtensionKind_artifact")
        StageKind.MCP_APP -> store.t("stageExtensionKind_mcp_app")
        else -> tab.rawKind
    }
    val sub = tab.subtitle ?: tab.url ?: tab.resourceUri ?: tab.path

    val body: @Composable () -> Unit = {
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(24.dp),
            contentAlignment = Alignment.Center,
        ) {
            Column(
                modifier = Modifier.widthIn(max = 420.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Icon(
                    painter = painterResource(StageStateOps.iconFor(tab.kind)),
                    contentDescription = null,
                    tint = p.mute,
                    modifier = Modifier.size(36.dp),
                )
                Text(
                    text = tab.title,
                    color = p.text,
                    fontSize = PebbleFont.title,
                    fontWeight = FontWeight.Bold,
                    textAlign = TextAlign.Center,
                )
                if (!sub.isNullOrBlank()) {
                    Text(
                        text = sub,
                        color = p.text2,
                        fontSize = PebbleFont.caption,
                        textAlign = TextAlign.Center,
                        maxLines = 2,
                        overflow = TextOverflow.MiddleEllipsis,
                    )
                }
                Text(
                    text = store.t("stagePlaceholderDesc", mapOf("kind" to kindName)),
                    color = p.text2,
                    fontSize = PebbleFont.secondary,
                    textAlign = TextAlign.Center,
                )
            }
        }
    }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(if (inWideStage) p.card else p.page),
    ) {
        body()
    }
}
