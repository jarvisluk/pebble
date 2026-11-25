package ai.pebble.android.ui.chat

import androidx.compose.ui.Alignment
import androidx.compose.ui.text.style.TextAlign
import ai.pebble.android.i18n.L
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
                    var summary = L.t("mdDetails")
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
