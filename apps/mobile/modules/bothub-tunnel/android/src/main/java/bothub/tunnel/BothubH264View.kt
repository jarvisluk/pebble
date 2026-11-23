package bothub.tunnel

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
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.viewevent.EventDispatcher
import expo.modules.kotlin.views.ExpoView
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import org.json.JSONObject
import java.nio.ByteBuffer
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class BothubH264View(context: Context, appContext: AppContext) : ExpoView(context, appContext),
  TextureView.SurfaceTextureListener {

  private val onMeta by EventDispatcher()
  private val onStatus by EventDispatcher()
  private val onStreamError by EventDispatcher()

  private val textureView = TextureView(context)
  private val mainHandler = Handler(Looper.getMainLooper())
  private val client = OkHttpClient.Builder()
    .readTimeout(0, TimeUnit.MILLISECONDS)
    .pingInterval(15, TimeUnit.SECONDS)
    .build()

  private var surface: Surface? = null
  private var ws: WebSocket? = null
  private var codec: MediaCodec? = null
  private var codecLock = Any()

  private var url: String = ""
  private var token: String = ""
  private var preset: String = "auto"
  private var active: Boolean = false

  private var vps: ByteArray? = null
  private var sps: ByteArray? = null
  private var pps: ByteArray? = null
  private var configuredCodec: String = ""
  private var configuredVps: ByteArray? = null
  private var configuredSps: ByteArray? = null
  private var configuredPps: ByteArray? = null
  private var configuredW = 0
  private var configuredH = 0
  private var needKeyframe = true
  private var videoW = 0
  private var videoH = 0
  private var streamFps = 60
  private var streamCodec = "h264"
  private var ptsUs = 0L
  private val frameCounter = AtomicInteger(0)

  private val fpsRunnable = object : Runnable {
    override fun run() {
      val fps = frameCounter.getAndSet(0)
      if (ws != null) {
        onStatus(mapOf("connected" to !needKeyframe, "fps" to fps))
        mainHandler.postDelayed(this, 1000)
      }
    }
  }

  init {
    textureView.surfaceTextureListener = this
    addView(textureView, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
  }

  fun setUrl(value: String) {
    if (url != value) {
      url = value
      reconnectIfNeeded()
    }
  }

  fun setToken(value: String) {
    if (token != value) {
      token = value
      reconnectIfNeeded()
    }
  }

  fun setPreset(value: String) {
    val next = if (value.isBlank()) "auto" else value
    if (preset != next) {
      preset = next
      val cur = ws
      if (cur != null) {
        val msg = JSONObject().put("type", "profile").put("preset", next).toString()
        cur.send(msg)
      }
    }
  }

  fun setActive(value: Boolean) {
    if (active != value) {
      active = value
      reconnectIfNeeded()
    }
  }

  fun sendInput(eventJson: String) {
    val cur = ws ?: return
    val payload = JSONObject().put("type", "input").put("event", JSONObject(eventJson)).toString()
    cur.send(payload)
  }

  override fun onSurfaceTextureAvailable(st: SurfaceTexture, width: Int, height: Int) {
    surface = Surface(st)
    updateTextureTransform(width, height)
    reconnectIfNeeded()
  }

  override fun onSurfaceTextureSizeChanged(st: SurfaceTexture, width: Int, height: Int) {
    updateTextureTransform(width, height)
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

  override fun onSurfaceTextureDestroyed(st: SurfaceTexture): Boolean {
    disconnect()
    surface?.release()
    surface = null
    return true
  }

  override fun onSurfaceTextureUpdated(st: SurfaceTexture) {}

  override fun onDetachedFromWindow() {
    disconnect()
    super.onDetachedFromWindow()
  }

  private fun reconnectIfNeeded() {
    disconnect()
    if (!active || url.isBlank() || surface == null) return

    val reqBuilder = Request.Builder().url(url)
    if (token.isNotBlank()) {
      reqBuilder.addHeader("Authorization", "Bearer $token")
    }
    needKeyframe = true
    frameCounter.set(0)
    mainHandler.postDelayed(fpsRunnable, 1000)

    ws = client.newWebSocket(reqBuilder.build(), object : WebSocketListener() {
      override fun onMessage(webSocket: WebSocket, text: String) {
        if (ws !== webSocket) return
        try {
          val obj = JSONObject(text)
          when (obj.optString("type")) {
            "meta" -> {
              videoW = obj.optInt("imageWidth", 1280)
              videoH = obj.optInt("imageHeight", 720)
              val nextFps = obj.optInt("fps", 60)
              streamFps = if (nextFps > 0) nextFps else 60
              streamCodec = if (obj.optString("codec", "h264") == "hevc") "hevc" else "h264"
              needKeyframe = true
              mainHandler.post {
                updateTextureTransform()
                onMeta(mapOf(
                  "imageWidth" to videoW,
                  "imageHeight" to videoH,
                  "logicalX" to obj.optDouble("logicalX", 0.0),
                  "logicalY" to obj.optDouble("logicalY", 0.0),
                  "logicalWidth" to obj.optDouble("logicalWidth", 1440.0),
                  "logicalHeight" to obj.optDouble("logicalHeight", 900.0),
                  "preset" to obj.optString("preset", ""),
                  "bitrateKbps" to obj.optInt("bitrateKbps", 0)
                ))
              }
            }
            "error" -> {
              val code = obj.optString("code", "")
              val msg = obj.optString("error", "stream error")
              mainHandler.post {
                onStreamError(mapOf("code" to code, "error" to msg))
              }
            }
          }
        } catch (_: Exception) {}
      }

      override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
        if (ws !== webSocket) return
        val arr = bytes.toByteArray()
        if (arr.size < 2) return
        val kind = arr[0].toInt() and 0xFF
        if (kind != 0x02 && kind != 0x03 && kind != 0x04 && kind != 0x05) return
        val isKey = (kind == 0x02 || kind == 0x04)
        val isHevc = (kind == 0x04 || kind == 0x05)
        if (needKeyframe && !isKey) return
        val payload = arr.copyOfRange(1, arr.size)
        decodeAnnexB(payload, isKey, isHevc)
      }

      override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
        if (ws !== webSocket) return
        mainHandler.post {
          onStatus(mapOf("connected" to false, "fps" to 0))
          onStreamError(mapOf("code" to "", "error" to (t.message ?: "connection failed")))
        }
      }

      override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
        if (ws !== webSocket) return
        mainHandler.post {
          onStatus(mapOf("connected" to false, "fps" to 0))
        }
      }
    })
  }

  private fun disconnect() {
    mainHandler.removeCallbacks(fpsRunnable)
    ws?.cancel()
    ws = null
    synchronized(codecLock) {
      try {
        codec?.stop()
      } catch (_: Exception) {}
      try {
        codec?.release()
      } catch (_: Exception) {}
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
  }

  private fun requestKeyframe() {
    needKeyframe = true
    ws?.send("""{"type":"keyframe"}""")
  }

  private fun decodeAnnexB(data: ByteArray, isKey: Boolean, isHevc: Boolean) {
    val nalus = splitAnnexB(data)
    if (nalus.isEmpty()) return
    for (n in nalus) {
      if (isHevc) {
        val type = (n[0].toInt() ushr 1) and 0x3F
        when (type) {
          32 -> vps = n
          33 -> sps = n
          34 -> pps = n
        }
      } else {
        val type = n[0].toInt() and 0x1F
        if (type == 7) sps = n
        else if (type == 8) pps = n
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
        try {
          codec?.stop()
        } catch (_: Exception) {}
        try {
          codec?.release()
        } catch (_: Exception) {}
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
          mainHandler.post { onStreamError(mapOf("code" to "", "error" to (e.message ?: "codec error"))) }
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
            mainHandler.post { onStatus(mapOf("connected" to true, "fps" to frameCounter.get())) }
          }
          frameCounter.incrementAndGet()
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
        i + 3 < data.size && data[i] == 0.toByte() && data[i + 1] == 0.toByte() && data[i + 2] == 0.toByte() && data[i + 3] == 1.toByte() -> 4
        i + 2 < data.size && data[i] == 0.toByte() && data[i + 1] == 0.toByte() && data[i + 2] == 1.toByte() -> 3
        else -> 0
      }
      if (scLen > 0) {
        if (start in 0 until i) {
          out.add(data.copyOfRange(start, i))
        }
        i += scLen
        start = i
      } else {
        i++
      }
    }
    if (start in 0 until data.size) {
      out.add(data.copyOfRange(start, data.size))
    }
    return out
  }
}
