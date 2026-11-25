package ai.pebble.android.ui.chat

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.BaselineShift
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import ai.pebble.android.ui.theme.LocalPalette
import ai.pebble.android.ui.theme.MonoFont

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

private sealed interface MathSeg {
    data class TextSeg(val s: String) : MathSeg
    data class FracSeg(val num: String, val den: String) : MathSeg
}

@Composable
internal fun MathBlockView(
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
