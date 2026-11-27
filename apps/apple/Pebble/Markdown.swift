import SwiftUI

/// 完整 Markdown / GFM 渲染：代码块、LaTeX 公式、标题 H1–H6、分隔线、表格、嵌套引用、列表与任务列表、折叠块、脚注、图片与单次扫描行内样式。
struct MarkdownText: View {
    @Environment(\.palette) private var p
    var text: String
    var color: Color?

    enum TableAlign: Hashable {
        case left, center, right
        var swiftUI: TextAlignment {
            switch self {
            case .left: return .leading
            case .center: return .center
            case .right: return .trailing
            }
        }
        var frameAlign: Alignment {
            switch self {
            case .left: return .leading
            case .center: return .center
            case .right: return .trailing
            }
        }
    }

    struct ListItem: Hashable {
        let marker: String
        let task: Bool
        let checked: Bool
        let indent: Int
        let text: String
    }

    indirect enum Block: Hashable {
        case code(lang: String, body: String)
        case math(String)
        case details(summary: String, isOpen: Bool, body: [Block])
        case heading(level: Int, text: String)
        case rule
        case quote(level: Int, text: String)
        case table(headers: [String], aligns: [TableAlign], rows: [[String]])
        case list(items: [ListItem])
        case footnote(id: String, text: String)
        case image(alt: String, url: String)
        case para(String)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(Self.parse(text).enumerated()), id: \.offset) { _, b in
                block(b)
            }
        }
        .textSelection(.enabled)
    }

    @ViewBuilder
    private func block(_ b: Block) -> some View {
        let fg = color ?? p.text
        switch b {
        case let .code(lang, body):
            CodeCard(lang: lang, code: body)
        case let .math(expr):
            MathBlockView(expr: expr, color: fg)
        case let .details(summary, isOpen, inner):
            DetailsBlockView(summary: summary, initiallyOpen: isOpen) {
                VStack(alignment: .leading, spacing: 8) {
                    ForEach(Array(inner.enumerated()), id: \.offset) { _, sub in
                        AnyView(block(sub))
                    }
                }
            }
        case let .heading(level, t):
            let sz: CGFloat = level == 1 ? AppFont.hero : level == 2 ? AppFont.title : level == 3 ? AppFont.cardTitle : AppFont.body
            inline(t, baseColor: level >= 5 ? p.text2 : fg)
                .font(.system(size: sz, weight: .bold))
                .foregroundStyle(level >= 5 ? p.text2 : fg)
        case .rule:
            Rectangle().fill(p.s3).frame(height: 1).padding(.vertical, 4)
        case let .quote(level, t):
            HStack(alignment: .top, spacing: 8) {
                ForEach(0..<max(1, min(level, 4)), id: \.self) { _ in
                    RoundedRectangle(cornerRadius: 2).fill(p.s3).frame(width: 3)
                }
                inline(t, baseColor: p.text2)
                    .font(.system(size: AppFont.body))
                    .foregroundStyle(p.text2)
                    .lineSpacing(4)
            }
            .fixedSize(horizontal: false, vertical: true)
        case let .table(headers, aligns, rows):
            TableBlockView(headers: headers, aligns: aligns, rows: rows, inlineBuilder: { cellText, bold in
                inline(cellText, baseColor: fg)
                    .font(.system(size: AppFont.secondary, weight: bold ? .semibold : .regular))
                    .foregroundStyle(fg)
            })
        case let .list(items):
            VStack(alignment: .leading, spacing: 5) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        if item.task {
                            Image(systemName: item.checked ? "checkmark.square.fill" : "square")
                                .font(.system(size: AppFont.secondary))
                                .foregroundStyle(item.checked ? p.accent : p.mute)
                        } else {
                            Text(item.marker)
                                .foregroundStyle(p.mute)
                                .monospacedDigit()
                        }
                        inline(item.text, baseColor: item.task && item.checked ? p.text2 : fg)
                            .foregroundStyle(item.task && item.checked ? p.text2 : fg)
                            .strikethrough(item.task && item.checked)
                    }
                    .font(.system(size: AppFont.body))
                    .padding(.leading, CGFloat(item.indent) * 16)
                }
            }
        case let .footnote(id, t):
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text("[\(id)]")
                    .font(.system(size: AppFont.caption, weight: .semibold))
                    .foregroundStyle(p.mute)
                inline(t, baseColor: p.text2)
                    .font(.system(size: AppFont.caption))
                    .foregroundStyle(p.text2)
            }
        case let .image(alt, urlStr):
            MarkdownImageView(alt: alt, urlString: urlStr)
        case let .para(t):
            inline(t, baseColor: fg)
                .font(.system(size: AppFont.body))
                .foregroundStyle(fg)
                .lineSpacing(4)
        }
    }

    static func compileRegex(_ pattern: String, options: NSRegularExpression.Options = []) -> NSRegularExpression {
        try! NSRegularExpression(pattern: pattern, options: options)
    }

    static func firstRange(_ rx: NSRegularExpression, in s: String, anchored: Bool = false) -> Range<String.Index>? {
        let nsRange = NSRange(s.startIndex..., in: s)
        let opts: NSRegularExpression.MatchingOptions = anchored ? [.anchored] : []
        guard let m = rx.firstMatch(in: s, options: opts, range: nsRange) else { return nil }
        return Range(m.range, in: s)
    }

    static func hasMatch(_ rx: NSRegularExpression, in s: String) -> Bool {
        firstRange(rx, in: s) != nil
    }

    private func inline(_ s: String, baseColor: Color) -> Text {
        Text(Self.buildInlineAttributed(s, palette: p, baseColor: baseColor))
    }
}
