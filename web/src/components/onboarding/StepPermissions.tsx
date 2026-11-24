import React, { useEffect } from 'react';
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

  const promptable = permissions.filter((p) => p.canPrompt);
  const coreItems = promptable.length > 0 ? promptable : permissions;
  const grantedCore = coreItems.filter((p) => p.status === 'granted').length;
  const allCoreGranted = coreItems.length > 0 && grantedCore === coreItems.length;
  const hasPromptableUngranted = permissions.some((p) => p.canPrompt && p.status !== 'granted');

  return (
    <div id="ubStepPerms" className="set-stack">
      <div className="note">
        <div className="row-between row-wrap">
          <span className={`mp${allCoreGranted ? ' ok' : ''}`}>
            {allCoreGranted
              ? t('ubPermAllReady')
              : t('ubPermProgress', { granted: grantedCore, total: coreItems.length })}
          </span>
          <div className="row-inline-sm">
            <button
              type="button"
              id="ubPermRefresh"
              className="mp"
              disabled={loading || busy}
              onClick={() => void onRefresh()}
            >
              {loading ? t('loading') : t('refresh')}
            </button>
            {hasPromptableUngranted && (
              <button
                type="button"
                id="ubPermRequestAll"
                className="mp go"
                disabled={busy || Boolean(busyPermId)}
                onClick={() => void onRequestAll()}
              >
                {t('ubPermRequestAll')}
              </button>
            )}
          </div>
        </div>
        <div className="set-card-desc">{t('ubPermSummaryHint')}</div>
      </div>

      <div id="ubPermsList" className="unbox-list">
        {permissions.map((perm) => {
          const statusLabel =
            perm.status === 'granted'
              ? t('deskPermGranted')
              : perm.status === 'denied'
                ? t('deskPermDenied')
                : perm.status === 'unsupported'
                  ? t('deskPermUnsupported')
                  : t('deskPermNotDetermined');
          const statusClass =
            perm.status === 'granted' ? ' ok' : perm.status === 'denied' ? ' bad' : '';
          const isBusy = busy || busyPermId === perm.id || busyPermId === 'all';
          const permName = permText(`deskPermName_${perm.id}`, perm.name);
          const permDesc = permText(`deskPermDesc_${perm.id}`, perm.description);

          return (
            <div key={perm.id} className="unbox-item">
              <div className="unbox-perm-row">
                <div className="unbox-perm-info">
                  <div className="desk-perm-head">
                    <span className="unbox-item-name">{permName}</span>
                    {!perm.canPrompt && <span className="mp">{t('ubOptional')}</span>}
                    <span className={`mp${statusClass}`}>{statusLabel}</span>
                  </div>
                  <div className="unbox-perm-desc">{permDesc}</div>
                </div>
                <div className="desk-perm-acts">
                  {perm.canPrompt && perm.status !== 'granted' && (
                    <button
                      type="button"
                      className="mp go"
                      disabled={isBusy}
                      onClick={() => void onRequestItem(perm, false)}
                    >
                      {t('deskPermRequest')}
                    </button>
                  )}
                  {perm.canOpenSettings && perm.status !== 'granted' && (
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
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};
