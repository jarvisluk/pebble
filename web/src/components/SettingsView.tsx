import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  BotStatus,
  ConfirmOptions,
  LangPref,
  LocalNodeState,
  MemoryResponse,
  NodeInfo,
  ResolvedLang,
  SettingsData,
  SettingsTab,
  TailcatStatus,
  TailscaleStatus,
  ThemePref,
} from '../types';
import { api } from '../lib/api';
import { translate } from '../lib/i18n';
import { applyThemePref, getThemePref } from '../lib/theme';
import type { ViewDir, ViewState } from '../lib/useViewTransition';
import { BotAvatar } from './BotAvatar';

interface SettingsViewProps {
  hidden: boolean;
  viewState: ViewState;
  viewDir: ViewDir;
  viewAnim: boolean;
  tab: SettingsTab;
  bots: BotStatus[];
  settings: SettingsData | null;
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

  // Models & Bots draft
  const [defaultHarness, setDefaultHarness] = useState('opencode');
  const [defaultModel, setDefaultModel] = useState('');
  const [perHarnessOpen, setPerHarnessOpen] = useState(false);
  const [defaultModelsDraft, setDefaultModelsDraft] = useState<Record<string, string>>({});
  const [sharedUserMd, setSharedUserMd] = useState('');
  const [botDrafts, setBotDrafts] = useState<Record<string, { harness: string; model: string }>>({});
  const [saving, setSaving] = useState(false);
  const [settingsErr, setSettingsErr] = useState('');

  // Magpie
  const [harnessModelsDraft, setHarnessModelsDraft] = useState<Record<string, string>>({});
  const [magpieModels, setMagpieModels] = useState<Record<string, string[]>>({});
  const [connectingHarnessId, setConnectingHarnessId] = useState('');
  const [magpieBusyId, setMagpieBusyId] = useState('');
  const [copiedKey, setCopiedKey] = useState('');

  // Remote & Devices
  const [localNode, setLocalNode] = useState<LocalNodeState | null>(null);
  const [localNodeName, setLocalNodeName] = useState('');
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [tailcat, setTailcat] = useState<TailcatStatus | null>(null);
  const [tailscale, setTailscale] = useState<TailscaleStatus | null>(null);
  const [tsQrBust, setTsQrBust] = useState(() => Date.now());
  const [tsHostnameIn, setTsHostnameIn] = useState('');
  const [tsControlUrlIn, setTsControlUrlIn] = useState('');
  const [tsAuthKeyIn, setTsAuthKeyIn] = useState('');
  const [tsBusy, setTsBusy] = useState(false);
  const [tsErr, setTsErr] = useState('');
  const [qrBust, setQrBust] = useState(() => Date.now());
  const [remoteTokenIn, setRemoteTokenIn] = useState('');
  const [remoteTokenVal, setRemoteTokenVal] = useState('');
  const [remoteTokenShow, setRemoteTokenShow] = useState(false);
  const [localDeskEngine, setLocalDeskEngine] = useState('');
  const [localDeskSupported, setLocalDeskSupported] = useState(false);
  const [deskHarness, setDeskHarness] = useState('');
  const [deskModel, setDeskModel] = useState('');
  const [mpWebUrl, setMpWebUrl] = useState('');
  const [remoteErr, setRemoteErr] = useState('');
  const [remoteBusy, setRemoteBusy] = useState(false);

  // General
  const [themePref, setThemePref] = useState<ThemePref>(() => getThemePref());
  const [memData, setMemData] = useState<MemoryResponse | null>(null);
  const [memStatus, setMemStatus] = useState('');
  const [memStatusClass, setMemStatusClass] = useState('');
  const [memQuery, setMemQuery] = useState('');
  const [memAddOpen, setMemAddOpen] = useState(false);
  const [memKind, setMemKind] = useState('fact');
  const [memTags, setMemTags] = useState('');
  const [memText, setMemText] = useState('');
  const [memErr, setMemErr] = useState('');

  const remoteTimerRef = useRef<number | null>(null);
  const tsTimerRef = useRef<number | null>(null);
  const localNodeTimerRef = useRef<number | null>(null);

  const baseline = useMemo(() => {
    const cleanPer = Object.fromEntries(
      Object.entries(settings?.defaultModels || {}).filter(([, v]) => Boolean(v))
    );
    const botMap: Record<string, { harness: string; model: string }> = {};
    for (const b of bots) {
      botMap[b.id] = {
        harness: b.harness || 'opencode',
        model: b.modelInherited ? '' : b.model || '',
      };
    }
    return {
      defaultHarness: settings?.defaultHarness || settings?.harnesses?.[0]?.id || 'opencode',
      defaultModel: settings?.defaultModel || '',
      defaultModels: cleanPer,
      sharedUserMd: settings?.sharedUserMd || '',
      bots: botMap,
    };
  }, [settings, bots]);

  const isDirty = useMemo(() => {
    if (!settings) return false;
    if (defaultHarness !== baseline.defaultHarness) return true;
    if (defaultModel.trim() !== baseline.defaultModel) return true;
    const ka = Object.keys(defaultModelsDraft).filter((k) => defaultModelsDraft[k]?.trim());
    const kb = Object.keys(baseline.defaultModels).filter((k) => baseline.defaultModels[k]);
    if (ka.length !== kb.length || !ka.every((k) => defaultModelsDraft[k].trim() === baseline.defaultModels[k])) {
      return true;
    }
    if (sharedUserMd.replace(/\s+$/, '') !== baseline.sharedUserMd.replace(/\s+$/, '')) return true;
    for (const b of bots) {
      const cur = botDrafts[b.id];
      const base = baseline.bots[b.id];
      if (!cur || !base) continue;
      if (cur.harness !== base.harness || cur.model.trim() !== base.model) return true;
    }
    return false;
  }, [settings, defaultHarness, defaultModel, defaultModelsDraft, sharedUserMd, botDrafts, baseline, bots]);

  useEffect(() => {
    onDirtyChange?.(isDirty);
  }, [isDirty, onDirtyChange]);

  const resetDraftsToBaseline = useCallback(() => {
    setDefaultHarness(baseline.defaultHarness);
    setDefaultModel(baseline.defaultModel);
    setDefaultModelsDraft({ ...baseline.defaultModels });
    setPerHarnessOpen(Object.values(baseline.defaultModels).some(Boolean));
    setSharedUserMd(baseline.sharedUserMd);
    setBotDrafts({ ...baseline.bots });
    setSettingsErr('');
  }, [baseline]);

  useEffect(() => {
    if (resetRef) resetRef.current = resetDraftsToBaseline;
    return () => {
      if (resetRef) resetRef.current = null;
    };
  }, [resetRef, resetDraftsToBaseline]);

  useEffect(() => {
    if (!settings) return;
    if (!isDirty) {
      setDefaultHarness(settings.defaultHarness || settings.harnesses?.[0]?.id || 'opencode');
      setDefaultModel(settings.defaultModel || '');
      setDefaultModelsDraft({ ...(settings.defaultModels || {}) });
      setPerHarnessOpen(Object.values(settings.defaultModels || {}).some(Boolean));
      setSharedUserMd(settings.sharedUserMd || '');
      setBotDrafts(
        Object.fromEntries(
          bots.map((b) => [
            b.id,
            { harness: b.harness || 'opencode', model: b.modelInherited ? '' : b.model || '' },
          ])
        )
      );
    }
    setDeskHarness(settings.desktopHarness || '');
    setDeskModel(settings.desktopModel || '');
    setMpWebUrl(settings.magpie?.webUrl || '');
    settings.harnesses.forEach((h) => {
      void onLoadModels(h.id);
    });
  }, [settings, bots.length]);

  useEffect(() => {
    if (nodesFromEvents) setNodes(nodesFromEvents);
  }, [nodesFromEvents]);

  const loadLocalNode = async () => {
    try {
      const res = await fetch('/api/local-node');
      const st = res.ok ? ((await res.json()) as LocalNodeState) : null;
      const valid = st && st.available ? st : null;
      setLocalNode(valid);
      if (valid) setLocalNodeName(valid.name || '');
      if (localNodeTimerRef.current) window.clearTimeout(localNodeTimerRef.current);
      if (valid && valid.enabled && !valid.connected && !hidden && tab === 'remote') {
        localNodeTimerRef.current = window.setTimeout(() => void loadLocalNode(), 1000);
      }
    } catch {
      setLocalNode(null);
    }
  };

  const applyTailcatState = (tr: TailcatStatus) => {
    setTailcat(tr);
    if (tr.state === 'ready') setQrBust(Date.now());
    if (tr.apiToken) setRemoteTokenVal(tr.apiToken);
    else if (!tr.hasToken || tr.tokenEnvRef) setRemoteTokenVal('');
    if (remoteTimerRef.current) window.clearTimeout(remoteTimerRef.current);
    if (tr.state === 'connecting' && !hidden && tab === 'remote') {
      remoteTimerRef.current = window.setTimeout(async () => {
        try {
          applyTailcatState(await api<TailcatStatus>('/api/tailcat'));
        } catch {}
      }, 800);
    }
  };

  const applyTailscaleState = (ts: TailscaleStatus) => {
    setTailscale(ts);
    if (ts.state === 'running') setTsQrBust(Date.now());
    if (ts.hostname !== undefined) setTsHostnameIn((prev) => prev || ts.hostname || '');
    if (ts.controlUrl !== undefined) setTsControlUrlIn((prev) => prev || ts.controlUrl || '');
    if (ts.apiToken) setRemoteTokenVal(ts.apiToken);
    else if (!ts.hasToken || ts.tokenEnvRef) setRemoteTokenVal('');
    if (tsTimerRef.current) window.clearTimeout(tsTimerRef.current);
    if ((ts.state === 'starting' || ts.state === 'needs_login') && !hidden && tab === 'remote') {
      tsTimerRef.current = window.setTimeout(async () => {
        try {
          applyTailscaleState(await api<TailscaleStatus>('/api/tailscale'));
        } catch {}
      }, 2000);
    }
  };

  const refreshRemoteTab = () => {
    setRemoteErr('');
    setTsErr('');
    setRemoteTokenIn('');
    void loadLocalNode();
    api<{ nodes?: NodeInfo[] }>('/api/nodes')
      .then((d) => setNodes(d.nodes || []))
      .catch(() => setNodes([]));
    api<TailcatStatus>('/api/tailcat')
      .then(applyTailcatState)
      .catch((err) => applyTailcatState({ state: 'error', error: err instanceof Error ? err.message : '' }));
    api<TailscaleStatus>('/api/tailscale')
      .then(applyTailscaleState)
      .catch((err) => applyTailscaleState({ state: 'error', error: err instanceof Error ? err.message : '' }));
    api<{ status?: { webrtcAvailable?: boolean; supported?: boolean } }>('/api/desktop/state?target=local')
      .then((st) => {
        setLocalDeskEngine(st?.status?.webrtcAvailable ? 'bot-host (WebRTC H.264)' : t('jpegStream'));
        setLocalDeskSupported(Boolean(st?.status?.supported));
      })
      .catch(() => {
        setLocalDeskEngine(t('unavailable'));
        setLocalDeskSupported(false);
      });
  };

  const loadGlobalMemories = async () => {
    setMemErr('');
    setMemStatus(t('loading'));
    setMemStatusClass('');
    try {
      const q = memQuery.trim();
      const d = await api<MemoryResponse>(
        `/api/memory?scope=global&limit=30${q ? `&query=${encodeURIComponent(q)}` : ''}`
      );
      setMemData(d);
      if (!d.installed) {
        setMemStatus(t('notLoaded'));
        setMemStatusClass('');
      } else if (!d.available) {
        setMemStatus(t('notConnected'));
        setMemStatusClass('bad');
      } else {
        setMemStatus(t('itemsCount', { n: (d.items || []).length }));
        setMemStatusClass('ok');
      }
    } catch (e) {
      setMemStatus(t('failed'));
      setMemStatusClass('bad');
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    }
  };

  useEffect(() => {
    if (hidden) {
      if (remoteTimerRef.current) window.clearTimeout(remoteTimerRef.current);
      if (localNodeTimerRef.current) window.clearTimeout(localNodeTimerRef.current);
      return;
    }
    if (tab === 'remote') {
      refreshRemoteTab();
    } else if (tab === 'memory') {
      void loadGlobalMemories();
    }
    return () => {
      if (remoteTimerRef.current) window.clearTimeout(remoteTimerRef.current);
      if (localNodeTimerRef.current) window.clearTimeout(localNodeTimerRef.current);
    };
  }, [hidden, tab]);

  useEffect(() => {
    if (hidden || tab !== 'models') return;
    for (const h of settings?.harnesses || []) {
      if (models[h.id] === undefined) {
        void onLoadModels(h.id);
      }
    }
  }, [hidden, tab, settings?.harnesses, models, onLoadModels]);

  useEffect(() => {
    if (hidden || tab !== 'models' || !settings?.magpie?.installed || !connectingHarnessId) return;
    if (magpieModels[connectingHarnessId]) return;
    let cancelled = false;
    api<{ models?: string[] }>(`/api/models?harness=${encodeURIComponent(connectingHarnessId)}&source=magpie`)
      .then((d) => {
        if (cancelled) return;
        setMagpieModels((prev) => ({ ...prev, [connectingHarnessId]: d.models || [] }));
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [hidden, tab, settings?.magpie?.installed, connectingHarnessId, magpieModels]);

  const connectedHarnesses = useMemo(
    () => (settings?.harnesses || []).filter((h) => Boolean(h.magpie?.connected)),
    [settings?.harnesses]
  );
  const nativeHarnesses = useMemo(
    () => (settings?.harnesses || []).filter((h) => !h.magpie?.connected),
    [settings?.harnesses]
  );

  const globalModels = useMemo(() => {
    const seen = new Set<string>();
    for (const h of settings?.harnesses || []) {
      for (const m of models[h.id] || []) seen.add(m);
    }
    return [...seen];
  }, [settings?.harnesses, models]);

  const resolveDefaultModel = (g: string, h: string) => {
    if (!g) return '';
    if (g.startsWith('magpie/')) return h === 'claude' ? g.slice(7) : g;
    if (!g.includes('/')) return g;
    return h === '' || h === 'opencode' || h === 'pi' ? g : '';
  };

  const copyCmd = (text: string, id: string) => {
    navigator.clipboard?.writeText(text).then(() => {
      setCopiedKey(id);
      setTimeout(() => setCopiedKey((c) => (c === id ? '' : c)), 1200);
    });
  };

  const saveSettingsChanges = useCallback(async (): Promise<boolean> => {
    setSaving(true);
    setSettingsErr('');
    try {
      const perHarness = Object.fromEntries(
        Object.entries(defaultModelsDraft)
          .map(([k, v]) => [k, v.trim()])
          .filter(([, v]) => Boolean(v))
      );
      const general = defaultModel.trim();
      const equalPer =
        Object.keys(perHarness).length === Object.keys(baseline.defaultModels).length &&
        Object.keys(perHarness).every((k) => perHarness[k] === baseline.defaultModels[k]);
      if (
        defaultHarness !== baseline.defaultHarness ||
        general !== baseline.defaultModel ||
        !equalPer ||
        sharedUserMd.replace(/\s+$/, '') !== baseline.sharedUserMd.replace(/\s+$/, '')
      ) {
        const next = await api<SettingsData>('/api/settings', {
          method: 'PUT',
          body: { defaultHarness, defaultModel: general, defaultModels: perHarness, sharedUserMd },
        });
        onSettingsUpdated(next);
      }
      for (const b of bots) {
        const draft = botDrafts[b.id];
        if (!draft) continue;
        const cur = b.modelInherited ? '' : b.model || '';
        if (draft.harness === b.harness && draft.model.trim() === cur) continue;
        await api(`/api/bots/${encodeURIComponent(b.id)}`, {
          method: 'PATCH',
          body: { harness: draft.harness, model: draft.model.trim() },
        });
      }
      onToast(t('saved'));
      await onReloadBots();
      return true;
    } catch (err) {
      if (err instanceof Error && err.message !== 'unauthorized') setSettingsErr(err.message);
      return false;
    } finally {
      setSaving(false);
    }
  }, [defaultHarness, defaultModelsDraft, defaultModel, baseline, sharedUserMd, onSettingsUpdated, bots, botDrafts, onToast, t, onReloadBots]);

  useEffect(() => {
    if (saveRef) saveRef.current = saveSettingsChanges;
    return () => {
      if (saveRef) saveRef.current = null;
    };
  }, [saveRef, saveSettingsChanges]);

  const handleSaveSettings = async (e: React.FormEvent) => {
    e.preventDefault();
    await saveSettingsChanges();
  };

  const prevTabRef = useRef<SettingsTab>(tab);
  const [tabAnim, setTabAnim] = useState(false);
  useEffect(() => {
    if (prevTabRef.current === tab) return;
    prevTabRef.current = tab;
    if (viewState !== 'active') return;
    setTabAnim(true);
    const id = window.setTimeout(() => setTabAnim(false), 200);
    return () => window.clearTimeout(id);
  }, [tab, viewState]);

  const titleKey =
    tab === 'remote'
      ? 'tabRemote'
      : tab === 'memory'
        ? 'tabMemory'
        : tab === 'general'
          ? 'tabGeneral'
          : 'tabModels';
  const mp = settings?.magpie;
  const onlineNodeCount = nodes.filter((n) => n.connected).length;

  const applyHarnessMagpie = async (hId: string, hLabel: string, model: string) => {
    const trimmed = model.trim();
    if (!trimmed) {
      setSettingsErr(t('mpPickModelErr', { label: hLabel }));
      return;
    }
    setSettingsErr('');
    setMagpieBusyId(hId);
    try {
      const next = await api<SettingsData>(
        `/api/harnesses/${encodeURIComponent(hId)}/magpie`,
        { method: 'POST', body: { action: 'connect', model: trimmed } }
      );
      setHarnessModelsDraft((prev) => {
        const copy = { ...prev };
        delete copy[hId];
        return copy;
      });
      onSettingsUpdated(next);
      setConnectingHarnessId('');
      onResetModels();
      void onLoadModels(hId, true);
      onToast(t('mpConnectedToast', { label: hLabel }));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setSettingsErr(e.message);
    } finally {
      setMagpieBusyId('');
    }
  };

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
          <button
            type="submit"
            form="settingsForm"
            id="settingsSubmit"
            className="btn go set-save"
            disabled={!isDirty || saving}
            hidden={tab === 'remote' && !isDirty}
          >
            {t('saveConfig')}
          </button>
        </div>
      </div>

      <datalist id="models-global">
        {globalModels.map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>
      {(settings?.harnesses || []).map((h) => (
        <React.Fragment key={h.id}>
          <datalist id={`models-${h.id}`}>
            {(models[h.id] || []).map((m) => (
              <option key={m} value={m} />
            ))}
          </datalist>
          <datalist id={`models-magpie-${h.id}`}>
            {(magpieModels[h.id] || []).map((m) => (
              <option key={m} value={m} />
            ))}
          </datalist>
        </React.Fragment>
      ))}

      <div className="scroll">
        <form id="settingsForm" className="set-body" autoComplete="off" onSubmit={handleSaveSettings}>
          {/* Tab 1: Models & Bots */}
          <div id="sTabModels" className={`tab-pane${tab === 'models' && tabAnim ? ' tab-anim' : ''}`} hidden={tab !== 'models'}>
            <section className="set-card">
              <div className="set-card-hd">
                <h2>{t('magpieUnified')}</h2>
                <span
                  id="sMagpieStatusPill"
                  className={`mp${!mp?.installed ? '' : mp.healthy ? ' ok' : ' bad'}`}
                >
                  {!mp ? t('checking') : !mp.installed ? t('mpNotInstalled') : mp.healthy ? t('mpHealthy') : t('mpUnhealthy')}
                </span>
              </div>

              {mp && !mp.installed && (
                <div id="sMagpieInstall" className="note">
                  <div>{t('mpInstallHint')}</div>
                  {mp.install?.script && (
                    <div className="cmd">
                      <code>{mp.install.script}</code>
                      <button type="button" onClick={() => copyCmd(mp.install!.script!, 'mp-script')}>
                        {copiedKey === 'mp-script' ? t('copied') : t('copy')}
                      </button>
                    </div>
                  )}
                  {mp.install?.brew && (
                    <div className="cmd">
                      <code>{mp.install.brew}</code>
                      <button type="button" onClick={() => copyCmd(mp.install!.brew!, 'mp-brew')}>
                        {copiedKey === 'mp-brew' ? t('copied') : t('copy')}
                      </button>
                    </div>
                  )}
                </div>
              )}

              {mp?.installed && (
                <div id="sMagpieBox" className="set-stack">
                  <p className="set-card-desc">{t('magpieIntro')}</p>
                  <div id="sMagpieHarnesses" className="stack6">
                    {connectedHarnesses.length === 0 ? (
                      <div className="sub">{t('mpNoConnected')}</div>
                    ) : (
                      connectedHarnesses.map((h) => {
                        const activeModel = (h.magpie?.model || '').trim();
                        const isBusy = magpieBusyId === h.id;
                        return (
                          <div key={h.id} className="mp-hrow">
                            <div className="mp-hname">
                              <span>{h.label}</span>
                              <small className="text-sage">{t('mpConnected')}</small>
                            </div>
                            <span className="mp-hmodel">{activeModel}</span>
                            <div className="mp-hacts">
                              <button
                                type="button"
                                className="mp"
                                disabled={isBusy}
                                onClick={() =>
                                  onConfirm({
                                    title: t('mpRestoreTitle', { label: h.label }),
                                    text: t('mpRestoreDesc', { id: h.id }),
                                    okText: t('mpConfirmRestore'),
                                    okClass: 'bad',
                                    onOk: async () => {
                                      const next = await api<SettingsData>(
                                        `/api/harnesses/${encodeURIComponent(h.id)}/magpie`,
                                        { method: 'POST', body: { action: 'disconnect' } }
                                      );
                                      onSettingsUpdated(next);
                                      onResetModels();
                                      void onLoadModels(h.id, true);
                                      onToast(t('mpRestoredToast', { label: h.label }));
                                    },
                                  })
                                }
                              >
                                {t('mpRestoreNative')}
                              </button>
                            </div>
                          </div>
                        );
                      })
                    )}
                  </div>

                  <div className="mp-open">
                    <span className="set-card-desc">{t('mpManageInApp')}</span>
                    <button
                      type="button"
                      className="mp go"
                      onClick={async () => {
                        setSettingsErr('');
                        const local = ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname);
                        if (mp.webUrl && !local) {
                          window.open(mp.webUrl, '_blank', 'noopener');
                          return;
                        }
                        try {
                          await api('/api/magpie/open', { method: 'POST' });
                        } catch (e) {
                          if (e instanceof Error && e.message !== 'unauthorized') setSettingsErr(e.message);
                        }
                      }}
                    >
                      {t('mpOpenApp')}
                    </button>
                  </div>
                  <div className="fld">
                    <label>
                      {t('mpWebUrl')}
                      <em>{t('mpWebUrlHint')}</em>
                    </label>
                    <input
                      className="mono"
                      placeholder="https://magpie.example.com"
                      spellCheck={false}
                      value={mpWebUrl}
                      onChange={(e) => setMpWebUrl(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          e.currentTarget.blur();
                        }
                      }}
                      onBlur={async () => {
                        const next = mpWebUrl.trim();
                        if (next === (mp.webUrl || '')) return;
                        setSettingsErr('');
                        try {
                          const fresh = await api<SettingsData>('/api/settings', {
                            method: 'PUT',
                            body: { magpieWebUrl: next },
                          });
                          onSettingsUpdated(fresh);
                          onToast(t('mpWebUrlSaved'));
                        } catch (e) {
                          if (e instanceof Error && e.message !== 'unauthorized') setSettingsErr(e.message);
                        }
                      }}
                    />
                  </div>
                </div>
              )}
            </section>

            {nativeHarnesses.length > 0 && (
              <section id="sNativeClisCard" className="set-card">
                <div className="set-card-hd">
                  <h2>{t('nativeClisTitle')}</h2>
                </div>
                <p className="set-card-desc">{t('nativeClisIntro')}</p>
                <div id="sNativeHarnesses" className="stack6">
                  {nativeHarnesses.map((h) => {
                    const isBusy = magpieBusyId === h.id;
                    const isConnecting = connectingHarnessId === h.id;
                    const draftMagpie = harnessModelsDraft[h.id] ?? '';
                    return (
                      <div key={h.id} className="mp-native-item">
                        <div className="mp-hrow">
                          <div className="mp-hname">
                            <span>{h.label}</span>
                            <small>
                              {t('mpNative')}
                              {h.magpie?.nativeModel ? ` · ${h.magpie.nativeModel}` : ''}
                            </small>
                          </div>
                          <input
                            className="mono"
                            list={`models-${h.id}`}
                            placeholder={t('nativeModelPh')}
                            spellCheck={false}
                            disabled={isBusy}
                            value={defaultModelsDraft[h.id] || ''}
                            onFocus={() => void onLoadModels(h.id)}
                            onChange={(e) =>
                              setDefaultModelsDraft((prev) => ({ ...prev, [h.id]: e.target.value }))
                            }
                          />
                          <div className="mp-hacts">
                            {mp?.installed && (
                              <button
                                type="button"
                                className="mp"
                                disabled={isBusy}
                                onClick={() =>
                                  setConnectingHarnessId((prev) => (prev === h.id ? '' : h.id))
                                }
                              >
                                {isConnecting ? t('mpConnectCancel') : t('mpConnectAction')}
                              </button>
                            )}
                          </div>
                        </div>
                        {mp?.installed && isConnecting && (
                          <div className="mp-connect-drawer">
                            <input
                              className="mono"
                              list={`models-magpie-${h.id}`}
                              placeholder={t('mpModelPh')}
                              spellCheck={false}
                              disabled={isBusy}
                              value={draftMagpie}
                              onChange={(e) => {
                                const nextVal = e.target.value;
                                setHarnessModelsDraft((prev) => ({ ...prev, [h.id]: nextVal }));
                              }}
                              onKeyDown={(e) => {
                                if (e.key === 'Enter') {
                                  e.preventDefault();
                                  void applyHarnessMagpie(h.id, h.label, draftMagpie);
                                }
                              }}
                            />
                            <button
                              type="button"
                              className="mp go"
                              disabled={isBusy}
                              onClick={() => void applyHarnessMagpie(h.id, h.label, draftMagpie)}
                            >
                              {t('mpConnect')}
                            </button>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </section>
            )}

            <section className="set-card">
              <div className="set-card-hd">
                <h2>{t('defaultModel')}</h2>
                <button
                  type="button"
                  id="sTogglePerHarness"
                  className="link-btn"
                  onClick={() => setPerHarnessOpen((v) => !v)}
                >
                  {perHarnessOpen ? t('collapse') : t('perHarness')}
                </button>
              </div>
              <div className="fld">
                <label>
                  {t('defaultHarness')} <em>{t('defaultHarnessDesc')}</em>
                </label>
                <select
                  id="sDefaultHarness"
                  value={defaultHarness}
                  onChange={(e) => setDefaultHarness(e.target.value)}
                >
                  {(settings?.harnesses || [{ id: 'opencode', label: 'opencode' }]).map((h) => (
                    <option key={h.id} value={h.id}>
                      {h.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label dangerouslySetInnerHTML={{ __html: t('globalDefaultModel') }} />
                <input
                  id="sDefaultModel"
                  className="mono"
                  spellCheck={false}
                  list="models-global"
                  placeholder={t('globalDefaultPh')}
                  value={defaultModel}
                  onChange={(e) => setDefaultModel(e.target.value)}
                />
              </div>
              {perHarnessOpen && (
                <div id="sPerHarnessBox" className="note">
                  <div className="set-card-desc">{t('perHarnessDesc')}</div>
                  <div id="sPerHarnessFields" className="two">
                    {(settings?.harnesses || []).map((h) => (
                      <div key={h.id} className="fld">
                        <label>{h.label}</label>
                        <input
                          className="mono"
                          list={`models-${h.id}`}
                          placeholder={t('followGlobalPh')}
                          spellCheck={false}
                          value={defaultModelsDraft[h.id] || ''}
                          onChange={(e) =>
                            setDefaultModelsDraft((prev) => ({ ...prev, [h.id]: e.target.value }))
                          }
                        />
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </section>

            <section className="set-card">
              <div className="set-card-hd">
                <h2>{t('botModels')}</h2>
                <span className="set-card-desc">{t('botModelsDesc')}</span>
              </div>
              <div id="sBots" className="rows">
                {bots.length === 0 ? (
                  <div className="sub">{t('noBotsSimple')}</div>
                ) : (
                  bots.map((b) => {
                    const draft = botDrafts[b.id] || {
                      harness: b.harness || 'opencode',
                      model: b.modelInherited ? '' : b.model || '',
                    };
                    const dm = defaultModelsDraft[draft.harness]?.trim() || '';
                    const def = dm || resolveDefaultModel(defaultModel.trim(), draft.harness);
                    return (
                      <div key={b.id} className="brow">
                        <BotAvatar id={b.id} bots={bots} />
                        <div className="nm" title={b.id}>
                          {b.name}
                        </div>
                        <select
                          value={draft.harness}
                          onChange={(e) => {
                            const h = e.target.value;
                            setBotDrafts((prev) => ({ ...prev, [b.id]: { harness: h, model: '' } }));
                            void onLoadModels(h);
                          }}
                        >
                          {(settings?.harnesses || []).map((h) => (
                            <option key={h.id} value={h.id}>
                              {h.label}
                            </option>
                          ))}
                        </select>
                        <input
                          list={`models-${draft.harness}`}
                          spellCheck={false}
                          placeholder={def ? t('defaultModelPh', { model: def }) : t('inheritGlobalPh')}
                          value={draft.model}
                          onChange={(e) =>
                            setBotDrafts((prev) => ({
                              ...prev,
                              [b.id]: { ...draft, model: e.target.value },
                            }))
                          }
                        />
                      </div>
                    );
                  })
                )}
              </div>
            </section>
          </div>

          {/* Tab 2: Remote & Devices */}
          <div id="sTabRemote" className={`tab-pane${tab === 'remote' && tabAnim ? ' tab-anim' : ''}`} hidden={tab !== 'remote'}>
            {localNode && (
              <section className="set-card" id="localNodeBox">
                <div className="set-card-hd">
                  <h2>
                    <span>{t('localNode')}</span>{' '}
                    <em id="localNodeId">{localNode.name ? `(${localNode.name})` : ''}</em>
                  </h2>
                  <div className="row-inline-sm">
                    <span id="localNodeState" className={`mp${localNode.connected ? ' ok' : ''}`}>
                      {({
                        disabled: t('disabled'),
                        connecting: t('nodeConnecting'),
                        connected: t('nodeConnected'),
                        waiting_auth: t('nodeWaitingAuth'),
                        error: t('nodeError'),
                      } as Record<string, string>)[localNode.status] || localNode.status}
                    </span>
                    <button
                      type="button"
                      className="mp"
                      id="localNodeToggle"
                      onClick={() => {
                        const next = !localNode.enabled;
                        const apply = async () => {
                          setRemoteErr('');
                          try {
                            const res = await fetch('/api/local-node', {
                              method: 'POST',
                              headers: { 'Content-Type': 'application/json' },
                              body: JSON.stringify({ enabled: next }),
                            });
                            const d = (await res.json()) as LocalNodeState & { error?: string };
                            if (!res.ok) throw new Error(d.error || `HTTP ${res.status}`);
                            setLocalNode(d);
                            onToast(d.enabled ? t('localNodeEnabled') : t('localNodeDisabled'));
                            setTimeout(
                              () =>
                                api<{ nodes?: NodeInfo[] }>('/api/nodes')
                                  .then((r) => setNodes(r.nodes || []))
                                  .catch(() => {}),
                              350
                            );
                          } catch (e) {
                            setRemoteErr(e instanceof Error ? e.message : String(e));
                          }
                        };
                        if (!next) {
                          void apply();
                          return;
                        }
                        onConfirm({
                          title: t('localNodeConfirmTitle'),
                          text: t('localNodeConfirmDesc'),
                          okText: t('confirmEnable'),
                          okClass: 'go',
                          onOk: apply,
                        });
                      }}
                    >
                      {localNode.enabled ? t('close') : t('enableControlBtn')}
                    </button>
                  </div>
                </div>
                <div className="row-inline">
                  <input
                    id="localNodeNameIn"
                    className="note-in"
                    placeholder={t('localNodeNamePh')}
                    autoComplete="off"
                    value={localNodeName}
                    onChange={(e) => setLocalNodeName(e.target.value)}
                  />
                  <button
                    type="button"
                    className="mp"
                    id="localNodeNameSave"
                    onClick={async () => {
                      const name = localNodeName.trim();
                      if (!name) {
                        setRemoteErr(t('nodeNameEmpty'));
                        return;
                      }
                      setRemoteErr('');
                      try {
                        const res = await fetch('/api/local-node', {
                          method: 'POST',
                          headers: { 'Content-Type': 'application/json' },
                          body: JSON.stringify({ name }),
                        });
                        const d = await res.json();
                        if (!res.ok) throw new Error(d.error || `HTTP ${res.status}`);
                        setLocalNode(d);
                        onToast(t('nodeNameSaved'));
                      } catch (e) {
                        setRemoteErr(e instanceof Error ? e.message : String(e));
                      }
                    }}
                  >
                    {t('saveName')}
                  </button>
                </div>
                <p className="set-card-desc">{t('localNodeDesc')}</p>
                <div className="perms" id="localNodePerms">
                  {(
                    [
                      ['files', 'permFiles'],
                      ['exec', 'permExec'],
                      ['desktop', 'permDesktop'],
                    ] as const
                  ).map(([perm, labelKey]) => {
                    const checked = (localNode.permissions || []).includes(perm);
                    return (
                      <label key={perm} className="perm">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={async (e) => {
                            const cur = new Set(localNode.permissions || []);
                            if (e.target.checked) cur.add(perm);
                            else cur.delete(perm);
                            try {
                              const res = await fetch('/api/local-node', {
                                method: 'POST',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({ permissions: [...cur] }),
                              });
                              const d = await res.json();
                              if (!res.ok) throw new Error(d.error || `HTTP ${res.status}`);
                              setLocalNode(d);
                            } catch (err) {
                              setRemoteErr(err instanceof Error ? err.message : String(err));
                            }
                          }}
                        />
                        <span>{t(labelKey)}</span>
                      </label>
                    );
                  })}
                </div>
              </section>
            )}

            <section className="set-card">
              <div className="set-card-hd">
                <h2>
                  <span>{t('tailcatTunnel')}</span>{' '}
                  <em>
                    ·{' '}
                    <span id="remoteState">
                      {tailcat
                        ? {
                            disabled: t('disabled'),
                            connecting: t('remoteConnecting'),
                            ready: t('remoteReady'),
                            error: t('remoteError'),
                          }[tailcat.state] || tailcat.state
                        : t('unreadState')}
                    </span>
                  </em>
                </h2>
                <button
                  type="button"
                  id="remoteToggle"
                  className={`mp ${!tailcat || tailcat.state === 'disabled' || tailcat.state === 'error' ? 'go' : 'bad'}`}
                  disabled={remoteBusy}
                  onClick={() => {
                    const enable = !tailcat || tailcat.state === 'disabled' || tailcat.state === 'error';
                    const run = async () => {
                      setRemoteBusy(true);
                      setRemoteErr('');
                      try {
                        const body: Record<string, unknown> = { enabled: enable };
                        if (enable) body.apiToken = remoteTokenIn.trim();
                        applyTailcatState(await api<TailcatStatus>('/api/tailcat', { method: 'POST', body }));
                        onToast(enable ? t('remoteEnabledToast') : t('remoteDisabledToast'));
                      } catch (e) {
                        if (e instanceof Error && e.message !== 'unauthorized') setRemoteErr(e.message);
                      } finally {
                        setRemoteBusy(false);
                      }
                    };
                    if (enable) void run();
                    else {
                      onConfirm({
                        title: t('remoteDisableTitle'),
                        text: t('remoteDisableDesc'),
                        okText: t('confirmDisable'),
                        onOk: run,
                      });
                    }
                  }}
                >
                  {!tailcat || tailcat.state === 'disabled' || tailcat.state === 'error'
                    ? tailcat?.state === 'error'
                      ? t('remoteRetry')
                      : t('enableRemote')
                    : t('remoteStop')}
                </button>
              </div>
              {tailcat?.state === 'ready' && (
                <>
                  <img
                    id="remoteQR"
                    alt=""
                    src={`/api/tailcat/qr.png?t=${qrBust}`}
                    className="qr-box"
                  />
                  <div className="fld" id="remoteLinkFld">
                    <textarea
                      id="remoteLink"
                      className="mono ta-sm"
                      readOnly
                      value={tailcat.link || ''}
                    />
                  </div>
                  <div className="row-end" id="remoteCopyWrap">
                    <button
                      type="button"
                      id="remoteCopy"
                      className="mp go"
                      onClick={async () => {
                        try {
                          await navigator.clipboard.writeText(tailcat.link || '');
                          onToast(t('pairLinkCopied'));
                        } catch {
                          onToast(t('copyFailed'));
                        }
                      }}
                    >
                      {t('copyPairLink')}
                    </button>
                  </div>
                </>
              )}
              {(tailcat?.state === 'disabled' || tailcat?.state === 'error') && !tailcat.hasToken && (
                <div className="fld" id="remoteTokenFld">
                  <label dangerouslySetInnerHTML={{ __html: t('accessTokenOptional') }} />
                  <input
                    id="remoteTokenIn"
                    className="mono"
                    placeholder={t('accessTokenPh')}
                    autoComplete="off"
                    value={remoteTokenIn}
                    onChange={(e) => setRemoteTokenIn(e.target.value)}
                  />
                </div>
              )}
              <p id="remoteHelp" className="set-card-desc">
                {tailcat?.state === 'ready'
                  ? t('remoteHelpReady')
                  : !tailcat || tailcat.state === 'disabled'
                    ? t('remoteHelpOff')
                    : tailcat.state === 'connecting'
                      ? t('remoteHelpConnecting')
                      : tailcat.error || t('remoteHelpFallback')}
              </p>
              {tailcat?.hasToken && (
                <div className="note" id="remoteTokBox">
                  <div className="row-between">
                    <span id="remoteTokTitle">
                      {tailcat.tokenEnvRef ? t('accessTokenEnv') : t('accessTokenReusable')}
                    </span>
                    {!tailcat.tokenEnvRef && (
                      <div id="remoteTokActs" className="row-inline-sm">
                        <button
                          type="button"
                          className="mp"
                          id="remoteTokEye"
                          onClick={async () => {
                            if (!remoteTokenShow) {
                              if (!remoteTokenVal) {
                                const d = await api<TailcatStatus>('/api/tailcat?reveal=1');
                                setRemoteTokenVal(d.apiToken || '');
                              }
                              setRemoteTokenShow(true);
                            } else {
                              setRemoteTokenShow(false);
                            }
                          }}
                        >
                          {remoteTokenShow ? t('hide') : t('show')}
                        </button>
                        <button
                          type="button"
                          className="mp"
                          id="remoteTokReset"
                          onClick={() =>
                            onConfirm({
                              title: t('resetTokenTitle'),
                              text: t('resetTokenDesc'),
                              okText: t('confirmResetToken'),
                              onOk: async () => {
                                const r = await api<TailcatStatus>('/api/tailcat', {
                                  method: 'POST',
                                  body: { resetToken: true },
                                });
                                setRemoteTokenShow(true);
                                applyTailcatState(r);
                                onToast(t('tokenResetToast'));
                              },
                            })
                          }
                        >
                          {t('resetToken')}
                        </button>
                      </div>
                    )}
                  </div>
                  {tailcat.tokenEnvRef ? (
                    <div id="remoteTokEnv" className="set-card-desc">
                      {t('accessTokenEnvHint', { ref: tailcat.tokenEnvRef })}
                    </div>
                  ) : (
                    <div className="cmd" id="remoteTokCmd">
                      <code id="remoteTokVal">
                        {remoteTokenShow && remoteTokenVal ? remoteTokenVal : '•'.repeat(24)}
                      </code>
                      <button
                        type="button"
                        id="remoteTokCopy"
                        onClick={async () => {
                          try {
                            let tok = remoteTokenVal;
                            if (!tok) {
                              const d = await api<TailcatStatus>('/api/tailcat?reveal=1');
                              tok = d.apiToken || '';
                              setRemoteTokenVal(tok);
                            }
                            await navigator.clipboard.writeText(tok);
                            onToast(t('tokenCopied'));
                          } catch {
                            onToast(t('copyFailed'));
                          }
                        }}
                      >
                        {t('copy')}
                      </button>
                    </div>
                  )}
                </div>
              )}
            </section>

            <section className="set-card" id="tailscaleBox">
              <div className="set-card-hd">
                <h2>
                  <span>{t('tsTitle')}</span>{' '}
                  <em>
                    ·{' '}
                    <span id="tsState">
                      {tailscale
                        ? {
                            disabled: t('disabled'),
                            starting: t('tsStarting'),
                            needs_login: t('tsNeedsLogin'),
                            running: t('tsRunning'),
                            error: t('remoteError'),
                          }[tailscale.state] || tailscale.state
                        : t('unreadState')}
                    </span>
                  </em>
                </h2>
                <button
                  type="button"
                  id="tsToggle"
                  className={`mp ${!tailscale || tailscale.state === 'disabled' || tailscale.state === 'error' ? 'go' : 'bad'}`}
                  disabled={tsBusy}
                  onClick={async () => {
                    const enable = !tailscale || tailscale.state === 'disabled' || tailscale.state === 'error';
                    setTsBusy(true);
                    setTsErr('');
                    try {
                      const body: Record<string, unknown> = { enabled: enable };
                      if (enable) {
                        if (tsHostnameIn.trim()) body.hostname = tsHostnameIn.trim();
                        if (tsControlUrlIn.trim()) body.controlUrl = tsControlUrlIn.trim();
                        if (tsAuthKeyIn.trim()) body.authKey = tsAuthKeyIn.trim();
                        if (remoteTokenIn.trim()) body.apiToken = remoteTokenIn.trim();
                      }
                      const res = await api<TailscaleStatus>('/api/tailscale', { method: 'POST', body });
                      setTsAuthKeyIn('');
                      applyTailscaleState(res);
                      onToast(enable ? t('tsEnabledToast') : t('tsDisabledToast'));
                    } catch (e) {
                      if (e instanceof Error && e.message !== 'unauthorized') setTsErr(e.message);
                    } finally {
                      setTsBusy(false);
                    }
                  }}
                >
                  {!tailscale || tailscale.state === 'disabled' || tailscale.state === 'error'
                    ? tailscale?.state === 'error'
                      ? t('remoteRetry')
                      : t('tsBind')
                    : t('tsDisable')}
                </button>
              </div>

              {tailscale?.state === 'needs_login' && tailscale.authUrl && (
                <div className="note">
                  <div className="row-inline-sm" style={{ flexWrap: 'wrap' }}>
                    <a
                      href={tailscale.authUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="mp go"
                      id="tsOpenLogin"
                    >
                      {t('tsOpenLogin')}
                    </a>
                    <button
                      type="button"
                      className="mp"
                      onClick={async () => {
                        try {
                          await navigator.clipboard.writeText(tailscale.authUrl || '');
                          onToast(t('tsLoginUrlCopied'));
                        } catch {
                          onToast(t('copyFailed'));
                        }
                      }}
                    >
                      {t('tsCopyLoginUrl')}
                    </button>
                  </div>
                </div>
              )}

              {tailscale?.state === 'running' && (
                <>
                  <img
                    id="tsQR"
                    alt=""
                    src={`/api/tailscale/qr.png?t=${tsQrBust}`}
                    className="qr-box"
                  />
                  {(tailscale.dnsName || tailscale.ip) && (
                    <p className="set-card-desc">
                      <code className="mono">{tailscale.dnsName || tailscale.ip}</code>
                      {tailscale.dnsName && tailscale.ip ? ` (${tailscale.ip})` : ''}
                    </p>
                  )}
                  <div className="fld">
                    <textarea
                      id="tsLink"
                      className="mono ta-sm"
                      readOnly
                      value={tailscale.link || ''}
                    />
                  </div>
                  <div className="row-end" style={{ gap: '8px' }}>
                    <button
                      type="button"
                      className="mp bad"
                      disabled={tsBusy}
                      onClick={() =>
                        onConfirm({
                          title: t('tsLogoutTitle'),
                          text: t('tsLogoutDesc'),
                          okText: t('tsConfirmLogout'),
                          onOk: async () => {
                            setTsBusy(true);
                            try {
                              const res = await api<TailscaleStatus>('/api/tailscale', {
                                method: 'POST',
                                body: { logout: true },
                              });
                              applyTailscaleState(res);
                              onToast(t('tsLoggedOutToast'));
                            } finally {
                              setTsBusy(false);
                            }
                          },
                        })
                      }
                    >
                      {t('tsLogout')}
                    </button>
                    <button
                      type="button"
                      className="mp go"
                      onClick={async () => {
                        try {
                          await navigator.clipboard.writeText(tailscale.link || '');
                          onToast(t('tsAddressCopied'));
                        } catch {
                          onToast(t('copyFailed'));
                        }
                      }}
                    >
                      {t('tsCopyLink')}
                    </button>
                  </div>
                </>
              )}

              <p className="set-card-desc">
                {tailscale?.state === 'running'
                  ? t('tsHelpReady', { tailnet: tailscale.tailnet || 'Tailscale' })
                  : tailscale?.state === 'needs_login'
                    ? t('tsHelpLogin')
                    : tailscale?.state === 'starting'
                      ? t('tsHelpStarting')
                      : tailscale?.error || tsErr || t('tsHelpOff')}
              </p>

              <details style={{ marginTop: '8px' }}>
                <summary className="set-card-desc" style={{ cursor: 'pointer' }}>
                  {t('tsAdvanced')}
                </summary>
                <div style={{ marginTop: '10px', display: 'flex', flexDirection: 'column', gap: '10px' }}>
                  <div className="fld">
                    <label>{t('tsHostname')}</label>
                    <input
                      className="mono"
                      placeholder="bothub-mac"
                      value={tsHostnameIn}
                      onChange={(e) => setTsHostnameIn(e.target.value)}
                    />
                  </div>
                  <div className="fld">
                    <label>{t('tsAuthKey')}</label>
                    {tailscale?.authKeyEnvRef ? (
                      <div className="set-card-desc">
                        {t('tsAuthKeyEnvHint', { ref: tailscale.authKeyEnvRef })}
                      </div>
                    ) : (
                      <input
                        type="password"
                        className="mono"
                        placeholder={t('tsAuthKeyPh')}
                        autoComplete="off"
                        value={tsAuthKeyIn}
                        onChange={(e) => setTsAuthKeyIn(e.target.value)}
                      />
                    )}
                  </div>
                  <div className="fld">
                    <label>{t('tsControlUrl')}</label>
                    <input
                      className="mono"
                      placeholder={t('tsControlUrlPh')}
                      autoComplete="off"
                      value={tsControlUrlIn}
                      onChange={(e) => setTsControlUrlIn(e.target.value)}
                    />
                  </div>
                </div>
              </details>
            </section>

            <section className="set-card" id="localDeskBox">
              <div className="set-card-hd">
                <h2>{t('localDesktopTitle')}</h2>
                <span id="localDeskEngine" className={`mp${localDeskSupported ? ' ok' : ''}`}>
                  {localDeskEngine || t('checking')}
                </span>
              </div>
              <div className="two">
                <div className="fld">
                  <label>
                    {t('deskWorkerHarness')} <em>{t('rtOptional')}</em>
                  </label>
                  <select
                    id="sDeskHarness"
                    value={deskHarness}
                    onChange={(e) => {
                      setDeskHarness(e.target.value);
                      if (e.target.value) void onLoadModels(e.target.value);
                    }}
                  >
                    <option value="">{t('deskWorkerFollowBot')}</option>
                    {(settings?.harnesses || []).map((h) => (
                      <option key={h.id} value={h.id}>
                        {h.label}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="fld">
                  <label>
                    {t('deskWorkerModel')} <em>{t('deskWorkerModelHint')}</em>
                  </label>
                  <div className="row-inline">
                    <input
                      id="sDeskModel"
                      className="mono flex-1"
                      list={deskHarness ? `models-${deskHarness}` : 'models-global'}
                      placeholder={t('rtModelPh')}
                      spellCheck={false}
                      value={deskModel}
                      onChange={(e) => setDeskModel(e.target.value)}
                    />
                    <button
                      type="button"
                      id="sDeskWorkerSave"
                      className="mp go"
                      disabled={remoteBusy}
                      onClick={async () => {
                        setRemoteBusy(true);
                        setRemoteErr('');
                        try {
                          const next = await api<SettingsData>('/api/settings', {
                            method: 'PUT',
                            body: { desktopHarness: deskHarness, desktopModel: deskModel.trim() },
                          });
                          onSettingsUpdated(next);
                          onToast(t('deskWorkerSaved'));
                        } catch (e) {
                          if (e instanceof Error && e.message !== 'unauthorized') setRemoteErr(e.message);
                        } finally {
                          setRemoteBusy(false);
                        }
                      }}
                    >
                      {t('save')}
                    </button>
                  </div>
                </div>
              </div>
              <p className="set-card-desc">
                {t('deskWorkerDesc')}
              </p>
            </section>

            <section className="set-card" id="remoteNodesBox">
              <div className="set-card-hd">
                <h2>{t('connectedNodes')}</h2>
                <span className="mp" id="remoteNodeCount">
                  {t('onlineCount', { n: onlineNodeCount })}
                </span>
              </div>
              <div id="remoteNodesList" className="stack6">
                {nodes.length === 0 ? (
                  <div className="set-card-desc">{t('noRemoteNodes')}</div>
                ) : (
                  nodes.map((n) => {
                    const permLabel = (p: string) =>
                      t(({ files: 'permFileShort', exec: 'permExecShort', desktop: 'permDeskShort' } as Record<string, string>)[p] || p);
                    const perms = (n.permissions || []).map(permLabel).join(' · ') || t('noPerm');
                    const meta = [n.os && n.arch ? `${n.os}/${n.arch}` : n.os || '', perms]
                      .filter(Boolean)
                      .join(' · ');
                    const canDesk = n.connected && (n.permissions || []).includes('desktop');
                    return (
                      <div key={n.id} className="node-row">
                        <div>
                          <div className="nm">{n.name || n.id}</div>
                          <div className="meta">{meta}</div>
                        </div>
                        <div className="row-inline-sm">
                          {canDesk && (
                            <button type="button" className="mp" onClick={() => onOpenDesktopTarget(n.id)}>
                              {t('openDesktop')}
                            </button>
                          )}
                          <span className={`mp${n.connected ? ' ok' : ''}`}>
                            {n.connected ? t('online') : t('offline')}
                          </span>
                        </div>
                      </div>
                    );
                  })
                )}
              </div>
            </section>
            <div id="remoteErr" className="msg-err">
              {remoteErr}
            </div>
          </div>

          {/* Tab 3: General */}
          <div id="sTabGeneral" className={`tab-pane${tab === 'general' && tabAnim ? ' tab-anim' : ''}`} hidden={tab !== 'general'}>
            <section className="set-card">
              <div className="set-card-hd">
                <h2>{t('appearance')}</h2>
                <span className="set-card-desc">{t('appearanceDesc')}</span>
              </div>
              <div className="fld">
                <select
                  id="sTheme"
                  value={themePref}
                  onChange={(e) => {
                    const next = e.target.value as ThemePref;
                    setThemePref(next);
                    applyThemePref(next);
                  }}
                >
                  <option value="system">{t('followSystem')}</option>
                  <option value="light">{t('themeLight')}</option>
                  <option value="dark">{t('themeDark')}</option>
                </select>
              </div>
            </section>

            <section className="set-card">
              <div className="set-card-hd">
                <h2>{t('language')}</h2>
                <span className="set-card-desc">{t('languageDesc')}</span>
              </div>
              <div className="fld">
                <select
                  id="sLang"
                  value={langPref}
                  onChange={(e) => onLangChange(e.target.value as LangPref)}
                >
                  <option value="system">{t('followSystem')}</option>
                  <option value="zh-CN">简体中文</option>
                  <option value="en">English</option>
                </select>
              </div>
            </section>

            <section className="set-card">
              <div className="set-card-hd">
                <h2>{t('ubCardTitle')}</h2>
                <button type="button" id="sReopenUnbox" className="mp" onClick={onReopenUnbox}>
                  {t('ubReopenBtn')}
                </button>
              </div>
              <p className="set-card-desc">{t('ubCardDesc')}</p>
            </section>
          </div>

          {/* Tab 4: Memory */}
          <div id="sTabMemory" className={`tab-pane${tab === 'memory' && tabAnim ? ' tab-anim' : ''}`} hidden={tab !== 'memory'}>
            <section className="set-card">
              <div className="set-card-hd">
                <h2>{t('globalSharedMem')}</h2>
                <span className="set-card-desc">{t('globalSharedMemDesc')}</span>
              </div>
              <div className="fld">
                <textarea
                  id="sUserMd"
                  rows={8}
                  className="ta-lg"
                  spellCheck={false}
                  placeholder={t('userMdPh')}
                  value={sharedUserMd}
                  onChange={(e) => setSharedUserMd(e.target.value)}
                />
              </div>
            </section>

            <section className="set-card" id="sMemCard">
              <div className="set-card-hd">
                <h2>{t('magpieGlobalMem')}</h2>
                <div className="row-inline-sm row-wrap">
                  <span id="sMemStatus" className={`mp ${memStatusClass}`}>
                    {memStatus || t('notLoaded')}
                  </span>
                  <button type="button" id="sMemRefresh" className="mp" onClick={() => void loadGlobalMemories()}>
                    {t('refresh')}
                  </button>
                  <button
                    type="button"
                    id="sMemAddToggle"
                    className="mp go"
                    onClick={() => setMemAddOpen((v) => !v)}
                  >
                    {t('addMemoryBtn')}
                  </button>
                </div>
              </div>
              <p className="set-card-desc">{t('magpieGlobalMemDesc')}</p>
              {memAddOpen && (
                <div id="sMemAddBox" className="add-prov mem-add">
                  <div className="two">
                    <div className="fld">
                      <label>{t('memType')}</label>
                      <select id="sMemKind" value={memKind} onChange={(e) => setMemKind(e.target.value)}>
                        <option value="fact">{t('kindFact')}</option>
                        <option value="preference">{t('kindPreference')}</option>
                        <option value="rule">{t('kindRule')}</option>
                        <option value="reference">{t('kindReference')}</option>
                        <option value="profile">{t('kindProfile')}</option>
                        <option value="procedure">{t('kindProcedure')}</option>
                        <option value="episode">{t('kindEpisode')}</option>
                      </select>
                    </div>
                    <div className="fld">
                      <label dangerouslySetInnerHTML={{ __html: t('tagsOptional') }} />
                      <input
                        id="sMemTags"
                        placeholder={t('tagsPh')}
                        value={memTags}
                        onChange={(e) => setMemTags(e.target.value)}
                      />
                    </div>
                  </div>
                  <div className="fld">
                    <label>{t('memContent')}</label>
                    <textarea
                      id="sMemText"
                      rows={3}
                      className="ta-md ta-font"
                      placeholder={t('globalMemPh')}
                      value={memText}
                      onChange={(e) => setMemText(e.target.value)}
                    />
                  </div>
                  <div className="row-end-sm">
                    <button
                      type="button"
                      id="sMemAddCancel"
                      className="mp"
                      onClick={() => {
                        setMemAddOpen(false);
                        setMemErr('');
                      }}
                    >
                      {t('cancel')}
                    </button>
                    <button
                      type="button"
                      id="sMemAddSubmit"
                      className="mp go"
                      onClick={async () => {
                        const text = memText.trim();
                        if (!text) {
                          setMemErr(t('memContentReq'));
                          return;
                        }
                        const tags = memTags
                          .split(/[,，]/)
                          .map((s) => s.trim())
                          .filter(Boolean);
                        setMemErr('');
                        try {
                          await api('/api/memory', {
                            method: 'POST',
                            body: { scope: 'global', kind: memKind, text, tags },
                          });
                          setMemText('');
                          setMemTags('');
                          setMemAddOpen(false);
                          onToast(t('globalMemSaved'));
                          await loadGlobalMemories();
                        } catch (e) {
                          if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
                        }
                      }}
                    >
                      {t('saveMemory')}
                    </button>
                  </div>
                </div>
              )}

              <div className="mem-bar">
                <input
                  id="sMemQuery"
                  className="note-in"
                  placeholder={t('searchGlobalMemPh')}
                  value={memQuery}
                  onChange={(e) => setMemQuery(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                      e.preventDefault();
                      void loadGlobalMemories();
                    }
                  }}
                />
                <button type="button" id="sMemSearchBtn" className="mp" onClick={() => void loadGlobalMemories()}>
                  {t('search')}
                </button>
              </div>
              <div id="sMemList" className="stack6">
                {!memData || !memData.installed ? (
                  <div className="set-card-desc">{t('memNotInstalled')}</div>
                ) : !memData.available ? (
                  <div className="set-card-desc">
                    {t('memUnavailable')}
                    {memData.error ? `: ${memData.error}` : ''}
                  </div>
                ) : !(memData.items || []).length ? (
                  <div className="set-card-desc">{t('memEmpty')}</div>
                ) : (
                  (memData.items || []).map((it) => {
                    const tags = (it.tags || []).filter((tag) => !tag.startsWith('bot:'));
                    const tm = it.updatedAt ? it.updatedAt.replace('T', ' ').slice(0, 16) : '';
                    return (
                      <div key={it.id} className="mem-row">
                        <div className="mem-top">
                          <div className="mem-meta">
                            {it.kind && <span className="mp ok">{it.kind}</span>}
                            {tags.map((tg) => (
                              <span key={tg} className="mp">
                                #{tg}
                              </span>
                            ))}
                            {tm && <span className="mem-time">{tm}</span>}
                          </div>
                          <button
                            type="button"
                            className="mp bad"
                            onClick={async () => {
                              setMemErr('');
                              try {
                                await api(`/api/memory/${encodeURIComponent(it.id)}`, { method: 'DELETE' });
                                onToast(t('memDeleted'));
                                await loadGlobalMemories();
                              } catch (e) {
                                if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
                              }
                            }}
                          >
                            {t('delBtn')}
                          </button>
                        </div>
                        <div className="mem-txt">{it.text}</div>
                      </div>
                    );
                  })
                )}
              </div>
              <div id="sMemErr" className="msg-err">
                {memErr}
              </div>
            </section>
          </div>

          <div id="settingsErr" className="msg-err">
            {settingsErr}
          </div>
        </form>
      </div>
    </section>
  );
};
