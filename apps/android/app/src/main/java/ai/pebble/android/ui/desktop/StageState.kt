package ai.pebble.android.ui.desktop

import ai.pebble.android.data.BotTask
import ai.pebble.android.data.StageKind
import ai.pebble.android.data.StageOpenEvent
import ai.pebble.android.data.StageRef
import ai.pebble.android.data.StageTab
import ai.pebble.android.ui.theme.PebbleIcons
import java.util.UUID

data class BotStageState(
    val tabs: List<StageTab> = emptyList(),
    val activeTabId: String = "",
    val unreadLatestStageId: String? = null,
) {
    val activeTab: StageTab?
        get() = tabs.firstOrNull { it.id == activeTabId } ?: tabs.firstOrNull()
}

object StageStateOps {
    fun basename(rawPath: String?): String {
        val clean = rawPath.orEmpty().trim().trim('/')
        if (clean.isEmpty()) return ""
        return clean.substringAfterLast('/')
    }

    fun iconFor(kind: StageKind): Int = when (kind) {
        StageKind.FILES -> PebbleIcons.Folder
        StageKind.FILE -> PebbleIcons.Description
        StageKind.DESKTOP -> PebbleIcons.DesktopWindows
        StageKind.TASK -> PebbleIcons.List
        StageKind.BROWSER -> PebbleIcons.Public
        StageKind.ARTIFACT -> PebbleIcons.Code
        StageKind.MCP_APP -> PebbleIcons.Extension
        StageKind.NEW -> PebbleIcons.Add
    }

    fun newTab(title: String): StageTab = StageTab(
        id = "new:${UUID.randomUUID().toString().take(8)}",
        kind = StageKind.NEW,
        rawKind = StageKind.NEW.raw,
        title = title,
        closable = true,
    )

    fun filesTab(title: String): StageTab = StageTab(
        id = "files",
        kind = StageKind.FILES,
        rawKind = StageKind.FILES.raw,
        title = title,
        closable = true,
    )

    fun fileTab(path: String, title: String? = null): StageTab {
        val clean = path.trim()
        val base = basename(clean)
        val resolvedTitle = title?.trim().orEmpty()
        return StageTab(
            id = "file:$clean",
            kind = StageKind.FILE,
            rawKind = StageKind.FILE.raw,
            title = resolvedTitle.ifEmpty { base.ifEmpty { clean } },
            subtitle = clean.ifEmpty { null },
            path = clean,
            closable = true,
        )
    }

    fun desktopTab(target: String = "local", title: String): StageTab {
        val dest = target.trim().ifEmpty { "local" }
        return StageTab(
            id = if (dest == "local") "desktop" else "desktop:$dest",
            kind = StageKind.DESKTOP,
            rawKind = StageKind.DESKTOP.raw,
            title = title,
            subtitle = if (dest == "local") null else dest,
            target = dest,
            closable = true,
        )
    }

    fun taskTab(task: BotTask): StageTab {
        val sub = listOfNotNull(
            task.kind.takeIf { it.isNotBlank() },
            task.deviceId?.takeIf { it.isNotBlank() && it != "local" },
        ).joinToString(" · ")
        return StageTab(
            id = "task:${task.id}",
            kind = StageKind.TASK,
            rawKind = StageKind.TASK.raw,
            title = task.displayTitle,
            subtitle = sub.ifEmpty { null },
            botId = task.botId,
            taskId = task.id,
            closable = true,
        )
    }

    fun taskTab(taskId: String, title: String?, botId: String?, defaultTitle: String): StageTab {
        val cleanTitle = title?.trim().orEmpty()
        return StageTab(
            id = "task:$taskId",
            kind = StageKind.TASK,
            rawKind = StageKind.TASK.raw,
            title = cleanTitle.ifEmpty { defaultTitle },
            botId = botId,
            taskId = taskId,
            closable = true,
        )
    }

    fun tabFromRef(
        ref: StageRef,
        botId: String? = null,
        filesTitle: String,
        desktopTitle: String,
        newTabTitle: String,
    ): StageTab {
        val kind = ref.resolvedKind
        val rawTitle = ref.title?.trim().orEmpty()
        val rawSub = ref.subtitle?.trim().orEmpty()
        return when (kind) {
            StageKind.FILE -> {
                val cleanPath = ref.path.orEmpty().trim()
                val base = basename(cleanPath)
                StageTab(
                    id = ref.id?.takeIf { it.isNotBlank() } ?: "file:$cleanPath",
                    kind = StageKind.FILE,
                    rawKind = ref.kind,
                    title = rawTitle.ifEmpty { base.ifEmpty { "file" } },
                    subtitle = rawSub.ifEmpty { cleanPath.ifEmpty { null } },
                    botId = botId,
                    path = cleanPath,
                    closable = ref.closable ?: true,
                )
            }
            StageKind.FILES -> StageTab(
                id = ref.id?.takeIf { it.isNotBlank() } ?: "files",
                kind = StageKind.FILES,
                rawKind = ref.kind,
                title = rawTitle.ifEmpty { filesTitle },
                subtitle = rawSub.ifEmpty { null },
                botId = botId,
                closable = ref.closable ?: true,
            )
            StageKind.DESKTOP -> {
                val dest = ref.target?.trim().orEmpty().ifEmpty { "local" }
                StageTab(
                    id = ref.id?.takeIf { it.isNotBlank() } ?: if (dest == "local") "desktop" else "desktop:$dest",
                    kind = StageKind.DESKTOP,
                    rawKind = ref.kind,
                    title = rawTitle.ifEmpty { desktopTitle },
                    subtitle = rawSub.ifEmpty { if (dest == "local") null else dest },
                    botId = botId,
                    target = dest,
                    closable = ref.closable ?: true,
                )
            }
            StageKind.NEW -> newTab(title = rawTitle.ifEmpty { newTabTitle })
            StageKind.TASK -> {
                val rawId = ref.id?.trim().orEmpty()
                val tid = ref.taskId?.trim().orEmpty().ifEmpty {
                    if (rawId.startsWith("task:")) rawId.removePrefix("task:") else rawId
                }
                StageTab(
                    id = rawId.ifEmpty { "task:$tid" },
                    kind = StageKind.TASK,
                    rawKind = ref.kind,
                    title = rawTitle.ifEmpty { tid.ifEmpty { "Task" } },
                    subtitle = rawSub.ifEmpty { null },
                    botId = botId,
                    taskId = tid.ifEmpty { null },
                    closable = ref.closable ?: true,
                )
            }
            StageKind.BROWSER, StageKind.ARTIFACT, StageKind.MCP_APP -> {
                val fallbackSub = ref.url ?: ref.resourceUri
                StageTab(
                    id = ref.resolvedId,
                    kind = kind,
                    rawKind = ref.kind,
                    title = rawTitle.ifEmpty { ref.kind },
                    subtitle = rawSub.ifEmpty { fallbackSub },
                    botId = botId,
                    path = ref.path,
                    url = ref.url,
                    resourceUri = ref.resourceUri,
                    target = ref.target,
                    closable = ref.closable ?: true,
                )
            }
        }
    }

    fun tabFromOpenEvent(
        ev: StageOpenEvent,
        filesTitle: String,
        desktopTitle: String,
        newTabTitle: String,
    ): StageTab {
        val ref = StageRef(
            id = ev.id,
            kind = ev.kind,
            title = ev.title,
            subtitle = ev.subtitle,
            path = ev.path,
            url = ev.url,
            resourceUri = ev.resourceUri,
            target = ev.target,
            taskId = ev.taskId,
            closable = ev.closable,
            createdAt = ev.createdAt,
        )
        return tabFromRef(ref, botId = ev.botId, filesTitle = filesTitle, desktopTitle = desktopTitle, newTabTitle = newTabTitle)
    }

    fun upsert(state: BotStageState, tab: StageTab, activate: Boolean = true): BotStageState {
        val tabs = state.tabs.toMutableList()
        val idx = tabs.indexOfFirst { it.id == tab.id }
        if (idx >= 0) {
            tabs[idx] = tab
        } else {
            tabs.add(tab)
        }
        val activeId = if (activate || state.activeTabId.isEmpty()) tab.id else state.activeTabId
        return state.copy(tabs = tabs, activeTabId = activeId)
    }

    fun replaceNewOrOpen(state: BotStageState, tab: StageTab): BotStageState {
        val active = state.activeTab
        if (active == null || active.kind != StageKind.NEW) {
            return upsert(state, tab, activate = true)
        }
        val tabs = state.tabs.toMutableList()
        val existingIdx = tabs.indexOfFirst { it.id == tab.id }
        if (existingIdx >= 0) {
            tabs[existingIdx] = tab
            tabs.removeAll { it.id == active.id }
            return state.copy(tabs = tabs, activeTabId = tab.id)
        }
        val newIdx = tabs.indexOfFirst { it.id == active.id }
        if (newIdx >= 0) {
            tabs[newIdx] = tab
            return state.copy(tabs = tabs, activeTabId = tab.id)
        }
        return upsert(state, tab, activate = true)
    }

    fun close(state: BotStageState, tabId: String, newTabTitle: String): BotStageState {
        val idx = state.tabs.indexOfFirst { it.id == tabId }
        if (idx < 0) return state
        val tabs = state.tabs.toMutableList()
        tabs.removeAt(idx)
        if (tabs.isEmpty()) {
            val fresh = newTab(title = newTabTitle)
            return state.copy(tabs = listOf(fresh), activeTabId = fresh.id)
        }
        val nextActive = if (state.activeTabId == tabId) {
            tabs[minOf(idx, tabs.size - 1)].id
        } else {
            state.activeTabId
        }
        return state.copy(tabs = tabs, activeTabId = nextActive)
    }

    fun ensureNonEmpty(state: BotStageState, newTabTitle: String): BotStageState {
        if (state.tabs.isEmpty()) {
            val fresh = newTab(title = newTabTitle)
            return state.copy(tabs = listOf(fresh), activeTabId = fresh.id)
        }
        if (state.tabs.none { it.id == state.activeTabId }) {
            return state.copy(activeTabId = state.tabs.first().id)
        }
        return state
    }
}
