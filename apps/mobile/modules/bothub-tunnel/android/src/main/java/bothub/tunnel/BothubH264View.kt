package bothub.tunnel

import android.content.Context
import android.graphics.SurfaceTexture
import android.media.MediaCodec
import android.media.MediaFormat
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

  private var sps: ByteArray? = null
  private var pps: ByteArray? = null
  private var needKeyframe = true
  private var videoW = 0
  private var videoH = 0
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
    reconnectIfNeeded()
  }

  override fun onSurfaceTextureSizeChanged(st: SurfaceTexture, width: Int, height: Int) {}

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
              needKeyframe = true
              mainHandler.post {
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
        if (kind != 0x02 && kind != 0x03) return
        val isKey = kind == 0x02
        if (needKeyframe && !isKey) return
        val payload = arr.copyOfRange(1, arr.size)
        decodeAnnexB(payload, isKey)
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
    sps = null
    pps = null
    needKeyframe = true
  }

  private fun decodeAnnexB(data: ByteArray, isKey: Boolean) {
    val nalus = splitAnnexB(data)
    if (nalus.isEmpty()) return
    for (n in nalus) {
      val type = n[0].toInt() and 0x1F
      if (type == 7) sps = n
      else if (type == 8) pps = n
    }

    synchronized(codecLock) {
      val surf = surface ?: return
      if (codec == null) {
        val curSps = sps ?: return
        val curPps = pps ?: return
        val w = if (videoW > 0) videoW else 1280
        val h = if (videoH > 0) videoH else 720
        try {
          val format = MediaFormat.createVideoFormat("video/avc", w, h)
          format.setByteBuffer("csd-0", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + curSps))
          format.setByteBuffer("csd-1", ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + curPps))
          val mc = MediaCodec.createDecoderByType("video/avc")
          mc.configure(format, surf, null, 0)
          mc.start()
          codec = mc
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
          ptsUs += 33_333
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
        needKeyframe = true
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
