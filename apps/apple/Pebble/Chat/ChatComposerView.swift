import SwiftUI
#if os(iOS)
import PhotosUI
#endif
import UniformTypeIdentifiers

/// 独立输入框与联想状态容器，避免打字时触发聊天记录区重建。
struct ChatComposerView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p
    @Environment(\.hubCompact) private var compact

    var botId: String
    var name: String
    var isBusy: Bool
    var inDesktopSidebar: Bool
    var pal: (accent: Color, text: Color, soft: Color)
    var commands: [SlashCommandItem]
    var pendingDrop: (seq: Int, urls: [URL])?
    @Binding var sendErr: String

    @State private var input = ""
    @State private var sending = false
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

    private var showStop: Bool {
        isBusy && input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
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

    var body: some View {
        let readyMediaIds = pendingAttachments.compactMap { (!$0.uploading && $0.error == nil) ? $0.media?.id : nil }
        let hasUploading = pendingAttachments.contains { $0.uploading }
        let hasText = !input.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        let hasContent = hasText || !readyMediaIds.isEmpty
        let showSlashAC = slashQuery != nil && !filteredCommands.isEmpty
        let showMentionAC = activeMentionRange != nil && !mentions.isEmpty

        VStack(spacing: 8) {
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

            composerInputBar(hasContent: hasContent, hasUploading: hasUploading)
        }
        .frame(maxWidth: 840)
        .padding(.horizontal, inDesktopSidebar ? 12 : (compact ? 10 : 14))
        .padding(.top, inDesktopSidebar ? 8 : 6)
        .padding(.bottom, inDesktopSidebar ? 10 : 8)
        .frame(maxWidth: .infinity)
        .task(id: botId) {
            pendingAttachments = []
            mentions = []
            acDismissed = false
        }
        .onChange(of: hub.pendingChatInsert?.seq) { _, seq in
            guard seq != nil, let insert = hub.pendingChatInsert, !insert.text.isEmpty else { return }
            let needsSpace = !input.isEmpty && !(input.last?.isWhitespace ?? false)
            input += (needsSpace ? " " : "") + insert.text
            focused = true
        }
        .onChange(of: pendingDrop?.seq) { _, _ in
            if let urls = pendingDrop?.urls {
                importFileURLs(urls)
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
    }

    @ViewBuilder
    private func composerInputBar(hasContent: Bool, hasUploading: Bool) -> some View {
        let row = HStack(alignment: .bottom, spacing: 10) {
            #if os(iOS)
            Menu {
                Button { showPhotoPicker = true } label: { Label(hub.t("photoLibrary"), systemImage: "photo") }
                Button { showFileImporter = true } label: { Label(hub.t("chooseFile"), systemImage: "doc") }
            } label: {
                Image(systemName: "plus")
                    .font(.system(size: AppFont.cardTitle, weight: .semibold))
                    .frame(width: composerControlHeight, height: composerControlHeight)
                    .contentShape(Circle())
            }
            .composerGlassButton(onCard: inDesktopSidebar)
            .accessibilityLabel(hub.t("attachments"))
            #else
            Button { showFileImporter = true } label: {
                Image(systemName: "plus")
                    .font(.system(size: AppFont.cardTitle, weight: .semibold))
                    .frame(width: composerControlHeight, height: composerControlHeight)
                    .contentShape(Circle())
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
                .padding(.vertical, 8)
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

            let permModes = hub.supportedPermissionModes(for: hub.bots.first(where: { $0.id == botId })?.harness)
            if permModes.count > 1 {
                let botPerm = hub.clampPermissionMode(hub.bots.first(where: { $0.id == botId })?.permissionMode,
                                                      harnessId: hub.bots.first(where: { $0.id == botId })?.harness)
                let isPlan = botPerm == "plan"
                Menu {
                    ForEach(permModes, id: \.self) { m in
                        Button {
                            guard m != botPerm else { return }
                            Task {
                                do {
                                    try await hub.updateBot(botId, ["permissionMode": m])
                                } catch {
                                    sendErr = errText(error, hub.t("updateFailed"))
                                }
                            }
                        } label: {
                            if botPerm == m {
                                Label(hub.t("permMode_\(m)"), systemImage: "checkmark")
                            } else {
                                Text(hub.t("permMode_\(m)"))
                            }
                        }
                    }
                } label: {
                    HStack(spacing: 4) {
                        Text(hub.t("permMode_\(botPerm)"))
                            .font(.system(size: AppFont.caption, weight: .medium))
                            .lineLimit(1)
                        Image(systemName: "chevron.up.chevron.down")
                            .font(.system(size: AppFont.caption, weight: .regular))
                    }
                }
                .composerPermissionMenu(isPlan: isPlan, onCard: inDesktopSidebar)
                .accessibilityLabel(hub.t("permMode"))
            }

            Button {
                showStop ? interrupt() : submit()
            } label: {
                Image(systemName: showStop ? "stop.fill" : "arrow.up")
                    .font(.system(size: showStop ? AppFont.secondary : AppFont.cardTitle, weight: .bold))
                    .frame(width: composerControlHeight, height: composerControlHeight)
                    .contentShape(Circle())
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
