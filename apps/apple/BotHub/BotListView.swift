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
            if hub.activeServer == nil {
                onboarding
            } else if hub.connState == .connecting && hub.bots.isEmpty {
                ProgressView(hub.t("connectingHost")).frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if hub.connState == .unauthorized || (hub.connState == .error && hub.bots.isEmpty) {
                disconnected
            } else {
                Group {
                    if router.compact {
                        List { rows }.navigationDestination(for: String.self) { ChatView(botId: $0) }
                    } else { List { rows } }
                }
                .hubList(sidebar: !router.compact)
                .refreshable {
                    if hub.isConnected { try? await hub.refreshBots() } else { await hub.reconnect() }
                }
            }
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
            }
            .padding(.horizontal, 12)
            .frame(height: 40)
            .background(p.card, in: Capsule())
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
                    Button(hub.t("desktopShort")) { router.sheet = .desktop(target: to.target ?? "local") }
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

    private var onboarding: some View {
        ContentUnavailableView {
            Label(hub.t("connectHost"), systemImage: "desktopcomputer")
        } description: {
            Text("1. \(hub.t("onboardStep1"))\n2. \(hub.t("onboardStep2"))")
        } actions: {
            Button(hub.t("scanConnect")) { router.sheet = .connect(scan: true) }.buttonStyle(.borderedProminent)
            Button(hub.t("manualEnterAddr")) { router.sheet = .connect(scan: false) }.buttonStyle(.bordered)
        }
    }

    private var disconnected: some View {
        ContentUnavailableView {
            Label(hub.connState == .unauthorized ? hub.t("tokenRequired") : hub.t("cantConnectHost"),
                  systemImage: hub.connState == .unauthorized ? "key" : "wifi.exclamationmark")
        } description: {
            Text(hub.connState == .error && !hub.connError.isEmpty ? hub.connError : hub.t("onboardDesc"))
        } actions: {
            Button(hub.connState == .unauthorized ? hub.t("updateToken") : hub.t("scanOrEnterAddr")) {
                router.sheet = .connect(scan: false)
            }
            .buttonStyle(.borderedProminent)
            Button(hub.t("retry")) { Task { await hub.reconnect() } }.buttonStyle(.bordered)
        }
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
            if bot.isBusy {
                if let tool = hub.tools[bot.id], !tool.isEmpty { return (hub.t("callingTool", ["tool": tool]), p.busy) }
                return (hub.t("thinking"), p.busy)
            }
            if let m = lastMsg {
                let pre = m.role == "user" ? hub.t("youPrefix") : ""
                let s = plainPreview(pre + m.body)
                return (s.isEmpty ? " " : s, p.text2)
            }
            return (bot.description ?? bot.harness ?? "", p.mute)
        }()
        HStack(spacing: 12) {
            BotAvatar(id: bot.id, size: 40, index: index, busy: bot.isBusy)
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
