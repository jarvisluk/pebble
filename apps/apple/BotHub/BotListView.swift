import SwiftUI

struct BotListView: View {
    @Environment(HubStore.self) private var hub
    @Environment(Router.self) private var router
    @Environment(\.palette) private var p
    @State private var query = ""
    @State private var searching = false
    @FocusState private var searchFocused: Bool

    private var filtered: [(Int, BotStatus)] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return hub.bots.enumerated().filter { _, b in
            q.isEmpty || b.displayName.lowercased().contains(q) || b.id.lowercased().contains(q)
                || (b.description ?? "").lowercased().contains(q)
        }.map { ($0.offset, $0.element) }
    }

    private func go(_ route: SheetRoute) {
        router.sheet = hub.isConnected ? route : .connect(scan: false)
    }

    var body: some View {
        Group {
            if router.compact {
                List { rows }.navigationDestination(for: String.self) { ChatView(botId: $0) }
            } else { List { rows } }
        }
        .hubList(sidebar: !router.compact)
        .refreshable {
            if hub.isConnected { try? await hub.refreshBots() } else { await hub.reconnect() }
        }
        .navigationTitle("Bots")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar }
    }

    @ViewBuilder
    private var rows: some View {
        if searching {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").foregroundStyle(p.mute)
                TextField(hub.t("searchBot"), text: $query)
                    .focused($searchFocused)
                    .submitLabel(.search)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            .padding(.horizontal, 12)
            .frame(height: 40)
            .background(router.compact ? p.card : p.s1, in: Capsule())
            .listRowSeparator(.hidden)
            .listRowBackground(Color.clear)
        }
        if let to = hub.takeovers.first { takeoverSection(to) }
        Section {
            ForEach(filtered, id: \.1.id) { idx, bot in
                if router.compact {
                    NavigationLink(value: bot.id) { BotRow(bot: bot, index: idx) }
                        .listRowSeparator(.hidden)
                } else {
                    Button { router.openBot(bot.id) } label: {
                        BotRow(bot: bot, index: idx).contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .listRowSeparator(.hidden)
                    .listRowInsets(EdgeInsets(top: 4, leading: 20, bottom: 4, trailing: 20))
                        .listRowBackground(
                            RoundedRectangle(cornerRadius: 12, style: .continuous)
                                .fill(router.selected == bot.id ? p.sel : Color.clear)
                                .padding(.horizontal, 8)
                        )
                }
            }
        }
        if filtered.isEmpty {
            Text(query.isEmpty ? hub.t("noBotsYet") : hub.t("noMatchedBots")).foregroundStyle(.secondary)
        }
    }

    private var title: some View {
        Text("Bots").font(.system(size: 22, weight: .bold)).fixedSize()
            .padding(.leading, router.compact ? 0 : 8)
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .principal) { Color.clear.frame(width: 1, height: 1) }
        if #available(iOS 26.0, *) {
            ToolbarItem(placement: .topBarLeading) { title }.sharedBackgroundVisibility(.hidden)
        } else {
            ToolbarItem(placement: .topBarLeading) { title }
        }
        ToolbarItemGroup(placement: .topBarTrailing) {
            Button {
                searching.toggle()
                if searching { searchFocused = true } else { query = "" }
            } label: {
                Label(hub.t("searchBot"), systemImage: searching ? "xmark" : "magnifyingglass")
            }
            Button { go(.createBot) } label: { Label(hub.t("newBot"), systemImage: "plus") }
            Button { go(.settings) } label: { Label(hub.t("settings"), systemImage: "gearshape") }
        }
    }

    private func takeoverSection(_ to: TakeoverRequest) -> some View {
        Section {
            VStack(alignment: .leading, spacing: 8) {
                Label(hub.t("takeoverBanner", ["bot": to.botName ?? to.botId ?? "Bot", "reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                    .font(.system(size: 15)).foregroundStyle(p.pinkInk)
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
    @Environment(\.palette) private var p
    var bot: BotStatus
    var index: Int

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
            BotAvatar(id: bot.id, size: 40, index: index, avatar: bot.avatar, color: bot.color, busy: bot.isBusy)
            VStack(alignment: .leading, spacing: 2) {
                HStack {
                    Text(bot.displayName).font(.system(size: 16, weight: .semibold)).lineLimit(1)
                    Spacer(minLength: 4)
                    Text(formatShortTime(lastMsg?.createdAt)).font(.system(size: 13)).foregroundStyle(p.mute)
                }
                HStack {
                    Text(sub).font(.system(size: 15)).foregroundStyle(subColor).lineLimit(1)
                    Spacer(minLength: 4)
                    if unread > 0 {
                        Text(unread > 99 ? "99+" : "\(unread)")
                            .font(.system(size: 13, weight: .semibold)).foregroundStyle(p.lilacInk)
                            .padding(.horizontal, 6).frame(minWidth: 20, minHeight: 18)
                            .background(p.lilac, in: Capsule())
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }
}
