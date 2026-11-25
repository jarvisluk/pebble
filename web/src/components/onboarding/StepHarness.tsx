import React, { useEffect, useRef, useState } from 'react';
import type { HarnessInfo, ResolvedLang, SettingsData, SetupTaskStatus } from '../../types';
import { api } from '../../lib/api';
import { HarnessIcon } from '../HarnessIcon';
import { ModelCombo } from '../ModelCombo';
import { StepMagpie } from './StepMagpie';

interface StepHarnessProps {
  t: (k: string, vars?: Record<string, string | number>) => string;
  lang: ResolvedLang;
  settings: SettingsData | null;
  harnesses: HarnessInfo[];
  selectedHarness: string;
  onSelectHarness: (id: string) => void;
  defaultModel: string;
  onChangeDefaultModel: (v: string) => void;
  models: Record<string, string[]>;
  loadingModels: Record<string, boolean>;
  busy: boolean;
  onBusyChange: (b: boolean) => void;
  onRecheck: () => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onFetchModels: (harnessId: string, force?: boolean) => Promise<void>;
  onLoadModels: (harness: string, force?: boolean) => Promise<string[]>;
  onLoadSettings: (fresh?: boolean) => Promise<SettingsData>;
  onResetModels: () => void;
  onToast: (msg: string) => void;
  onError: (msg: string) => void;
}

export const StepHarness: React.FC<StepHarnessProps> = ({
  t,
  lang,
  settings,
  harnesses,
  selectedHarness,
  onSelectHarness,
  defaultModel,
  onChangeDefaultModel,
  models,
  loadingModels,
  busy,
  onBusyChange,
  onRecheck,
  onSettingsUpdated,
  onFetchModels,
  onLoadModels,
  onLoadSettings,
  onResetModels,
  onToast,
  onError,
}) => {
  const [expandedInstall, setExpandedInstall] = useState<Set<string>>(() => new Set());
  const [showUninstalled, setShowUninstalled] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [advConfigOpen, setAdvConfigOpen] = useState(false);
  const [installTasks, setInstallTasks] = useState<Record<string, SetupTaskStatus>>({});
  const [loginTasks, setLoginTasks] = useState<Record<string, SetupTaskStatus>>({});
  const [copiedId, setCopiedId] = useState('');

  const setupTimersRef = useRef<Record<string, number>>({});
  const prefetchedRef = useRef<Set<string>>(new Set());
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      for (const tid of Object.values(setupTimersRef.current)) {
        window.clearTimeout(tid);
      }
      setupTimersRef.current = {};
    };
  }, []);

  // Pre-fetch model lists for all installed harnesses in the background so switching
  // harnesses and clicking the Default model picker is instant.
  useEffect(() => {
    const installed = harnesses.filter((h) => h.installed);
    for (const h of installed) {
      if (prefetchedRef.current.has(h.id) || (models[h.id] && models[h.id].length > 0)) continue;
      prefetchedRef.current.add(h.id);
      void onFetchModels(h.id);
    }
  }, [harnesses, models, onFetchModels]);

  const copyText = (text: string, tag: string) => {
    navigator.clipboard?.writeText(text).then(() => {
      setCopiedId(tag);
      setTimeout(() => setCopiedId((cur) => (cur === tag ? '' : cur)), 1200);
    });
  };

  const pollHarnessLogin = async (id: string) => {
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/login`);
      if (!mountedRef.current) return;
      setLoginTasks((prev) => ({ ...prev, [id]: st }));
      if (st.authStatus === 'ready' || st.state === 'done') {
        onResetModels();
        await onLoadSettings(true);
        onSelectHarness(id);
        onToast(t('ubLoginConfirmed'));
        return;
      }
      if (st.state === 'running') {
        setupTimersRef.current[`login:${id}`] = window.setTimeout(() => {
          void pollHarnessLogin(id);
        }, 1500);
      } else if (st.state === 'error' && st.error) {
        onError(st.error);
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
    }
  };

  const startHarnessLogin = async (id: string) => {
    onError('');
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/login`, {
        method: 'POST',
      });
      if (!mountedRef.current) return;
      setLoginTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'done' || st.authStatus === 'ready') {
        await onLoadSettings(true);
        onSelectHarness(id);
        onToast(t('ubLoginConfirmed'));
        return;
      }
      if (st.state === 'running') {
        if (setupTimersRef.current[`login:${id}`]) {
          window.clearTimeout(setupTimersRef.current[`login:${id}`]);
        }
        setupTimersRef.current[`login:${id}`] = window.setTimeout(() => {
          void pollHarnessLogin(id);
        }, 1200);
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
    }
  };

  const pollHarnessInstall = async (id: string) => {
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/install`);
      if (!mountedRef.current) return;
      setInstallTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'running') {
        setupTimersRef.current[`install:${id}`] = window.setTimeout(() => {
          void pollHarnessInstall(id);
        }, 1200);
        return;
      }
      if (st.state === 'done') {
        onResetModels();
        const fresh = await onLoadSettings(true);
        const h = fresh?.harnesses?.find((item) => item.id === id);
        onSelectHarness(id);
        onToast(`${h?.label || id} ${t('ubInstallDone')}`);
        if (h?.requiresLogin && h.authStatus === 'needs_login') {
          void startHarnessLogin(id);
        }
      } else if (st.state === 'error' && st.error) {
        setExpandedInstall((prev) => new Set(prev).add(id));
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
    }
  };

  const startHarnessInstall = async (id: string) => {
    onError('');
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/install`, {
        method: 'POST',
      });
      if (!mountedRef.current) return;
      setInstallTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'running') {
        if (setupTimersRef.current[`install:${id}`]) {
          window.clearTimeout(setupTimersRef.current[`install:${id}`]);
        }
        setupTimersRef.current[`install:${id}`] = window.setTimeout(() => {
          void pollHarnessInstall(id);
        }, 1200);
      } else if (st.state === 'done') {
        onResetModels();
        const fresh = await onLoadSettings(true);
        const h = fresh?.harnesses?.find((item) => item.id === id);
        onSelectHarness(id);
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

  const installedHarnesses = harnesses.filter((h) => h.installed);
  const uninstalledHarnesses = harnesses.filter((h) => !h.installed);
  const recommendedUninstalled = uninstalledHarnesses.filter((h) => h.category !== 'advanced');
  const advancedUninstalled = uninstalledHarnesses.filter((h) => h.category === 'advanced');
  const hInfo = harnesses.find((h) => h.id === selectedHarness);
  const hLabel = hInfo?.label || selectedHarness || 'opencode';

  const recommendedId =
    installedHarnesses.find((h) => h.authStatus !== 'needs_login')?.id ||
    installedHarnesses[0]?.id ||
    recommendedUninstalled.find((h) => h.id === 'opencode')?.id ||
    recommendedUninstalled[0]?.id ||
    '';

  const advSummary = hInfo?.magpie?.connected
    ? t('ubAdvModelSummaryMagpie', { model: hInfo.magpie.model || t('defaultTag') })
    : defaultModel.trim()
      ? t('ubAdvModelSummaryCustom', { model: defaultModel.trim() })
      : t('ubAdvModelSummaryDefault', { label: hLabel });

  const renderHarnessRow = (h: HarnessInfo) => {
    const selected = h.installed && h.id === selectedHarness;
    const isRecommended = h.id === recommendedId;
    const expanded = expandedInstall.has(h.id);
    const rawCmd = h.install?.command || '';
    const isShellCmd = rawCmd.includes(' ') && !rawCmd.startsWith('查看');
    const cmdText = isShellCmd ? rawCmd : rawCmd ? t('ubManualInstall', { name: h.label }) : '';
    const url = h.install?.url || '';

    const instTask = installTasks[h.id];
    const loginTask = loginTasks[h.id];
    const installing = instTask?.state === 'running';
    const loggingIn = loginTask?.state === 'running';
    const installErr = instTask?.state === 'error' ? instTask.error || t('ubInstallFailed') : '';
    const loginErr = loginTask?.state === 'error' ? loginTask.error || '' : '';
    const needsLogin = Boolean(h.installed && h.authStatus === 'needs_login');

    const lastLog = installing
      ? instTask?.logs?.[instTask.logs.length - 1] || t('ubInstalling')
      : loggingIn
        ? loginTask?.logs?.[loginTask.logs.length - 1] || t('ubLoggingIn')
        : '';

    const descKey = `ubDesc_${h.id}`;
    const translatedDesc = t(descKey);
    const sub = translatedDesc !== descKey ? translatedDesc : h.installed ? t('ubReady') : t('ubNotInstalled');

    const toggleInstall = () => {
      setExpandedInstall((prev) => {
        const next = new Set(prev);
        if (next.has(h.id)) next.delete(h.id);
        else next.add(h.id);
        return next;
      });
    };

    return (
      <div key={h.id} className={`unbox-item${selected ? ' on' : ''}`} data-ub-row={h.id}>
        <div className="unbox-item-hd">
          <button
            type="button"
            className="unbox-item-main unbox-item-main-row"
            onClick={() => {
              if (h.installed) {
                onSelectHarness(h.id);
                onError('');
              } else {
                toggleInstall();
              }
            }}
          >
            <HarnessIcon id={h.id} />
            <span className="unbox-item-text">
              <span className="unbox-item-name">{h.label}</span>
              <span className="unbox-item-path">{lastLog || sub}</span>
            </span>
          </button>
          <div className="unbox-item-acts">
            {h.installed ? (
              <>
                {selected ? (
                  <span className="mp go unbox-selected-pill">
                    <svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden="true">
                      <path d="M3.5 8.5l3 3 6-6.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                    {t('ubSelected')}
                  </span>
                ) : (
                  isRecommended && <span className="mp">{t('ubRecommended')}</span>
                )}
                <span className={`mp${needsLogin ? ' bad' : ' ok'}`}>
                  {needsLogin ? t('ubNeedsLogin') : t('ubReady')}
                </span>
                {needsLogin && (
                  <button
                    type="button"
                    className="mp go"
                    disabled={loggingIn}
                    onClick={() => void startHarnessLogin(h.id)}
                  >
                    {loggingIn ? t('ubLoggingIn') : t('ubConfigBtn')}
                  </button>
                )}
              </>
            ) : h.canAutoInstall ? (
              <>
                {isRecommended && <span className="mp ok">{t('ubRecommended')}</span>}
                <button
                  type="button"
                  className="mp go"
                  disabled={installing}
                  onClick={() => void startHarnessInstall(h.id)}
                >
                  {installing ? t('ubInstalling') : t('ubOneClickInstall')}
                </button>
              </>
            ) : (
              <>
                {isRecommended && <span className="mp ok">{t('ubRecommended')}</span>}
                <button type="button" className="mp go" onClick={toggleInstall}>
                  {expanded ? t('ubHideInstall') : t('ubOneClickInstall')}
                </button>
              </>
            )}
          </div>
        </div>

        {h.installed && needsLogin && (loggingIn || loginTask?.loginUrl || loginErr) && (
          <div className="unbox-install">
            {loginErr && <div className="unbox-item-err">{loginErr}</div>}
            <div className="row-inline row-wrap">
              {loginTask?.loginUrl && (
                <a href={loginTask.loginUrl} target="_blank" rel="noopener noreferrer">
                  {t('ubOpenLoginLink')}
                </a>
              )}
              <button type="button" className="mp" onClick={() => void pollHarnessLogin(h.id)}>
                {t('ubCheckLogin')}
              </button>
            </div>
          </div>
        )}

        {!h.installed && (expanded || installErr) && (
          <div className="unbox-install">
            {installErr && <div className="unbox-item-err">{installErr}</div>}
            {cmdText && (
              <div className="cmd">
                <code>{cmdText}</code>
                {isShellCmd && (
                  <button type="button" onClick={() => copyText(rawCmd, `ub-inst-${h.id}`)}>
                    {copiedId === `ub-inst-${h.id}` ? t('copied') : t('copy')}
                  </button>
                )}
              </div>
            )}
            {url && (
              <a href={url} target="_blank" rel="noopener noreferrer">
                {t('ubViewDocs')}
              </a>
            )}
          </div>
        )}
      </div>
    );
  };

  return (
    <div id="ubStep1" className="set-stack">
      {harnesses.length === 0 ? (
        <div className="set-card-hd">
          <span className="set-card-desc">{t('ubNoHarnesses')}</span>
          <button
            type="button"
            id="ubRecheck"
            className="mp"
            disabled={busy}
            onClick={() => {
              prefetchedRef.current.clear();
              onRecheck();
            }}
          >
            {t('ubRecheck')}
          </button>
        </div>
      ) : (
        <>
          <div className="unbox-group-row">
            <div className="unbox-group-hd">{t('ubInstalledGroup')}</div>
            <button
              type="button"
              id="ubRecheck"
              className="mp"
              disabled={busy}
              onClick={() => {
                prefetchedRef.current.clear();
                onRecheck();
              }}
            >
              {t('ubRecheck')}
            </button>
          </div>
          <div id="ubHarnessInstalled" className="unbox-list unbox-full-grid">
            {installedHarnesses.length === 0 ? (
              <div className="set-card-desc unbox-grid-span">{t('ubNoInstalledYet')}</div>
            ) : (
              installedHarnesses.map(renderHarnessRow)
            )}
          </div>

          {uninstalledHarnesses.length > 0 && (
            <>
              {installedHarnesses.length > 0 && (
                <button
                  type="button"
                  id="ubToggleUninstalled"
                  className="link-btn unbox-more-btn"
                  onClick={() => setShowUninstalled((v) => !v)}
                >
                  {showUninstalled
                    ? t('ubHideInstallOther')
                    : t('ubInstallOtherToggle', { n: uninstalledHarnesses.length })}
                </button>
              )}
              {(installedHarnesses.length === 0 || showUninstalled) && (
                <>
                  {installedHarnesses.length > 0 && (
                    <div className="unbox-group-hd">{t('ubAvailableGroup')}</div>
                  )}
                  <div id="ubHarnessAvailable" className="unbox-list unbox-full-grid">
                    {recommendedUninstalled.map(renderHarnessRow)}
                  </div>
                  {advancedUninstalled.length > 0 && (
                    <>
                      <button
                        type="button"
                        className="link-btn unbox-more-btn"
                        onClick={() => setShowAdvanced((v) => !v)}
                      >
                        {showAdvanced ? t('ubHideMoreEngines') : t('ubMoreEngines')}
                      </button>
                      {showAdvanced && (
                        <div className="unbox-list unbox-full-grid">{advancedUninstalled.map(renderHarnessRow)}</div>
                      )}
                    </>
                  )}
                </>
              )}
            </>
          )}

          {(installedHarnesses.length > 0 || hInfo?.installed) && (
            <div className="note unbox-fold">
              <div className="unbox-fold-hd">
                <div className="unbox-fold-info">
                  <strong>{t('ubAdvModelTitle')}</strong>
                  <span className="set-card-desc">{advSummary}</span>
                </div>
                <button
                  type="button"
                  id="ubAdvConfigToggle"
                  className="mp"
                  onClick={() => setAdvConfigOpen((v) => !v)}
                >
                  {advConfigOpen ? t('ubCollapseConfig') : t('ubExpandConfig')}
                </button>
              </div>
              {advConfigOpen && (
                <div className="unbox-fold-body">
                  <div className="fld">
                    <label>
                      {t('ubDefaultModelLbl')} <em>{t('ubDefaultModelHint')}</em>
                    </label>
                    <ModelCombo
                      id="ubStep1DefaultModel"
                      value={defaultModel}
                      onChange={onChangeDefaultModel}
                      options={models[selectedHarness] || []}
                      loading={Boolean(loadingModels[selectedHarness])}
                      placeholder={t('ubDefaultModelPh')}
                      loadingText={t('ubLoadingModels')}
                      emptyText={t('ubNoModelsFound')}
                      dropUp
                    />
                  </div>
                  <StepMagpie
                    t={t}
                    lang={lang}
                    settings={settings}
                    selectedHarness={selectedHarness}
                    hLabel={hLabel}
                    hInfo={hInfo}
                    defaultModel={defaultModel}
                    onChangeDefaultModel={onChangeDefaultModel}
                    busy={busy}
                    onBusyChange={onBusyChange}
                    onRecheck={onRecheck}
                    onSettingsUpdated={onSettingsUpdated}
                    onLoadSettings={onLoadSettings}
                    onLoadModels={onLoadModels}
                    onResetModels={onResetModels}
                    onToast={onToast}
                    onError={onError}
                  />
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
};
