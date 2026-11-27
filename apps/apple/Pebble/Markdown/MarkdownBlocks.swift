import SwiftUI

struct DetailsBlockView<Content: View>: View {
    @Environment(\.palette) private var p
    var summary: String
    @State private var expanded: Bool
    @ViewBuilder var content: () -> Content

    init(summary: String, initiallyOpen: Bool, @ViewBuilder content: @escaping () -> Content) {
        self.summary = summary
        _expanded = State(initialValue: initiallyOpen)
        self.content = content
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.easeOut(duration: 0.15)) { expanded.toggle() }
            } label: {
                HStack(spacing: 6) {
                    Image(systemName: expanded ? "chevron.down" : "chevron.right")
                        .font(.system(size: AppFont.caption, weight: .semibold))
                        .foregroundStyle(p.mute)
                    Text(summary)
                        .font(.system(size: AppFont.body, weight: .semibold))
                        .foregroundStyle(p.text)
                    Spacer(minLength: 0)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if expanded {
                Rectangle().fill(p.s2).frame(height: 1).padding(.vertical, 8)
                content()
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .background(p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}

struct TableBlockView<Cell: View>: View {
    @Environment(\.palette) private var p
    var headers: [String]
    var aligns: [MarkdownText.TableAlign]
    var rows: [[String]]
    var inlineBuilder: (String, Bool) -> Cell

    var body: some View {
        let colCount = max(headers.count, aligns.count)
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
                GridRow {
                    ForEach(0..<colCount, id: \.self) { col in
                        let align = col < aligns.count ? aligns[col] : .left
                        inlineBuilder(col < headers.count ? headers[col] : "", true)
                            .multilineTextAlignment(align.swiftUI)
                            .frame(maxWidth: 260, alignment: align.frameAlign)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 7)
                            .background(p.s1)
                            .overlay(alignment: .trailing) {
                                if col < colCount - 1 {
                                    Rectangle().fill(p.s2).frame(width: 1)
                                }
                            }
                    }
                }
                Rectangle().fill(p.s2).frame(height: 1)
                ForEach(Array(rows.enumerated()), id: \.offset) { rIdx, row in
                    GridRow {
                        ForEach(0..<colCount, id: \.self) { col in
                            let align = col < aligns.count ? aligns[col] : .left
                            inlineBuilder(col < row.count ? row[col] : "", false)
                                .multilineTextAlignment(align.swiftUI)
                                .frame(maxWidth: 260, alignment: align.frameAlign)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 7)
                                .overlay(alignment: .trailing) {
                                    if col < colCount - 1 {
                                        Rectangle().fill(p.s2).frame(width: 1)
                                    }
                                }
                        }
                    }
                    if rIdx < rows.count - 1 {
                        Rectangle().fill(p.s2).frame(height: 1)
                    }
                }
            }
            .overlay(RoundedRectangle(cornerRadius: 12, style: .continuous).stroke(p.s2, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        }
    }
}

struct MarkdownImageView: View {
    @Environment(\.palette) private var p
    var alt: String
    var urlString: String

    var body: some View {
        let lower = urlString.lowercased()
        if (lower.hasPrefix("http://") || lower.hasPrefix("https://")), let url = URL(string: urlString) {
            AsyncImage(url: url) { phase in
                switch phase {
                case let .success(image):
                    image
                        .resizable()
                        .scaledToFit()
                        .frame(maxHeight: 280)
                        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                case .failure:
                    fallbackChip(link: url)
                default:
                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                        .fill(p.s1)
                        .frame(width: 180, height: 110)
                        .overlay(ProgressView())
                }
            }
        } else {
            fallbackChip(link: nil)
        }
    }

    @ViewBuilder
    private func fallbackChip(link: URL?) -> some View {
        let label = alt.isEmpty ? L.t("mdImageFallback") : L.t("mdImageFallbackAlt", ["alt": alt])
        if let link {
            Link(label, destination: link)
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(p.accent)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        } else {
            Text(label)
                .font(.system(size: AppFont.secondary))
                .foregroundStyle(p.text2)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(p.s1, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        }
    }
}

struct CodeCard: View {
    @Environment(\.palette) private var p
    var lang: String
    var code: String
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(lang.isEmpty ? "code" : lang).font(.system(size: AppFont.caption)).foregroundStyle(p.mute)
                Spacer()
                Button(copied ? L.t("copied") : L.t("copy")) {
                    copyToClipboard(code)
                    copied = true
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { copied = false }
                }
                .font(.system(size: AppFont.caption, weight: .semibold))
                .foregroundStyle(p.text2)
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 12)
            .padding(.top, 8)
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(size: AppFont.secondary, design: .monospaced))
                    .foregroundStyle(p.text)
                    .padding(12)
            }
        }
        .background(p.s2.opacity(p.dark ? 0.6 : 0.7), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}
