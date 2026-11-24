import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  BotStatus,
  DesktopPermissionItem,
  DesktopPermissionRequestResponse,
  DesktopPermissionsResponse,
  ResolvedLang,
  SettingsData,
} from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { PEBBLE_COLOR } from '../../lib/pebble';
import { BotAvatar } from '../BotAvatar';
import { DragBar } from '../DragBar';
import { StepHarness } from './StepHarness';
import { StepMagpie } from './StepMagpie';
import { StepPair } from './StepPair';
import { StepPermissions } from './StepPermissions';

type UnboxStepId = 'harness' | 'magpie' | 'permissions' | 'pair';

function mapInitialStep(initialStep: 1 | 2 | 3): UnboxStepId {
  if (initialStep === 2) return 'magpie';
  if (initialStep === 3) return 'pair';
  return 'harness';
}

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

  const [step, setStep] = useState<UnboxStepId>('harness');
  const [stepDir, setStepDir] = useState<'enter' | 'next' | 'prev'>('enter');
  const [selectedHarness, setSelectedHarness] = useState('opencode');
  const [defaultModel, setDefaultModel] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [loadingModels, setLoadingModels] = useState<Record<string, boolean>>({});
  const inflightModelsRef = useRef<Record<string, number>>({});
  const [deskPerms, setDeskPerms] = useState<DesktopPermissionItem[]>([]);
  const [deskPermsLoading, setDeskPermsLoading] = useState(false);
  const [permBusyId, setPermBusyId] = useState('');
  const permTimerRef = useRef<number | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (permTimerRef.current !== null) {
        window.clearTimeout(permTimerRef.current);
        permTimerRef.current = null;
      }
    };
  }, []);

  const loadDesktopPermissions = useCallback(async () => {
    setDeskPermsLoading(true);
    try {
      const res = await api<DesktopPermissionsResponse>('/api/desktop/permissions?target=local');
      if (mountedRef.current) {
        setDeskPerms(res.items || []);
      }
    } catch {
      if (mountedRef.current) {
        setDeskPerms([]);
      }
    } finally {
      if (mountedRef.current) {
        setDeskPermsLoading(false);
      }
    }
  }, []);

  const schedulePermRefresh = useCallback(() => {
    if (permTimerRef.current !== null) {
      window.clearTimeout(permTimerRef.current);
    }
    permTimerRef.current = window.setTimeout(() => {
      permTimerRef.current = null;
      if (mountedRef.current) {
        void loadDesktopPermissions();
      }
    }, 1500);
  }, [loadDesktopPermissions]);

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
    setStep(mapInitialStep(initialStep));
    setStepDir('enter');
    setErr('');
    setSelectedHarness(pickDefaultHarness(settings));
    setDefaultModel(settings?.defaultModel || '');
    void loadDesktopPermissions();
  }, [open, initialStep, loadDesktopPermissions]);

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
    if ((step === 'harness' || step === 'magpie') && selectedHarness) {
      void fetchModels(selectedHarness);
    }
  }, [open, step, selectedHarness, fetchModels]);

  const hasActionablePerms = useMemo(
    () => deskPerms.some((p) => p.status !== 'unsupported'),
    [deskPerms]
  );

  const steps: UnboxStepId[] = useMemo(
    () => (hasActionablePerms ? ['harness', 'magpie', 'permissions', 'pair'] : ['harness', 'magpie', 'pair']),
    [hasActionablePerms]
  );

  const safeStep: UnboxStepId = steps.includes(step) ? step : 'pair';
  const stepIdx = Math.max(0, steps.indexOf(safeStep));

  const recheck = useCallback(async () => {
    setBusy(true);
    setErr('');
    try {
      onResetModels();
      await Promise.all([onLoadSettings(true), loadDesktopPermissions()]);
      onToast(t('ubRecheckDone'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setBusy(false);
    }
  }, [loadDesktopPermissions, onLoadSettings, onResetModels, onToast, t]);

  const requestPermissions = useCallback(
    async (permissionId: string, openSettings: boolean) => {
      setPermBusyId(permissionId);
      setErr('');
      try {
        const res = await api<DesktopPermissionRequestResponse>('/api/desktop/permissions/request', {
          method: 'POST',
          body: { target: 'local', permission: permissionId, openSettings },
        });
        if (!mountedRef.current) return;
        if (res.items) setDeskPerms(res.items);
        if (res.openedSettings) onToast(t('deskPermOpened'));
        else if (res.prompted) onToast(t('deskPermPrompted'));
        schedulePermRefresh();
      } catch (e) {
        if (mountedRef.current && e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
      } finally {
        if (mountedRef.current) setPermBusyId('');
      }
    },
    [onToast, schedulePermRefresh, t]
  );

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
      if (safeStep === 'harness') {
        await persistHarness();
        await persistModelAndComplete(false);
      } else if (safeStep === 'magpie') {
        await persistModelAndComplete(false);
      }
      if (stepIdx < steps.length - 1) {
        setStepDir('next');
        setStep(steps[stepIdx + 1]);
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

  const headerMeta: Record<UnboxStepId, { title: string; sub: string }> = {
    harness: { title: t('ubStep1Title'), sub: t('ubStep1Sub') },
    magpie: { title: t('ubStep2Title'), sub: t('ubStep2Sub') },
    permissions: { title: t('ubPermTitle'), sub: t('ubPermSub') },
    pair: { title: t('ubStep3Title'), sub: t('ubStep3Sub') },
  };
  const currentMeta = headerMeta[safeStep];

  const stepShortLabel = (id: UnboxStepId) => {
    switch (id) {
      case 'harness':
        return t('ubStepHarnessShort');
      case 'magpie':
        return 'Magpie';
      case 'permissions':
        return t('ubStepPermsShort');
      case 'pair':
        return t('ubStepPairShort');
    }
  };

  return (
    <div id="unboxDlg" className="unbox-full-page open" aria-label={t('ubAria')}>
      <DragBar />
      <div className="unbox-full-scroll">
        <main className="unbox-full-main">
          <div key={safeStep} className={`unbox-step-pane is-${stepDir}`}>
            <header className="unbox-full-head">
              <div className="unbox-top">
                <div className="unbox-guide-lead">
                  <BotAvatar id="assistant" bots={[]} color={PEBBLE_COLOR} className="pebble-guide-av" busy={busy} />
                  <div>
                    <h1 id="ubTitle">{currentMeta.title}</h1>
                    <p id="ubSub">{currentMeta.sub}</p>
                  </div>
                </div>
                <button type="button" id="ubSkipTop" className="link-btn" onClick={() => void handleSkip()}>
                  {t('ubSkip')}
                </button>
              </div>
            </header>
            <section className="unbox-full-body">
              {safeStep === 'harness' && (
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
              {safeStep === 'magpie' && (
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
              {safeStep === 'permissions' && (
                <StepPermissions
                  t={t}
                  permissions={deskPerms}
                  loading={deskPermsLoading}
                  busy={busy}
                  busyPermId={permBusyId}
                  onRefresh={loadDesktopPermissions}
                  onRequestAll={() => requestPermissions('all', false)}
                  onRequestItem={(item, openSettings) => requestPermissions(item.id, openSettings)}
                />
              )}
              {safeStep === 'pair' && (
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
            {(stepIdx > 0 || Boolean(settings?.canResetDesktopMode)) && (
              <button
                type="button"
                id="ubPrev"
                className="btn"
                disabled={busy}
                onClick={() => {
                  setErr('');
                  if (stepIdx > 0) {
                    setStepDir('prev');
                    setStep(steps[stepIdx - 1]);
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
              {steps.map((id, idx) => (
                <span
                  key={id}
                  className={`unbox-progress-seg${idx === stepIdx ? ' on' : ''}${idx < stepIdx ? ' done' : ''}`}
                  data-ub-step={idx + 1}
                />
              ))}
            </div>
            <div className="unbox-progress-labels">
              {steps.map((id, idx) => (
                <span
                  key={id}
                  className={`unbox-progress-lbl${idx === stepIdx ? ' on' : ''}${idx < stepIdx ? ' done' : ''}`}
                >
                  {`${idx + 1} ${stepShortLabel(id)}`}
                </span>
              ))}
            </div>
          </div>
          <div className="unbox-full-foot-r">
            <button type="button" id="ubNext" className="btn go" disabled={busy} onClick={() => void handleNext()}>
              {stepIdx === steps.length - 1
                ? t('ubFinish')
                : safeStep === 'harness' && !anyInstalled
                  ? t('ubContinue')
                  : t('ubNext')}
            </button>
          </div>
        </div>
      </footer>
    </div>
  );
};
