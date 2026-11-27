import QuickLook
import SwiftUI

struct StageFilePreviewView: View {
    @Environment(HubStore.self) private var hub
    @Environment(\.palette) private var p

    var botId: String
    var filePath: String
    var inWideStage: Bool = false
    var onQuotePath: (String) -> Void

    @State private var preview: WorkspaceFileContent?
    @State private var loading = false
    @State private var err = ""
    @State private var seq = 0
    @State private var imageURL: URL?
    @State private var quickLookURL: URL?
    @State private var downloading = false
    @State private var toastMsg = ""

    var body: some View {
        let cardContent = VStack(spacing: 0) {
            HStack(spacing: 10) {
                VStack(alignment: .leading, spacing: 1) {
                    Text((filePath as NSString).lastPathComponent)
                        .font(.system(size: AppFont.cardTitle, weight: .semibold))
                        .foregroundStyle(p.text)
                        .lineLimit(1)
                    if filePath.contains("/") {
                        Text(filePath)
                            .font(.system(size: AppFont.caption, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .truncationMode(.middle)
                    }
                }

                Spacer(minLength: 8)

                Button {
                    onQuotePath(filePath)
                } label: {
                    if inWideStage {
                        Label(hub.t("insertToChat"), systemImage: "quote.bubble")
                            .font(.system(size: AppFont.secondary, weight: .semibold))
                            .lineLimit(1)
                    } else {
                        Image(systemName: "quote.bubble")
                            .font(.system(size: AppFont.body, weight: .semibold))
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
                        Task { await downloadAndPreview() }
                    } label: {
                        Image(systemName: "square.and.arrow.up")
                            .font(.system(size: AppFont.body, weight: .semibold))
                    }
                    .buttonStyle(.bordered)
                    .controlSize(.small)
                }

                Button {
                    Task { await loadFile() }
                } label: {
                    Image(systemName: "arrow.clockwise")
                        .font(.system(size: AppFont.body, weight: .semibold))
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
                .accessibilityLabel(hub.t("refreshFiles"))
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 12)
            .overlay(alignment: .bottom) {
                Rectangle().fill(p.s2).frame(height: 0.5)
            }

            if !toastMsg.isEmpty {
                Text(toastMsg)
                    .font(.system(size: AppFont.secondary))
                    .foregroundStyle(.secondary)
                    .padding(.top, 8)
            }

            if loading {
                Spacer()
                ProgressView()
                Spacer()
            } else if !err.isEmpty {
                VStack(spacing: 10) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.system(size: AppFont.hero))
                        .foregroundStyle(p.mute)
                    Text(hub.t("workspaceFiles"))
                        .font(.system(size: AppFont.title, weight: .semibold))
                        .foregroundStyle(p.text)
                    Text(err)
                        .font(.system(size: AppFont.secondary))
                        .foregroundStyle(p.text2)
                        .multilineTextAlignment(.center)
                    Button(hub.t("retry")) {
                        Task { await loadFile() }
                    }
                    .buttonStyle(.borderedProminent)
                }
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let doc = preview {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        if doc.truncated == true {
                            Text(hub.t("fileTruncatedHint", ["size": formatByteSize(Int64(doc.content?.utf8.count ?? 0))]))
                                .font(.system(size: AppFont.secondary))
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
                                .font(.system(size: AppFont.secondary, design: .monospaced))
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        } else {
                            VStack(spacing: 12) {
                                Image(systemName: "doc.zipper")
                                    .font(.system(size: AppFont.hero))
                                    .foregroundStyle(.secondary)
                                Text(doc.name)
                                    .font(.system(size: AppFont.title, weight: .bold))
                                Text("\(doc.mimeType) · \(formatByteSize(doc.size))")
                                    .font(.system(size: AppFont.caption, design: .monospaced))
                                    .foregroundStyle(.secondary)
                                Text(hub.t("binaryFileHint"))
                                    .font(.system(size: AppFont.secondary))
                                    .foregroundStyle(.secondary)
                                    .multilineTextAlignment(.center)
                                Button(hub.t("openQuickLook")) {
                                    Task { await downloadAndPreview() }
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

        cardContent
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(inWideStage ? p.card : p.page)
            .task(id: "\(botId)|\(filePath)") {
            await loadFile()
        }
        .quickLookPreview($quickLookURL)
    }

    private func loadFile() async {
        guard !botId.isEmpty, !filePath.isEmpty else { return }
        seq += 1
        let cur = seq
        loading = true
        err = ""
        imageURL = nil
        do {
            let doc = try await hub.readWorkspaceFile(botId: botId, path: filePath)
            guard cur == seq else { return }
            preview = doc
            if doc.kind == "image" {
                let url = try await hub.workspaceFileImageURL(botId: botId, path: filePath)
                guard cur == seq else { return }
                imageURL = url
            }
        } catch {
            guard cur == seq else { return }
            preview = nil
            err = error.localizedDescription
        }
        if cur == seq {
            loading = false
        }
    }

    private func downloadAndPreview() async {
        downloading = true
        defer { downloading = false }
        let name = preview?.name ?? (filePath as NSString).lastPathComponent
        do {
            quickLookURL = try await hub.downloadWorkspaceFileToTemp(botId: botId, path: filePath, fileName: name)
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
