package ai.pebble.android.data

import ai.pebble.android.i18n.L
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

private fun PebbleStore.applyRotatedAPIToken(newTok: String?) {
    if (newTok.isNullOrEmpty() || newTok == authToken) return
    authToken = newTok
    val id = activeServerId
    if (id != null) {
        val list = servers.toMutableList()
        val idx = list.indexOfFirst { it.id == id }
        if (idx >= 0) {
            list[idx] = list[idx].copy(token = newTok)
            servers = list
            persistServers()
        }
    }
    if (baseURL.isNotEmpty()) {
        startStream(baseURL, newTok)
    }
}

internal suspend fun PebbleStore.refreshTailcatImpl() {
    tailcat = api.call("/api/tailcat")
}

internal suspend fun PebbleStore.revealTailcatTokenImpl(): String =
    api.call<TailcatStatus>("/api/tailcat?reveal=1").apiToken.orEmpty()

internal suspend fun PebbleStore.setTailcatImpl(body: Map<String, Any?>): TailcatStatus {
    val res = api.call<TailcatStatus>("/api/tailcat", method = "POST", body = body)
    tailcat = res
    applyRotatedAPIToken(res.apiToken)
    return res
}

internal suspend fun PebbleStore.fetchTailcatQRImpl(): ByteArray = api.rawBytes("/api/tailcat/qr.png")

internal suspend fun PebbleStore.refreshHostTailscaleImpl() {
    hostTailscale = api.call("/api/tailscale")
}

internal suspend fun PebbleStore.setHostTailscaleImpl(body: Map<String, Any?>): HostTailscaleStatus {
    val res = api.call<HostTailscaleStatus>("/api/tailscale", method = "POST", body = body)
    hostTailscale = res
    applyRotatedAPIToken(res.apiToken)
    return res
}

internal suspend fun PebbleStore.fetchHostTailscaleQRImpl(): ByteArray = api.rawBytes("/api/tailscale/qr.png")

internal suspend fun PebbleStore.refreshMobileTailscaleImpl(includePeers: Boolean = true) {
    val st = PebbleTunnel.tailscaleStatus(includePeers)
    mobileTailscale = st
    if (st.state == "running" && !tsBound) {
        tsBound = true
        prefs.edit().putBoolean("pebble.tsBound", true).apply()
    }
}

internal suspend fun PebbleStore.bindTailscaleImpl(authKey: String = "", controlUrl: String = "") {
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

internal suspend fun PebbleStore.logoutTailscaleImpl() {
    PebbleTunnel.tailscaleLogout()
    tsBound = false
    tsControlUrl = ""
    prefs.edit()
        .putBoolean("pebble.tsBound", false)
        .putString("pebble.tsControlUrl", "")
        .apply()
    mobileTailscale = null
}

internal suspend fun PebbleStore.refreshDesktopTargetsImpl(): List<DesktopTarget> {
    val d = api.call<TargetsResp>("/api/desktop/targets")
    desktopTargets = d.targets.orEmpty()
    d.activeTarget?.let { activeDesktopTarget = it }
    takeovers = d.takeovers.orEmpty()
    return desktopTargets
}

internal suspend fun PebbleStore.setActiveDesktopTargetImpl(target: String) {
    val res = api.call<ActiveTargetResp>("/api/desktop/target", method = "PUT", body = mapOf("target" to target))
    res.activeTarget?.let { activeDesktopTarget = it }
}

internal suspend fun PebbleStore.fetchDesktopStateImpl(target: String): DesktopState {
    val d = api.call<DesktopState>("/api/desktop/state?target=${encodeURIComponent(target)}")
    takeovers = d.takeovers.orEmpty()
    return d
}

internal suspend fun PebbleStore.fetchDesktopPermissionsImpl(target: String): DesktopPermissionsResp =
    api.call("/api/desktop/permissions?target=${encodeURIComponent(target)}")

internal suspend fun PebbleStore.requestDesktopPermissionImpl(
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

internal suspend fun PebbleStore.fetchDesktopFrameImpl(target: String): DesktopFrame =
    api.call("/api/desktop/frame?target=${encodeURIComponent(target)}&windowId=&maxDim=1280&quality=72")

internal fun PebbleStore.makeDesktopWsUrlImpl(target: String, preset: String? = null, codec: String = "hevc"): String? {
    if (baseURL.isBlank()) return null
    val wsBase = when {
        baseURL.startsWith("https://", ignoreCase = true) -> "wss://" + baseURL.substring(8)
        baseURL.startsWith("http://", ignoreCase = true) -> "ws://" + baseURL.substring(7)
        else -> return null
    }
    val resolvedCodec = codec.ifBlank { "hevc" }
    val presetPart = if (!preset.isNullOrBlank()) "&preset=${encodeURIComponent(preset)}" else ""
    return "$wsBase/api/desktop/stream/ws?target=${encodeURIComponent(target)}&codec=${encodeURIComponent(resolvedCodec)}$presetPart"
}

internal suspend fun PebbleStore.sendDesktopInputImpl(body: Map<String, Any?>) {
    api.rawText("/api/desktop/input", method = "POST", body = body)
}

internal suspend fun PebbleStore.resolveTakeoverImpl(id: String) {
    val r = api.call<TakeoversResp>(
        "/api/desktop/takeover/resolve",
        method = "POST",
        body = mapOf("id" to id, "note" to L.t("takeoverResolvedNote")),
    )
    takeovers = r.items.orEmpty()
}

internal suspend fun PebbleStore.replyQuestionImpl(id: String, answers: Map<String, Any?>, cancelled: Boolean) {
    val r = api.call<QuestionsResp>(
        "/api/questions/${encodeURIComponent(id)}/reply",
        method = "POST",
        body = mapOf("answers" to answers, "cancelled" to cancelled),
    )
    questions = r.items.orEmpty()
}

internal fun PebbleStore.harnessInfoImpl(id: String?): HarnessInfo? {
    if (id.isNullOrEmpty()) return null
    return settings?.harnesses?.firstOrNull { it.id == id }
}

internal fun PebbleStore.supportedPermissionModesImpl(harnessId: String?): List<String> =
    harnessInfo(harnessId)?.supportedModes ?: listOf("auto")

internal fun PebbleStore.clampPermissionModeImpl(raw: String?, harnessId: String?): String {
    val modes = supportedPermissionModes(harnessId)
    val cur = raw?.trim().orEmpty()
    if (cur.isNotEmpty() && cur in modes) return cur
    return if ("auto" in modes) "auto" else modes.firstOrNull() ?: "auto"
}

internal suspend fun PebbleStore.replyApprovalImpl(id: String, decision: String, optionId: String? = null, reason: String? = null) {
    val body = mutableMapOf<String, Any?>("decision" to decision)
    if (!optionId.isNullOrEmpty()) body["optionId"] = optionId
    if (!reason.isNullOrEmpty()) body["reason"] = reason
    val r = api.call<ApprovalsResp>(
        "/api/approvals/${encodeURIComponent(id)}/reply",
        method = "POST",
        body = body,
    )
    approvals = r.items.orEmpty()
}

// MARK: Workspace Files & Stage

internal suspend fun PebbleStore.listWorkspaceFilesImpl(
    botId: String,
    path: String = "",
    showHidden: Boolean = false,
): WorkspaceListResult = api.listWorkspaceFiles(botId, path, showHidden)

internal suspend fun PebbleStore.readWorkspaceFileImpl(botId: String, path: String): WorkspaceFileContent =
    api.readWorkspaceFile(botId, path)

internal suspend fun PebbleStore.workspaceFileImageURLImpl(botId: String, path: String): String {
    val tk = api.issueWorkspaceFileTicket(botId, path)
    return api.workspaceFileRawUrl(botId, path, tk.ticket, download = false)
}

internal suspend fun PebbleStore.downloadWorkspaceFileToTempImpl(botId: String, path: String, fileName: String): java.io.File =
    api.downloadWorkspaceFileToTemp(appContext.cacheDir, botId, path, fileName)

internal fun PebbleStore.insertIntoChatImpl(text: String) {
    val nextSeq = (pendingChatInsert?.seq ?: 0) + 1
    pendingChatInsert = PendingChatInsert(seq = nextSeq, text = text)
}

internal fun PebbleStore.consumePendingStageOpenImpl() {
    pendingStageOpen = null
}
