package ai.pebble.android.data

import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// MARK: Settings

internal suspend fun PebbleStore.loadSettingsImpl(fresh: Boolean = false): SettingsData {
    val s = api.call<SettingsData>(if (fresh) "/api/settings?fresh=1" else "/api/settings")
    settings = s
    return s
}

internal suspend fun PebbleStore.loadModelsImpl(harness: String, force: Boolean = false): List<String> {
    if (harness.isEmpty()) return emptyList()
    val cached = models[harness]
    if (!force && !cached.isNullOrEmpty()) return cached
    if (force) {
        modelLoadJobs.remove(harness)?.cancel()
    } else {
        modelLoadJobs[harness]?.let { return it.await() }
    }
    val job = scope.async {
        api.call<ModelsResp>("/api/models?harness=${encodeURIComponent(harness)}").models.orEmpty()
    }
    modelLoadJobs[harness] = job
    return try {
        val list = job.await()
        if (list.isNotEmpty() || force || models[harness] == null) {
            models[harness] = list
        }
        models[harness].orEmpty()
    } finally {
        if (modelLoadJobs[harness] === job) {
            modelLoadJobs.remove(harness)
        }
    }
}

internal fun PebbleStore.resetModelsCacheImpl() {
    modelLoadJobs.values.forEach { it.cancel() }
    modelLoadJobs.clear()
    models.clear()
    modelsGeneration += 1
}

internal suspend fun PebbleStore.saveModelsSettingsImpl(
    changed: Boolean,
    defaultHarness: String,
    defaultPermissionMode: String? = null,
    defaultModel: String,
    defaultModelsPatch: Map<String, String>? = null,
    botUpdates: List<Triple<String, String, String>>,
) {
    if (changed) {
        val body = mutableMapOf<String, Any?>(
            "defaultModel" to defaultModel,
        )
        if (defaultHarness.isNotEmpty()) {
            body["defaultHarness"] = defaultHarness
        }
        if (!defaultModelsPatch.isNullOrEmpty()) {
            body["defaultModelsPatch"] = defaultModelsPatch
        }
        if (!defaultPermissionMode.isNullOrEmpty()) {
            body["defaultPermissionMode"] = defaultPermissionMode
        }
        settings = api.call("/api/settings", method = "PUT", body = body)
    }
    for ((bid, h, m) in botUpdates) {
        updateBot(bid, mapOf("harness" to h, "model" to m))
    }
    loadSettings()
    refreshBots()
}

internal suspend fun PebbleStore.saveSharedUserMdImpl(content: String) {
    settings = api.call("/api/settings", method = "PUT", body = mapOf("sharedUserMd" to content))
}

internal suspend fun PebbleStore.startHarnessInstallImpl(harness: String): SetupTaskStatus =
    api.call("/api/harnesses/${encodeURIComponent(harness)}/install", method = "POST", body = emptyMap())

internal suspend fun PebbleStore.getHarnessInstallStatusImpl(harness: String): SetupTaskStatus =
    api.call("/api/harnesses/${encodeURIComponent(harness)}/install")

internal suspend fun PebbleStore.startHarnessLoginImpl(harness: String): SetupTaskStatus =
    api.call("/api/harnesses/${encodeURIComponent(harness)}/login", method = "POST", body = emptyMap())

internal suspend fun PebbleStore.getHarnessLoginStatusImpl(harness: String): SetupTaskStatus =
    api.call("/api/harnesses/${encodeURIComponent(harness)}/login")

internal suspend fun PebbleStore.setDesktopWorkerImpl(harness: String, model: String) {
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

internal suspend fun PebbleStore.refreshNodesImpl() {
    if (baseURL.isEmpty()) return
    nodes = api.call<NodesResp>("/api/nodes").nodes.orEmpty()
}

// MARK: Software Update

internal fun isSemVerNewerImpl(candidate: String, current: String): Boolean {
    fun parse(raw: String): Triple<IntArray, List<String>, Boolean>? {
        var s = raw.trim()
        if (s.startsWith("v", ignoreCase = true)) s = s.substring(1)
        if (s.isEmpty()) return null
        val plusIdx = s.indexOf('+')
        if (plusIdx >= 0) s = s.substring(0, plusIdx)
        val dashIdx = s.indexOf('-')
        val core: String
        val pre: List<String>
        if (dashIdx >= 0) {
            core = s.substring(0, dashIdx)
            val p = s.substring(dashIdx + 1)
            if (p.isEmpty()) return null
            pre = p.split('.')
            if (pre.any { it.isEmpty() }) return null
        } else {
            core = s
            pre = emptyList()
        }
        val nums = core.split('.')
        if (nums.size != 3) return null
        val maj = nums[0].toIntOrNull() ?: return null
        val min = nums[1].toIntOrNull() ?: return null
        val pat = nums[2].toIntOrNull() ?: return null
        if (maj < 0 || min < 0 || pat < 0) return null
        return Triple(intArrayOf(maj, min, pat), pre, true)
    }
    val a = parse(candidate) ?: return false
    val b = parse(current) ?: return false
    for (i in 0..2) {
        if (a.first[i] != b.first[i]) return a.first[i] > b.first[i]
    }
    val ap = a.second
    val bp = b.second
    if (ap.isEmpty() && bp.isEmpty()) return false
    if (ap.isEmpty() != bp.isEmpty()) return ap.isEmpty()
    val limit = minOf(ap.size, bp.size)
    for (i in 0 until limit) {
        val ai = ap[i]
        val bi = bp[i]
        if (ai == bi) continue
        val an = ai.toIntOrNull()
        val bn = bi.toIntOrNull()
        return when {
            an != null && bn != null -> an > bn
            an != null -> false
            bn != null -> true
            else -> ai > bi
        }
    }
    return ap.size > bp.size
}

internal suspend fun PebbleStore.refreshUpdateStatusImpl() {
    if (baseURL.isEmpty() || !supports("update.check")) return
    try {
        updateStatus = api.call<UpdateStatus>("/api/update")
    } catch (err: HubException) {
        if (err.isUnsupportedCapability) markUnsupported("update.check")
    } catch (_: Throwable) {}
}

internal suspend fun PebbleStore.checkForUpdatesImpl() {
    if (baseURL.isEmpty()) return
    updateChecking = true
    try {
        updateStatus = api.call<UpdateStatus>("/api/update/check", method = "POST", body = emptyMap<String, Any?>())
    } catch (err: HubException) {
        if (err.isUnsupportedCapability) markUnsupported("update.check")
    } catch (_: Throwable) {
    } finally {
        updateChecking = false
    }
}

internal suspend fun PebbleStore.saveUpdateConfigImpl(
    autoCheck: Boolean? = null,
    interval: String? = null,
    channel: String? = null,
) {
    if (baseURL.isEmpty()) return
    val body = mutableMapOf<String, Any?>()
    if (autoCheck != null) body["autoCheck"] = autoCheck
    if (interval != null) body["interval"] = interval
    if (channel != null) body["channel"] = channel
    if (body.isEmpty()) return
    try {
        updateStatus = api.call<UpdateStatus>("/api/update/config", method = "PATCH", body = body)
    } catch (err: HubException) {
        if (err.isUnsupportedCapability) markUnsupported("update.check")
    } catch (_: Throwable) {}
}
