import SwiftUI

enum BotSortMode: String, CaseIterable {
    case recent
    case manual
}

struct BotListView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p

    var compact = true

    @State private var query = ""
    @AppStorage("bothub_bot_sort_mode") private var sortModeRaw = BotSortMode.recent.rawValue
    @State private var pinnedIds: [String] = UserDefaults.standard.stringArray(forKey: "bothub_pinned_bots") ?? []
    @State private var manualOrder: [String] = UserDefaults.standard.stringArray(forKey: "bothub_manual_order") ?? []
    @State private var confirm: ConfirmItem?

    private var sortMode: BotSortMode {
        BotSortMode(rawValue: sortModeRaw) ?? .recent
    }

    private var orderedBots: [(Int, BotStatus)] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        let indexed = hub.bots.enumerated().filter { _, b in
            q.isEmpty || b.displayName.lowercased().contains(q) || b.id.lowercased().contains(q)
                || (b.description ?? "").lowercased().contains(q)
        }
        let sorted = indexed.sorted { a, b in
            let aPinned = pinnedIds.firstIndex(of: a.element.id)
            let bPinned = pinnedIds.firstIndex(of: b.element.id)
            if let ap = aPinned, let bp = bPinned { return ap < bp }
            if aPinned != nil { return true }
            if bPinned != nil { return false }

            switch sortMode {
            case .recent:
                let ta = hub.last[a.element.id]?.createdAt ?? ""
                let tb = hub.last[b.element.id]?.createdAt ?? ""
                if ta != tb { return ta > tb }
                return a.offset < b.offset
            case .manual:
                let ma = manualOrder.firstIndex(of: a.element.id)
                let mb = manualOrder.firstIndex(of: b.element.id)
                if let ia = ma, let ib = mb { return ia < ib }
                if ma != nil { return true }
                if mb != nil { return false }
                return a.offset < b.offset
            }
        }
        return sorted.map { ($0.offset, $0.element) }
    }

    private func go(_ route: SheetRoute) {
        router.sheet = hub.isConnected ? route : .connect(scan: false)
    }

    private func togglePin(_ id: String) {
        if let idx = pinnedIds.firstIndex(of: id) {
            pinnedIds.remove(at: idx)
        } else {
            pinnedIds.insert(id, at: 0)
        }
        UserDefaults.standard.set(pinnedIds, forKey: "bothub_pinned_bots")
    }

    private func moveBots(from source: IndexSet, to destination: Int) {
        var ids = orderedBots.map(\.1.id)
        ids.move(fromOffsets: source, toOffset: destination)
        manualOrder = ids
        UserDefaults.standard.set(manualOrder, forKey: "bothub_manual_order")
    }

    var body: some View {
        #if os(macOS)
        List { macRows }
            .hubColumnList(compact: compact, page: p.page)
            .contentMargins(.top, 4, for: .scrollContent)
            .refreshable {
                if hub.isConnected { try? await hub.refreshBots() } else { await hub.reconnect() }
            }
            .safeAreaInset(edge: .top, spacing: 0) { sidebarSearchSortBar }
            .safeAreaInset(edge: .bottom, spacing: 0) { sidebarFooterBar }
            .frame(minWidth: 0, maxWidth: .infinity)
            .navigationTitle("")
            .confirmAlert($confirm)
        #else
        @Bindable var r = router
        let base = List(selection: $r.selected) { rows }
            .hubColumnList(compact: compact, page: p.page)
            .hubListSectionSpacing(12)
            .contentMargins(.top, compact ? 8 : 4, for: .scrollContent)
            .refreshable {
                if hub.isConnected { try? await hub.refreshBots() } else { await hub.reconnect() }
            }
            .navigationTitle("")
            .inlineNavTitle()
            .toolbar { toolbar }
            .navigationDestination(isPresented: Binding(
                get: { compact && router.inSettings },
                set: { if !$0 { router.leaveSettings() } }
            )) {
                SettingsSidebarView(compact: true)
            }
            .confirmAlert($confirm)
        if compact {
            base
        } else {
            base
                .safeAreaInset(edge: .top, spacing: 0) { sidebarSearchSortBar }
                .safeAreaInset(edge: .bottom, spacing: 0) { sidebarFooterBar }
        }
        #endif
    }

    @ViewBuilder
    private var sidebarSearchSortBar: some View {
        HStack(spacing: 6) {
            NativeSearchBar(text: $query, placeholder: hub.t("searchBot"))
                .frame(minWidth: 0, maxWidth: .infinity)
                #if os(macOS)
                .frame(height: 28)
                #else
                .frame(height: 34)
                #endif
            Menu {
                Picker(hub.t("sortBots"), selection: $sortModeRaw) {
                    Text(hub.t("sortByRecent")).tag(BotSortMode.recent.rawValue)
                    Text(hub.t("sortManual")).tag(BotSortMode.manual.rawValue)
                }
            } label: {
                Label(hub.t("sortBots"), systemImage: "arrow.up.arrow.down")
                    .labelStyle(.iconOnly)
                    .foregroundStyle(.secondary)
                    #if os(macOS)
                    .frame(width: 26, height: 26)
                    #else
                    .frame(width: 32, height: 32)
                    #endif
            }
            #if os(macOS)
            .menuIndicator(.hidden)
            .buttonStyle(.borderless)
            .help(hub.t("sortBots"))
            #endif
        }
        .padding(.horizontal, 12)
        .padding(.top, 4)
        .padding(.bottom, 6)
    }

    @ViewBuilder
    private var sidebarFooterBar: some View {
        HStack {
            Button { go(.createBot) } label: {
                Label(hub.t("newBot"), systemImage: "plus")
                    .font(.system(size: AppFont.secondary, weight: .medium))
                    .foregroundStyle(.primary)
            }
            #if os(macOS)
            .buttonStyle(.borderless)
            #else
            .buttonStyle(.plain)
            #endif
            Spacer()
            Button { router.openSettings(compact: compact) } label: {
                Label(hub.t("settings"), systemImage: "gearshape")
                    .font(.system(size: AppFont.secondary, weight: .medium))
                    .foregroundStyle(.secondary)
            }
            #if os(macOS)
            .buttonStyle(.borderless)
            #else
            .buttonStyle(.plain)
            #endif
        }
        #if os(macOS)
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        #else
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        #endif
    }

    #if os(macOS)
    @ViewBuilder
    private var macRows: some View {
        if let to = hub.takeovers.first { takeoverSection(to) }
        ForEach(orderedBots, id: \.1.id) { idx, bot in
            let pinned = pinnedIds.contains(bot.id)
            let isUnread = (hub.unread[bot.id] ?? 0) > 0
            Button {
                router.openBot(bot.id)
            } label: {
                BotRow(bot: bot, index: idx, pinned: pinned)
                    .padding(.vertical, 6)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .listRowInsets(EdgeInsets(top: 2, leading: 6, bottom: 2, trailing: 6))
            .listRowBackground(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(router.selected == bot.id ? Color.primary.opacity(p.dark ? 0.14 : 0.08) : Color.clear)
                    .padding(.horizontal, 11)
            )
            .listRowSeparator(.hidden)
            .contextMenu {
                Button {
                    togglePin(bot.id)
                } label: {
                    Label(hub.t(pinned ? "unpinBot" : "pinBot"), systemImage: pinned ? "pin.slash" : "pin")
                }
                Button {
                    hub.toggleUnread(bot.id)
                } label: {
                    Label(hub.t(isUnread ? "markRead" : "markUnread"), systemImage: isUnread ? "envelope.open" : "envelope.badge")
                }
                if hub.bots.count > 1 {
                    Divider()
                    Button(role: .destructive) {
                        confirm = ConfirmItem(title: hub.t("deleteBotTitle", ["name": bot.displayName]), message: hub.t("deleteBotDesc")) {
                            try? await hub.deleteBot(bot.id)
                            if router.selected == bot.id { router.closeBot() }
                        }
                    } label: {
                        Label(hub.t("delete"), systemImage: "trash")
                    }
                }
            }
        }
        .onMove(perform: sortMode == .manual && query.isEmpty ? moveBots : nil)
        if orderedBots.isEmpty {
            Text(query.isEmpty ? hub.t("noBotsYet") : hub.t("noMatchedBots"))
                .foregroundStyle(.secondary)
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
        }
    }
    #endif

    @ViewBuilder
    private var rows: some View {
        #if os(iOS)
        if compact {
            NativeSearchBar(text: $query, placeholder: hub.t("searchBot"))
                .frame(height: 34)
                .listRowInsets(EdgeInsets(top: 0, leading: 0, bottom: 0, trailing: 0))
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
                .selectionDisabled()
        }
        #endif
        if let to = hub.takeovers.first { takeoverSection(to) }
        Section {
            ForEach(orderedBots, id: \.1.id) { idx, bot in
                let pinned = pinnedIds.contains(bot.id)
                let isUnread = (hub.unread[bot.id] ?? 0) > 0
                NavigationLink(value: bot.id) {
                    BotRow(bot: bot, index: idx, pinned: pinned)
                }
                .tag(bot.id)
                .listRowInsets(EdgeInsets(top: 11, leading: 10, bottom: 11, trailing: 10))
                .listRowBackground(compact ? (router.selected == bot.id ? p.s1 : p.card) : nil)
                .listRowSeparator(.hidden)
                .contextMenu {
                    Button {
                        togglePin(bot.id)
                    } label: {
                        Label(hub.t(pinned ? "unpinBot" : "pinBot"), systemImage: pinned ? "pin.slash" : "pin")
                    }
                    Button {
                        hub.toggleUnread(bot.id)
                    } label: {
                        Label(hub.t(isUnread ? "markRead" : "markUnread"), systemImage: isUnread ? "envelope.open" : "envelope.badge")
                    }
                    if hub.bots.count > 1 {
                        Divider()
                        Button(role: .destructive) {
                            confirm = ConfirmItem(title: hub.t("deleteBotTitle", ["name": bot.displayName]), message: hub.t("deleteBotDesc")) {
                                try? await hub.deleteBot(bot.id)
                                if router.selected == bot.id { router.closeBot() }
                            }
                        } label: {
                            Label(hub.t("delete"), systemImage: "trash")
                        }
                    }
                }
            }
            .onMove(perform: sortMode == .manual && query.isEmpty ? moveBots : nil)
        }
        if orderedBots.isEmpty {
            Text(query.isEmpty ? hub.t("noBotsYet") : hub.t("noMatchedBots"))
                .foregroundStyle(.secondary)
                .selectionDisabled()
        }
    }

    #if os(iOS)
    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        if compact {
            ToolbarItem(placement: .hubLeading) {
                Menu {
                    Picker(hub.t("sortBots"), selection: $sortModeRaw) {
                        Text(hub.t("sortByRecent")).tag(BotSortMode.recent.rawValue)
                        Text(hub.t("sortManual")).tag(BotSortMode.manual.rawValue)
                    }
                } label: {
                    Label(hub.t("sortBots"), systemImage: "arrow.up.arrow.down")
                }
            }
            ToolbarItemGroup(placement: .hubTrailing) {
                Button { router.openSettings(compact: compact) } label: { Label(hub.t("settings"), systemImage: "gearshape") }
            }
        }
    }
    #endif

    private func takeoverSection(_ to: TakeoverRequest) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                Label(hub.t("takeoverBanner", ["bot": to.botName ?? to.botId ?? "Bot", "reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                    .font(.system(size: AppFont.body)).foregroundStyle(p.pinkInk)
                HStack {
                    Button(hub.t("desktopShort")) { router.openDesktop(to.target ?? "local") }
                        .buttonStyle(.bordered)
                    Button(hub.t("resumeAI")) { Task { try? await hub.resolveTakeover(to.id) } }
                        .buttonStyle(.borderedProminent)
                }
                .controlSize(.small)
            }
            .padding(.vertical, 4)
        }
        .selectionDisabled()
    }
}

struct BotRow: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    var bot: BotStatus
    var index: Int
    var pinned = false
    @State private var selectBounce = 0

    var body: some View {
        let lastMsg = hub.last[bot.id]
        let unread = hub.unread[bot.id] ?? 0
        let (sub, subColor): (String, Color) = {
            if hub.questions.contains(where: { $0.botId == bot.id }) {
                return (hub.t("questionWaitingBadge"), p.busy)
            }
            if bot.isBusy {
                return (hub.t("processing"), p.busy)
            }
            if let m = lastMsg {
                let pre = m.role == "user" ? hub.t("youPrefix") : ""
                var rawBody = m.body
                if rawBody.isEmpty, let first = m.media?.first {
                    if first.isImage {
                        rawBody = "[\(hub.t("imageAltFallback"))]"
                    } else {
                        let fn = (first.name ?? "").isEmpty ? "" : " \(first.name!)"
                        rawBody = "[\(hub.t("fileAltFallback"))]\(fn)"
                    }
                }
                let s = plainPreview(pre + rawBody)
                return (s.isEmpty ? " " : s, p.text2)
            }
            return (bot.description ?? bot.harness ?? "", p.mute)
        }()
        HStack(spacing: 12) {
            BotAvatar(id: bot.id, size: 40, index: index, avatar: bot.avatar, color: bot.color, busy: bot.isBusy, bounceTrigger: selectBounce)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 4) {
                    if pinned {
                        Image(systemName: "pin.fill")
                            .font(.system(size: AppFont.caption))
                            .foregroundStyle(.secondary)
                    }
                    Text(bot.displayName).font(.system(size: AppFont.body, weight: .semibold)).foregroundStyle(p.text).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(formatShortTime(lastMsg?.createdAt)).font(.system(size: AppFont.caption)).foregroundStyle(p.mute)
                }
                HStack {
                    Text(sub).font(.system(size: AppFont.secondary)).foregroundStyle(subColor).lineLimit(1)
                    Spacer(minLength: 4)
                    if unread > 0 {
                        Text(unread > 99 ? "99+" : "\(unread)")
                            .font(.system(size: AppFont.caption, weight: .semibold)).foregroundStyle(p.lilacInk)
                            .padding(.horizontal, 6).frame(minWidth: 20, minHeight: 18)
                            .background(p.lilac, in: Capsule())
                    }
                }
            }
        }
        .padding(.vertical, 4)
        .onChange(of: router.selected) { _, newId in
            if newId == bot.id { selectBounce += 1 }
        }
    }
}

#if os(iOS)
private struct NativeSearchBar: View {
    @Environment(\.palette) private var p
    @Binding var text: String
    var placeholder: String

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(.secondary)
            TextField(placeholder, text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: AppFont.secondary))
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .submitLabel(.search)
            if !text.isEmpty {
                Button {
                    text = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, 10)
        .frame(height: 34)
        .background(Color.primary.opacity(p.dark ? 0.12 : 0.07), in: Capsule())
    }
}
#elseif os(macOS)
private struct NativeSearchBar: NSViewRepresentable {
    @Binding var text: String
    var placeholder: String

    func makeCoordinator() -> Coordinator { Coordinator(text: $text) }

    func makeNSView(context: Context) -> NSSearchField {
        let field = NSSearchField()
        field.placeholderString = placeholder
        field.delegate = context.coordinator
        field.sendsSearchStringImmediately = true
        field.sendsWholeSearchString = false
        field.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        field.setContentHuggingPriority(.defaultLow, for: .horizontal)
        context.coordinator.observeFocus(field: field)
        return field
    }

    func updateNSView(_ nsView: NSSearchField, context: Context) {
        if nsView.stringValue != text { nsView.stringValue = text }
        if nsView.placeholderString != placeholder { nsView.placeholderString = placeholder }
    }

    func sizeThatFits(_ proposal: ProposedViewSize, nsView: NSSearchField, context: Context) -> CGSize? {
        CGSize(width: max(80, proposal.width ?? 160), height: 28)
    }

    final class Coordinator: NSObject, NSSearchFieldDelegate {
        @Binding var text: String
        private var observer: NSObjectProtocol?
        init(text: Binding<String>) { _text = text }

        deinit {
            if let observer { NotificationCenter.default.removeObserver(observer) }
        }

        func observeFocus(field: NSSearchField) {
            if let observer { NotificationCenter.default.removeObserver(observer) }
            observer = NotificationCenter.default.addObserver(
                forName: .hubFocusSearch,
                object: nil,
                queue: .main
            ) { [weak field] _ in
                guard let field else { return }
                field.window?.makeFirstResponder(field)
            }
        }

        func controlTextDidChange(_ obj: Notification) {
            if let field = obj.object as? NSSearchField {
                text = field.stringValue
            }
        }
    }
}
#endif
