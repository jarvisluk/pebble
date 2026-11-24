import SwiftUI
#if os(iOS)
import PhotosUI
#endif
import UniformTypeIdentifiers

struct ConfirmItem: Identifiable {
    let id = UUID()
    var title: String
    var message: String
    var destructive = true
    var action: () async -> Void
}

extension View {
    func confirmAlert(_ item: Binding<ConfirmItem?>) -> some View {
        alert(item.wrappedValue?.title ?? "", isPresented: Binding(get: { item.wrappedValue != nil }, set: { if !$0 { item.wrappedValue = nil } }),
              presenting: item.wrappedValue) { c in
            Button(L.t("cancel"), role: .cancel) {}
            Button(L.t("confirm"), role: c.destructive ? .destructive : nil) { Task { await c.action() } }
        } message: { c in Text(c.message) }
    }
}

struct ChatView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @Environment(\.hubCompact) private var compact
    var botId: String
    var inDesktopSidebar = false
    var onSelectBot: ((String) -> Void)? = nil
    var onCloseSidebar: (() -> Void)? = nil

    @State private var input = ""
    @State private var sending = false
    @State private var sendErr = ""
    @State private var collabPeer: CollabTarget?
    @State private var previewMedia: MessageMedia?
    @State private var commands: [SlashCommandItem] = []
    @State private var mentions: [MentionCandidate] = []
    @State private var mentionTask: Task<Void, Never>?
    @State private var acDismissed = false
    @State private var pendingAttachments: [PendingAttachment] = []
    @State private var showPhotoPicker = false
    #if os(iOS)
    @State private var selectedPhotoItems: [PhotosPickerItem] = []
    #endif
    @State private var showFileImporter = false
    @FocusState private var focused: Bool

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
    private var botQuestions: [PendingQuestion] { hub.questions.filter { $0.botId == botId } }
    private var showStop: Bool { (bot?.isBusy ?? false) && input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

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
            composer(pal)
                .background(inDesktopSidebar ? Color.clear : p.page)
                .overlay(alignment: .top) {
                    if inDesktopSidebar {
                        Rectangle().fill(p.s2).frame(height: 0.5)
                    }
                }
        }
            .task(id: botId) {
                pendingAttachments = []
                mentions = []
                acDismissed = false
                if let list = try? await hub.fetchCommands(botId) {
                    commands = list
                }
            }
            #if os(iOS)
            .photosPicker(isPresented: $showPhotoPicker, selection: $selectedPhotoItems, maxSelectionCount: 5, matching: .images)
            .onChange(of: selectedPhotoItems) { _, items in
                guard !items.isEmpty else { return }
                let picked = items
                selectedPhotoItems = []
                for (idx, item) in picked.enumerated() {
                    Task {
                        if let data = try? await item.loadTransferable(type: Data.self) {
                            await uploadData(data: data, filename: "photo_\(Int(Date().timeIntervalSince1970))_\(idx).jpg", mimeType: "image/jpeg", isImage: true)
                        }
                    }
                }
            }
            #endif
            .fileImporter(isPresented: $showFileImporter, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
                guard case .success(let urls) = result else { return }
                importFileURLs(urls)
            }
            .hubDropFileURLs { importFileURLs($0) }
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
            }
            .onChange(of: hub.pendingChatInsert?.seq) { _, seq in
                guard seq != nil, let insert = hub.pendingChatInsert, !insert.text.isEmpty else { return }
                let needsSpace = !input.isEmpty && !(input.last?.isWhitespace ?? false)
                input += (needsSpace ? " " : "") + insert.text
                focused = true
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
                .macSeamlessWindowToolbar()
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
                        Button { router.openFiles(botId: botId) } label: {
                            Label(hub.t("workspaceFiles"), systemImage: "folder")
                        }
                        Button { router.openDesktop(hub.takeovers.first?.target ?? "local") } label: {
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

    private var slashQuery: String? {
        guard !acDismissed, input.hasPrefix("/") else { return nil }
        let rest = String(input.dropFirst())
        if rest.contains(where: { $0.isWhitespace }) { return nil }
        return rest
    }

    private var filteredCommands: [SlashCommandItem] {
        guard let q = slashQuery?.lowercased() else { return [] }
        if q.isEmpty { return commands }
        return commands.filter {
            $0.name.lowercased().contains(q) || ($0.description?.lowercased().contains(q) ?? false)
        }
    }

    private var activeMentionRange: (range: Range<String.Index>, query: String)? {
        guard !acDismissed, slashQuery == nil else { return nil }
        guard let atIdx = input.lastIndex(of: "@") else { return nil }
        if atIdx != input.startIndex {
            let prev = input[input.index(before: atIdx)]
            if !prev.isWhitespace { return nil }
        }
        let after = input[input.index(after: atIdx)...]
        if after.contains(where: { $0.isWhitespace }) { return nil }
        return (atIdx..<input.endIndex, String(after))
    }

    private func composer(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        let readyMediaIds = pendingAttachments.compactMap { (!$0.uploading && $0.error == nil) ? $0.media?.id : nil }
        let hasUploading = pendingAttachments.contains { $0.uploading }
        let hasText = !input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        let hasContent = hasText || !readyMediaIds.isEmpty
        let showSlashAC = slashQuery != nil && !filteredCommands.isEmpty
        let showMentionAC = activeMentionRange != nil && !mentions.isEmpty

        return VStack(spacing: 8) {
            if showSlashAC || showMentionAC {
                ScrollView {
                    LazyVStack(spacing: 2) {
                        if showSlashAC {
                            ForEach(filteredCommands.prefix(12)) { cmd in
                                Button {
                                    let immediate: Set<String> = ["status", "bots", "reset", "compact", "clear", "cost", "context"]
                                    acDismissed = true
                                    input = (immediate.contains(cmd.name) && (cmd.argumentHint ?? "").isEmpty) ? "/\(cmd.name)" : "/\(cmd.name) "
                                    focused = true
                                } label: {
                                    HStack(spacing: 10) {
                                        VStack(alignment: .leading, spacing: 2) {
                                            Text("/\(cmd.name)" + ((cmd.argumentHint ?? "").isEmpty ? "" : " \(cmd.argumentHint!)"))
                                                .font(.system(size: AppFont.body, weight: .medium, design: .monospaced))
                                                .foregroundStyle(p.text)
                                            if let d = cmd.description, !d.isEmpty {
                                                Text(d).font(.system(size: AppFont.caption)).foregroundStyle(p.mute).lineLimit(1)
                                            }
                                        }
                                        Spacer()
                                        Text(sourceLabel(cmd.source))
                                            .font(.system(size: AppFont.caption, weight: .semibold))
                                            .foregroundStyle(p.text2)
                                            .padding(.horizontal, 8).padding(.vertical, 3)
                                            .background(p.s1, in: Capsule())
                                    }
                                    .padding(.horizontal, 12).padding(.vertical, 8)
                                }
                                .buttonStyle(.plain)
                            }
                        } else if let mRange = activeMentionRange {
                            ForEach(mentions.prefix(12)) { item in
                                Button {
                                    acDismissed = true
                                    input.replaceSubrange(mRange.range, with: "@\(item.value) ")
                                    focused = true
                                } label: {
                                    HStack(spacing: 10) {
                                        VStack(alignment: .leading, spacing: 2) {
                                            Text(item.kind == "file" ? item.value : "@\(item.value)")
                                                .font(.system(size: AppFont.body, weight: .medium, design: .monospaced))
                                                .foregroundStyle(p.text)
                                                .lineLimit(1)
                                            if let d = item.description, !d.isEmpty, d != item.value {
                                                Text(d).font(.system(size: AppFont.caption)).foregroundStyle(p.mute).lineLimit(1)
                                            }
                                        }
                                        Spacer()
                                        Text(mentionLabel(item.kind))
                                            .font(.system(size: AppFont.caption, weight: .semibold))
                                            .foregroundStyle(p.text2)
                                            .padding(.horizontal, 8).padding(.vertical, 3)
                                            .background(p.s1, in: Capsule())
                                    }
                                    .padding(.horizontal, 12).padding(.vertical, 8)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                    .padding(6)
                }
                .frame(maxHeight: 220)
                .background(p.card, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
                .shadow(color: .black.opacity(0.12), radius: 12, y: 4)
            }

            if !pendingAttachments.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(pendingAttachments) { att in
                            ZStack(alignment: .topTrailing) {
                                Group {
                                    if att.isImage, let data = att.imageData, let img = PlatformImage(data: data) {
                                        Image(platform: img)
                                            .resizable()
                                            .scaledToFill()
                                            .frame(width: 52, height: 52)
                                            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
                                    } else {
                                        HStack(spacing: 6) {
                                            Image(systemName: "doc")
                                                .font(.system(size: AppFont.secondary))
                                                .foregroundStyle(p.text2)
                                            Text(att.name)
                                                .font(.system(size: AppFont.secondary))
                                                .foregroundStyle(p.text)
                                                .lineLimit(1)
                                            if att.uploading {
                                                ProgressView().controlSize(.mini)
                                            }
                                        }
                                        .padding(.horizontal, 10).padding(.vertical, 8)
                                        .frame(maxWidth: 180)
                                        .background(p.card, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                                    }
                                }
                                .overlay(
                                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                                        .stroke(att.error != nil ? p.danger : p.s2, lineWidth: 1)
                                )
                                Button {
                                    pendingAttachments.removeAll { $0.id == att.id }
                                } label: {
                                    Image(systemName: "xmark.circle.fill")
                                        .font(.system(size: AppFont.cardTitle))
                                        .foregroundStyle(p.text, p.card)
                                }
                                .offset(x: 5, y: -5)
                            }
                            .padding(.top, 5)
                            .padding(.trailing, 5)
                        }
                    }
                }
            }

            composerInputBar(pal)
        }
        .frame(maxWidth: 840)
        .padding(.horizontal, inDesktopSidebar ? 12 : (compact ? 10 : 14))
        .padding(.top, inDesktopSidebar ? 8 : 6)
        .padding(.bottom, inDesktopSidebar ? 10 : 8)
        .frame(maxWidth: .infinity)
    }

    @ViewBuilder
    private func composerInputBar(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        let hasContent = !input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !pendingAttachments.isEmpty
        let hasUploading = pendingAttachments.contains { $0.uploading }
        let row = HStack(alignment: .bottom, spacing: 10) {
            #if os(iOS)
            Menu {
                Button { showPhotoPicker = true } label: { Label(hub.t("photoLibrary"), systemImage: "photo") }
                Button { showFileImporter = true } label: { Label(hub.t("chooseFile"), systemImage: "doc") }
            } label: {
                Image(systemName: "plus")
                    .font(.system(size: AppFont.cardTitle, weight: .semibold))
                    .frame(width: 28, height: 28)
            }
            .composerGlassButton(onCard: inDesktopSidebar)
            .accessibilityLabel(hub.t("attachments"))
            #else
            Button { showFileImporter = true } label: {
                Image(systemName: "plus")
                    .font(.system(size: AppFont.cardTitle, weight: .semibold))
                    .frame(width: 28, height: 28)
            }
            .composerGlassButton(onCard: inDesktopSidebar)
            .help(hub.t("macChooseFileOrImage"))
            .accessibilityLabel(hub.t("attachments"))
            #endif

            TextField(hub.t("msgBotPh", ["name": name]), text: $input, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.body))
                .lineLimit(1...6)
                .focused($focused)
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .composerGlassField(onCard: inDesktopSidebar)
                .hubSubmitOnReturn {
                    if showStop {
                        interrupt()
                    } else {
                        submit()
                    }
                }
                .onChange(of: input) { _, v in
                    acDismissed = false
                    if v.count > 8000 { input = String(v.prefix(8000)) }
                    mentionTask?.cancel()
                    if let m = activeMentionRange {
                        let q = m.query
                        mentionTask = Task {
                            try? await Task.sleep(nanoseconds: 120_000_000)
                            guard !Task.isCancelled else { return }
                            if let items = try? await hub.fetchMentions(botId, query: q) {
                                await MainActor.run { mentions = items }
                            }
                        }
                    } else {
                        mentions = []
                    }
                }

            Button {
                showStop ? interrupt() : submit()
            } label: {
                Image(systemName: showStop ? "stop.fill" : "arrow.up")
                    .font(.system(size: showStop ? AppFont.secondary : AppFont.cardTitle, weight: .bold))
                    .frame(width: 28, height: 28)
            }
            .composerSendButton(accent: showStop ? p.danger : pal.accent)
            .disabled(!showStop && (!hasContent || hasUploading || sending))
            .keyboardShortcut(.return, modifiers: .command)
            .accessibilityLabel(showStop ? hub.t("stopOutput") : hub.t("sendMsg"))
        }

        row.glassRowContainer(spacing: 10)
    }

    private func sourceLabel(_ source: String?) -> String {
        switch source {
        case "hub": return hub.t("cmdSourceHub")
        case "skill": return hub.t("cmdSourceSkill")
        case "workflow": return hub.t("cmdSourceWorkflow")
        default: return hub.t("cmdSourceHarness")
        }
    }

    private func mentionLabel(_ kind: String) -> String {
        switch kind {
        case "bot": return hub.t("mentionBot")
        case "agent": return hub.t("mentionAgent")
        default: return hub.t("mentionFile")
        }
    }

    private func importFileURLs(_ urls: [URL]) {
        for url in urls {
            let accessed = url.startAccessingSecurityScopedResource()
            defer { if accessed { url.stopAccessingSecurityScopedResource() } }
            if let data = try? Data(contentsOf: url) {
                let name = url.lastPathComponent
                let mime = UTType(filenameExtension: url.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
                let isImg = mime.hasPrefix("image/")
                Task {
                    await uploadData(data: data, filename: name, mimeType: mime, isImage: isImg)
                }
            }
        }
    }

    private func uploadData(data: Data, filename: String, mimeType: String, isImage: Bool) async {
        let localId = UUID().uuidString
        await MainActor.run {
            pendingAttachments.append(PendingAttachment(id: localId, name: filename, isImage: isImage, imageData: isImage ? data : nil, uploading: true))
        }
        do {
            let res = try await hub.uploadAttachment(botId, data: data, filename: filename, mimeType: mimeType)
            await MainActor.run {
                if let idx = pendingAttachments.firstIndex(where: { $0.id == localId }) {
                    pendingAttachments[idx].media = res.media
                    pendingAttachments[idx].uploading = false
                }
            }
        } catch {
            await MainActor.run {
                if let idx = pendingAttachments.firstIndex(where: { $0.id == localId }) {
                    pendingAttachments[idx].uploading = false
                    pendingAttachments[idx].error = errText(error, hub.t("uploadFailed"))
                }
            }
        }
    }

    private func submit() {
        let text = input
        let readyMediaIds = pendingAttachments.compactMap { (!$0.uploading && $0.error == nil) ? $0.media?.id : nil }
        let hasText = !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        guard (hasText || !readyMediaIds.isEmpty), !sending else { return }
        input = ""
        pendingAttachments = []
        send(text, mediaIds: readyMediaIds, restore: true)
    }

    private func send(_ raw: String, mediaIds: [String] = [], restore: Bool = false) {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty || !mediaIds.isEmpty else { return }
        sendErr = ""
        sending = true
        Task {
            do { try await hub.sendMessage(botId, trimmed, mediaIds: mediaIds) } catch {
                if restore { input = raw }
                sendErr = errText(error, hub.t("sendFailed"))
            }
            sending = false
        }
    }

    private func interrupt() {
        Task {
            do { _ = try await hub.interruptBot(botId) } catch { sendErr = errText(error, hub.t("stopFailed")) }
        }
    }
}

struct MessageBubble: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var msg: ChatMessage
    var soft: Color
    var showCollabHeader: Bool = true
    var bubbleColor: Color? = nil
    var botId: String = ""
    var inSidebar: Bool = false
    var onPreviewMedia: ((MessageMedia) -> Void)? = nil

    var body: some View {
        let hPad: CGFloat = inSidebar ? 12 : 16
        let vPad: CGFloat = inSidebar ? 9 : 10
        let radius: CGFloat = inSidebar ? 16 : 20
        switch msg.role {
        case "user":
            HStack {
                Spacer(minLength: inSidebar ? 28 : 48)
                VStack(alignment: .trailing, spacing: 6) {
                    if !msg.body.isEmpty {
                        Text(msg.body).font(.system(size: AppFont.body)).textSelection(.enabled)
                    }
                    mediaGrid
                }
                .padding(.horizontal, hPad).padding(.vertical, vPad)
                .background((inSidebar && p.dark) ? p.s2 : soft, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            }
        case "assistant":
            let clock = formatClockTime(msg.createdAt)
            let toolCalls = countToolCalls(msg.tools)
            let dur = formatDurationMs(msg.durationMs)
            let meta = [
                clock.isEmpty ? nil : clock,
                toolCalls > 0 ? hub.t("msgToolCalls", ["count": "\(toolCalls)"]) : nil,
                dur.isEmpty ? nil : hub.t("msgDuration", ["dur": dur]),
                (msg.costUsd ?? 0) > 0 ? String(format: "$%.4f", msg.costUsd!) : nil,
            ].compactMap { $0 }
            VStack(alignment: .leading, spacing: 6) {
                if let from = msg.from, from.hasPrefix("routine:") {
                    Text(hub.t("routineReceiptFrom", ["name": String(from.dropFirst(8))]))
                        .font(.system(size: AppFont.caption, weight: .semibold)).foregroundStyle(p.accent)
                }
                if !msg.body.isEmpty {
                    MarkdownText(text: msg.body)
                }
                mediaGrid
                if !meta.isEmpty {
                    Text(meta.joined(separator: " · ")).font(.system(size: AppFont.caption)).foregroundStyle(p.mute)
                }
            }
            .padding(.horizontal, hPad).padding(.vertical, vPad)
            .background(inSidebar ? p.s1 : p.card, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .padding(.trailing, inSidebar ? 8 : 24)
        case "bot_out", "bot_in":
            let isOut = msg.role == "bot_out"
            let rawPeer = (isOut ? msg.to : msg.from) ?? ""
            let peerId = rawPeer.hasPrefix("bot:") ? String(rawPeer.dropFirst(4)) : rawPeer
            let senderId = (isOut && !botId.isEmpty) ? botId : peerId
            let senderIdx = max(0, hub.bots.firstIndex { $0.id == senderId } ?? 0)
            let senderObj = hub.bots.first { $0.id == senderId }
            let senderCollab = p.botCollab(senderId, senderIdx, custom: senderObj?.color)
            VStack(alignment: .leading, spacing: 6) {
                if showCollabHeader {
                    let peerDisplay = hub.bots.first { $0.id == peerId }?.displayName ?? peerId
                    Text(isOut ? hub.t("delegateTo", ["peer": peerDisplay]) : hub.t("fromPeer", ["peer": peerDisplay]))
                        .font(.system(size: AppFont.caption, weight: .semibold)).foregroundStyle(senderCollab.ink)
                }
                if !msg.body.isEmpty {
                    MarkdownText(text: msg.body)
                }
                mediaGrid
            }
            .padding(.horizontal, hPad).padding(.vertical, vPad)
            .background(bubbleColor ?? senderCollab.bg, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .padding(.leading, isOut ? 24 : 0)
            .padding(.trailing, isOut ? 0 : 24)
        default:
            if msg.from == "command" {
                Text(msg.body)
                    .font(.system(size: AppFont.secondary, design: .monospaced))
                    .foregroundStyle(p.text)
                    .textSelection(.enabled)
                    .padding(.horizontal, hPad).padding(.vertical, vPad)
                    .background(inSidebar ? p.s1 : p.card, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
                    .padding(.trailing, inSidebar ? 8 : 24)
            } else {
                Text(msg.body)
                    .font(.system(size: AppFont.caption))
                    .foregroundStyle(msg.from == "error" ? p.danger : p.mute)
                    .multilineTextAlignment(.center)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity)
            }
        }
    }

    @ViewBuilder
    private var mediaGrid: some View {
        let all = msg.media ?? []
        let imageItems = all.filter(\.isImage)
        let fileItems = all.filter { !$0.isImage }
        if !imageItems.isEmpty {
            let cols = imageItems.count == 1
                ? [GridItem(.flexible())]
                : [GridItem(.flexible(), spacing: 8), GridItem(.flexible(), spacing: 8)]
            LazyVGrid(columns: cols, spacing: 8) {
                ForEach(imageItems) { img in
                    Button {
                        onPreviewMedia?(img)
                    } label: {
                        AuthMediaImage(path: img.url)
                            .frame(maxWidth: imageItems.count == 1 ? 280 : .infinity, maxHeight: 240)
                            .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                    }
                    .buttonStyle(PressStyle())
                }
            }
        }
        if !fileItems.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                ForEach(fileItems) { file in
                    FileAttachmentRow(file: file)
                }
            }
        }
    }
}

private struct SharedFileURL: Identifiable {
    let id = UUID()
    let url: URL
}

struct FileAttachmentRow: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var file: MessageMedia
    @State private var loading = false
    @State private var shareItem: SharedFileURL?
    @State private var errMsg = ""

    private var displayName: String {
        let n = (file.name ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return n.isEmpty ? file.id : n
    }

    private var sizeLabel: String {
        guard let bytes = file.size, bytes > 0 else { return "" }
        if bytes < 1024 { return "\(bytes) B" }
        if bytes < 1024 * 1024 { return String(format: "%.1f KB", Double(bytes) / 1024.0) }
        return String(format: "%.1f MB", Double(bytes) / (1024.0 * 1024.0))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button {
                downloadAndShare()
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: "doc.fill")
                        .font(.system(size: AppFont.cardTitle))
                        .foregroundStyle(p.text2)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(displayName)
                            .font(.system(size: AppFont.secondary, weight: .semibold))
                            .foregroundStyle(p.text)
                            .lineLimit(1)
                        if !sizeLabel.isEmpty {
                            Text(sizeLabel)
                                .font(.system(size: AppFont.caption))
                                .foregroundStyle(p.mute)
                        }
                    }
                    Spacer(minLength: 8)
                    if loading {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: "arrow.down.circle")
                            .font(.system(size: AppFont.cardTitle))
                            .foregroundStyle(p.mute)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            }
            .buttonStyle(PressStyle())
            .disabled(loading)

            if !errMsg.isEmpty {
                Text(errMsg).font(.system(size: AppFont.caption)).foregroundStyle(p.danger)
            }
        }
        #if os(iOS)
        .sheet(item: $shareItem) { item in
            ShareSheet(items: [item.url])
        }
        #endif
    }

    private func downloadAndShare() {
        loading = true
        errMsg = ""
        Task {
            do {
                let data = try await hub.fetchMediaData(file.url)
                let safeName = (displayName as NSString).lastPathComponent
                let fname = safeName.isEmpty ? file.id : safeName
                #if os(macOS)
                let panel = NSSavePanel()
                panel.nameFieldStringValue = fname
                if panel.runModal() == .OK, let dst = panel.url {
                    try data.write(to: dst, options: .atomic)
                }
                #else
                let dir = FileManager.default.temporaryDirectory.appendingPathComponent("bothub-files-\(file.id)", isDirectory: true)
                try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                let dst = dir.appendingPathComponent(fname)
                try data.write(to: dst, options: .atomic)
                shareItem = SharedFileURL(url: dst)
                #endif
            } catch {
                errMsg = hub.t("downloadFailed")
            }
            loading = false
        }
    }
}

#if os(iOS)
struct ShareSheet: UIViewControllerRepresentable {
    var items: [Any]

    func makeUIViewController(context: Context) -> UIActivityViewController {
        let vc = UIActivityViewController(activityItems: items, applicationActivities: nil)
        if let pop = vc.popoverPresentationController {
            if let scene = UIApplication.shared.connectedScenes.first as? UIWindowScene,
               let win = scene.windows.first(where: { $0.isKeyWindow }) ?? scene.windows.first {
                pop.sourceView = win
                pop.sourceRect = CGRect(x: win.bounds.midX, y: win.bounds.midY, width: 1, height: 1)
                pop.permittedArrowDirections = []
            }
        }
        return vc
    }

    func updateUIViewController(_ uiViewController: UIActivityViewController, context: Context) {}
}
#endif

struct AuthMediaImage: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var path: String
    @State private var data: Data?

    var body: some View {
        Group {
            if let data, let img = PlatformImage(data: data) {
                Image(platform: img)
                    .resizable()
                    .scaledToFill()
            } else {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(p.s1)
                    .frame(height: 120)
                    .overlay { ProgressView() }
            }
        }
        .task(id: path) {
            data = try? await hub.fetchMediaData(path)
        }
    }
}

struct MediaPreviewSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(HubStore.self) private var hub
    var media: MessageMedia
    @State private var data: Data?

    var body: some View {
        NavigationStack {
            ZStack {
                Color.black.ignoresSafeArea()
                if let data, let img = PlatformImage(data: data) {
                    Image(platform: img)
                        .resizable()
                        .scaledToFit()
                        .padding()
                } else {
                    ProgressView().tint(.white)
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(hub.t("close")) { dismiss() }
                        .foregroundStyle(.white)
                }
            }
            .task(id: media.url) {
                data = try? await hub.fetchMediaData(media.url)
            }
        }
    }
}

struct CollabSheetView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var botId: String
    var peerId: String

    @State private var history: [ChatMessage] = []
    @State private var loading = false
    @State private var previewMedia: MessageMedia?

    private var botIdx: Int { max(0, hub.bots.firstIndex { $0.id == botId } ?? 0) }
    private var peerIdx: Int { max(0, hub.bots.firstIndex { $0.id == peerId } ?? 0) }
    private var botObj: BotStatus? { hub.bots.first { $0.id == botId } }
    private var peerObj: BotStatus? { hub.bots.first { $0.id == peerId } }
    private var botName: String { botObj?.displayName ?? botId }
    private var peerName: String { peerObj?.displayName ?? peerId }

    private var items: [ChatMessage] {
        let peerTag = "bot:\(peerId)"
        var map: [String: ChatMessage] = [:]
        for m in history { map[m.id] = m }
        for m in (hub.msgs[botId] ?? []) where (m.role == "bot_out" || m.role == "bot_in") && (m.from == peerTag || m.to == peerTag) {
            map[m.id] = m
        }
        return map.values.sorted { ($0.createdAt ?? "") < ($1.createdAt ?? "") }
    }

    var body: some View {
        let botCollab = p.botCollab(botId, botIdx, custom: botObj?.color)
        let peerCollab = p.botCollab(peerId, peerIdx, custom: peerObj?.color)
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                if loading && items.isEmpty {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 40)
                } else if items.isEmpty {
                    Text(hub.t("collabEmpty"))
                        .font(.system(size: AppFont.body))
                        .foregroundStyle(p.mute)
                        .frame(maxWidth: .infinity)
                        .padding(.top, 40)
                } else {
                    ForEach(items) { m in
                        let isOut = m.role == "bot_out"
                        let collab = isOut ? botCollab : peerCollab
                        let senderId = isOut ? botId : peerId
                        let senderIdx = isOut ? botIdx : peerIdx
                        let senderObj = isOut ? botObj : peerObj
                        VStack(alignment: isOut ? .trailing : .leading, spacing: 4) {
                            HStack(spacing: 6) {
                                if !isOut {
                                    BotAvatar(id: senderId, size: 18, index: senderIdx, avatar: senderObj?.avatar, color: senderObj?.color)
                                }
                                Text(isOut ? hub.t("delegateTo", ["peer": peerName]) : hub.t("fromPeer", ["peer": peerName]))
                                    .font(.system(size: AppFont.caption, weight: .semibold))
                                    .foregroundStyle(collab.ink)
                                if isOut {
                                    BotAvatar(id: senderId, size: 18, index: senderIdx, avatar: senderObj?.avatar, color: senderObj?.color)
                                }
                            }
                            MessageBubble(msg: m, soft: p.s1, showCollabHeader: false, bubbleColor: collab.bg, onPreviewMedia: { previewMedia = $0 })
                        }
                        .frame(maxWidth: .infinity, alignment: isOut ? .trailing : .leading)
                    }
                }
            }
            .padding(16)
        }
        .background(p.page)
        .toolbar {
            ToolbarItem(placement: .principal) {
                HStack(spacing: 8) {
                    BotAvatar(id: peerId, size: 22, index: peerIdx, avatar: peerObj?.avatar, color: peerObj?.color)
                    Text(hub.t("collabTitle", ["bot": botName, "peer": peerName]))
                        .font(.system(size: AppFont.title, weight: .bold))
                        .lineLimit(1)
                }
            }
        }
        .inlineNavTitle()
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button(hub.t("close")) { dismiss() }
            }
        }
        .sheet(item: $previewMedia) { media in
            MediaPreviewSheet(media: media)
        }
        .task(id: "\(botId)-\(peerId)") {
            loading = true
            history = (try? await hub.loadCollabMessages(botId, peerId: peerId)) ?? []
            loading = false
        }
    }
}

struct QuestionCardView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    var question: PendingQuestion
    var pal: (accent: Color, text: Color, soft: Color)

    @State private var singles: [String: String] = [:]
    @State private var multis: [String: [String]] = [:]
    @State private var bools: [String: Bool] = [:]
    @State private var customText: [String: String] = [:]
    @State private var customActive: [String: Bool] = [:]
    @State private var submitting = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text((question.title ?? "").isEmpty ? hub.t("questionTitle") : question.title!)
                    .font(.system(size: AppFont.cardTitle, weight: .bold)).foregroundStyle(p.text)
                Spacer()
                Text(hub.t("questionWaitingBadge"))
                    .font(.system(size: AppFont.caption, weight: .semibold)).foregroundStyle(p.busy)
                    .padding(.horizontal, 8).padding(.vertical, 3)
                    .background(p.s1, in: Capsule())
            }

            ForEach(question.fields, id: \.key) { f in
                fieldView(f)
            }

            HStack {
                Spacer()
                Button(hub.t("questionCancel")) { submit(cancelled: true) }
                    .buttonStyle(.bordered).disabled(submitting)
                Button(hub.t("questionSubmit")) { submit(cancelled: false) }
                    .buttonStyle(.borderedProminent).tint(pal.accent)
                    .disabled(submitting || !canSubmit)
            }
        }
        .padding(16)
        .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .onAppear { seedDefaults() }
        .onChange(of: question.id) { _, _ in seedDefaults() }
    }

    @ViewBuilder
    private func fieldView(_ f: QuestionField) -> some View {
        let isMulti = f.type == "multiselect"
        let opts = f.options ?? []
        VStack(alignment: .leading, spacing: 8) {
            if !(f.title ?? "").isEmpty || isMulti {
                HStack {
                    if let title = f.title, !title.isEmpty {
                        Text(title).font(.system(size: AppFont.body, weight: .semibold)).foregroundStyle(p.text)
                    }
                    Spacer()
                    if isMulti {
                        Text(hub.t("questionMultiHint")).font(.system(size: AppFont.caption)).foregroundStyle(p.mute)
                    }
                }
            }
            if let desc = f.description, !desc.isEmpty {
                Text(desc).font(.system(size: AppFont.secondary)).foregroundStyle(p.text2)
            }

            if f.type == "boolean" {
                HStack(spacing: 8) {
                    boolButton(f.key, value: true, label: hub.t("questionYes"))
                    boolButton(f.key, value: false, label: hub.t("questionNo"))
                }
            } else if !opts.isEmpty {
                VStack(spacing: 8) {
                    ForEach(opts, id: \.value) { opt in
                        let active = isMulti
                            ? (multis[f.key] ?? []).contains(opt.value)
                            : (!(customActive[f.key] ?? false) && singles[f.key] == opt.value)
                        Button {
                            if isMulti {
                                var list = multis[f.key] ?? []
                                if let idx = list.firstIndex(of: opt.value) { list.remove(at: idx) }
                                else { list.append(opt.value) }
                                multis[f.key] = list
                            } else {
                                customActive[f.key] = false
                                singles[f.key] = opt.value
                            }
                        } label: {
                            VStack(alignment: .leading, spacing: 2) {
                                Text((opt.label ?? "").isEmpty ? opt.value : opt.label!)
                                    .font(.system(size: AppFont.body, weight: .semibold)).foregroundStyle(p.text)
                                if let d = opt.description, !d.isEmpty {
                                    Text(d).font(.system(size: AppFont.caption)).foregroundStyle(p.text2)
                                }
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 12).padding(.vertical, 10)
                            .background(active ? pal.soft : p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous)
                                .stroke(active ? pal.accent : Color.clear, lineWidth: 1))
                        }
                        .buttonStyle(.plain)
                        .disabled(submitting)
                    }
                    if f.custom == true {
                        TextField(hub.t("questionCustomPlaceholder"), text: Binding(
                            get: { customText[f.key] ?? "" },
                            set: { v in
                                customText[f.key] = v
                                if !isMulti { customActive[f.key] = !v.isEmpty }
                            }
                        ))
                        .textFieldStyle(.plain)
                        .font(.system(size: AppFont.secondary))
                        .padding(.horizontal, 12).padding(.vertical, 9)
                        .background(p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                        .disabled(submitting)
                    }
                }
            } else {
                TextField(f.placeholder ?? hub.t("questionInputPlaceholder"), text: Binding(
                    get: { singles[f.key] ?? "" },
                    set: { singles[f.key] = $0 }
                ))
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.secondary))
                .padding(.horizontal, 12).padding(.vertical, 9)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .disabled(submitting)
            }
        }
    }

    private func boolButton(_ key: String, value: Bool, label: String) -> some View {
        let active = bools[key] == value
        return Button {
            bools[key] = value
        } label: {
            Text(label)
                .font(.system(size: AppFont.body, weight: .semibold)).foregroundStyle(p.text)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 10)
                .background(active ? pal.soft : p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .stroke(active ? pal.accent : Color.clear, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .disabled(submitting)
    }

    private var canSubmit: Bool {
        question.fields.allSatisfy { f in
            guard f.required == true else { return true }
            if f.type == "multiselect" {
                let c = (customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                return !(multis[f.key] ?? []).isEmpty || (f.custom == true && !c.isEmpty)
            }
            if f.type == "boolean" { return bools[f.key] != nil }
            if !(f.options ?? []).isEmpty, customActive[f.key] == true {
                return !(customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            }
            return !(singles[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
    }

    private func seedDefaults() {
        var s: [String: String] = [:]
        var m: [String: [String]] = [:]
        var b: [String: Bool] = [:]
        for f in question.fields {
            if f.type == "multiselect" {
                m[f.key] = f.default?.stringArrayValue ?? []
            } else if f.type == "boolean" {
                if let bv = f.default?.boolValue { b[f.key] = bv }
            } else if let sv = f.default?.stringValue {
                s[f.key] = sv
            }
        }
        singles = s
        multis = m
        bools = b
        customText = [:]
        customActive = [:]
        submitting = false
    }

    private func submit(cancelled: Bool) {
        guard !submitting else { return }
        submitting = true
        var answers: [String: Any] = [:]
        if !cancelled {
            for f in question.fields {
                if f.type == "multiselect" {
                    var list = multis[f.key] ?? []
                    let c = (customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    if f.custom == true, !c.isEmpty, !list.contains(c) { list.append(c) }
                    answers[f.key] = list
                } else if f.type == "boolean" {
                    answers[f.key] = bools[f.key] ?? false
                } else if f.type == "integer" {
                    let raw = (singles[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    if let n = Int(raw) { answers[f.key] = n }
                } else if f.type == "number" {
                    let raw = (singles[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    if let n = Double(raw) { answers[f.key] = n }
                } else if !(f.options ?? []).isEmpty, customActive[f.key] == true {
                    answers[f.key] = (customText[f.key] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                } else {
                    answers[f.key] = singles[f.key] ?? ""
                }
            }
        }
        Task {
            try? await hub.replyQuestion(id: question.id, answers: answers, cancelled: cancelled)
            submitting = false
        }
    }
}
