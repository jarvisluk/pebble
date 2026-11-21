import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { Platform, useColorScheme } from 'react-native';

import {
  loadAccentPref,
  loadThemePref,
  saveAccentPref,
  saveThemePref,
  type ThemePreference,
} from './storage';

export const DEFAULT_ACCENT = '#d9774b';
export const ACCENT_PRESETS = ['#d9774b', '#3b82f6', '#10b981', '#8b5cf6', '#e11d48', '#f59e0b'];
export const BOT_COLORS = ['#f08a2c', '#8a7ad8', '#86a33a', '#b85a4a', '#7a7465', '#e0685a'];

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

export function getBotColor(id: string, index = -1): string {
  const h = fnv1a(id || '?');
  return BOT_COLORS[(index >= 0 ? index : h) % BOT_COLORS.length];
}

export function getBotPalette(
  id: string,
  index: number,
  dark: boolean
): { accent: string; text: string; soft: string } {
  const accent = getBotColor(id, index);
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
  accent: DEFAULT_ACCENT,
  accentText: '#ffffff',
  accentSoft: '#f8e1d6',
  sel: '#faebe4',
  pink: '#f7d9d3',
  pinkInk: '#b2443a',
  lilac: '#e0dbfb',
  lilacInk: '#6a55c9',
  sage: '#e2e9cc',
  sageInk: '#6b8a2a',
  busy: '#f08a2c',
  danger: '#d9483b',
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
  accent: DEFAULT_ACCENT,
  accentText: '#ffffff',
  accentSoft: '#2a2a2a',
  sel: '#262626',
  pink: '#171717',
  pinkInk: '#f0a59b',
  lilac: '#24203a',
  lilacInk: '#b7aaf5',
  sage: '#171717',
  sageInk: '#a9c46a',
  busy: '#f08a2c',
  danger: '#f0605a',
  eye: '#1c1917',
  faceMix: 0.16,
};

interface ThemePrefContextValue {
  pref: ThemePreference;
  setPref: (next: ThemePreference) => void;
  accent: string;
  setAccent: (next: string | null) => void;
}

const ThemePrefContext = createContext<ThemePrefContextValue | null>(null);

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [pref, setPrefState] = useState<ThemePreference>('system');
  const [accent, setAccentState] = useState<string>(DEFAULT_ACCENT);

  useEffect(() => {
    loadThemePref()
      .then((saved) => setPrefState(saved))
      .catch(() => {});
    loadAccentPref()
      .then((saved) => {
        if (saved) setAccentState(saved);
      })
      .catch(() => {});
  }, []);

  const setPref = useCallback((next: ThemePreference) => {
    setPrefState(next);
    void saveThemePref(next).catch(() => {});
  }, []);

  const setAccent = useCallback((next: string | null) => {
    const clean = next && parseHex(next) ? next.toLowerCase() : DEFAULT_ACCENT;
    setAccentState(clean);
    void saveAccentPref(clean === DEFAULT_ACCENT ? null : clean).catch(() => {});
  }, []);

  const value = useMemo(
    () => ({ pref, setPref, accent, setAccent }),
    [pref, setPref, accent, setAccent]
  );

  return React.createElement(ThemePrefContext.Provider, { value }, children);
}

export function useThemePref(): ThemePrefContextValue {
  const ctx = useContext(ThemePrefContext);
  if (!ctx) {
    return { pref: 'system', setPref: () => {}, accent: DEFAULT_ACCENT, setAccent: () => {} };
  }
  return ctx;
}

export function useTheme(): Theme {
  const systemScheme = useColorScheme();
  const ctx = useContext(ThemePrefContext);
  const effective = ctx && ctx.pref !== 'system' ? ctx.pref : systemScheme;
  const accent = ctx?.accent || DEFAULT_ACCENT;
  const base = effective === 'dark' ? DARK_THEME : LIGHT_THEME;
  return useMemo(() => {
    if (accent === DEFAULT_ACCENT) return base;
    const accentText = contrastText(accent);
    if (base.dark) {
      return { ...base, accent, accentText };
    }
    return {
      ...base,
      accent,
      accentText,
      accentSoft: mixHex(accent, base.card, 0.16),
      sel: mixHex(accent, base.card, 0.10),
    };
  }, [accent, base]);
}

export const MONO_FONT = Platform.select({
  ios: 'Menlo',
  android: 'monospace',
  default: 'monospace',
});
