import React, { useCallback, useEffect, useRef, useState } from 'react';
import type { TailcatStatus } from '../../types';
import { api } from '../../lib/api';
import { DOWNLOAD_LINKS } from '../../lib/downloads';

interface StepPairProps {
  t: (k: string, vars?: Record<string, string | number>) => string;
  busy: boolean;
  onBusyChange: (b: boolean) => void;
  onToast: (msg: string) => void;
  onError: (msg: string) => void;
}

export const StepPair: React.FC<StepPairProps> = ({
  t,
  busy,
  onBusyChange,
  onToast,
  onError,
}) => {
  const [tailcat, setTailcat] = useState<TailcatStatus | null>(null);
  const [qrBust, setQrBust] = useState(() => Date.now());
  const timerRef = useRef<number | null>(null);
  const mountedRef = useRef(true);

  const refreshTailcat = useCallback(async () => {
    try {
      const tr = await api<TailcatStatus>('/api/tailcat');
      if (!mountedRef.current) return;
      setTailcat(tr);
      if (tr.state === 'ready') setQrBust(Date.now());
      if (tr.error) onError(tr.error);
      if (timerRef.current) window.clearTimeout(timerRef.current);
      if (tr.state === 'connecting' && mountedRef.current) {
        timerRef.current = window.setTimeout(() => {
          void refreshTailcat();
        }, 800);
      }
    } catch (e) {
      if (mountedRef.current && e instanceof Error && e.message !== 'unauthorized') {
        onError(e.message);
      }
    }
  }, [onError]);

  useEffect(() => {
    mountedRef.current = true;
    void refreshTailcat();
    return () => {
      mountedRef.current = false;
      if (timerRef.current) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [refreshTailcat]);

  return (
    <div id="ubStep3" className="set-stack">
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
                onError('');
                onBusyChange(true);
                try {
                  const tr = await api<TailcatStatus>('/api/tailcat', {
                    method: 'POST',
                    body: { enabled: true },
                  });
                  if (!mountedRef.current) return;
                  setTailcat(tr);
                  if (tr.state === 'ready') setQrBust(Date.now());
                  if (tr.state === 'connecting') void refreshTailcat();
                  onToast(t('remoteEnabledToast'));
                } catch (e) {
                  if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
                } finally {
                  if (mountedRef.current) onBusyChange(false);
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
  );
};
