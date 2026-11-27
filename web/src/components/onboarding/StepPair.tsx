import React, { useCallback, useEffect, useRef, useState } from 'react';
import type { TailcatStatus } from '../../types';
import { api } from '../../lib/api';
import { DOWNLOAD_LINKS } from '../../lib/downloads';
import { buildPairingLink, maskedPairingLink, stripLinkFragment } from '../../lib/pairing';

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
  const [tokenVal, setTokenVal] = useState('');
  const [tokenShow, setTokenShow] = useState(false);
  const [copyBusy, setCopyBusy] = useState(false);
  const timerRef = useRef<number | null>(null);
  const mountedRef = useRef(true);

  const applyTailcat = useCallback((tr: TailcatStatus) => {
    setTailcat(tr);
    if (tr.state === 'ready') setQrBust(Date.now());
    if (tr.apiToken) setTokenVal(tr.apiToken);
    else if (!tr.hasToken || tr.tokenEnvRef) {
      setTokenVal('');
      setTokenShow(false);
    }
  }, []);

  const refreshTailcat = useCallback(async () => {
    try {
      const tr = await api<TailcatStatus>('/api/tailcat');
      if (!mountedRef.current) return;
      applyTailcat(tr);
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
  }, [applyTailcat, onError]);

  const resolveToken = useCallback(async (): Promise<string> => {
    if (tokenVal) return tokenVal;
    const d = await api<TailcatStatus>('/api/tailcat?reveal=1');
    const tok = d.apiToken || '';
    if (tok && mountedRef.current) setTokenVal(tok);
    return tok;
  }, [tokenVal]);

  const toggleTokenShow = useCallback(async () => {
    if (tokenShow) {
      setTokenShow(false);
      return;
    }
    try {
      const tok = await resolveToken();
      if (!mountedRef.current) return;
      if (tok) setTokenShow(true);
      else onToast(t('copyPairLinkNoToken'));
    } catch (e) {
      if (mountedRef.current && e instanceof Error && e.message !== 'unauthorized') {
        onError(e.message);
      }
    }
  }, [tokenShow, resolveToken, onToast, onError, t]);

  const copyFullLink = useCallback(
    async (baseLink: string) => {
      if (copyBusy) return;
      setCopyBusy(true);
      let full = '';
      try {
        const tok = await resolveToken();
        full = buildPairingLink(baseLink, tok);
      } catch {
        full = '';
      }
      if (!full) {
        if (mountedRef.current) setCopyBusy(false);
        onToast(t('copyPairLinkNoToken'));
        return;
      }
      try {
        await navigator.clipboard.writeText(full);
        onToast(t('pairLinkCopied'));
      } catch {
        onToast(t('copyFailed'));
      } finally {
        if (mountedRef.current) setCopyBusy(false);
      }
    },
    [copyBusy, resolveToken, onToast, t]
  );

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
                  applyTailcat(tr);
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
            {tailcat.link &&
              (() => {
                const envRef = tailcat.tokenEnvRef || '';
                const canIncludeToken = !!tailcat.hasToken && !envRef;
                const revealed =
                  canIncludeToken && tokenShow ? buildPairingLink(tailcat.link, tokenVal) : '';
                const displayText = canIncludeToken
                  ? revealed || maskedPairingLink(tailcat.link)
                  : stripLinkFragment(tailcat.link);
                const isMasked = canIncludeToken && !revealed;

                return (
                  <div id="ubRemoteLinkFld" className="pair-sec">
                    <div className="pair-link-hd">
                      <span className="pair-link-title">
                        {t(canIncludeToken ? 'pairingLinkTitle' : 'tunnelAddressTitle')}
                      </span>
                      <span className={`mp${canIncludeToken ? ' ok' : ''}`}>
                        {t(canIncludeToken ? 'pairingLinkWithToken' : 'pairingLinkNoToken')}
                      </span>
                    </div>
                    <div id="ubRemoteLink" className={`pair-link-text${isMasked ? ' masked' : ''}`}>
                      {displayText}
                    </div>
                    {canIncludeToken ? (
                      <div id="ubRemoteCopyWrap" className="row-end">
                        <button
                          type="button"
                          className="mp"
                          onClick={() => void toggleTokenShow()}
                        >
                          {tokenShow ? t('hide') : t('show')}
                        </button>
                        <button
                          type="button"
                          id="ubRemoteCopy"
                          className="mp go"
                          disabled={copyBusy}
                          onClick={() => void copyFullLink(tailcat.link || '')}
                        >
                          {t('copyPairLink')}
                        </button>
                      </div>
                    ) : (
                      <div className="set-card-desc">
                        {envRef
                          ? t('pairingLinkEnvNote', { ref: envRef })
                          : t('pairingLinkNoTokenNote')}
                      </div>
                    )}
                  </div>
                );
              })()}
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
