import SwiftUI

/// 轻量 Markdown：代码块、标题、分隔线、引用、列表；行内交给 AttributedString。
struct MarkdownText: View {
    @Environment(\.palette) private var p
    var text: String
    var color: Color?

    enum Block: Hashable {
        case code(lang: String, body: String)
        case heading(level: Int, text: String)
        case rule
        case quote(String)
        case list(ordered: Bool, items: [String])
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
        case let .heading(level, t):
            inline(t).font(.system(size: level == 1 ? 17 : 15, weight: .bold)).foregroundStyle(fg)
        case .rule:
            Rectangle().fill(p.s3).frame(height: 1).padding(.vertical, 4)
        case let .quote(t):
            HStack(spacing: 10) {
                RoundedRectangle(cornerRadius: 2).fill(p.s3).frame(width: 3)
                inline(t).font(.system(size: 14)).foregroundStyle(p.text2)
            }
            .fixedSize(horizontal: false, vertical: true)
        case let .list(ordered, items):
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(items.enumerated()), id: \.offset) { i, item in
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(ordered ? "\(i + 1)." : "•").foregroundStyle(p.text2)
                        inline(item).foregroundStyle(fg)
                    }
                    .font(.system(size: 14))
                }
            }
        case let .para(t):
            inline(t).font(.system(size: 14)).foregroundStyle(fg).lineSpacing(4)
        }
    }

    private func inline(_ s: String) -> Text {
        let linked = s.replacingOccurrences(of: "(?<![(<\\]])(https?://[^\\s)<>]+)", with: "<$1>", options: .regularExpression)
        if var a = try? AttributedString(markdown: linked, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)) {
            for run in a.runs where run.inlinePresentationIntent?.contains(.code) == true {
                a[run.range].font = .system(size: 13, design: .monospaced)
                a[run.range].backgroundColor = p.s2
            }
            for run in a.runs where run.link != nil {
                a[run.range].foregroundColor = p.accent
            }
            return Text(a)
        }
        return Text(s)
    }

    static func parse(_ src: String) -> [Block] {
        var out: [Block] = []
        let lines = src.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
        var i = 0
        var para: [String] = []
        func flush() {
            if !para.isEmpty { out.append(.para(para.joined(separator: "\n"))); para = [] }
        }
        while i < lines.count {
            let line = lines[i]
            let t = line.trimmingCharacters(in: .whitespaces)
            if t.hasPrefix("```") {
                flush()
                let lang = String(t.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                var body: [String] = []
                i += 1
                while i < lines.count, !lines[i].trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                    body.append(lines[i]); i += 1
                }
                out.append(.code(lang: lang, body: body.joined(separator: "\n")))
                i += 1
                continue
            }
            if t.isEmpty { flush(); i += 1; continue }
            if let m = t.range(of: "^#{1,4}\\s+", options: .regularExpression) {
                flush()
                let level = t[m].filter { $0 == "#" }.count
                out.append(.heading(level: level, text: String(t[m.upperBound...])))
                i += 1; continue
            }
            if t.range(of: "^(-{3,}|\\*{3,}|_{3,})$", options: .regularExpression) != nil {
                flush(); out.append(.rule); i += 1; continue
            }
            if t.hasPrefix(">") {
                flush()
                var q: [String] = []
                while i < lines.count, lines[i].trimmingCharacters(in: .whitespaces).hasPrefix(">") {
                    q.append(String(lines[i].trimmingCharacters(in: .whitespaces).dropFirst()).trimmingCharacters(in: .whitespaces))
                    i += 1
                }
                out.append(.quote(q.joined(separator: "\n")))
                continue
            }
            let ul = "^[-*+]\\s+", ol = "^\\d+[.)]\\s+"
            if t.range(of: ul, options: .regularExpression) != nil || t.range(of: ol, options: .regularExpression) != nil {
                flush()
                let ordered = t.range(of: ol, options: .regularExpression) != nil
                let pat = ordered ? ol : ul
                var items: [String] = []
                while i < lines.count {
                    let lt = lines[i].trimmingCharacters(in: .whitespaces)
                    guard let r = lt.range(of: pat, options: .regularExpression) else { break }
                    items.append(String(lt[r.upperBound...]))
                    i += 1
                }
                out.append(.list(ordered: ordered, items: items))
                continue
            }
            para.append(line)
            i += 1
        }
        flush()
        return out
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
                Text(lang.isEmpty ? "code" : lang).font(.system(size: 12)).foregroundStyle(p.mute)
                Spacer()
                Button(copied ? L.t("copied") : L.t("copy")) {
                    copyToClipboard(code)
                    copied = true
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { copied = false }
                }
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(p.text2)
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 12)
            .padding(.top, 8)
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(size: 13, design: .monospaced))
                    .foregroundStyle(p.text)
                    .padding(12)
            }
        }
        .background(p.s2.opacity(p.dark ? 0.6 : 0.7), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}
