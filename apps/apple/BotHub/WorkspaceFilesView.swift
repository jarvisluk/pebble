import QuickLook
import SwiftUI

struct WorkspaceFilesView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var botId: String
    var initialPath: String? = nil
    @Binding var showHidden: Bool
    var refreshTrigger: Int = 0
    var onQuotePath: (String) -> Void

    @State private var dirPath = ""
    @State private var listResult: WorkspaceListResult?
    @State private var listLoading = false
    @State private var listErr = ""
    @State private var listSeq = 0

    @State private var selectedPath: String?
    @State private var preview: WorkspaceFileContent?
    @State private var previewLoading = false
    @State private var previewErr = ""
    @State private var previewSeq = 0
    @State private var imageURL: URL?
    @State private var quickLookURL: URL?
    @State private var downloading = false
    @State private var toastMsg = ""

    private var segments: [String] {
        dirPath.split(separator: "/").map(String.init)
    }

    private var botCollab: (bg: Color, ink: Color) {
        let idx = max(0, hub.bots.firstIndex { $0.id == botId } ?? 0)
        let botObj = hub.bots.first { $0.id == botId }
        return p.botCollab(botId, idx, custom: botObj?.color)
    }

    var body: some View {
        GeometryReader { geo in
            let isSplit = geo.size.width >= 680
            Group {
                if isSplit {
                    HStack(spacing: 12) {
                        listPanel(isSplit: true)
                            .frame(width: 296)
                            .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                            .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))

                        previewPanel(isSplit: true)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .background(p.card, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                            .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                    }
                    .padding(12)
                } else {
                    if let path = selectedPath {
                        previewContent(path: path, isSplit: false)
                            .background(p.page)
                    } else {
                        listPanel(isSplit: false)
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(p.page)
        }
        .task(id: "\(botId)-\(initialPath ?? "")") {
            selectedPath = nil
            preview = nil
            imageURL = nil
            if let initP = initialPath, !initP.isEmpty {
                let parent = (initP as NSString).deletingLastPathComponent
                await loadDir(parent == "." ? "" : parent)
                await openFile(initP)
            } else {
                await loadDir("")
            }
        }
        .onChange(of: showHidden) { _, _ in
            Task { await loadDir(dirPath) }
        }
        .onChange(of: refreshTrigger) { _, _ in
            Task { await loadDir(dirPath) }
        }
        .quickLookPreview($quickLookURL)
    }

    @ViewBuilder
    private func listPanel(isSplit: Bool) -> some View {
        VStack(spacing: 0) {
            if !dirPath.isEmpty {
                inlineBreadcrumbs
            }

            if listLoading && listResult == nil {
                Spacer()
                ProgressView()
                Spacer()
            } else if !listErr.isEmpty && listResult == nil {
                ContentUnavailableView {
                    Label(hub.t("workspaceFiles"), systemImage: "exclamationmark.triangle")
                } description: {
                    Text(listErr)
                } actions: {
                    Button(hub.t("retry")) {
                        Task { await loadDir(dirPath) }
                    }
                    .buttonStyle(.borderedProminent)
                }
            } else if let entries = listResult?.entries, entries.isEmpty, dirPath.isEmpty {
                ContentUnavailableView(
                    hub.t("emptyFolder"),
                    systemImage: "folder"
                )
            } else {
                let entries = listResult?.entries ?? []
                ScrollView {
                    if entries.isEmpty && !dirPath.isEmpty {
                        VStack(spacing: 0) {
                            if !dirPath.isEmpty {
                                parentBackRow(isSplit: isSplit)
                            }
                            Text(hub.t("emptyFolder"))
                                .font(.system(size: 15))
                                .foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity, alignment: .center)
                                .padding(.vertical, 24)
                        }
                        .padding(isSplit ? 8 : 0)
                        .background(isSplit ? Color.clear : p.card, in: RoundedRectangle(cornerRadius: isSplit ? 0 : 18, style: .continuous))
                        .padding(isSplit ? 0 : 12)
                    } else {
                        LazyVStack(spacing: 0) {
                            if !dirPath.isEmpty {
                                parentBackRow(isSplit: isSplit)
                                rowSeparator(inset: 14)
                            }

                            ForEach(Array(entries.enumerated()), id: \.element.id) { idx, ent in
                                let isSelected = isSplit && !ent.isDir && selectedPath == ent.path
                                let prevSelected = idx > 0 && isSplit && !entries[idx - 1].isDir && selectedPath == entries[idx - 1].path
                                if idx > 0 && !isSelected && !prevSelected {
                                    rowSeparator(inset: isSplit ? 16 : 14)
                                }

                                Button {
                                    if ent.isDir {
                                        Task { await loadDir(ent.path) }
                                    } else {
                                        Task { await openFile(ent.path) }
                                    }
                                } label: {
                                    fileRow(ent, isSelected: isSelected, isSplit: isSplit)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.vertical, isSplit ? 8 : 2)
                        .background(isSplit ? Color.clear : p.card, in: RoundedRectangle(cornerRadius: isSplit ? 0 : 18, style: .continuous))
                        .padding(isSplit ? 0 : 12)
                    }
                }
                .refreshable {
                    await loadDir(dirPath)
                }
            }
        }
    }

    private func parentBackRow(isSplit: Bool) -> some View {
        Button {
            let parent = listResult?.parent ?? ""
            Task { await loadDir(parent) }
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "arrow.uturn.backward")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(p.text2)
                    .frame(width: 24)
                Text("..")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(p.text)
                Spacer()
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.horizontal, isSplit ? 6 : 0)
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
                        .font(.system(size: 13, weight: .medium))
                        .foregroundStyle(.secondary)
                }
                .buttonStyle(.plain)

                ForEach(Array(segments.enumerated()), id: \.offset) { idx, seg in
                    Image(systemName: "chevron.right")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(.tertiary)
                    let target = segments.prefix(idx + 1).joined(separator: "/")
                    Button(seg) {
                        Task { await loadDir(target) }
                    }
                    .buttonStyle(.plain)
                    .font(.system(size: 13, weight: idx == segments.count - 1 ? .semibold : .medium))
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

    private func fileRow(_ ent: WorkspaceFileEntry, isSelected: Bool, isSplit: Bool) -> some View {
        let meta = fileSymbol(for: ent)
        return HStack(spacing: 12) {
            Image(systemName: meta.symbol)
                .font(.system(size: 17, weight: .medium))
                .foregroundStyle(meta.color)
                .frame(width: 24)

            VStack(alignment: .leading, spacing: 2) {
                Text(ent.name)
                    .font(.system(size: 16, weight: isSelected ? .semibold : .medium))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                if ent.isShared == true || !ent.isDir {
                    HStack(spacing: 6) {
                        if ent.isShared == true {
                            Text(hub.t("sharedBadge"))
                                .font(.system(size: 11, weight: .semibold))
                                .padding(.horizontal, 6)
                                .padding(.vertical, 1)
                                .background(botCollab.bg, in: Capsule())
                                .foregroundStyle(botCollab.ink)
                        }
                        if !ent.isDir {
                            Text(formatByteSize(ent.size))
                                .font(.system(size: 13))
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            Spacer(minLength: 4)
            if ent.isDir {
                Image(systemName: "chevron.right")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(
            RoundedRectangle(cornerRadius: isSplit ? 12 : 0, style: .continuous)
                .fill(isSelected ? p.sel : Color.clear)
        )
        .padding(.horizontal, isSplit ? 8 : 2)
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

    @ViewBuilder
    private func previewPanel(isSplit: Bool) -> some View {
        if let path = selectedPath {
            previewContent(path: path, isSplit: isSplit)
        } else {
            ContentUnavailableView(
                hub.t("workspaceFiles"),
                systemImage: "doc.text",
                description: Text(hub.t("selectFileHint"))
            )
        }
    }

    private func previewContent(path: String, isSplit: Bool) -> some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                if !isSplit {
                    Button {
                        selectedPath = nil
                        preview = nil
                        imageURL = nil
                    } label: {
                        Label(hub.t("back"), systemImage: "chevron.backward")
                            .font(.system(size: 15, weight: .semibold))
                    }
                }

                VStack(alignment: .leading, spacing: 1) {
                    Text((path as NSString).lastPathComponent)
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(p.text)
                        .lineLimit(1)
                    if path.contains("/") {
                        Text(path)
                            .font(.system(size: 12, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                }

                Spacer(minLength: 8)

                Button {
                    onQuotePath(path)
                } label: {
                    if isSplit {
                        Label(hub.t("insertToChat"), systemImage: "quote.bubble")
                            .font(.system(size: 13, weight: .semibold))
                            .lineLimit(1)
                    } else {
                        Image(systemName: "quote.bubble")
                            .font(.system(size: 14, weight: .semibold))
                    }
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .fixedSize()
                .accessibilityLabel(hub.t("insertToChat"))

                if downloading {
                    ProgressView().controlSize(.small)
                } else {
                    Button {
                        Task { await downloadAndPreview(path: path, name: preview?.name ?? (path as NSString).lastPathComponent) }
                    } label: {
                        Image(systemName: "square.and.arrow.up")
                            .font(.system(size: 15, weight: .semibold))
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .background(isSplit ? Color.clear : p.card)
            .overlay(alignment: .bottom) {
                Rectangle().fill(p.s2).frame(height: 0.5)
            }

            if !toastMsg.isEmpty {
                Text(toastMsg)
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
                    .padding(.top, 8)
            }

            if previewLoading {
                Spacer()
                ProgressView()
                Spacer()
            } else if !previewErr.isEmpty {
                ContentUnavailableView {
                    Label(hub.t("workspaceFiles"), systemImage: "exclamationmark.triangle")
                } description: {
                    Text(previewErr)
                } actions: {
                    Button(hub.t("retry")) {
                        Task { await openFile(path) }
                    }
                    .buttonStyle(.borderedProminent)
                }
            } else if let doc = preview {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        if doc.truncated == true {
                            Text(hub.t("fileTruncatedHint", ["size": formatByteSize(Int64(doc.content?.utf8.count ?? 0))]))
                                .font(.system(size: 13))
                                .padding(10)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(p.s1, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                        }

                        if doc.kind == "image", let url = imageURL {
                            AsyncImage(url: url) { phase in
                                switch phase {
                                case let .success(img):
                                    img.resizable().scaledToFit()
                                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                                case .failure:
                                    Text(hub.t("downloadFailed")).foregroundStyle(p.pinkInk)
                                default:
                                    ProgressView().frame(maxWidth: .infinity, minHeight: 160)
                                }
                            }
                        } else if doc.kind == "markdown" {
                            MarkdownText(text: doc.content ?? "")
                        } else if doc.kind == "text" {
                            Text(doc.content ?? "")
                                .font(.system(size: 14, design: .monospaced))
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        } else {
                            VStack(spacing: 12) {
                                Image(systemName: "doc.zipper")
                                    .font(.system(size: 36))
                                    .foregroundStyle(.secondary)
                                Text(doc.name)
                                    .font(.system(size: 17, weight: .bold))
                                Text("\(doc.mimeType) · \(formatByteSize(doc.size))")
                                    .font(.system(size: 13, design: .monospaced))
                                    .foregroundStyle(.secondary)
                                Text(hub.t("binaryFileHint"))
                                    .font(.system(size: 14))
                                    .foregroundStyle(.secondary)
                                    .multilineTextAlignment(.center)
                                Button(hub.t("openQuickLook")) {
                                    Task { await downloadAndPreview(path: doc.path, name: doc.name) }
                                }
                                .buttonStyle(.borderedProminent)
                            }
                            .frame(maxWidth: .infinity)
                            .padding(.vertical, 32)
                        }
                    }
                    .padding(16)
                }
            }
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

    private func openFile(_ path: String) async {
        guard !botId.isEmpty else { return }
        previewSeq += 1
        let seq = previewSeq
        selectedPath = path
        previewLoading = true
        previewErr = ""
        imageURL = nil
        do {
            let doc = try await hub.readWorkspaceFile(botId: botId, path: path)
            guard seq == previewSeq else { return }
            preview = doc
            if doc.kind == "image" {
                let url = try await hub.workspaceFileImageURL(botId: botId, path: path)
                guard seq == previewSeq else { return }
                imageURL = url
            }
        } catch {
            guard seq == previewSeq else { return }
            preview = nil
            previewErr = error.localizedDescription
        }
        if seq == previewSeq {
            previewLoading = false
        }
    }

    private func downloadAndPreview(path: String, name: String) async {
        downloading = true
        defer { downloading = false }
        do {
            quickLookURL = try await hub.downloadWorkspaceFileToTemp(botId: botId, path: path, fileName: name)
        } catch {
            toastMsg = error.localizedDescription
        }
    }

    private func formatByteSize(_ bytes: Int64) -> String {
        if bytes < 1024 { return "\(bytes) B" }
        let kb = Double(bytes) / 1024
        if kb < 1024 { return String(format: "%.1f KB", kb) }
        return String(format: "%.1f MB", kb / 1024)
    }
}
