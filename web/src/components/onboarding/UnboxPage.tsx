import React, { useCallback, useEffect, useRef, useState } from 'react';
import type { BotStatus, ResolvedLang, SettingsData } from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { PEBBLE_COLOR } from '../../lib/pebble';
import { BotAvatar } from '../BotAvatar';
import { DragBar } from '../DragBar';
import { StepHarness } from './StepHarness';
import { StepMagpie } from './StepMagpie';
import { StepPair } from './StepPair';

export interface UnboxPageProps {
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

export const UnboxPage: React.FC<UnboxPageProps> = ({
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
  const t = useCallback((k: string, vars?: Record<string, string | number>) => translate(lang, k, vars), [lang]);

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [stepDir, setStepDir] = useState<'enter' | 'next' | 'prev'>('enter');
  const [selectedHarness, setSelectedHarness] = useState('opencode');
  const [defaultModel, setDefaultModel] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [loadingModels, setLoadingModels] = useState<Record<string, boolean>>({});
  const inflightModelsRef = useRef<Record<string, number>>({});

  const pickDefaultHarness = useCallback(
    (s: SettingsData | null) => {
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
    },
    [bots]
  );

  useEffect(() => {
    if (!open) return;
    setStep(initialStep);
    setStepDir('enter');
    setErr('');
    setSelectedHarness(pickDefaultHarness(settings));
    setDefaultModel(settings?.defaultModel || '');
  }, [open, initialStep]);

  const fetchModels = useCallback(
    async (harnessId: string, force = false) => {
      if (!harnessId) return;
      if (!force && (inflightModelsRef.current[harnessId] || 0) > 0) return;
      inflightModelsRef.current[harnessId] = (inflightModelsRef.current[harnessId] || 0) + 1;
      setLoadingModels((prev) => ({ ...prev, [harnessId]: true }));
      try {
        await onLoadModels(harnessId, force);
      } finally {
        const next = Math.max(0, (inflightModelsRef.current[harnessId] || 1) - 1);
        inflightModelsRef.current[harnessId] = next;
        if (next === 0) {
          setLoadingModels((prev) => ({ ...prev, [harnessId]: false }));
        }
      }
    },
    [onLoadModels]
  );

  useEffect(() => {
    if (!open) return;
    if ((step === 1 || step === 2) && selectedHarness) {
      void fetchModels(selectedHarness);
    }
  }, [open, step, selectedHarness, fetchModels]);

  const recheck = useCallback(async () => {
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
  }, [onLoadSettings, onResetModels, onToast, t]);

  const persistHarness = async () => {
    const primary = bots[0];
    const target = (settings?.harnesses || []).find((h) => h.id === selectedHarness);
    if (!primary || !selectedHarness || !target?.installed || primary.harness === selectedHarness) return;
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
    const prevDh = settings?.defaultHarness || '';
    const target = (settings?.harnesses || []).find((h) => h.id === selectedHarness);
    const body: Record<string, unknown> = {};
    if (target?.installed && selectedHarness !== prevDh) body.defaultHarness = selectedHarness;
    if (dm !== prevDm) body.defaultModel = dm;
    if (markCompleted) body.onboardingCompleted = true;
    if (Object.keys(body).length) {
      const updated = await api<SettingsData>('/api/settings', { method: 'PUT', body });
      onSettingsUpdated(updated);
    }
  };

  const handleSkip = async () => {
    try {
      await persistHarness();
      if (settings && !settings.onboarding?.completed) {
        await persistModelAndComplete(true);
      } else {
        await persistModelAndComplete(false);
      }
    } catch (e) {
      if (e instanceof Error && e.message === 'unauthorized') return;
    }
    onClose();
  };

  const handleNext = async () => {
    setErr('');
    setBusy(true);
    try {
      if (step === 1) {
        await persistHarness();
        await persistModelAndComplete(false);
        setStepDir('next');
        setStep(2);
        return;
      }
      if (step === 2) {
        await persistModelAndComplete(false);
        setStepDir('next');
        setStep(3);
        return;
      }
      await persistHarness();
      await persistModelAndComplete(true);
      onClose();
      await onReloadBots();
      onToast(t('ubFinishToast'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  if (!open) return null;

  const harnesses = settings?.harnesses || [];
  const anyInstalled = harnesses.some((h) => h.installed);
  const hInfo = harnesses.find((h) => h.id === selectedHarness);
  const hLabel = hInfo?.label || selectedHarness || 'opencode';

  const headerMeta = {
    1: { title: t('ubStep1Title'), sub: t('ubStep1Sub') },
    2: { title: t('ubStep2Title'), sub: t('ubStep2Sub') },
    3: { title: t('ubStep3Title'), sub: t('ubStep3Sub') },
  }[step];

  return (
    <div id="unboxDlg" className="unbox-full-page open" aria-label={t('ubAria')}>
      <DragBar />
      <div className="unbox-full-scroll">
        <main className="unbox-full-main">
          <div key={step} className={`unbox-step-pane is-${stepDir}`}>
            <header className="unbox-full-head">
              <div className="unbox-top">
                <div className="unbox-guide-lead">
                  <BotAvatar id="assistant" bots={[]} color={PEBBLE_COLOR} className="pebble-guide-av" busy={busy} />
                  <div>
                    <h1 id="ubTitle">{headerMeta.title}</h1>
                    <p id="ubSub">{headerMeta.sub}</p>
                  </div>
                </div>
                <button type="button" id="ubSkipTop" className="link-btn" onClick={() => void handleSkip()}>
                  {t('ubSkip')}
                </button>
              </div>
            </header>
            <section className="unbox-full-body">
              {step === 1 && (
                <StepHarness
                  t={t}
                  harnesses={harnesses}
                  selectedHarness={selectedHarness}
                  onSelectHarness={setSelectedHarness}
                  defaultModel={defaultModel}
                  onChangeDefaultModel={setDefaultModel}
                  models={models}
                  loadingModels={loadingModels}
                  busy={busy}
                  onRecheck={() => void recheck()}
                  onFetchModels={fetchModels}
                  onLoadSettings={onLoadSettings}
                  onResetModels={onResetModels}
                  onToast={onToast}
                  onError={setErr}
                />
              )}
              {step === 2 && (
                <StepMagpie
                  t={t}
                  lang={lang}
                  settings={settings}
                  selectedHarness={selectedHarness}
                  hLabel={hLabel}
                  hInfo={hInfo}
                  defaultModel={defaultModel}
                  onChangeDefaultModel={setDefaultModel}
                  busy={busy}
                  onBusyChange={setBusy}
                  onRecheck={() => void recheck()}
                  onSettingsUpdated={onSettingsUpdated}
                  onLoadSettings={onLoadSettings}
                  onLoadModels={onLoadModels}
                  onResetModels={onResetModels}
                  onToast={onToast}
                  onError={setErr}
                />
              )}
              {step === 3 && (
                <StepPair
                  t={t}
                  busy={busy}
                  onBusyChange={setBusy}
                  onToast={onToast}
                  onError={setErr}
                />
              )}
              <div id="ubErr" className="msg-err">
                {err}
              </div>
            </section>
          </div>
        </main>
      </div>
      <footer className="unbox-full-foot-wrap">
        <div className="unbox-full-foot">
          <div className="unbox-full-foot-l">
            {(step > 1 || Boolean(settings?.canResetDesktopMode)) && (
              <button
                type="button"
                id="ubPrev"
                className="btn"
                disabled={busy}
                onClick={() => {
                  setErr('');
                  if (step > 1) {
                    setStepDir('prev');
                    setStep((s) => (s > 1 ? ((s - 1) as 1 | 2 | 3) : s));
                  } else if (settings?.canResetDesktopMode) {
                    setBusy(true);
                    void api('/api/desktop/reset', { method: 'POST' }).catch((e) => {
                      setBusy(false);
                      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
                    });
                  }
                }}
              >
                {t('ubPrev')}
              </button>
            )}
          </div>
          <div className="unbox-progress" aria-label={t('ubStepsAria')}>
            <div className="unbox-progress-bars">
              {([1, 2, 3] as const).map((n) => (
                <span
                  key={n}
                  className={`unbox-progress-seg${n === step ? ' on' : ''}${n < step ? ' done' : ''}`}
                  data-ub-step={n}
                />
              ))}
            </div>
            <div className="unbox-progress-labels">
              {([1, 2, 3] as const).map((n) => (
                <span
                  key={n}
                  className={`unbox-progress-lbl${n === step ? ' on' : ''}${n < step ? ' done' : ''}`}
                >
                  {t(`ubStep${n}Label`)}
                </span>
              ))}
            </div>
          </div>
          <div className="unbox-full-foot-r">
            <button type="button" id="ubNext" className="btn go" disabled={busy} onClick={() => void handleNext()}>
              {step === 3 ? t('ubFinish') : step === 1 && !anyInstalled ? t('ubContinue') : t('ubNext')}
            </button>
          </div>
        </div>
      </footer>
    </div>
  );
};
