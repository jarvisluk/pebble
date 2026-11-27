import Foundation

struct BotStageState: Equatable {
    var tabs: [StageTab] = []
    var activeTabId: String = ""
    var unreadLatestStageId: String? = nil

    var activeTab: StageTab? {
        tabs.first(where: { $0.id == activeTabId }) ?? tabs.first
    }
}

enum StageStateOps {
    static func basename(of rawPath: String?) -> String {
        let clean = (rawPath ?? "").trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        guard !clean.isEmpty else { return "" }
        return (clean as NSString).lastPathComponent
    }

    static func symbolName(for kind: StageKind, rawKind: String = "") -> String {
        switch kind {
        case .files:
            return "folder"
        case .file:
            return "doc.text"
        case .desktop:
            return "display"
        case .task:
            return "terminal"
        case .browser:
            return "globe"
        case .artifact:
            return "chevron.left.forwardslash.chevron.right"
        case .mcpApp:
            return "square.grid.2x2"
        case .new:
            return "plus.square"
        }
    }

    static func newTab(title: String) -> StageTab {
        StageTab(
            id: "new:\(UUID().uuidString.prefix(8))",
            kind: .new,
            rawKind: StageKind.new.rawValue,
            title: title,
            closable: true
        )
    }

    static func filesTab(title: String) -> StageTab {
        StageTab(
            id: "files",
            kind: .files,
            rawKind: StageKind.files.rawValue,
            title: title,
            closable: true
        )
    }

    static func fileTab(path: String, title: String? = nil) -> StageTab {
        let clean = path.trimmingCharacters(in: .whitespacesAndNewlines)
        let base = basename(of: clean)
        let resolvedTitle = (title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return StageTab(
            id: "file:\(clean)",
            kind: .file,
            rawKind: StageKind.file.rawValue,
            title: resolvedTitle.isEmpty ? (base.isEmpty ? clean : base) : resolvedTitle,
            subtitle: clean.isEmpty ? nil : clean,
            path: clean,
            closable: true
        )
    }

    static func desktopTab(target: String = "local", title: String) -> StageTab {
        let resolved = target.trimmingCharacters(in: .whitespacesAndNewlines)
        let dest = resolved.isEmpty ? "local" : resolved
        return StageTab(
            id: dest == "local" ? "desktop" : "desktop:\(dest)",
            kind: .desktop,
            rawKind: StageKind.desktop.rawValue,
            title: title,
            subtitle: dest == "local" ? nil : dest,
            target: dest,
            closable: true
        )
    }

    static func taskTab(task: BotTask) -> StageTab {
        let sub = [task.kind, (task.deviceId ?? "").isEmpty || task.deviceId == "local" ? nil : task.deviceId]
            .compactMap { $0 }
            .joined(separator: " · ")
        return StageTab(
            id: "task:\(task.id)",
            kind: .task,
            rawKind: StageKind.task.rawValue,
            title: task.displayTitle,
            subtitle: sub.isEmpty ? nil : sub,
            botId: task.botId,
            taskId: task.id,
            closable: true
        )
    }

    static func taskTab(taskId: String, title: String?, botId: String?, defaultTitle: String) -> StageTab {
        let cleanTitle = (title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return StageTab(
            id: "task:\(taskId)",
            kind: .task,
            rawKind: StageKind.task.rawValue,
            title: cleanTitle.isEmpty ? defaultTitle : cleanTitle,
            botId: botId,
            taskId: taskId,
            closable: true
        )
    }

    static func tab(from ref: StageRef, botId: String? = nil, filesTitle: String, desktopTitle: String, newTabTitle: String) -> StageTab {
        let kind = ref.resolvedKind
        switch kind {
        case .file:
            let cleanPath = (ref.path ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let base = basename(of: cleanPath)
            let rawTitle = (ref.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let rawSub = (ref.subtitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return StageTab(
                id: (ref.id ?? "").isEmpty ? "file:\(cleanPath)" : ref.id!,
                kind: .file,
                rawKind: ref.kind,
                title: rawTitle.isEmpty ? (base.isEmpty ? "file" : base) : rawTitle,
                subtitle: rawSub.isEmpty ? (cleanPath.isEmpty ? nil : cleanPath) : rawSub,
                botId: botId,
                path: cleanPath,
                closable: ref.closable ?? true
            )
        case .files:
            let rawTitle = (ref.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let rawSub = (ref.subtitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return StageTab(
                id: (ref.id ?? "").isEmpty ? "files" : ref.id!,
                kind: .files,
                rawKind: ref.kind,
                title: rawTitle.isEmpty ? filesTitle : rawTitle,
                subtitle: rawSub.isEmpty ? nil : rawSub,
                botId: botId,
                closable: ref.closable ?? true
            )
        case .desktop:
            let target = (ref.target ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let dest = target.isEmpty ? "local" : target
            let rawTitle = (ref.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let rawSub = (ref.subtitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return StageTab(
                id: (ref.id ?? "").isEmpty ? (dest == "local" ? "desktop" : "desktop:\(dest)") : ref.id!,
                kind: .desktop,
                rawKind: ref.kind,
                title: rawTitle.isEmpty ? desktopTitle : rawTitle,
                subtitle: rawSub.isEmpty ? (dest == "local" ? nil : dest) : rawSub,
                botId: botId,
                target: dest,
                closable: ref.closable ?? true
            )
        case .new:
            let rawTitle = (ref.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return newTab(title: rawTitle.isEmpty ? newTabTitle : rawTitle)
        case .task:
            let rawId = (ref.id ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let resolvedTaskId: String = {
                if let tid = ref.taskId?.trimmingCharacters(in: .whitespacesAndNewlines), !tid.isEmpty { return tid }
                if rawId.hasPrefix("task:") { return String(rawId.dropFirst(5)) }
                return rawId
            }()
            let rawTitle = (ref.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let rawSub = (ref.subtitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return StageTab(
                id: rawId.isEmpty ? "task:\(resolvedTaskId)" : rawId,
                kind: .task,
                rawKind: ref.kind,
                title: rawTitle.isEmpty ? (resolvedTaskId.isEmpty ? "Task" : resolvedTaskId) : rawTitle,
                subtitle: rawSub.isEmpty ? nil : rawSub,
                botId: botId,
                taskId: resolvedTaskId.isEmpty ? nil : resolvedTaskId,
                closable: ref.closable ?? true
            )
        case .browser, .artifact, .mcpApp:
            let rawTitle = (ref.title ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let rawSub = (ref.subtitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            let fallbackSub = ref.url ?? ref.resourceUri
            return StageTab(
                id: ref.resolvedId,
                kind: kind,
                rawKind: ref.kind,
                title: rawTitle.isEmpty ? ref.kind : rawTitle,
                subtitle: rawSub.isEmpty ? fallbackSub : rawSub,
                botId: botId,
                path: ref.path,
                url: ref.url,
                resourceUri: ref.resourceUri,
                target: ref.target,
                closable: ref.closable ?? true
            )
        }
    }

    static func tab(from ev: StageOpenEvent, filesTitle: String, desktopTitle: String, newTabTitle: String) -> StageTab {
        let ref = StageRef(
            id: ev.id,
            kind: ev.kind,
            title: ev.title,
            subtitle: ev.subtitle,
            path: ev.path,
            url: ev.url,
            resourceUri: ev.resourceUri,
            target: ev.target,
            taskId: ev.taskId,
            closable: ev.closable,
            createdAt: ev.createdAt
        )
        return tab(from: ref, botId: ev.botId, filesTitle: filesTitle, desktopTitle: desktopTitle, newTabTitle: newTabTitle)
    }

    static func upsert(_ state: BotStageState, tab: StageTab, activate: Bool = true) -> BotStageState {
        var next = state
        if let idx = next.tabs.firstIndex(where: { $0.id == tab.id }) {
            next.tabs[idx] = tab
        } else {
            next.tabs.append(tab)
        }
        if activate || next.activeTabId.isEmpty {
            next.activeTabId = tab.id
        }
        return next
    }

    static func replaceNewOrOpen(_ state: BotStageState, tab: StageTab) -> BotStageState {
        guard let active = state.activeTab, active.kind == .new else {
            return upsert(state, tab: tab, activate: true)
        }
        var next = state
        if let existingIdx = next.tabs.firstIndex(where: { $0.id == tab.id }) {
            next.tabs[existingIdx] = tab
            next.tabs.removeAll { $0.id == active.id }
            next.activeTabId = tab.id
            return next
        }
        if let newIdx = next.tabs.firstIndex(where: { $0.id == active.id }) {
            next.tabs[newIdx] = tab
            next.activeTabId = tab.id
            return next
        }
        return upsert(state, tab: tab, activate: true)
    }

    static func close(_ state: BotStageState, tabId: String, newTabTitle: String) -> BotStageState {
        guard let idx = state.tabs.firstIndex(where: { $0.id == tabId }) else { return state }
        var next = state
        next.tabs.remove(at: idx)
        if next.tabs.isEmpty {
            let fresh = newTab(title: newTabTitle)
            next.tabs = [fresh]
            next.activeTabId = fresh.id
            return next
        }
        if next.activeTabId == tabId {
            let pick = min(idx, next.tabs.count - 1)
            next.activeTabId = next.tabs[pick].id
        }
        return next
    }

    static func ensureNonEmpty(_ state: BotStageState, newTabTitle: String) -> BotStageState {
        if state.tabs.isEmpty {
            var next = state
            let fresh = newTab(title: newTabTitle)
            next.tabs = [fresh]
            next.activeTabId = fresh.id
            return next
        }
        if !state.tabs.contains(where: { $0.id == state.activeTabId }) {
            var next = state
            next.activeTabId = state.tabs[0].id
            return next
        }
        return state
    }
}
