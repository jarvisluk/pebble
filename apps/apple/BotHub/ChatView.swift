import SwiftUI

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
    var botId: String

    @State private var input = ""
    @State private var sending = false
    @State private var sendErr = ""
    @State private var detailOpen = false
    @FocusState private var focused: Bool

    private var index: Int { hub.bots.firstIndex { $0.id == botId } ?? -1 }
    private var bot: BotStatus? { index >= 0 ? hub.bots[index] : nil }
    private var name: String { bot?.displayName ?? botId }
    private var list: [ChatMessage] { hub.msgs[botId] ?? [] }
    private var showStop: Bool { (bot?.isBusy ?? false) && input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        let pal = p.bot(botId, index, custom: bot?.color)
        thread(pal)
            .safeAreaInset(edge: .top, spacing: 0) {
                if let to = hub.takeovers.first {
                    HStack(spacing: 8) {
                        Label(hub.t("takeoverInline", ["reason": to.reason ?? ""]), systemImage: "hand.raised.fill")
                            .font(.system(size: 15)).foregroundStyle(p.pinkInk).lineLimit(2)
                        Spacer(minLength: 4)
                        Button(hub.t("handleAction")) { router.sheet = .desktop(target: to.target ?? "local") }
                            .buttonStyle(.bordered)
                        Button(hub.t("resumeShort")) { Task { try? await hub.resolveTakeover(to.id) } }
                            .buttonStyle(.borderedProminent).tint(pal.accent)
                    }
                    .controlSize(.small)
                    .padding(.horizontal, 16).padding(.vertical, 8)
                    .background(.bar)
                }
            }
            .safeAreaInset(edge: .bottom, spacing: 0) { composer(pal) }
            .background(p.page)
            .toolbarBackground(p.page, for: .navigationBar)
            .detailPanel(isPresented: $detailOpen, compact: router.compact) {
                BotDetailView(botId: botId, isPresented: $detailOpen, inline: !router.compact)
            }
            .navigationTitle(name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) { titleView }
                ToolbarItemGroup(placement: .primaryAction) {
                    Button { router.sheet = .desktop(target: hub.takeovers.first?.target ?? "local") } label: {
                        Label(hub.t("remoteDesktop"), systemImage: hub.takeovers.isEmpty ? "display" : "exclamationmark.triangle")
                    }
                    Button { detailOpen.toggle() } label: { Label(hub.t("botDetails"), systemImage: "info.circle") }
                }
            }
            .task(id: botId) {
                hub.setActiveChat(botId)
                try? await hub.loadMessages(botId)
            }
            .onDisappear { hub.setActiveChat(nil) }
            .onChange(of: hub.bots.map(\.id)) { _, ids in
                if !ids.isEmpty, !ids.contains(botId) {
                    detailOpen = false
                    router.closeBot()
                }
            }
    }

    private var titleView: some View {
        HStack(spacing: 8) {
            BotAvatar(id: botId, size: 28, index: index, avatar: bot?.avatar, color: bot?.color, busy: bot?.isBusy ?? false)
            Text(name).font(.system(size: 17, weight: .bold)).lineLimit(1)
        }
    }

    private func thread(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 10) {
                    if list.isEmpty { emptyState(pal) }
                    ForEach(Array(list.enumerated()), id: \.element.id) { i, m in
                        let prev = i > 0 ? list[i - 1] : nil
                        if showStamp(m, prev) {
                            Text(formatStamp(m.createdAt)).font(.system(size: 13)).foregroundStyle(p.mute)
                                .frame(maxWidth: .infinity).padding(.top, 4)
                        }
                        MessageBubble(msg: m, soft: pal.soft)
                    }
                    if bot?.isBusy == true {
                        HStack(spacing: 8) {
                            BotAvatar(id: botId, size: 24, index: index, avatar: bot?.avatar, color: bot?.color, busy: true)
                            Text(hub.tools[botId].map { $0.isEmpty ? hub.t("thinking") : hub.t("callingTool", ["tool": $0]) } ?? hub.t("thinking"))
                                .font(.system(size: 15)).foregroundStyle(p.busy)
                        }
                        .padding(.vertical, 6)
                    }
                    if !sendErr.isEmpty { StatusText(text: sendErr) }
                    Color.clear.frame(height: 1).id("bottom")
                }
                .frame(maxWidth: 760)
                .padding(.horizontal, router.compact ? 12 : 24)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity)
            }
            .defaultScrollAnchor(.bottom)
            .scrollDismissesKeyboard(.interactively)
            .onChange(of: list.count) { _, _ in withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
            .onChange(of: bot?.isBusy) { _, _ in withAnimation { proxy.scrollTo("bottom", anchor: .bottom) } }
        }
    }

    private func showStamp(_ m: ChatMessage, _ prev: ChatMessage?) -> Bool {
        guard let prev else { return true }
        guard let a = parseISO(m.createdAt), let b = parseISO(prev.createdAt) else { return false }
        return a.timeIntervalSince(b) > 300
    }

    private func emptyState(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        let cards: [(String, String, Color, Color, () -> Void)] = [
            (hub.t("sugAskTitle"), hub.t("sugAskSub"), p.pink, p.pinkInk, { send(hub.t("sugAskTitle")) }),
            (hub.t("statusLabel"), hub.t("sugStatusSub"), p.lilac, p.lilacInk, { send("/status") }),
            (hub.t("sugCollabTitle"), "/ask ", p.sage, p.sageInk, { input = "/ask "; focused = true }),
            (hub.t("sugListTitle"), "/bots", pal.soft, pal.accent, { send("/bots") }),
        ]
        return VStack(spacing: 10) {
            BotAvatar(id: botId, size: 72, index: index, avatar: bot?.avatar, color: bot?.color)
            Text(name).font(.system(size: 22, weight: .bold))
            Text((bot?.description ?? "").isEmpty ? hub.t("emptyChatHint") : bot!.description!)
                .font(.system(size: 15)).foregroundStyle(p.text2).multilineTextAlignment(.center)
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10), GridItem(.flexible(), spacing: 10)], spacing: 10) {
                ForEach(cards, id: \.0) { c in
                    Button(action: c.4) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(c.0).font(.system(size: 16, weight: .semibold)).foregroundStyle(c.3)
                            Text(c.1).font(.system(size: 13)).foregroundStyle(p.text2)
                        }
                        .frame(maxWidth: .infinity, minHeight: 56, alignment: .topLeading)
                        .padding(12)
                        .background(c.2, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                    }
                    .buttonStyle(PressStyle())
                }
            }
            .frame(maxWidth: 460)
            .padding(.top, 8)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 40)
    }

    private func composer(_ pal: (accent: Color, text: Color, soft: Color)) -> some View {
        let hasText = !input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return HStack(alignment: .bottom, spacing: 8) {
            Menu {
                Button { input = ""; send("/status") } label: { Label("/status · \(hub.t("statusLabel"))", systemImage: "info.circle") }
                Button { input = ""; send("/bots") } label: { Label("/bots · \(hub.t("allBotsLabel"))", systemImage: "person.3") }
                Button { input = "/ask "; focused = true } label: { Label("/ask · \(hub.t("collabLabel"))", systemImage: "arrow.triangle.branch") }
                Divider()
                Button(role: .destructive) { input = ""; send("/reset") } label: { Label("/reset · \(hub.t("resetSessionLabel"))", systemImage: "arrow.counterclockwise") }
            } label: {
                Text("/").font(.system(size: 19, weight: .semibold)).foregroundStyle(p.text2)
                    .frame(width: 40, height: 40)
                    .background(p.s1, in: Circle())
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .fixedSize()
            TextField(hub.t("msgBotPh", ["name": name]), text: $input, axis: .vertical)
                .textFieldStyle(.plain)
                .font(.system(size: 16))
                .lineLimit(1...6)
                .focused($focused)
                .padding(.vertical, 11)
                .onChange(of: input) { _, v in if v.count > 8000 { input = String(v.prefix(8000)) } }
            Button {
                showStop ? interrupt() : submit()
            } label: {
                Image(systemName: showStop ? "stop.fill" : "arrow.up")
                    .font(.system(size: showStop ? 13 : 16, weight: .bold))
                    .foregroundStyle(showStop ? p.page : pal.text)
                    .frame(width: 40, height: 40)
                    .background(showStop ? p.text : (hasText ? pal.accent : p.s2), in: Circle())
            }
            .buttonStyle(PressStyle())
            .disabled(!showStop && (!hasText || sending))
            .keyboardShortcut(.return, modifiers: .command)
            .accessibilityLabel(showStop ? hub.t("stopOutput") : hub.t("sendMsg"))
        }
        .padding(6)
        .background(p.card, in: RoundedRectangle(cornerRadius: 26, style: .continuous))
        .frame(maxWidth: 760)
        .padding(.horizontal, router.compact ? 12 : 24)
        .padding(.bottom, 8)
        .frame(maxWidth: .infinity)
    }

    private func submit() {
        let text = input
        guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, !sending else { return }
        input = ""
        send(text, restore: true)
    }

    private func send(_ raw: String, restore: Bool = false) {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return }
        sendErr = ""
        sending = true
        Task {
            do { try await hub.sendMessage(botId, trimmed) } catch {
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

    var body: some View {
        switch msg.role {
        case "user":
            HStack {
                Spacer(minLength: 48)
                Text(msg.body).font(.system(size: 16)).textSelection(.enabled)
                    .padding(.horizontal, 16).padding(.vertical, 10)
                    .background(soft, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            }
        case "assistant":
            let meta = [
                (msg.durationMs ?? 0) > 0 ? String(format: "%.1fs", msg.durationMs! / 1000) : nil,
                (msg.costUsd ?? 0) > 0 ? String(format: "$%.4f", msg.costUsd!) : nil,
            ].compactMap { $0 }
            VStack(alignment: .leading, spacing: 6) {
                if let from = msg.from, from.hasPrefix("routine:") {
                    Text(hub.t("routineReceiptFrom", ["name": String(from.dropFirst(8))]))
                        .font(.system(size: 13, weight: .semibold)).foregroundStyle(p.accent)
                }
                MarkdownText(text: msg.body)
                if !meta.isEmpty {
                    Text(meta.joined(separator: " · ")).font(.system(size: 13)).foregroundStyle(p.mute)
                }
            }
            .padding(.horizontal, 16).padding(.vertical, 10)
            .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .padding(.trailing, 24)
        case "bot_out", "bot_in":
            VStack(alignment: .leading, spacing: 6) {
                Text(msg.role == "bot_out" ? hub.t("delegateTo", ["peer": msg.to ?? ""]) : hub.t("fromPeer", ["peer": msg.from ?? ""]))
                    .font(.system(size: 13, weight: .semibold)).foregroundStyle(p.lilacInk)
                MarkdownText(text: msg.body)
            }
            .padding(.horizontal, 16).padding(.vertical, 10)
            .background(p.lilac, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .padding(.trailing, 24)
        default:
            if msg.from == "command" {
                Text(msg.body)
                    .font(.system(size: 15, design: .monospaced))
                    .foregroundStyle(p.text)
                    .textSelection(.enabled)
                    .padding(.horizontal, 16).padding(.vertical, 10)
                    .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                    .padding(.trailing, 24)
            } else {
                Text(msg.body)
                    .font(.system(size: 13))
                    .foregroundStyle(msg.from == "error" ? p.danger : p.mute)
                    .multilineTextAlignment(.center)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity)
            }
        }
    }
}

extension View {
    /// 宽屏用 inspector；iPhone 上 inspector 会拆掉已推入的页面，改用 sheet。
    @ViewBuilder
    func detailPanel<C: View>(isPresented: Binding<Bool>, compact: Bool, @ViewBuilder content: @escaping () -> C) -> some View {
        if compact {
            sheet(isPresented: isPresented) { NavigationStack { content() } }
        } else {
            inspector(isPresented: isPresented) { content().inspectorColumnWidth(min: 340, ideal: 380, max: 460) }
        }
    }
}
