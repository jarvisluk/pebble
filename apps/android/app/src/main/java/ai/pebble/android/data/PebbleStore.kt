package ai.pebble.android.data

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import ai.pebble.android.i18n.L
import ai.pebble.android.notify.BotActivityService
import ai.pebble.android.notify.NotificationHelper
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.roundToInt

enum class ConnState { IDLE, CONNECTING, CONNECTED, UNAUTHORIZED, ERROR }
enum class BotSortMode(val raw: String) { RECENT("recent"), MANUAL("manual") }

fun resolveDefaultModel(g: String, h: String): String {
    if (g.isEmpty()) return ""
    if (g.startsWith("magpie/")) return if (h == "claude") g.removePrefix("magpie/") else g
    if (!g.contains("/")) return g
    return if (h.isEmpty() || h == "opencode" || h == "pi") g else ""
}

fun parseIsoInstant(s: String?): Instant? {
    if (s.isNullOrBlank()) return null
    return runCatching { Instant.parse(s) }.getOrNull()
}

private val hhmmEnFormatter: DateTimeFormatter = DateTimeFormatter.ofPattern("h:mm a", Locale.US)
private val hhmmZhFormatter: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm", Locale.CHINA)
private val previewCodeRegex = Regex("```[\\s\\S]*?```")
private val previewMdRegex = Regex("[#>*`_\\[\\]]")
private val previewWsRegex = Regex("\\s+")

private fun formatHhMm(instant: Instant): String {
    val zdt = instant.atZone(ZoneId.systemDefault())
    val fmt = if (L.lang == "en") hhmmEnFormatter else hhmmZhFormatter
    return fmt.format(zdt)
}

fun formatShortTime(iso: String?): String {
    val inst = parseIsoInstant(iso) ?: return ""
    val zone = ZoneId.systemDefault()
    val date = inst.atZone(zone).toLocalDate()
    val today = LocalDate.now(zone)
    if (date == today) return formatHhMm(inst)
    if (date == today.minusDays(1)) return L.t("yesterday")
    return "${date.monthValue}/${date.dayOfMonth}"
}

fun formatStamp(iso: String?): String {
    val inst = parseIsoInstant(iso) ?: return ""
    val zone = ZoneId.systemDefault()
    val date = inst.atZone(zone).toLocalDate()
    val today = LocalDate.now(zone)
    val t = formatHhMm(inst)
    if (date == today) return "${L.t("today")} $t"
    if (date == today.minusDays(1)) return "${L.t("yesterday")} $t"
    return if (L.lang == "en") {
        "${date.monthValue}/${date.dayOfMonth} $t"
    } else {
        "${date.monthValue}月${date.dayOfMonth}日 $t"
    }
}

fun formatClockTime(iso: String?): String {
    val inst = parseIsoInstant(iso) ?: return ""
    return formatHhMm(inst)
}

fun plainPreview(s: String): String {
    var out = s.replace(previewCodeRegex, L.t("codeTag"))
    out = out.replace(previewMdRegex, "")
    out = out.replace(previewWsRegex, " ")
    return out.trim()
}

fun countToolCalls(tools: List<String>?): Int {
    if (tools.isNullOrEmpty()) return 0
    var total = 0
    for (item in tools) {
        val idx = item.lastIndexOf('\u00d7')
        val n = if (idx >= 0) item.substring(idx + 1).toIntOrNull() else null
        total += if (n != null && n > 0) n else 1
    }
    return total
}

fun formatDurationMs(ms: Double?): String {
    if (ms == null || ms <= 0.0) return ""
    val sec = ms / 1000.0
    if (sec < 60.0) return String.format(Locale.US, "%.1fs", sec)
    var mins = (sec / 60.0).toInt()
    var rem = (sec % 60.0).roundToInt()
    if (rem == 60) {
        mins += 1
        rem = 0
    }
    return "${mins}m ${rem}s"
}

class PebbleStore(context: Context) {
    private val appContext = context.applicationContext
    private val prefs = appContext.getSharedPreferences("pebble_prefs", Context.MODE_PRIVATE)
    private val secureStore = SecureStore(appContext)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val stream = EventStream()
    private val mediaCache = LinkedHashMap<String, ByteArray>(64, 0.75f, true)
    private var mediaCacheBytes = 0L

    // Preferences
    var themePref by mutableStateOf(prefs.getString("pebble.theme", "system") ?: "system")
        private set
    var langPref by mutableStateOf(prefs.getString("pebble.lang", "system") ?: "system")
        private set
    var tsBound by mutableStateOf(prefs.getBoolean("pebble.tsBound", false))
        private set
    var tsControlUrl by mutableStateOf(prefs.getString("pebble.tsControlUrl", "") ?: "")
        private set
    var sortMode by mutableStateOf(
        BotSortMode.entries.firstOrNull { it.raw == prefs.getString("pebble.sortMode", "recent") } ?: BotSortMode.RECENT
    )
        private set
    var pinnedIds by mutableStateOf(loadStringList("pebble.pinned"))
        private set
    var manualOrder by mutableStateOf(loadStringList("pebble.manualOrder"))
        private set
    var langRevision by mutableIntStateOf(0)
        private set

    // Connection
    var servers by mutableStateOf<List<ServerProfile>>(emptyList())
        private set
    var activeServerId by mutableStateOf<String?>(prefs.getString("pebble.activeServer", null))
        private set
    var connState by mutableStateOf(ConnState.IDLE)
        private set
    var connError by mutableStateOf("")
        private set
    var baseURL by mutableStateOf("")
        private set
    var authToken by mutableStateOf("")
        private set
    var isTailcat by mutableStateOf(false)
        private set
    var isTailscale by mutableStateOf(false)
        private set
    var online by mutableStateOf(false)
        private set
    var usingStandby by mutableStateOf(false)
        private set

    // Domain Data
    var bots by mutableStateOf<List<BotStatus>>(emptyList())
        private set
    val msgs = mutableStateMapOf<String, List<ChatMessage>>()
    val last = mutableStateMapOf<String, ChatMessage>()
    val unread = mutableStateMapOf<String, Int>()
    val tools = mutableStateMapOf<String, String>()
    var settings by mutableStateOf<SettingsData?>(null)
        private set
    val models = mutableStateMapOf<String, List<String>>()
    var nodes by mutableStateOf<List<NodeInfo>>(emptyList())
        private set
    var tailcat by mutableStateOf<TailcatStatus?>(null)
        private set
    var mobileTailscale by mutableStateOf<MobileTailscaleStatus?>(null)
        private set
    var hostTailscale by mutableStateOf<HostTailscaleStatus?>(null)
        private set
    var tsBinding by mutableStateOf(false)
        private set
    var takeovers by mutableStateOf<List<TakeoverRequest>>(emptyList())
        private set
    var questions by mutableStateOf<List<PendingQuestion>>(emptyList())
        private set
    var desktopTargets by mutableStateOf<List<DesktopTarget>>(emptyList())
        private set
    var activeDesktopTarget by mutableStateOf(ActiveDesktopTarget(target = "local"))
        private set
    val routines = mutableStateMapOf<String, List<RoutineItem>>()
    var pendingStageOpen by mutableStateOf<StageOpenEvent?>(null)
        private set
    var pendingChatInsert by mutableStateOf<PendingChatInsert?>(null)
        private set

    // UI Callback for opening external URLs (Tailscale auth)
    var onOpenExternalUrl: ((String) -> Unit)? = null

    private var activeChat: String? = null
    private var connSeq = 0
    private var inForeground = false
    private val botSteps = mutableMapOf<String, Int>()
    private val activeBusyBots = mutableSetOf<String>()
    private val activeTakeoverIds = mutableSetOf<String>()

    init {
        L.lang = resolvedLang
        val savedJson = secureStore.read("servers")
        if (!savedJson.isNullOrBlank()) {
            servers = runCatching { HubJson.decodeFromString<List<ServerProfile>>(savedJson) }.getOrDefault(emptyList())
        }
        val target = servers.firstOrNull { it.id == activeServerId } ?: servers.firstOrNull()
        activeServerId = target?.id
        if (tsBound) {
            scope.launch {
                PebbleTunnel.tailscaleStart(appContext, controlUrl = tsControlUrl)
                refreshMobileTailscale(includePeers = true)
            }
        }
        if (target != null) {
            scope.launch { connectProfile(target) }
        }
    }

    val resolvedLang: String
        get() = if (langPref == "zh-CN" || langPref == "en") langPref else L.systemLang

    fun t(key: String, vars: Map<String, Any> = emptyMap()): String {
        @Suppress("UNUSED_VARIABLE")
        val rev = langRevision
        return L.t(key, vars)
    }

    val activeServer: ServerProfile?
        get() = servers.firstOrNull { it.id == activeServerId }

    val api: ApiClient
        get() = ApiClient(baseURL, authToken)

    val isConnected: Boolean
        get() = connState == ConnState.CONNECTED

    fun updateThemePref(value: String) {
        themePref = value
        prefs.edit().putString("pebble.theme", value).apply()
    }

    fun updateLangPref(value: String) {
        langPref = value
        prefs.edit().putString("pebble.lang", value).apply()
        L.lang = resolvedLang
        langRevision += 1
    }

    fun updateSortMode(mode: BotSortMode) {
        sortMode = mode
        prefs.edit().putString("pebble.sortMode", mode.raw).apply()
    }

    fun togglePin(id: String) {
        val list = pinnedIds.toMutableList()
        if (id in list) list.remove(id) else list.add(0, id)
        pinnedIds = list
        saveStringList("pebble.pinned", list)
    }

    fun moveManualOrder(newOrder: List<String>) {
        manualOrder = newOrder
        saveStringList("pebble.manualOrder", newOrder)
    }

    private fun loadStringList(key: String): List<String> {
        val raw = prefs.getString(key, null) ?: return emptyList()
        return runCatching { HubJson.decodeFromString<List<String>>(raw) }.getOrDefault(emptyList())
    }

    private fun saveStringList(key: String, list: List<String>) {
        prefs.edit().putString(key, HubJson.encodeToString(list)).apply()
    }

    private fun persistServers() {
        secureStore.write("servers", HubJson.encodeToString(servers))
        prefs.edit().putString("pebble.activeServer", activeServerId).apply()
    }

    fun onForegroundChanged(foreground: Boolean) {
        val wasBg = !inForeground
        inForeground = foreground
        if (foreground) {
            if (wasBg && baseURL.isNotEmpty() && activeServer != null) {
                scope.launch { resume() }
            }
            BotActivityService.stop(appContext)
        } else {
            syncBackgroundService()
        }
    }

    private fun syncBackgroundService() {
        val busyNames = bots.filter { it.isBusy }.map { it.displayName }
        if (!inForeground && busyNames.isNotEmpty() && isConnected) {
            BotActivityService.start(appContext, busyNames)
        } else if (busyNames.isEmpty()) {
            BotActivityService.stop(appContext)
        }
    }

    fun setActiveChat(botId: String?) {
        activeChat = botId
        if (botId != null) {
            unread.remove(botId)
        }
    }

    fun toggleUnread(id: String) {
        if ((unread[id] ?: 0) > 0) unread.remove(id) else unread[id] = 1
    }

    private fun removeBotState(id: String) {
        bots = bots.filterNot { it.id == id }
        msgs.remove(id)
        last.remove(id)
        unread.remove(id)
        tools.remove(id)
        botSteps.remove(id)
        activeBusyBots.remove(id)
    }

    private fun pushIncoming(botId: String, msg: ChatMessage) {
        val list = msgs[botId].orEmpty()
        if (list.any { it.id == msg.id }) return
        msgs[botId] = list + msg
        if (msg.role != "system") {
            last[botId] = msg
            if (activeChat != botId) {
                unread[botId] = (unread[botId] ?: 0) + 1
            }
        }
    }

    private fun updateIncoming(botId: String, msg: ChatMessage) {
        val list = msgs[botId]?.toMutableList()
        if (list != null) {
            val idx = list.indexOfFirst { it.id == msg.id }
            if (idx >= 0) {
                list[idx] = msg
                msgs[botId] = list
            }
        }
        if (last[botId]?.id == msg.id) {
            last[botId] = msg
        }
    }

    private fun upsertBot(st: BotStatus) {
        val list = bots.toMutableList()
        val idx = list.indexOfFirst { it.id == st.id }
        if (idx >= 0) list[idx] = st else list.add(st)
        bots = list
    }

    private fun startStream(url: String, tok: String) {
        stream.start(
            scope = scope,
            base = url,
            token = tok,
            onStatus = { ok -> online = ok },
            onUnauthorized = {
                connState = ConnState.UNAUTHORIZED
                connError = L.t("needsToken")
            },
            onEvent = { type, payload -> handleEvent(type, payload, url, tok) },
        )
    }

    private fun handleEvent(type: String, p: JsonObject, url: String, tok: String) {
        when (type) {
            "message" -> {
                val bid = p["botId"]?.asDisplayString() ?: return
                val m = runCatching { HubJson.decodeFromJsonElement<ChatMessage>(p) }.getOrNull() ?: return
                pushIncoming(bid, m)
            }
            "message_updated" -> {
                val bid = p["botId"]?.asDisplayString() ?: return
                val m = runCatching { HubJson.decodeFromJsonElement<ChatMessage>(p) }.getOrNull() ?: return
                updateIncoming(bid, m)
            }
            "bot_status" -> {
                val st = runCatching { HubJson.decodeFromJsonElement<BotStatus>(p) }.getOrNull() ?: return
                val wasBusy = bots.firstOrNull { it.id == st.id }?.isBusy == true
                upsertBot(st)
                if (st.isBusy) {
                    activeBusyBots.add(st.id)
                    if (!wasBusy) botSteps[st.id] = 0
                    val tool = tools[st.id]
                    val statusLabel = if (!tool.isNullOrBlank()) L.t("callingTool", mapOf("tool" to tool)) else L.t("thinking")
                    if (!inForeground) {
                        NotificationHelper.updateBotActivity(appContext, st.id, st.displayName, statusLabel, botSteps[st.id] ?: 0)
                    }
                } else {
                    if (tools.containsKey(st.id)) tools.remove(st.id)
                    val hadBusy = activeBusyBots.remove(st.id) || wasBusy
                    val steps = botSteps.remove(st.id) ?: 0
                    if (hadBusy && !inForeground) {
                        val preview = last[st.id]?.let { plainPreview(it.body) }.orEmpty()
                        val summary = if (preview.isNotEmpty()) preview.take(90) else if (steps > 0) "$steps 步已完成" else ""
                        NotificationHelper.endBotActivity(appContext, st.id, st.displayName, summary, notify = activeChat != st.id)
                    } else {
                        NotificationHelper.cancelBotActivity(appContext, st.id)
                    }
                }
                syncBackgroundService()
            }
            "tool_use" -> {
                val bid = p["botId"]?.asDisplayString() ?: return
                val tool = p["tool"]?.asDisplayString().orEmpty()
                if (tools[bid] != tool) tools[bid] = tool
                if (tool.isNotBlank()) {
                    val steps = (botSteps[bid] ?: 0) + 1
                    botSteps[bid] = steps
                    if (!inForeground) {
                        val name = bots.firstOrNull { it.id == bid }?.displayName ?: bid
                        NotificationHelper.updateBotActivity(
                            appContext,
                            bid,
                            name,
                            L.t("callingTool", mapOf("tool" to tool)),
                            steps,
                        )
                    }
                }
            }
            "bot_created" -> {
                val st = runCatching { HubJson.decodeFromJsonElement<BotStatus>(p) }.getOrNull() ?: return
                if (bots.none { it.id == st.id }) bots = bots + st
            }
            "bot_deleted" -> {
                val bid = p["id"]?.asDisplayString() ?: p["botId"]?.asDisplayString() ?: return
                removeBotState(bid)
            }
            "nodes_changed" -> {
                val arr = p["nodes"] as? JsonArray ?: return
                nodes = runCatching { HubJson.decodeFromJsonElement<List<NodeInfo>>(arr) }.getOrDefault(nodes)
            }
            "desktop_target" -> {
                val at = runCatching { HubJson.decodeFromJsonElement<ActiveDesktopTarget>(p) }.getOrNull() ?: return
                activeDesktopTarget = at
            }
            "stage_open" -> {
                val ev = runCatching { HubJson.decodeFromJsonElement<StageOpenEvent>(p) }.getOrNull() ?: return
                pendingStageOpen = ev
            }
            "takeover" -> {
                val arr = p["items"] as? JsonArray ?: return
                val list = runCatching { HubJson.decodeFromJsonElement<List<TakeoverRequest>>(arr) }.getOrDefault(emptyList())
                takeovers = list
                val nextIds = list.map { it.id }.toSet()
                for (oldId in activeTakeoverIds - nextIds) {
                    NotificationHelper.cancelTakeoverAlert(appContext, oldId)
                }
                for (item in list) {
                    if (item.id !in activeTakeoverIds && !inForeground) {
                        NotificationHelper.showTakeoverAlert(
                            appContext,
                            item.id,
                            item.reason.orEmpty(),
                            item.target ?: "local",
                        )
                    }
                }
                activeTakeoverIds.clear()
                activeTakeoverIds.addAll(nextIds)
            }
            "questions" -> {
                val arr = p["items"] as? JsonArray ?: return
                questions = runCatching { HubJson.decodeFromJsonElement<List<PendingQuestion>>(arr) }.getOrDefault(emptyList())
            }
            "routine_updated", "routine_run" -> {
                val bid = p["botId"]?.asDisplayString() ?: return
                scope.launch {
                    runCatching { ApiClient(url, tok).call<RoutinesResp>("/api/bots/${encodeURIComponent(bid)}/routines") }
                        .getOrNull()?.routines?.let { routines[bid] = it }
                }
            }
        }
    }

    suspend fun connectProfile(profile: ServerProfile) {
        connSeq += 1
        val seq = connSeq
        stream.stop()
        online = false
        usingStandby = false
        connState = ConnState.CONNECTING
        connError = ""

        try {
            connectEndpoint(profile, profile.address, profile.token, isStandby = false, seq = seq)
        } catch (err: Throwable) {
            if (seq != connSeq) return
            if ((err as? HubException)?.unauthorized == true) {
                handleConnectError(err)
                return
            }
            val candidates = mutableListOf<Pair<String, String>>()
            val sbAddr = profile.standbyAddress.orEmpty().trim()
            if (sbAddr.isNotEmpty()) candidates.add(sbAddr to profile.standbyToken.orEmpty())
            for (fb in profile.fallbackHosts.orEmpty()) {
                val addr = fb.address.trim()
                if (addr.isNotEmpty() && addr != sbAddr) {
                    candidates.add(addr to fb.token.orEmpty())
                }
            }
            var lastErr = err
            for ((addr, tok) in candidates) {
                try {
                    connectEndpoint(profile, addr, tok, isStandby = true, seq = seq)
                    return
                } catch (e2: Throwable) {
                    if (seq != connSeq) return
                    lastErr = e2
                    if ((e2 as? HubException)?.unauthorized == true) break
                }
            }
            handleConnectError(lastErr)
        }
    }

    private fun handleConnectError(error: Throwable) {
        usingStandby = false
        if (error is HubException && error.unauthorized) {
            connState = ConnState.UNAUTHORIZED
            connError = error.message
        } else {
            connState = ConnState.ERROR
            connError = errText(error, L.t("connFailed"))
        }
    }

    private suspend fun connectEndpoint(
        profile: ServerProfile,
        rawAddress: String,
        rawToken: String,
        isStandby: Boolean,
        seq: Int,
    ) {
        val norm = normalizeAddress(rawAddress)
        var tok = rawToken.ifEmpty { norm.token }
        var url = norm.cleanAddress
        try {
            if (norm.isTailscale) {
                if (mobileTailscale?.state != "running") {
                    val st = PebbleTunnel.tailscaleStart(appContext, controlUrl = tsControlUrl)
                    mobileTailscale = st
                    if (st.state == "needs_login" && !st.authUrl.isNullOrBlank()) {
                        onOpenExternalUrl?.invoke(st.authUrl)
                    }
                }
                val r = PebbleTunnel.start(norm.cleanAddress, tok)
                if (seq != connSeq) return
                url = r.url
                if (tok.isEmpty() && r.token.isNotEmpty()) tok = r.token
            } else if (norm.isTailcat) {
                val r = PebbleTunnel.start(norm.cleanAddress, tok)
                if (seq != connSeq) return
                url = r.url
                if (tok.isEmpty() && r.token.isNotEmpty()) tok = r.token
            } else {
                PebbleTunnel.stop()
            }
            if (seq != connSeq) return

            if (!isStandby && (tok != profile.token || norm.cleanAddress != profile.address)) {
                val list = servers.toMutableList()
                val idx = list.indexOfFirst { it.id == profile.id }
                if (idx >= 0) {
                    list[idx] = list[idx].copy(address = norm.cleanAddress, token = tok)
                    servers = list
                    persistServers()
                }
            }
            isTailcat = norm.isTailcat
            isTailscale = norm.isTailscale
            baseURL = url
            authToken = tok

            val client = ApiClient(url, tok)
            val data = client.call<BotsResp>("/api/bots")
            if (seq != connSeq) return
            val list = data.bots.orEmpty()
            bots = list
            msgs.clear(); last.clear(); unread.clear(); tools.clear(); models.clear(); routines.clear()
            usingStandby = isStandby
            connState = ConnState.CONNECTED
            connError = ""

            for (b in list) {
                scope.launch {
                    runCatching { client.call<MessagesResp>("/api/bots/${encodeURIComponent(b.id)}/messages?limit=1") }
                        .getOrNull()?.messages?.lastOrNull()?.let { m ->
                            if (seq == connSeq) last[b.id] = m
                        }
                }
            }
            scope.launch { runCatching { client.call<SettingsData>("/api/settings") }.getOrNull()?.let { if (seq == connSeq) settings = it } }
            scope.launch { runCatching { client.call<NodesResp>("/api/nodes") }.getOrNull()?.nodes?.let { if (seq == connSeq) nodes = it } }
            scope.launch { runCatching { client.call<TailcatStatus>("/api/tailcat") }.getOrNull()?.let { if (seq == connSeq) tailcat = it } }
            scope.launch { runCatching { client.call<HostTailscaleStatus>("/api/tailscale") }.getOrNull()?.let { if (seq == connSeq) hostTailscale = it } }
            scope.launch { runCatching { client.call<TakeoversResp>("/api/desktop/takeovers") }.getOrNull()?.items?.let { if (seq == connSeq) takeovers = it } }
            scope.launch {
                runCatching { client.call<TargetsResp>("/api/desktop/targets") }.getOrNull()?.let { d ->
                    if (seq == connSeq) {
                        desktopTargets = d.targets.orEmpty()
                        d.activeTarget?.let { activeDesktopTarget = it }
                    }
                }
            }
            scope.launch { runCatching { client.call<QuestionsResp>("/api/questions") }.getOrNull()?.items?.let { if (seq == connSeq) questions = it } }
            startStream(url, tok)
        } catch (err: Throwable) {
            if (seq == connSeq) {
                baseURL = url
                authToken = tok
                isTailcat = norm.isTailcat
                isTailscale = norm.isTailscale
            }
            throw err
        }
    }

    suspend fun resume() {
        val cur = activeServer ?: return
        if (baseURL.isEmpty()) return
        val url = baseURL
        val tok = authToken
        try {
            val d = api.call<BotsResp>("/api/bots")
            bots = d.bots.orEmpty()
            startStream(url, tok)
            activeChat?.let { bid ->
                val m = api.call<MessagesResp>("/api/bots/${encodeURIComponent(bid)}/messages?limit=200")
                msgs[bid] = m.messages.orEmpty()
            }
        } catch (_: Throwable) {
            connectProfile(cur)
        }
    }

    suspend fun addAndConnectServer(
        label: String,
        address: String,
        token: String,
        standbyAddress: String? = null,
        standbyToken: String? = null,
    ) {
        val norm = normalizeAddress(address)
        if (norm.cleanAddress.isEmpty()) throw HubException(L.t("enterValidAddr"))
        val tok = token.ifEmpty { norm.token }.trim()
        val lbl = label.trim()
        val sbTokTrim = standbyToken.orEmpty().trim()
        val sbParts = standbyAddress.orEmpty()
            .split(',', '\n')
            .map { it.trim() }
            .filter { it.isNotEmpty() }
        val fallbacks = mutableListOf<FallbackHost>()
        for (part in sbParts) {
            val fn = normalizeAddress(part)
            if (fn.cleanAddress.isNotEmpty()) {
                val ft = sbTokTrim.ifEmpty { fn.token.ifEmpty { null } }
                fallbacks.add(FallbackHost(label = null, address = fn.cleanAddress, token = ft))
            }
        }
        val sbAddr = fallbacks.firstOrNull()?.address
        val sbTok = fallbacks.firstOrNull()?.token

        val list = servers.toMutableList()
        val existingIdx = list.indexOfFirst { it.address == norm.cleanAddress }
        val target: ServerProfile
        if (existingIdx >= 0) {
            val cur = list[existingIdx]
            target = cur.copy(
                label = lbl.ifEmpty { cur.label },
                token = tok.ifEmpty { cur.token },
                standbyAddress = if (!sbAddr.isNullOrEmpty()) sbAddr else cur.standbyAddress,
                standbyToken = if (!sbTok.isNullOrEmpty()) sbTok else cur.standbyToken,
                fallbackHosts = if (fallbacks.isNotEmpty()) fallbacks else cur.fallbackHosts,
            )
            list[existingIdx] = target
        } else {
            val def = if (norm.isTailcat) {
                L.t("remoteHostDefault")
            } else {
                norm.cleanAddress.replace(Regex("^https?://"), "")
            }
            target = ServerProfile(
                id = System.currentTimeMillis().toString(),
                label = lbl.ifEmpty { def },
                address = norm.cleanAddress,
                token = tok,
                standbyAddress = if (!sbAddr.isNullOrEmpty()) sbAddr else null,
                standbyToken = sbTok,
                fallbackHosts = if (fallbacks.isEmpty()) null else fallbacks,
            )
            list.add(0, target)
        }
        servers = list
        activeServerId = target.id
        persistServers()
        connectProfile(target)
    }

    suspend fun selectServer(id: String) {
        val target = servers.firstOrNull { it.id == id } ?: return
        activeServerId = id
        persistServers()
        connectProfile(target)
    }

    suspend fun removeServer(id: String) {
        val list = servers.filterNot { it.id == id }
        val removingActive = activeServerId == id
        servers = list
        if (removingActive) activeServerId = list.firstOrNull()?.id
        persistServers()
        if (!removingActive) return
        val next = list.firstOrNull()
        if (next != null) {
            connectProfile(next)
        } else {
            connSeq += 1
            stream.stop()
            PebbleTunnel.stop()
            baseURL = ""; authToken = ""
            bots = emptyList(); msgs.clear(); last.clear()
            connState = ConnState.IDLE
            connError = ""
        }
    }

    suspend fun reconnect() {
        activeServer?.let { connectProfile(it) }
    }

    // MARK: Bots & Messages

    suspend fun refreshBots() {
        if (baseURL.isEmpty()) return
        bots = api.call<BotsResp>("/api/bots").bots.orEmpty()
    }

    suspend fun loadMessages(botId: String) {
        if (baseURL.isEmpty()) return
        val list = api.call<MessagesResp>("/api/bots/${encodeURIComponent(botId)}/messages?limit=200").messages.orEmpty()
        msgs[botId] = list
        list.lastOrNull()?.let { last[botId] = it }
    }

    suspend fun loadCollabMessages(botId: String, peerId: String, limit: Int = 200): List<ChatMessage> {
        if (baseURL.isEmpty()) return emptyList()
        return api.call<MessagesResp>(
            "/api/bots/${encodeURIComponent(botId)}/collab/${encodeURIComponent(peerId)}?limit=$limit"
        ).messages.orEmpty()
    }

    suspend fun fetchMediaBytes(path: String): ByteArray {
        val key = "$baseURL$path"
        synchronized(mediaCache) {
            mediaCache[key]?.let { return it }
        }
        val bytes = api.rawBytes(path)
        val maxEntries = 64
        val maxBytes = 32L * 1024L * 1024L
        synchronized(mediaCache) {
            mediaCache.remove(key)?.let { mediaCacheBytes -= it.size }
            while (mediaCache.isNotEmpty() && (mediaCache.size >= maxEntries || mediaCacheBytes + bytes.size > maxBytes)) {
                val it = mediaCache.entries.iterator()
                val eldest = it.next()
                mediaCacheBytes -= eldest.value.size
                it.remove()
            }
            mediaCache[key] = bytes
            mediaCacheBytes += bytes.size
        }
        return bytes
    }

    suspend fun fetchCommands(botId: String): List<SlashCommandItem> =
        api.call<CommandsResp>("/api/bots/${encodeURIComponent(botId)}/commands").commands.orEmpty()

    suspend fun fetchMentions(botId: String, query: String): List<MentionCandidate> =
        api.call<MentionsResp>(
            "/api/bots/${encodeURIComponent(botId)}/mentions?q=${encodeURIComponent(query)}"
        ).items.orEmpty()

    suspend fun uploadAttachment(
        botId: String,
        bytes: ByteArray,
        filename: String,
        mimeType: String,
    ): InboundAttachmentResponse =
        api.uploadAttachment("/api/bots/${encodeURIComponent(botId)}/attachments", bytes, filename, mimeType)

    suspend fun sendMessage(botId: String, text: String, mediaIds: List<String> = emptyList()) {
        val body = mutableMapOf<String, Any?>("message" to text, "source" to "mobile")
        if (mediaIds.isNotEmpty()) body["mediaIds"] = mediaIds
        val d = api.call<TextResp>("/api/bots/${encodeURIComponent(botId)}/message", method = "POST", body = body)
        if (text in listOf("/bots", "/status", "/reset") && !d.text.isNullOrEmpty()) {
            pushIncoming(
                botId,
                ChatMessage(
                    id = "cmd_${System.currentTimeMillis()}",
                    botId = botId,
                    role = "system",
                    from = "command",
                    text = d.text,
                    createdAt = Instant.now().toString(),
                ),
            )
        }
        if (text == "/reset") refreshBots()
    }

    suspend fun interruptBot(botId: String): Boolean =
        api.call<InterruptResp>("/api/bots/${encodeURIComponent(botId)}/interrupt", method = "POST").interrupted == true

    suspend fun resetBot(botId: String) {
        api.rawText("/api/bots/${encodeURIComponent(botId)}/reset", method = "POST")
        refreshBots()
    }

    suspend fun botDetail(botId: String): BotStatus =
        api.call("/api/bots/${encodeURIComponent(botId)}")

    suspend fun createBot(body: Map<String, Any?>): BotStatus {
        val st = api.call<BotStatus>("/api/bots", method = "POST", body = body)
        if (bots.none { it.id == st.id }) bots = bots + st
        return st
    }

    suspend fun updateBot(botId: String, body: Map<String, Any?>): BotStatus {
        val st = api.call<BotStatus>("/api/bots/${encodeURIComponent(botId)}", method = "PATCH", body = body)
        val list = bots.toMutableList()
        val idx = list.indexOfFirst { it.id == st.id }
        if (idx >= 0) list[idx] = st
        bots = list
        return st
    }

    suspend fun deleteBot(botId: String) {
        api.rawText("/api/bots/${encodeURIComponent(botId)}", method = "DELETE")
        removeBotState(botId)
    }

    // MARK: Memory

    suspend fun fetchMemories(
        scope: String,
        botId: String? = null,
        query: String = "",
        kind: String = "",
        limit: Int = 0,
    ): MemoryList {
        val params = mutableListOf("scope=${encodeURIComponent(scope)}")
        if (!botId.isNullOrBlank()) params.add("botId=${encodeURIComponent(botId)}")
        if (query.trim().isNotEmpty()) params.add("query=${encodeURIComponent(query.trim())}")
        if (kind.trim().isNotEmpty()) params.add("kind=${encodeURIComponent(kind.trim())}")
        if (limit > 0) params.add("limit=$limit")
        return api.call("/api/memory?${params.joinToString("&")}")
    }

    suspend fun saveMemory(body: Map<String, Any?>) {
        api.rawText("/api/memory", method = "POST", body = body)
    }

    suspend fun deleteMemory(id: String) {
        api.rawText("/api/memory/${encodeURIComponent(id)}", method = "DELETE")
    }

    // MARK: Routines

    suspend fun fetchRoutines(botId: String): List<RoutineItem> {
        val list = api.call<RoutinesResp>("/api/bots/${encodeURIComponent(botId)}/routines").routines.orEmpty()
        routines[botId] = list
        return list
    }

    suspend fun createRoutine(botId: String, body: Map<String, Any?>) {
        api.rawText("/api/bots/${encodeURIComponent(botId)}/routines", method = "POST", body = body)
        fetchRoutines(botId)
    }

    suspend fun updateRoutine(botId: String, rid: String, body: Map<String, Any?>) {
        api.rawText("/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rid)}", method = "PATCH", body = body)
        fetchRoutines(botId)
    }

    suspend fun deleteRoutine(botId: String, rid: String) {
        api.rawText("/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rid)}", method = "DELETE")
        fetchRoutines(botId)
    }

    suspend fun runRoutine(botId: String, rid: String) {
        api.rawText("/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(rid)}/run?async=1", method = "POST")
        fetchRoutines(botId)
    }

    suspend fun fetchRoutineRuns(botId: String, rid: String, limit: Int = 6): List<RoutineRun> =
        api.call<RunsResp>(
            "/api/bots/${encodeURIComponent(botId)}/routine-runs?routineId=${encodeURIComponent(rid)}&limit=$limit"
        ).runs.orEmpty()

    // MARK: Settings & Magpie

    suspend fun loadSettings(): SettingsData {
        val s = api.call<SettingsData>("/api/settings")
        settings = s
        return s
    }

    suspend fun loadModels(harness: String): List<String> {
        val list = api.call<ModelsResp>("/api/models?harness=${encodeURIComponent(harness)}").models.orEmpty()
        models[harness] = list
        return list
    }

    suspend fun saveSettings(
        changed: Boolean,
        defaultHarness: String,
        defaultModel: String,
        defaultModels: Map<String, String>,
        sharedUserMd: String,
        botUpdates: List<Triple<String, String, String>>,
    ) {
        if (changed) {
            settings = api.call(
                "/api/settings",
                method = "PUT",
                body = mapOf(
                    "defaultHarness" to defaultHarness,
                    "defaultModel" to defaultModel,
                    "defaultModels" to defaultModels,
                    "sharedUserMd" to sharedUserMd,
                ),
            )
        }
        for ((bid, h, m) in botUpdates) {
            updateBot(bid, mapOf("harness" to h, "model" to m))
        }
        loadSettings()
        refreshBots()
    }

    suspend fun connectMagpie(harness: String, model: String) {
        settings = api.call(
            "/api/harnesses/${encodeURIComponent(harness)}/magpie",
            method = "POST",
            body = mapOf("action" to "connect", "model" to model),
        )
        models.clear()
    }

    suspend fun disconnectMagpie(harness: String) {
        settings = api.call(
            "/api/harnesses/${encodeURIComponent(harness)}/magpie",
            method = "POST",
            body = mapOf("action" to "disconnect"),
        )
        models.clear()
    }

    suspend fun openMagpie() {
        api.call<OkResp>("/api/magpie/open", method = "POST", body = emptyMap())
    }

    suspend fun installMagpie() {
        var st = api.call<SetupTaskStatus>("/api/magpie/install", method = "POST", body = emptyMap())
        while (st.state == "running") {
            delay(1200)
            st = api.call("/api/magpie/install")
        }
        if (st.state == "error") {
            throw HubException(st.error ?: "Magpie install failed")
        }
        loadSettings()
    }

    suspend fun setDesktopWorker(harness: String, model: String) {
        settings = api.call(
            "/api/settings",
            method = "PUT",
            body = mapOf(
                "desktopHarness" to harness.trim(),
                "desktopModel" to model.trim(),
            ),
        )
    }

    // MARK: Nodes, Tunnel & Desktop

    suspend fun refreshNodes() {
        if (baseURL.isEmpty()) return
        nodes = api.call<NodesResp>("/api/nodes").nodes.orEmpty()
    }

    suspend fun refreshTailcat() {
        tailcat = api.call("/api/tailcat")
    }

    suspend fun refreshHostTailscale() {
        hostTailscale = api.call("/api/tailscale")
    }

    suspend fun refreshMobileTailscale(includePeers: Boolean = true) {
        val st = PebbleTunnel.tailscaleStatus(includePeers)
        mobileTailscale = st
        if (st.state == "running" && !tsBound) {
            tsBound = true
            prefs.edit().putBoolean("pebble.tsBound", true).apply()
        }
    }

    suspend fun bindTailscale(authKey: String = "", controlUrl: String = "") {
        tsBinding = true
        try {
            val ctrl = controlUrl.trim()
            val key = authKey.trim()
            var st = PebbleTunnel.tailscaleStart(appContext, controlUrl = ctrl, authKey = key)
            mobileTailscale = st
            var openedAuth = ""
            val auth = st.authUrl
            if (st.state == "needs_login" && !auth.isNullOrBlank()) {
                openedAuth = auth
                onOpenExternalUrl?.invoke(openedAuth)
            }
            val deadline = System.currentTimeMillis() + 180_000L
            while ((st.state == "starting" || st.state == "needs_login") && System.currentTimeMillis() < deadline) {
                delay(1500)
                st = PebbleTunnel.tailscaleStatus(includePeers = true)
                mobileTailscale = st
                val nextAuth = st.authUrl
                if (st.state == "needs_login" && !nextAuth.isNullOrBlank() && nextAuth != openedAuth) {
                    openedAuth = nextAuth
                    onOpenExternalUrl?.invoke(openedAuth)
                }
            }
            if (st.state == "running") {
                tsBound = true
                tsControlUrl = ctrl
                prefs.edit()
                    .putBoolean("pebble.tsBound", true)
                    .putString("pebble.tsControlUrl", ctrl)
                    .apply()
                if (st.peers.isNullOrEmpty()) {
                    refreshMobileTailscale(includePeers = true)
                }
            }
        } finally {
            tsBinding = false
        }
    }

    suspend fun logoutTailscale() {
        PebbleTunnel.tailscaleLogout()
        tsBound = false
        tsControlUrl = ""
        prefs.edit()
            .putBoolean("pebble.tsBound", false)
            .putString("pebble.tsControlUrl", "")
            .apply()
        mobileTailscale = null
    }

    suspend fun refreshDesktopTargets(): List<DesktopTarget> {
        val d = api.call<TargetsResp>("/api/desktop/targets")
        desktopTargets = d.targets.orEmpty()
        d.activeTarget?.let { activeDesktopTarget = it }
        takeovers = d.takeovers.orEmpty()
        return desktopTargets
    }

    suspend fun setActiveDesktopTarget(target: String) {
        val res = api.call<ActiveTargetResp>("/api/desktop/target", method = "PUT", body = mapOf("target" to target))
        res.activeTarget?.let { activeDesktopTarget = it }
    }

    suspend fun fetchDesktopState(target: String): DesktopState {
        val d = api.call<DesktopState>("/api/desktop/state?target=${encodeURIComponent(target)}")
        takeovers = d.takeovers.orEmpty()
        return d
    }

    suspend fun fetchDesktopPermissions(target: String): DesktopPermissionsResp =
        api.call("/api/desktop/permissions?target=${encodeURIComponent(target)}")

    suspend fun requestDesktopPermission(
        target: String,
        permission: String,
        openSettings: Boolean = false,
    ): DesktopPermissionRequestResp =
        api.call(
            "/api/desktop/permissions/request",
            method = "POST",
            body = mapOf(
                "target" to target,
                "permission" to permission,
                "openSettings" to openSettings,
            ),
        )

    fun makeDesktopWsUrl(target: String, preset: String? = null): String? {
        if (baseURL.isBlank()) return null
        val wsBase = when {
            baseURL.startsWith("https://", ignoreCase = true) -> "wss://" + baseURL.substring(8)
            baseURL.startsWith("http://", ignoreCase = true) -> "ws://" + baseURL.substring(7)
            else -> return null
        }
        val presetPart = if (!preset.isNullOrBlank()) "&preset=${encodeURIComponent(preset)}" else ""
        return "$wsBase/api/desktop/stream/ws?target=${encodeURIComponent(target)}&codec=hevc$presetPart"
    }

    suspend fun sendDesktopInput(body: Map<String, Any?>) {
        api.rawText("/api/desktop/input", method = "POST", body = body)
    }

    suspend fun resolveTakeover(id: String) {
        val r = api.call<TakeoversResp>(
            "/api/desktop/takeover/resolve",
            method = "POST",
            body = mapOf("id" to id, "note" to L.t("takeoverResolvedNote")),
        )
        takeovers = r.items.orEmpty()
    }

    suspend fun replyQuestion(id: String, answers: Map<String, Any?>, cancelled: Boolean) {
        val r = api.call<QuestionsResp>(
            "/api/questions/${encodeURIComponent(id)}/reply",
            method = "POST",
            body = mapOf("answers" to answers, "cancelled" to cancelled),
        )
        questions = r.items.orEmpty()
    }

    // MARK: Workspace Files & Stage

    suspend fun listWorkspaceFiles(
        botId: String,
        path: String = "",
        showHidden: Boolean = false,
    ): WorkspaceListResult = api.listWorkspaceFiles(botId, path, showHidden)

    suspend fun readWorkspaceFile(botId: String, path: String): WorkspaceFileContent =
        api.readWorkspaceFile(botId, path)

    suspend fun workspaceFileImageURL(botId: String, path: String): String {
        val tk = api.issueWorkspaceFileTicket(botId, path)
        return api.workspaceFileRawUrl(botId, path, tk.ticket, download = false)
    }

    suspend fun downloadWorkspaceFileToTemp(botId: String, path: String, fileName: String): java.io.File =
        api.downloadWorkspaceFileToTemp(appContext.cacheDir, botId, path, fileName)

    fun insertIntoChat(text: String) {
        val nextSeq = (pendingChatInsert?.seq ?: 0) + 1
        pendingChatInsert = PendingChatInsert(seq = nextSeq, text = text)
    }

    fun consumePendingStageOpen() {
        pendingStageOpen = null
    }
}
