import type { StageKind, StageRef, StageTab } from '../types';

export interface BotStageState {
  tabs: StageTab[];
  activeTabId: string;
  unreadLatestStageId?: string;
}

export const EMPTY_BOT_STAGE_STATE: BotStageState = {
  tabs: [],
  activeTabId: '',
};

type TranslateFn = (key: string, vars?: Record<string, string | number>) => string;

let newTabCounter = 0;

export function basenameOf(rawPath?: string): string {
  const clean = (rawPath || '').replace(/\/+$/, '');
  if (!clean) return '';
  const idx = clean.lastIndexOf('/');
  return idx >= 0 ? clean.slice(idx + 1) : clean;
}

export function normalizeStageKind(kind: StageKind, path?: string): StageKind {
  if (kind === 'files' && (path || '').trim() !== '') {
    return 'file';
  }
  return kind;
}

export function createNewTab(t: TranslateFn, idSuffix?: string): StageTab {
  newTabCounter += 1;
  const suffix = idSuffix || `${Date.now()}_${newTabCounter}`;
  return {
    id: `new:${suffix}`,
    kind: 'new',
    title: t('stageNewTab'),
    closable: true,
  };
}

export function createFilesTab(t: TranslateFn): StageTab {
  return {
    id: 'files',
    kind: 'files',
    title: t('stageFilesTab'),
    closable: true,
  };
}

export function createFileTab(path: string, title?: string): StageTab {
  const clean = path.trim();
  const base = basenameOf(clean) || clean;
  return {
    id: `file:${clean}`,
    kind: 'file',
    title: (title || '').trim() || base,
    subtitle: clean,
    path: clean,
    closable: true,
  };
}

export function createDesktopTab(t: TranslateFn, target = 'local', label?: string): StageTab {
  const resolved = (target || '').trim() || 'local';
  return {
    id: resolved === 'local' ? 'desktop' : `desktop:${resolved}`,
    kind: 'desktop',
    title: (label || '').trim() || t('stageDesktopTab'),
    subtitle: resolved !== 'local' ? resolved : undefined,
    target: resolved,
    closable: true,
  };
}

export function stageTabFromRef(ref: StageRef, t: TranslateFn, botId?: string): StageTab {
  const kind = normalizeStageKind(ref.kind, ref.path);
  if (kind === 'file') {
    const cleanPath = (ref.path || '').trim();
    const base = basenameOf(cleanPath) || cleanPath || 'file';
    return {
      id: (ref.id || '').trim() || `file:${cleanPath}`,
      kind: 'file',
      title: (ref.title || '').trim() || base,
      subtitle: (ref.subtitle || '').trim() || cleanPath || undefined,
      botId,
      path: cleanPath,
      closable: ref.closable !== false,
    };
  }
  if (kind === 'files') {
    return {
      id: (ref.id || '').trim() || 'files',
      kind: 'files',
      title: (ref.title || '').trim() || t('stageFilesTab'),
      subtitle: (ref.subtitle || '').trim() || undefined,
      botId,
      closable: ref.closable !== false,
    };
  }
  if (kind === 'desktop') {
    const target = (ref.target || '').trim() || 'local';
    return {
      id: (ref.id || '').trim() || (target === 'local' ? 'desktop' : `desktop:${target}`),
      kind: 'desktop',
      title: (ref.title || '').trim() || t('stageDesktopTab'),
      subtitle: (ref.subtitle || '').trim() || (target !== 'local' ? target : undefined),
      botId,
      target,
      closable: ref.closable !== false,
    };
  }
  const fallbackId = (ref.id || '').trim() || `${kind}:${Date.now()}`;
  return {
    id: fallbackId,
    kind,
    title: (ref.title || '').trim() || kind,
    subtitle: (ref.subtitle || '').trim() || ref.url || ref.resourceUri || undefined,
    botId,
    path: ref.path,
    url: ref.url,
    resourceUri: ref.resourceUri,
    target: ref.target,
    closable: ref.closable !== false,
  };
}

export function upsertTab(state: BotStageState | undefined, tab: StageTab, activate = true): BotStageState {
  const prev = state || EMPTY_BOT_STAGE_STATE;
  const idx = prev.tabs.findIndex((x) => x.id === tab.id);
  const nextTabs = idx >= 0 ? prev.tabs.map((x, i) => (i === idx ? { ...x, ...tab } : x)) : [...prev.tabs, tab];
  return {
    ...prev,
    tabs: nextTabs,
    activeTabId: activate ? tab.id : prev.activeTabId || tab.id,
  };
}

export function replaceNewTabOrOpen(state: BotStageState | undefined, targetTab: StageTab): BotStageState {
  const prev = state || EMPTY_BOT_STAGE_STATE;
  const active = prev.tabs.find((x) => x.id === prev.activeTabId);
  if (!active || active.kind !== 'new') {
    return upsertTab(prev, targetTab, true);
  }
  const existingIdx = prev.tabs.findIndex((x) => x.id === targetTab.id);
  if (existingIdx >= 0) {
    const filtered = prev.tabs.filter((x) => x.id !== active.id);
    return {
      ...prev,
      tabs: filtered.map((x) => (x.id === targetTab.id ? { ...x, ...targetTab } : x)),
      activeTabId: targetTab.id,
    };
  }
  return {
    ...prev,
    tabs: prev.tabs.map((x) => (x.id === active.id ? targetTab : x)),
    activeTabId: targetTab.id,
  };
}

export function closeTab(state: BotStageState | undefined, tabId: string, t: TranslateFn): BotStageState {
  const prev = state || EMPTY_BOT_STAGE_STATE;
  const idx = prev.tabs.findIndex((x) => x.id === tabId);
  if (idx < 0) return prev;
  const remaining = prev.tabs.filter((x) => x.id !== tabId);
  if (remaining.length === 0) {
    const fallback = createNewTab(t);
    return {
      ...prev,
      tabs: [fallback],
      activeTabId: fallback.id,
    };
  }
  const nextActive =
    prev.activeTabId === tabId
      ? remaining[Math.min(idx, remaining.length - 1)].id
      : prev.activeTabId;
  return {
    ...prev,
    tabs: remaining,
    activeTabId: nextActive,
  };
}

export function ensureNonEmptyStageState(state: BotStageState | undefined, t: TranslateFn): BotStageState {
  if (state && state.tabs.length > 0) {
    if (state.tabs.some((x) => x.id === state.activeTabId)) return state;
    return { ...state, activeTabId: state.tabs[0].id };
  }
  const fresh = createNewTab(t);
  return {
    tabs: [fresh],
    activeTabId: fresh.id,
    unreadLatestStageId: state?.unreadLatestStageId,
  };
}
