import type { BotStatus, ResolvedLang, ThemePref } from '../types';
import { translate } from './i18n';

export const DEFAULT_ACCENT = '#d9774b';
export const ACCENT_PRESETS = ['#d9774b', '#3b82f6', '#10b981', '#8b5cf6', '#e11d48', '#f59e0b'];
export const BOT_COLORS = ['#f08a2c', '#8a7ad8', '#86a33a', '#b85a4a', '#7a7465', '#e0685a'];

export function hashStr(s: string): number {
  let h = 2166136261;
  for (const c of String(s)) {
    h = Math.imul(h ^ (c.codePointAt(0) || 0), 16777619);
  }
  return h >>> 0;
}

export function contrastText(hex: string): string {
  const m = /^#?([0-9a-fA-F]{6})$/.exec(String(hex || '').trim());
  if (!m) return '#ffffff';
  const n = parseInt(m[1], 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  const lum = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
  return lum > 0.62 ? '#1d1b19' : '#ffffff';
}

export function getThemePref(): ThemePref {
  try {
    const tv = localStorage.getItem('bothub.theme');
    return tv === 'light' || tv === 'dark' ? tv : 'system';
  } catch {
    return 'system';
  }
}

export function applyThemePref(val: ThemePref): void {
  if (val === 'light' || val === 'dark') {
    document.documentElement.dataset.theme = val;
    try {
      localStorage.setItem('bothub.theme', val);
    } catch {}
  } else {
    delete document.documentElement.dataset.theme;
    try {
      localStorage.removeItem('bothub.theme');
    } catch {}
  }
}

export function getAccentPref(): string {
  try {
    const a = localStorage.getItem('bothub.accent');
    return a && /^#[0-9a-fA-F]{6}$/.test(a) ? a.toLowerCase() : DEFAULT_ACCENT;
  } catch {
    return DEFAULT_ACCENT;
  }
}

export function applyAccentPref(hex: string): string {
  const clean = /^#[0-9a-fA-F]{6}$/.test(String(hex || '').trim()) ? hex.toLowerCase() : DEFAULT_ACCENT;
  if (clean === DEFAULT_ACCENT) {
    document.documentElement.style.removeProperty('--accent');
    document.documentElement.style.removeProperty('--accent-text');
    try {
      localStorage.removeItem('bothub.accent');
    } catch {}
  } else {
    document.documentElement.style.setProperty('--accent', clean);
    document.documentElement.style.setProperty('--accent-text', contrastText(clean));
    try {
      localStorage.setItem('bothub.accent', clean);
    } catch {}
  }
  return clean;
}

export function botColor(id: string, bots: BotStatus[]): string {
  const h = hashStr(id || '?');
  const i = bots.findIndex((b) => b.id === id);
  return BOT_COLORS[(i >= 0 ? i : h) % BOT_COLORS.length];
}

export function formatStamp(iso: string | undefined, lang: ResolvedLang): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const loc = lang === 'en' ? 'en-US' : 'zh-CN';
  const timeStr = d.toLocaleTimeString(loc, { hour: '2-digit', minute: '2-digit' });
  const today = new Date();
  const y = new Date(today);
  y.setDate(today.getDate() - 1);
  if (d.toDateString() === today.toDateString()) return `${translate(lang, 'today')} ${timeStr}`;
  if (d.toDateString() === y.toDateString()) return `${translate(lang, 'yesterday')} ${timeStr}`;
  return translate(lang, 'dateFmt', { m: d.getMonth() + 1, d: d.getDate(), t: timeStr });
}

export function formatShortTime(iso: string | undefined, lang: ResolvedLang): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const today = new Date();
  const y = new Date(today);
  y.setDate(today.getDate() - 1);
  const loc = lang === 'en' ? 'en-US' : 'zh-CN';
  if (d.toDateString() === today.toDateString()) {
    return d.toLocaleTimeString(loc, { hour: '2-digit', minute: '2-digit' });
  }
  if (d.toDateString() === y.toDateString()) return translate(lang, 'yesterday');
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

export function plainPreviewText(s: string | undefined, lang: ResolvedLang): string {
  return String(s || '')
    .replace(/```[\s\S]*?```/g, translate(lang, 'codeTag'))
    .replace(/[#>*`_\[\]]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}
