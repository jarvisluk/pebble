import type { BotStatus, ResolvedLang, ThemePref } from '../types';
import { translate } from './i18n';

export const BOT_COLORS = ['#d9774b', '#e0a23a', '#7fa14a', '#7aaa98', '#5b8bb5', '#9a7bc0', '#c4605e'];
export const BOT_AVATARS = ['blob', 'pebble-1', 'pebble-2', 'pebble-3', 'pebble-4', 'pebble-5', 'pebble-6', 'pebble-7'] as const;
export type BotAvatarKind = (typeof BOT_AVATARS)[number];

const LEGACY_AVATAR_MAP: Record<string, BotAvatarKind> = {
  squircle: 'pebble-1',
  cat: 'pebble-2',
  bear: 'pebble-3',
  cloud: 'pebble-4',
  ghost: 'pebble-5',
  star: 'pebble-6',
  sprout: 'pebble-7',
};

export function normalizeBotAvatar(avatar?: string): BotAvatarKind {
  const s = String(avatar || '').trim().toLowerCase();
  if (!s || s === 'blob') return 'blob';
  if (s in LEGACY_AVATAR_MAP) return LEGACY_AVATAR_MAP[s];
  if ((BOT_AVATARS as readonly string[]).includes(s)) return s as BotAvatarKind;
  return 'blob';
}

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

export function normalizeBotHex(hex: string | undefined): string {
  const clean = String(hex || '').trim().toLowerCase();
  return /^#[0-9a-f]{6}$/.test(clean) ? clean : '';
}

export function botColor(id: string, bots: BotStatus[], overrideColor?: string): string {
  const custom = normalizeBotHex(overrideColor);
  if (custom) return custom;
  const found = bots.find((b) => b.id === id);
  const stored = normalizeBotHex(found?.color);
  if (stored) return stored;
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

export function formatClockTime(iso: string | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

export function countToolCalls(tools?: string[]): number {
  if (!tools || tools.length === 0) return 0;
  let total = 0;
  for (const item of tools) {
    const idx = item.lastIndexOf('\u00d7');
    if (idx >= 0) {
      const n = Number.parseInt(item.slice(idx + 1), 10);
      total += Number.isFinite(n) && n > 0 ? n : 1;
    } else {
      total += 1;
    }
  }
  return total;
}

export function formatDurationMs(ms?: number): string {
  if (!ms || ms <= 0) return '';
  const sec = ms / 1000;
  if (sec < 60) return `${sec.toFixed(1)}s`;
  let mins = Math.floor(sec / 60);
  let rem = Math.round(sec % 60);
  if (rem === 60) {
    mins += 1;
    rem = 0;
  }
  return `${mins}m ${rem}s`;
}
