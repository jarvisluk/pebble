import SwiftUI

private final class ParsedBlocksBox {
    let blocks: [MarkdownText.Block]
    init(_ blocks: [MarkdownText.Block]) { self.blocks = blocks }
}

private let parsedCache: NSCache<NSString, ParsedBlocksBox> = {
    let c = NSCache<NSString, ParsedBlocksBox>()
    c.countLimit = 256
    return c
}()

private let rxDetailsOpen = MarkdownText.compileRegex("^<details(\\s+open)?\\s*>", options: [.caseInsensitive])
private let rxDetailsClose = MarkdownText.compileRegex("</details>", options: [.caseInsensitive])
private let rxSummary = MarkdownText.compileRegex("<summary>([\\s\\S]*?)</summary>", options: [.caseInsensitive])
private let rxSummaryOpen = MarkdownText.compileRegex("^<summary>", options: [.caseInsensitive])
private let rxSummaryClose = MarkdownText.compileRegex("</summary>$", options: [.caseInsensitive])
private let rxHeading = MarkdownText.compileRegex("^#{1,6}\\s+")
private let rxRule = MarkdownText.compileRegex("^(?:-\\s*){3,}$|^(?:\\*\\s*){3,}$|^(?:_\\s*){3,}$")
private let rxTableAlign = MarkdownText.compileRegex("^:?-{3,}:?$")
private let rxListUl = MarkdownText.compileRegex("^[-*+]\\s+")
private let rxListOl = MarkdownText.compileRegex("^(\\d+)[.)]\\s+")
private let rxListTask = MarkdownText.compileRegex("^\\[[xX ]\\]\\s+")
private let rxFootnoteDef = MarkdownText.compileRegex("^\\[\\^([^\\]]+)\\]:\\s*")

extension MarkdownText {
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
}
