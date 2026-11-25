import React, { useEffect, useState } from 'react';
import type { DesktopPermissionItem } from '../../types';

interface StepPermissionsProps {
  t: (k: string, vars?: Record<string, string | number>) => string;
  permissions: DesktopPermissionItem[];
  loading: boolean;
  busy: boolean;
  busyPermId: string;
  onRefresh: () => Promise<void>;
  onRequestAll: () => Promise<void>;
  onRequestItem: (item: DesktopPermissionItem, openSettings: boolean) => Promise<void>;
}

export const StepPermissions: React.FC<StepPermissionsProps> = ({
  t,
  permissions,
  loading,
  busy,
  busyPermId,
  onRefresh,
  onRequestAll,
  onRequestItem,
}) => {
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);

  useEffect(() => {
    const refreshIfVisible = () => {
      if (document.visibilityState === 'visible') {
        void onRefresh();
      }
    };
    window.addEventListener('focus', refreshIfVisible);
    document.addEventListener('visibilitychange', refreshIfVisible);
    return () => {
      window.removeEventListener('focus', refreshIfVisible);
      document.removeEventListener('visibilitychange', refreshIfVisible);
    };
  }, [onRefresh]);

  const permText = (key: string, fallback: string) => {
    const val = t(key);
    return val === key ? fallback : val;
  };

  const actionable = permissions.filter((p) => p.status !== 'unsupported');
  const remoteItems = actionable.filter((p) => p.id !== 'full_disk_access');
  const advancedItems = actionable.filter((p) => p.id === 'full_disk_access');

  const remoteGranted = remoteItems.filter((p) => p.status === 'granted').length;
  const allRemoteGranted = remoteItems.length > 0 && remoteGranted === remoteItems.length;
  const hasPromptableRemote = remoteItems.some((p) => p.canPrompt && p.status !== 'granted');

  const renderDetailRow = (perm: DesktopPermissionItem) => {
    const granted = perm.status === 'granted';
    const isBusy = busy || busyPermId === perm.id || busyPermId === 'all';
    const permName = permText(`deskPermName_${perm.id}`, perm.name);
    const permDesc = permText(`deskPermDesc_${perm.id}`, perm.description);

    return (
      <div key={perm.id} className="unbox-perm-detail-row">
        <div className="unbox-perm-info">
          <div className="desk-perm-head">
            <span className="unbox-item-name">{permName}</span>
            <span className={`mp${granted ? ' ok' : ''}`}>
              {granted ? t('ubPermEnabled') : t('ubPermNotEnabled')}
            </span>
          </div>
          <div className="unbox-perm-desc">{permDesc}</div>
        </div>
        {!granted && (
          <div className="desk-perm-acts">
            {perm.canPrompt && (
              <button
                type="button"
                className="mp go"
                disabled={isBusy}
                onClick={() => void onRequestItem(perm, false)}
              >
                {t('deskPermRequest')}
              </button>
            )}
            {perm.canOpenSettings && (
              <button
                type="button"
                className="mp"
                disabled={isBusy}
                onClick={() => void onRequestItem(perm, true)}
              >
                {t('deskPermOpenSettings')}
              </button>
            )}
          </div>
        )}
      </div>
    );
  };

  return (
    <div id="ubStepPerms" className="set-stack">
      <div className="note">
        <span className="set-card-desc">{t('ubPermNoNeedBanner')}</span>
      </div>

      {remoteItems.length > 0 && (
        <section className="unbox-perm-card">
          <div className="row-between row-wrap">
            <div className="row-inline row-wrap">
              <strong>{t('ubPermRemoteTitle')}</strong>
              <span className={`mp${allRemoteGranted ? ' ok' : ''}`}>
                {allRemoteGranted
                  ? t('ubPermRemoteAllReady')
                  : t('ubPermRemoteProgress', { granted: remoteGranted, total: remoteItems.length })}
              </span>
            </div>
            <button
              type="button"
              id="ubPermRefresh"
              className="mp"
              disabled={loading || busy}
              onClick={() => void onRefresh()}
            >
              {loading ? t('loading') : t('refresh')}
            </button>
          </div>

          <p className="unbox-perm-desc">{t('ubPermRemoteDesc')}</p>

          <div className="unbox-perm-caps">
            {remoteItems.map((perm) => {
              const granted = perm.status === 'granted';
              const permName = permText(`deskPermName_${perm.id}`, perm.name);
              const capPurpose = permText(`ubPermCap_${perm.id}`, '');
              return (
                <div key={perm.id} className={`unbox-perm-cap${granted ? ' on' : ''}`}>
                  <span className="unbox-perm-cap-dot" aria-hidden="true" />
                  <span className="unbox-perm-cap-name">{permName}</span>
                  {capPurpose && <span className="unbox-perm-cap-purpose">· {capPurpose}</span>}
                </div>
              );
            })}
          </div>

          <div className="row-between row-wrap">
            <div className="row-inline-sm">
              {hasPromptableRemote && (
                <button
                  type="button"
                  id="ubPermRequestAll"
                  className="mp go"
                  disabled={busy || Boolean(busyPermId)}
                  onClick={() => {
                    setDetailsOpen(true);
                    void onRequestAll();
                  }}
                >
                  {t('ubPermRemoteEnableBtn')}
                </button>
              )}
              <button
                type="button"
                id="ubPermDetailsToggle"
                className="mp"
                onClick={() => setDetailsOpen((v) => !v)}
              >
                {detailsOpen ? t('ubPermHideDetails') : t('ubPermShowDetails')}
              </button>
            </div>
          </div>

          {detailsOpen && (
            <div id="ubPermsList" className="unbox-perm-details">
              {remoteItems.map(renderDetailRow)}
            </div>
          )}
        </section>
      )}

      {advancedItems.length > 0 && (
        <section className="note unbox-fold">
          <div className="unbox-fold-hd">
            <div className="unbox-fold-info">
              <strong>{t('ubPermAdvTitle')}</strong>
              <span className="set-card-desc">{t('ubPermAdvHint')}</span>
            </div>
            <button
              type="button"
              id="ubPermAdvToggle"
              className="mp"
              onClick={() => setAdvancedOpen((v) => !v)}
            >
              {advancedOpen ? t('ubCollapseConfig') : t('ubExpandConfig')}
            </button>
          </div>
          {advancedOpen && <div className="unbox-perm-details">{advancedItems.map(renderDetailRow)}</div>}
        </section>
      )}
    </div>
  );
};
