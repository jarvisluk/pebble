import SwiftUI

struct WorkspaceFilesView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var botId: String
    var initialDir: String = ""
    @Binding var showHidden: Bool
    var refreshTrigger: Int = 0
    var inWideStage: Bool = false
    var onOpenFile: (String) -> Void

    @State private var dirPath = ""
    @State private var listResult: WorkspaceListResult?
    @State private var listLoading = false
    @State private var listErr = ""
    @State private var listSeq = 0

    private var segments: [String] {
        dirPath.split(separator: "/").map(String.init)
    }

    private var botCollab: (bg: Color, ink: Color) {
        let idx = max(0, hub.bots.firstIndex { $0.id == botId } ?? 0)
        let botObj = hub.bots.first { $0.id == botId }
        return p.botCollab(botId, idx, custom: botObj?.color)
    }

    var body: some View {
        Group {
            if inWideStage {
                listPanel(inCard: true)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                    .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                    .padding(12)
            } else {
                listPanel(inCard: false)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(p.page)
        .task(id: "\(botId)-\(initialDir)") {
            await loadDir(initialDir)
        }
        .onChange(of: showHidden) { _, _ in
            Task { await loadDir(dirPath) }
        }
        .onChange(of: refreshTrigger) { _, _ in
            Task { await loadDir(dirPath) }
        }
    }

    @ViewBuilder
    private func listPanel(inCard: Bool) -> some View {
        VStack(spacing: 0) {
            if !dirPath.isEmpty {
                inlineBreadcrumbs
            }

            if listLoading && listResult == nil {
                Spacer()
                ProgressView()
                Spacer()
            } else if !listErr.isEmpty && listResult == nil {
                VStack(spacing: 10) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.system(size: AppFont.hero))
                        .foregroundStyle(p.mute)
                    Text(hub.t("workspaceFiles"))
                        .font(.system(size: AppFont.title, weight: .semibold))
                        .foregroundStyle(p.text)
                    Text(listErr)
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.text2)
                        .multilineTextAlignment(.center)
                    Button(hub.t("retry")) {
                        Task { await loadDir(dirPath) }
                    }
                    .buttonStyle(.borderedProminent)
                }
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let entries = listResult?.entries, entries.isEmpty, dirPath.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "folder")
                        .font(.system(size: AppFont.hero))
                        .foregroundStyle(p.mute)
                    Text(hub.t("emptyFolder"))
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.text2)
                }
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                let entries = listResult?.entries ?? []
                ScrollView {
                    if entries.isEmpty && !dirPath.isEmpty {
                        VStack(spacing: 0) {
                            parentBackRow(inCard: inCard)
                            Text(hub.t("emptyFolder"))
                                .font(.system(size: AppFont.secondary))
                                .foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity, alignment: .center)
                                .padding(.vertical, 24)
                        }
                        .padding(inCard ? 8 : 0)
                        .background(inCard ? Color.clear : p.card, in: RoundedRectangle(cornerRadius: inCard ? 0 : 18, style: .continuous))
                        .padding(inCard ? 0 : 12)
                    } else {
                        LazyVStack(spacing: 0) {
                            if !dirPath.isEmpty {
                                parentBackRow(inCard: inCard)
                                rowSeparator(inset: 14)
                            }

                            ForEach(Array(entries.enumerated()), id: \.element.id) { idx, ent in
                                if idx > 0 {
                                    rowSeparator(inset: inCard ? 16 : 14)
                                }
                                Button {
                                    if ent.isDir {
                                        Task { await loadDir(ent.path) }
                                    } else {
                                        onOpenFile(ent.path)
                                    }
                                } label: {
                                    fileRow(ent, inCard: inCard)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.vertical, inCard ? 8 : 2)
                        .background(inCard ? Color.clear : p.card, in: RoundedRectangle(cornerRadius: inCard ? 0 : 18, style: .continuous))
                        .padding(inCard ? 0 : 12)
                    }
                }
                .refreshable {
                    await loadDir(dirPath)
                }
            }
        }
    }

    private func parentBackRow(inCard: Bool) -> some View {
        Button {
            let parent = listResult?.parent ?? ""
            Task { await loadDir(parent) }
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "arrow.uturn.backward")
                    .font(.system(size: AppFont.body, weight: .medium))
                    .foregroundStyle(p.text2)
                    .frame(width: 24)
                Text("..")
                    .font(.system(size: AppFont.body, weight: .medium))
                    .foregroundStyle(p.text)
                Spacer()
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.horizontal, inCard ? 6 : 0)
    }

    private func rowSeparator(inset: CGFloat) -> some View {
        Rectangle()
            .fill(p.s2)
            .frame(height: 0.5)
            .padding(.horizontal, inset)
    }

    private var inlineBreadcrumbs: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 4) {
                Button {
                    Task { await loadDir("") }
                } label: {
                    Label(hub.t("rootWorkspace"), systemImage: "folder")
                        .font(.system(size: AppFont.secondary, weight: .medium))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)

                ForEach(Array(segments.enumerated()), id: \.offset) { idx, seg in
                    Image(systemName: "chevron.right")
                        .font(.system(size: AppFont.caption, weight: .semibold))
                        .foregroundStyle(.tertiary)
                    let target = segments.prefix(idx + 1).joined(separator: "/")
                    Button(seg) {
                        Task { await loadDir(target) }
                    }
                    .buttonStyle(.plain)
                    .font(.system(size: AppFont.secondary, weight: idx == segments.count - 1 ? .semibold : .medium))
                    .foregroundStyle(idx == segments.count - 1 ? p.text : .secondary)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
        }
        .overlay(alignment: .bottom) {
            Rectangle().fill(p.s2).frame(height: 0.5)
        }
    }

    private func fileRow(_ ent: WorkspaceFileEntry, inCard: Bool) -> some View {
        let meta = fileSymbol(for: ent)
        return HStack(spacing: 12) {
            Image(systemName: meta.symbol)
                .font(.system(size: AppFont.cardTitle, weight: .medium))
                .foregroundStyle(meta.color)
                .frame(width: 24)

            VStack(alignment: .leading, spacing: 2) {
                Text(ent.name)
                    .font(.system(size: AppFont.body, weight: .medium))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                if ent.isShared == true || !ent.isDir {
                    HStack(spacing: 6) {
                        if ent.isShared == true {
                            Text(hub.t("sharedBadge"))
                                .font(.system(size: AppFont.caption, weight: .semibold))
                                .padding(.horizontal, 6)
                                .padding(.vertical, 1)
                                .background(botCollab.bg, in: Capsule())
                                .foregroundStyle(botCollab.ink)
                        }
                        if !ent.isDir {
                            Text(formatByteSize(ent.size))
                                .font(.system(size: AppFont.caption))
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            Spacer(minLength: 4)
            Image(systemName: "chevron.right")
                .font(.system(size: AppFont.caption, weight: .semibold))
                .foregroundStyle(.tertiary)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .padding(.horizontal, inCard ? 8 : 2)
        .contentShape(Rectangle())
    }

    private func fileSymbol(for ent: WorkspaceFileEntry) -> (symbol: String, color: Color) {
        if ent.isDir {
            if ent.isShared == true {
                return ("folder.fill.badge.person.crop", botCollab.ink)
            }
            return ("folder.fill", p.accent)
        }
        let ext = (ent.name as NSString).pathExtension.lowercased()
        switch ext {
        case "md", "markdown", "mdx":
            return ("doc.richtext", .secondary)
        case "png", "jpg", "jpeg", "gif", "webp":
            return ("photo", .secondary)
        case "go", "swift", "ts", "tsx", "js", "jsx", "py", "rs", "json", "yaml", "yml", "toml", "sh", "sql", "css", "html":
            return ("chevron.left.forwardslash.chevron.right", .secondary)
        default:
            return ("doc.text", .secondary)
        }
    }

    private func loadDir(_ path: String) async {
        guard !botId.isEmpty else { return }
        listSeq += 1
        let seq = listSeq
        listLoading = true
        listErr = ""
        do {
            let res = try await hub.listWorkspaceFiles(botId: botId, path: path, showHidden: showHidden)
            guard seq == listSeq else { return }
            listResult = res
            dirPath = res.path
        } catch {
            guard seq == listSeq else { return }
            listErr = error.localizedDescription
        }
        if seq == listSeq {
            listLoading = false
        }
    }

    private func formatByteSize(_ bytes: Int64) -> String {
        if bytes < 1024 { return "\(bytes) B" }
        let kb = Double(bytes) / 1024
        if kb < 1024 { return String(format: "%.1f KB", kb) }
        return String(format: "%.1f MB", kb / 1024)
    }
}
