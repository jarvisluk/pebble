import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import React, { useState } from 'react';
import { Image, Linking, Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { useI18n } from '../i18n';
import { MONO_FONT, type Theme } from '../theme';

type TableAlign = 'left' | 'center' | 'right';

interface ListItem {
  marker: string;
  task: boolean;
  checked: boolean;
  indent: number;
  text: string;
}

type Block =
  | { type: 'code'; lang: string; code: string }
  | { type: 'math'; expr: string }
  | { type: 'details'; summary: string; isOpen: boolean; body: Block[] }
  | { type: 'heading'; level: number; text: string }
  | { type: 'rule' }
  | { type: 'quote'; level: number; text: string }
  | { type: 'table'; headers: string[]; aligns: TableAlign[]; rows: string[][] }
  | { type: 'list'; items: ListItem[] }
  | { type: 'footnote'; id: string; text: string }
  | { type: 'image'; alt: string; url: string }
  | { type: 'para'; text: string };

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
  quad: '  ', qquad: '    ', ',': ' ', ';': ' ', ':': ' ', '!': '',
  '%': '%', '$': '$', '&': '&', '#': '#', '_': '_', '{': '{', '}': '}',
};

const LATEX_ACCENTS: Record<string, string> = {
  hat: '\u0302', widehat: '\u0302',
  bar: '\u0304', overline: '\u0304',
  vec: '\u20d7',
  tilde: '\u0303', widetilde: '\u0303',
  dot: '\u0307', ddot: '\u0308',
};

const SUP_MAP: Record<string, string> = {
  '0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
  '+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
  a: 'ᵃ', b: 'ᵇ', c: 'ᶜ', d: 'ᵈ', e: 'ᵉ', f: 'ᶠ', g: 'ᵍ', h: 'ʰ', i: 'ⁱ', j: 'ʲ', k: 'ᵏ', l: 'ˡ', m: 'ᵐ',
  n: 'ⁿ', o: 'ᵒ', p: 'ᵖ', r: 'ʳ', s: 'ˢ', t: 'ᵗ', u: 'ᵘ', v: 'ᵛ', w: 'ʷ', x: 'ˣ', y: 'ʸ', z: 'ᶻ',
};

const SUB_MAP: Record<string, string> = {
  '0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
  '+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
  a: 'ₐ', e: 'ₑ', h: 'ₕ', i: 'ᵢ', j: 'ⱼ', k: 'ₖ', l: 'ₗ', m: 'ₘ', n: 'ₙ', o: 'ₒ', p: 'ₚ', r: 'ᵣ', s: 'ₛ', t: 'ₜ', u: 'ᵤ', v: 'ᵥ', x: 'ₓ',
};

function toScriptUnicode(text: string, isSup: boolean): string | null {
  const map = isSup ? SUP_MAP : SUB_MAP;
  let out = '';
  for (const ch of text) {
    if (ch in map) out += map[ch];
    else return null;
  }
  return out;
}

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

function latexToText(expr: string, depth = 0): string {
  if (!expr) return '';
  if (depth > 8) return expr;

  let out = '';
  let i = 0;
  const n = expr.length;
  while (i < n) {
    const ch = expr[i];
    if (ch === '\\') {
      if (i + 1 < n && expr[i + 1] === '\\') {
        out += '\n';
        i += 2;
        continue;
      }
      if (i + 1 < n && !/[a-zA-Z]/.test(expr[i + 1])) {
        const sym = expr[i + 1];
        out += LATEX_SYMBOLS[sym] ?? sym;
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
        const a = latexToText(r1.arg, depth + 1).trim();
        const b = latexToText(r2.arg, depth + 1).trim();
        const wa = a.length > 1 && /[+\-*/=<> ]/.test(a) ? `(${a})` : a;
        const wb = b.length > 1 && /[+\-*/=<> ]/.test(b) ? `(${b})` : b;
        out += `${wa}/${wb}`;
        continue;
      }
      if (cmd === 'sqrt') {
        let degPrefix = '';
        if (i < n && expr[i] === '[') {
          const closeIdx = expr.indexOf(']', i + 1);
          if (closeIdx !== -1) {
            const deg = latexToText(expr.slice(i + 1, closeIdx), depth + 1);
            degPrefix = toScriptUnicode(deg, true) ?? deg;
            i = closeIdx + 1;
          }
        }
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        const inner = latexToText(r1.arg, depth + 1).trim();
        out += `${degPrefix}√${inner.length <= 1 ? inner : `(${inner})`}`;
        continue;
      }
      if (cmd === 'text' || cmd === 'mathrm' || cmd === 'operatorname' || cmd === 'textrm' || cmd === 'mathbf' || cmd === 'boldsymbol' || cmd === 'textbf' || cmd === 'mathbb' || cmd === 'mathcal') {
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        out += cmd === 'text' || cmd === 'mathrm' || cmd === 'textrm' ? r1.arg : latexToText(r1.arg, depth + 1);
        continue;
      }
      if (cmd in LATEX_ACCENTS) {
        const r1 = readLatexArg(expr, i);
        i = r1.next;
        out += `${latexToText(r1.arg, depth + 1)}${LATEX_ACCENTS[cmd]}`;
        continue;
      }
      if (cmd === 'sin' || cmd === 'cos' || cmd === 'tan' || cmd === 'cot' || cmd === 'sec' || cmd === 'csc' ||
          cmd === 'log' || cmd === 'ln' || cmd === 'exp' || cmd === 'lim' || cmd === 'max' || cmd === 'min' ||
          cmd === 'sup' || cmd === 'inf' || cmd === 'arg' || cmd === 'det' || cmd === 'dim' || cmd === 'mod') {
        out += cmd;
        continue;
      }
      out += LATEX_SYMBOLS[cmd] ?? cmd;
      continue;
    }

    if (ch === '^' || ch === '_') {
      const r1 = readLatexArg(expr, i + 1);
      i = r1.next;
      const inner = latexToText(r1.arg, depth + 1);
      const uni = toScriptUnicode(inner, ch === '^');
      out += uni ?? (inner.length <= 1 ? `${ch}${inner}` : `${ch}(${inner})`);
      continue;
    }
    if (ch === '{') {
      const r1 = readLatexArg(expr, i);
      i = r1.next;
      out += latexToText(r1.arg, depth + 1);
      continue;
    }
    if (ch === '}') {
      i++;
      continue;
    }
    out += ch;
    i++;
  }
  return out;
}

type MathSeg = { type: 'text'; value: string } | { type: 'frac'; num: string; den: string };

function splitTopLevelFracs(line: string): MathSeg[] {
  const segs: MathSeg[] = [];
  let cur = '';
  let i = 0;
  while (i < line.length) {
    if (line[i] === '\\') {
      let end = i + 1;
      while (end < line.length && /[a-zA-Z]/.test(line[end])) end++;
      const cmd = line.slice(i + 1, end);
      if (cmd === 'frac' || cmd === 'dfrac' || cmd === 'tfrac') {
        if (cur) {
          segs.push({ type: 'text', value: cur });
          cur = '';
        }
        const r1 = readLatexArg(line, end);
        const r2 = readLatexArg(line, r1.next);
        segs.push({ type: 'frac', num: r1.arg, den: r2.arg });
        i = r2.next;
        continue;
      }
    }
    cur += line[i];
    i++;
  }
  if (cur) segs.push({ type: 'text', value: cur });
  return segs;
}

function MathBlockView({ expr, theme, baseColor }: { expr: string; theme: Theme; baseColor: string }) {
  const lines = expr
    .replace(/\\\\/g, '\n')
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean);

  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      style={[styles.mathBlock, { backgroundColor: theme.s1 }]}
      contentContainerStyle={styles.mathBlockContent}
    >
      <View style={styles.mathLines}>
        {lines.map((line, li) => {
          const segs = splitTopLevelFracs(line);
          return (
            <View key={li} style={styles.mathRow}>
              {segs.map((seg, si) => {
                if (seg.type === 'text') {
                  return (
                    <Text key={si} selectable style={[styles.mathText, { color: baseColor }]}>
                      {latexToText(seg.value)}
                    </Text>
                  );
                }
                return (
                  <View key={si} style={styles.fracWrap}>
                    <Text selectable style={[styles.fracText, { color: baseColor }]}>
                      {latexToText(seg.num)}
                    </Text>
                    <View style={[styles.fracBar, { backgroundColor: baseColor }]} />
                    <Text selectable style={[styles.fracText, { color: baseColor }]}>
                      {latexToText(seg.den)}
                    </Text>
                  </View>
                );
              })}
            </View>
          );
        })}
      </View>
    </ScrollView>
  );
}

function DetailsCard({
  summary,
  initiallyOpen,
  theme,
  children,
}: {
  summary: string;
  initiallyOpen: boolean;
  theme: Theme;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(initiallyOpen);
  return (
    <View style={[styles.detailsBox, { backgroundColor: theme.s1 }]}>
      <Pressable onPress={() => setOpen((v) => !v)} style={styles.detailsHead} hitSlop={6}>
        <Text style={[styles.detailsArrow, { color: theme.mute }]}>{open ? '▾' : '▸'}</Text>
        <Text style={[styles.detailsSummary, { color: theme.text }]}>{summary}</Text>
      </Pressable>
      {open ? <View style={[styles.detailsBody, { borderTopColor: theme.s2 }]}>{children}</View> : null}
    </View>
  );
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

function CodeCard({ lang, code, theme }: { lang: string; code: string; theme: Theme }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    await Clipboard.setStringAsync(code);
    if (Platform.OS !== 'web') {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  };

  return (
    <View style={[styles.codeWrap, { backgroundColor: theme.s1 }]}>
      <View style={[styles.codeHead, { borderBottomColor: theme.s2 }]}>
        <Text style={[styles.codeLang, { color: theme.mute }]}>{lang || 'code'}</Text>
        <Pressable onPress={onCopy} style={[styles.copyBtn, { backgroundColor: theme.card }]} hitSlop={8}>
          <Text style={[styles.copyTxt, { color: theme.text2 }]}>{copied ? t('copied') : t('copy')}</Text>
        </Pressable>
      </View>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.codeBody}>
        <Text selectable style={[styles.codeText, { color: theme.text, fontFamily: MONO_FONT }]}>
          {code}
        </Text>
      </ScrollView>
    </View>
  );
}

function MarkdownImage({ alt, url, theme }: { alt: string; url: string; theme: Theme }) {
  const [failed, setFailed] = useState(false);
  const safeRemote = isSafeUrl(url, false);

  if (safeRemote && !failed) {
    return (
      <Pressable onPress={() => Linking.openURL(url).catch(() => {})}>
        <Image
          source={{ uri: url }}
          accessibilityLabel={alt || 'image'}
          onError={() => setFailed(true)}
          resizeMode="contain"
          style={[styles.mdImage, { backgroundColor: theme.s1 }]}
        />
      </Pressable>
    );
  }

  const label = alt ? `[图片: ${alt}]` : '[图片]';
  if (safeRemote) {
    return (
      <Pressable onPress={() => Linking.openURL(url).catch(() => {})} style={[styles.imgFallback, { backgroundColor: theme.s1 }]}>
        <Text style={[styles.imgFallbackTxt, { color: theme.accent, textDecorationLine: 'underline' }]}>{label}</Text>
      </Pressable>
    );
  }
  return (
    <View style={[styles.imgFallback, { backgroundColor: theme.s1 }]}>
      <Text style={[styles.imgFallbackTxt, { color: theme.text2 }]}>{label}</Text>
    </View>
  );
}

interface InlineStyleFlags {
  bold?: boolean;
  italic?: boolean;
  strike?: boolean;
  underline?: boolean;
}

const SAFE_INLINE_TAG_RE = /^<(sup|sub|u|s|del|b|strong|i|em|code|kbd|mark)>([\s\S]*?)<\/\1>/i;

function renderInline(
  src: string,
  theme: Theme,
  baseColor: string,
  keyPrefix: string,
  flags: InlineStyleFlags = {},
  depth = 0
): React.ReactNode[] {
  if (!src) return [];
  if (depth > 8) return [src];

  const parts: React.ReactNode[] = [];
  let plain = '';
  let i = 0;
  let tokIdx = 0;
  const n = src.length;

  const textDecor =
    flags.strike && flags.underline
      ? ('underline line-through' as const)
      : flags.strike
      ? ('line-through' as const)
      : flags.underline
      ? ('underline' as const)
      : undefined;

  const styleObj = {
    color: flags.strike ? theme.text2 : baseColor,
    fontWeight: flags.bold ? ('700' as const) : undefined,
    fontStyle: flags.italic ? ('italic' as const) : undefined,
    textDecorationLine: textDecor,
  };

  const flushPlain = () => {
    if (!plain) return;
    if (flags.bold || flags.italic || flags.strike || flags.underline) {
      parts.push(
        <Text key={`${keyPrefix}-p-${tokIdx++}`} style={styleObj}>
          {plain}
        </Text>
      );
    } else {
      parts.push(plain);
    }
    plain = '';
  };

  while (i < n) {
    const ch = src[i];

    // 1. Backslash escape
    if (ch === '\\' && i + 1 < n && /[\\`*_{}[\]()#+\-.!|>~$]/.test(src[i + 1])) {
      plain += src[i + 1];
      i += 2;
      continue;
    }

    // 2. Inline code (`...` or ``...``)
    if (ch === '`') {
      let ticks = 0;
      while (i + ticks < n && src[i + ticks] === '`') ticks++;
      const end = src.indexOf('`'.repeat(ticks), i + ticks);
      if (end !== -1) {
        flushPlain();
        let content = src.slice(i + ticks, end);
        if (ticks >= 2 && content.startsWith(' ') && content.endsWith(' ') && content.trim().length > 0) {
          content = content.slice(1, -1);
        }
        parts.push(
          <Text
            key={`${keyPrefix}-c-${tokIdx++}`}
            style={{ fontFamily: MONO_FONT, fontSize: 14, backgroundColor: theme.s2, color: baseColor }}
          >
            {` ${content} `}
          </Text>
        );
        i = end + ticks;
        continue;
      }
    }

    // 3. <br> / safe attribute-free HTML tags / autolink <https://...>
    if (ch === '<') {
      const slice = src.slice(i);
      const brMatch = slice.match(/^<br\s*\/?>/i);
      if (brMatch) {
        plain += '\n';
        i += brMatch[0].length;
        continue;
      }
      const safeTagMatch = slice.match(SAFE_INLINE_TAG_RE);
      if (safeTagMatch) {
        const tag = safeTagMatch[1].toLowerCase();
        const inner = safeTagMatch[2];
        flushPlain();
        if (tag === 'code' || tag === 'kbd') {
          parts.push(
            <Text
              key={`${keyPrefix}-t-${tokIdx++}`}
              style={{ fontFamily: MONO_FONT, fontSize: 13.5, backgroundColor: theme.s2, color: baseColor }}
            >
              {` ${inner} `}
            </Text>
          );
        } else if (tag === 'sup' || tag === 'sub') {
          const uni = toScriptUnicode(inner, tag === 'sup');
          if (uni) {
            parts.push(
              <Text key={`${keyPrefix}-t-${tokIdx++}`} style={{ color: baseColor }}>
                {uni}
              </Text>
            );
          } else {
            parts.push(
              <Text key={`${keyPrefix}-t-${tokIdx++}`} style={{ fontSize: 11, color: baseColor }}>
                {inner}
              </Text>
            );
          }
        } else if (tag === 'mark') {
          parts.push(
            <Text key={`${keyPrefix}-t-${tokIdx++}`} style={{ backgroundColor: theme.s2, color: baseColor }}>
              {renderInline(inner, theme, baseColor, `${keyPrefix}-mk-${tokIdx}`, flags, depth + 1)}
            </Text>
          );
        } else {
          const nextFlags: InlineStyleFlags = {
            ...flags,
            bold: flags.bold || tag === 'b' || tag === 'strong',
            italic: flags.italic || tag === 'i' || tag === 'em',
            strike: flags.strike || tag === 's' || tag === 'del',
            underline: flags.underline || tag === 'u',
          };
          parts.push(...renderInline(inner, theme, baseColor, `${keyPrefix}-t-${tokIdx++}`, nextFlags, depth + 1));
        }
        i += safeTagMatch[0].length;
        continue;
      }
      const autoMatch = slice.match(/^<((?:https?:\/\/|mailto:)[^>\s]+)>/i);
      if (autoMatch && isSafeUrl(autoMatch[1], true)) {
        const href = autoMatch[1];
        flushPlain();
        parts.push(
          <Text
            key={`${keyPrefix}-a-${tokIdx++}`}
            style={{ color: theme.accent, textDecorationLine: 'underline' }}
            onPress={() => Linking.openURL(href).catch(() => {})}
          >
            {href}
          </Text>
        );
        i += autoMatch[0].length;
        continue;
      }
    }

    // 4. Inline image ![alt](url)
    if (ch === '!' && i + 1 < n && src[i + 1] === '[') {
      const closeB = src.indexOf(']', i + 2);
      if (closeB !== -1 && closeB + 1 < n && src[closeB + 1] === '(') {
        const closeP = findMatchingParen(src, closeB + 1);
        if (closeP !== -1) {
          const alt = src.slice(i + 2, closeB).trim();
          const url = parseLinkTarget(src.slice(closeB + 2, closeP));
          flushPlain();
          const label = alt ? `[图片: ${alt}]` : '[图片]';
          if (isSafeUrl(url, false)) {
            parts.push(
              <Text
                key={`${keyPrefix}-img-${tokIdx++}`}
                style={{ color: theme.accent, textDecorationLine: 'underline' }}
                onPress={() => Linking.openURL(url).catch(() => {})}
              >
                {label}
              </Text>
            );
          } else {
            parts.push(
              <Text key={`${keyPrefix}-img-${tokIdx++}`} style={{ color: theme.text2, backgroundColor: theme.s1 }}>
                {` ${label} `}
              </Text>
            );
          }
          i = closeP + 1;
          continue;
        }
      }
    }

    // 5. Footnote ref [^id]
    if (ch === '[' && i + 1 < n && src[i + 1] === '^') {
      const fnMatch = src.slice(i).match(/^\[\^([^\]]+)\](?!:)/);
      if (fnMatch) {
        flushPlain();
        parts.push(
          <Text key={`${keyPrefix}-fn-${tokIdx++}`} style={{ fontSize: 12, color: theme.text2, fontWeight: '600' }}>
            {`[${fnMatch[1]}]`}
          </Text>
        );
        i += fnMatch[0].length;
        continue;
      }
    }

    // 6. Link [label](url)
    if (ch === '[') {
      const closeB = src.indexOf(']', i + 1);
      if (closeB !== -1 && closeB + 1 < n && src[closeB + 1] === '(') {
        const closeP = findMatchingParen(src, closeB + 1);
        if (closeP !== -1) {
          const label = src.slice(i + 1, closeB);
          const url = parseLinkTarget(src.slice(closeB + 2, closeP));
          if (isSafeUrl(url, true)) {
            flushPlain();
            parts.push(
              <Text
                key={`${keyPrefix}-l-${tokIdx++}`}
                style={{ color: theme.accent, textDecorationLine: 'underline' }}
                onPress={() => Linking.openURL(url).catch(() => {})}
              >
                {renderInline(label, theme, theme.accent, `${keyPrefix}-l-${tokIdx}`, flags, depth + 1)}
              </Text>
            );
            i = closeP + 1;
            continue;
          }
        }
      }
    }

    // 7. Inline math $...$ (avoid currency like $100 - $200)
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
          flushPlain();
          parts.push(
            <Text
              key={`${keyPrefix}-m-${tokIdx++}`}
              style={{ fontStyle: 'italic', fontSize: 15, backgroundColor: theme.s1, color: baseColor }}
            >
              {` ${latexToText(expr)} `}
            </Text>
          );
          i = end + 1;
          continue;
        }
      }
    }

    // 8. Bold+Italic *** or ___
    if ((src.startsWith('***', i) || src.startsWith('___', i)) && i + 3 < n && !/\s/.test(src[i + 3])) {
      const delim = src.slice(i, i + 3);
      const isUnder = delim === '___';
      if (!isUnder || !isWordChar(i > 0 ? src[i - 1] : undefined)) {
        const end = findClosingDelimiter(src, i + 3, delim, isUnder);
        if (end !== -1) {
          flushPlain();
          parts.push(
            ...renderInline(
              src.slice(i + 3, end),
              theme,
              baseColor,
              `${keyPrefix}-bi-${tokIdx++}`,
              { ...flags, bold: true, italic: true },
              depth + 1
            )
          );
          i = end + 3;
          continue;
        }
      }
    }

    // 9. Bold ** or __
    if ((src.startsWith('**', i) || src.startsWith('__', i)) && i + 2 < n && !/\s/.test(src[i + 2])) {
      const delim = src.slice(i, i + 2);
      const isUnder = delim === '__';
      if (!isUnder || !isWordChar(i > 0 ? src[i - 1] : undefined)) {
        const end = findClosingDelimiter(src, i + 2, delim, isUnder);
        if (end !== -1) {
          flushPlain();
          parts.push(
            ...renderInline(
              src.slice(i + 2, end),
              theme,
              baseColor,
              `${keyPrefix}-b-${tokIdx++}`,
              { ...flags, bold: true },
              depth + 1
            )
          );
          i = end + 2;
          continue;
        }
      }
    }

    // 10. Strikethrough ~~
    if (src.startsWith('~~', i) && i + 2 < n && !/\s/.test(src[i + 2])) {
      const end = findClosingDelimiter(src, i + 2, '~~', false);
      if (end !== -1) {
        flushPlain();
        parts.push(
          ...renderInline(
            src.slice(i + 2, end),
            theme,
            baseColor,
            `${keyPrefix}-s-${tokIdx++}`,
            { ...flags, strike: true },
            depth + 1
          )
        );
        i = end + 2;
        continue;
      }
    }

    // 11. Italic * or _
    if ((ch === '*' || ch === '_') && i + 1 < n && !/\s/.test(src[i + 1]) && src[i + 1] !== ch) {
      const isUnder = ch === '_';
      if (!isUnder || !isWordChar(i > 0 ? src[i - 1] : undefined)) {
        const end = findClosingDelimiter(src, i + 1, ch, isUnder);
        if (end !== -1) {
          flushPlain();
          parts.push(
            ...renderInline(
              src.slice(i + 1, end),
              theme,
              baseColor,
              `${keyPrefix}-i-${tokIdx++}`,
              { ...flags, italic: true },
              depth + 1
            )
          );
          i = end + 1;
          continue;
        }
      }
    }

    // 12. Bare URL
    if ((src.startsWith('https://', i) || src.startsWith('http://', i)) && (i === 0 || /[\s(]/.test(src[i - 1]))) {
      const urlMatch = src.slice(i).match(/^https?:\/\/[^\s<]+/);
      if (urlMatch) {
        const { url, trailing } = splitTrailingPunct(urlMatch[0]);
        if (url.length > 8) {
          flushPlain();
          parts.push(
            <Text
              key={`${keyPrefix}-u-${tokIdx++}`}
              style={{ color: theme.accent, textDecorationLine: 'underline' }}
              onPress={() => Linking.openURL(url).catch(() => {})}
            >
              {url}
            </Text>
          );
          plain += trailing;
          i += urlMatch[0].length;
          continue;
        }
      }
    }

    plain += ch;
    i++;
  }

  flushPlain();
  return parts;
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

function parseTableAlignments(line: string): TableAlign[] | null {
  const trimmed = line.trim();
  if (!trimmed.includes('|')) return null;
  const cells = splitTableCells(trimmed);
  if (cells.length === 0) return null;
  const aligns: TableAlign[] = [];
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

function parseListLine(line: string): ListItem | null {
  const m = line.match(/^([ \t]*)(?:([-*+])|(\d+)[.)])\s+(.*)$/);
  if (!m) return null;
  const indent = calcIndentLevel(m[1] || '');
  const marker = m[3] ? `${m[3]}.` : '•';
  let text = m[4] || '';
  let task = false;
  let checked = false;
  const tm = text.match(/^\[([xX ])\]\s+(.*)$/);
  if (tm) {
    task = true;
    checked = tm[1].toLowerCase() === 'x';
    text = tm[2];
  }
  return { marker, task, checked, indent, text };
}

function parseBlocks(src: string): Block[] {
  const lines = String(src || '').replace(/\r\n/g, '\n').split('\n');
  const out: Block[] = [];
  let para: string[] = [];
  let list: ListItem[] = [];

  const flushPara = () => {
    if (para.length) {
      out.push({ type: 'para', text: para.join('\n') });
      para = [];
    }
  };
  const flushList = () => {
    if (list.length) {
      out.push({ type: 'list', items: list });
      list = [];
    }
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
      out.push({ type: 'code', lang, code: codeLines.join('\n') });
      continue;
    }

    // 2. Block math $$...$$
    if (trimmed.startsWith('$$')) {
      flushPara();
      flushList();
      if (trimmed.length > 4 && trimmed.endsWith('$$')) {
        out.push({ type: 'math', expr: trimmed.slice(2, -2).trim() });
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
      out.push({ type: 'math', expr: mathLines.join('\n').trim() });
      continue;
    }

    // 3. <details>...</details> collapsible block
    const detailsOpenMatch = trimmed.match(/^<details(\s+open)?\s*>(.*)$/i);
    if (detailsOpenMatch) {
      const endLineIdx = (() => {
        for (let k = i; k < lines.length; k++) {
          if (/<\/details>/i.test(lines[k])) return k;
        }
        return -1;
      })();
      if (endLineIdx !== -1) {
        flushPara();
        flushList();
        const isOpen = Boolean(detailsOpenMatch[1]);
        const collected: string[] = [];
        if (detailsOpenMatch[2]) collected.push(detailsOpenMatch[2]);
        for (let k = i + 1; k <= endLineIdx; k++) {
          collected.push(lines[k]);
        }
        const rawBlock = collected.join('\n').replace(/<\/details>\s*$/i, '').trim();
        let summary = '详情';
        let bodyText = rawBlock;
        const sumMatch = rawBlock.match(/<summary>([\s\S]*?)<\/summary>/i);
        if (sumMatch) {
          summary = sumMatch[1].trim() || '详情';
          bodyText = rawBlock.replace(sumMatch[0], '').trim();
        }
        out.push({
          type: 'details',
          summary,
          isOpen,
          body: bodyText ? parseBlocks(bodyText) : [],
        });
        i = endLineIdx + 1;
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

    // 5. Headings H1–H6
    const hMatch = trimmed.match(/^(#{1,6})\s+(.*)$/);
    if (hMatch) {
      flushPara();
      flushList();
      out.push({ type: 'heading', level: hMatch[1].length, text: hMatch[2] });
      i++;
      continue;
    }

    // 6. Horizontal rule
    if (/^(?:-\s*){3,}$|^(?:\*\s*){3,}$|^(?:_\s*){3,}$/.test(trimmed)) {
      flushPara();
      flushList();
      out.push({ type: 'rule' });
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
          const rows: string[][] = [];
          i += 2;
          while (i < lines.length) {
            const rt = lines[i].trim();
            if (!rt || !rt.includes('|') || parseTableAlignments(lines[i])) break;
            rows.push(splitTableCells(lines[i]));
            i++;
          }
          out.push({ type: 'table', headers, aligns, rows });
          continue;
        }
      }
    }

    // 8. Blockquote (multiline + nested >>)
    if (trimmed.startsWith('>')) {
      flushPara();
      flushList();
      let maxLevel = 1;
      const qLines: string[] = [];
      while (i < lines.length && lines[i].trim().startsWith('>')) {
        let lt = lines[i].trim();
        let lvl = 0;
        while (lt.startsWith('>')) {
          lvl++;
          lt = lt.slice(1).trimStart();
        }
        maxLevel = Math.max(maxLevel, lvl);
        qLines.push(lt);
        i++;
      }
      out.push({ type: 'quote', level: maxLevel, text: qLines.join('\n') });
      continue;
    }

    // 9. List
    const listItem = parseListLine(line);
    if (listItem) {
      flushPara();
      list.push(listItem);
      i++;
      continue;
    }

    // 10. Footnote definition [^id]: ...
    const fnMatch = trimmed.match(/^\[\^([^\]]+)\]:\s*(.*)$/);
    if (fnMatch) {
      flushPara();
      flushList();
      out.push({ type: 'footnote', id: fnMatch[1], text: fnMatch[2] });
      i++;
      continue;
    }

    // 11. Standalone image ![alt](url)
    const imgMatch = trimmed.match(/^!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)$/);
    if (imgMatch) {
      flushPara();
      flushList();
      out.push({ type: 'image', alt: imgMatch[1].trim(), url: imgMatch[2].trim() });
      i++;
      continue;
    }

    flushList();
    para.push(line);
    i++;
  }

  flushPara();
  flushList();
  return out;
}

function renderBlocksList(parsed: Block[], theme: Theme, baseColor: string, keyPrefix: string): React.ReactNode[] {
  return parsed.map((b, idx) => {
    const key = `${keyPrefix}-${idx}`;
    switch (b.type) {
      case 'code':
        return <CodeCard key={key} lang={b.lang} code={b.code} theme={theme} />;
      case 'math':
        return <MathBlockView key={key} expr={b.expr} theme={theme} baseColor={baseColor} />;
      case 'details':
        return (
          <DetailsCard key={key} summary={b.summary} initiallyOpen={b.isOpen} theme={theme}>
            <View style={styles.root}>{renderBlocksList(b.body, theme, baseColor, key)}</View>
          </DetailsCard>
        );
      case 'heading': {
        const size = b.level === 1 ? 20 : b.level === 2 ? 18 : b.level === 3 ? 17 : 16;
        const hColor = b.level >= 5 ? theme.text2 : baseColor;
        return (
          <Text key={key} selectable style={[styles.heading, { fontSize: size, color: hColor }]}>
            {renderInline(b.text, theme, hColor, key)}
          </Text>
        );
      }
      case 'rule':
        return <View key={key} style={[styles.hr, { backgroundColor: theme.s3 }]} />;
      case 'quote': {
        const bars = Math.max(1, Math.min(4, b.level));
        return (
          <View key={key} style={styles.quoteWrap}>
            <View style={styles.quoteBars}>
              {Array.from({ length: bars }, (_, bi) => (
                <View key={bi} style={[styles.quoteBar, { backgroundColor: theme.s3 }]} />
              ))}
            </View>
            <Text selectable style={[styles.para, styles.quoteBody, { color: theme.text2 }]}>
              {renderInline(b.text, theme, theme.text2, key)}
            </Text>
          </View>
        );
      }
      case 'table': {
        const colCount = Math.max(b.headers.length, b.aligns.length);
        return (
          <ScrollView
            key={key}
            horizontal
            showsHorizontalScrollIndicator={false}
            style={[styles.tableWrap, { borderColor: theme.s2 }]}
          >
            <View>
              <View style={[styles.tableRow, { backgroundColor: theme.s1, borderBottomColor: theme.s2 }]}>
                {Array.from({ length: colCount }, (_, ci) => {
                  const align = b.aligns[ci] || 'left';
                  return (
                    <View
                      key={`th-${ci}`}
                      style={[
                        styles.tableCell,
                        ci > 0 && { borderLeftWidth: 1, borderLeftColor: theme.s2 },
                      ]}
                    >
                      <Text selectable style={[styles.tableHeaderText, { color: baseColor, textAlign: align }]}>
                        {renderInline(b.headers[ci] || '', theme, baseColor, `${key}-h-${ci}`, { bold: true })}
                      </Text>
                    </View>
                  );
                })}
              </View>
              {b.rows.map((row, ri) => (
                <View
                  key={`tr-${ri}`}
                  style={[
                    styles.tableRow,
                    ri < b.rows.length - 1 ? { borderBottomColor: theme.s2 } : { borderBottomWidth: 0 },
                  ]}
                >
                  {Array.from({ length: colCount }, (_, ci) => {
                    const align = b.aligns[ci] || 'left';
                    return (
                      <View
                        key={`td-${ri}-${ci}`}
                        style={[
                          styles.tableCell,
                          ci > 0 && { borderLeftWidth: 1, borderLeftColor: theme.s2 },
                        ]}
                      >
                        <Text selectable style={[styles.tableCellText, { color: baseColor, textAlign: align }]}>
                          {renderInline(row[ci] || '', theme, baseColor, `${key}-r-${ri}-${ci}`)}
                        </Text>
                      </View>
                    );
                  })}
                </View>
              ))}
            </View>
          </ScrollView>
        );
      }
      case 'list':
        return (
          <View key={key} style={styles.list}>
            {b.items.map((it, li) => (
              <View key={li} style={[styles.listRow, it.indent > 0 && { paddingLeft: it.indent * 16 }]}>
                {it.task ? (
                  <View
                    style={[
                      styles.checkbox,
                      { borderColor: it.checked ? theme.accent : theme.s3 },
                      it.checked && { backgroundColor: theme.accent },
                    ]}
                  >
                    {it.checked ? (
                      <Text style={[styles.checkMark, { color: theme.accentText }]}>✓</Text>
                    ) : null}
                  </View>
                ) : (
                  <Text style={[styles.listBullet, { color: theme.mute }]}>{it.marker}</Text>
                )}
                <Text
                  selectable
                  style={[
                    styles.para,
                    styles.listBody,
                    { color: it.task && it.checked ? theme.text2 : baseColor },
                    it.task && it.checked && styles.taskDoneText,
                  ]}
                >
                  {renderInline(it.text, theme, it.task && it.checked ? theme.text2 : baseColor, `${key}-li-${li}`)}
                </Text>
              </View>
            ))}
          </View>
        );
      case 'footnote':
        return (
          <View key={key} style={styles.footnoteRow}>
            <Text style={[styles.footnoteId, { color: theme.mute }]}>{`[${b.id}]`}</Text>
            <Text selectable style={[styles.footnoteBody, { color: theme.text2 }]}>
              {renderInline(b.text, theme, theme.text2, key)}
            </Text>
          </View>
        );
      case 'image':
        return <MarkdownImage key={key} alt={b.alt} url={b.url} theme={theme} />;
      case 'para':
        return (
          <Text key={key} selectable style={[styles.para, { color: baseColor }]}>
            {renderInline(b.text, theme, baseColor, key)}
          </Text>
        );
    }
  });
}

export function MarkdownText({
  text,
  theme,
  color,
}: {
  text: string;
  theme: Theme;
  color?: string;
}) {
  const baseColor = color || theme.text;
  const parsed = parseBlocks(text);
  return <View style={styles.root}>{renderBlocksList(parsed, theme, baseColor, 'b')}</View>;
}

const styles = StyleSheet.create({
  root: {
    gap: 8,
  },
  para: {
    fontSize: 16,
    lineHeight: 24,
  },
  heading: {
    fontWeight: '700',
    lineHeight: 26,
    marginTop: 2,
  },
  list: {
    gap: 4,
  },
  listRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: 8,
  },
  listBullet: {
    fontSize: 16,
    lineHeight: 24,
    minWidth: 14,
  },
  listBody: {
    flex: 1,
  },
  checkbox: {
    width: 16,
    height: 16,
    marginTop: 4,
    borderRadius: 4,
    borderWidth: 1.5,
    alignItems: 'center',
    justifyContent: 'center',
  },
  checkMark: {
    fontSize: 11,
    fontWeight: '700',
    lineHeight: 13,
  },
  taskDoneText: {
    textDecorationLine: 'line-through',
  },
  quoteWrap: {
    flexDirection: 'row',
    alignItems: 'stretch',
    gap: 8,
  },
  quoteBars: {
    flexDirection: 'row',
    gap: 4,
  },
  quoteBar: {
    width: 3,
    borderRadius: 2,
  },
  quoteBody: {
    flex: 1,
  },
  hr: {
    height: 1,
    marginVertical: 4,
  },
  tableWrap: {
    borderWidth: 1,
    borderRadius: 12,
  },
  tableRow: {
    flexDirection: 'row',
    borderBottomWidth: 1,
  },
  tableCell: {
    minWidth: 96,
    maxWidth: 240,
    paddingHorizontal: 10,
    paddingVertical: 7,
  },
  tableHeaderText: {
    fontSize: 14,
    lineHeight: 20,
    fontWeight: '600',
  },
  tableCellText: {
    fontSize: 14,
    lineHeight: 20,
  },
  mathBlock: {
    borderRadius: 12,
  },
  mathBlockContent: {
    paddingHorizontal: 14,
    paddingVertical: 10,
    flexGrow: 1,
    justifyContent: 'center',
  },
  mathLines: {
    alignItems: 'center',
    gap: 6,
  },
  mathRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 3,
  },
  mathText: {
    fontSize: 16,
    fontStyle: 'italic',
    lineHeight: 24,
  },
  fracWrap: {
    alignItems: 'center',
    paddingHorizontal: 2,
  },
  fracText: {
    fontSize: 13,
    fontStyle: 'italic',
    lineHeight: 17,
    paddingHorizontal: 3,
  },
  fracBar: {
    height: 1,
    alignSelf: 'stretch',
    marginVertical: 1,
  },
  detailsBox: {
    borderRadius: 12,
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  detailsHead: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  detailsArrow: {
    fontSize: 12,
    width: 12,
  },
  detailsSummary: {
    flex: 1,
    fontSize: 14,
    fontWeight: '600',
  },
  detailsBody: {
    marginTop: 8,
    paddingTop: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  mdImage: {
    width: '100%',
    height: 220,
    borderRadius: 12,
  },
  imgFallback: {
    alignSelf: 'flex-start',
    borderRadius: 8,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  imgFallbackTxt: {
    fontSize: 14,
  },
  footnoteRow: {
    flexDirection: 'row',
    alignItems: 'baseline',
    gap: 6,
  },
  footnoteId: {
    fontSize: 12,
    fontWeight: '600',
  },
  footnoteBody: {
    flex: 1,
    fontSize: 13,
    lineHeight: 18,
  },
  codeWrap: {
    borderRadius: 14,
    overflow: 'hidden',
  },
  codeHead: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  codeLang: {
    fontSize: 13,
    fontWeight: '600',
  },
  copyBtn: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 3,
  },
  copyTxt: {
    fontSize: 13,
    fontWeight: '600',
  },
  codeBody: {
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  codeText: {
    fontSize: 13.5,
    lineHeight: 20,
  },
});
