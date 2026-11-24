import React, { useCallback, useEffect, useRef, useState } from 'react';
import { resolveLang, translate } from '../lib/i18n';
import { applyThemePref, getThemePref } from '../lib/theme';

interface DesktopStateResponse {
  mode?: string;
  link?: string;
  status?: string;
  statusState?: '' | 'starting' | 'error';
  statusError?: string;
  dataDir?: string;
}

interface TailscalePeer {
  id: string;
  hostname: string;
  dnsName?: string;
  ip?: string;
  os?: string;
  address: string;
  online: boolean;
  isBotHub: boolean;
}

interface TailscaleStatus {
  state: 'disabled' | 'starting' | 'needs_login' | 'running' | 'error';
  authUrl?: string;
  hostname?: string;
  dnsName?: string;
  ip?: string;
  tailnet?: string;
  link?: string;
  error?: string;
  peers?: TailscalePeer[];
}

interface WailsWebKitBridge {
  messageHandlers?: {
    external?: {
      postMessage: (msg: string) => void;
    };
  };
}

function inferRemoteKind(raw: string): 'tailcat' | 'tailscale' {
  const s = raw.trim().toLowerCase();
  if (!s) return 'tailcat';
  if (s.startsWith('tailcat://')) return 'tailcat';
  return 'tailscale';
}

export default function DesktopSetup() {
  const [lang] = useState(() => resolveLang());
  const t = useCallback(
    (key: string, vars?: Record<string, string | number>) => translate(lang, key, vars),
    [lang],
  );

  const [dataDir, setDataDir] = useState('');
  const [remoteKind, setRemoteKind] = useState<'tailcat' | 'tailscale'>('tailcat');
  const [link, setLink] = useState('');
  const [statusState, setStatusState] = useState<'' | 'starting' | 'error'>('');
  const [statusError, setStatusError] = useState('');
  const [legacyStatus, setLegacyStatus] = useState('');
  const [busy, setBusy] = useState(false);

  // Embedded Tailscale state
  const [ts, setTs] = useState<TailscaleStatus>({ state: 'disabled' });
  const [tsBusy, setTsBusy] = useState(false);
  const [tsAdvOpen, setTsAdvOpen] = useState(false);
  const [tsManualOpen, setTsManualOpen] = useState(false);
  const [tsAuthKey, setTsAuthKey] = useState('');
  const [tsControlUrl, setTsControlUrl] = useState('');
  const [tsToken, setTsToken] = useState('');

  const linkTouchedRef = useRef(false);
  const pollTimerRef = useRef<number | null>(null);
  const tsPollTimerRef = useRef<number | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    applyThemePref(getThemePref());
    document.documentElement.lang = lang;
    return () => {
      mountedRef.current = false;
      if (pollTimerRef.current !== null) window.clearTimeout(pollTimerRef.current);
      if (tsPollTimerRef.current !== null) window.clearTimeout(tsPollTimerRef.current);
    };
  }, [lang]);

  const refresh = useCallback(async (): Promise<boolean> => {
    const res = await fetch('/api/desktop');
    const s = (await res.json()) as DesktopStateResponse;
    if (!mountedRef.current) return false;
    setDataDir(s.dataDir || '');
    if (!linkTouchedRef.current && s.link) {
      setLink((prev) => prev || s.link || '');
      setRemoteKind(inferRemoteKind(s.link));
    }
    const stState = s.statusState || '';
    setStatusState(stState);
    setStatusError(s.statusError || '');
    setLegacyStatus(stState ? '' : s.status || '');
    const isBusy = stState === 'starting';
    setBusy(isBusy);
    return isBusy;
  }, []);

  useEffect(() => {
    refresh().catch(() => {});
  }, [refresh]);

  const schedulePoll = useCallback(() => {
    if (pollTimerRef.current !== null) window.clearTimeout(pollTimerRef.current);
    pollTimerRef.current = window.setTimeout(async () => {
      try {
        const stillBusy = await refresh();
        if (stillBusy && mountedRef.current) {
          schedulePoll();
        }
      } catch {
        if (mountedRef.current) setBusy(false);
      }
    }, 500);
  }, [refresh]);

  const refreshTailscale = useCallback(async (includePeers = true): Promise<TailscaleStatus | null> => {
    try {
      const res = await fetch(`/api/desktop/tailscale${includePeers ? '?peers=1' : ''}`);
      if (!res.ok) return null;
      const data = (await res.json()) as TailscaleStatus;
      if (!mountedRef.current) return null;
      setTs(data);
      return data;
    } catch {
      return null;
    }
  }, []);

  useEffect(() => {
    if (remoteKind !== 'tailscale') {
      if (tsPollTimerRef.current !== null) {
        window.clearTimeout(tsPollTimerRef.current);
        tsPollTimerRef.current = null;
      }
      return;
    }
    let cancelled = false;
    const poll = async () => {
      const data = await refreshTailscale(true);
      if (cancelled || !mountedRef.current) return;
      if (data && (data.state === 'starting' || data.state === 'needs_login')) {
        tsPollTimerRef.current = window.setTimeout(() => {
          void poll();
        }, 1500);
      }
    };
    void poll();
    return () => {
      cancelled = true;
      if (tsPollTimerRef.current !== null) {
        window.clearTimeout(tsPollTimerRef.current);
        tsPollTimerRef.current = null;
      }
    };
  }, [remoteKind, refreshTailscale]);

  const startTailscaleSignIn = async () => {
    setTsBusy(true);
    try {
      const res = await fetch('/api/desktop/tailscale/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          authKey: tsAuthKey.trim(),
          controlUrl: tsControlUrl.trim(),
        }),
      });
      if (res.ok) {
        const data = (await res.json()) as TailscaleStatus;
        if (!mountedRef.current) return;
        setTs(data);
        setTsAuthKey('');
        if (data.state === 'starting' || data.state === 'needs_login') {
          if (tsPollTimerRef.current !== null) window.clearTimeout(tsPollTimerRef.current);
          const pollNext = async () => {
            const cur = await refreshTailscale(true);
            if (!mountedRef.current) return;
            if (cur && (cur.state === 'starting' || cur.state === 'needs_login')) {
              tsPollTimerRef.current = window.setTimeout(() => {
                void pollNext();
              }, 1500);
            }
          };
          tsPollTimerRef.current = window.setTimeout(() => {
            void pollNext();
          }, 1500);
        }
      }
    } finally {
      if (mountedRef.current) setTsBusy(false);
    }
  };

  const logoutTailscale = async () => {
    setTsBusy(true);
    try {
      const res = await fetch('/api/desktop/tailscale/logout', { method: 'POST' });
      if (res.ok && mountedRef.current) {
        setTs({ state: 'disabled' });
      }
    } finally {
      if (mountedRef.current) setTsBusy(false);
    }
  };

  const start = async (body: { mode: 'local' | 'remote'; link?: string }) => {
    setStatusState('starting');
    setStatusError('');
    setLegacyStatus('');
    setBusy(true);
    try {
      await fetch('/api/desktop/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      schedulePoll();
    } catch {
      if (mountedRef.current) {
        setStatusState('');
        setBusy(false);
      }
    }
  };

  const connectPeer = (peer: TailscalePeer) => {
    const tok = tsToken.trim();
    const fullLink = tok ? `${peer.address}#token=${encodeURIComponent(tok)}` : peer.address;
    void start({ mode: 'remote', link: fullLink });
  };

  // Native macOS titlebar drag handler for Wails WebView
  const dragArmedRef = useRef(false);
  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragArmedRef.current || !(e.buttons & 1)) {
        dragArmedRef.current = false;
        return;
      }
      dragArmedRef.current = false;
      (window as unknown as { webkit?: WailsWebKitBridge }).webkit?.messageHandlers?.external?.postMessage(
        'wails:drag',
      );
    };
    const clearArm = () => {
      dragArmedRef.current = false;
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', clearArm);
    window.addEventListener('blur', clearArm);
    return () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', clearArm);
      window.removeEventListener('blur', clearArm);
    };
  }, []);

  const trimmedLink = link.trim();
  const hasToken = trimmedLink.includes('#token=') || trimmedLink.includes('token=');
  const isError = statusState === 'error';
  const statusText =
    statusState === 'starting'
      ? t('desktopSetupStarting')
      : statusState === 'error'
        ? `${t('desktopSetupStartFailed')}${statusError}`
        : legacyStatus;

  const onlinePeers = (ts.peers || []).filter((p) => p.online);

  return (
    <div className="dsetup-page">
      <div
        className="dsetup-drag"
        onMouseDown={(e) => {
          dragArmedRef.current = e.button === 0 && e.detail === 1;
        }}
        onDoubleClick={(e) => {
          const bridge = (window as unknown as { webkit?: WailsWebKitBridge }).webkit;
          if (bridge?.messageHandlers?.external) {
            e.preventDefault();
            bridge.messageHandlers.external.postMessage('wails:drag:doubleclick');
          }
        }}
      />
      <main className="dsetup-main">
        <header className="dsetup-head">
          <h1>Pebble</h1>
          <p className="dsetup-sub">{t('desktopSetupSub')}</p>
        </header>

        <section className="dsetup-card">
          <div className="dsetup-card-hd">
            <h2>{t('desktopSetupLocalTitle')}</h2>
          </div>
          <p className="dsetup-desc">
            <span>{t('desktopSetupLocalDesc')}</span>
            <code className="dsetup-code">{dataDir}</code>
          </p>
          <div className="dsetup-actions">
            <button
              type="button"
              className="btn go"
              disabled={busy}
              onClick={() => void start({ mode: 'local' })}
            >
              {t('desktopSetupLocalBtn')}
            </button>
          </div>
        </section>

        <section className="dsetup-card">
          <div className="dsetup-card-hd">
            <h2>{t('desktopSetupRemoteTitle')}</h2>
            <div className="dsetup-tabs" role="tablist">
              <button
                type="button"
                role="tab"
                aria-selected={remoteKind === 'tailcat'}
                className={`mp${remoteKind === 'tailcat' ? ' go' : ''}`}
                onClick={() => setRemoteKind('tailcat')}
              >
                {t('desktopSetupTabTailcat')}
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={remoteKind === 'tailscale'}
                className={`mp${remoteKind === 'tailscale' ? ' go' : ''}`}
                onClick={() => setRemoteKind('tailscale')}
              >
                {t('desktopSetupTabTailscale')}
              </button>
            </div>
          </div>

          {remoteKind === 'tailcat' ? (
            <>
              <p className="dsetup-desc">{t('desktopSetupRemoteDesc')}</p>
              <textarea
                className="dsetup-textarea"
                value={link}
                onChange={(e) => {
                  const val = e.target.value;
                  linkTouchedRef.current = true;
                  setLink(val);
                  if (val.trim().toLowerCase().startsWith('tailscale://')) {
                    setRemoteKind('tailscale');
                  }
                }}
                placeholder="tailcat://connect/tc…?port=3789"
                spellCheck={false}
              />
              <p className={`dsetup-hint${hasToken ? ' ok' : ''}`}>
                {hasToken ? t('desktopSetupTokenDetected') : t('desktopSetupLinkHint')}
              </p>
              <div className="dsetup-actions">
                <button
                  type="button"
                  className="btn go"
                  disabled={busy || !trimmedLink}
                  onClick={() => void start({ mode: 'remote', link: trimmedLink })}
                >
                  {t('desktopSetupRemoteBtn')}
                </button>
              </div>
            </>
          ) : (
            <div className="dsetup-ts-box">
              {ts.state === 'running' ? (
                <>
                  <div className="dsetup-meta">
                    <div className="dsetup-ts-meta-info">
                      <span className="dsetup-meta-lbl ok">
                        {t('tsConnectedTailnet', { tailnet: ts.tailnet || 'Tailscale' })}
                      </span>
                      {ts.ip && <span className="dsetup-code">{ts.ip}</span>}
                    </div>
                    <div className="row-inline-sm">
                      <button
                        type="button"
                        className="mp"
                        disabled={tsBusy}
                        onClick={() => void refreshTailscale(true)}
                      >
                        {t('tsRefreshPeers')}
                      </button>
                      <button
                        type="button"
                        className="mp"
                        disabled={tsBusy}
                        onClick={() => void logoutTailscale()}
                      >
                        {t('tsSignOut')}
                      </button>
                    </div>
                  </div>

                  {onlinePeers.length === 0 ? (
                    <p className="dsetup-desc">{t('tsNoOnlinePeers')}</p>
                  ) : (
                    <div className="dsetup-ts-peers">
                      {onlinePeers.map((peer) => (
                        <button
                          key={peer.id}
                          type="button"
                          className="dsetup-ts-peer"
                          disabled={busy}
                          onClick={() => connectPeer(peer)}
                        >
                          <span className="dsetup-ts-peer-main">
                            <span className="dsetup-ts-peer-name">
                              {peer.hostname}
                              {peer.isBotHub && <span className="mp ok">Pebble</span>}
                            </span>
                            <span className="dsetup-code">{peer.dnsName || peer.ip || peer.address}</span>
                          </span>
                          <span className="mp go">{t('tsPeerConnect')}</span>
                        </button>
                      ))}
                    </div>
                  )}

                  <input
                    type="password"
                    className="dsetup-input mono"
                    value={tsToken}
                    onChange={(e) => setTsToken(e.target.value)}
                    placeholder={t('tsOptionalTokenPh')}
                    autoComplete="new-password"
                  />
                </>
              ) : ts.state === 'needs_login' ? (
                <div className="dsetup-meta">
                  <span className="dsetup-meta-lbl">{t('tsNeedsLoginHint')}</span>
                  {ts.authUrl && (
                    <a className="mp go" href={ts.authUrl} target="_blank" rel="noopener noreferrer">
                      {t('tsOpenLoginPage')}
                    </a>
                  )}
                </div>
              ) : ts.state === 'starting' || tsBusy ? (
                <div className="dsetup-meta">
                  <span className="dsetup-meta-lbl">{t('tsConnecting')}</span>
                </div>
              ) : (
                <>
                  <p className="dsetup-desc">{t('desktopSetupTailscaleDesc')}</p>
                  {ts.state === 'error' && ts.error && <p className="dsetup-status warn">{ts.error}</p>}
                  <div className="dsetup-actions row-inline row-wrap">
                    <button
                      type="button"
                      className="btn go"
                      disabled={busy || tsBusy}
                      onClick={() => void startTailscaleSignIn()}
                    >
                      {t('tsSignInBtn')}
                    </button>
                    <button
                      type="button"
                      className="link-btn"
                      onClick={() => setTsAdvOpen((v) => !v)}
                    >
                      {t('tsAdvancedToggle')}
                    </button>
                  </div>
                  {tsAdvOpen && (
                    <div className="dsetup-ts-adv">
                      <input
                        type="password"
                        className="dsetup-input mono"
                        value={tsAuthKey}
                        onChange={(e) => setTsAuthKey(e.target.value)}
                        placeholder={t('tsAuthKeyPh')}
                        autoComplete="new-password"
                      />
                      <input
                        type="text"
                        className="dsetup-input mono"
                        value={tsControlUrl}
                        onChange={(e) => setTsControlUrl(e.target.value)}
                        placeholder={t('tsControlUrlPh')}
                        spellCheck={false}
                      />
                    </div>
                  )}
                </>
              )}

              <button
                type="button"
                className="link-btn dsetup-ts-manual-toggle"
                onClick={() => setTsManualOpen((v) => !v)}
              >
                {t('tsManualLinkToggle')}
              </button>
              {tsManualOpen && (
                <div className="dsetup-ts-adv">
                  <textarea
                    className="dsetup-textarea"
                    value={link}
                    onChange={(e) => {
                      linkTouchedRef.current = true;
                      setLink(e.target.value);
                    }}
                    placeholder="tailscale://mac-mini.ts.net:3789#token=..."
                    spellCheck={false}
                  />
                  <div className="dsetup-actions">
                    <button
                      type="button"
                      className="btn go"
                      disabled={busy || !trimmedLink}
                      onClick={() => void start({ mode: 'remote', link: trimmedLink })}
                    >
                      {t('desktopSetupRemoteBtn')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}
        </section>

        <p className={`dsetup-status${isError ? ' warn' : ''}`}>{statusText}</p>
      </main>
    </div>
  );
}
