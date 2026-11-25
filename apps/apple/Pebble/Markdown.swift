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

    private final class ParsedBlocksBox {
        let blocks: [Block]
        init(_ blocks: [Block]) { self.blocks = blocks }
    }

    private final class InlineAttrBox {
        let attr: AttributedString
        init(_ attr: AttributedString) { self.attr = attr }
    }

    private static let parsedCache: NSCache<NSString, ParsedBlocksBox> = {
        let c = NSCache<NSString, ParsedBlocksBox>()
        c.countLimit = 256
        return c
    }()

    private static let inlineCache: NSCache<NSString, InlineAttrBox> = {
        let c = NSCache<NSString, InlineAttrBox>()
        c.countLimit = 512
        return c
    }()

    private static func compileRegex(_ pattern: String, options: NSRegularExpression.Options = []) -> NSRegularExpression {
        try! NSRegularExpression(pattern: pattern, options: options)
    }

    private static let rxDetailsOpen = compileRegex("^<details(\\s+open)?\\s*>", options: [.caseInsensitive])
    private static let rxDetailsClose = compileRegex("</details>", options: [.caseInsensitive])
    private static let rxSummary = compileRegex("<summary>([\\s\\S]*?)</summary>", options: [.caseInsensitive])
    private static let rxSummaryOpen = compileRegex("^<summary>", options: [.caseInsensitive])
    private static let rxSummaryClose = compileRegex("</summary>$", options: [.caseInsensitive])
    private static let rxHeading = compileRegex("^#{1,6}\\s+")
    private static let rxRule = compileRegex("^(?:-\\s*){3,}$|^(?:\\*\\s*){3,}$|^(?:_\\s*){3,}$")
    private static let rxTableAlign = compileRegex("^:?-{3,}:?$")
    private static let rxListUl = compileRegex("^[-*+]\\s+")
    private static let rxListOl = compileRegex("^(\\d+)[.)]\\s+")
    private static let rxListTask = compileRegex("^\\[[xX ]\\]\\s+")
    private static let rxFootnoteDef = compileRegex("^\\[\\^([^\\]]+)\\]:\\s*")
    private static let rxSafeTag = compileRegex("^<(sup|sub|u|s|del|b|strong|i|em|code|kbd|mark)>([\\s\\S]*?)</\\1>", options: [.caseInsensitive])
    private static let rxBrTag = compileRegex("^<br\\s*/?>", options: [.caseInsensitive])
    private static let rxAutolink = compileRegex("^<(https?://[^>\\s]+|mailto:[^>\\s]+)>", options: [.caseInsensitive])
    private static let rxFootnoteRef = compileRegex("^\\[\\^([^\\]]+)\\](?!:)")
    private static let rxNumericOnly = compileRegex("^\\d+(?:[.,]\\d+)?$")
    private static let rxBareURL = compileRegex("^https?://[^\\s<]+")

    private static func firstRange(_ rx: NSRegularExpression, in s: String, anchored: Bool = false) -> Range<String.Index>? {
        let nsRange = NSRange(s.startIndex..., in: s)
        let opts: NSRegularExpression.MatchingOptions = anchored ? [.anchored] : []
        guard let m = rx.firstMatch(in: s, options: opts, range: nsRange) else { return nil }
        return Range(m.range, in: s)
    }

    private static func hasMatch(_ rx: NSRegularExpression, in s: String) -> Bool {
        firstRange(rx, in: s) != nil
    }

    private func inline(_ s: String, baseColor: Color) -> Text {
        Text(Self.buildInlineAttributed(s, palette: p, baseColor: baseColor))
    }

    // MARK: - Block Parser

    static func parse(_ src: String) -> [Block] {
        let cacheKey = src as NSString
        if let cached = parsedCache.object(forKey: cacheKey) {
            return cached.blocks
        }
        var out: [Block] = []
        let lines = src.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")
        var i = 0
        var para: [String] = []
        var list: [ListItem] = []

        func flushPara() {
            if !para.isEmpty {
                out.append(.para(para.joined(separator: "\n")))
                para = []
            }
        }
        func flushList() {
            if !list.isEmpty {
                out.append(.list(items: list))
                list = []
            }
        }

        while i < lines.count {
            let line = lines[i]
            let t = line.trimmingCharacters(in: .whitespaces)

            // 1. Fenced code block (``` or ~~~)
            if let fence = matchFenceStart(t) {
                flushPara()
                flushList()
                var body: [String] = []
                i += 1
                while i < lines.count {
                    let ct = lines[i].trimmingCharacters(in: .whitespaces)
                    if isFenceClose(ct, char: fence.char, minLen: fence.len) {
                        i += 1
                        break
                    }
                    body.append(lines[i])
                    i += 1
                }
                out.append(.code(lang: fence.lang, body: body.joined(separator: "\n")))
                continue
            }

            // 2. Block math $$...$$
            if t.hasPrefix("$$") {
                flushPara()
                flushList()
                if t.count > 4, t.hasSuffix("$$") {
                    let inner = String(t.dropFirst(2).dropLast(2)).trimmingCharacters(in: .whitespaces)
                    out.append(.math(inner))
                    i += 1
                    continue
                }
                var exprLines: [String] = []
                let firstRest = String(t.dropFirst(2)).trimmingCharacters(in: .whitespaces)
                if !firstRest.isEmpty { exprLines.append(firstRest) }
                i += 1
                while i < lines.count {
                    let ct = lines[i].trimmingCharacters(in: .whitespaces)
                    if ct.hasSuffix("$$") {
                        let tail = String(ct.dropLast(2)).trimmingCharacters(in: .whitespaces)
                        if !tail.isEmpty { exprLines.append(tail) }
                        i += 1
                        break
                    }
                    exprLines.append(lines[i])
                    i += 1
                }
                out.append(.math(exprLines.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)))
                continue
            }

            // 3. <details>...</details> collapsible block
            if let dm = firstRange(rxDetailsOpen, in: t) {
                var endLineIdx: Int? = nil
                for k in i..<lines.count {
                    if hasMatch(rxDetailsClose, in: lines[k]) {
                        endLineIdx = k
                        break
                    }
                }
                if let endIdx = endLineIdx {
                    flushPara()
                    flushList()
                    let isOpen = t[dm].lowercased().contains("open")
                    var collected: [String] = []
                    let afterOpen = String(t[dm.upperBound...])
                    if !afterOpen.isEmpty { collected.append(afterOpen) }
                    if endIdx > i {
                        for k in (i + 1)...endIdx { collected.append(lines[k]) }
                    }
                    var rawBlock = collected.joined(separator: "\n")
                    if let closeRange = rawBlock.range(of: "</details>", options: [.caseInsensitive, .backwards]) {
                        rawBlock.removeSubrange(closeRange.lowerBound..<rawBlock.endIndex)
                    }
                    rawBlock = rawBlock.trimmingCharacters(in: .whitespacesAndNewlines)
                    var summary = L.t("mdDetails")
                    var bodyText = rawBlock
                    if let sm = firstRange(rxSummary, in: rawBlock) {
                        var inner = String(rawBlock[sm])
                        if let openR = firstRange(rxSummaryOpen, in: inner) {
                            inner.removeSubrange(openR)
                        }
                        if let closeR = firstRange(rxSummaryClose, in: inner) {
                            inner.removeSubrange(closeR)
                        }
                        inner = inner.trimmingCharacters(in: .whitespacesAndNewlines)
                        if !inner.isEmpty { summary = inner }
                        bodyText.removeSubrange(sm)
                        bodyText = bodyText.trimmingCharacters(in: .whitespacesAndNewlines)
                    }
                    let innerBlocks = bodyText.isEmpty ? [] : parse(bodyText)
                    out.append(.details(summary: summary, isOpen: isOpen, body: innerBlocks))
                    i = endIdx + 1
                    continue
                }
            }

            // 4. Blank line
            if t.isEmpty {
                flushPara()
                flushList()
                i += 1
                continue
            }

            // 5. Headings H1–H6
            if let m = firstRange(rxHeading, in: t) {
                flushPara()
                flushList()
                let level = t[m].filter { $0 == "#" }.count
                out.append(.heading(level: level, text: String(t[m.upperBound...])))
                i += 1
                continue
            }

            // 6. Horizontal rule
            if hasMatch(rxRule, in: t) {
                flushPara()
                flushList()
                out.append(.rule)
                i += 1
                continue
            }

            // 7. GFM Table
            if t.contains("|"), i + 1 < lines.count, let aligns = parseTableAlignments(lines[i + 1]) {
                let headers = splitTableCells(line)
                if !headers.isEmpty {
                    flushPara()
                    flushList()
                    var rows: [[String]] = []
                    i += 2
                    while i < lines.count {
                        let rt = lines[i].trimmingCharacters(in: .whitespaces)
                        if rt.isEmpty || !rt.contains("|") || parseTableAlignments(lines[i]) != nil {
                            break
                        }
                        rows.append(splitTableCells(lines[i]))
                        i += 1
                    }
                    out.append(.table(headers: headers, aligns: aligns, rows: rows))
                    continue
                }
            }

            // 8. Blockquote (supports multiline + nested >>)
            if t.hasPrefix(">") {
                flushPara()
                flushList()
                var baseLevel = 0
                var qLines: [String] = []
                while i < lines.count, lines[i].trimmingCharacters(in: .whitespaces).hasPrefix(">") {
                    var lt = lines[i].trimmingCharacters(in: .whitespaces)
                    var lvl = 0
                    while lt.hasPrefix(">") {
                        lvl += 1
                        lt = String(lt.dropFirst()).trimmingCharacters(in: .whitespaces)
                    }
                    baseLevel = max(baseLevel, lvl)
                    qLines.append(lt)
                    i += 1
                }
                out.append(.quote(level: baseLevel, text: qLines.joined(separator: "\n")))
                continue
            }

            // 9. List (ordered, unordered, task, indented)
            if let item = parseListLine(line) {
                flushPara()
                list.append(item)
                i += 1
                continue
            }

            // 10. Footnote definition [^id]: text
            if let fnMatch = parseFootnoteDef(t) {
                flushPara()
                flushList()
                out.append(.footnote(id: fnMatch.id, text: fnMatch.text))
                i += 1
                continue
            }

            // 11. Standalone image line ![alt](url)
            if let img = parseStandaloneImage(t) {
                flushPara()
                flushList()
                out.append(.image(alt: img.alt, url: img.url))
                i += 1
                continue
            }

            flushList()
            para.append(line)
            i += 1
        }

        flushPara()
        flushList()
        parsedCache.setObject(ParsedBlocksBox(out), forKey: cacheKey)
        return out
    }

    private static func matchFenceStart(_ t: String) -> (char: Character, len: Int, lang: String)? {
        guard let first = t.first, first == "`" || first == "~" else { return nil }
        var len = 0
        for c in t {
            if c == first { len += 1 } else { break }
        }
        guard len >= 3 else { return nil }
        let rest = String(t.dropFirst(len)).trimmingCharacters(in: .whitespaces)
        return (first, len, rest)
    }

    private static func isFenceClose(_ t: String, char: Character, minLen: Int) -> Bool {
        guard !t.isEmpty else { return false }
        var len = 0
        for c in t {
            if c == char { len += 1 } else { return false }
        }
        return len >= minLen
    }

    private static func splitTableCells(_ line: String) -> [String] {
        var s = line.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("|") { s.removeFirst() }
        if s.hasSuffix("|") && !s.hasSuffix("\\|") { s.removeLast() }
        var cells: [String] = []
        var cur = ""
        let chars = Array(s)
        var i = 0
        while i < chars.count {
            if chars[i] == "\\", i + 1 < chars.count, chars[i + 1] == "|" {
                cur.append("|")
                i += 2
                continue
            }
            if chars[i] == "|" {
                cells.append(cur.trimmingCharacters(in: .whitespaces))
                cur = ""
            } else {
                cur.append(chars[i])
            }
            i += 1
        }
        cells.append(cur.trimmingCharacters(in: .whitespaces))
        return cells
    }

    private static func parseTableAlignments(_ line: String) -> [TableAlign]? {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard t.contains("|") else { return nil }
        let cells = splitTableCells(t)
        guard !cells.isEmpty else { return nil }
        var aligns: [TableAlign] = []
        for c in cells {
            guard hasMatch(rxTableAlign, in: c) else { return nil }
            if c.hasPrefix(":") && c.hasSuffix(":") { aligns.append(.center) }
            else if c.hasSuffix(":") { aligns.append(.right) }
            else { aligns.append(.left) }
        }
        return aligns
    }

    private static func parseListLine(_ line: String) -> ListItem? {
        var indentSpaces = 0
        var idx = line.startIndex
        while idx < line.endIndex {
            let ch = line[idx]
            if ch == " " { indentSpaces += 1 }
            else if ch == "\t" { indentSpaces += 2 }
            else { break }
            idx = line.index(after: idx)
        }
        let rest = String(line[idx...])
        let indent = min(4, indentSpaces / 2)

        var marker = "•"
        var body = ""
        if let m = firstRange(rxListUl, in: rest) {
            body = String(rest[m.upperBound...])
        } else if let m = firstRange(rxListOl, in: rest) {
            let digits = rest[m].prefix { $0.isNumber }
            marker = "\(digits)."
            body = String(rest[m.upperBound...])
        } else {
            return nil
        }

        var task = false
        var checked = false
        if let tm = firstRange(rxListTask, in: body) {
            task = true
            let markChar = body[body.index(after: body.startIndex)]
            checked = markChar == "x" || markChar == "X"
            body = String(body[tm.upperBound...])
        }
        return ListItem(marker: marker, task: task, checked: checked, indent: indent, text: body)
    }

    private static func parseFootnoteDef(_ t: String) -> (id: String, text: String)? {
        guard let m = firstRange(rxFootnoteDef, in: t) else { return nil }
        let prefix = String(t[m])
        guard let closeIdx = prefix.firstIndex(of: "]") else { return nil }
        let idStart = prefix.index(prefix.startIndex, offsetBy: 2)
        let id = String(prefix[idStart..<closeIdx])
        let text = String(t[m.upperBound...])
        return (id, text)
    }

    private static func parseStandaloneImage(_ t: String) -> (alt: String, url: String)? {
        guard t.hasPrefix("!["), t.hasSuffix(")"),
              let closeBracket = t.firstIndex(of: "]") else { return nil }
        let afterBracket = t.index(after: closeBracket)
        guard afterBracket < t.endIndex, t[afterBracket] == "(" else { return nil }
        let altStart = t.index(t.startIndex, offsetBy: 2)
        let alt = String(t[altStart..<closeBracket]).trimmingCharacters(in: .whitespaces)
        let urlStart = t.index(after: afterBracket)
        let urlEnd = t.index(before: t.endIndex)
        let rawTarget = String(t[urlStart..<urlEnd]).trimmingCharacters(in: .whitespaces)
        let url = rawTarget.components(separatedBy: .whitespaces).first ?? ""
        guard !url.isEmpty else { return nil }
        return (alt, url)
    }

    // MARK: - LaTeX Math Renderer -> AttributedString

    private static let latexSymbols: [String: String] = [
        "sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬", "oint": "∮",
        "alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε",
        "zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
        "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "varpi": "ϖ",
        "rho": "ρ", "varrho": "ϱ", "sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ",
        "phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
        "Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
        "Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
        "leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠",
        "approx": "≈", "equiv": "≡", "cong": "≅", "sim": "∼", "simeq": "≃", "propto": "∝",
        "ll": "≪", "gg": "≫", "prec": "≺", "succ": "≻",
        "times": "×", "div": "÷", "cdot": "·", "pm": "±", "mp": "∓", "ast": "∗", "star": "⋆",
        "oplus": "⊕", "otimes": "⊗", "odot": "⊙", "bullet": "•",
        "infty": "∞", "partial": "∂", "nabla": "∇", "forall": "∀", "exists": "∃", "nexists": "∄",
        "in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "subseteq": "⊆", "supset": "⊃", "supseteq": "⊇",
        "nsubseteq": "⊈", "cup": "∪", "cap": "∩", "setminus": "∖", "empty": "∅", "emptyset": "∅", "varnothing": "∅",
        "to": "→", "rightarrow": "→", "leftarrow": "←", "leftrightarrow": "↔",
        "Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "mapsto": "↦",
        "uparrow": "↑", "downarrow": "↓", "Uparrow": "⇑", "Downarrow": "⇓",
        "land": "∧", "wedge": "∧", "lor": "∨", "vee": "∨", "lnot": "¬", "neg": "¬",
        "therefore": "∴", "because": "∵", "implies": "⟹", "iff": "⟺",
        "dots": "…", "ldots": "…", "cdots": "⋯", "vdots": "⋮", "ddots": "⋱",
        "angle": "∠", "triangle": "△", "perp": "⊥", "parallel": "∥", "mid": "∣",
        "degree": "°", "circ": "∘", "prime": "′", "aleph": "ℵ", "hbar": "ℏ", "ell": "ℓ",
        "Re": "ℜ", "Im": "ℑ", "wp": "℘", "top": "⊤", "bot": "⊥", "models": "⊨", "vdash": "⊢",
        "langle": "⟨", "rangle": "⟩", "lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉",
        "quad": "  ", "qquad": "    ", ",": " ", ";": " ", ":": " ", "!": "",
        "%": "%", "$": "$", "&": "&", "#": "#", "_": "_", "{": "{", "}": "}",
    ]

    private static let latexAccents: [String: String] = [
        "hat": "\u{0302}", "widehat": "\u{0302}",
        "bar": "\u{0304}", "overline": "\u{0304}",
        "vec": "\u{20D7}",
        "tilde": "\u{0303}", "widetilde": "\u{0303}",
        "dot": "\u{0307}", "ddot": "\u{0308}",
    ]

    static func readLatexArg(_ chars: [Character], from pos: Int) -> (arg: String, next: Int) {
        var p = pos
        while p < chars.count, chars[p] == " " || chars[p] == "\t" { p += 1 }
        guard p < chars.count else { return ("", p) }
        if chars[p] != "{" {
            if chars[p] == "\\" {
                var end = p + 1
                while end < chars.count, chars[end].isLetter && chars[end].isASCII { end += 1 }
                if end == p + 1 && end < chars.count { end += 1 }
                return (String(chars[p..<end]), end)
            }
            return (String(chars[p]), p + 1)
        }
        var depth = 0
        let start = p + 1
        var i = p
        while i < chars.count {
            if chars[i] == "\\", i + 1 < chars.count { i += 2; continue }
            if chars[i] == "{" { depth += 1 }
            else if chars[i] == "}" {
                depth -= 1
                if depth == 0 { return (String(chars[start..<i]), i + 1) }
            }
            i += 1
        }
        return (String(chars[start...]), chars.count)
    }

    private static func wrapSimpleIfNeeded(_ s: String) -> String {
        let t = s.trimmingCharacters(in: .whitespaces)
        if t.isEmpty { return "" }
        if t.count <= 1 || t.rangeOfCharacter(from: CharacterSet(charactersIn: " +-*/=<>")) == nil {
            return t
        }
        return "(\(t))"
    }

    static func renderMathAttributed(_ expr: String, baseColor: Color, baseSize: CGFloat = 15, displayMode: Bool = false, depth: Int = 0) -> AttributedString {
        var result = AttributedString()
        if expr.isEmpty { return result }
        if depth > 8 {
            var a = AttributedString(expr)
            a.foregroundColor = baseColor
            return a
        }

        let chars = Array(expr)
        let n = chars.count
        var i = 0
        var buf = ""

        func flushBuf(size: CGFloat = baseSize, bold: Bool = false, italic: Bool = true, offset: CGFloat = 0) {
            guard !buf.isEmpty else { return }
            var a = AttributedString(buf)
            var font = Font.system(size: size, weight: bold ? .bold : .regular, design: .serif)
            if italic { font = font.italic() }
            a.font = font
            a.foregroundColor = baseColor
            if offset != 0 { a.baselineOffset = offset }
            result.append(a)
            buf = ""
        }

        while i < n {
            let ch = chars[i]
            if ch == "\\" {
                if i + 1 < n, chars[i + 1] == "\\" {
                    buf.append(displayMode ? "\n" : " ")
                    i += 2
                    continue
                }
                if i + 1 < n, !(chars[i + 1].isLetter && chars[i + 1].isASCII) {
                    let sym = String(chars[i + 1])
                    buf.append(latexSymbols[sym] ?? sym)
                    i += 2
                    continue
                }
                var end = i + 1
                while end < n, chars[end].isLetter && chars[end].isASCII { end += 1 }
                let cmd = String(chars[(i + 1)..<end])
                i = end

                if ["left", "right", "big", "Big", "bigg", "Bigg"].contains(cmd) { continue }

                if cmd == "frac" || cmd == "dfrac" || cmd == "tfrac" {
                    let r1 = readLatexArg(chars, from: i)
                    let r2 = readLatexArg(chars, from: r1.next)
                    i = r2.next
                    flushBuf()
                    let numAttr = renderMathAttributed(r1.arg, baseColor: baseColor, baseSize: max(11, baseSize * 0.85), depth: depth + 1)
                    let denAttr = renderMathAttributed(r2.arg, baseColor: baseColor, baseSize: max(11, baseSize * 0.85), depth: depth + 1)
                    let numPlain = String(numAttr.characters)
                    let denPlain = String(denAttr.characters)
                    let needNumParen = numPlain.count > 1 && numPlain.rangeOfCharacter(from: CharacterSet(charactersIn: " +-*/=<>")) != nil
                    let needDenParen = denPlain.count > 1 && denPlain.rangeOfCharacter(from: CharacterSet(charactersIn: " +-*/=<>")) != nil
                    if needNumParen { result.append(AttributedString("(")) }
                    result.append(numAttr)
                    if needNumParen { result.append(AttributedString(")")) }
                    var slash = AttributedString("/")
                    slash.font = .system(size: baseSize, design: .serif)
                    slash.foregroundColor = baseColor
                    result.append(slash)
                    if needDenParen { result.append(AttributedString("(")) }
                    result.append(denAttr)
                    if needDenParen { result.append(AttributedString(")")) }
                    continue
                }

                if cmd == "sqrt" {
                    var deg: String? = nil
                    if i < n, chars[i] == "[", let closeBracket = chars[i...].firstIndex(of: "]") {
                        deg = String(chars[(i + 1)..<closeBracket])
                        i = closeBracket + 1
                    }
                    let r1 = readLatexArg(chars, from: i)
                    i = r1.next
                    flushBuf()
                    if let deg {
                        var degAttr = renderMathAttributed(deg, baseColor: baseColor, baseSize: max(10, baseSize * 0.7), depth: depth + 1)
                        for run in degAttr.runs { degAttr[run.range].baselineOffset = 4 }
                        result.append(degAttr)
                    }
                    var radSign = AttributedString("√(")
                    radSign.font = .system(size: baseSize, design: .serif)
                    radSign.foregroundColor = baseColor
                    result.append(radSign)
                    result.append(renderMathAttributed(r1.arg, baseColor: baseColor, baseSize: baseSize, depth: depth + 1))
                    var closeRad = AttributedString(")")
                    closeRad.font = .system(size: baseSize, design: .serif)
                    closeRad.foregroundColor = baseColor
                    result.append(closeRad)
                    continue
                }

                if ["text", "mathrm", "operatorname", "textrm", "mathbb", "mathcal"].contains(cmd) {
                    let r1 = readLatexArg(chars, from: i)
                    i = r1.next
                    flushBuf()
                    var rm = AttributedString(r1.arg)
                    rm.font = .system(size: baseSize, design: .serif)
                    rm.foregroundColor = baseColor
                    result.append(rm)
                    continue
                }

                if ["mathbf", "boldsymbol", "textbf"].contains(cmd) {
                    let r1 = readLatexArg(chars, from: i)
                    i = r1.next
                    flushBuf()
                    var boldAttr = renderMathAttributed(r1.arg, baseColor: baseColor, baseSize: baseSize, depth: depth + 1)
                    for run in boldAttr.runs {
                        boldAttr[run.range].inlinePresentationIntent = .stronglyEmphasized
                    }
                    result.append(boldAttr)
                    continue
                }

                if let accent = latexAccents[cmd] {
                    let r1 = readLatexArg(chars, from: i)
                    i = r1.next
                    flushBuf()
                    var inner = renderMathAttributed(r1.arg, baseColor: baseColor, baseSize: baseSize, depth: depth + 1)
                    inner.append(AttributedString(accent))
                    result.append(inner)
                    continue
                }

                if ["sin", "cos", "tan", "cot", "sec", "csc", "log", "ln", "exp", "lim", "max", "min", "sup", "inf", "arg", "det", "dim", "mod"].contains(cmd) {
                    flushBuf()
                    var fnAttr = AttributedString(cmd)
                    fnAttr.font = .system(size: baseSize, design: .serif)
                    fnAttr.foregroundColor = baseColor
                    result.append(fnAttr)
                    continue
                }

                buf.append(latexSymbols[cmd] ?? cmd)
                continue
            }

            if ch == "^" || ch == "_" {
                let r1 = readLatexArg(chars, from: i + 1)
                i = r1.next
                flushBuf()
                let subSize = max(10, baseSize * 0.72)
                let offset: CGFloat = ch == "^" ? 5 : -3
                var scriptAttr = renderMathAttributed(r1.arg, baseColor: baseColor, baseSize: subSize, depth: depth + 1)
                for run in scriptAttr.runs {
                    scriptAttr[run.range].baselineOffset = offset
                }
                result.append(scriptAttr)
                continue
            }

            if ch == "{" {
                let r1 = readLatexArg(chars, from: i)
                i = r1.next
                flushBuf()
                result.append(renderMathAttributed(r1.arg, baseColor: baseColor, baseSize: baseSize, depth: depth + 1))
                continue
            }
            if ch == "}" {
                i += 1
                continue
            }
            if ch == "\n" {
                buf.append(displayMode ? "\n" : " ")
                i += 1
                continue
            }

            buf.append(ch)
            i += 1
        }
        flushBuf()
        return result
    }

    // MARK: - Single-Pass Inline Tokenizer -> AttributedString

    private static func isWordChar(_ ch: Character?) -> Bool {
        guard let ch else { return false }
        return ch.isLetter || ch.isNumber || ch == "_"
    }

    private static let safeInlineTags = Set(["sup", "sub", "u", "s", "del", "b", "strong", "i", "em", "code", "kbd", "mark"])

    private static func matchSafeInlineTag(_ src: String) -> (tag: String, inner: String, length: Int)? {
        guard let m = firstRange(rxSafeTag, in: src, anchored: true) else {
            return nil
        }
        let full = String(src[m])
        guard let gt = full.firstIndex(of: ">"),
              let closeStart = full.range(of: "</", options: .backwards)?.lowerBound else { return nil }
        let tag = String(full[full.index(after: full.startIndex)..<gt]).lowercased()
        guard safeInlineTags.contains(tag) else { return nil }
        let inner = String(full[full.index(after: gt)..<closeStart])
        return (tag, inner, full.count)
    }

    private static func buildInlineAttributed(_ src: String, palette: Palette, baseColor: Color, depth: Int = 0) -> AttributedString {
        var result = AttributedString()
        if src.isEmpty { return result }
        if depth > 8 {
            var plain = AttributedString(src)
            plain.foregroundColor = baseColor
            return plain
        }
        let cacheKey: NSString? = depth == 0 ? ("\(palette.dark ? 1 : 0)|\(baseColor.description)|\(src)" as NSString) : nil
        if let cacheKey, let cached = inlineCache.object(forKey: cacheKey) {
            return cached.attr
        }

        let chars = Array(src)
        let n = chars.count
        var i = 0
        var plainBuf = ""

        func flushPlain() {
            if !plainBuf.isEmpty {
                var a = AttributedString(plainBuf)
                a.foregroundColor = baseColor
                result.append(a)
                plainBuf = ""
            }
        }

        func appendStyled(_ sub: String, bold: Bool = false, italic: Bool = false, strike: Bool = false, underline: Bool = false) {
            var inner = buildInlineAttributed(sub, palette: palette, baseColor: baseColor, depth: depth + 1)
            for run in inner.runs {
                if bold {
                    inner[run.range].inlinePresentationIntent = (inner[run.range].inlinePresentationIntent ?? []).union(.stronglyEmphasized)
                }
                if italic {
                    inner[run.range].inlinePresentationIntent = (inner[run.range].inlinePresentationIntent ?? []).union(.emphasized)
                }
                if strike {
                    inner[run.range].strikethroughStyle = .single
                    inner[run.range].foregroundColor = palette.text2
                }
                if underline {
                    inner[run.range].underlineStyle = .single
                }
            }
            result.append(inner)
        }

        let escapable: Set<Character> = Set("\\`*_{}[]()#+-.!|>~$")

        while i < n {
            let ch = chars[i]

            // 1. Backslash escape
            if ch == "\\", i + 1 < n, escapable.contains(chars[i + 1]) {
                plainBuf.append(chars[i + 1])
                i += 2
                continue
            }

            // 2. Inline code (`...` or ``...``)
            if ch == "`" {
                var ticks = 0
                while i + ticks < n, chars[i + ticks] == "`" { ticks += 1 }
                if let end = findCharRun(chars, from: i + ticks, char: "`", count: ticks) {
                    flushPlain()
                    var content = String(chars[(i + ticks)..<end])
                    if ticks >= 2, content.hasPrefix(" "), content.hasSuffix(" "), !content.trimmingCharacters(in: .whitespaces).isEmpty {
                        content = String(content.dropFirst().dropLast())
                    }
                    var codeAttr = AttributedString(" \(content) ")
                    codeAttr.font = .system(size: 15, design: .monospaced)
                    codeAttr.backgroundColor = palette.s2
                    codeAttr.foregroundColor = baseColor
                    result.append(codeAttr)
                    i = end + ticks
                    continue
                }
            }

            // 3. <br> / safe attribute-free HTML tags / autolink <https://...>
            if ch == "<" {
                let tail = String(chars[i...])
                if let m = firstRange(rxBrTag, in: tail, anchored: true) {
                    plainBuf.append("\n")
                    i += tail[m].count
                    continue
                }
                if let safeTag = matchSafeInlineTag(tail) {
                    flushPlain()
                    switch safeTag.tag {
                    case "code", "kbd":
                        var tagAttr = AttributedString(" \(safeTag.inner) ")
                        tagAttr.font = .system(size: 14, design: .monospaced)
                        tagAttr.backgroundColor = palette.s2
                        tagAttr.foregroundColor = baseColor
                        result.append(tagAttr)
                    case "sup", "sub":
                        var scriptAttr = buildInlineAttributed(safeTag.inner, palette: palette, baseColor: baseColor, depth: depth + 1)
                        for run in scriptAttr.runs {
                            scriptAttr[run.range].font = .system(size: 11)
                            scriptAttr[run.range].baselineOffset = safeTag.tag == "sup" ? 5 : -3
                        }
                        result.append(scriptAttr)
                    case "mark":
                        var markAttr = buildInlineAttributed(safeTag.inner, palette: palette, baseColor: baseColor, depth: depth + 1)
                        for run in markAttr.runs {
                            markAttr[run.range].backgroundColor = palette.s2
                        }
                        result.append(markAttr)
                    case "b", "strong":
                        appendStyled(safeTag.inner, bold: true)
                    case "i", "em":
                        appendStyled(safeTag.inner, italic: true)
                    case "s", "del":
                        appendStyled(safeTag.inner, strike: true)
                    case "u":
                        appendStyled(safeTag.inner, underline: true)
                    default:
                        result.append(buildInlineAttributed(safeTag.inner, palette: palette, baseColor: baseColor, depth: depth + 1))
                    }
                    i += safeTag.length
                    continue
                }
                if let m = firstRange(rxAutolink, in: tail, anchored: true) {
                    let matched = String(tail[m])
                    let urlStr = String(matched.dropFirst().dropLast())
                    if let url = URL(string: urlStr) {
                        flushPlain()
                        var linkAttr = AttributedString(urlStr)
                        linkAttr.link = url
                        linkAttr.foregroundColor = palette.accent
                        linkAttr.underlineStyle = .single
                        result.append(linkAttr)
                        i += matched.count
                        continue
                    }
                }
            }

            // 4. Inline image ![alt](url) -> clickable [图片: alt] or text
            if ch == "!", i + 1 < n, chars[i + 1] == "[" {
                if let closeB = findSingleChar(chars, from: i + 2, target: "]"),
                   closeB + 1 < n, chars[closeB + 1] == "(",
                   let closeP = findMatchingParen(chars, openIdx: closeB + 1) {
                    let alt = String(chars[(i + 2)..<closeB]).trimmingCharacters(in: .whitespaces)
                    let rawTarget = String(chars[(closeB + 2)..<closeP]).trimmingCharacters(in: .whitespaces)
                    let urlStr = rawTarget.components(separatedBy: .whitespaces).first ?? ""
                    flushPlain()
                    let label = alt.isEmpty ? L.t("mdImageFallback") : L.t("mdImageFallbackAlt", ["alt": alt])
                    var imgAttr = AttributedString(label)
                    if let url = URL(string: urlStr), urlStr.lowercased().hasPrefix("http://") || urlStr.lowercased().hasPrefix("https://") {
                        imgAttr.link = url
                        imgAttr.foregroundColor = palette.accent
                        imgAttr.underlineStyle = .single
                    } else {
                        imgAttr.foregroundColor = palette.text2
                        imgAttr.backgroundColor = palette.s1
                    }
                    result.append(imgAttr)
                    i = closeP + 1
                    continue
                }
            }

            // 5. Footnote ref [^id]
            if ch == "[", i + 1 < n, chars[i + 1] == "^" {
                let tail = String(chars[i...])
                if let m = firstRange(rxFootnoteRef, in: tail, anchored: true) {
                    let matched = String(tail[m])
                    let id = String(matched.dropFirst(2).dropLast())
                    flushPlain()
                    var fnAttr = AttributedString("[\(id)]")
                    fnAttr.font = .system(size: 11, weight: .semibold)
                    fnAttr.foregroundColor = palette.text2
                    fnAttr.baselineOffset = 4
                    result.append(fnAttr)
                    i += matched.count
                    continue
                }
            }

            // 6. Link [label](url)
            if ch == "[" {
                if let closeB = findSingleChar(chars, from: i + 1, target: "]"),
                   closeB + 1 < n, chars[closeB + 1] == "(",
                   let closeP = findMatchingParen(chars, openIdx: closeB + 1) {
                    let label = String(chars[(i + 1)..<closeB])
                    let rawTarget = String(chars[(closeB + 2)..<closeP]).trimmingCharacters(in: .whitespaces)
                    let urlStr = rawTarget.components(separatedBy: .whitespaces).first ?? ""
                    let lower = urlStr.lowercased()
                    if (lower.hasPrefix("http://") || lower.hasPrefix("https://") || lower.hasPrefix("mailto:")),
                       let url = URL(string: urlStr) {
                        flushPlain()
                        var linkAttr = buildInlineAttributed(label, palette: palette, baseColor: palette.accent, depth: depth + 1)
                        for run in linkAttr.runs {
                            linkAttr[run.range].link = url
                            linkAttr[run.range].foregroundColor = palette.accent
                            linkAttr[run.range].underlineStyle = .single
                        }
                        result.append(linkAttr)
                        i = closeP + 1
                        continue
                    }
                }
            }

            // 7. Inline math $...$ (avoid currency like $100 - $200)
            if ch == "$", (i == 0 || chars[i - 1] != "$"), i + 1 < n, chars[i + 1] != "$", !chars[i + 1].isWhitespace {
                var end: Int? = nil
                var j = i + 1
                while j < n {
                    if chars[j] == "\\", j + 1 < n { j += 2; continue }
                    if chars[j] == "\n" { break }
                    if chars[j] == "$" {
                        if j > i + 1, !chars[j - 1].isWhitespace, !(j + 1 < n && chars[j + 1].isNumber) {
                            end = j
                        }
                        break
                    }
                    j += 1
                }
                if let end {
                    let expr = String(chars[(i + 1)..<end])
                    if !hasMatch(rxNumericOnly, in: expr) {
                        flushPlain()
                        var padL = AttributedString(" ")
                        padL.backgroundColor = palette.s1
                        result.append(padL)
                        var mathAttr = renderMathAttributed(expr, baseColor: baseColor, baseSize: 15)
                        for run in mathAttr.runs {
                            mathAttr[run.range].backgroundColor = palette.s1
                        }
                        result.append(mathAttr)
                        var padR = AttributedString(" ")
                        padR.backgroundColor = palette.s1
                        result.append(padR)
                        i = end + 1
                        continue
                    }
                }
            }

            // 8. Bold+Italic *** or ___
            if i + 2 < n, ((chars[i] == "*" && chars[i + 1] == "*" && chars[i + 2] == "*") ||
                           (chars[i] == "_" && chars[i + 1] == "_" && chars[i + 2] == "_")),
               i + 3 < n, !chars[i + 3].isWhitespace {
                let delim = chars[i]
                let isUnder = delim == "_"
                if !isUnder || !isWordChar(i > 0 ? chars[i - 1] : nil) {
                    if let end = findClosingDelim(chars, from: i + 3, delim: delim, count: 3, checkWord: isUnder) {
                        flushPlain()
                        appendStyled(String(chars[(i + 3)..<end]), bold: true, italic: true)
                        i = end + 3
                        continue
                    }
                }
            }

            // 9. Bold ** or __
            if i + 1 < n, ((chars[i] == "*" && chars[i + 1] == "*") || (chars[i] == "_" && chars[i + 1] == "_")),
               i + 2 < n, !chars[i + 2].isWhitespace {
                let delim = chars[i]
                let isUnder = delim == "_"
                if !isUnder || !isWordChar(i > 0 ? chars[i - 1] : nil) {
                    if let end = findClosingDelim(chars, from: i + 2, delim: delim, count: 2, checkWord: isUnder) {
                        flushPlain()
                        appendStyled(String(chars[(i + 2)..<end]), bold: true)
                        i = end + 2
                        continue
                    }
                }
            }

            // 10. Strikethrough ~~
            if i + 1 < n, chars[i] == "~", chars[i + 1] == "~", i + 2 < n, !chars[i + 2].isWhitespace {
                if let end = findClosingDelim(chars, from: i + 2, delim: "~", count: 2, checkWord: false) {
                    flushPlain()
                    appendStyled(String(chars[(i + 2)..<end]), strike: true)
                    i = end + 2
                    continue
                }
            }

            // 11. Italic * or _
            if (ch == "*" || ch == "_"), i + 1 < n, !chars[i + 1].isWhitespace, chars[i + 1] != ch {
                let isUnder = ch == "_"
                if !isUnder || !isWordChar(i > 0 ? chars[i - 1] : nil) {
                    if let end = findClosingDelim(chars, from: i + 1, delim: ch, count: 1, checkWord: isUnder) {
                        flushPlain()
                        appendStyled(String(chars[(i + 1)..<end]), italic: true)
                        i = end + 1
                        continue
                    }
                }
            }

            // 12. Bare URL
            if ch == "h", (i == 0 || chars[i - 1].isWhitespace || chars[i - 1] == "(") {
                let tail = String(chars[i...])
                if let m = firstRange(rxBareURL, in: tail, anchored: true) {
                    let rawUrl = String(tail[m])
                    let (cleanUrl, trailing) = splitTrailingPunct(rawUrl)
                    if cleanUrl.count > 8, let url = URL(string: cleanUrl) {
                        flushPlain()
                        var linkAttr = AttributedString(cleanUrl)
                        linkAttr.link = url
                        linkAttr.foregroundColor = palette.accent
                        linkAttr.underlineStyle = .single
                        result.append(linkAttr)
                        plainBuf.append(trailing)
                        i += rawUrl.count
                        continue
                    }
                }
            }

            plainBuf.append(ch)
            i += 1
        }

        flushPlain()
        if let cacheKey {
            inlineCache.setObject(InlineAttrBox(result), forKey: cacheKey)
        }
        return result
    }

    private static func findCharRun(_ chars: [Character], from: Int, char: Character, count: Int) -> Int? {
        var i = from
        while i <= chars.count - count {
            var ok = true
            for k in 0..<count {
                if chars[i + k] != char { ok = false; break }
            }
            if ok { return i }
            i += 1
        }
        return nil
    }

    private static func findSingleChar(_ chars: [Character], from: Int, target: Character) -> Int? {
        var i = from
        while i < chars.count {
            if chars[i] == "\\", i + 1 < chars.count { i += 2; continue }
            if chars[i] == "\n" { return nil }
            if chars[i] == target { return i }
            i += 1
        }
        return nil
    }

    private static func findMatchingParen(_ chars: [Character], openIdx: Int) -> Int? {
        var depth = 0
        var i = openIdx
        while i < chars.count {
            if chars[i] == "\\", i + 1 < chars.count { i += 2; continue }
            if chars[i] == "\n" { return nil }
            if chars[i] == "(" { depth += 1 }
            else if chars[i] == ")" {
                depth -= 1
                if depth == 0 { return i }
            }
            i += 1
        }
        return nil
    }

    private static func findClosingDelim(_ chars: [Character], from: Int, delim: Character, count: Int, checkWord: Bool) -> Int? {
        var i = from
        while i <= chars.count - count {
            if chars[i] == "\\", i + 1 < chars.count { i += 2; continue }
            if chars[i] == "`" {
                var ticks = 0
                while i + ticks < chars.count, chars[i + ticks] == "`" { ticks += 1 }
                if let end = findCharRun(chars, from: i + ticks, char: "`", count: ticks) {
                    i = end + ticks
                    continue
                }
            }
            var ok = true
            for k in 0..<count {
                if chars[i + k] != delim { ok = false; break }
            }
            if ok {
                let prev: Character? = i > 0 ? chars[i - 1] : nil
                if let prev, !prev.isWhitespace {
                    let next: Character? = (i + count < chars.count) ? chars[i + count] : nil
                    if !checkWord || !isWordChar(next) {
                        return i
                    }
                }
            }
            i += 1
        }
        return nil
    }

    private static func splitTrailingPunct(_ url: String) -> (String, String) {
        var u = url
        var trailing = ""
        let punct: Set<Character> = Set(".,;:!?。，；：！？、）】」』》\"'") // i18n:allow-cjk
        while let last = u.last {
            if punct.contains(last) {
                trailing = String(last) + trailing
                u.removeLast()
            } else if last == ")" {
                let opens = u.filter { $0 == "(" }.count
                let closes = u.filter { $0 == ")" }.count
                if closes > opens {
                    trailing = ")" + trailing
                    u.removeLast()
                } else {
                    break
                }
            } else {
                break
            }
        }
        return (u, trailing)
    }
}

private enum MathSeg {
    case text(String)
    case frac(num: String, den: String)
}

private struct MathBlockView: View {
    @Environment(\.palette) private var p
    var expr: String
    var color: Color

    var body: some View {
        let lines = splitTopLevelLines(expr)
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .center, spacing: 6) {
                ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                    let segs = splitTopLevelFracs(line)
                    HStack(alignment: .center, spacing: 3) {
                        ForEach(Array(segs.enumerated()), id: \.offset) { _, seg in
                            switch seg {
                            case let .text(sub):
                                Text(MarkdownText.renderMathAttributed(sub, baseColor: color, baseSize: 16, displayMode: true))
                            case let .frac(num, den):
                                VStack(spacing: 2) {
                                    Text(MarkdownText.renderMathAttributed(num, baseColor: color, baseSize: 13))
                                        .padding(.horizontal, 4)
                                    Rectangle().fill(color.opacity(0.75)).frame(height: 1)
                                    Text(MarkdownText.renderMathAttributed(den, baseColor: color, baseSize: 13))
                                        .padding(.horizontal, 4)
                                }
                                .fixedSize(horizontal: true, vertical: false)
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
        }
        .frame(maxWidth: .infinity, alignment: .center)
        .background(p.s1, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private func splitTopLevelLines(_ s: String) -> [String] {
        s.replacingOccurrences(of: "\\\\", with: "\n")
            .components(separatedBy: "\n")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    private func splitTopLevelFracs(_ s: String) -> [MathSeg] {
        let chars = Array(s)
        var segs: [MathSeg] = []
        var cur = ""
        var i = 0
        while i < chars.count {
            if chars[i] == "\\" {
                var end = i + 1
                while end < chars.count, chars[end].isLetter && chars[end].isASCII { end += 1 }
                let cmd = String(chars[(i + 1)..<end])
                if cmd == "frac" || cmd == "dfrac" || cmd == "tfrac" {
                    if !cur.isEmpty { segs.append(.text(cur)); cur = "" }
                    let r1 = MarkdownText.readLatexArg(chars, from: end)
                    let r2 = MarkdownText.readLatexArg(chars, from: r1.next)
                    segs.append(.frac(num: r1.arg, den: r2.arg))
                    i = r2.next
                    continue
                }
            }
            cur.append(chars[i])
            i += 1
        }
        if !cur.isEmpty { segs.append(.text(cur)) }
        return segs
    }
}

private struct DetailsBlockView<Content: View>: View {
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

private struct TableBlockView<Cell: View>: View {
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

private struct MarkdownImageView: View {
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
