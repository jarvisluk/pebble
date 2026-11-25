export function escHtml(s: unknown): string {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  }[c] || c));
}

function isSafeUrl(url: string, allowMailto: boolean): boolean {
  const lower = url.trim().toLowerCase();
  if (lower.startsWith('http://') || lower.startsWith('https://')) return true;
  if (allowMailto && lower.startsWith('mailto:')) return true;
  return false;
}

function isWordChar(ch: string | undefined): boolean {
  if (!ch) return false;
  return /[\p{L}\p{N}_]/u.test(ch);
}

const LATEX_SYMBOLS: Record<string, string> = {
  sum: '∑', prod: '∏', coprod: '∐', int: '∫', iint: '∬', oint: '∮',
  alpha: 'α', beta: 'β', gamma: 'γ', delta: 'δ', epsilon: 'ε', varepsilon: 'ε',
  zeta: 'ζ', eta: 'η', theta: 'θ', vartheta: 'ϑ', iota: 'ι', kappa: 'κ',
  lambda: 'λ', mu: 'μ', nu: 'ν', xi: 'ξ', pi: 'π', varpi: 'ϖ',
  rho: 'ρ', varrho: 'ϱ', sigma: 'σ', varsigma: 'ς', tau: 'τ', upsilon: 'υ',
  phi: 'φ', varphi: 'φ', chi: 'χ', psi: 'ψ', omega: 'ω',
  Gamma: 'Γ', Delta: 'Δ', Theta: 'Θ', Lambda: 'Λ', Xi: 'Ξ', Pi: 'Π',
  Sigma: 'Σ', Upsilon: 'Υ', Phi: 'Φ', Psi: 'Ψ', Omega: 'Ω',
  leq: '≤', le: '≤', geq: '≥', ge: '≥', neq: '≠', ne: '≠',
  approx: '≈', equiv: '≡', cong: '≅', sim: '∼', simeq: '≃', propto: '∝',
  ll: '≪', gg: '≫', prec: '≺', succ: '≻',
  times: '×', div: '÷', cdot: '·', pm: '±', mp: '∓', ast: '∗', star: '⋆',
  oplus: '⊕', otimes: '⊗', odot: '⊙', bullet: '•',
  infty: '∞', partial: '∂', nabla: '∇', forall: '∀', exists: '∃', nexists: '∄',
  in: '∈', notin: '∉', ni: '∋', subset: '⊂', subseteq: '⊆', supset: '⊃', supseteq: '⊇',
  nsubseteq: '⊈', cup: '∪', cap: '∩', setminus: '∖', empty: '∅', emptyset: '∅', varnothing: '∅',
  to: '→', rightarrow: '→', leftarrow: '←', leftrightarrow: '↔',
  Rightarrow: '⇒', Leftarrow: '⇐', Leftrightarrow: '⇔', mapsto: '↦',
  uparrow: '↑', downarrow: '↓', Uparrow: '⇑', Downarrow: '⇓',
  land: '∧', wedge: '∧', lor: '∨', vee: '∨', lnot: '¬', neg: '¬',
  therefore: '∴', because: '∵', implies: '⟹', iff: '⟺',
  dots: '…', ldots: '…', cdots: '⋯', vdots: '⋮', ddots: '⋱',
  angle: '∠', triangle: '△', perp: '⊥', parallel: '∥', mid: '∣',
  degree: '°', circ: '∘', prime: '′', aleph: 'ℵ', hbar: 'ℏ', ell: 'ℓ',
  Re: 'ℜ', Im: 'ℑ', wp: '℘', top: '⊤', bot: '⊥', models: '⊨', vdash: '⊢',
  langle: '⟨', rangle: '⟩', lfloor: '⌊', rfloor: '⌋', lceil: '⌈', rceil: '⌉',
  quad: '\u2003', qquad: '\u2003\u2003', ',': '\u2009', ';': '\u2005', ':': '\u2004', '!': '',
  '%': '%', '$': '$', '&': '&', '#': '#', '_': '_', '{': '{', '}': '}',
};

const LATEX_ACCENTS: Record<string, string> = {
  hat: '\u0302',
  widehat: '\u0302',
  bar: '\u0304',
  overline: '\u0304',
  vec: '\u20d7',
  tilde: '\u0303',
  widetilde: '\u0303',
  dot: '\u0307',
  ddot: '\u0308',
};

function readLatexArg(src: string, pos: number): { arg: string; next: number } {
  let p = pos;
  while (p < src.length && (src[p] === ' ' || src[p] === '\t')) p++;
  if (p >= src.length) return { arg: '', next: p };
  if (src[p] !== '{') {
    if (src[p] === '\\') {
      let end = p + 1;
      while (end < src.length && /[a-zA-Z]/.test(src[end])) end++;
      if (end === p + 1 && end < src.length) end++;
      return { arg: src.slice(p, end), next: end };
    }
    return { arg: src[p], next: p + 1 };
  }
  let depth = 0;
  const start = p + 1;
  for (let i = p; i < src.length; i++) {
    if (src[i] === '\\' && i + 1 < src.length) {
      i++;
      continue;
    }
    if (src[i] === '{') depth++;
    else if (src[i] === '}') {
      depth--;
      if (depth === 0) return { arg: src.slice(start, i), next: i + 1 };
    }
  }
  return { arg: src.slice(start), next: src.length };
}

function renderMathHtml(expr: string, displayMode: boolean, depth = 0): string {
  if (!expr) return '';
  if (depth > 8) return escHtml(expr);

  let out = '';
  let i = 0;
  const n = expr.length;

  while (i < n) {
    const ch = expr[i];

    if (ch === '\\') {
      if (i + 1 < n && expr[i + 1] === '\\') {
        out += displayMode ? '<br>' : ' ';
        i += 2;
        continue;
      }
      if (i + 1 < n && !/[a-zA-Z]/.test(expr[i + 1])) {
        const sym = expr[i + 1];
        out += escHtml(LATEX_SYMBOLS[sym] ?? sym);
        i += 2;
        continue;
      }
      let end = i + 1;
      while (end < n && /[a-zA-Z]/.test(expr[end])) end++;
      const cmd = expr.slice(i + 1, end);
      i = end;

      if (cmd === 'left' || cmd === 'right' || cmd === 'big' || cmd === 'Big' || cmd === 'bigg' || cmd === 'Bigg') {
        continue;
      }
      if (cmd === 'frac' || cmd === 'dfrac' || cmd === 'tfrac') {
        const r1 = readLatexArg(expr, i);
        const r2 = readLatexArg(expr, r1.next);
        i = r2.next;
        out += `<span class="md-frac"><span class="md-frac-num">${renderMathHtml(r1.arg, false, depth + 1)}</span><span class="md-frac-den">${renderMathHtml(r2.arg, false, depth + 1)}</span></span>`;
        continue;
      }
      if (cmd === 'sqrt') {
        if (i < n && expr[i] === '[') {
          const closeIdx = expr.indexOf(']', i + 1);
          if (closeIdx !== -1) {
            const deg = expr.slice(i + 1, closeIdx);
            const r1 = readLatexArg(expr, closeIdx + 1);
            i = r1.next;
            out += `<sup class="md-math-deg">${renderMathHtml(deg, false, depth + 1)}</sup><span class="md-sqrt">√<span class="md-sqrt-rad">${renderMathHtml(r1.arg, false, depth + 1)}</span></span>`;
            continue;
          }
        }
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        out += `<span class="md-sqrt">√<span class="md-sqrt-rad">${renderMathHtml(r1.arg, false, depth + 1)}</span></span>`;
        continue;
      }
      if (cmd === 'text' || cmd === 'mathrm' || cmd === 'operatorname' || cmd === 'textrm') {
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        out += `<span class="md-math-rm">${escHtml(r1.arg)}</span>`;
        continue;
      }
      if (cmd === 'mathbf' || cmd === 'boldsymbol' || cmd === 'textbf') {
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        out += `<strong>${renderMathHtml(r1.arg, false, depth + 1)}</strong>`;
        continue;
      }
      if (cmd === 'mathbb' || cmd === 'mathcal') {
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        out += `<span class="md-math-rm">${escHtml(r1.arg)}</span>`;
        continue;
      }
      if (cmd in LATEX_ACCENTS) {
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        const inner = renderMathHtml(r1.arg, false, depth + 1);
        out += `${inner}${LATEX_ACCENTS[cmd]}`;
        continue;
      }
      if (cmd === 'binom') {
        const r1 = readLatexArg(expr, i);
        const r2 = readLatexArg(expr, r1.next);
        i = r2.next;
        out += `(${renderMathHtml(r1.arg, false, depth + 1)} ; ${renderMathHtml(r2.arg, false, depth + 1)})`;
        continue;
      }
      if (cmd === 'sin' || cmd === 'cos' || cmd === 'tan' || cmd === 'cot' || cmd === 'sec' || cmd === 'csc' ||
          cmd === 'log' || cmd === 'ln' || cmd === 'exp' || cmd === 'lim' || cmd === 'max' || cmd === 'min' ||
          cmd === 'sup' || cmd === 'inf' || cmd === 'arg' || cmd === 'det' || cmd === 'dim' || cmd === 'mod') {
        out += `<span class="md-math-rm">${escHtml(cmd)}</span>`;
        continue;
      }
      if (cmd in LATEX_SYMBOLS) {
        out += escHtml(LATEX_SYMBOLS[cmd]);
      } else {
        out += `<span class="md-math-rm">${escHtml(cmd)}</span>`;
      }
      continue;
    }

    if (ch === '^' || ch === '_') {
      const r1 = readLatexArg(expr, i + 1);
      i = r1.next;
      const tag = ch === '^' ? 'sup' : 'sub';
      out += `<${tag}>${renderMathHtml(r1.arg, false, depth + 1)}</${tag}>`;
      continue;
    }

    if (ch === '{') {
      const r1 = readLatexArg(expr, i);
      i = r1.next;
      out += renderMathHtml(r1.arg, false, depth + 1);
      continue;
    }
    if (ch === '}') {
      i++;
      continue;
    }
    if (ch === '\n') {
      out += displayMode ? '<br>' : ' ';
      i++;
      continue;
    }

    out += escHtml(ch);
    i++;
  }
  return out;
}

function findClosingDelimiter(src: string, from: number, delim: string, checkWordBoundary: boolean): number {
  let i = from;
  while (i <= src.length - delim.length) {
    if (src[i] === '\\' && i + 1 < src.length) {
      i += 2;
      continue;
    }
    if (src[i] === '`') {
      let ticks = 0;
      while (i + ticks < src.length && src[i + ticks] === '`') ticks++;
      const end = src.indexOf('`'.repeat(ticks), i + ticks);
      if (end !== -1) {
        i = end + ticks;
        continue;
      }
    }
    if (src.startsWith(delim, i)) {
      const prev = i > 0 ? src[i - 1] : undefined;
      if (prev && !/\s/.test(prev)) {
        if (!checkWordBoundary || !isWordChar(src[i + delim.length])) {
          return i;
        }
      }
    }
    i++;
  }
  return -1;
}

function findMatchingParen(src: string, openIdx: number): number {
  let depth = 0;
  let i = openIdx;
  while (i < src.length) {
    const ch = src[i];
    if (ch === '\\' && i + 1 < src.length) {
      i += 2;
      continue;
    }
    if (ch === '(') depth++;
    else if (ch === ')') {
      depth--;
      if (depth === 0) return i;
    } else if (ch === '\n') {
      return -1;
    }
    i++;
  }
  return -1;
}

function parseLinkTarget(rawInsideParens: string): string {
  const trimmed = rawInsideParens.trim();
  if (!trimmed) return '';
  if (trimmed.startsWith('<')) {
    const endAngle = trimmed.indexOf('>');
    if (endAngle > 1) return trimmed.slice(1, endAngle).trim();
  }
  const spaceIdx = trimmed.search(/\s/);
  return spaceIdx === -1 ? trimmed : trimmed.slice(0, spaceIdx);
}

function splitTrailingPunct(url: string): { url: string; trailing: string } {
  let u = url;
  let trailing = '';
  while (u.length > 0) {
    const last = u[u.length - 1];
    if ('.,;:!?。，；：！？、）】」』》"\''.includes(last)) {
      trailing = last + trailing;
      u = u.slice(0, -1);
    } else if (last === ')') {
      const opens = (u.match(/\(/g) || []).length;
      const closes = (u.match(/\)/g) || []).length;
      if (closes > opens) {
        trailing = ')' + trailing;
        u = u.slice(0, -1);
      } else {
        break;
      }
    } else {
      break;
    }
  }
  return { url: u, trailing };
}

const SAFE_INLINE_TAG_RE = /^<(sup|sub|u|s|del|b|strong|i|em|code|kbd|mark)>([\s\S]*?)<\/\1>/i;

export interface MarkdownLabels {
  details?: string;
  image?: string;
}

function inlineMd(src: string, depth = 0, imageLabel = 'Image'): string {
  if (!src) return '';
  if (depth > 8) return escHtml(src);

  let out = '';
  let i = 0;
  const n = src.length;

  while (i < n) {
    const ch = src[i];

    // 1. Backslash escape
    if (ch === '\\' && i + 1 < n && /[\\`*_{}[\]()#+\-.!|>~$]/.test(src[i + 1])) {
      out += escHtml(src[i + 1]);
      i += 2;
      continue;
    }

    // 2. Inline code (`...` or ``...``) — never parse inner markdown
    if (ch === '`') {
      let ticks = 0;
      while (i + ticks < n && src[i + ticks] === '`') ticks++;
      const marker = '`'.repeat(ticks);
      const end = src.indexOf(marker, i + ticks);
      if (end !== -1) {
        let content = src.slice(i + ticks, end);
        if (ticks >= 2 && content.startsWith(' ') && content.endsWith(' ') && content.trim().length > 0) {
          content = content.slice(1, -1);
        }
        out += `<code>${escHtml(content)}</code>`;
        i = end + ticks;
        continue;
      }
    }

    // 3. <br> / safe attribute-free HTML tags / autolink <https://...>
    if (ch === '<') {
      const slice = src.slice(i);
      const brMatch = slice.match(/^<br\s*\/?>/i);
      if (brMatch) {
        out += '<br>';
        i += brMatch[0].length;
        continue;
      }
      const safeTagMatch = slice.match(SAFE_INLINE_TAG_RE);
      if (safeTagMatch) {
        const tag = safeTagMatch[1].toLowerCase();
        const inner = safeTagMatch[2];
        const renderedInner = (tag === 'code' || tag === 'kbd') ? escHtml(inner) : inlineMd(inner, depth + 1, imageLabel);
        out += `<${tag}>${renderedInner}</${tag}>`;
        i += safeTagMatch[0].length;
        continue;
      }
      const autoMatch = slice.match(/^<((?:https?:\/\/|mailto:)[^>\s]+)>/i);
      if (autoMatch) {
        const url = autoMatch[1];
        if (isSafeUrl(url, true)) {
          const safe = escHtml(url);
          out += `<a href="${safe}" target="_blank" rel="noopener noreferrer">${safe}</a>`;
          i += autoMatch[0].length;
          continue;
        }
      }
    }

    // 4. Markdown Image ![alt](url)
    if (ch === '!' && i + 1 < n && src[i + 1] === '[') {
      const closeBracket = src.indexOf(']', i + 2);
      if (closeBracket !== -1 && closeBracket + 1 < n && src[closeBracket + 1] === '(') {
        const closeParen = findMatchingParen(src, closeBracket + 1);
        if (closeParen !== -1) {
          const alt = src.slice(i + 2, closeBracket).trim();
          const url = parseLinkTarget(src.slice(closeBracket + 2, closeParen));
          if (isSafeUrl(url, false)) {
            const safeUrl = escHtml(url);
            const safeAlt = escHtml(alt || 'image');
            out += `<a href="${safeUrl}" target="_blank" rel="noopener noreferrer"><img class="md-img" src="${safeUrl}" alt="${safeAlt}" loading="lazy"></a>`;
          } else {
            out += `<span class="md-img-fallback">${escHtml(alt ? `[${imageLabel}: ${alt}]` : `[${imageLabel}]`)}</span>`;
          }
          i = closeParen + 1;
          continue;
        }
      }
    }

    // 5. Footnote ref [^id]
    if (ch === '[' && i + 1 < n && src[i + 1] === '^') {
      const fnMatch = src.slice(i).match(/^\[\^([^\]]+)\](?!:)/);
      if (fnMatch) {
        out += `<sup class="md-fn-ref">${escHtml(fnMatch[1])}</sup>`;
        i += fnMatch[0].length;
        continue;
      }
    }

    // 6. Link [label](url)
    if (ch === '[') {
      const closeBracket = src.indexOf(']', i + 1);
      if (closeBracket !== -1 && closeBracket + 1 < n && src[closeBracket + 1] === '(') {
        const closeParen = findMatchingParen(src, closeBracket + 1);
        if (closeParen !== -1) {
          const label = src.slice(i + 1, closeBracket);
          const url = parseLinkTarget(src.slice(closeBracket + 2, closeParen));
          if (isSafeUrl(url, true)) {
            const safeUrl = escHtml(url);
            out += `<a href="${safeUrl}" target="_blank" rel="noopener noreferrer">${inlineMd(label, depth + 1, imageLabel)}</a>`;
            i = closeParen + 1;
            continue;
          }
        }
      }
    }

    // 7. Inline Math $...$ (avoid currency like $100 - $200)
    if (ch === '$' && (i === 0 || src[i - 1] !== '$') && i + 1 < n && src[i + 1] !== '$' && !/\s/.test(src[i + 1])) {
      let end = -1;
      for (let j = i + 1; j < n; j++) {
        if (src[j] === '\\' && j + 1 < n) {
          j++;
          continue;
        }
        if (src[j] === '\n') break;
        if (src[j] === '$') {
          if (j > i + 1 && !/\s/.test(src[j - 1]) && !(j + 1 < n && /\d/.test(src[j + 1]))) {
            end = j;
          }
          break;
        }
      }
      if (end !== -1) {
        const expr = src.slice(i + 1, end);
        if (!/^\d+(?:[.,]\d+)?$/.test(expr)) {
          out += `<span class="md-math-inline">${renderMathHtml(expr, false)}</span>`;
          i = end + 1;
          continue;
        }
      }
    }

    // 8. Bold+Italic ***...*** or ___...___
    if ((src.startsWith('***', i) || src.startsWith('___', i)) && i + 3 < n && !/\s/.test(src[i + 3])) {
      const delim = src.slice(i, i + 3);
      const isUnder = delim === '___';
      if (!isUnder || !isWordChar(i > 0 ? src[i - 1] : undefined)) {
        const end = findClosingDelimiter(src, i + 3, delim, isUnder);
        if (end !== -1) {
          out += `<strong><em>${inlineMd(src.slice(i + 3, end), depth + 1, imageLabel)}</em></strong>`;
          i = end + 3;
          continue;
        }
      }
    }

    // 9. Bold **...** or __...__
    if ((src.startsWith('**', i) || src.startsWith('__', i)) && i + 2 < n && !/\s/.test(src[i + 2])) {
      const delim = src.slice(i, i + 2);
      const isUnder = delim === '__';
      if (!isUnder || !isWordChar(i > 0 ? src[i - 1] : undefined)) {
        const end = findClosingDelimiter(src, i + 2, delim, isUnder);
        if (end !== -1) {
          out += `<strong>${inlineMd(src.slice(i + 2, end), depth + 1, imageLabel)}</strong>`;
          i = end + 2;
          continue;
        }
      }
    }

    // 10. Strikethrough ~~...~~
    if (src.startsWith('~~', i) && i + 2 < n && !/\s/.test(src[i + 2])) {
      const end = findClosingDelimiter(src, i + 2, '~~', false);
      if (end !== -1) {
        out += `<del>${inlineMd(src.slice(i + 2, end), depth + 1, imageLabel)}</del>`;
        i = end + 2;
        continue;
      }
    }

    // 11. Italic *...* or _..._
    if ((ch === '*' || ch === '_') && i + 1 < n && !/\s/.test(src[i + 1]) && src[i + 1] !== ch) {
      const isUnder = ch === '_';
      if (!isUnder || !isWordChar(i > 0 ? src[i - 1] : undefined)) {
        const end = findClosingDelimiter(src, i + 1, ch, isUnder);
        if (end !== -1) {
          out += `<em>${inlineMd(src.slice(i + 1, end), depth + 1, imageLabel)}</em>`;
          i = end + 1;
          continue;
        }
      }
    }

    // 12. Bare URL https://... or http://...
    if ((src.startsWith('https://', i) || src.startsWith('http://', i)) && (i === 0 || /[\s(]/.test(src[i - 1]))) {
      const urlMatch = src.slice(i).match(/^https?:\/\/[^\s<]+/);
      if (urlMatch) {
        const { url, trailing } = splitTrailingPunct(urlMatch[0]);
        if (url.length > 8) {
          const safe = escHtml(url);
          out += `<a href="${safe}" target="_blank" rel="noopener noreferrer">${safe}</a>${escHtml(trailing)}`;
          i += urlMatch[0].length;
          continue;
        }
      }
    }

    out += escHtml(ch);
    i++;
  }

  return out;
}

function splitTableCells(line: string): string[] {
  let s = line.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1);

  const cells: string[] = [];
  let cur = '';
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '\\' && i + 1 < s.length && s[i + 1] === '|') {
      cur += '|';
      i++;
      continue;
    }
    if (s[i] === '|') {
      cells.push(cur.trim());
      cur = '';
    } else {
      cur += s[i];
    }
  }
  cells.push(cur.trim());
  return cells;
}

function parseTableAlignments(line: string): Array<'left' | 'center' | 'right'> | null {
  const trimmed = line.trim();
  if (!trimmed.includes('|')) return null;
  const cells = splitTableCells(trimmed);
  if (cells.length === 0) return null;
  const aligns: Array<'left' | 'center' | 'right'> = [];
  for (const c of cells) {
    if (!/^:?-{3,}:?$/.test(c)) return null;
    if (c.startsWith(':') && c.endsWith(':')) aligns.push('center');
    else if (c.endsWith(':')) aligns.push('right');
    else aligns.push('left');
  }
  return aligns;
}

function calcIndentLevel(leading: string): number {
  let spaces = 0;
  for (const ch of leading) {
    spaces += ch === '\t' ? 2 : 1;
  }
  return Math.min(4, Math.floor(spaces / 2));
}

interface ListItem {
  ordered: boolean;
  marker: string;
  task: boolean;
  checked: boolean;
  indent: number;
  text: string;
}

function parseListLine(line: string): ListItem | null {
  const m = line.match(/^([ \t]*)(?:([-*+])|(\d+)[.)])\s+(.*)$/);
  if (!m) return null;
  const indent = calcIndentLevel(m[1] || '');
  const ordered = Boolean(m[3]);
  const marker = ordered ? `${m[3]}.` : '•';
  let text = m[4] || '';
  let task = false;
  let checked = false;
  const tm = text.match(/^\[([xX ])\]\s+(.*)$/);
  if (tm) {
    task = true;
    checked = tm[1].toLowerCase() === 'x';
    text = tm[2];
  }
  return { ordered, marker, task, checked, indent, text };
}

function renderBlocks(lines: string[], copyLabel: string, labels: Required<MarkdownLabels>): string[] {
  const { details: detailsLabel, image: imageLabel } = labels;
  const out: string[] = [];
  let para: string[] = [];
  let list: ListItem[] = [];

  const flushPara = () => {
    if (para.length) {
      out.push(`<p>${para.map((ln) => inlineMd(ln, 0, imageLabel)).join('<br>')}</p>`);
      para = [];
    }
  };

  const flushList = () => {
    if (!list.length) return;
    const rows = list
      .map((item) => {
        const cls = [
          'md-li',
          item.indent > 0 ? `md-indent-${item.indent}` : '',
          item.task ? 'md-task' : '',
          item.task && item.checked ? 'is-checked' : '',
        ]
          .filter(Boolean)
          .join(' ');
        const markerHtml = item.task
          ? `<span class="md-check${item.checked ? ' is-checked' : ''}" aria-hidden="true">${item.checked ? '✓' : ''}</span>`
          : `<span class="md-li-mark">${escHtml(item.marker)}</span>`;
        return `<div class="${cls}">${markerHtml}<div class="md-li-body">${inlineMd(item.text, 0, imageLabel)}</div></div>`;
      })
      .join('');
    out.push(`<div class="md-list">${rows}</div>`);
    list = [];
  };

  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();

    // 1. Fenced code block (``` or ~~~)
    const fenceMatch = trimmed.match(/^(`{3,}|~{3,})(.*)$/);
    if (fenceMatch) {
      flushPara();
      flushList();
      const fenceChar = fenceMatch[1][0];
      const fenceLen = fenceMatch[1].length;
      const lang = fenceMatch[2].trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length) {
        const closeTrim = lines[i].trim();
        const closeRe = new RegExp(`^${fenceChar}{${fenceLen},}\\s*$`);
        if (closeRe.test(closeTrim)) {
          i++;
          break;
        }
        codeLines.push(lines[i]);
        i++;
      }
      out.push(
        `<div class="code"><div class="code-h"><span>${escHtml(lang || 'code')}</span><button type="button" data-copy>${escHtml(copyLabel)}</button></div><pre><code>${escHtml(codeLines.join('\n'))}</code></pre></div>`
      );
      continue;
    }

    // 2. Block math $$...$$
    if (trimmed.startsWith('$$')) {
      flushPara();
      flushList();
      if (trimmed.length > 4 && trimmed.endsWith('$$')) {
        out.push(`<div class="md-math">${renderMathHtml(trimmed.slice(2, -2).trim(), true)}</div>`);
        i++;
        continue;
      }
      const mathLines: string[] = [];
      const firstRest = trimmed.slice(2).trim();
      if (firstRest) mathLines.push(firstRest);
      i++;
      while (i < lines.length) {
        const curTrim = lines[i].trim();
        if (curTrim.endsWith('$$')) {
          const tail = curTrim.slice(0, -2).trim();
          if (tail) mathLines.push(tail);
          i++;
          break;
        }
        mathLines.push(lines[i]);
        i++;
      }
      out.push(`<div class="md-math">${renderMathHtml(mathLines.join('\n').trim(), true)}</div>`);
      continue;
    }

    // 3. <details>...</details> collapsible block
    const detailsOpenMatch = trimmed.match(/^<details(\s+open)?\s*>(.*)$/i);
    if (detailsOpenMatch) {
      const hasEndOnSameOrLater = (() => {
        for (let k = i; k < lines.length; k++) {
          if (/<\/details>/i.test(lines[k])) return k;
        }
        return -1;
      })();
      if (hasEndOnSameOrLater !== -1) {
        flushPara();
        flushList();
        const isOpen = Boolean(detailsOpenMatch[1]);
        const collected: string[] = [];
        if (detailsOpenMatch[2]) collected.push(detailsOpenMatch[2]);
        for (let k = i + 1; k <= hasEndOnSameOrLater; k++) {
          collected.push(lines[k]);
        }
        const rawBlock = collected.join('\n').replace(/<\/details>\s*$/i, '').trim();
        let summaryText = detailsLabel;
        let bodyText = rawBlock;
        const sumMatch = rawBlock.match(/<summary>([\s\S]*?)<\/summary>/i);
        if (sumMatch) {
          summaryText = sumMatch[1].trim() || detailsLabel;
          bodyText = rawBlock.replace(sumMatch[0], '').trim();
        }
        const bodyHtml = bodyText ? renderBlocks(bodyText.split('\n'), copyLabel, labels).join('') : '';
        out.push(
          `<details class="md-details"${isOpen ? ' open' : ''}><summary>${inlineMd(summaryText, 0, imageLabel)}</summary>${bodyHtml ? `<div class="md-details-body">${bodyHtml}</div>` : ''}</details>`
        );
        i = hasEndOnSameOrLater + 1;
        continue;
      }
    }

    // 4. Blank line
    if (!trimmed) {
      flushPara();
      flushList();
      i++;
      continue;
    }

    // 5. Headings H1-H6
    const hMatch = trimmed.match(/^(#{1,6})\s+(.*)$/);
    if (hMatch) {
      flushPara();
      flushList();
      const level = hMatch[1].length;
      out.push(`<h${level}>${inlineMd(hMatch[2], 0, imageLabel)}</h${level}>`);
      i++;
      continue;
    }

    // 6. Horizontal rule --- / *** / ___
    if (/^(?:-\s*){3,}$|^(?:\*\s*){3,}$|^(?:_\s*){3,}$/.test(trimmed)) {
      flushPara();
      flushList();
      out.push('<hr>');
      i++;
      continue;
    }

    // 7. GFM Table
    if (trimmed.includes('|') && i + 1 < lines.length) {
      const aligns = parseTableAlignments(lines[i + 1]);
      if (aligns) {
        const headers = splitTableCells(line);
        if (headers.length > 0) {
          flushPara();
          flushList();
          const colCount = Math.max(headers.length, aligns.length);
          const ths = Array.from({ length: colCount }, (_, idx) => {
            const align = aligns[idx] || 'left';
            return `<th style="text-align:${align}">${inlineMd(headers[idx] || '', 0, imageLabel)}</th>`;
          }).join('');
          const bodyRows: string[] = [];
          i += 2;
          while (i < lines.length) {
            const rowTrim = lines[i].trim();
            if (!rowTrim || !rowTrim.includes('|')) break;
            if (parseTableAlignments(lines[i])) break;
            const cells = splitTableCells(lines[i]);
            const tds = Array.from({ length: colCount }, (_, idx) => {
              const align = aligns[idx] || 'left';
              return `<td style="text-align:${align}">${inlineMd(cells[idx] || '', 0, imageLabel)}</td>`;
            }).join('');
            bodyRows.push(`<tr>${tds}</tr>`);
            i++;
          }
          out.push(
            `<div class="md-table-wrap"><table><thead><tr>${ths}</tr></thead>${bodyRows.length ? `<tbody>${bodyRows.join('')}</tbody>` : ''}</table></div>`
          );
          continue;
        }
      }
    }

    // 8. Blockquote (supports multiline + nested >>)
    if (trimmed.startsWith('>')) {
      flushPara();
      flushList();
      const quoteLines: string[] = [];
      while (i < lines.length && lines[i].trim().startsWith('>')) {
        quoteLines.push(lines[i].trim().replace(/^>\s?/, ''));
        i++;
      }
      out.push(`<blockquote>${renderBlocks(quoteLines, copyLabel, labels).join('')}</blockquote>`);
      continue;
    }

    // 9. List (unordered / ordered / task / indented)
    const listItem = parseListLine(line);
    if (listItem) {
      flushPara();
      list.push(listItem);
      i++;
      continue;
    }

    // 10. Footnote definition [^id]: ...
    const fnDefMatch = trimmed.match(/^\[\^([^\]]+)\]:\s*(.*)$/);
    if (fnDefMatch) {
      flushPara();
      flushList();
      out.push(
        `<div class="md-footnote"><span class="md-fn-id">${escHtml(fnDefMatch[1])}</span><div class="md-fn-body">${inlineMd(fnDefMatch[2], 0, imageLabel)}</div></div>`
      );
      i++;
      continue;
    }

    // 11. Normal paragraph line
    flushList();
    para.push(line);
    i++;
  }

  flushPara();
  flushList();
  return out;
}

export function renderMarkdownHtml(text: string, copyLabel: string, labels?: MarkdownLabels): string {
  const normalized = String(text || '').replace(/\r\n/g, '\n');
  const resolved: Required<MarkdownLabels> = {
    details: labels?.details || 'Details',
    image: labels?.image || 'Image',
  };
  return `<div class="md">${renderBlocks(normalized.split('\n'), copyLabel, resolved).join('')}</div>`;
}
