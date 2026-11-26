import SwiftUI

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
