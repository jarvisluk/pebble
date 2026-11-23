import React, { useEffect, useRef, useState } from 'react';
import type { BotStatus, HarnessInfo, ResolvedLang, SettingsData, SetupTaskStatus, TailcatStatus } from '../types';
import { api } from '../lib/api';
import { DOWNLOAD_LINKS } from '../lib/downloads';
import { translate } from '../lib/i18n';
import { MagpieInstallBanner } from './MagpieInstallBanner';

interface UnboxModalProps {
  open: boolean;
  initialStep: 1 | 2 | 3;
  bots: BotStatus[];
  settings: SettingsData | null;
  models: Record<string, string[]>;
  lang: ResolvedLang;
  onClose: () => void;
  onToast: (msg: string) => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onLoadSettings: (fresh?: boolean) => Promise<SettingsData>;
  onLoadModels: (harness: string, force?: boolean) => Promise<string[]>;
  onReloadBots: () => Promise<void>;
  onBotUpdated: (st: BotStatus) => void;
  onResetModels: () => void;
}

export const UnboxModal: React.FC<UnboxModalProps> = ({
  open,
  initialStep,
  bots,
  settings,
  models,
  lang,
  onClose,
  onToast,
  onSettingsUpdated,
  onLoadSettings,
  onLoadModels,
  onReloadBots,
  onBotUpdated,
  onResetModels,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [selectedHarness, setSelectedHarness] = useState('opencode');
  const [expandedInstall, setExpandedInstall] = useState<Set<string>>(() => new Set());
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [installTasks, setInstallTasks] = useState<Record<string, SetupTaskStatus>>({});
  const [loginTasks, setLoginTasks] = useState<Record<string, SetupTaskStatus>>({});

  const [defaultModel, setDefaultModel] = useState('');
  const [provDismissed, setProvDismissed] = useState(false);
  const [addProvOpen, setAddProvOpen] = useState(false);

  // Add provider form
  const [apKind, setApKind] = useState<'preset' | 'custom'>('preset');
  const [apPreset, setApPreset] = useState('');
  const [apProtocol, setApProtocol] = useState('openai');
  const [apName, setApName] = useState('');
  const [apKey, setApKey] = useState('');
  const [apUrl, setApUrl] = useState('');
  const [apModels, setApModels] = useState('');
  const [apSubmitting, setApSubmitting] = useState(false);

  const [tailcat, setTailcat] = useState<TailcatStatus | null>(null);
  const [qrBust, setQrBust] = useState(() => Date.now());
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [copiedId, setCopiedId] = useState('');

  const tailcatTimerRef = useRef<number | null>(null);
  const setupTimersRef = useRef<Record<string, number>>({});

  const clearSetupTimers = () => {
    for (const tid of Object.values(setupTimersRef.current)) {
      window.clearTimeout(tid);
    }
    setupTimersRef.current = {};
  };

  const pickDefaultHarness = (s: SettingsData | null) => {
    const harnesses = s?.harnesses || [];
    const primary = bots[0];
    if (primary) {
      const match = harnesses.find((h) => h.id === primary.harness);
      if (match?.installed && match.authStatus !== 'needs_login') return match.id;
    }
    const readyInstalled = harnesses.find((h) => h.installed && h.authStatus !== 'needs_login');
    if (readyInstalled) return readyInstalled.id;
    const firstInstalled = harnesses.find((h) => h.installed);
    if (firstInstalled) return firstInstalled.id;
    return primary?.harness || harnesses[0]?.id || 'opencode';
  };

  useEffect(() => {
    if (!open) {
      if (tailcatTimerRef.current) window.clearTimeout(tailcatTimerRef.current);
      clearSetupTimers();
      return;
    }
    setStep(initialStep);
    setErr('');
    setProvDismissed(false);
    setAddProvOpen(false);
    setSelectedHarness(pickDefaultHarness(settings));
    setDefaultModel(settings?.defaultModel || '');
    const presets = settings?.magpie?.presets || [];
    if (presets[0] && !apPreset) setApPreset(presets[0].id);
  }, [open, initialStep]);

  const refreshTailcat = async () => {
    try {
      const tr = await api<TailcatStatus>('/api/tailcat');
      setTailcat(tr);
      if (tr.state === 'ready') setQrBust(Date.now());
      if (tr.error) setErr(tr.error);
      if (tailcatTimerRef.current) window.clearTimeout(tailcatTimerRef.current);
      if (tr.state === 'connecting' && open) {
        tailcatTimerRef.current = window.setTimeout(() => {
          void refreshTailcat();
        }, 800);
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  useEffect(() => {
    if (!open) return;
    if (step === 2) {
      void onLoadModels(selectedHarness);
      const presets = settings?.magpie?.presets || [];
      if (presets[0] && (!apPreset || !presets.some((p) => p.id === apPreset))) {
        setApPreset(presets[0].id);
      }
    } else if (step === 3) {
      void refreshTailcat();
    }
    return () => {
      if (tailcatTimerRef.current) window.clearTimeout(tailcatTimerRef.current);
    };
  }, [open, step, selectedHarness]);

  const copyText = (text: string, tag: string) => {
    navigator.clipboard?.writeText(text).then(() => {
      setCopiedId(tag);
      setTimeout(() => setCopiedId((cur) => (cur === tag ? '' : cur)), 1200);
    });
  };

  const recheck = async () => {
    setBusy(true);
    setErr('');
    try {
      onResetModels();
      await onLoadSettings(true);
      onToast(t('ubRecheckDone'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  const pollHarnessLogin = async (id: string) => {
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/login`);
      setLoginTasks((prev) => ({ ...prev, [id]: st }));
      if (st.authStatus === 'ready' || st.state === 'done') {
        onResetModels();
        await onLoadSettings(true);
        setSelectedHarness(id);
        onToast(t('ubLoginConfirmed'));
        return;
      }
      if (st.state === 'running' && open) {
        setupTimersRef.current[`login:${id}`] = window.setTimeout(() => {
          void pollHarnessLogin(id);
        }, 1500);
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
      setLoginTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'done' || st.authStatus === 'ready') {
        await onLoadSettings(true);
        setSelectedHarness(id);
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
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  const pollHarnessInstall = async (id: string) => {
    try {
      const st = await api<SetupTaskStatus>(`/api/harnesses/${encodeURIComponent(id)}/install`);
      setInstallTasks((prev) => ({ ...prev, [id]: st }));
      if (st.state === 'running' && open) {
        setupTimersRef.current[`install:${id}`] = window.setTimeout(() => {
          void pollHarnessInstall(id);
        }, 1200);
        return;
      }
      if (st.state === 'done') {
        onResetModels();
        const fresh = await onLoadSettings(true);
        const h = fresh?.harnesses?.find((item) => item.id === id);
        setSelectedHarness(id);
        onToast(`${h?.label || id} ${t('ubInstallDone')}`);
        if (h?.requiresLogin && h.authStatus === 'needs_login') {
          void startHarnessLogin(id);
        }
      } else if (st.state === 'error' && st.error) {
        setErr(st.error);
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
        setSelectedHarness(id);
        if (h?.requiresLogin && h.authStatus === 'needs_login') {
          void startHarnessLogin(id);
        }
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  const persistHarness = async () => {
    const primary = bots[0];
    if (!primary || !selectedHarness || primary.harness === selectedHarness) return;
    const st = await api<BotStatus>(`/api/bots/${encodeURIComponent(primary.id)}`, {
      method: 'PATCH',
      body: { harness: selectedHarness },
    });
    onBotUpdated(st);
    await onLoadSettings();
  };

  const persistModelAndComplete = async (markCompleted: boolean) => {
    const dm = defaultModel.trim();
    const prevDm = settings?.defaultModel || '';
    const body: Record<string, unknown> = {};
    if (dm !== prevDm) body.defaultModel = dm;
    if (markCompleted) body.onboardingCompleted = true;
    if (Object.keys(body).length) {
      const updated = await api<SettingsData>('/api/settings', { method: 'PUT', body });
      onSettingsUpdated(updated);
    }
  };

  const handleSkip = async () => {
    if (tailcatTimerRef.current) window.clearTimeout(tailcatTimerRef.current);
    clearSetupTimers();
    if (settings && !settings.onboarding?.completed) {
      try {
        const updated = await api<SettingsData>('/api/settings', {
          method: 'PUT',
          body: { onboardingCompleted: true },
        });
        onSettingsUpdated(updated);
      } catch (e) {
        if (e instanceof Error && e.message === 'unauthorized') return;
      }
    }
    onClose();
  };

  const handleNext = async () => {
    setErr('');
    setBusy(true);
    try {
      if (step === 1) {
        await persistHarness();
        setStep(2);
        return;
      }
      if (step === 2) {
        await persistModelAndComplete(false);
        setStep(3);
        return;
      }
      await persistHarness();
      await persistModelAndComplete(true);
      if (tailcatTimerRef.current) window.clearTimeout(tailcatTimerRef.current);
      clearSetupTimers();
      onClose();
      await onReloadBots();
      onToast(t('ubFinishToast'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  const harnesses = settings?.harnesses || [];
  const installedHarnesses = harnesses.filter((h) => h.installed);
  const uninstalledHarnesses = harnesses.filter((h) => !h.installed);
  const recommendedUninstalled = uninstalledHarnesses.filter((h) => h.category !== 'advanced');
  const advancedUninstalled = uninstalledHarnesses.filter((h) => h.category === 'advanced');

  const anyInstalled = installedHarnesses.length > 0;
  const hInfo = harnesses.find((h) => h.id === selectedHarness);
  const hLabel = hInfo?.label || selectedHarness || 'opencode';
  const mp = settings?.magpie;
  const provs = mp?.providers || [];
  const presets = mp?.presets || [];
  const showAddProv = Boolean(mp?.installed) && (addProvOpen || (provs.length === 0 && !provDismissed));

  const headerMeta = {
    1: { title: t('ubStep1Title'), sub: t('ubStep1Sub') },
    2: { title: t('ubStep2Title'), sub: t('ubStep2Sub') },
    3: { title: t('ubStep3Title'), sub: t('ubStep3Sub') },
  }[step];

  const renderHarnessRow = (h: HarnessInfo) => {
    const selected = h.id === selectedHarness;
    const expanded = expandedInstall.has(h.id);
    const rawCmd = h.install?.command || '';
    const isShellCmd = rawCmd.includes(' ') && !rawCmd.startsWith('查看');
    const cmdText = isShellCmd ? rawCmd : rawCmd ? t('ubManualInstall', { name: h.label }) : '';
    const url = h.install?.url || '';

    const instTask = installTasks[h.id];
    const loginTask = loginTasks[h.id];
    const installing = instTask?.state === 'running';
    const loggingIn = loginTask?.state === 'running';
    const needsLogin = Boolean(h.installed && h.authStatus === 'needs_login');

    const lastLog = installing
      ? instTask?.logs?.[instTask.logs.length - 1]
      : loggingIn
        ? loginTask?.logs?.[loginTask.logs.length - 1]
        : '';

    const sub = h.installed
      ? [h.summary, h.binaryPath || t('ubReady')].filter(Boolean).join(' · ')
      : h.summary || t('ubNotDetectedHint');

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
            className="unbox-item-main"
            onClick={() => {
              if (h.installed) {
                setSelectedHarness(h.id);
                setErr('');
              } else {
                toggleInstall();
              }
            }}
          >
            <span className="unbox-item-name">{h.label}</span>
            <span className="unbox-item-path">{lastLog || sub}</span>
          </button>
          <div className="unbox-item-acts">
            {h.installed ? (
              <>
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
                    {loggingIn ? t('ubLoggingIn') : t('ubLoginBtn')}
                  </button>
                )}
              </>
            ) : (
              <>
                {h.canAutoInstall && (
                  <button
                    type="button"
                    className="mp go"
                    disabled={installing}
                    onClick={() => void startHarnessInstall(h.id)}
                  >
                    {installing ? t('ubInstalling') : t('ubOneClickInstall')}
                  </button>
                )}
                <button type="button" className="mp" onClick={toggleInstall}>
                  {expanded ? t('ubHideInstall') : t('ubViewInstall')}
                </button>
              </>
            )}
          </div>
        </div>

        {h.installed && needsLogin && (loggingIn || loginTask?.loginUrl) && (
          <div className="unbox-install">
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

        {!h.installed && expanded && (
          <div className="unbox-install">
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
    <div id="unboxDlg" className={`dlg-wrap${open ? ' open' : ''}`} aria-label={t('ubAria')}>
      <div className="dlg unbox">
        <div className="dlg-h dlg-h-col">
          <div className="unbox-top">
            <div>
              <h3 id="ubTitle">{headerMeta.title}</h3>
              <p id="ubSub">{headerMeta.sub}</p>
            </div>
            <button type="button" id="ubSkipTop" className="link-btn" onClick={() => void handleSkip()}>
              {t('ubSkip')}
            </button>
          </div>
          <div className="unbox-steps" aria-label={t('ubStepsAria')}>
            {([1, 2, 3] as const).map((n) => (
              <div
                key={n}
                className={`unbox-step${n === step ? ' on' : ''}${n < step ? ' done' : ''}`}
                data-ub-step={n}
              >
                {t(`ubStep${n}Label`)}
              </div>
            ))}
          </div>
        </div>

        <div className="dlg-b">
          {step === 1 && (
            <div id="ubStep1" className="set-stack">
              <div className="set-card-hd">
                <span className="set-card-desc">{t('ubStep1Hint')}</span>
                <button type="button" id="ubRecheck" className="mp" disabled={busy} onClick={() => void recheck()}>
                  {t('ubRecheck')}
                </button>
              </div>

              {harnesses.length === 0 ? (
                <div className="set-card-desc">{t('ubNoHarnesses')}</div>
              ) : (
                <>
                  <div className="unbox-group-hd">{t('ubInstalledGroup')}</div>
                  <div id="ubHarnessInstalled" className="unbox-list">
                    {installedHarnesses.length === 0 ? (
                      <div className="set-card-desc">{t('ubNoInstalledYet')}</div>
                    ) : (
                      installedHarnesses.map(renderHarnessRow)
                    )}
                  </div>

                  {uninstalledHarnesses.length > 0 && (
                    <>
                      <div className="unbox-group-hd">{t('ubAvailableGroup')}</div>
                      <div id="ubHarnessAvailable" className="unbox-list">
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
                          {showAdvanced && <div className="unbox-list">{advancedUninstalled.map(renderHarnessRow)}</div>}
                        </>
                      )}
                    </>
                  )}
                </>
              )}
            </div>
          )}

          {step === 2 && (
            <div id="ubStep2" className="set-stack">
              <div className="set-card-desc">{t('ubMagpieOptionalHint')}</div>
              <div id="ubMagpiePanel" className="note">
                {!mp?.installed ? (
                  <MagpieInstallBanner
                    lang={lang}
                    hint={t('ubMagpieNotInstalledPlain', { label: hLabel })}
                    installCmds={mp?.install}
                    onToast={onToast}
                    onInstalled={async () => {
                      onResetModels();
                      await onLoadSettings(true);
                      await onLoadModels(selectedHarness, true);
                    }}
                  />
                ) : (
                  <>
                    <div className="row-between row-wrap">
                      <div className="row-inline row-wrap">
                        <strong>Magpie</strong>
                        <span className={`mp ${mp.healthy ? 'ok' : 'bad'}`}>
                          {mp.healthy ? t('ubGatewayRunning') : t('ubGatewayNotStarted')}
                        </span>
                        <span className="mp">{t('ubProviderCount', { n: provs.length })}</span>
                      </div>
                      <div className="row-inline-sm">
                        <button type="button" id="ubRecheck2" className="mp" disabled={busy} onClick={() => void recheck()}>
                          {t('ubRecheck')}
                        </button>
                        {(provs.length > 0 || provDismissed) && (
                          <button
                            type="button"
                            id="ubShowAddProv"
                            className="mp go"
                            onClick={() => {
                              setProvDismissed(false);
                              setAddProvOpen((v) => !v);
                            }}
                          >
                            {t('addProviderBtn')}
                          </button>
                        )}
                      </div>
                    </div>
                    <div>{provs.length ? t('ubHasProvidersDesc') : t('ubNoProvidersDesc')}</div>
                  </>
                )}
              </div>

              {showAddProv && (
                <div id="ubAddProvBox" className="add-prov">
                  <div className="two">
                    <div className="fld">
                      <label>{t('addMethod')}</label>
                      <select
                        id="ubApKind"
                        value={apKind}
                        onChange={(e) => setApKind(e.target.value as 'preset' | 'custom')}
                      >
                        <option value="preset">{t('presetVendor')}</option>
                        <option value="custom">{t('customEndpoint')}</option>
                      </select>
                    </div>
                    {apKind === 'preset' ? (
                      <div className="fld" id="ubApPresetFld">
                        <label>{t('presetVendor')}</label>
                        <select id="ubApPreset" value={apPreset} onChange={(e) => setApPreset(e.target.value)}>
                          {presets.map((p) => (
                            <option key={p.id} value={p.id}>
                              {p.name} ({p.id})
                            </option>
                          ))}
                        </select>
                      </div>
                    ) : (
                      <div className="fld" id="ubApProtoFld">
                        <label>{t('apiProtocol')}</label>
                        <select id="ubApProtocol" value={apProtocol} onChange={(e) => setApProtocol(e.target.value)}>
                          <option value="openai">{t('protoOpenAI')}</option>
                          <option value="anthropic">{t('protoAnthropic')}</option>
                          <option value="responses">OpenAI Responses</option>
                          <option value="gemini">Google Gemini</option>
                        </select>
                      </div>
                    )}
                  </div>
                  <div className="two">
                    <div className="fld">
                      {apKind === 'custom' ? (
                        <label>{t('aliasRequired')}</label>
                      ) : (
                        <label dangerouslySetInnerHTML={{ __html: t('aliasOptional') }} />
                      )}
                      <input
                        id="ubApName"
                        placeholder={t('aliasPh')}
                        value={apName}
                        onChange={(e) => setApName(e.target.value)}
                      />
                    </div>
                    <div className="fld">
                      <label dangerouslySetInnerHTML={{ __html: t('apiKeyLabel') }} />
                      <input
                        id="ubApKey"
                        type="password"
                        className="mono"
                        placeholder="sk-..."
                        autoComplete="new-password"
                        value={apKey}
                        onChange={(e) => setApKey(e.target.value)}
                      />
                    </div>
                  </div>
                  {apKind === 'custom' && (
                    <div id="ubApCustomRow" className="two">
                      <div className="fld">
                        <label>Base URL</label>
                        <input
                          id="ubApUrl"
                          className="mono"
                          placeholder="https://api.example.com/v1"
                          value={apUrl}
                          onChange={(e) => setApUrl(e.target.value)}
                        />
                      </div>
                      <div className="fld">
                        <label dangerouslySetInnerHTML={{ __html: t('modelsListOptional') }} />
                        <input
                          id="ubApModels"
                          className="mono"
                          placeholder="model-a,model-b"
                          value={apModels}
                          onChange={(e) => setApModels(e.target.value)}
                        />
                      </div>
                    </div>
                  )}
                  <div className="row-end-sm">
                    <button
                      type="button"
                      id="ubApCancel"
                      className="mp"
                      onClick={() => {
                        setProvDismissed(true);
                        setAddProvOpen(false);
                      }}
                    >
                      {t('cancel')}
                    </button>
                    <button
                      type="button"
                      id="ubApSubmit"
                      className="mp go"
                      disabled={apSubmitting}
                      onClick={async () => {
                        const custom = apKind === 'custom';
                        setApSubmitting(true);
                        setErr('');
                        try {
                          await api('/api/magpie/providers', {
                            method: 'POST',
                            body: {
                              action: 'add',
                              preset: custom ? '' : apPreset,
                              name: apName.trim(),
                              protocol: custom ? apProtocol : '',
                              url: custom ? apUrl.trim() : '',
                              key: apKey.trim(),
                              models: custom ? apModels.trim() : '',
                            },
                          });
                          setApKey('');
                          setApName('');
                          setApUrl('');
                          setApModels('');
                          setProvDismissed(false);
                          setAddProvOpen(false);
                          onResetModels();
                          await onLoadSettings(true);
                          await onLoadModels(selectedHarness, true);
                          onToast(t('providerAdded'));
                        } catch (e) {
                          if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
                        } finally {
                          setApSubmitting(false);
                        }
                      }}
                    >
                      {t('confirmAdd')}
                    </button>
                  </div>
                </div>
              )}

              {mp?.installed && (hInfo?.magpie?.connected || provs.length > 0) && (
                <div id="ubConnectBox" className="note">
                  <div className="row-between row-wrap">
                    <span id="ubConnectText">
                      {hInfo?.magpie?.connected
                        ? t('ubConnectedMagpie', { label: hLabel, model: hInfo.magpie.model || t('defaultTag') })
                        : t('ubNativeCliUsing', { label: hLabel })}
                    </span>
                    {!hInfo?.magpie?.connected && (
                      <button
                        type="button"
                        id="ubConnectMagpie"
                        className="mp go"
                        disabled={busy}
                        onClick={async () => {
                          setErr('');
                          setBusy(true);
                          try {
                            const list = await onLoadModels(selectedHarness);
                            const model = defaultModel.trim() || list[0] || '';
                            if (!model) {
                              setErr(t('ubPickModelFirst'));
                              return;
                            }
                            const updated = await api<SettingsData>(
                              `/api/harnesses/${encodeURIComponent(selectedHarness)}/magpie`,
                              { method: 'POST', body: { action: 'connect', model } }
                            );
                            onSettingsUpdated(updated);
                            if (!defaultModel.trim()) setDefaultModel(model);
                            onResetModels();
                            onToast(t('ubConnectedToast', { label: hLabel }));
                          } catch (e) {
                            if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
                          } finally {
                            setBusy(false);
                          }
                        }}
                      >
                        {t('ubConnectMagpieBtn')}
                      </button>
                    )}
                  </div>
                </div>
              )}

              <div className="fld">
                <label>
                  {t('ubDefaultModelLbl')} <em>{t('ubDefaultModelHint')}</em>
                </label>
                <input
                  id="ubDefaultModel"
                  className="mono"
                  spellCheck={false}
                  list="ubDefaultModelList"
                  placeholder={t('ubDefaultModelPh')}
                  value={defaultModel}
                  onChange={(e) => setDefaultModel(e.target.value)}
                />
                <datalist id="ubDefaultModelList">
                  {(models[selectedHarness] || []).map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
              </div>
            </div>
          )}

          {step === 3 && (
            <div id="ubStep3" className="set-stack">
              <p className="set-card-desc">{t('ubStep3Hint')}</p>
              <div className="note">
                <div className="row-between">
                  <span>
                    <span>{t('ubTailcatLabel')}</span>
                    <strong id="ubRemoteState">
                      {tailcat
                        ? {
                            disabled: t('disabled'),
                            connecting: t('remoteConnecting'),
                            ready: t('remoteReady'),
                            error: t('remoteError'),
                          }[tailcat.state] || tailcat.state
                        : t('disabled')}
                    </strong>
                  </span>
                  {tailcat?.state !== 'ready' && tailcat?.state !== 'connecting' && (
                    <button
                      type="button"
                      id="ubRemoteEnable"
                      className="mp go"
                      disabled={busy}
                      onClick={async () => {
                        setErr('');
                        setBusy(true);
                        try {
                          const tr = await api<TailcatStatus>('/api/tailcat', {
                            method: 'POST',
                            body: { enabled: true },
                          });
                          setTailcat(tr);
                          if (tr.state === 'ready') setQrBust(Date.now());
                          if (tr.state === 'connecting') void refreshTailcat();
                          onToast(t('remoteEnabledToast'));
                        } catch (e) {
                          if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
                        } finally {
                          setBusy(false);
                        }
                      }}
                    >
                      {tailcat?.state === 'error' ? t('remoteRetry') : t('enableRemote')}
                    </button>
                  )}
                </div>
                {tailcat?.state === 'ready' && (
                  <>
                    <img id="ubRemoteQR" className="unbox-qr" alt="" src={`/api/tailcat/qr.png?t=${qrBust}`} />
                    <div id="ubRemoteLinkFld" className="fld">
                      <textarea
                        id="ubRemoteLink"
                        className="mono ta-sm"
                        readOnly
                        value={tailcat.link || ''}
                      />
                    </div>
                    <div id="ubRemoteCopyWrap" className="row-end">
                      <button
                        type="button"
                        id="ubRemoteCopy"
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
              </div>
              <div className="note">
                <div className="row-between row-wrap">
                  <span className="set-card-desc">{t('ubGetMobileApp')}</span>
                  <div className="row-inline row-wrap">
                    <a className="mp" href={DOWNLOAD_LINKS.iosAppStore} target="_blank" rel="noopener noreferrer">
                      {t('ubAppStore')}
                    </a>
                    <a className="mp" href={DOWNLOAD_LINKS.androidPlayStore} target="_blank" rel="noopener noreferrer">
                      {t('ubGooglePlay')}
                    </a>
                  </div>
                </div>
              </div>
            </div>
          )}

          <div id="ubErr" className="msg-err">
            {err}
          </div>
        </div>

        <div className="dlg-f unbox-foot">
          {step > 1 && (
            <button
              type="button"
              id="ubPrev"
              className="btn"
              onClick={() => {
                setErr('');
                setStep((s) => (s > 1 ? ((s - 1) as 1 | 2 | 3) : s));
              }}
            >
              {t('ubPrev')}
            </button>
          )}
          <span id="ubFootSpacer" />
          <div className="unbox-foot-r">
            <button type="button" id="ubNext" className="btn go" disabled={busy} onClick={() => void handleNext()}>
              {step === 3 ? t('ubFinish') : step === 1 && !anyInstalled ? t('ubContinue') : t('ubNext')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
