import React, { useCallback, useEffect, useState } from 'react';
import type {
  DesktopPermissionItem,
  DesktopPermissionRequestResponse,
  DesktopPermissionsResponse,
  DesktopTarget,
  ResolvedLang,
  SettingsData,
} from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';

interface DesktopTabProps {
  active: boolean;
  settings: SettingsData | null;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onLoadModels: (harness: string, force?: boolean) => Promise<string[]>;
  onPreloadAllModels: (list: SettingsData['harnesses'] | undefined, force?: boolean) => void;
  onOpenDesktopTarget: (target: string) => void;
  onDirtyChange: (dirty: boolean) => void;
  saveRef: React.MutableRefObject<(() => Promise<boolean>) | null>;
  resetRef: React.MutableRefObject<(() => void) | null>;
}

export const DesktopTab: React.FC<DesktopTabProps> = ({
  active,
  settings,
  lang,
  onToast,
  onSettingsUpdated,
  onLoadModels,
  onPreloadAllModels,
  onOpenDesktopTarget,
  onDirtyChange,
  saveRef,
  resetRef,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const permText = (key: string, fallback: string) => {
    const val = t(key);
    return val === key ? fallback : val;
  };

  const [permTarget, setPermTarget] = useState('local');
  const [permTargets, setPermTargets] = useState<DesktopTarget[]>([]);
  const [deskPerms, setDeskPerms] = useState<DesktopPermissionItem[]>([]);
  const [deskPermsLoading, setDeskPermsLoading] = useState(false);
  const [permBusyId, setPermBusyId] = useState('');
  const [deskPermErr, setDeskPermErr] = useState('');

  const [localDeskEngine, setLocalDeskEngine] = useState('');
  const [localDeskSupported, setLocalDeskSupported] = useState(false);

  const baseHarness = settings?.desktopHarness || '';
  const baseModel = (settings?.desktopModel || '').trim();
  const [deskHarness, setDeskHarness] = useState(baseHarness);
  const [deskModel, setDeskModel] = useState(baseModel);
  const [deskTouched, setDeskTouched] = useState(false);
  const [deskSaving, setDeskSaving] = useState(false);
  const [deskErr, setDeskErr] = useState('');

  const isDirty = Boolean(
    settings &&
      deskTouched &&
      (deskHarness !== baseHarness || deskModel.trim() !== baseModel)
  );

  useEffect(() => {
    if (!deskTouched) {
      setDeskHarness(baseHarness);
      setDeskModel(baseModel);
    }
  }, [baseHarness, baseModel, deskTouched]);

  useEffect(() => {
    onDirtyChange(isDirty);
  }, [isDirty, onDirtyChange]);

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const resetWorker = useCallback(() => {
    setDeskTouched(false);
    setDeskHarness(baseHarness);
    setDeskModel(baseModel);
    setDeskErr('');
  }, [baseHarness, baseModel]);

  const saveWorker = useCallback(async (): Promise<boolean> => {
    setDeskSaving(true);
    setDeskErr('');
    try {
      const next = await api<SettingsData>('/api/settings', {
        method: 'PUT',
        body: { desktopHarness: deskHarness, desktopModel: deskModel.trim() },
      });
      onSettingsUpdated(next);
      setDeskTouched(false);
      onToast(translate(lang, 'deskWorkerSaved'));
      return true;
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setDeskErr(e.message);
      return false;
    } finally {
      setDeskSaving(false);
    }
  }, [deskHarness, deskModel, lang, onSettingsUpdated, onToast]);

  useEffect(() => {
    saveRef.current = saveWorker;
    resetRef.current = resetWorker;
    return () => {
      saveRef.current = null;
      resetRef.current = null;
    };
  }, [saveRef, resetRef, saveWorker, resetWorker]);

  const loadDesktopPermissions = useCallback(async (targetId = permTarget) => {
    setDeskPermsLoading(true);
    setDeskPermErr('');
    try {
      const res = await api<DesktopPermissionsResponse>(
        `/api/desktop/permissions?target=${encodeURIComponent(targetId || 'local')}`
      );
      setDeskPerms(res.items || []);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setDeskPermErr(e.message);
      setDeskPerms([]);
    } finally {
      setDeskPermsLoading(false);
    }
  }, [permTarget]);

  const refreshDesktopTab = useCallback(() => {
    setDeskPermErr('');
    api<{ targets?: DesktopTarget[] }>('/api/desktop/targets')
      .then((d) => setPermTargets(d.targets || []))
      .catch(() => setPermTargets([]));
    api<{ status?: { webrtcAvailable?: boolean; supported?: boolean; backend?: string } }>('/api/desktop/state?target=local')
      .then((st) => {
        setLocalDeskEngine(
          st?.status?.backend || (st?.status?.webrtcAvailable ? 'WebRTC / H.265·H.264' : translate(lang, 'unavailable'))
        );
        setLocalDeskSupported(Boolean(st?.status?.supported));
      })
      .catch(() => {
        setLocalDeskEngine(translate(lang, 'unavailable'));
        setLocalDeskSupported(false);
      });
    void loadDesktopPermissions(permTarget);
  }, [lang, loadDesktopPermissions, permTarget]);

  useEffect(() => {
    if (!active) return;
    refreshDesktopTab();
    const onFocus = () => {
      void loadDesktopPermissions(permTarget);
    };
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [active, permTarget, loadDesktopPermissions, refreshDesktopTab]);

  const handleRequestPermission = async (item: DesktopPermissionItem, openSettings: boolean) => {
    setPermBusyId(item.id);
    setDeskPermErr('');
    try {
      const res = await api<DesktopPermissionRequestResponse>('/api/desktop/permissions/request', {
        method: 'POST',
        body: { target: permTarget, permission: item.id, openSettings },
      });
      if (res.items) setDeskPerms(res.items);
      if (res.openedSettings) onToast(t('deskPermOpened'));
      else if (res.prompted) onToast(t('deskPermPrompted'));
      window.setTimeout(() => {
        void loadDesktopPermissions(permTarget);
      }, 1500);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setDeskPermErr(e.message);
    } finally {
      setPermBusyId('');
    }
  };

  const nonVirtualTargets = permTargets.filter((tr) => !tr.virtual);

  return (
    <div id="sTabDesktop" className="tab-pane tab-anim">
      <section className="set-card" id="sDeskPermsBox">
        <div className="set-card-hd">
          <h2>{t('deskPermsTitle')}</h2>
          <div className="row-inline-sm row-wrap">
            {nonVirtualTargets.length > 1 && (
              <select
                id="sDeskPermTarget"
                aria-label={t('deskPermTarget')}
                value={permTarget}
                onChange={(e) => {
                  const next = e.target.value;
                  setPermTarget(next);
                  void loadDesktopPermissions(next);
                }}
              >
                {nonVirtualTargets.map((tr) => (
                  <option key={tr.id} value={tr.id}>
                    {tr.id === 'local' ? t('localDesktopName') : tr.name || tr.id}
                  </option>
                ))}
              </select>
            )}
            <button
              type="button"
              id="sDeskPermsRefresh"
              className="mp"
              disabled={deskPermsLoading}
              onClick={() => void loadDesktopPermissions(permTarget)}
            >
              {deskPermsLoading ? t('loading') : t('refresh')}
            </button>
            <button
              type="button"
              id="sDeskOpenBtn"
              className="mp go"
              onClick={() => onOpenDesktopTarget(permTarget || 'local')}
            >
              {t('openDesktop')}
            </button>
          </div>
        </div>
        <p className="set-card-desc">{t('deskPermsDesc')}</p>
        <div id="sDeskPermsList" className="stack6">
          {deskPerms.map((perm) => {
            const statusLabel =
              perm.status === 'granted'
                ? t('deskPermGranted')
                : perm.status === 'denied'
                  ? t('deskPermDenied')
                  : perm.status === 'unsupported'
                    ? t('deskPermUnsupported')
                    : t('deskPermNotDetermined');
            const statusClass = perm.status === 'granted' ? ' ok' : perm.status === 'denied' ? ' bad' : '';
            const isBusy = permBusyId === perm.id;
            const permName = permText(`deskPermName_${perm.id}`, perm.name);
            const permDesc = permText(`deskPermDesc_${perm.id}`, perm.description);
            return (
              <div key={perm.id} className="node-row desk-perm-row">
                <div className="desk-perm-info">
                  <div className="desk-perm-head">
                    <span className="nm">{permName}</span>
                    <span className={`mp${statusClass}`}>{statusLabel}</span>
                  </div>
                  <div className="meta">{permDesc}</div>
                </div>
                <div className="desk-perm-acts">
                  {perm.canPrompt && perm.status !== 'granted' && (
                    <button
                      type="button"
                      className="mp go"
                      disabled={isBusy}
                      onClick={() => void handleRequestPermission(perm, false)}
                    >
                      {t('deskPermRequest')}
                    </button>
                  )}
                  {perm.canOpenSettings && (
                    <button
                      type="button"
                      className="mp"
                      disabled={isBusy}
                      onClick={() => void handleRequestPermission(perm, true)}
                    >
                      {t('deskPermOpenSettings')}
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
        {deskPermErr && <div className="msg-err">{deskPermErr}</div>}
      </section>

      <section className="set-card" id="localDeskBox">
        <div className="set-card-hd">
          <h2>{t('localDesktopTitle')}</h2>
          <span id="localDeskEngine" className={`mp${localDeskSupported ? ' ok' : ''}`}>
            {localDeskEngine || t('checking')}
          </span>
        </div>
        <div className="fld">
          <label>
            {t('deskWorkerHarness')} <em>{t('rtOptional')}</em>
          </label>
          <select
            id="sDeskHarness"
            value={deskHarness}
            onChange={(e) => {
              const next = e.target.value;
              setDeskHarness(next);
              setDeskTouched(true);
              if (next) void onLoadModels(next);
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
          <input
            id="sDeskModel"
            className="mono"
            list={deskHarness ? `models-${deskHarness}` : 'models-global'}
            placeholder={t('rtModelPh')}
            spellCheck={false}
            value={deskModel}
            onFocus={() => {
              if (deskHarness) void onLoadModels(deskHarness);
              else onPreloadAllModels(settings?.harnesses);
            }}
            onChange={(e) => {
              setDeskModel(e.target.value);
              setDeskTouched(true);
            }}
          />
        </div>
        <div className="desk-worker-foot">
          <p className="set-card-desc">{t('deskWorkerDesc')}</p>
          <button
            type="button"
            id="sDeskWorkerSave"
            className="btn go set-save"
            disabled={!isDirty || deskSaving}
            onClick={() => void saveWorker()}
          >
            {deskSaving ? t('saving') : t('save')}
          </button>
        </div>
        {deskErr && <div className="msg-err">{deskErr}</div>}
      </section>
    </div>
  );
};
