package ai.pebble.android.ui.desktop

import android.content.Context
import android.graphics.Matrix
import android.graphics.SurfaceTexture
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.view.Surface
import android.view.TextureView
import android.widget.FrameLayout
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import ai.pebble.android.data.ApiClient
import ai.pebble.android.data.SharedHttp
import ai.pebble.android.data.mapToJsonObject
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import org.json.JSONObject
import java.nio.ByteBuffer
import java.util.concurrent.atomic.AtomicInteger

data class DesktopStreamMeta(
    val imageWidth: Double = 1280.0,
    val imageHeight: Double = 720.0,
    val logicalX: Double = 0.0,
    val logicalY: Double = 0.0,
    val logicalWidth: Double = 1440.0,
    val logicalHeight: Double = 900.0,
    val preset: String = "",
    val bitrateKbps: Int = 0,
)

class H264StreamController {
    internal var surfaceView: H264NativeSurfaceView? = null

    fun sendInput(event: Map<String, Any?>) {
        surfaceView?.sendInput(mapToJsonObject(event).toString())
    }

    fun requestKeyframe() {
        surfaceView?.requestKeyframe()
    }
}

@Composable
fun H264SurfaceView(
    url: String,
    token: String,
    preset: String = "auto",
    active: Boolean = true,
    trackFps: Boolean = true,
    controller: H264StreamController? = null,
    onMeta: (DesktopStreamMeta) -> Unit = {},
    onStatus: (connected: Boolean, fps: Int) -> Unit = { _, _ -> },
    onStreamError: (code: String, error: String) -> Unit = { _, _ -> },
    modifier: Modifier = Modifier,
) {
    val internalCtrl = remember { controller ?: H264StreamController() }

    DisposableEffect(internalCtrl) {
        onDispose {
            internalCtrl.surfaceView?.stopAll()
            internalCtrl.surfaceView = null
        }
    }

    AndroidView(
        modifier = modifier,
        factory = { ctx ->
            H264NativeSurfaceView(ctx).also { view ->
                internalCtrl.surfaceView = view
                view.onMeta = onMeta
                view.onStatus = onStatus
                view.onStreamError = onStreamError
                view.updateConfig(url, token, preset, active, trackFps)
            }
        },
        update = { view ->
            internalCtrl.surfaceView = view
            view.onMeta = onMeta
            view.onStatus = onStatus
            view.onStreamError = onStreamError
            view.updateConfig(url, token, preset, active, trackFps)
        },
    )
}

internal class H264NativeSurfaceView(context: Context) : FrameLayout(context), TextureView.SurfaceTextureListener {
    var onMeta: ((DesktopStreamMeta) -> Unit)? = null
    var onStatus: ((Boolean, Int) -> Unit)? = null
    var onStreamError: ((String, String) -> Unit)? = null

    private val textureView = TextureView(context)
    private val mainHandler = Handler(Looper.getMainLooper())
    private val codecLock = Any()

    private var surface: Surface? = null
    private var ws: WebSocket? = null
    private var codec: MediaCodec? = null

    private var url: String = ""
    private var token: String = ""
    private var preset: String = "auto"
    private var active: Boolean = false
    private var trackFps: Boolean = true
    private var lastReportedConnected: Boolean? = null
    private var lastReportedFps: Int = -1

    private var vps: ByteArray? = null
    private var sps: ByteArray? = null
    private var pps: ByteArray? = null
    private var configuredCodec: String = ""
    private var configuredVps: ByteArray? = null
    private var configuredSps: ByteArray? = null
    private var configuredPps: ByteArray? = null
    private var configuredW = 0
    private var configuredH = 0
    @Volatile
    private var needKeyframe = true
    @Volatile
    private var hadFirstFrame = false
    private var reportedPreFrameError = false
    private var videoW = 0
    private var videoH = 0
    private var streamFps = 60
    private var ptsUs = 0L
    private val frameCounter = AtomicInteger(0)

    private fun reportStatus(connected: Boolean, fps: Int) {
        if (lastReportedConnected == connected && lastReportedFps == fps) return
        lastReportedConnected = connected
        lastReportedFps = fps
        onStatus?.invoke(connected, fps)
    }

    private fun emitStreamError(code: String, msg: String) {
        mainHandler.post {
            reportStatus(false, 0)
            if (!hadFirstFrame) {
                if (!reportedPreFrameError) {
                    reportedPreFrameError = true
                    onStreamError?.invoke(code, msg)
                }
            } else {
                onStreamError?.invoke(code, msg)
            }
        }
    }

    private val fpsRunnable = object : Runnable {
        override fun run() {
            val fps = frameCounter.getAndSet(0)
            if (ws != null && trackFps) {
                reportStatus(!needKeyframe, fps)
                mainHandler.postDelayed(this, 1000)
            }
        }
    }

    init {
        textureView.surfaceTextureListener = this
        addView(textureView, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
    }

    fun updateConfig(nextUrl: String, nextToken: String, nextPreset: String, nextActive: Boolean, nextTrackFps: Boolean = true) {
        val resolvedPreset = nextPreset.ifBlank { "auto" }
        val connChanged = url != nextUrl || token != nextToken || active != nextActive
        url = nextUrl
        token = nextToken
        active = nextActive
        if (trackFps != nextTrackFps) {
            trackFps = nextTrackFps
            mainHandler.removeCallbacks(fpsRunnable)
            if (trackFps && ws != null) {
                mainHandler.postDelayed(fpsRunnable, 1000)
            }
        }
        if (preset != resolvedPreset) {
            preset = resolvedPreset
            ws?.send(JSONObject().put("type", "profile").put("preset", resolvedPreset).toString())
        }
        if (connChanged) {
            reconnectIfNeeded()
        }
    }

    fun sendInput(eventJson: String) {
        val cur = ws ?: return
        val payload = JSONObject().put("type", "input").put("event", JSONObject(eventJson)).toString()
        cur.send(payload)
    }

    fun requestKeyframe() {
        needKeyframe = true
        ws?.send("""{"type":"keyframe"}""")
    }

    fun stopAll() {
        disconnect()
    }

    override fun onSurfaceTextureAvailable(st: SurfaceTexture, width: Int, height: Int) {
        surface = Surface(st)
        updateTextureTransform(width, height)
        reconnectIfNeeded()
    }

    override fun onSurfaceTextureSizeChanged(st: SurfaceTexture, width: Int, height: Int) {
        updateTextureTransform(width, height)
    }

    override fun onSurfaceTextureDestroyed(st: SurfaceTexture): Boolean {
        disconnect()
        surface?.release()
        surface = null
        return true
    }

    override fun onSurfaceTextureUpdated(st: SurfaceTexture) = Unit

    override fun onDetachedFromWindow() {
        disconnect()
        super.onDetachedFromWindow()
    }

    private fun updateTextureTransform(viewW: Int = textureView.width, viewH: Int = textureView.height) {
        if (viewW <= 0 || viewH <= 0 || videoW <= 0 || videoH <= 0) return
        val scale = minOf(viewW.toFloat() / videoW.toFloat(), viewH.toFloat() / videoH.toFloat())
        val sx = (videoW * scale) / viewW.toFloat()
        val sy = (videoH * scale) / viewH.toFloat()
        val m = Matrix()
        m.setScale(sx, sy, viewW / 2f, viewH / 2f)
        textureView.setTransform(m)
    }

    private fun reconnectIfNeeded() {
        disconnect()
        if (!active || url.isBlank() || surface == null) return

        val reqBuilder = ApiClient.attachClientHeaders(Request.Builder().url(url))
        if (token.isNotBlank()) {
            reqBuilder.addHeader("Authorization", "Bearer $token")
        }
        needKeyframe = true
        hadFirstFrame = false
        reportedPreFrameError = false
        frameCounter.set(0)
        lastReportedConnected = null
        lastReportedFps = -1
        if (trackFps) {
            mainHandler.postDelayed(fpsRunnable, 1000)
        }

        ws = SharedHttp.wsClient.newWebSocket(reqBuilder.build(), object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                if (ws === webSocket && preset != "auto") {
                    webSocket.send(JSONObject().put("type", "profile").put("preset", preset).toString())
                }
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                if (ws !== webSocket) return
                runCatching {
                    val obj = JSONObject(text)
                    when (obj.optString("type")) {
                        "meta" -> {
                            videoW = obj.optInt("imageWidth", 1280)
                            videoH = obj.optInt("imageHeight", 720)
                            val nextFps = obj.optInt("fps", 60)
                            streamFps = if (nextFps > 0) nextFps else 60
                            needKeyframe = true
                            val meta = DesktopStreamMeta(
                                imageWidth = videoW.toDouble(),
                                imageHeight = videoH.toDouble(),
                                logicalX = obj.optDouble("logicalX", 0.0),
                                logicalY = obj.optDouble("logicalY", 0.0),
                                logicalWidth = obj.optDouble("logicalWidth", 1440.0),
                                logicalHeight = obj.optDouble("logicalHeight", 900.0),
                                preset = obj.optString("preset", ""),
                                bitrateKbps = obj.optInt("bitrateKbps", 0),
                            )
                            mainHandler.post {
                                updateTextureTransform()
                                onMeta?.invoke(meta)
                            }
                        }
                        "error" -> {
                            val code = obj.optString("code", "")
                            val msg = obj.optString("error", "stream error")
                            emitStreamError(code, msg)
                        }
                    }
                }
            }

            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                if (ws !== webSocket) return
                val arr = bytes.toByteArray()
                if (arr.size < 2) return
                val kind = arr[0].toInt() and 0xFF
                if (kind != 0x02 && kind != 0x03 && kind != 0x04 && kind != 0x05) return
                val isKey = kind == 0x02 || kind == 0x04
                val isHevc = kind == 0x04 || kind == 0x05
                if (needKeyframe && !isKey) return
                decodeAnnexB(arr.copyOfRange(1, arr.size), isKey, isHevc)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                if (ws !== webSocket) return
                emitStreamError("", t.message ?: "connection failed")
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                if (ws !== webSocket) return
                if (!hadFirstFrame) {
                    emitStreamError("", reason.ifBlank { "closed" })
                } else {
                    mainHandler.post { reportStatus(false, 0) }
                }
            }
        })
    }

    private fun disconnect() {
        mainHandler.removeCallbacks(fpsRunnable)
        ws?.cancel()
        ws = null
        synchronized(codecLock) {
            runCatching { codec?.stop() }
            runCatching { codec?.release() }
            codec = null
        }
        vps = null
        sps = null
        pps = null
        configuredVps = null
        configuredSps = null
        configuredPps = null
        configuredCodec = ""
        needKeyframe = true
        hadFirstFrame = false
        reportedPreFrameError = false
    }

    private fun decodeAnnexB(data: ByteArray, isKey: Boolean, isHevc: Boolean) {
        val nalus = splitAnnexB(data)
        if (nalus.isEmpty()) return
        for (n in nalus) {
            if (isHevc) {
                when ((n[0].toInt() ushr 1) and 0x3F) {
                    32 -> vps = n
                    33 -> sps = n
                    34 -> pps = n
                }
            } else {
                when (n[0].toInt() and 0x1F) {
                    7 -> sps = n
                    8 -> pps = n
                }
            }
        }

        val targetCodec = if (isHevc) "hevc" else "h264"
        synchronized(codecLock) {
            val surf = surface ?: return
            val w = if (videoW > 0) videoW else 1280
            val h = if (videoH > 0) videoH else 720
            val curVps = vps
            val curSps = sps
            val curPps = pps
            val paramsChanged = isKey && curSps != null && curPps != null && (!isHevc || curVps != null) && (
                configuredCodec != targetCodec ||
                    configuredW != w ||
                    configuredH != h ||
                    !curSps.contentEquals(configuredSps) ||
                    !curPps.contentEquals(configuredPps) ||
                    (isHevc && !curVps.contentEquals(configuredVps))
                )
            if (codec != null && paramsChanged) {
                runCatching { codec?.stop() }
                runCatching { codec?.release() }
                codec = null
            }
            if (codec == null) {
                val initSps = curSps ?: return
                val initPps = curPps ?: return
                val initVps = if (isHevc) (curVps ?: return) else null
                try {
                    val mime = if (isHevc) MediaFormat.MIMETYPE_VIDEO_HEVC else MediaFormat.MIMETYPE_VIDEO_AVC
                    val format = MediaFormat.createVideoFormat(mime, w, h)
                    if (isHevc && initVps != null) {
                        val sc = byteArrayOf(0, 0, 0, 1)
                        format.setByteBuffer("csd-0", ByteBuffer.wrap(sc + initVps + sc + initSps + sc + initPps))
                    } else {
                        format.setByteBuffer("csd-0", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + initSps))
                        format.setByteBuffer("csd-1", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + initPps))
                    }
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                        format.setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
                    }
                    val mc = MediaCodec.createDecoderByType(mime)
                    mc.configure(format, surf, null, 0)
                    mc.start()
                    codec = mc
                    configuredCodec = targetCodec
                    configuredVps = initVps?.copyOf()
                    configuredSps = initSps.copyOf()
                    configuredPps = initPps.copyOf()
                    configuredW = w
                    configuredH = h
                } catch (e: Exception) {
                    emitStreamError("", e.message ?: "codec error")
                    return
                }
            }

            val mc = codec ?: return
            try {
                val inIdx = mc.dequeueInputBuffer(10_000)
                if (inIdx >= 0) {
                    val buf = mc.getInputBuffer(inIdx)
                    buf?.clear()
                    buf?.put(data)
                    ptsUs += 1_000_000L / maxOf(1, streamFps)
                    val flags = if (isKey) MediaCodec.BUFFER_FLAG_KEY_FRAME else 0
                    mc.queueInputBuffer(inIdx, 0, data.size, ptsUs, flags)
                }

                val info = MediaCodec.BufferInfo()
                var outIdx = mc.dequeueOutputBuffer(info, 0)
                while (outIdx >= 0) {
                    mc.releaseOutputBuffer(outIdx, true)
                    if (needKeyframe) {
                        needKeyframe = false
                        hadFirstFrame = true
                        val curFps = if (trackFps) frameCounter.get() else 0
                        mainHandler.post { reportStatus(true, curFps) }
                    }
                    if (trackFps) {
                        frameCounter.incrementAndGet()
                    }
                    outIdx = mc.dequeueOutputBuffer(info, 0)
                }
            } catch (_: Exception) {
                requestKeyframe()
            }
        }
    }

    private fun splitAnnexB(data: ByteArray): List<ByteArray> {
        val out = ArrayList<ByteArray>()
        var i = 0
        var start = -1
        while (i < data.size) {
            val scLen = when {
                i + 3 < data.size && data[i] == 0.toByte() && data[i + 1] == 0.toByte() &&
                    data[i + 2] == 0.toByte() && data[i + 3] == 1.toByte() -> 4
                i + 2 < data.size && data[i] == 0.toByte() && data[i + 1] == 0.toByte() &&
                    data[i + 2] == 1.toByte() -> 3
                else -> 0
            }
            if (scLen > 0) {
                if (start in 0 until i) out.add(data.copyOfRange(start, i))
                i += scLen
                start = i
            } else {
                i++
            }
        }
        if (start in 0 until data.size) out.add(data.copyOfRange(start, data.size))
        return out
    }
}
