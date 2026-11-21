import React, { createContext, useCallback, useContext, useEffect, useState } from 'react';
import { Platform, useColorScheme } from 'react-native';

import { loadThemePref, saveThemePref, type ThemePreference } from './storage';

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
  accent: '#d9774b',
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
  card: '#111111',
  s1: '#1a1a1a',
  s2: '#262626',
  s3: '#333333',
  text: '#f3f1ee',
  text2: '#bdb8b2',
  mute: '#77726d',
  accent: '#d9774b',
  accentText: '#ffffff',
  accentSoft: '#262626',
  sel: '#1e1e1e',
  pink: '#111111',
  pinkInk: '#f0a59b',
  lilac: '#1b1928',
  lilacInk: '#b7aaf5',
  sage: '#111111',
  sageInk: '#a9c46a',
  busy: '#f08a2c',
  danger: '#f0605a',
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

  return React.createElement(ThemePrefContext.Provider, { value: { pref, setPref } }, children);
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
