package ai.pebble.android.data

import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// MARK: Settings & Magpie

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

internal suspend fun PebbleStore.loadMagpieModelsImpl(harness: String): List<String> {
    if (harness.isEmpty()) return emptyList()
    return api.call<ModelsResp>("/api/models?harness=${encodeURIComponent(harness)}&source=magpie").models.orEmpty()
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
    defaultModels: Map<String, String>,
    magpieWebUrl: String?,
    botUpdates: List<Triple<String, String, String>>,
) {
    if (changed) {
        val body = mutableMapOf<String, Any?>(
            "defaultHarness" to defaultHarness,
            "defaultModel" to defaultModel,
            "defaultModels" to defaultModels,
        )
        if (!defaultPermissionMode.isNullOrEmpty()) {
            body["defaultPermissionMode"] = defaultPermissionMode
        }
        if (magpieWebUrl != null) {
            body["magpieWebUrl"] = magpieWebUrl
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

internal suspend fun PebbleStore.connectMagpieImpl(harness: String, model: String) {
    settings = api.call(
        "/api/harnesses/${encodeURIComponent(harness)}/magpie",
        method = "POST",
        body = mapOf("action" to "connect", "model" to model),
    )
    resetModelsCache()
}

internal suspend fun PebbleStore.disconnectMagpieImpl(harness: String) {
    settings = api.call(
        "/api/harnesses/${encodeURIComponent(harness)}/magpie",
        method = "POST",
        body = mapOf("action" to "disconnect"),
    )
    resetModelsCache()
}

internal suspend fun PebbleStore.openMagpieImpl() {
    api.call<OkResp>("/api/magpie/open", method = "POST", body = emptyMap())
}

internal suspend fun PebbleStore.installMagpieImpl() {
    var st = api.call<SetupTaskStatus>("/api/magpie/install", method = "POST", body = emptyMap())
    while (st.state == "running") {
        delay(1200)
        st = api.call("/api/magpie/install")
    }
    if (st.state == "error") {
        throw HubException(st.error ?: "Magpie install failed")
    }
    loadSettings()
    resetModelsCache()
}

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
