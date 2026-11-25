import SwiftUI

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
                let dir = FileManager.default.temporaryDirectory.appendingPathComponent("pebble-files-\(file.id)", isDirectory: true)
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
