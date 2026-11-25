import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  BotStatus,
  ConfirmOptions,
  HarnessInfo,
  PermissionMode,
  ResolvedLang,
  SettingsData,
  SetupTaskStatus,
} from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { BotAvatar } from '../BotAvatar';
import { HarnessIcon } from '../HarnessIcon';
import { MagpieInstallBanner } from '../MagpieInstallBanner';

interface ModelsTabProps {
  active: boolean;
  bots: BotStatus[];
  settings: SettingsData | null;
  models: Record<string, string[]>;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onConfirm: (opts: ConfirmOptions) => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onReloadBots: () => Promise<void>;
  onLoadModels: (harness: string, force?: boolean) => Promise<string[]>;
  onResetModels: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onSavingChange: (saving: boolean) => void;
  saveRef: React.MutableRefObject<(() => Promise<boolean>) | null>;
  resetRef: React.MutableRefObject<(() => void) | null>;
}

const resolveDefaultModel = (g: string, h: string) => {
  if (!g) return '';
  if (g.startsWith('magpie/')) return h === 'claude' ? g.slice(7) : g;
  if (!g.includes('/')) return g;
  return h === '' || h === 'opencode' || h === 'pi' ? g : '';
};

export const ModelsTab: React.FC<ModelsTabProps> = ({
  active,
  bots,
  settings,
  models,
  lang,
  onToast,
  onSettingsUpdated,
  onReloadBots,
  onLoadModels,
  onResetModels,
  onDirtyChange,
  onSavingChange,
  saveRef,
  resetRef,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [defaultHarness, setDefaultHarness] = useState('opencode');
  const [defaultPermMode, setDefaultPermMode] = useState<PermissionMode>('auto');
  const [defaultModel, setDefaultModel] = useState('');
  const [defaultModelsDraft, setDefaultModelsDraft] = useState<Record<string, string>>({});
  const [mpWebUrlDraft, setMpWebUrlDraft] = useState('');
  const [botDrafts, setBotDrafts] = useState<Record<string, { harness: string; model: string }>>({});
  const [touchedFields, setTouchedFields] = useState({
    defaultHarness: false,
    defaultPermMode: false,
    defaultModel: false,
    defaultModels: false,
    magpieWebUrl: false,
  });
  const [touchedBots, setTouchedBots] = useState<Set<string>>(() => new Set());
  const [err, setErr] = useState('');

  // Engine install/login
  const [moreEnginesOpen, setMoreEnginesOpen] = useState(false);
  const [perOverridesOpen, setPerOverridesOpen] = useState(false);
  const [expandedInstall, setExpandedInstall] = useState<Set<string>>(() => new Set());
  const [installTasks, setInstallTasks] = useState<Record<string, SetupTaskStatus>>({});
  const [loginTasks, setLoginTasks] = useState<Record<string, SetupTaskStatus>>({});
  const [rechecking, setRechecking] = useState(false);
  const [copiedKey, setCopiedKey] = useState('');

  const pollTimersRef = useRef<Record<string, number>>({});
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      for (const tid of Object.values(pollTimersRef.current)) {
        window.clearTimeout(tid);
      }
      pollTimersRef.current = {};
    };
  }, []);

  const preloadAllModels = useCallback(
    (list: SettingsData['harnesses'] | undefined, force = false) => {
      for (const h of list || []) {
        if (h.installed !== false) void onLoadModels(h.id, force);
      }
    },
    [onLoadModels]
  );

  const baseline = useMemo(() => {
    const cleanPer = Object.fromEntries(
      Object.entries(settings?.defaultModels || {})
        .map(([k, v]) => [k, v.trim()])
        .filter(([, v]) => Boolean(v))
    );
    const botMap: Record<string, { harness: string; model: string }> = {};
    for (const b of bots) {
      botMap[b.id] = {
        harness: b.harness || 'opencode',
        model: (b.modelInherited ? '' : b.model || '').trim(),
      };
    }
    return {
      defaultHarness: settings?.defaultHarness || settings?.harnesses?.[0]?.id || 'opencode',
      defaultPermMode: (settings?.defaultPermissionMode || 'auto') as PermissionMode,
      defaultModel: (settings?.defaultModel || '').trim(),
      defaultModels: cleanPer,
      magpieWebUrl: (settings?.magpie?.webUrl || '').trim(),
      bots: botMap,
    };
  }, [settings, bots]);

  const isDirty = useMemo(() => {
    if (!settings) return false;
    if (touchedFields.defaultHarness && defaultHarness !== baseline.defaultHarness) return true;
    if (touchedFields.defaultPermMode && defaultPermMode !== baseline.defaultPermMode) return true;
    if (touchedFields.defaultModel && defaultModel.trim() !== baseline.defaultModel) return true;
    if (touchedFields.magpieWebUrl && mpWebUrlDraft.trim() !== baseline.magpieWebUrl) return true;
    if (touchedFields.defaultModels) {
      const ka = Object.keys(defaultModelsDraft).filter((k) => defaultModelsDraft[k]?.trim());
      const kb = Object.keys(baseline.defaultModels);
      if (ka.length !== kb.length || !ka.every((k) => defaultModelsDraft[k].trim() === baseline.defaultModels[k])) {
        return true;
      }
    }
    for (const b of bots) {
      if (!touchedBots.has(b.id)) continue;
      const cur = botDrafts[b.id];
      const base = baseline.bots[b.id];
      if (!cur || !base) continue;
      if (cur.harness !== base.harness || cur.model.trim() !== base.model) return true;
    }
    return false;
  }, [
    settings,
    touchedFields,
    touchedBots,
    defaultHarness,
    defaultPermMode,
    defaultModel,
    mpWebUrlDraft,
    defaultModelsDraft,
    botDrafts,
    baseline,
    bots,
  ]);

  useEffect(() => {
    onDirtyChange(isDirty);
  }, [isDirty, onDirtyChange]);

  useEffect(() => {
    return () => {
      onDirtyChange(false);
      onSavingChange(false);
    };
  }, [onDirtyChange, onSavingChange]);

  const resetDrafts = useCallback(() => {
    setTouchedFields({
      defaultHarness: false,
      defaultPermMode: false,
      defaultModel: false,
      defaultModels: false,
      magpieWebUrl: false,
    });
    setTouchedBots(new Set());
    setDefaultHarness(baseline.defaultHarness);
    setDefaultPermMode(baseline.defaultPermMode);
    setDefaultModel(baseline.defaultModel);
    setDefaultModelsDraft({ ...baseline.defaultModels });
    setMpWebUrlDraft(baseline.magpieWebUrl);
    setBotDrafts({ ...baseline.bots });
    setErr('');
  }, [baseline]);

  useEffect(() => {
    if (!settings) return;
    if (!touchedFields.defaultHarness) setDefaultHarness(baseline.defaultHarness);
    if (!touchedFields.defaultPermMode) setDefaultPermMode(baseline.defaultPermMode);
    if (!touchedFields.defaultModel) setDefaultModel(baseline.defaultModel);
    if (!touchedFields.defaultModels) setDefaultModelsDraft({ ...baseline.defaultModels });
    if (!touchedFields.magpieWebUrl) setMpWebUrlDraft(baseline.magpieWebUrl);
  }, [settings, baseline, touchedFields]);

  useEffect(() => {
    setBotDrafts((prev) => {
      const next: Record<string, { harness: string; model: string }> = {};
      let changed = false;
      const validIds = new Set(bots.map((b) => b.id));
      for (const id of Object.keys(prev)) {
        if (!validIds.has(id)) changed = true;
      }
      for (const b of bots) {
        if (touchedBots.has(b.id) && prev[b.id]) {
          next[b.id] = prev[b.id];
          continue;
        }
        const target = {
          harness: b.harness || 'opencode',
          model: (b.modelInherited ? '' : b.model || '').trim(),
        };
        const cur = prev[b.id];
        if (!cur || cur.harness !== target.harness || cur.model !== target.model) changed = true;
        next[b.id] = target;
      }
      return changed ? next : prev;
    });
  }, [bots, touchedBots]);

  useEffect(() => {
    if (!active) return;
    preloadAllModels(settings?.harnesses);
  }, [active, settings?.harnesses, preloadAllModels]);

  const saveAll = useCallback(async (): Promise<boolean> => {
    onSavingChange(true);
    setErr('');
    try {
      const perHarness = Object.fromEntries(
        Object.entries(defaultModelsDraft)
          .map(([k, v]) => [k, v.trim()])
          .filter(([, v]) => Boolean(v))
      );
      const general = defaultModel.trim();
      const webUrl = mpWebUrlDraft.trim();
      const equalPer =
        Object.keys(perHarness).length === Object.keys(baseline.defaultModels).length &&
        Object.keys(perHarness).every((k) => perHarness[k] === baseline.defaultModels[k]);
      if (
        defaultHarness !== baseline.defaultHarness ||
        defaultPermMode !== baseline.defaultPermMode ||
        general !== baseline.defaultModel ||
        !equalPer ||
        webUrl !== baseline.magpieWebUrl
      ) {
        const next = await api<SettingsData>('/api/settings', {
          method: 'PUT',
          body: {
            defaultHarness,
            defaultPermissionMode: defaultPermMode,
            defaultModel: general,
            defaultModels: perHarness,
            magpieWebUrl: webUrl,
          },
        });
        onSettingsUpdated(next);
      }
      for (const b of bots) {
        const draft = botDrafts[b.id];
        if (!draft) continue;
        const cur = (b.modelInherited ? '' : b.model || '').trim();
        if (draft.harness === b.harness && draft.model.trim() === cur) continue;
        await api(`/api/bots/${encodeURIComponent(b.id)}`, {
          method: 'PATCH',
          body: { harness: draft.harness, model: draft.model.trim() },
        });
      }
      setTouchedFields({
        defaultHarness: false,
        defaultPermMode: false,
        defaultModel: false,
        defaultModels: false,
        magpieWebUrl: false,
      });
      setTouchedBots(new Set());
      onToast(t('saved'));
      await onReloadBots();
      return true;
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
      return false;
    } finally {
      onSavingChange(false);
    }
  }, [
    onSavingChange,
    defaultModelsDraft,
    defaultModel,
    mpWebUrlDraft,
    baseline,
    defaultHarness,
    defaultPermMode,
    onSettingsUpdated,
    bots,
    botDrafts,
    onToast,
    t,
    onReloadBots,
  ]);

  useEffect(() => {
    saveRef.current = saveAll;
    resetRef.current = resetDrafts;
    return () => {
      saveRef.current = null;
      resetRef.current = null;
    };
  }, [saveRef, resetRef, saveAll, resetDrafts]);

  const recheckEnvironment = async () => {
    setRechecking(true);
    setErr('');
    try {
      const fresh = await api<SettingsData>('/api/settings?fresh=1');
      onSettingsUpdated(fresh);
      onResetModels();
      preloadAllModels(fresh.harnesses, true);
      onToast(t('ubRecheckDone'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setRechecking(false);
    }
  };

  const pollHarnessLogin = async (id: string) => {
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/login`);
      if (!mountedRef.current) return;
      setLoginTasks((prev) => ({ ...prev, [id]: st }));
      if (st.authStatus === 'ready' || st.state === 'done') {
        onResetModels();
        const fresh = await api<SettingsData>('/api/settings?fresh=1');
        onSettingsUpdated(fresh);
        preloadAllModels(fresh.harnesses, true);
        onToast(t('engineLoginConfirmed'));
        return;
      }
      if (st.state === 'running') {
        pollTimersRef.current[`login:${id}`] = window.setTimeout(() => void pollHarnessLogin(id), 1500);
      } else if (st.state === 'error' && st.error) {
        setErr(st.error);
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  const startHarnessLogin = async (id: string) => {
    setErr('');
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/login`, {
        method: 'POST',
      });
      if (!mountedRef.current) return;
      setLoginTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'done' || st.authStatus === 'ready') {
        onResetModels();
        const fresh = await api<SettingsData>('/api/settings?fresh=1');
        onSettingsUpdated(fresh);
        onToast(t('engineLoginConfirmed'));
        return;
      }
      if (st.state === 'running') {
        if (pollTimersRef.current[`login:${id}`]) window.clearTimeout(pollTimersRef.current[`login:${id}`]);
        pollTimersRef.current[`login:${id}`] = window.setTimeout(() => void pollHarnessLogin(id), 1200);
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  const pollHarnessInstall = async (id: string) => {
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/install`);
      if (!mountedRef.current) return;
      setInstallTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'running') {
        pollTimersRef.current[`install:${id}`] = window.setTimeout(() => void pollHarnessInstall(id), 1200);
        return;
      }
      if (st.state === 'done') {
        onResetModels();
        const fresh = await api<SettingsData>('/api/settings?fresh=1');
        onSettingsUpdated(fresh);
        preloadAllModels(fresh.harnesses, true);
        const h = fresh.harnesses?.find((item) => item.id === id);
        onToast(t('engineInstallDone', { label: h?.label || id }));
        if (h?.requiresLogin && h.authStatus === 'needs_login') {
          void startHarnessLogin(id);
        }
      } else if (st.state === 'error' && st.error) {
        setExpandedInstall((prev) => new Set(prev).add(id));
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  const startHarnessInstall = async (id: string) => {
    setErr('');
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/install`, {
        method: 'POST',
      });
      if (!mountedRef.current) return;
      setInstallTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'running') {
        if (pollTimersRef.current[`install:${id}`]) window.clearTimeout(pollTimersRef.current[`install:${id}`]);
        pollTimersRef.current[`install:${id}`] = window.setTimeout(() => void pollHarnessInstall(id), 1200);
      } else if (st.state === 'done') {
        onResetModels();
        const fresh = await api<SettingsData>('/api/settings?fresh=1');
        onSettingsUpdated(fresh);
        const h = fresh.harnesses?.find((item) => item.id === id);
        onToast(t('engineInstallDone', { label: h?.label || id }));
        if (h?.requiresLogin && h.authStatus === 'needs_login') {
          void startHarnessLogin(id);
        }
      } else if (st.state === 'error') {
        setExpandedInstall((prev) => new Set(prev).add(id));
      }
    } catch (e) {
      setExpandedInstall((prev) => new Set(prev).add(id));
      if (e instanceof Error && e.message !== 'unauthorized') {
        setInstallTasks((prev) => ({
          ...prev,
          [id]: { target: id, kind: 'install', state: 'error', error: e.message },
        }));
      }
    }
  };

  const copyText = (text: string, key: string) => {
    navigator.clipboard?.writeText(text).then(() => {
      setCopiedKey(key);
      setTimeout(() => setCopiedKey((cur) => (cur === key ? '' : cur)), 1200);
    });
  };

  const mp = settings?.magpie;
  const harnesses = settings?.harnesses || [];
  const installedHarnesses = harnesses.filter((h) => h.installed !== false);
  const connectedHarnesses = installedHarnesses.filter((h) => Boolean(h.magpie?.connected));
  const nativeHarnesses = installedHarnesses.filter((h) => !h.magpie?.connected);
  const uninstalledHarnesses = harnesses.filter((h) => h.installed === false);

  const renderInstalledEngine = (h: HarnessInfo) => {
    const connected = Boolean(h.magpie?.connected);
    const loginTask = loginTasks[h.id];
    const loggingIn = loginTask?.state === 'running';
    const needsLogin = h.authStatus === 'needs_login';
    const loginErr = loginTask?.state === 'error' ? loginTask.error || '' : '';

    return (
      <div key={h.id} className="mp-native-item">
        <div className="mp-hrow">
          <div className="mp-hname-row">
            <HarnessIcon id={h.id} />
            <div className="mp-hname-meta">
              <div className="mp-hname-badges">
                <span className={`harness-dot${connected ? ' ok' : needsLogin ? ' bad' : ''}`} />
                <span className="nm">{h.label}</span>
                {needsLogin && <span className="mp bad">{t('engineNeedsLogin')}</span>}
              </div>
              {connected ? (
                <small className="harness-sub-ok">{h.magpie?.model || t('mpConnected')}</small>
              ) : h.magpie?.nativeModel ? (
                <small className="harness-sub-mono">{h.magpie.nativeModel}</small>
              ) : (
                <small>{h.authStatus === 'ready' ? t('engineReady') : h.id}</small>
              )}
            </div>
          </div>

          {needsLogin && (
            <div className="mp-hacts">
              <button
                type="button"
                className="mp go"
                disabled={loggingIn}
                onClick={() => void startHarnessLogin(h.id)}
              >
                {loggingIn ? t('engineLoggingIn') : t('engineLoginBtn')}
              </button>
            </div>
          )}
        </div>

        {needsLogin && (loggingIn || loginTask?.loginUrl || loginErr) && (
          <div className="unbox-install">
            {loginErr && <div className="msg-err">{loginErr}</div>}
            <div className="row-inline row-wrap">
              {loginTask?.loginUrl && (
                <a href={loginTask.loginUrl} target="_blank" rel="noopener noreferrer" className="mp go">
                  {t('engineOpenLogin')}
                </a>
              )}
              <button type="button" className="mp" onClick={() => void pollHarnessLogin(h.id)}>
                {t('engineCheckLogin')}
              </button>
            </div>
          </div>
        )}
      </div>
    );
  };

  const renderUninstalledEngine = (h: HarnessInfo) => {
    const expanded = expandedInstall.has(h.id);
    const instTask = installTasks[h.id];
    const installing = instTask?.state === 'running';
    const installErr = instTask?.state === 'error' ? instTask.error || t('ubInstallFailed') : '';
    const lastLog = installing ? instTask?.logs?.[instTask.logs.length - 1] || t('engineInstalling') : '';
    const rawCmd = (h.install?.command || '').trim();
    const url = h.install?.url || '';
    const isShellCmd = Boolean(rawCmd);
    const cmdText = isShellCmd ? rawCmd : url ? t('engineManualHint', { name: h.label }) : '';
    const descKey = `ubDesc_${h.id}`;
    const descVal = t(descKey);
    const sub = lastLog || (descVal !== descKey ? descVal : t('ubNotInstalled'));

    const toggleExpand = () => {
      setExpandedInstall((prev) => {
        const next = new Set(prev);
        if (next.has(h.id)) next.delete(h.id);
        else next.add(h.id);
        return next;
      });
    };

    return (
      <div key={h.id} className="engine-uninstalled-row">
        <div className="engine-uninstalled-hd">
          <div className="engine-uninstalled-main">
            <HarnessIcon id={h.id} />
            <div className="mp-hname-meta">
              <span className="nm">{h.label}</span>
              <small>{sub}</small>
            </div>
          </div>
          {h.canAutoInstall ? (
            <button
              type="button"
              className="mp go"
              disabled={installing}
              onClick={() => void startHarnessInstall(h.id)}
            >
              {installing ? t('engineInstalling') : t('engineInstallBtn')}
            </button>
          ) : (
            <button type="button" className="mp" onClick={toggleExpand}>
              {expanded ? t('collapse') : t('engineManualInstall')}
            </button>
          )}
        </div>
        {(expanded || installErr) && (
          <div className="unbox-install">
            {installErr && <div className="msg-err">{installErr}</div>}
            {cmdText && (
              <div className="cmd">
                <code>{cmdText}</code>
                {isShellCmd && (
                  <button type="button" onClick={() => copyText(rawCmd, `eng-inst-${h.id}`)}>
                    {copiedKey === `eng-inst-${h.id}` ? t('copied') : t('copy')}
                  </button>
                )}
              </div>
            )}
            {url && (
              <a href={url} target="_blank" rel="noopener noreferrer">
                {t('engineViewDocs')}
              </a>
            )}
          </div>
        )}
      </div>
    );
  };

  return (
    <div id="sTabModels" className="tab-pane tab-anim">
      {/* Card 1: Defaults & Bot Models */}
      <section className="set-card">
        <div className="set-card-hd">
          <h2>{t('globalAndBotModels')}</h2>
        </div>
        <div className="two">
          <div className="fld">
            <label>
              {t('defaultHarness')} <em>{t('defaultHarnessDesc')}</em>
            </label>
            <select
              id="sDefaultHarness"
              value={defaultHarness}
              onChange={(e) => {
                const nextVal = e.target.value;
                if (nextVal === defaultHarness) return;
                setTouchedFields((prev) => ({ ...prev, defaultHarness: true }));
                setDefaultHarness(nextVal);
                void onLoadModels(nextVal);
              }}
            >
              {(harnesses.length > 0 ? harnesses : [{ id: 'opencode', label: 'opencode' }]).map((h) => (
                <option key={h.id} value={h.id}>
                  {h.label}
                </option>
              ))}
            </select>
          </div>
          <div className="fld">
            <label>
              {t('permDefaultMode')} <em>{t('permDefaultModeDesc')}</em>
            </label>
            <select
              id="sDefaultPermMode"
              value={defaultPermMode}
              onChange={(e) => {
                const nextVal = e.target.value as PermissionMode;
                if (nextVal === defaultPermMode) return;
                setTouchedFields((prev) => ({ ...prev, defaultPermMode: true }));
                setDefaultPermMode(nextVal);
              }}
            >
              {(['auto', 'edit', 'ask', 'plan'] as PermissionMode[]).map((m) => (
                <option key={m} value={m}>
                  {t(`permMode_${m}`)}
                </option>
              ))}
            </select>
          </div>
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
            onFocus={() => {
              void onLoadModels(defaultHarness);
              preloadAllModels(settings?.harnesses);
            }}
            onChange={(e) => {
              const nextVal = e.target.value;
              if (nextVal === defaultModel) return;
              setTouchedFields((prev) => ({ ...prev, defaultModel: true }));
              setDefaultModel(nextVal);
            }}
          />
        </div>

        {installedHarnesses.length > 0 && (() => {
          const overrideCount = installedHarnesses.filter((h) => (defaultModelsDraft[h.id] || '').trim()).length;
          return (
            <div>
              <button
                type="button"
                className="link-btn engine-more-btn"
                onClick={() => setPerOverridesOpen((v) => !v)}
              >
                {perOverridesOpen
                  ? t('collapse')
                  : overrideCount > 0
                    ? t('perHarnessOverridesCount', { n: overrideCount })
                    : t('perHarnessOverrides')}
              </button>
              {perOverridesOpen && (
                <div className="per-harness-list">
                  {installedHarnesses.map((h) => (
                    <div key={h.id} className="per-harness-row">
                      <label>{h.label}</label>
                      <input
                        className="mono"
                        list={`models-${h.id}`}
                        placeholder={t('engineModelPh')}
                        spellCheck={false}
                        value={defaultModelsDraft[h.id] || ''}
                        onFocus={() => void onLoadModels(h.id)}
                        onChange={(e) => {
                          const nextVal = e.target.value;
                          if (nextVal === (defaultModelsDraft[h.id] || '')) return;
                          setTouchedFields((prev) => ({ ...prev, defaultModels: true }));
                          setDefaultModelsDraft((prev) => ({ ...prev, [h.id]: nextVal }));
                        }}
                      />
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })()}

        <div className="set-subsec-hd">
          <h3>{t('botModels')}</h3>
          <span className="set-card-desc">{t('botModelsDesc')}</span>
        </div>
        <div id="sBots" className="rows">
          {bots.length === 0 ? (
            <div className="sub">{t('noBotsSimple')}</div>
          ) : (
            bots.map((b) => {
              const draft = botDrafts[b.id] || {
                harness: b.harness || 'opencode',
                model: (b.modelInherited ? '' : b.model || '').trim(),
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
                      if (h === draft.harness) return;
                      setTouchedBots((prev) => new Set(prev).add(b.id));
                      setBotDrafts((prev) => ({ ...prev, [b.id]: { harness: h, model: '' } }));
                      void onLoadModels(h);
                    }}
                  >
                    {harnesses.map((h) => (
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
                    onFocus={() => void onLoadModels(draft.harness)}
                    onChange={(e) => {
                      const nextVal = e.target.value;
                      if (nextVal === draft.model) return;
                      setTouchedBots((prev) => new Set(prev).add(b.id));
                      setBotDrafts((prev) => ({ ...prev, [b.id]: { ...draft, model: nextVal } }));
                    }}
                  />
                </div>
              );
            })
          )}
        </div>
      </section>

      {/* Card 2: Harnesses (read-only routing status) */}
      <section id="sEnginesCard" className="set-card">
        <div className="set-card-hd">
          <h2>{t('enginesTitle')}</h2>
          <button
            type="button"
            id="sEnginesRecheck"
            className="mp"
            disabled={rechecking}
            onClick={() => void recheckEnvironment()}
          >
            {rechecking ? t('loading') : t('enginesRecheck')}
          </button>
        </div>
        <p className="set-card-desc">{t('enginesIntro')}</p>
        <div id="sEnginesInstalled">
          {installedHarnesses.length === 0 ? (
            <div className="set-card-desc">{t('engineNoInstalled')}</div>
          ) : (
            <>
              {connectedHarnesses.length > 0 && (
                <div className="harness-group">
                  <div className="harness-group-hd ok">
                    <span>{t('harnessGroupConnected')} ({connectedHarnesses.length})</span>
                  </div>
                  <div className="harness-group-list">
                    {connectedHarnesses.map(renderInstalledEngine)}
                  </div>
                </div>
              )}
              {nativeHarnesses.length > 0 && (
                <div className="harness-group">
                  <div className="harness-group-hd">
                    <span>{t('harnessGroupNative')} ({nativeHarnesses.length})</span>
                  </div>
                  <div className="harness-group-list">
                    {nativeHarnesses.map(renderInstalledEngine)}
                  </div>
                </div>
              )}
            </>
          )}
        </div>
        {uninstalledHarnesses.length > 0 && (
          <>
            <button
              type="button"
              id="sEnginesMoreToggle"
              className="link-btn engine-more-btn"
              onClick={() => setMoreEnginesOpen((v) => !v)}
            >
              {moreEnginesOpen
                ? t('collapse')
                : t('enginesMoreAvailable', { n: uninstalledHarnesses.length })}
            </button>
            {moreEnginesOpen && (
              <div id="sEnginesAvailable" className="stack6">
                {uninstalledHarnesses.map(renderUninstalledEngine)}
              </div>
            )}
          </>
        )}
      </section>

      {/* Card 3: Magpie Gateway */}
      <section className="set-card">
        <div className="set-card-hd">
          <h2>{t('magpieGatewayTitle')}</h2>
          <span
            id="sMagpieStatusPill"
            className={`mp${!mp?.installed ? '' : mp.healthy ? ' ok' : ' bad'}`}
          >
            {!mp ? t('checking') : !mp.installed ? t('mpNotInstalled') : mp.healthy ? t('mpHealthy') : t('mpUnhealthy')}
          </span>
        </div>

        {mp && !mp.installed && (
          <div id="sMagpieInstall" className="note">
            <MagpieInstallBanner
              lang={lang}
              hint={t('mpInstallHint')}
              installCmds={mp.install}
              onToast={onToast}
              onInstalled={async () => {
                const fresh = await api<SettingsData>('/api/settings?fresh=1');
                onSettingsUpdated(fresh);
                onResetModels();
                preloadAllModels(fresh.harnesses, true);
              }}
            />
          </div>
        )}

        {mp?.installed && (
          <div id="sMagpieBox" className="set-stack">
            <div className="mp-open">
              <span className="set-card-desc">{t('mpManageInApp')}</span>
              <button
                type="button"
                className="mp go"
                onClick={async () => {
                  setErr('');
                  const local = ['localhost', '127.0.0.1', '[::1]'].includes(location.hostname);
                  if (mp.webUrl && !local) {
                    window.open(mp.webUrl, '_blank', 'noopener');
                    return;
                  }
                  try {
                    await api('/api/magpie/open', { method: 'POST' });
                  } catch (e) {
                    if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
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
                id="sMagpieWebUrl"
                className="mono"
                placeholder="https://magpie.example.com"
                spellCheck={false}
                value={mpWebUrlDraft}
                onChange={(e) => {
                  const nextVal = e.target.value;
                  if (nextVal === mpWebUrlDraft) return;
                  setTouchedFields((prev) => ({ ...prev, magpieWebUrl: true }));
                  setMpWebUrlDraft(nextVal);
                }}
              />
            </div>
          </div>
        )}
      </section>

      {err && (
        <div id="settingsErr" className="msg-err">
          {err}
        </div>
      )}
    </div>
  );
};
