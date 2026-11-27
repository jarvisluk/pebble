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
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.serialization.encodeToString

class PebbleStore(context: Context) {
    internal val appContext = context.applicationContext
    internal val prefs = appContext.getSharedPreferences("pebble_prefs", Context.MODE_PRIVATE)
    private val secureStore = SecureStore(appContext)
    internal val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    internal val stream = EventStream()
    internal val mediaCache = LinkedHashMap<String, ByteArray>(64, 0.75f, true)
    internal var mediaCacheBytes = 0L

    // Preferences
    var themePref by mutableStateOf(prefs.getString("pebble.theme", "system") ?: "system")
        internal set
    var langPref by mutableStateOf(prefs.getString("pebble.lang", "system") ?: "system")
        internal set
    var tsBound by mutableStateOf(prefs.getBoolean("pebble.tsBound", false))
        internal set
    var tsControlUrl by mutableStateOf(prefs.getString("pebble.tsControlUrl", "") ?: "")
        internal set
    var sortMode by mutableStateOf(
        BotSortMode.entries.firstOrNull { it.raw == prefs.getString("pebble.sortMode", "recent") } ?: BotSortMode.RECENT
    )
        internal set
    var pinnedIds by mutableStateOf(loadStringList("pebble.pinned"))
        internal set
    var manualOrder by mutableStateOf(loadStringList("pebble.manualOrder"))
        internal set
    var langRevision by mutableIntStateOf(0)
        internal set
    var unboxPermsDone by mutableStateOf(prefs.getBoolean("pebble.unboxPermsDone", false))
        internal set

    // Connection
    var servers by mutableStateOf<List<ServerProfile>>(emptyList())
        internal set
    var activeServerId by mutableStateOf<String?>(prefs.getString("pebble.activeServer", null))
        internal set
    var connState by mutableStateOf(ConnState.IDLE)
        internal set
    var connError by mutableStateOf("")
        internal set
    var baseURL by mutableStateOf("")
        internal set
    var authToken by mutableStateOf("")
        internal set
    var isTailcat by mutableStateOf(false)
        internal set
    var isTailscale by mutableStateOf(false)
        internal set
    var online by mutableStateOf(false)
        internal set
    var usingStandby by mutableStateOf(false)
        internal set

    // Domain Data
    var bots by mutableStateOf<List<BotStatus>>(emptyList())
        internal set
    val msgs = mutableStateMapOf<String, List<ChatMessage>>()
    val last = mutableStateMapOf<String, ChatMessage>()
    val unread = mutableStateMapOf<String, Int>()
    val tools = mutableStateMapOf<String, String>()
    var settings by mutableStateOf<SettingsData?>(null)
        internal set
    val models = mutableStateMapOf<String, List<String>>()
    var modelsGeneration by mutableIntStateOf(0)
        internal set
    var nodes by mutableStateOf<List<NodeInfo>>(emptyList())
        internal set
    var tailcat by mutableStateOf<TailcatStatus?>(null)
        internal set
    var mobileTailscale by mutableStateOf<MobileTailscaleStatus?>(null)
        internal set
    var hostTailscale by mutableStateOf<HostTailscaleStatus?>(null)
        internal set
    var tsBinding by mutableStateOf(false)
        internal set
    var takeovers by mutableStateOf<List<TakeoverRequest>>(emptyList())
        internal set
    var questions by mutableStateOf<List<PendingQuestion>>(emptyList())
        internal set
    var approvals by mutableStateOf<List<PendingApproval>>(emptyList())
        internal set
    var desktopTargets by mutableStateOf<List<DesktopTarget>>(emptyList())
        internal set
    var activeDesktopTarget by mutableStateOf(ActiveDesktopTarget(target = "local"))
        internal set
    var serverInfo by mutableStateOf<ServerInfo?>(null)
        internal set
    var unsupportedCaps by mutableStateOf<Set<String>>(emptySet())
        internal set
    var dismissedCompatProto by mutableIntStateOf(0)
        internal set
    var updateStatus by mutableStateOf<UpdateStatus?>(null)
        internal set
    var updateChecking by mutableStateOf(false)
        internal set
    var dismissedUpdateVersion by mutableStateOf(prefs.getString("pebble.dismissedUpdate", "").orEmpty())
        internal set
    val routines = mutableStateMapOf<String, List<RoutineItem>>()
    val tasks = mutableStateMapOf<String, List<BotTask>>()
    val taskEvents = mutableStateMapOf<String, List<BotTaskEvent>>()
    val taskTools = mutableStateMapOf<String, String>()
    val taskStreamText = mutableStateMapOf<String, String>()
    var pendingStageOpen by mutableStateOf<StageOpenEvent?>(null)
        internal set
    var pendingChatInsert by mutableStateOf<PendingChatInsert?>(null)
        internal set

    fun supports(cap: String): Boolean {
        if (cap in unsupportedCaps) return false
        val info = serverInfo
        if (info != null) {
            return info.capabilities?.contains(cap) == true
        }
        return true
    }

    fun markUnsupported(cap: String) {
        if (cap !in unsupportedCaps) {
            unsupportedCaps = unsupportedCaps + cap
        }
    }

    val isClientOutdated: Boolean
        get() = (serverInfo?.protocolVersion ?: 0) > CLIENT_PROTOCOL

    val isClientUpgradeRequired: Boolean
        get() = (serverInfo?.minClientProtocol ?: 1) > CLIENT_PROTOCOL

    val showCompatBanner: Boolean
        get() {
            val info = serverInfo ?: return false
            if (info.protocolVersion <= CLIENT_PROTOCOL) return false
            return isClientUpgradeRequired || dismissedCompatProto != info.protocolVersion
        }

    fun dismissCompatBanner() {
        serverInfo?.protocolVersion?.let { dismissedCompatProto = it }
    }

    val effectiveHasUpdate: Boolean
        get() {
            val st = updateStatus ?: return false
            if (st.hasUpdate) return true
            val ver = st.latest?.version.orEmpty()
            return ver.isNotEmpty() && isSemVerNewer(ver, ai.pebble.android.BuildConfig.VERSION_NAME)
        }

    val showUpdateBanner: Boolean
        get() {
            if (isClientUpgradeRequired || !effectiveHasUpdate) return false
            val ver = updateStatus?.latest?.version.orEmpty()
            return ver.isNotEmpty() && dismissedUpdateVersion != ver
        }

    fun dismissUpdateBanner() {
        val ver = updateStatus?.latest?.version.orEmpty()
        if (ver.isEmpty()) return
        dismissedUpdateVersion = ver
        prefs.edit().putString("pebble.dismissedUpdate", ver).apply()
    }

    companion object {
        fun isSemVerNewer(candidate: String, current: String): Boolean = isSemVerNewerImpl(candidate, current)

        fun isLocalDesktopTarget(target: String): Boolean =
            target.isEmpty() || target == "local" || target.startsWith("virtual:") || target.startsWith("local:virtual:")
    }

    // UI Callback for opening external URLs (Tailscale auth)
    var onOpenExternalUrl: ((String) -> Unit)? = null

    internal var activeChat: String? = null
    internal var connSeq = 0
    internal var inForeground = false
    internal val botSteps = mutableMapOf<String, Int>()
    internal val activeBusyBots = mutableSetOf<String>()
    internal val activeTakeoverIds = mutableSetOf<String>()
    internal val activeApprovalIds = mutableSetOf<String>()
    internal val modelLoadJobs = mutableMapOf<String, Deferred<List<String>>>()

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
        NotificationHelper.ensureChannels(appContext)
    }

    fun markUnboxPermsDone() {
        unboxPermsDone = true
        prefs.edit().putBoolean("pebble.unboxPermsDone", true).apply()
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

    internal fun persistServers() {
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

    fun setActiveChat(botId: String?) {
        activeChat = botId
        if (botId != null) {
            unread.remove(botId)
        }
    }

    fun toggleUnread(id: String) {
        if ((unread[id] ?: 0) > 0) unread.remove(id) else unread[id] = 1
    }

    internal fun removeBotState(id: String) {
        bots = bots.filterNot { it.id == id }
        msgs.remove(id)
        last.remove(id)
        unread.remove(id)
        tools.remove(id)
        botSteps.remove(id)
        activeBusyBots.remove(id)
        tasks.remove(id)?.forEach { t ->
            taskEvents.remove(t.id)
            taskTools.remove(t.id)
            taskStreamText.remove(t.id)
        }
    }

    internal fun pushIncoming(botId: String, msg: ChatMessage) {
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

    internal fun updateIncoming(botId: String, msg: ChatMessage) {
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

    internal fun upsertBot(st: BotStatus) {
        val list = bots.toMutableList()
        val idx = list.indexOfFirst { it.id == st.id }
        if (idx >= 0) list[idx] = st else list.add(st)
        bots = list
    }

    internal fun startStream(url: String, tok: String) = startStreamImpl(url, tok)

    internal fun syncBackgroundService() = syncBackgroundServiceImpl()
    suspend fun connectProfile(profile: ServerProfile) = connectProfileImpl(profile)
    suspend fun resume() = resumeImpl()
    suspend fun addAndConnectServer(
        label: String,
        address: String,
        token: String,
        standbyAddress: String? = null,
        standbyToken: String? = null,
    ) = addAndConnectServerImpl(label, address, token, standbyAddress, standbyToken)
    suspend fun selectServer(id: String) = selectServerImpl(id)
    suspend fun removeServer(id: String) = removeServerImpl(id)
    suspend fun reconnect() = reconnectImpl()

    suspend fun refreshBots() = refreshBotsImpl()
    suspend fun loadMessages(botId: String) = loadMessagesImpl(botId)
    suspend fun loadCollabMessages(botId: String, peerId: String, limit: Int = 200) : List<ChatMessage> = loadCollabMessagesImpl(botId, peerId, limit)
    suspend fun fetchMediaBytes(path: String) : ByteArray = fetchMediaBytesImpl(path)
    suspend fun fetchCommands(botId: String) : List<SlashCommandItem> = fetchCommandsImpl(botId)
    suspend fun fetchMentions(botId: String, query: String) : List<MentionCandidate> = fetchMentionsImpl(botId, query)
    suspend fun uploadAttachment(
        botId: String,
        bytes: ByteArray,
        filename: String,
        mimeType: String,
    ) : InboundAttachmentResponse = uploadAttachmentImpl(botId, bytes, filename, mimeType)
    suspend fun sendMessage(botId: String, text: String, mediaIds: List<String> = emptyList()) = sendMessageImpl(botId, text, mediaIds)
    suspend fun interruptBot(botId: String) : Boolean = interruptBotImpl(botId)
    suspend fun resetBot(botId: String) = resetBotImpl(botId)
    suspend fun botDetail(botId: String) : BotStatus = botDetailImpl(botId)
    suspend fun createBot(body: Map<String, Any?>) : BotStatus = createBotImpl(body)
    suspend fun updateBot(botId: String, body: Map<String, Any?>) : BotStatus = updateBotImpl(botId, body)
    suspend fun deleteBot(botId: String) = deleteBotImpl(botId)
    suspend fun fetchMemories(
        scope: String,
        botId: String? = null,
        query: String = "",
        kind: String = "",
        tags: String = "",
        status: String = "",
        limit: Int = 50,
    ): MemoryList = fetchMemoriesImpl(scope, botId, query, kind, tags, status, limit)
    suspend fun saveMemory(body: Map<String, Any?>): MemorySaveResp = saveMemoryImpl(body)
    suspend fun patchMemory(id: String, body: Map<String, Any?>): MemoryPatchResp = patchMemoryImpl(id, body)
    suspend fun deleteMemory(id: String, hard: Boolean = false, dismiss: Boolean = false) = deleteMemoryImpl(id, hard, dismiss)
    suspend fun fetchMemoryIndex(scope: String? = null, botId: String? = null): MemoryIndexDoc = fetchMemoryIndexImpl(scope, botId)
    suspend fun saveMemoryIndex(content: String, scope: String? = null, botId: String? = null): MemoryIndexDoc = saveMemoryIndexImpl(content, scope, botId)
    suspend fun fetchMemoryStatus(botId: String? = null): MemoryStatusResp = fetchMemoryStatusImpl(botId)
    suspend fun runMemoryDream(botId: String? = null): MemoryDreamRunResp = runMemoryDreamImpl(botId)
    suspend fun reindexMemory(): MemoryReindexResp = reindexMemoryImpl()
    suspend fun importMagpieMemory(mode: String, botId: String? = null): MemoryImportResp = importMagpieMemoryImpl(mode, botId)
    suspend fun saveMemoryConfig(memory: Map<String, Any?>): SettingsData = saveMemoryConfigImpl(memory)
    suspend fun fetchRoutines(botId: String) : List<RoutineItem> = fetchRoutinesImpl(botId)
    suspend fun createRoutine(botId: String, body: Map<String, Any?>) = createRoutineImpl(botId, body)
    suspend fun updateRoutine(botId: String, rid: String, body: Map<String, Any?>) = updateRoutineImpl(botId, rid, body)
    suspend fun deleteRoutine(botId: String, rid: String) = deleteRoutineImpl(botId, rid)
    suspend fun runRoutine(botId: String, rid: String) = runRoutineImpl(botId, rid)
    suspend fun fetchRoutineRuns(botId: String, rid: String, limit: Int = 6) : List<RoutineRun> = fetchRoutineRunsImpl(botId, rid, limit)
    fun upsertBotTask(task: BotTask) = upsertBotTaskImpl(task)
    suspend fun fetchBotTasks(
        botId: String,
        kind: String? = null,
        status: String? = null,
        limit: Int = 50,
    ): List<BotTask> = fetchBotTasksImpl(botId, kind, status, limit)
    suspend fun spawnBotTask(
        botId: String,
        title: String? = null,
        prompt: String,
        kind: String = "worker",
        deviceId: String = "local",
        harness: String? = null,
        model: String? = null,
        permissionMode: String? = null,
        workdir: String? = null,
        delivery: String = "receipt",
    ): BotTask = spawnBotTaskImpl(botId, title, prompt, kind, deviceId, harness, model, permissionMode, workdir, delivery)
    suspend fun fetchTaskEvents(taskId: String, afterSeq: Long = 0L, limit: Int = 200): List<BotTaskEvent> =
        fetchTaskEventsImpl(taskId, afterSeq, limit)
    suspend fun sendToTask(taskId: String, message: String): BotTask = sendToTaskImpl(taskId, message)
    suspend fun cancelTask(taskId: String): BotTask = cancelTaskImpl(taskId)
    suspend fun fetchTaskDiff(taskId: String): TaskDiffResult = fetchTaskDiffImpl(taskId)
    suspend fun rollbackTask(taskId: String, removeWorktree: Boolean = false): TaskRollbackResult =
        rollbackTaskImpl(taskId, removeWorktree)

    suspend fun loadSettings(fresh: Boolean = false) : SettingsData = loadSettingsImpl(fresh)
    suspend fun loadModels(harness: String, force: Boolean = false) : List<String> = loadModelsImpl(harness, force)
    fun resetModelsCache() = resetModelsCacheImpl()
    suspend fun saveModelsSettings(
        changed: Boolean,
        defaultHarness: String,
        defaultPermissionMode: String? = null,
        defaultModel: String,
        defaultModelsPatch: Map<String, String>? = null,
        botUpdates: List<Triple<String, String, String>>,
    ) = saveModelsSettingsImpl(changed, defaultHarness, defaultPermissionMode, defaultModel, defaultModelsPatch, botUpdates)
    suspend fun saveSharedUserMd(content: String) = saveSharedUserMdImpl(content)
    suspend fun startHarnessInstall(harness: String) : SetupTaskStatus = startHarnessInstallImpl(harness)
    suspend fun getHarnessInstallStatus(harness: String) : SetupTaskStatus = getHarnessInstallStatusImpl(harness)
    suspend fun startHarnessLogin(harness: String) : SetupTaskStatus = startHarnessLoginImpl(harness)
    suspend fun getHarnessLoginStatus(harness: String) : SetupTaskStatus = getHarnessLoginStatusImpl(harness)
    suspend fun setDesktopWorker(harness: String, model: String) = setDesktopWorkerImpl(harness, model)
    suspend fun refreshNodes() = refreshNodesImpl()

    suspend fun refreshTailcat() = refreshTailcatImpl()
    suspend fun revealTailcatToken() : String = revealTailcatTokenImpl()
    suspend fun setTailcat(body: Map<String, Any?>) : TailcatStatus = setTailcatImpl(body)
    suspend fun fetchTailcatQR() : ByteArray = api.rawBytes("/api/tailcat/qr.png")

    suspend fun refreshHostTailscale() = fetchTailcatQRImpl()
    suspend fun setHostTailscale(body: Map<String, Any?>) : HostTailscaleStatus = setHostTailscaleImpl(body)
    suspend fun fetchHostTailscaleQR() : ByteArray = api.rawBytes("/api/tailscale/qr.png")

    suspend fun refreshMobileTailscale(includePeers: Boolean = true) = fetchHostTailscaleQRImpl()
    suspend fun bindTailscale(authKey: String = "", controlUrl: String = "") = bindTailscaleImpl(authKey, controlUrl)
    suspend fun logoutTailscale() = logoutTailscaleImpl()
    suspend fun refreshDesktopTargets() : List<DesktopTarget> = refreshDesktopTargetsImpl()
    suspend fun setActiveDesktopTarget(target: String) = setActiveDesktopTargetImpl(target)
    suspend fun fetchDesktopState(target: String) : DesktopState = fetchDesktopStateImpl(target)
    suspend fun fetchDesktopPermissions(target: String) : DesktopPermissionsResp = fetchDesktopPermissionsImpl(target)
    suspend fun requestDesktopPermission(
        target: String,
        permission: String,
        openSettings: Boolean = false,
    ) : DesktopPermissionRequestResp = requestDesktopPermissionImpl(target, permission, openSettings)
    suspend fun fetchDesktopFrame(target: String) : DesktopFrame = fetchDesktopFrameImpl(target)
    fun makeDesktopWsUrl(target: String, preset: String? = null, codec: String = "hevc") : String? = makeDesktopWsUrlImpl(target, preset, codec)
    suspend fun sendDesktopInput(body: Map<String, Any?>) = sendDesktopInputImpl(body)
    suspend fun resolveTakeover(id: String) = resolveTakeoverImpl(id)
    suspend fun replyQuestion(id: String, answers: Map<String, Any?>, cancelled: Boolean) = replyQuestionImpl(id, answers, cancelled)
    fun harnessInfo(id: String?) : HarnessInfo? = harnessInfoImpl(id)
    fun supportedPermissionModes(harnessId: String?) : List<String> = supportedPermissionModesImpl(harnessId)
    fun clampPermissionMode(raw: String?, harnessId: String?) : String = clampPermissionModeImpl(raw, harnessId)
    suspend fun replyApproval(id: String, decision: String, optionId: String? = null, reason: String? = null) = replyApprovalImpl(id, decision, optionId, reason)
    suspend fun listWorkspaceFiles(
        botId: String,
        path: String = "",
        showHidden: Boolean = false,
    ): WorkspaceListResult = listWorkspaceFilesImpl(botId, path, showHidden)
    suspend fun readWorkspaceFile(botId: String, path: String): WorkspaceFileContent = readWorkspaceFileImpl(botId, path)
    suspend fun workspaceFileImageURL(botId: String, path: String) : String = workspaceFileImageURLImpl(botId, path)
    suspend fun downloadWorkspaceFileToTemp(botId: String, path: String, fileName: String) : java.io.File = downloadWorkspaceFileToTempImpl(botId, path, fileName)
    fun insertIntoChat(text: String) = insertIntoChatImpl(text)
    fun consumePendingStageOpen() = consumePendingStageOpenImpl()
    suspend fun refreshUpdateStatus() = refreshUpdateStatusImpl()
    suspend fun checkForUpdates() = checkForUpdatesImpl()
    suspend fun saveUpdateConfig(
        autoCheck: Boolean? = null,
        interval: String? = null,
        channel: String? = null,
    ) = saveUpdateConfigImpl(autoCheck, interval, channel)
}
