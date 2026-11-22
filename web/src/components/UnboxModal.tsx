import React, { useEffect, useRef, useState } from 'react';
import type { BotStatus, ResolvedLang, SettingsData, TailcatStatus } from '../types';
import { api } from '../lib/api';
import { translate } from '../lib/i18n';

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

  const pickDefaultHarness = (s: SettingsData | null) => {
    const harnesses = s?.harnesses || [];
    const primary = bots[0];
    if (primary) {
      const match = harnesses.find((h) => h.id === primary.harness);
      if (match?.installed) return match.id;
    }
    const firstInstalled = harnesses.find((h) => h.installed);
    if (firstInstalled) return firstInstalled.id;
    return primary?.harness || harnesses[0]?.id || 'opencode';
  };

  useEffect(() => {
    if (!open) {
      if (tailcatTimerRef.current) window.clearTimeout(tailcatTimerRef.current);
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
  const anyInstalled = harnesses.some((h) => h.installed);
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
              <div id="ubHarnessList" className="unbox-list">
                {harnesses.length === 0 ? (
                  <div className="set-card-desc">{t('ubNoHarnesses')}</div>
                ) : (
                  harnesses.map((h) => {
                    const selected = h.id === selectedHarness;
                    const expanded = expandedInstall.has(h.id);
                    const rawCmd = h.install?.command || '';
                    const isShellCmd = rawCmd.includes(' ') && !rawCmd.startsWith('查看');
                    const cmdText = isShellCmd ? rawCmd : rawCmd ? t('ubManualInstall', { name: h.label }) : '';
                    const url = h.install?.url || '';
                    const sub = h.installed ? h.binaryPath || t('ubReady') : t('ubNotDetectedHint');

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
                            <span className="unbox-item-path">{sub}</span>
                          </button>
                          <div className="unbox-item-acts">
                            <span className={`mp${h.installed ? ' ok' : ''}`}>
                              {h.installed ? t('ubInstalled') : t('ubNotInstalled')}
                            </span>
                            {!h.installed && (
                              <button type="button" className="mp" onClick={toggleInstall}>
                                {expanded ? t('ubHideInstall') : t('ubViewInstall')}
                              </button>
                            )}
                          </div>
                        </div>
                        {expanded && (
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
                  })
                )}
              </div>
            </div>
          )}

          {step === 2 && (
            <div id="ubStep2" className="set-stack">
              <div id="ubMagpiePanel" className="note">
                {!mp?.installed ? (
                  <>
                    <div className="row-between row-wrap">
                      <span>{t('ubMagpieNotInstalledPlain', { label: hLabel })}</span>
                      <button type="button" id="ubRecheck2" className="mp" disabled={busy} onClick={() => void recheck()}>
                        {t('ubRecheck')}
                      </button>
                    </div>
                    <div>{t('ubMagpieInstallHint')}</div>
                    {mp?.install?.script && (
                      <div className="cmd">
                        <code>{mp.install.script}</code>
                        <button type="button" onClick={() => copyText(mp.install!.script!, 'ub-mp-script')}>
                          {copiedId === 'ub-mp-script' ? t('copied') : t('copy')}
                        </button>
                      </div>
                    )}
                    {mp?.install?.brew && (
                      <div className="cmd">
                        <code>{mp.install.brew}</code>
                        <button type="button" onClick={() => copyText(mp.install!.brew!, 'ub-mp-brew')}>
                          {copiedId === 'ub-mp-brew' ? t('copied') : t('copy')}
                        </button>
                      </div>
                    )}
                  </>
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
