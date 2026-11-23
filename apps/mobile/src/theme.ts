import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { Platform, useColorScheme } from 'react-native';

import {
  loadThemePref,
  saveThemePref,
  type ThemePreference,
} from './storage';

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

export function normalizeBotHex(hex?: string): string {
  const clean = String(hex || '').trim().toLowerCase();
  return /^#[0-9a-f]{6}$/.test(clean) ? clean : '';
}

export interface Theme {
  dark: boolean;
  page: string;
  card: string;
  s1: string;
  s2: string;
  s3: string;
  text: string;
  text2: string;
  mute: string;
  accent: string;
  accentText: string;
  accentSoft: string;
  sel: string;
  pink: string;
  pinkInk: string;
  lilac: string;
  lilacInk: string;
  sage: string;
  sageInk: string;
  busy: string;
  danger: string;
  eye: string;
  faceMix: number;
}

function parseHex(hex: string): [number, number, number] | null {
  const m = /^#?([0-9a-fA-F]{6})$/.exec(hex.trim());
  if (!m) return null;
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

export function mixHex(hexA: string, hexB: string, weightA: number): string {
  const a = parseHex(hexA);
  const b = parseHex(hexB);
  if (!a || !b) return hexA;
  const w = Math.max(0, Math.min(1, weightA));
  const ch = (x: number, y: number) => Math.round(x * w + y * (1 - w)).toString(16).padStart(2, '0');
  return `#${ch(a[0], b[0])}${ch(a[1], b[1])}${ch(a[2], b[2])}`;
}

export function contrastText(hex: string): string {
  const rgb = parseHex(hex);
  if (!rgb) return '#ffffff';
  const lum = (0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2]) / 255;
  return lum > 0.62 ? '#1d1b19' : '#ffffff';
}

function fnv1a(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  }
  return h >>> 0;
}

export function getBotColor(id: string, index = -1, customColor?: string): string {
  const custom = normalizeBotHex(customColor);
  if (custom) return custom;
  const h = fnv1a(id || '?');
  return BOT_COLORS[(index >= 0 ? index : h) % BOT_COLORS.length];
}

export function getBotPalette(
  id: string,
  index: number,
  dark: boolean,
  customColor?: string
): { accent: string; text: string; soft: string } {
  const accent = getBotColor(id, index, customColor);
  return {
    accent,
    text: contrastText(accent),
    soft: dark ? '#2a2a2a' : mixHex(accent, '#ffffff', 0.16),
  };
}

export const LIGHT_THEME: Theme = {
  dark: false,
  page: '#efece8',
  card: '#ffffff',
  s1: '#f5f3f0',
  s2: '#e9e5e0',
  s3: '#dcd7d0',
  text: '#1d1b19',
  text2: '#57524d',
  mute: '#9b958f',
  accent: '#1d1b19',
  accentText: '#ffffff',
  accentSoft: '#e9e5e0',
  sel: '#eeebe6',
  pink: '#fde8e8',
  pinkInk: '#dc2626',
  lilac: '#e0dbfb',
  lilacInk: '#6a55c9',
  sage: '#e6f6ec',
  sageInk: '#16a34a',
  busy: '#f59e0b',
  danger: '#dc2626',
  eye: '#1c1917',
  faceMix: 1,
};

export const DARK_THEME: Theme = {
  dark: true,
  page: '#000000',
  card: '#171717',
  s1: '#232323',
  s2: '#303030',
  s3: '#404040',
  text: '#f3f1ee',
  text2: '#ccc7c1',
  mute: '#948e88',
  accent: '#f3f1ee',
  accentText: '#171717',
  accentSoft: '#2a2a2a',
  sel: '#262626',
  pink: '#2c1616',
  pinkInk: '#ff6961',
  lilac: '#24203a',
  lilacInk: '#b7aaf5',
  sage: '#14261c',
  sageInk: '#30d158',
  busy: '#ff9f0a',
  danger: '#ff453a',
  eye: '#1c1917',
  faceMix: 0.16,
};

interface ThemePrefContextValue {
  pref: ThemePreference;
  setPref: (next: ThemePreference) => void;
}

const ThemePrefContext = createContext<ThemePrefContextValue | null>(null);

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [pref, setPrefState] = useState<ThemePreference>('system');

  useEffect(() => {
    loadThemePref()
      .then((saved) => setPrefState(saved))
      .catch(() => {});
  }, []);

  const setPref = useCallback((next: ThemePreference) => {
    setPrefState(next);
    void saveThemePref(next).catch(() => {});
  }, []);

  const value = useMemo(
    () => ({ pref, setPref }),
    [pref, setPref]
  );

  return React.createElement(ThemePrefContext.Provider, { value }, children);
}

export function useThemePref(): ThemePrefContextValue {
  const ctx = useContext(ThemePrefContext);
  if (!ctx) {
    return { pref: 'system', setPref: () => {} };
  }
  return ctx;
}

export function useTheme(): Theme {
  const systemScheme = useColorScheme();
  const ctx = useContext(ThemePrefContext);
  const effective = ctx && ctx.pref !== 'system' ? ctx.pref : systemScheme;
  return effective === 'dark' ? DARK_THEME : LIGHT_THEME;
}

export const MONO_FONT = Platform.select({
  ios: 'Menlo',
  android: 'monospace',
  default: 'monospace',
});
