import SwiftUI

struct ChatView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.hubCompact) private var compact
    var botId: String
    var inDesktopSidebar = false
    var onSelectBot: ((String) -> Void)? = nil
    var onCloseSidebar: (() -> Void)? = nil

    @State private var sendErr = ""
    @State private var collabPeer: CollabTarget?
    @State private var previewMedia: MessageMedia?
    @State private var commands: [SlashCommandItem] = []
    @State private var pendingDrop: (seq: Int, urls: [URL])?

    private struct CollabTarget: Identifiable {
        let id: String
    }

    private enum TimelineEntry: Identifiable {
        case message(ChatMessage)
        case collab(id: String, peerId: String, items: [ChatMessage], createdAt: String?)

        var id: String {
            switch self {
            case let .message(m): return m.id
            case let .collab(id, _, _, _): return id
            }
        }
        var createdAt: String? {
            switch self {
            case let .message(m): return m.createdAt
            case let .collab(_, _, _, ts): return ts
            }
        }
    }

    private var timelineEntries: [TimelineEntry] {
        var entries: [TimelineEntry] = []
        for m in list {
            if m.role == "bot_out" || m.role == "bot_in" {
                let rawPeer = (m.role == "bot_out" ? (m.to ?? "") : (m.from ?? ""))
                let peerId = rawPeer.hasPrefix("bot:") ? String(rawPeer.dropFirst(4)) : rawPeer
                if case let .collab(eid, lastPeer, items, ts) = entries.last, lastPeer == peerId {
                    entries[entries.count - 1] = .collab(id: eid, peerId: peerId, items: items + [m], createdAt: ts)
                } else {
                    entries.append(.collab(id: "collab_\(m.id)", peerId: peerId, items: [m], createdAt: m.createdAt))
                }
            } else {
                entries.append(.message(m))
            }
        }
        return entries
    }

    private var index: Int { hub.bots.firstIndex { $0.id == botId } ?? -1 }
    private var bot: BotStatus? { index >= 0 ? hub.bots[index] : nil }
    private var name: String { bot?.displayName ?? botId }
    private var list: [ChatMessage] { hub.msgs[botId] ?? [] }
    private var botApprovals: [PendingApproval] { hub.approvals.filter { $0.botId == botId } }
    private var botQuestions: [PendingQuestion] { hub.questions.filter { $0.botId == botId } }

    var body: some View {
        let pal = p.bot(botId, index, custom: bot?.color)
        let base = VStack(spacing: 0) {
            if inDesktopSidebar {
                HStack(spacing: 8) {
                    if hub.bots.count > 1 {
                        Menu {
                            ForEach(hub.bots) { b in
                                Button {
                                    onSelectBot?(b.id)
                                } label: {
                                    Label(b.displayName, systemImage: b.id == botId ? "checkmark" : "bubble.left")
                                }
                            }
                        } label: {
                            HStack(spacing: 6) {
                                sidebarTitleView
                                Image(systemName: "chevron.down")
                                    .font(.system(size: AppFont.caption, weight: .semibold))
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .buttonStyle(.plain)
                    } else {
                        sidebarTitleView
                    }
                    Spacer()
                    if let onCloseSidebar {
                        Button { onCloseSidebar() } label: {
                            Image(systemName: "xmark")
                                .font(.system(size: AppFont.caption, weight: .semibold))
                        }
                        .buttonStyle(.borderless)
                        .accessibilityLabel(hub.t("close"))
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .overlay(alignment: .bottom) {
                    Rectangle().fill(p.s2).frame(height: 0.5)
                }
            }
            topBanners(pal)
            thread(pal)
            if !inDesktopSidebar {
                BotChatTaskStrip(botId: botId, pal: pal) { task in
                    router.openTask(botId: botId, task: task)
                }
            }
            ChatComposerView(
                botId: botId,
                name: name,
                isBusy: bot?.isBusy ?? false,
                inDesktopSidebar: inDesktopSidebar,
                pal: pal,
                commands: commands,
                pendingDrop: pendingDrop,
                sendErr: $sendErr
            )
            .background(inDesktopSidebar ? Color.clear : p.page)
            .overlay(alignment: .top) {
                if inDesktopSidebar {
                    Rectangle().fill(p.s2).frame(height: 0.5)
                }
            }
        }
            .task(id: botId) {
                if let list = try? await hub.fetchCommands(botId) {
                    commands = list
                }
            }
            .hubDropFileURLs { urls in
                let seq = (pendingDrop?.seq ?? 0) + 1
                pendingDrop = (seq, urls)
            }
            .background {
                if inDesktopSidebar {
                    p.card
                } else {
                    p.page.ignoresSafeArea()
                }
            }
            .sheet(item: $collabPeer) { target in
                NavigationStack {
                    CollabSheetView(botId: botId, peerId: target.id)
                }
            }
            .sheet(item: $previewMedia) { media in
                MediaPreviewSheet(media: media)
            }
            .task(id: botId) {
                hub.setActiveChat(botId)
                try? await hub.loadMessages(botId)
                _ = try? await hub.fetchBotTasks(botId)
            }
            .onDisappear { hub.setActiveChat(nil) }
            .onChange(of: hub.bots.map(\.id)) { _, ids in
                if !inDesktopSidebar, !ids.isEmpty, !ids.contains(botId) {
                    router.closeBot()
                }
            }

        if inDesktopSidebar {
            base
        } else {
            base
                .navigationTitle("")
                .inlineNavTitle()
                .toolbar {
                    ToolbarItem(placement: .principal) {
                        Button {
                            router.sheet = .botInfo(botId)
                        } label: {
                            titleView
                        }
                        .buttonStyle(.plain)
                        .contentShape(Capsule())
                        .help(hub.t("botDetails"))
                        .accessibilityLabel(hub.t("botDetails"))
                    }
                    ToolbarItemGroup(placement: .hubTrailing) {
                        Button {
                            router.openLatestStage(botId: botId, latestStage: bot?.latestStage)
                        } label: {
                            let hasUnread = router.hasUnreadLatestStage(botId)
                            let latestTitle = (bot?.latestStage?.title ?? bot?.latestStage?.path ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                            ZStack(alignment: .topTrailing) {
                                Label(
                                    latestTitle.isEmpty ? hub.t("stageOpenWorkspace") : hub.t("stageLatestTab", ["title": latestTitle]),
                                    systemImage: "macwindow"
                                )
                                .labelStyle(.iconOnly)
                                if hasUnread {
                                    Circle()
                                        .fill(p.busy)
                                        .frame(width: 7, height: 7)
                                        .offset(x: 2, y: -2)
                                }
                            }
                        }
                        .help({
                            let latestTitle = (bot?.latestStage?.title ?? bot?.latestStage?.path ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                            return latestTitle.isEmpty ? hub.t("stageOpenWorkspace") : hub.t("stageLatestTab", ["title": latestTitle])
                        }())
                        Button {
                            let toTarget = hub.takeovers.first(where: { $0.botId == botId })?.target
                                ?? bot?.desktopTarget.flatMap { $0.isEmpty ? nil : $0 }
                                ?? hub.takeovers.first?.target
                                ?? (hub.activeDesktopTarget.target.isEmpty ? "local" : hub.activeDesktopTarget.target)
                            router.openDesktop(toTarget, botId: botId)
                        } label: {
                            Label(hub.t("remoteDesktop"), systemImage: hub.takeovers.isEmpty ? "display" : "exclamationmark.triangle")
                        }
                        Button { router.detailOpen.toggle() } label: {
                            Label(hub.t("rightPanelTitle"), systemImage: "list.bullet")
                        }
                    }
                }
        }
    }

    @ViewBuilder
    private func topBanners(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        let showStandby = !inDesktopSidebar && hub.usingStandby
        let showTemp = !inDesktopSidebar && hub.activeDesktopTarget.temporary
        let takeover = hub.takeovers.first
        if showStandby || showTemp || takeover != nil {
            VStack(spacing: 0) {
                if showStandby {
                    HStack(spacing: 8) {
                        Label(hub.t("standbyActiveBanner"), systemImage: "arrow.triangle.2.circlepath")
                            .font(.system(size: AppFont.body, weight: .medium)).foregroundStyle(p.text).lineLimit(1)
                        Spacer(minLength: 4)
                        Button(hub.t("retryPrimary")) {
                            Task { await hub.reconnect() }
                        }
                        .buttonStyle(.bordered)
                    }
                    .controlSize(.small)
                    .padding(.horizontal, 16).padding(.vertical, 8)
                    .background(p.card)
                }
                if showTemp {
                    HStack(spacing: 8) {
                        Label(
                            hub.t("tempDesktopBanner", ["name": hub.activeDesktopTarget.name ?? hub.activeDesktopTarget.target]),
                            systemImage: "desktopcomputer"
                        )
                        .font(.system(size: AppFont.body, weight: .medium)).foregroundStyle(p.text).lineLimit(1)
                        Spacer(minLength: 4)
                        Button(hub.t("desktopShort")) {
                            router.openDesktop(hub.activeDesktopTarget.target)
                        }
                        .buttonStyle(.bordered)
                        Button(hub.t("switchBackLocal")) {
                            Task { try? await hub.setActiveDesktopTarget("local") }
                        }
                        .buttonStyle(.bordered)
                    }
                    .controlSize(.small)
                    .padding(.horizontal, 16).padding(.vertical, 8)
                    .background(p.card)
                }
                if let to = takeover {
                    HStack(spacing: 8) {
                        Label(hub.t("takeoverInline", ["reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                            .font(.system(size: AppFont.body)).foregroundStyle(p.pinkInk).lineLimit(2)
                        Spacer(minLength: 4)
                        Button(hub.t("handleAction")) { router.openDesktop(to.target ?? "local") }
                            .buttonStyle(.bordered)
                        Button(hub.t("resumeShort")) { Task { try? await hub.resolveTakeover(to.id) } }
                            .buttonStyle(.borderedProminent).tint(pal.accent)
                    }
                    .controlSize(.small)
                    .padding(.horizontal, 16).padding(.vertical, 8)
                    .background(p.card)
                }
                Rectangle().fill(p.s2).frame(height: 1)
            }
        }
    }

    private var titleView: some View {
        HStack(spacing: 8) {
            BotAvatar(id: botId, size: 28, index: index, avatar: bot?.avatar, color: bot?.color, busy: bot?.isBusy ?? false)
            Text(name).font(.system(size: AppFont.cardTitle, weight: .bold)).lineLimit(1)
        }
        .padding(.horizontal, 6)
        .fixedSize(horizontal: true, vertical: false)
    }

    private var sidebarTitleView: some View {
        HStack(spacing: 8) {
            BotAvatar(id: botId, size: 24, index: index, avatar: bot?.avatar, color: bot?.color, busy: bot?.isBusy ?? false)
            Text(name)
                .font(.system(size: AppFont.cardTitle, weight: .semibold))
                .foregroundStyle(p.text)
                .lineLimit(1)
        }
        .fixedSize(horizontal: true, vertical: false)
    }

    @ViewBuilder
    private func thread(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        if list.isEmpty && bot?.isBusy != true && botQuestions.isEmpty && sendErr.isEmpty {
            emptyState(pal)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .padding(.horizontal, 24)
        } else {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 10) {
                        ForEach(Array(timelineEntries.enumerated()), id: \.element.id) { i, entry in
                            let prevAt = i > 0 ? timelineEntries[i - 1].createdAt : nil
                            if showStamp(entry.createdAt, prevAt) {
                                Text(formatStamp(entry.createdAt)).font(.system(size: AppFont.caption)).foregroundStyle(p.mute)
                                    .frame(maxWidth: .infinity).padding(.top, 4)
                            }
                            switch entry {
                            case let .collab(_, peerId, items, _):
                                let peerIdx = hub.bots.firstIndex { $0.id == peerId } ?? -1
                                let peerBot = peerIdx >= 0 ? hub.bots[peerIdx] : nil
                                let peerName = peerBot?.displayName ?? peerId
                                let peerCollab = p.botCollab(peerId, max(0, peerIdx), custom: peerBot?.color)
                                Button {
                                    if !peerId.isEmpty { collabPeer = CollabTarget(id: peerId) }
                                } label: {
                                    HStack(spacing: 8) {
                                        BotAvatar(id: peerId, size: 16, index: peerIdx, avatar: peerBot?.avatar, color: peerBot?.color)
                                        Text("\(hub.t("collabWith", ["peer": peerName])) · \(hub.t("collabCount", ["count": "\(items.count)"]))")
                                            .font(.system(size: AppFont.caption, weight: .semibold))
                                            .foregroundStyle(peerCollab.ink)
                                        Image(systemName: "chevron.right")
                                            .font(.system(size: AppFont.caption, weight: .semibold))
                                            .foregroundStyle(peerCollab.ink.opacity(0.75))
                                    }
                                    .padding(.horizontal, 12).padding(.vertical, 7)
                                    .background(peerCollab.bg, in: Capsule())
                                }
                                .buttonStyle(PressStyle())
                            case let .message(m):
                                MessageBubble(msg: m, soft: pal.soft, botId: botId, inSidebar: inDesktopSidebar, onPreviewMedia: { previewMedia = $0 })
                            }
                        }
                        if bot?.isBusy == true {
                            HStack(spacing: 8) {
                                BotAvatar(id: botId, size: 24, index: index, avatar: bot?.avatar, color: bot?.color, busy: true)
                                Text(hub.t("processing"))
                                    .font(.system(size: AppFont.body)).foregroundStyle(p.busy)
                            }
                            .padding(.vertical, 6)
                        }
                        ForEach(botApprovals) { apr in
                            ApprovalCardView(approval: apr, pal: pal)
                        }
                        ForEach(botQuestions) { q in
                            QuestionCardView(question: q, pal: pal)
                        }
                        if !sendErr.isEmpty { StatusText(text: sendErr) }
                        Color.clear.frame(height: 1).id("bottom")
                    }
                    .frame(maxWidth: 840)
                    .padding(.horizontal, inDesktopSidebar ? 12 : (compact ? 10 : 14))
                    .padding(.vertical, 12)
                    .frame(maxWidth: .infinity)
                }
                .defaultScrollAnchor(.bottom)
                .scrollDismissesKeyboard(.interactively)
                .onChange(of: list.count) { _, _ in withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
                .onChange(of: botApprovals.count) { _, _ in withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
                .onChange(of: botQuestions.count) { _, _ in withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
                .onChange(of: bot?.isBusy) { _, _ in withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
            }
        }
    }

    private func showStamp(_ cur: String?, _ prev: String?) -> Bool {
        guard let prev else { return true }
        guard let a = parseISO(cur), let b = parseISO(prev) else { return false }
        return a.timeIntervalSince(b) > 300
    }

    private func emptyState(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        VStack(spacing: 10) {
            BotAvatar(id: botId, size: 72, index: index, avatar: bot?.avatar, color: bot?.color)
            Text(name).font(.system(size: AppFont.hero, weight: .bold))
            Text((bot?.description ?? "").isEmpty ? hub.t("emptyChatHint") : bot!.description!)
                .font(.system(size: AppFont.secondary)).foregroundStyle(p.text2).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
    }
}
