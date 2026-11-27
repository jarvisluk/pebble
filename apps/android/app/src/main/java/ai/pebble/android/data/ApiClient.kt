package ai.pebble.android.data

import ai.pebble.android.BuildConfig
import ai.pebble.android.i18n.L
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonObject
import okhttp3.Call
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okio.BufferedSource
import java.io.IOException
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.URLDecoder
import java.net.URLEncoder
import java.net.UnknownHostException
import java.util.concurrent.TimeUnit
import kotlin.math.min

class HubException(
    override val message: String,
    val unauthorized: Boolean = false,
    val status: Int = 0,
    val isKey: Boolean = false,
    val code: String = "",
) : Exception(message) {
    val isUnsupportedCapability: Boolean
        get() = status == 404 || status == 405 || status == 501 || code == "capability_unsupported"
}

fun errText(error: Throwable, fallback: String): String {
    if (error is HubException) {
        if (error.isKey) return L.t(error.message)
        return error.message.ifEmpty { fallback }
    }
    return when (error) {
        is SocketTimeoutException -> L.t("netTimeout")
        is UnknownHostException, is ConnectException -> L.t("netUnreachable")
        is IOException -> {
            val msg = error.message.orEmpty()
            if (msg.contains("timed out", ignoreCase = true)) L.t("netTimeout") else fallback
        }
        else -> fallback
    }
}

fun encodeURIComponent(s: String): String =
    URLEncoder.encode(s, "UTF-8")
        .replace("+", "%20")
        .replace("%21", "!")
        .replace("%27", "'")
        .replace("%28", "(")
        .replace("%29", ")")
        .replace("%7E", "~")

data class NormalizedAddress(
    val cleanAddress: String,
    val token: String,
    val isTailcat: Boolean,
    val isTailscale: Boolean,
)

fun normalizeAddress(raw: String): NormalizedAddress {
    val trimmed = raw.trim().replace(Regex("[\\r\\n]+"), "")
    var base = trimmed
    var token = ""
    val hashIdx = trimmed.indexOf('#')
    if (hashIdx >= 0) {
        base = trimmed.substring(0, hashIdx)
        val frag = trimmed.substring(hashIdx + 1)
        for (part in frag.split('&')) {
            val eq = part.indexOf('=')
            val kRaw = if (eq >= 0) part.substring(0, eq) else part
            val vRaw = if (eq >= 0) part.substring(eq + 1) else ""
            val k = runCatching { URLDecoder.decode(kRaw, "UTF-8") }.getOrDefault(kRaw)
            if (k == "token") {
                token = runCatching {
                    URLDecoder.decode(vRaw.replace("+", "%20"), "UTF-8")
                }.getOrDefault(vRaw).trim()
                break
            }
        }
    }
    val s = base.trim()
    val lower = s.lowercase()
    if (lower.startsWith("tailcat://")) {
        return NormalizedAddress(cleanAddress = s, token = token, isTailcat = true, isTailscale = false)
    }
    if (lower.startsWith("tailscale://")) {
        return NormalizedAddress(cleanAddress = s, token = token, isTailcat = false, isTailscale = true)
    }
    if (s.isEmpty()) {
        return NormalizedAddress(cleanAddress = "", token = token, isTailcat = false, isTailscale = false)
    }
    val hasProto = Regex("^https?://", RegexOption.IGNORE_CASE).containsMatchIn(s)
    var withProto = if (hasProto) s else "http://$s"
    withProto = withProto.trimEnd('/')
    return NormalizedAddress(cleanAddress = withProto, token = token, isTailcat = false, isTailscale = false)
}

fun stripLinkFragment(raw: String): String {
    val trimmed = raw.trim().replace(Regex("[\\r\\n]+"), "")
    val hashIdx = trimmed.indexOf('#')
    return (if (hashIdx >= 0) trimmed.substring(0, hashIdx) else trimmed).trim()
}

fun maskedPairingLink(baseLink: String): String {
    val base = stripLinkFragment(baseLink)
    return if (base.isEmpty()) "" else "$base#token=••••••••"
}

fun buildPairingLink(baseLink: String, token: String): String? {
    val base = stripLinkFragment(baseLink)
    val tok = token.trim()
    if (base.isEmpty() || tok.isEmpty()) return null
    return "$base#token=${encodeURIComponent(tok)}"
}

object SharedHttp {
    val apiClient: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .build()

    val sseClient: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(75, TimeUnit.SECONDS)
        .writeTimeout(15, TimeUnit.SECONDS)
        .build()

    val wsClient: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .pingInterval(15, TimeUnit.SECONDS)
        .build()
}

class ApiClient(
    val base: String,
    val token: String,
) {
    companion object {
        fun attachClientHeaders(builder: Request.Builder): Request.Builder =
            builder
                .header("X-Pebble-Protocol", CLIENT_PROTOCOL.toString())
                .header("X-Pebble-Client", "android")
                .header("X-Pebble-Version", BuildConfig.VERSION_NAME)
    }

    suspend fun rawBytes(path: String, method: String = "GET", body: Map<String, Any?>? = null): ByteArray =
        withContext(Dispatchers.IO) {
            val url = resolveUrl(path)
            val builder = attachClientHeaders(Request.Builder().url(url))
            if (token.isNotEmpty()) {
                builder.header("Authorization", "Bearer $token")
            }
            val reqBody = if (body != null) {
                val jsonText = mapToJsonObject(body).toString()
                jsonText.toRequestBody("application/json; charset=utf-8".toMediaType())
            } else if (method == "POST" || method == "PUT" || method == "PATCH") {
                "{}".toRequestBody("application/json; charset=utf-8".toMediaType())
            } else {
                null
            }
            when (method) {
                "GET" -> builder.get()
                "DELETE" -> builder.delete(reqBody)
                else -> builder.method(method, reqBody)
            }
            SharedHttp.apiClient.newCall(builder.build()).execute().use { resp ->
                val code = resp.code
                val bytes = resp.body?.bytes() ?: ByteArray(0)
                if (code == 401) {
                    throw HubException(L.t("invalidTokenErr"), unauthorized = true, status = 401)
                }
                if (code !in 200..299) {
                    val (errText, errCode) = extractErrorDetails(bytes)
                    throw HubException(errText ?: "HTTP $code", status = code, code = errCode)
                }
                bytes
            }
        }

    suspend fun rawText(path: String, method: String = "GET", body: Map<String, Any?>? = null): String =
        String(rawBytes(path, method, body), Charsets.UTF_8)

    suspend inline fun <reified T> call(
        path: String,
        method: String = "GET",
        body: Map<String, Any?>? = null,
    ): T {
        val text = rawText(path, method, body).ifBlank { "{}" }
        return HubJson.decodeFromString(text)
    }

    suspend fun uploadAttachment(
        path: String,
        bytes: ByteArray,
        filename: String,
        mimeType: String,
    ): InboundAttachmentResponse = withContext(Dispatchers.IO) {
        val url = resolveUrl(path)
        val safeName = filename.replace("\"", "_")
        val fileBody = bytes.toRequestBody(mimeType.toMediaType())
        val multipart = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("file", safeName, fileBody)
            .build()
        val builder = attachClientHeaders(
            Request.Builder()
                .url(url)
                .post(multipart),
        )
        if (token.isNotEmpty()) {
            builder.header("Authorization", "Bearer $token")
        }
        val uploadClient = SharedHttp.apiClient.newBuilder()
            .writeTimeout(60, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .build()
        uploadClient.newCall(builder.build()).execute().use { resp ->
            val code = resp.code
            val respBytes = resp.body?.bytes() ?: ByteArray(0)
            if (code == 401) {
                throw HubException(L.t("invalidTokenErr"), unauthorized = true, status = 401)
            }
            if (code !in 200..299) {
                val (errText, errCode) = extractErrorDetails(respBytes)
                throw HubException(errText ?: "HTTP $code", status = code, code = errCode)
            }
            HubJson.decodeFromString(String(respBytes, Charsets.UTF_8))
        }
    }

    suspend fun listWorkspaceFiles(botId: String, path: String = "", showHidden: Boolean = false): WorkspaceListResult {
        val params = mutableListOf<String>()
        if (path.isNotEmpty()) params.add("path=${encodeURIComponent(path)}")
        if (showHidden) params.add("showHidden=1")
        val qs = if (params.isEmpty()) "" else "?" + params.joinToString("&")
        return call("/api/bots/${encodeURIComponent(botId)}/files$qs")
    }

    suspend fun readWorkspaceFile(botId: String, path: String): WorkspaceFileContent =
        call("/api/bots/${encodeURIComponent(botId)}/files/content?path=${encodeURIComponent(path)}")

    suspend fun issueWorkspaceFileTicket(botId: String, path: String): WorkspaceFileTicketResp =
        call("/api/bots/${encodeURIComponent(botId)}/files/ticket", method = "POST", body = mapOf("path" to path))

    fun workspaceFileRawUrl(botId: String, path: String, ticket: String, download: Boolean = false): String {
        val qs = buildString {
            append("path=").append(encodeURIComponent(path))
            append("&ticket=").append(encodeURIComponent(ticket))
            if (download) append("&download=1")
        }
        return "$base/api/bots/${encodeURIComponent(botId)}/files/raw?$qs"
    }

    suspend fun downloadWorkspaceFileToTemp(
        cacheDir: java.io.File,
        botId: String,
        path: String,
        fileName: String,
    ): java.io.File = withContext(Dispatchers.IO) {
        val tk = issueWorkspaceFileTicket(botId, path)
        val url = workspaceFileRawUrl(botId, path, tk.ticket, download = true)
        val bytes = rawBytes(url)
        val destDir = java.io.File(cacheDir, "pebble-files").apply { mkdirs() }
        val safeName = fileName.replace('/', '_').replace('\\', '_').ifBlank { "file" }
        val dest = java.io.File(destDir, safeName)
        dest.writeBytes(bytes)
        dest
    }

    private fun resolveUrl(path: String): String {
        if (base.isBlank()) throw HubException(L.t("enterValidAddr"))
        return if (path.startsWith("http://") || path.startsWith("https://")) path else "$base$path"
    }

    private fun extractErrorDetails(bytes: ByteArray): Pair<String?, String> = runCatching {
        val obj = HubJson.parseToJsonElement(String(bytes, Charsets.UTF_8)).jsonObject
        val msg = obj["error"]?.asDisplayString()
        val code = obj["code"]?.asDisplayString().orEmpty()
        msg to code
    }.getOrDefault(null to "")
}

class EventStream {
    private var job: Job? = null
    @Volatile
    private var activeCall: Call? = null

    fun start(
        scope: CoroutineScope,
        base: String,
        token: String,
        onStatus: (Boolean) -> Unit,
        onUnauthorized: () -> Unit,
        onEvent: (String, JsonObject) -> Unit,
    ) {
        stop()
        if (base.isBlank()) return
        job = scope.launch(Dispatchers.IO) {
            var delayMs = 1000L
            var lastSeq = 0L
            while (isActive) {
                try {
                    val eventPath = if (lastSeq > 0L) "/api/events?since=$lastSeq" else "/api/events"
                    val reqBuilder = ApiClient.attachClientHeaders(
                        Request.Builder()
                            .url("$base$eventPath")
                            .header("Accept", "text/event-stream"),
                    )
                    if (lastSeq > 0L) {
                        reqBuilder.header("Last-Event-ID", lastSeq.toString())
                    }
                    if (token.isNotEmpty()) {
                        reqBuilder.header("Authorization", "Bearer $token")
                    }
                    val call = SharedHttp.sseClient.newCall(reqBuilder.build())
                    activeCall = call
                    call.execute().use { resp ->
                        val code = resp.code
                        if (code == 401) {
                            withContext(Dispatchers.Main) {
                                onStatus(false)
                                onUnauthorized()
                            }
                            return@launch
                        }
                        if (code != 200) {
                            throw HubException("HTTP $code", status = code)
                        }
                        val source = resp.body?.source() ?: throw IOException("empty sse stream")
                        readSseLoop(
                            source = source,
                            isStillActive = { isActive },
                            onReady = {
                                delayMs = 1000L
                                withContext(Dispatchers.Main) { onStatus(true) }
                            },
                            onSeq = { seq ->
                                if (seq > 0L) lastSeq = seq
                            },
                            onEvent = { ev, obj ->
                                withContext(Dispatchers.Main) { onEvent(ev, obj) }
                            },
                        )
                    }
                } catch (ce: CancellationException) {
                    throw ce
                } catch (_: Throwable) {
                    // fall through to reconnect
                } finally {
                    activeCall = null
                }
                if (!isActive) break
                withContext(Dispatchers.Main) { onStatus(false) }
                delay(delayMs)
                delayMs = min((delayMs * 1.8).toLong(), 12_000L)
            }
        }
    }

    private suspend fun readSseLoop(
        source: BufferedSource,
        isStillActive: () -> Boolean,
        onReady: suspend () -> Unit,
        onSeq: (Long) -> Unit,
        onEvent: suspend (String, JsonObject) -> Unit,
    ) {
        var event = "message"
        var eventId = 0L
        val dataLines = mutableListOf<String>()
        while (isStillActive()) {
            val rawLine = source.readUtf8Line() ?: break
            val line = rawLine.removeSuffix("\r")
            when {
                line.isEmpty() -> {
                    if (event == "ready") {
                        onReady()
                    } else if (dataLines.isNotEmpty()) {
                        val rawJson = dataLines.joinToString("\n")
                        val obj = runCatching { HubJson.parseToJsonElement(rawJson).jsonObject }.getOrNull()
                        if (obj != null) {
                            val seq = if (eventId > 0L) eventId else obj["seq"]?.asDisplayString()?.toLongOrNull() ?: 0L
                            if (seq > 0L) onSeq(seq)
                            val payload = (obj["payload"] as? JsonObject) ?: obj
                            onEvent(event, payload)
                        }
                    }
                    event = "message"
                    eventId = 0L
                    dataLines.clear()
                }
                line.startsWith(":") -> Unit
                line.startsWith("id:") -> {
                    val parsed = line.substring(3).trim().toLongOrNull() ?: 0L
                    if (parsed > 0L) eventId = parsed
                }
                line.startsWith("event:") -> {
                    event = line.substring(6).trim()
                }
                line.startsWith("data:") -> {
                    dataLines.add(line.substring(5).trimStart())
                }
            }
        }
    }

    fun stop() {
        job?.cancel()
        job = null
        activeCall?.cancel()
        activeCall = null
    }
}
