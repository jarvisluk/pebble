package ai.pebble.android.data

import ai.pebble.android.i18n.L
import ai.pebble.android.notify.BotActivityService
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

internal fun PebbleStore.syncBackgroundServiceImpl() {
    val busyNames = bots.filter { it.isBusy }.map { it.displayName }
    if (!inForeground && busyNames.isNotEmpty() && isConnected) {
        BotActivityService.start(appContext, busyNames)
    } else if (busyNames.isEmpty()) {
        BotActivityService.stop(appContext)
    }
}


internal suspend fun PebbleStore.connectProfileImpl(profile: ServerProfile) {
    connSeq += 1
    val seq = connSeq
    stream.stop()
    online = false
    usingStandby = false
    connState = ConnState.CONNECTING
    connError = ""
    serverInfo = null
    unsupportedCaps = emptySet()
    dismissedCompatProto = 0
    updateStatus = null
    updateChecking = false

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

private fun PebbleStore.handleConnectError(error: Throwable) {
    usingStandby = false
    if (error is HubException && error.unauthorized) {
        connState = ConnState.UNAUTHORIZED
        connError = error.message
    } else {
        connState = ConnState.ERROR
        connError = errText(error, L.t("connFailed"))
    }
}

private suspend fun PebbleStore.connectEndpoint(
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
        serverInfo = data.server
        unsupportedCaps = emptySet()
        val list = data.bots.orEmpty()
        bots = list
        msgs.clear(); last.clear(); unread.clear(); tools.clear(); routines.clear()
        resetModelsCache()
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
        scope.launch { runCatching { client.call<ApprovalsResp>("/api/approvals") }.getOrNull()?.items?.let { if (seq == connSeq) approvals = it } }
        scope.launch { runCatching { client.call<UpdateStatus>("/api/update") }.getOrNull()?.let { if (seq == connSeq) updateStatus = it } }
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

internal suspend fun PebbleStore.resumeImpl() {
    val cur = activeServer ?: return
    if (baseURL.isEmpty()) return
    val url = baseURL
    val tok = authToken
    try {
        val d = api.call<BotsResp>("/api/bots")
        serverInfo = d.server
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

internal suspend fun PebbleStore.addAndConnectServerImpl(
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

internal suspend fun PebbleStore.selectServerImpl(id: String) {
    val target = servers.firstOrNull { it.id == id } ?: return
    activeServerId = id
    persistServers()
    connectProfile(target)
}

internal suspend fun PebbleStore.removeServerImpl(id: String) {
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
        takeovers = emptyList(); questions = emptyList(); approvals = emptyList()
        activeTakeoverIds.clear(); activeApprovalIds.clear()
        serverInfo = null; unsupportedCaps = emptySet(); dismissedCompatProto = 0
        updateStatus = null; updateChecking = false
        connState = ConnState.IDLE
        connError = ""
    }
}

internal suspend fun PebbleStore.reconnectImpl() {
    activeServer?.let { connectProfile(it) }
}
