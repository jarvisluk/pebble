import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  BotStatus,
  ConfirmOptions,
  LangPref,
  NodeInfo,
  ResolvedLang,
  ServerInfo,
  SettingsData,
  SettingsTab,
} from '../../types';
import { translate } from '../../lib/i18n';
import type { ViewDir, ViewState } from '../../lib/useViewTransition';
import { DesktopTab } from './DesktopTab';
import { GeneralTab } from './GeneralTab';
import { MemoryTab } from './MemoryTab';
import { ModelsTab } from './ModelsTab';
import { NetworkTab } from './NetworkTab';

interface SettingsViewProps {
  hidden: boolean;
  viewState: ViewState;
  viewDir: ViewDir;
  viewAnim: boolean;
  tab: SettingsTab;
  bots: BotStatus[];
  settings: SettingsData | null;
  serverInfo: ServerInfo | null;
  models: Record<string, string[]>;
  nodesFromEvents: NodeInfo[] | null;
  lang: ResolvedLang;
  langPref: LangPref;
  onOpenMenu: () => void;
  onLangChange: (pref: LangPref) => void;
  onToast: (msg: string) => void;
  onConfirm: (opts: ConfirmOptions) => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onReloadBots: () => Promise<void>;
  onLoadModels: (harness: string, force?: boolean) => Promise<string[]>;
  onResetModels: () => void;
  onOpenDesktopTarget: (target: string) => void;
  onReopenUnbox: () => void;
  onDirtyChange?: (dirty: boolean) => void;
  saveRef?: React.MutableRefObject<(() => Promise<boolean>) | null>;
  resetRef?: React.MutableRefObject<(() => void) | null>;
}

export const SettingsView: React.FC<SettingsViewProps> = ({
  hidden,
  viewState,
  viewDir,
  viewAnim,
  tab,
  bots,
  settings,
  serverInfo,
  models,
  nodesFromEvents,
  lang,
  langPref,
  onOpenMenu,
  onLangChange,
  onToast,
  onConfirm,
  onSettingsUpdated,
  onReloadBots,
  onLoadModels,
  onResetModels,
  onOpenDesktopTarget,
  onReopenUnbox,
  onDirtyChange,
  saveRef,
  resetRef,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [modelsDirty, setModelsDirty] = useState(false);
  const [memoryDirty, setMemoryDirty] = useState(false);
  const [desktopDirty, setDesktopDirty] = useState(false);
  const [modelsSaving, setModelsSaving] = useState(false);

  const modelsSaveRef = useRef<(() => Promise<boolean>) | null>(null);
  const modelsResetRef = useRef<(() => void) | null>(null);
  const memorySaveRef = useRef<(() => Promise<boolean>) | null>(null);
  const memoryResetRef = useRef<(() => void) | null>(null);
  const desktopSaveRef = useRef<(() => Promise<boolean>) | null>(null);
  const desktopResetRef = useRef<(() => void) | null>(null);

  const activeDirty =
    tab === 'models' ? modelsDirty : tab === 'memory' ? memoryDirty : tab === 'desktop' ? desktopDirty : false;

  useEffect(() => {
    onDirtyChange?.(activeDirty);
  }, [activeDirty, onDirtyChange]);

  const activeSave = useCallback(async (): Promise<boolean> => {
    if (tab === 'models' && modelsSaveRef.current) return modelsSaveRef.current();
    if (tab === 'memory' && memorySaveRef.current) return memorySaveRef.current();
    if (tab === 'desktop' && desktopSaveRef.current) return desktopSaveRef.current();
    return true;
  }, [tab]);

  const activeReset = useCallback(() => {
    modelsResetRef.current?.();
    memoryResetRef.current?.();
    desktopResetRef.current?.();
  }, []);

  useEffect(() => {
    if (saveRef) saveRef.current = activeSave;
    if (resetRef) resetRef.current = activeReset;
    return () => {
      if (saveRef) saveRef.current = null;
      if (resetRef) resetRef.current = null;
    };
  }, [saveRef, resetRef, activeSave, activeReset]);

  const preloadAllModels = useCallback(
    (list: SettingsData['harnesses'] | undefined, force = false) => {
      for (const h of list || []) {
        if (h.installed === true) void onLoadModels(h.id, force);
      }
    },
    [onLoadModels]
  );

  const handleAfterMagpieInstall = useCallback(
    (fresh: SettingsData) => {
      onSettingsUpdated(fresh);
      onResetModels();
      preloadAllModels(fresh.harnesses, true);
    },
    [onSettingsUpdated, onResetModels, preloadAllModels]
  );

  const titleKey =
    tab === 'remote'
      ? 'tabRemote'
      : tab === 'desktop'
        ? 'tabDesktop'
        : tab === 'memory'
          ? 'tabMemory'
          : tab === 'general'
            ? 'tabGeneral'
            : 'tabModels';

  const activePane = !hidden;

  const globalModels = useMemo(() => {
    const list = settings?.harnesses || [];
    const defHarness = settings?.defaultHarness || list[0]?.id || 'opencode';
    const anyInstalled = list.some((h) => h.installed === true);
    const ordered: typeof list = [];
    const seenH = new Set<string>();
    const pushH = (h?: (typeof list)[number]) => {
      if (!h || seenH.has(h.id)) return;
      seenH.add(h.id);
      ordered.push(h);
    };
    const def = list.find((h) => h.id === defHarness);
    if (!anyInstalled || def?.installed) pushH(def);
    for (const h of list) {
      if ((!anyInstalled || h.installed) && !h.magpie?.connected) pushH(h);
    }
    for (const h of list) {
      if ((!anyInstalled || h.installed) && h.magpie?.connected) pushH(h);
    }
    for (const h of list) {
      if (!anyInstalled || (h.installed && (models[h.id]?.length ?? 0) > 0)) pushH(h);
    }
    const seen = new Set<string>();
    const out: { value: string; label: string }[] = [];
    for (const h of ordered) {
      const tag = h.magpie?.connected ? `${h.label} · Magpie` : h.label;
      for (const m of models[h.id] || []) {
        if (seen.has(m)) continue;
        seen.add(m);
        out.push({ value: m, label: `${m} (${tag})` });
      }
    }
    return out;
  }, [settings?.harnesses, settings?.defaultHarness, models]);

  return (
    <section
      id="settingsView"
      className="main-pane view-pane"
      aria-label={t('settings')}
      hidden={hidden}
      aria-hidden={viewState !== 'active'}
      data-view-state={viewState}
      data-view-dir={viewDir}
      data-view-anim={viewAnim ? '1' : '0'}
    >
      <div className="set-hd">
        <button
          type="button"
          id="settingsMenuBtn"
          className="ghost menu"
          aria-label={t('openSettingsTabs')}
          title={t('settingsTabs')}
          onClick={onOpenMenu}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M4 7h16M4 12h16M4 17h10" />
          </svg>
        </button>
        <h1 id="setTitle">{t(titleKey)}</h1>
        <div className="hd-acts">
          {tab === 'models' && (
            <button
              type="button"
              id="settingsSubmit"
              className="btn go set-save"
              disabled={!modelsDirty || modelsSaving}
              onClick={() => void modelsSaveRef.current?.()}
            >
              {modelsSaving ? t('saving') : t('saveConfig')}
            </button>
          )}
        </div>
      </div>

      <div className="scroll">
        <div id="settingsForm" className="set-body">
          <datalist id="models-global">
            {globalModels.map((item) => (
              <option key={item.value} value={item.value} label={item.label} />
            ))}
          </datalist>
          {(settings?.harnesses || []).map((h) => (
            <datalist key={h.id} id={`models-${h.id}`}>
              {(models[h.id] || []).map((m) => (
                <option key={m} value={m} />
              ))}
            </datalist>
          ))}
          {tab === 'general' && (
            <GeneralTab
              lang={lang}
              langPref={langPref}
              serverInfo={serverInfo}
              onLangChange={onLangChange}
              onReopenUnbox={onReopenUnbox}
            />
          )}
          {tab === 'models' && (
            <ModelsTab
              active={activePane}
              bots={bots}
              settings={settings}
              models={models}
              lang={lang}
              onToast={onToast}
              onConfirm={onConfirm}
              onSettingsUpdated={onSettingsUpdated}
              onReloadBots={onReloadBots}
              onLoadModels={onLoadModels}
              onResetModels={onResetModels}
              onDirtyChange={setModelsDirty}
              onSavingChange={setModelsSaving}
              saveRef={modelsSaveRef}
              resetRef={modelsResetRef}
            />
          )}
          {tab === 'memory' && (
            <MemoryTab
              active={activePane}
              settings={settings}
              lang={lang}
              onToast={onToast}
              onSettingsUpdated={onSettingsUpdated}
              onAfterMagpieInstall={handleAfterMagpieInstall}
              onDirtyChange={setMemoryDirty}
              saveRef={memorySaveRef}
              resetRef={memoryResetRef}
            />
          )}
          {tab === 'desktop' && (
            <DesktopTab
              active={activePane}
              settings={settings}
              lang={lang}
              onToast={onToast}
              onSettingsUpdated={onSettingsUpdated}
              onLoadModels={onLoadModels}
              onPreloadAllModels={preloadAllModels}
              onOpenDesktopTarget={onOpenDesktopTarget}
              onDirtyChange={setDesktopDirty}
              saveRef={desktopSaveRef}
              resetRef={desktopResetRef}
            />
          )}
          {tab === 'remote' && (
            <NetworkTab
              active={activePane}
              nodesFromEvents={nodesFromEvents}
              lang={lang}
              onToast={onToast}
              onConfirm={onConfirm}
              onOpenDesktopTarget={onOpenDesktopTarget}
            />
          )}
        </div>
      </div>
    </section>
  );
};
