import SwiftUI

private let latexSymbols: [String: String] = [
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

private let latexAccents: [String: String] = [
    "hat": "\u{0302}", "widehat": "\u{0302}",
    "bar": "\u{0304}", "overline": "\u{0304}",
    "vec": "\u{20D7}",
    "tilde": "\u{0303}", "widetilde": "\u{0303}",
    "dot": "\u{0307}", "ddot": "\u{0308}",
]

extension MarkdownText {
    // MARK: - LaTeX Math Renderer -> AttributedString

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
}

private enum MathSeg {
    case text(String)
    case frac(num: String, den: String)
}

struct MathBlockView: View {
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
