package ai.pebble.android.ui.chat

import ai.pebble.android.i18n.L
import ai.pebble.android.ui.theme.PebbleFont
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.BaselineShift
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.sp
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebblePalette

// MARK: - Single-Pass Inline Tokenizer

private val safeInlineTags = setOf("sup", "sub", "u", "s", "del", "b", "strong", "i", "em", "code", "kbd", "mark")
private val safeTagRegex = Regex("^<(sup|sub|u|s|del|b|strong|i|em|code|kbd|mark)>([\\s\\S]*?)</\\1>", RegexOption.IGNORE_CASE)
private val safeAnchorRegex = Regex("^<a\\s+[^>]*href\\s*=\\s*[\"']([^\"'>\\s]+)[\"'][^>]*>([\\s\\S]*?)</a>", RegexOption.IGNORE_CASE)
private val brTagRegex = Regex("^<br\\s*/?>", RegexOption.IGNORE_CASE)
private val autoLinkRegex = Regex("^<(https?://[^>\\s]+|mailto:[^>\\s]+)>", RegexOption.IGNORE_CASE)
private val footnoteRefRegex = Regex("^\\[\\^([^\\]]+)](?!:)")
private val bareUrlRegex = Regex("^https?://[^\\s<]+")
private val pureNumberRegex = Regex("^\\d+(?:[.,]\\d+)?$")
private val escapableChars = "\\`*_{}[]()#+-.!|>~$".toSet()
private val trailingUrlPunct = ".,;:!?。，；：！？、）】」』》\"'".toSet() // i18n:allow-cjk

private fun isWordChar(ch: Char?): Boolean = ch != null && (ch.isLetterOrDigit() || ch == '_')

private fun findCharRun(src: String, from: Int, ch: Char, count: Int): Int? {
    var i = from
    while (i <= src.length - count) {
        var ok = true
        for (k in 0 until count) {
            if (src[i + k] != ch) {
                ok = false
                break
            }
        }
        if (ok) return i
        i += 1
    }
    return null
}

private fun findSingleChar(src: String, from: Int, target: Char): Int? {
    var i = from
    while (i < src.length) {
        if (src[i] == '\\' && i + 1 < src.length) {
            i += 2
            continue
        }
        if (src[i] == '\n') return null
        if (src[i] == target) return i
        i += 1
    }
    return null
}

private fun findMatchingParen(src: String, openIdx: Int): Int? {
    var depth = 0
    var i = openIdx
    while (i < src.length) {
        if (src[i] == '\\' && i + 1 < src.length) {
            i += 2
            continue
        }
        if (src[i] == '\n') return null
        when (src[i]) {
            '(' -> depth += 1
            ')' -> {
                depth -= 1
                if (depth == 0) return i
            }
        }
        i += 1
    }
    return null
}

private fun findClosingDelim(src: String, from: Int, delim: Char, count: Int, checkWord: Boolean): Int? {
    var i = from
    while (i <= src.length - count) {
        if (src[i] == '\\' && i + 1 < src.length) {
            i += 2
            continue
        }
        if (src[i] == '`') {
            var ticks = 0
            while (i + ticks < src.length && src[i + ticks] == '`') ticks += 1
            val end = findCharRun(src, i + ticks, '`', ticks)
            if (end != null) {
                i = end + ticks
                continue
            }
        }
        var ok = true
        for (k in 0 until count) {
            if (src[i + k] != delim) {
                ok = false
                break
            }
        }
        if (ok) {
            val prev = if (i > 0) src[i - 1] else null
            if (prev != null && !prev.isWhitespace()) {
                val next = if (i + count < src.length) src[i + count] else null
                if (!checkWord || !isWordChar(next)) return i
            }
        }
        i += 1
    }
    return null
}

private fun splitTrailingPunct(url: String): Pair<String, String> {
    var u = url
    val trailing = StringBuilder()
    while (u.isNotEmpty()) {
        val last = u.last()
        when {
            last in trailingUrlPunct -> {
                trailing.insert(0, last)
                u = u.dropLast(1)
            }
            last == ')' && u.count { it == ')' } > u.count { it == '(' } -> {
                trailing.insert(0, ')')
                u = u.dropLast(1)
            }
            else -> break
        }
    }
    return u to trailing.toString()
}

internal fun isSafeUrl(url: String): Boolean {
    val lower = url.trim().lowercase()
    return lower.startsWith("http://") || lower.startsWith("https://") || lower.startsWith("mailto:")
}

internal fun buildInlineMarkdown(
    src: String,
    p: PebblePalette,
    baseColor: Color,
    depth: Int = 0,
): AnnotatedString {
    val cleaned = MarkdownParser.sanitizeHtml(src)
    if (cleaned.isEmpty()) return AnnotatedString("")
    if (depth > 8) {
        return buildAnnotatedString {
            withStyle(SpanStyle(color = baseColor)) { append(cleaned) }
        }
    }

    return buildAnnotatedString {
        val n = cleaned.length
        var i = 0
        val plainBuf = StringBuilder()

        fun flushPlain() {
            if (plainBuf.isNotEmpty()) {
                withStyle(SpanStyle(color = baseColor)) { append(plainBuf.toString()) }
                plainBuf.clear()
            }
        }

        fun appendStyled(sub: String, style: SpanStyle) {
            withStyle(style) {
                append(buildInlineMarkdown(sub, p, style.color.takeIf { it != Color.Unspecified } ?: baseColor, depth + 1))
            }
        }

        while (i < n) {
            val ch = cleaned[i]

            // 1. Inline \(...\) math
            if (ch == '\\' && i + 1 < n && cleaned[i + 1] == '(') {
                val closeIdx = cleaned.indexOf("\\)", i + 2)
                if (closeIdx > i + 2 && !cleaned.substring(i + 2, closeIdx).contains('\n')) {
                    flushPlain()
                    val expr = cleaned.substring(i + 2, closeIdx)
                    withStyle(SpanStyle(background = p.s1)) { append(" ") }
                    append(renderMathAnnotated(expr, baseColor, 15f, bgColor = p.s1))
                    withStyle(SpanStyle(background = p.s1)) { append(" ") }
                    i = closeIdx + 2
                    continue
                }
            }

            // 2. Backslash escape
            if (ch == '\\' && i + 1 < n && cleaned[i + 1] in escapableChars) {
                plainBuf.append(cleaned[i + 1])
                i += 2
                continue
            }

            // 3. Inline code (`...` or ``...``)
            if (ch == '`') {
                var ticks = 0
                while (i + ticks < n && cleaned[i + ticks] == '`') ticks += 1
                val end = findCharRun(cleaned, i + ticks, '`', ticks)
                if (end != null) {
                    flushPlain()
                    var content = cleaned.substring(i + ticks, end)
                    if (ticks >= 2 && content.startsWith(" ") && content.endsWith(" ") && content.isNotBlank()) {
                        content = content.drop(1).dropLast(1)
                    }
                    withStyle(SpanStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary, background = p.s2, color = baseColor)) {
                        append(" $content ")
                    }
                    i = end + ticks
                    continue
                }
            }

            // 4. <br> / safe HTML / <a href> / <https://...>
            if (ch == '<') {
                val tail = cleaned.substring(i)
                val br = brTagRegex.find(tail)
                if (br != null) {
                    plainBuf.append('\n')
                    i += br.value.length
                    continue
                }
                val anc = safeAnchorRegex.find(tail)
                if (anc != null) {
                    val href = anc.groupValues[1].trim()
                    val inner = anc.groupValues[2]
                    flushPlain()
                    if (isSafeUrl(href)) {
                        withLink(
                            LinkAnnotation.Url(
                                url = href,
                                styles = TextLinkStyles(
                                    style = SpanStyle(
                                        color = p.accent,
                                        textDecoration = TextDecoration.Underline,
                                        fontWeight = FontWeight.Medium,
                                    )
                                ),
                            )
                        ) {
                            append(buildInlineMarkdown(inner, p, p.accent, depth + 1))
                        }
                    } else {
                        append(buildInlineMarkdown(inner, p, baseColor, depth + 1))
                    }
                    i += anc.value.length
                    continue
                }
                val tagM = safeTagRegex.find(tail)
                if (tagM != null) {
                    val tag = tagM.groupValues[1].lowercase()
                    val inner = tagM.groupValues[2]
                    if (tag in safeInlineTags) {
                        flushPlain()
                        when (tag) {
                            "code", "kbd" -> withStyle(
                                SpanStyle(fontFamily = MonoFont, fontSize = PebbleFont.secondary, background = p.s2, color = baseColor)
                            ) { append(" $inner ") }
                            "sup", "sub" -> appendStyled(
                                inner,
                                SpanStyle(
                                    fontSize = PebbleFont.caption,
                                    baselineShift = if (tag == "sup") BaselineShift(0.35f) else BaselineShift(-0.22f),
                                )
                            )
                            "mark" -> appendStyled(inner, SpanStyle(background = p.s2))
                            "b", "strong" -> appendStyled(inner, SpanStyle(fontWeight = FontWeight.Bold))
                            "i", "em" -> appendStyled(inner, SpanStyle(fontStyle = FontStyle.Italic))
                            "s", "del" -> appendStyled(inner, SpanStyle(textDecoration = TextDecoration.LineThrough, color = p.text2))
                            "u" -> appendStyled(inner, SpanStyle(textDecoration = TextDecoration.Underline))
                        }
                        i += tagM.value.length
                        continue
                    }
                }
                val autoM = autoLinkRegex.find(tail)
                if (autoM != null) {
                    val urlStr = autoM.groupValues[1]
                    flushPlain()
                    withLink(
                        LinkAnnotation.Url(
                            url = urlStr,
                            styles = TextLinkStyles(
                                style = SpanStyle(color = p.accent, textDecoration = TextDecoration.Underline)
                            ),
                        )
                    ) { append(urlStr) }
                    i += autoM.value.length
                    continue
                }
            }

            // 5. Inline image ![alt](url)
            if (ch == '!' && i + 1 < n && cleaned[i + 1] == '[') {
                val closeB = findSingleChar(cleaned, i + 2, ']')
                if (closeB != null && closeB + 1 < n && cleaned[closeB + 1] == '(') {
                    val closeP = findMatchingParen(cleaned, closeB + 1)
                    if (closeP != null) {
                        val alt = cleaned.substring(i + 2, closeB).trim()
                        val urlStr = cleaned.substring(closeB + 2, closeP).trim().split(Regex("\\s+")).firstOrNull().orEmpty()
                        flushPlain()
                        val label = if (alt.isEmpty()) L.t("mdImageFallback") else L.t("mdImageFallbackAlt", mapOf("alt" to alt))
                        if (isSafeUrl(urlStr)) {
                            withLink(
                                LinkAnnotation.Url(
                                    url = urlStr,
                                    styles = TextLinkStyles(
                                        style = SpanStyle(color = p.accent, textDecoration = TextDecoration.Underline)
                                    ),
                                )
                            ) { append(label) }
                        } else {
                            withStyle(SpanStyle(color = p.text2, background = p.s1)) { append(label) }
                        }
                        i = closeP + 1
                        continue
                    }
                }
            }

            // 6. Footnote ref [^id]
            if (ch == '[' && i + 1 < n && cleaned[i + 1] == '^') {
                val fnM = footnoteRefRegex.find(cleaned.substring(i))
                if (fnM != null) {
                    flushPlain()
                    withStyle(
                        SpanStyle(
                            fontSize = PebbleFont.caption,
                            fontWeight = FontWeight.SemiBold,
                            color = p.text2,
                            baselineShift = BaselineShift(0.35f),
                        )
                    ) { append("[${fnM.groupValues[1]}]") }
                    i += fnM.value.length
                    continue
                }
            }

            // 7. Link [label](url)
            if (ch == '[') {
                val closeB = findSingleChar(cleaned, i + 1, ']')
                if (closeB != null && closeB + 1 < n && cleaned[closeB + 1] == '(') {
                    val closeP = findMatchingParen(cleaned, closeB + 1)
                    if (closeP != null) {
                        val label = cleaned.substring(i + 1, closeB)
                        val urlStr = cleaned.substring(closeB + 2, closeP).trim().split(Regex("\\s+")).firstOrNull().orEmpty()
                        if (isSafeUrl(urlStr)) {
                            flushPlain()
                            withLink(
                                LinkAnnotation.Url(
                                    url = urlStr,
                                    styles = TextLinkStyles(
                                        style = SpanStyle(
                                            color = p.accent,
                                            textDecoration = TextDecoration.Underline,
                                            fontWeight = FontWeight.Medium,
                                        )
                                    ),
                                )
                            ) {
                                append(buildInlineMarkdown(label, p, p.accent, depth + 1))
                            }
                            i = closeP + 1
                            continue
                        }
                    }
                }
            }

            // 8. Inline math $...$ (avoid currency like $100 - $200)
            if (ch == '$' && (i == 0 || cleaned[i - 1] != '$') && i + 1 < n && cleaned[i + 1] != '$' && !cleaned[i + 1].isWhitespace()) {
                var end: Int? = null
                var j = i + 1
                while (j < n) {
                    if (cleaned[j] == '\\' && j + 1 < n) {
                        j += 2
                        continue
                    }
                    if (cleaned[j] == '\n') break
                    if (cleaned[j] == '$') {
                        if (j > i + 1 && !cleaned[j - 1].isWhitespace() && !(j + 1 < n && cleaned[j + 1].isDigit())) {
                            end = j
                        }
                        break
                    }
                    j += 1
                }
                if (end != null) {
                    val expr = cleaned.substring(i + 1, end)
                    if (!pureNumberRegex.matches(expr)) {
                        flushPlain()
                        withStyle(SpanStyle(background = p.s1)) { append(" ") }
                        append(renderMathAnnotated(expr, baseColor, 15f, bgColor = p.s1))
                        withStyle(SpanStyle(background = p.s1)) { append(" ") }
                        i = end + 1
                        continue
                    }
                }
            }

            // 9. Bold + Italic *** or ___
            if (i + 2 < n && ((cleaned[i] == '*' && cleaned[i + 1] == '*' && cleaned[i + 2] == '*') ||
                    (cleaned[i] == '_' && cleaned[i + 1] == '_' && cleaned[i + 2] == '_')) &&
                i + 3 < n && !cleaned[i + 3].isWhitespace()
            ) {
                val delim = cleaned[i]
                val isUnder = delim == '_'
                if (!isUnder || !isWordChar(if (i > 0) cleaned[i - 1] else null)) {
                    val end = findClosingDelim(cleaned, i + 3, delim, 3, isUnder)
                    if (end != null) {
                        flushPlain()
                        appendStyled(cleaned.substring(i + 3, end), SpanStyle(fontWeight = FontWeight.Bold, fontStyle = FontStyle.Italic))
                        i = end + 3
                        continue
                    }
                }
            }

            // 10. Bold ** or __
            if (i + 1 < n && ((cleaned[i] == '*' && cleaned[i + 1] == '*') || (cleaned[i] == '_' && cleaned[i + 1] == '_')) &&
                i + 2 < n && !cleaned[i + 2].isWhitespace()
            ) {
                val delim = cleaned[i]
                val isUnder = delim == '_'
                if (!isUnder || !isWordChar(if (i > 0) cleaned[i - 1] else null)) {
                    val end = findClosingDelim(cleaned, i + 2, delim, 2, isUnder)
                    if (end != null) {
                        flushPlain()
                        appendStyled(cleaned.substring(i + 2, end), SpanStyle(fontWeight = FontWeight.Bold))
                        i = end + 2
                        continue
                    }
                }
            }

            // 11. Strikethrough ~~
            if (i + 1 < n && cleaned[i] == '~' && cleaned[i + 1] == '~' && i + 2 < n && !cleaned[i + 2].isWhitespace()) {
                val end = findClosingDelim(cleaned, i + 2, '~', 2, false)
                if (end != null) {
                    flushPlain()
                    appendStyled(cleaned.substring(i + 2, end), SpanStyle(textDecoration = TextDecoration.LineThrough, color = p.text2))
                    i = end + 2
                    continue
                }
            }

            // 12. Italic * or _
            if ((ch == '*' || ch == '_') && i + 1 < n && !cleaned[i + 1].isWhitespace() && cleaned[i + 1] != ch) {
                val isUnder = ch == '_'
                if (!isUnder || !isWordChar(if (i > 0) cleaned[i - 1] else null)) {
                    val end = findClosingDelim(cleaned, i + 1, ch, 1, isUnder)
                    if (end != null) {
                        flushPlain()
                        appendStyled(cleaned.substring(i + 1, end), SpanStyle(fontStyle = FontStyle.Italic))
                        i = end + 1
                        continue
                    }
                }
            }

            // 13. Bare URL
            if (ch == 'h' && (i == 0 || cleaned[i - 1].isWhitespace() || cleaned[i - 1] == '(')) {
                val m = bareUrlRegex.find(cleaned.substring(i))
                if (m != null) {
                    val rawUrl = m.value
                    val (cleanUrl, trailing) = splitTrailingPunct(rawUrl)
                    if (cleanUrl.length > 8) {
                        flushPlain()
                        withLink(
                            LinkAnnotation.Url(
                                url = cleanUrl,
                                styles = TextLinkStyles(
                                    style = SpanStyle(color = p.accent, textDecoration = TextDecoration.Underline)
                                ),
                            )
                        ) { append(cleanUrl) }
                        plainBuf.append(trailing)
                        i += rawUrl.length
                        continue
                    }
                }
            }

            plainBuf.append(ch)
            i += 1
        }

        flushPlain()
    }
}
