import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';

import type { ServerProfile } from './types';

const SERVERS_KEY = 'bothub.servers.v1';
const ACTIVE_KEY = 'bothub.activeServerId.v1';
const THEME_KEY = 'bothub.theme.v1';
const LANG_KEY = 'bothub.lang.v1';
const ACCENT_KEY = 'bothub.accent.v1';
const TS_BIND_KEY = 'bothub.tsBind.v1';

export type ThemePreference = 'system' | 'light' | 'dark';
export type LanguagePreference = 'system' | 'zh-CN' | 'en';
export type BotSortMode = 'recent' | 'manual';

const BOT_SORT_KEY = 'bothub.botSortMode.v1';
const BOT_PINNED_KEY = 'bothub.pinnedBots.v1';

async function readKey(key: string): Promise<string | null> {
  if (Platform.OS === 'web') {
    try {
      return typeof localStorage !== 'undefined' ? localStorage.getItem(key) : null;
    } catch {
      return null;
    }
  }
  return SecureStore.getItemAsync(key);
}

async function writeKey(key: string, value: string | null): Promise<void> {
  if (Platform.OS === 'web') {
    try {
      if (typeof localStorage === 'undefined') return;
      if (value == null) localStorage.removeItem(key);
      else localStorage.setItem(key, value);
    } catch {
      // ignore web storage errors
    }
    return;
  }
  if (value == null) {
    await SecureStore.deleteItemAsync(key);
  } else {
    await SecureStore.setItemAsync(key, value);
  }
}

export async function loadSavedServers(): Promise<{ servers: ServerProfile[]; activeId: string | null }> {
  const [rawServers, activeId] = await Promise.all([readKey(SERVERS_KEY), readKey(ACTIVE_KEY)]);
  if (!rawServers) return { servers: [], activeId };
  try {
    const parsed = JSON.parse(rawServers);
    if (Array.isArray(parsed)) {
      return { servers: parsed, activeId };
    }
  } catch {
    // ignore malformed storage
  }
  return { servers: [], activeId: null };
}

export async function saveServers(servers: ServerProfile[], activeId: string | null): Promise<void> {
  await Promise.all([
    writeKey(SERVERS_KEY, JSON.stringify(servers)),
    writeKey(ACTIVE_KEY, activeId),
  ]);
}

export async function loadThemePref(): Promise<ThemePreference> {
  const val = await readKey(THEME_KEY);
  return val === 'light' || val === 'dark' ? val : 'system';
}

export async function saveThemePref(pref: ThemePreference): Promise<void> {
  await writeKey(THEME_KEY, pref === 'system' ? null : pref);
}

export async function loadLangPref(): Promise<LanguagePreference> {
  const val = await readKey(LANG_KEY);
  return val === 'zh-CN' || val === 'en' ? val : 'system';
}

export async function saveLangPref(pref: LanguagePreference): Promise<void> {
  await writeKey(LANG_KEY, pref === 'system' ? null : pref);
}

export async function loadAccentPref(): Promise<string | null> {
  const val = await readKey(ACCENT_KEY);
  return val && /^#[0-9a-fA-F]{6}$/.test(val) ? val.toLowerCase() : null;
}

export async function saveAccentPref(hex: string | null): Promise<void> {
  await writeKey(ACCENT_KEY, hex && /^#[0-9a-fA-F]{6}$/.test(hex) ? hex.toLowerCase() : null);
}

export interface TailscaleBindPref {
  bound: boolean;
  controlUrl?: string;
}

export async function loadTailscaleBindPref(): Promise<TailscaleBindPref> {
  const raw = await readKey(TS_BIND_KEY);
  if (!raw) return { bound: false };
  try {
    const parsed = JSON.parse(raw) as TailscaleBindPref;
    return { bound: Boolean(parsed.bound), controlUrl: parsed.controlUrl || '' };
  } catch {
    return { bound: false };
  }
}

export async function saveTailscaleBindPref(pref: TailscaleBindPref | null): Promise<void> {
  await writeKey(TS_BIND_KEY, pref && pref.bound ? JSON.stringify(pref) : null);
}

export async function loadBotListPrefs(): Promise<{ sortMode: BotSortMode; pinnedIds: string[] }> {
  const [sortRaw, pinnedRaw] = await Promise.all([readKey(BOT_SORT_KEY), readKey(BOT_PINNED_KEY)]);
  const sortMode: BotSortMode = sortRaw === 'manual' ? 'manual' : 'recent';
  let pinnedIds: string[] = [];
  if (pinnedRaw) {
    try {
      const parsed = JSON.parse(pinnedRaw);
      if (Array.isArray(parsed)) {
        pinnedIds = parsed.filter((x): x is string => typeof x === 'string');
      }
    } catch {
      // ignore malformed storage
    }
  }
  return { sortMode, pinnedIds };
}

export async function saveBotSortMode(mode: BotSortMode): Promise<void> {
  await writeKey(BOT_SORT_KEY, mode === 'recent' ? null : mode);
}

export async function savePinnedBots(ids: string[]): Promise<void> {
  await writeKey(BOT_PINNED_KEY, ids.length > 0 ? JSON.stringify(ids) : null);
}
