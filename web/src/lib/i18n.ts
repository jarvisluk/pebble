import type { LangPref, ResolvedLang } from '../types';
import { en } from './i18n/en';
import { zhCN } from './i18n/zh-CN';

export const I18N: Record<ResolvedLang, Record<string, string>> = {
  'zh-CN': zhCN,
  en,
};

export function getLangPref(): LangPref {
  try {
    const l = localStorage.getItem('bothub.lang');
    return l === 'zh-CN' || l === 'en' ? l : 'system';
  } catch {
    return 'system';
  }
}

export function resolveLang(pref: LangPref = getLangPref()): ResolvedLang {
  if (pref === 'zh-CN' || pref === 'en') return pref;
  return /^zh\b/i.test(navigator.language || '') ? 'zh-CN' : 'en';
}

export function setLangPref(val: LangPref): ResolvedLang {
  if (val === 'zh-CN' || val === 'en') {
    try {
      localStorage.setItem('bothub.lang', val);
    } catch {}
  } else {
    try {
      localStorage.removeItem('bothub.lang');
    } catch {}
  }
  const resolved = resolveLang(val);
  document.documentElement.lang = resolved;
  return resolved;
}

export function translate(lang: ResolvedLang, key: string, vars?: Record<string, string | number>): string {
  let s = I18N[lang]?.[key] ?? I18N['zh-CN'][key] ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replaceAll(`{${k}}`, String(v ?? ''));
    }
  }
  return s;
}
