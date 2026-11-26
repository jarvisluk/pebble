import SwiftUI

private final class InlineAttrBox {
    let attr: AttributedString
    init(_ attr: AttributedString) { self.attr = attr }
}

private let inlineCache: NSCache<NSString, InlineAttrBox> = {
    let c = NSCache<NSString, InlineAttrBox>()
    c.countLimit = 512
    return c
}()

private let rxSafeTag = MarkdownText.compileRegex("^<(sup|sub|u|s|del|b|strong|i|em|code|kbd|mark)>([\\s\\S]*?)</\\1>", options: [.caseInsensitive])
private let rxBrTag = MarkdownText.compileRegex("^<br\\s*/?>", options: [.caseInsensitive])
private let rxAutolink = MarkdownText.compileRegex("^<(https?://[^>\\s]+|mailto:[^>\\s]+)>", options: [.caseInsensitive])
private let rxFootnoteRef = MarkdownText.compileRegex("^\\[\\^([^\\]]+)\\](?!:)")
private let rxNumericOnly = MarkdownText.compileRegex("^\\d+(?:[.,]\\d+)?$")
private let rxBareURL = MarkdownText.compileRegex("^https?://[^\\s<]+")
private let safeInlineTags = Set(["sup", "sub", "u", "s", "del", "b", "strong", "i", "em", "code", "kbd", "mark"])

extension MarkdownText {
    // MARK: - Single-Pass Inline Tokenizer -> AttributedString

    private static func isWordChar(_ ch: Character?) -> Bool {
        guard let ch else { return false }
        return ch.isLetter || ch.isNumber || ch == "_"
    }

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

    static func buildInlineAttributed(_ src: String, palette: Palette, baseColor: Color, depth: Int = 0) -> AttributedString {
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
