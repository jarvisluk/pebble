package ai.pebble.android.ui.chat

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.BaselineShift
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.i18n.L
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.PebbleFont
import ai.pebble.android.ui.theme.MonoFont
import ai.pebble.android.ui.theme.PebblePalette
import ai.pebble.android.ui.theme.copyToClipboard
import coil3.compose.SubcomposeAsyncImage
import kotlinx.coroutines.delay
import androidx.compose.ui.res.painterResource
import ai.pebble.android.ui.theme.PebbleIcons

enum class TableAlign {
    LEFT, CENTER, RIGHT;

    fun textAlign(): TextAlign = when (this) {
        LEFT -> TextAlign.Start
        CENTER -> TextAlign.Center
        RIGHT -> TextAlign.End
    }

    fun boxAlign(): Alignment = when (this) {
        LEFT -> Alignment.CenterStart
        CENTER -> Alignment.Center
        RIGHT -> Alignment.CenterEnd
    }
}

data class MarkdownListItem(
    val marker: String,
    val task: Boolean,
    val checked: Boolean,
    val indent: Int,
    val text: String,
)

sealed interface MarkdownBlock {
    data class Code(val lang: String, val body: String) : MarkdownBlock
    data class Math(val expr: String) : MarkdownBlock
    data class Details(val summary: String, val isOpen: Boolean, val body: List<MarkdownBlock>) : MarkdownBlock
    data class Heading(val level: Int, val text: String) : MarkdownBlock
    data object Rule : MarkdownBlock
    data class Quote(val level: Int, val text: String) : MarkdownBlock
    data class Table(val headers: List<String>, val aligns: List<TableAlign>, val rows: List<List<String>>) : MarkdownBlock
    data class ListBlock(val items: List<MarkdownListItem>) : MarkdownBlock
    data class Footnote(val id: String, val text: String) : MarkdownBlock
    data class ImageBlock(val alt: String, val url: String) : MarkdownBlock
    data class Para(val text: String) : MarkdownBlock
}

object MarkdownParser {
    private val headingRegex = Regex("^#{1,6}\\s+")
    private val ruleRegex = Regex("^(?:-\\s*){3,}$|^(?:\\*\\s*){3,}$|^(?:_\\s*){3,}$|^<hr\\s*/?>$", RegexOption.IGNORE_CASE)
    private val ulRegex = Regex("^[-*+]\\s+")
    private val olRegex = Regex("^(\\d+)[.)]\\s+")
    private val taskRegex = Regex("^\\[([xX ])]\\s+")
    private val tableAlignRegex = Regex("^:?-{3,}:?$")
    private val detailsOpenRegex = Regex("^<details(\\s+open)?\\s*>", RegexOption.IGNORE_CASE)
    private val detailsCloseRegex = Regex("</details>", RegexOption.IGNORE_CASE)
    private val summaryRegex = Regex("<summary>([\\s\\S]*?)</summary>", RegexOption.IGNORE_CASE)
    private val footnoteDefRegex = Regex("^\\[\\^([^\\]]+)]:\\s*")
    private val unsafeBlockHtmlRegex = Regex("<(script|style|iframe|object|embed)\\b[^>]*>[\\s\\S]*?</\\1>|<\\/?(?:script|style|iframe|object|embed)\\b[^>]*>", RegexOption.IGNORE_CASE)

    fun sanitizeHtml(src: String): String = unsafeBlockHtmlRegex.replace(src, "")

    fun parse(src: String): List<MarkdownBlock> {
        val cleaned = sanitizeHtml(src.replace("\r\n", "\n"))
        val out = mutableListOf<MarkdownBlock>()
        val lines = cleaned.split('\n')
        var i = 0
        val para = mutableListOf<String>()
        val list = mutableListOf<MarkdownListItem>()

        fun flushPara() {
            if (para.isNotEmpty()) {
                out.add(MarkdownBlock.Para(para.joinToString("\n")))
                para.clear()
            }
        }

        fun flushList() {
            if (list.isNotEmpty()) {
                out.add(MarkdownBlock.ListBlock(list.toList()))
                list.clear()
            }
        }

        while (i < lines.size) {
            val line = lines[i]
            val t = line.trim()

            // 1. Fenced code block (``` or ~~~)
            val fence = matchFenceStart(t)
            if (fence != null) {
                flushPara()
                flushList()
                val body = mutableListOf<String>()
                i += 1
                while (i < lines.size) {
                    val ct = lines[i].trim()
                    if (isFenceClose(ct, fence.first, fence.second)) {
                        i += 1
                        break
                    }
                    body.add(lines[i])
                    i += 1
                }
                out.add(MarkdownBlock.Code(fence.third, body.joinToString("\n")))
                continue
            }

            // 2. Block math $$...$$ or \[...\]
            if (t.startsWith("$$") || t.startsWith("\\[")) {
                val closeDelim = if (t.startsWith("$$")) "$$" else "\\]"
                flushPara()
                flushList()
                if (t.length > 4 && t.endsWith(closeDelim)) {
                    val inner = t.drop(2).dropLast(2).trim()
                    out.add(MarkdownBlock.Math(inner))
                    i += 1
                    continue
                }
                val exprLines = mutableListOf<String>()
                val firstRest = t.drop(2).trim()
                if (firstRest.isNotEmpty()) exprLines.add(firstRest)
                i += 1
                while (i < lines.size) {
                    val ct = lines[i].trim()
                    if (ct.endsWith(closeDelim)) {
                        val tail = ct.dropLast(2).trim()
                        if (tail.isNotEmpty()) exprLines.add(tail)
                        i += 1
                        break
                    }
                    exprLines.add(lines[i])
                    i += 1
                }
                out.add(MarkdownBlock.Math(exprLines.joinToString("\n").trim()))
                continue
            }

            // 3. <details>...</details>
            val dm = detailsOpenRegex.find(t)
            if (dm != null) {
                var endIdx: Int? = null
                for (k in i until lines.size) {
                    if (detailsCloseRegex.containsMatchIn(lines[k])) {
                        endIdx = k
                        break
                    }
                }
                if (endIdx != null) {
                    flushPara()
                    flushList()
                    val isOpen = dm.value.lowercase().contains("open")
                    val collected = mutableListOf<String>()
                    val afterOpen = t.substring(dm.range.last + 1)
                    if (afterOpen.isNotEmpty()) collected.add(afterOpen)
                    for (k in (i + 1)..endIdx) collected.add(lines[k])
                    var rawBlock = collected.joinToString("\n")
                    val closeMatches = detailsCloseRegex.findAll(rawBlock).toList()
                    if (closeMatches.isNotEmpty()) {
                        rawBlock = rawBlock.substring(0, closeMatches.last().range.first)
                    }
                    rawBlock = rawBlock.trim()
                    var summary = "详情"
                    var bodyText = rawBlock
                    val sm = summaryRegex.find(rawBlock)
                    if (sm != null) {
                        val inner = sm.groupValues[1].trim()
                        if (inner.isNotEmpty()) summary = inner
                        bodyText = (rawBlock.substring(0, sm.range.first) + rawBlock.substring(sm.range.last + 1)).trim()
                    }
                    val innerBlocks = if (bodyText.isEmpty()) emptyList() else parse(bodyText)
                    out.add(MarkdownBlock.Details(summary, isOpen, innerBlocks))
                    i = endIdx + 1
                    continue
                }
            }

            // 4. Blank line
            if (t.isEmpty()) {
                flushPara()
                flushList()
                i += 1
                continue
            }

            // 5. Headings H1–H6
            val hm = headingRegex.find(t)
            if (hm != null) {
                flushPara()
                flushList()
                val level = hm.value.count { it == '#' }
                out.add(MarkdownBlock.Heading(level, t.substring(hm.range.last + 1)))
                i += 1
                continue
            }

            // 6. Horizontal rule
            if (ruleRegex.matches(t)) {
                flushPara()
                flushList()
                out.add(MarkdownBlock.Rule)
                i += 1
                continue
            }

            // 7. GFM Table
            if (t.contains('|') && i + 1 < lines.size) {
                val aligns = parseTableAlignments(lines[i + 1])
                if (aligns != null) {
                    val headers = splitTableCells(line)
                    if (headers.isNotEmpty()) {
                        flushPara()
                        flushList()
                        val rows = mutableListOf<List<String>>()
                        i += 2
                        while (i < lines.size) {
                            val rt = lines[i].trim()
                            if (rt.isEmpty() || !rt.contains('|') || parseTableAlignments(lines[i]) != null) break
                            rows.add(splitTableCells(lines[i]))
                            i += 1
                        }
                        out.add(MarkdownBlock.Table(headers, aligns, rows))
                        continue
                    }
                }
            }

            // 8. Blockquote (supports multiline + nested >>)
            if (t.startsWith(">")) {
                flushPara()
                flushList()
                var baseLevel = 0
                val qLines = mutableListOf<String>()
                while (i < lines.size && lines[i].trim().startsWith(">")) {
                    var lt = lines[i].trim()
                    var lvl = 0
                    while (lt.startsWith(">")) {
                        lvl += 1
                        lt = lt.drop(1).trim()
                    }
                    baseLevel = maxOf(baseLevel, lvl)
                    qLines.add(lt)
                    i += 1
                }
                out.add(MarkdownBlock.Quote(baseLevel, qLines.joinToString("\n")))
                continue
            }

            // 9. List (ordered, unordered, task, indented)
            val listItem = parseListLine(line)
            if (listItem != null) {
                flushPara()
                list.add(listItem)
                i += 1
                continue
            }

            // 10. Footnote definition [^id]: text
            val fn = parseFootnoteDef(t)
            if (fn != null) {
                flushPara()
                flushList()
                out.add(MarkdownBlock.Footnote(fn.first, fn.second))
                i += 1
                continue
            }

            // 11. Standalone image line ![alt](url)
            val img = parseStandaloneImage(t)
            if (img != null) {
                flushPara()
                flushList()
                out.add(MarkdownBlock.ImageBlock(img.first, img.second))
                i += 1
                continue
            }

            flushList()
            para.add(line)
            i += 1
        }

        flushPara()
        flushList()
        return out
    }

    private fun matchFenceStart(t: String): Triple<Char, Int, String>? {
        val first = t.firstOrNull() ?: return null
        if (first != '`' && first != '~') return null
        var len = 0
        for (c in t) {
            if (c == first) len += 1 else break
        }
        if (len < 3) return null
        return Triple(first, len, t.drop(len).trim())
    }

    private fun isFenceClose(t: String, ch: Char, minLen: Int): Boolean {
        if (t.isEmpty()) return false
        var len = 0
        for (c in t) {
            if (c == ch) len += 1 else return false
        }
        return len >= minLen
    }

    private fun splitTableCells(line: String): List<String> {
        var s = line.trim()
        if (s.startsWith("|")) s = s.drop(1)
        if (s.endsWith("|") && !s.endsWith("\\|")) s = s.dropLast(1)
        val cells = mutableListOf<String>()
        val cur = StringBuilder()
        var i = 0
        while (i < s.length) {
            if (s[i] == '\\' && i + 1 < s.length && s[i + 1] == '|') {
                cur.append('|')
                i += 2
                continue
            }
            if (s[i] == '|') {
                cells.add(cur.toString().trim())
                cur.clear()
            } else {
                cur.append(s[i])
            }
            i += 1
        }
        cells.add(cur.toString().trim())
        return cells
    }

    private fun parseTableAlignments(line: String): List<TableAlign>? {
        val t = line.trim()
        if (!t.contains('|')) return null
        val cells = splitTableCells(t)
        if (cells.isEmpty()) return null
        val aligns = mutableListOf<TableAlign>()
        for (c in cells) {
            if (!tableAlignRegex.matches(c)) return null
            aligns.add(
                when {
                    c.startsWith(":") && c.endsWith(":") -> TableAlign.CENTER
                    c.endsWith(":") -> TableAlign.RIGHT
                    else -> TableAlign.LEFT
                }
            )
        }
        return aligns
    }

    private fun parseListLine(line: String): MarkdownListItem? {
        var indentSpaces = 0
        var idx = 0
        while (idx < line.length) {
            when (line[idx]) {
                ' ' -> indentSpaces += 1
                '\t' -> indentSpaces += 2
                else -> break
            }
            idx += 1
        }
        val rest = line.substring(idx)
        val indent = (indentSpaces / 2).coerceAtMost(4)
        var marker = "•"
        val body: String
        val ulm = ulRegex.find(rest)
        val olm = olRegex.find(rest)
        when {
            ulm != null -> body = rest.substring(ulm.range.last + 1)
            olm != null -> {
                marker = "${olm.groupValues[1]}."
                body = rest.substring(olm.range.last + 1)
            }
            else -> return null
        }

        var task = false
        var checked = false
        var finalBody = body
        val tm = taskRegex.find(body)
        if (tm != null) {
            task = true
            val markChar = tm.groupValues[1]
            checked = markChar.equals("x", ignoreCase = true)
            finalBody = body.substring(tm.range.last + 1)
        }
        return MarkdownListItem(marker, task, checked, indent, finalBody)
    }

    private fun parseFootnoteDef(t: String): Pair<String, String>? {
        val m = footnoteDefRegex.find(t) ?: return null
        val id = m.groupValues[1]
        val text = t.substring(m.range.last + 1)
        return id to text
    }

    private fun parseStandaloneImage(t: String): Pair<String, String>? {
        if (!t.startsWith("![") || !t.endsWith(")")) return null
        val closeB = t.indexOf(']')
        if (closeB < 2 || closeB + 1 >= t.length || t[closeB + 1] != '(') return null
        val alt = t.substring(2, closeB).trim()
        val rawTarget = t.substring(closeB + 2, t.length - 1).trim()
        val url = rawTarget.split(Regex("\\s+")).firstOrNull().orEmpty()
        if (url.isEmpty()) return null
        return alt to url
    }
}

// MARK: - LaTeX Math Renderer

private val latexSymbols = mapOf(
    "sum" to "∑", "prod" to "∏", "coprod" to "∐", "int" to "∫", "iint" to "∬", "oint" to "∮",
    "alpha" to "α", "beta" to "β", "gamma" to "γ", "delta" to "δ", "epsilon" to "ε", "varepsilon" to "ε",
    "zeta" to "ζ", "eta" to "η", "theta" to "θ", "vartheta" to "ϑ", "iota" to "ι", "kappa" to "κ",
    "lambda" to "λ", "mu" to "μ", "nu" to "ν", "xi" to "ξ", "pi" to "π", "varpi" to "ϖ",
    "rho" to "ρ", "varrho" to "ϱ", "sigma" to "σ", "varsigma" to "ς", "tau" to "τ", "upsilon" to "υ",
    "phi" to "φ", "varphi" to "φ", "chi" to "χ", "psi" to "ψ", "omega" to "ω",
    "Gamma" to "Γ", "Delta" to "Δ", "Theta" to "Θ", "Lambda" to "Λ", "Xi" to "Ξ", "Pi" to "Π",
    "Sigma" to "Σ", "Upsilon" to "Υ", "Phi" to "Φ", "Psi" to "Ψ", "Omega" to "Ω",
    "leq" to "≤", "le" to "≤", "geq" to "≥", "ge" to "≥", "neq" to "≠", "ne" to "≠",
    "approx" to "≈", "equiv" to "≡", "cong" to "≅", "sim" to "∼", "simeq" to "≃", "propto" to "∝",
    "ll" to "≪", "gg" to "≫", "prec" to "≺", "succ" to "≻",
    "times" to "×", "div" to "÷", "cdot" to "·", "pm" to "±", "mp" to "∓", "ast" to "∗", "star" to "⋆",
    "oplus" to "⊕", "otimes" to "⊗", "odot" to "⊙", "bullet" to "•",
    "infty" to "∞", "partial" to "∂", "nabla" to "∇", "forall" to "∀", "exists" to "∃", "nexists" to "∄",
    "in" to "∈", "notin" to "∉", "ni" to "∋", "subset" to "⊂", "subseteq" to "⊆", "supset" to "⊃", "supseteq" to "⊇",
    "nsubseteq" to "⊈", "cup" to "∪", "cap" to "∩", "setminus" to "∖", "empty" to "∅", "emptyset" to "∅", "varnothing" to "∅",
    "to" to "→", "rightarrow" to "→", "leftarrow" to "←", "leftrightarrow" to "↔",
    "Rightarrow" to "⇒", "Leftarrow" to "⇐", "Leftrightarrow" to "⇔", "mapsto" to "↦",
    "uparrow" to "↑", "downarrow" to "↓", "Uparrow" to "⇑", "Downarrow" to "⇓",
    "land" to "∧", "wedge" to "∧", "lor" to "∨", "vee" to "∨", "lnot" to "¬", "neg" to "¬",
    "therefore" to "∴", "because" to "∵", "implies" to "⟹", "iff" to "⟺",
    "dots" to "…", "ldots" to "…", "cdots" to "⋯", "vdots" to "⋮", "ddots" to "⋱",
    "angle" to "∠", "triangle" to "△", "perp" to "⊥", "parallel" to "∥", "mid" to "∣",
    "degree" to "°", "circ" to "∘", "prime" to "′", "aleph" to "ℵ", "hbar" to "ℏ", "ell" to "ℓ",
    "Re" to "ℜ", "Im" to "ℑ", "wp" to "℘", "top" to "⊤", "bot" to "⊥", "models" to "⊨", "vdash" to "⊢",
    "langle" to "⟨", "rangle" to "⟩", "lfloor" to "⌊", "rfloor" to "⌋", "lceil" to "⌈", "rceil" to "⌉",
    "quad" to "  ", "qquad" to "    ", "," to " ", ";" to " ", ":" to " ", "!" to "",
    "%" to "%", "$" to "$", "&" to "&", "#" to "#", "_" to "_", "{" to "{", "}" to "}",
)

private val latexAccents = mapOf(
    "hat" to "\u0302", "widehat" to "\u0302",
    "bar" to "\u0304", "overline" to "\u0304",
    "vec" to "\u20D7",
    "tilde" to "\u0303", "widetilde" to "\u0303",
    "dot" to "\u0307", "ddot" to "\u0308",
)

private val latexRomanFuncs = setOf(
    "sin", "cos", "tan", "cot", "sec", "csc", "log", "ln", "exp", "lim",
    "max", "min", "sup", "inf", "arg", "det", "dim", "mod",
)

private val latexOpsChars = setOf(' ', '+', '-', '*', '/', '=', '<', '>')

internal fun readLatexArg(src: String, pos: Int): Pair<String, Int> {
    var p = pos
    while (p < src.length && (src[p] == ' ' || src[p] == '\t')) p += 1
    if (p >= src.length) return "" to p
    if (src[p] != '{') {
        if (src[p] == '\\') {
            var end = p + 1
            while (end < src.length && src[end] in 'A'..'Z' || (end < src.length && src[end] in 'a'..'z')) {
                end += 1
            }
            if (end == p + 1 && end < src.length) end += 1
            return src.substring(p, end) to end
        }
        return src[p].toString() to (p + 1)
    }
    var depth = 0
    val start = p + 1
    var i = p
    while (i < src.length) {
        if (src[i] == '\\' && i + 1 < src.length) {
            i += 2
            continue
        }
        if (src[i] == '{') depth += 1
        else if (src[i] == '}') {
            depth -= 1
            if (depth == 0) return src.substring(start, i) to (i + 1)
        }
        i += 1
    }
    return src.substring(start) to src.length
}

internal fun renderMathAnnotated(
    expr: String,
    baseColor: Color,
    baseSizeSp: Float = 15f,
    displayMode: Boolean = false,
    bgColor: Color? = null,
    depth: Int = 0,
): AnnotatedString {
    if (expr.isEmpty()) return AnnotatedString("")
    if (depth > 8) {
        return buildAnnotatedString {
            withStyle(SpanStyle(color = baseColor, background = bgColor ?: Color.Unspecified)) {
                append(expr)
            }
        }
    }
    return buildAnnotatedString {
        val n = expr.length
        var i = 0
        val buf = StringBuilder()

        fun flush(size: Float = baseSizeSp, bold: Boolean = false, italic: Boolean = true, shift: BaselineShift? = null) {
            if (buf.isEmpty()) return
            withStyle(
                SpanStyle(
                    fontFamily = FontFamily.Serif,
                    fontSize = size.sp,
                    fontWeight = if (bold) FontWeight.Bold else FontWeight.Normal,
                    fontStyle = if (italic) FontStyle.Italic else FontStyle.Normal,
                    color = baseColor,
                    background = bgColor ?: Color.Unspecified,
                    baselineShift = shift,
                )
            ) {
                append(buf.toString())
            }
            buf.clear()
        }

        while (i < n) {
            val ch = expr[i]
            if (ch == '\\') {
                if (i + 1 < n && expr[i + 1] == '\\') {
                    buf.append(if (displayMode) '\n' else ' ')
                    i += 2
                    continue
                }
                if (i + 1 < n && !(expr[i + 1] in 'a'..'z' || expr[i + 1] in 'A'..'Z')) {
                    val sym = expr[i + 1].toString()
                    buf.append(latexSymbols[sym] ?: sym)
                    i += 2
                    continue
                }
                var end = i + 1
                while (end < n && (expr[end] in 'a'..'z' || expr[end] in 'A'..'Z')) end += 1
                val cmd = expr.substring(i + 1, end)
                i = end

                if (cmd in setOf("left", "right", "big", "Big", "bigg", "Bigg")) continue

                if (cmd == "frac" || cmd == "dfrac" || cmd == "tfrac") {
                    val (a1, next1) = readLatexArg(expr, i)
                    val (a2, next2) = readLatexArg(expr, next1)
                    i = next2
                    flush()
                    val subSize = maxOf(11f, baseSizeSp * 0.85f)
                    val numA = renderMathAnnotated(a1, baseColor, subSize, bgColor = bgColor, depth = depth + 1)
                    val denA = renderMathAnnotated(a2, baseColor, subSize, bgColor = bgColor, depth = depth + 1)
                    val numParen = numA.text.length > 1 && numA.text.any { it in latexOpsChars }
                    val denParen = denA.text.length > 1 && denA.text.any { it in latexOpsChars }
                    val opStyle = SpanStyle(fontFamily = FontFamily.Serif, fontSize = baseSizeSp.sp, color = baseColor, background = bgColor ?: Color.Unspecified)
                    if (numParen) withStyle(opStyle) { append("(") }
                    append(numA)
                    if (numParen) withStyle(opStyle) { append(")") }
                    withStyle(opStyle) { append("/") }
                    if (denParen) withStyle(opStyle) { append("(") }
                    append(denA)
                    if (denParen) withStyle(opStyle) { append(")") }
                    continue
                }

                if (cmd == "sqrt") {
                    var deg: String? = null
                    if (i < n && expr[i] == '[') {
                        val closeIdx = expr.indexOf(']', i + 1)
                        if (closeIdx >= 0) {
                            deg = expr.substring(i + 1, closeIdx)
                            i = closeIdx + 1
                        }
                    }
                    val (arg, next) = readLatexArg(expr, i)
                    i = next
                    flush()
                    if (deg != null) {
                        val degA = renderMathAnnotated(deg, baseColor, maxOf(10f, baseSizeSp * 0.7f), bgColor = bgColor, depth = depth + 1)
                        withStyle(SpanStyle(baselineShift = BaselineShift(0.3f))) { append(degA) }
                    }
                    val opStyle = SpanStyle(fontFamily = FontFamily.Serif, fontSize = baseSizeSp.sp, color = baseColor, background = bgColor ?: Color.Unspecified)
                    withStyle(opStyle) { append("√(") }
                    append(renderMathAnnotated(arg, baseColor, baseSizeSp, bgColor = bgColor, depth = depth + 1))
                    withStyle(opStyle) { append(")") }
                    continue
                }

                if (cmd in setOf("text", "mathrm", "operatorname", "textrm", "mathbb", "mathcal")) {
                    val (arg, next) = readLatexArg(expr, i)
                    i = next
                    flush()
                    withStyle(SpanStyle(fontFamily = FontFamily.Serif, fontSize = baseSizeSp.sp, color = baseColor, background = bgColor ?: Color.Unspecified)) {
                        append(arg)
                    }
                    continue
                }

                if (cmd in setOf("mathbf", "boldsymbol", "textbf")) {
                    val (arg, next) = readLatexArg(expr, i)
                    i = next
                    flush()
                    val inner = renderMathAnnotated(arg, baseColor, baseSizeSp, bgColor = bgColor, depth = depth + 1)
                    withStyle(SpanStyle(fontWeight = FontWeight.Bold)) { append(inner) }
                    continue
                }

                val accent = latexAccents[cmd]
                if (accent != null) {
                    val (arg, next) = readLatexArg(expr, i)
                    i = next
                    flush()
                    append(renderMathAnnotated(arg, baseColor, baseSizeSp, bgColor = bgColor, depth = depth + 1))
                    withStyle(SpanStyle(fontFamily = FontFamily.Serif, fontSize = baseSizeSp.sp, color = baseColor, background = bgColor ?: Color.Unspecified)) {
                        append(accent)
                    }
                    continue
                }

                if (cmd in latexRomanFuncs) {
                    flush()
                    withStyle(SpanStyle(fontFamily = FontFamily.Serif, fontSize = baseSizeSp.sp, color = baseColor, background = bgColor ?: Color.Unspecified)) {
                        append(cmd)
                    }
                    continue
                }

                buf.append(latexSymbols[cmd] ?: cmd)
                continue
            }

            if (ch == '^' || ch == '_') {
                val (arg, next) = readLatexArg(expr, i + 1)
                i = next
                flush()
                val subSize = maxOf(10f, baseSizeSp * 0.72f)
                val shift = if (ch == '^') BaselineShift(0.35f) else BaselineShift(-0.22f)
                val sub = renderMathAnnotated(arg, baseColor, subSize, bgColor = bgColor, depth = depth + 1)
                withStyle(SpanStyle(baselineShift = shift)) { append(sub) }
                continue
            }

            if (ch == '{') {
                val (arg, next) = readLatexArg(expr, i)
                i = next
                flush()
                append(renderMathAnnotated(arg, baseColor, baseSizeSp, bgColor = bgColor, depth = depth + 1))
                continue
            }
            if (ch == '}') {
                i += 1
                continue
            }
            if (ch == '\n') {
                buf.append(if (displayMode) '\n' else ' ')
                i += 1
                continue
            }

            buf.append(ch)
            i += 1
        }
        flush()
    }
}

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
private val trailingUrlPunct = ".,;:!?。，；：！？、）】」』》\"'".toSet()

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

private fun isSafeUrl(url: String): Boolean {
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
                        val label = if (alt.isEmpty()) "[图片]" else "[图片: $alt]"
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

// MARK: - Compose Views

@Composable
fun MarkdownText(
    text: String,
    modifier: Modifier = Modifier,
    color: Color? = null,
) {
    val p = LocalPalette.current
    val fg = color ?: p.text
    val blocks = remember(text) { MarkdownParser.parse(text) }

    SelectionContainer(modifier = modifier) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            blocks.forEach { block ->
                RenderMarkdownBlock(block = block, p = p, fg = fg)
            }
        }
    }
}

@Composable
private fun RenderMarkdownBlock(
    block: MarkdownBlock,
    p: PebblePalette,
    fg: Color,
) {
    when (block) {
        is MarkdownBlock.Code -> CodeCard(lang = block.lang, code = block.body)
        is MarkdownBlock.Math -> MathBlockView(expr = block.expr, color = fg)
        is MarkdownBlock.Details -> DetailsBlockView(summary = block.summary, initiallyOpen = block.isOpen) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                block.body.forEach { sub -> RenderMarkdownBlock(sub, p, fg) }
            }
        }
        is MarkdownBlock.Heading -> {
            val size = when (block.level) {
                1 -> PebbleFont.hero
                2 -> PebbleFont.title
                3 -> PebbleFont.cardTitle
                else -> PebbleFont.body
            }
            val headColor = if (block.level >= 5) p.text2 else fg
            Text(
                text = buildInlineMarkdown(block.text, p, headColor),
                fontSize = size,
                fontWeight = FontWeight.Bold,
                color = headColor,
            )
        }
        is MarkdownBlock.Rule -> {
            HorizontalDivider(
                modifier = Modifier.padding(vertical = 4.dp),
                thickness = 1.dp,
                color = p.s3,
            )
        }
        is MarkdownBlock.Quote -> {
            Row(
                modifier = Modifier.height(IntrinsicSize.Min),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                repeat(block.level.coerceIn(1, 4)) {
                    Box(
                        modifier = Modifier
                            .width(3.dp)
                            .fillMaxHeight()
                            .clip(RoundedCornerShape(2.dp))
                            .background(p.s3),
                    )
                }
                Text(
                    text = buildInlineMarkdown(block.text, p, p.text2),
                    fontSize = PebbleFont.body,
                    lineHeight = 22.sp,
                    color = p.text2,
                )
            }
        }
        is MarkdownBlock.Table -> TableBlockView(headers = block.headers, aligns = block.aligns, rows = block.rows, fg = fg)
        is MarkdownBlock.ListBlock -> {
            Column(verticalArrangement = Arrangement.spacedBy(5.dp)) {
                block.items.forEach { item ->
                    Row(
                        modifier = Modifier.padding(start = (item.indent * 16).dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalAlignment = Alignment.Top,
                    ) {
                        if (item.task) {
                            Icon(
                                painter = painterResource(if (item.checked) PebbleIcons.CheckBox else PebbleIcons.CheckBoxOutlineBlank),
                                contentDescription = null,
                                tint = if (item.checked) p.accent else p.mute,
                                modifier = Modifier
                                    .padding(top = 3.dp)
                                    .size(16.dp),
                            )
                        } else {
                            Text(
                                text = item.marker,
                                fontSize = PebbleFont.body,
                                lineHeight = 22.sp,
                                color = p.mute,
                            )
                        }
                        val itemColor = if (item.task && item.checked) p.text2 else fg
                        Text(
                            text = buildInlineMarkdown(item.text, p, itemColor),
                            fontSize = PebbleFont.body,
                            lineHeight = 22.sp,
                            color = itemColor,
                            textDecoration = if (item.task && item.checked) TextDecoration.LineThrough else null,
                        )
                    }
                }
            }
        }
        is MarkdownBlock.Footnote -> {
            Row(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.Top,
            ) {
                Text(
                    text = "[${block.id}]",
                    fontSize = PebbleFont.caption,
                    fontWeight = FontWeight.SemiBold,
                    color = p.mute,
                )
                Text(
                    text = buildInlineMarkdown(block.text, p, p.text2),
                    fontSize = PebbleFont.secondary,
                    lineHeight = 19.sp,
                    color = p.text2,
                )
            }
        }
        is MarkdownBlock.ImageBlock -> MarkdownImageView(alt = block.alt, url = block.url)
        is MarkdownBlock.Para -> {
            Text(
                text = buildInlineMarkdown(block.text, p, fg),
                fontSize = PebbleFont.body,
                lineHeight = 22.sp,
                color = fg,
            )
        }
    }
}

private sealed interface MathSeg {
    data class TextSeg(val s: String) : MathSeg
    data class FracSeg(val num: String, val den: String) : MathSeg
}

@Composable
private fun MathBlockView(
    expr: String,
    color: Color,
) {
    val p = LocalPalette.current
    val lines = remember(expr) {
        expr.replace("\\\\", "\n")
            .split('\n')
            .map { it.trim() }
            .filter { it.isNotEmpty() }
    }

    Box(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(p.s1)
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 14.dp, vertical = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            lines.forEach { line ->
                val segs = remember(line) { splitTopLevelFracs(line) }
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    segs.forEach { seg ->
                        when (seg) {
                            is MathSeg.TextSeg -> {
                                Text(
                                    text = renderMathAnnotated(seg.s, color, 16f, displayMode = true),
                                )
                            }
                            is MathSeg.FracSeg -> {
                                Column(
                                    modifier = Modifier.width(IntrinsicSize.Max),
                                    horizontalAlignment = Alignment.CenterHorizontally,
                                    verticalArrangement = Arrangement.spacedBy(2.dp),
                                ) {
                                    Text(
                                        text = renderMathAnnotated(seg.num, color, 13f),
                                        modifier = Modifier.padding(horizontal = 4.dp),
                                    )
                                    Box(
                                        modifier = Modifier
                                            .fillMaxWidth()
                                            .height(1.dp)
                                            .background(color.copy(alpha = 0.75f)),
                                    )
                                    Text(
                                        text = renderMathAnnotated(seg.den, color, 13f),
                                        modifier = Modifier.padding(horizontal = 4.dp),
                                    )
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun splitTopLevelFracs(s: String): List<MathSeg> {
    val segs = mutableListOf<MathSeg>()
    val cur = StringBuilder()
    var i = 0
    while (i < s.length) {
        if (s[i] == '\\') {
            var end = i + 1
            while (end < s.length && (s[end] in 'a'..'z' || s[end] in 'A'..'Z')) end += 1
            val cmd = s.substring(i + 1, end)
            if (cmd == "frac" || cmd == "dfrac" || cmd == "tfrac") {
                if (cur.isNotEmpty()) {
                    segs.add(MathSeg.TextSeg(cur.toString()))
                    cur.clear()
                }
                val (num, next1) = readLatexArg(s, end)
                val (den, next2) = readLatexArg(s, next1)
                segs.add(MathSeg.FracSeg(num, den))
                i = next2
                continue
            }
        }
        cur.append(s[i])
        i += 1
    }
    if (cur.isNotEmpty()) segs.add(MathSeg.TextSeg(cur.toString()))
    return segs
}

@Composable
private fun DetailsBlockView(
    summary: String,
    initiallyOpen: Boolean,
    content: @Composable () -> Unit,
) {
    val p = LocalPalette.current
    var expanded by remember(summary, initiallyOpen) { mutableStateOf(initiallyOpen) }

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(p.s1)
            .padding(horizontal = 12.dp, vertical = 9.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable { expanded = !expanded },
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(
                painter = painterResource(if (expanded) PebbleIcons.KeyboardArrowDown else PebbleIcons.ChevronRight),
                contentDescription = null,
                tint = p.mute,
                modifier = Modifier.size(16.dp),
            )
            Text(
                text = summary,
                color = p.text,
                fontSize = PebbleFont.secondary,
                fontWeight = FontWeight.SemiBold,
            )
        }
        AnimatedVisibility(visible = expanded) {
            Column {
                HorizontalDivider(
                    modifier = Modifier.padding(vertical = 8.dp),
                    thickness = 1.dp,
                    color = p.s2,
                )
                content()
            }
        }
    }
}

@Composable
private fun TableBlockView(
    headers: List<String>,
    aligns: List<TableAlign>,
    rows: List<List<String>>,
    fg: Color,
) {
    val p = LocalPalette.current
    val colCount = maxOf(headers.size, aligns.size, rows.maxOfOrNull { it.size } ?: 0).coerceAtLeast(1)
    val allRows = listOf(headers) + rows

    Box(
        modifier = Modifier
            .horizontalScroll(rememberScrollState())
            .clip(RoundedCornerShape(12.dp))
            .border(1.dp, p.s2, RoundedCornerShape(12.dp)),
    ) {
        Layout(
            content = {
                allRows.forEachIndexed { rIdx, row ->
                    val isHeader = rIdx == 0
                    for (col in 0 until colCount) {
                        val align = aligns.getOrElse(col) { TableAlign.LEFT }
                        val cellText = row.getOrElse(col) { "" }
                        Box(
                            modifier = Modifier
                                .background(if (isHeader) p.s1 else Color.Transparent)
                                .then(
                                    if (col < colCount - 1) {
                                        Modifier.drawBehind {
                                            val stroke = 1.dp.toPx()
                                            val x = size.width - stroke / 2f
                                            drawLine(p.s2, Offset(x, 0f), Offset(x, size.height), strokeWidth = stroke)
                                        }
                                    } else Modifier
                                )
                                .padding(horizontal = 10.dp, vertical = 7.dp),
                            contentAlignment = align.boxAlign(),
                        ) {
                            Text(
                                text = buildInlineMarkdown(cellText, p, fg),
                                fontSize = PebbleFont.secondary,
                                fontWeight = if (isHeader) FontWeight.SemiBold else FontWeight.Normal,
                                color = fg,
                                textAlign = align.textAlign(),
                                modifier = Modifier.widthIn(max = 260.dp),
                            )
                        }
                    }
                    if (rIdx < allRows.lastIndex) {
                        Box(modifier = Modifier.height(1.dp).background(p.s2))
                    }
                }
            },
        ) { measurables, _ ->
            val minCellPx = 48.dp.roundToPx()
            val maxCellPx = 260.dp.roundToPx()
            val minRowPx = 24.dp.roundToPx()
            val divH = 1.dp.roundToPx().coerceAtLeast(1)
            val rowCount = allRows.size

            // Pass 1 (intrinsic only): compute unified width per column
            val colWidths = IntArray(colCount) { minCellPx }
            var idx = 0
            for (r in 0 until rowCount) {
                for (c in 0 until colCount) {
                    val w = measurables[idx++].maxIntrinsicWidth(Int.MAX_VALUE).coerceIn(minCellPx, maxCellPx)
                    if (w > colWidths[c]) colWidths[c] = w
                }
                if (r < rowCount - 1) idx++
            }

            // Pass 2 (intrinsic only): compute unified height per row
            val rowHeights = IntArray(rowCount) { minRowPx }
            idx = 0
            for (r in 0 until rowCount) {
                for (c in 0 until colCount) {
                    val h = measurables[idx++].minIntrinsicHeight(colWidths[c]).coerceAtLeast(minRowPx)
                    if (h > rowHeights[r]) rowHeights[r] = h
                }
                if (r < rowCount - 1) idx++
            }

            // Pass 3 (single measure per measurable)
            val totalW = colWidths.sum()
            val cellPlaceables = Array(rowCount) { arrayOfNulls<androidx.compose.ui.layout.Placeable>(colCount) }
            val dividerPlaceables = mutableListOf<androidx.compose.ui.layout.Placeable>()
            idx = 0
            for (r in 0 until rowCount) {
                for (c in 0 until colCount) {
                    cellPlaceables[r][c] = measurables[idx++].measure(
                        Constraints.fixed(colWidths[c], rowHeights[r])
                    )
                }
                if (r < rowCount - 1) {
                    dividerPlaceables.add(measurables[idx++].measure(Constraints.fixed(totalW, divH)))
                }
            }

            val finalDividers = dividerPlaceables
            val totalH = rowHeights.sum() + divH * (rowCount - 1).coerceAtLeast(0)

            layout(totalW, totalH) {
                var y = 0
                for (r in 0 until rowCount) {
                    var x = 0
                    for (c in 0 until colCount) {
                        cellPlaceables[r][c]?.placeRelative(x, y)
                        x += colWidths[c]
                    }
                    y += rowHeights[r]
                    if (r < rowCount - 1) {
                        finalDividers[r].placeRelative(0, y)
                        y += divH
                    }
                }
            }
        }
    }
}

@Composable
private fun MarkdownImageView(alt: String, url: String) {
    val p = LocalPalette.current
    val uriHandler = LocalUriHandler.current
    val label = if (alt.isEmpty()) "[图片]" else "[图片: $alt]"

    if (isSafeUrl(url)) {
        SubcomposeAsyncImage(
            model = url,
            contentDescription = alt.ifEmpty { null },
            contentScale = ContentScale.Fit,
            modifier = Modifier
                .widthIn(max = 320.dp)
                .heightIn(max = 280.dp)
                .clip(RoundedCornerShape(12.dp)),
            loading = {
                Box(
                    modifier = Modifier
                        .size(width = 180.dp, height = 110.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(p.s1),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp, color = p.mute)
                }
            },
            error = {
                Text(
                    text = label,
                    color = p.accent,
                    fontSize = PebbleFont.secondary,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .background(p.s1)
                        .clickable { runCatching { uriHandler.openUri(url) } }
                        .padding(horizontal = 10.dp, vertical = 5.dp),
                )
            },
        )
    } else {
        Text(
            text = label,
            color = p.text2,
            fontSize = PebbleFont.secondary,
            modifier = Modifier
                .clip(RoundedCornerShape(8.dp))
                .background(p.s1)
                .padding(horizontal = 10.dp, vertical = 5.dp),
        )
    }
}

@Composable
fun CodeCard(
    lang: String,
    code: String,
    modifier: Modifier = Modifier,
) {
    val p = LocalPalette.current
    val context = LocalContext.current
    var copied by remember { mutableStateOf(false) }

    LaunchedEffect(copied) {
        if (copied) {
            delay(1500)
            copied = false
        }
    }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(p.s2.copy(alpha = if (p.dark) 0.6f else 0.7f)),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = lang.ifEmpty { "code" },
                color = p.mute,
                fontSize = PebbleFont.secondary,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = if (copied) L.t("copied") else L.t("copy"),
                color = p.text2,
                fontSize = PebbleFont.secondary,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier
                    .clip(RoundedCornerShape(6.dp))
                    .clickable {
                        copyToClipboard(context, code)
                        copied = true
                    }
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            )
        }
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .horizontalScroll(rememberScrollState())
                .padding(start = 12.dp, end = 12.dp, bottom = 12.dp),
        ) {
            Text(
                text = code,
                fontFamily = MonoFont,
                fontSize = PebbleFont.secondary,
                lineHeight = 20.sp,
                color = p.text,
            )
        }
    }
}
