import React, { useCallback, useEffect, useRef, useState } from 'react';
import type {
  ConfirmOptions,
  LocalNodeState,
  NodeInfo,
  ResolvedLang,
  TailcatStatus,
  TailscaleStatus,
} from '../../types';
import { api, CLIENT_PROTOCOL } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { buildPairingLink, maskedPairingLink, stripLinkFragment } from '../../lib/pairing';

interface NetworkTabProps {
  active: boolean;
  nodesFromEvents: NodeInfo[] | null;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onConfirm: (opts: ConfirmOptions) => void;
  onOpenDesktopTarget: (target: string) => void;
}

export const NetworkTab: React.FC<NetworkTabProps> = ({
  active,
  nodesFromEvents,
  lang,
  onToast,
  onConfirm,
  onOpenDesktopTarget,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [localNode, setLocalNode] = useState<LocalNodeState | null>(null);
  const [localNodeName, setLocalNodeName] = useState('');
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [tailcat, setTailcat] = useState<TailcatStatus | null>(null);
  const [tailscale, setTailscale] = useState<TailscaleStatus | null>(null);
  const [tsQrBust, setTsQrBust] = useState(() => Date.now());
  const [tsHostnameIn, setTsHostnameIn] = useState('');
  const [tsControlUrlIn, setTsControlUrlIn] = useState('');
  const [tsAuthKeyIn, setTsAuthKeyIn] = useState('');
  const [tsBusy, setTsBusy] = useState(false);
  const [tsErr, setTsErr] = useState('');
  const [qrBust, setQrBust] = useState(() => Date.now());
  const [remoteTokenIn, setRemoteTokenIn] = useState('');
  const [remoteTokenVal, setRemoteTokenVal] = useState('');
  const [remoteTokenShow, setRemoteTokenShow] = useState(false);
  const [remoteErr, setRemoteErr] = useState('');
  const [remoteBusy, setRemoteBusy] = useState(false);
  const [pairCopyBusy, setPairCopyBusy] = useState(false);

  const remoteTimerRef = useRef<number | null>(null);
  const tsTimerRef = useRef<number | null>(null);
  const localNodeTimerRef = useRef<number | null>(null);

  const clearTimers = useCallback(() => {
    if (remoteTimerRef.current) {
      window.clearTimeout(remoteTimerRef.current);
      remoteTimerRef.current = null;
    }
    if (tsTimerRef.current) {
      window.clearTimeout(tsTimerRef.current);
      tsTimerRef.current = null;
    }
    if (localNodeTimerRef.current) {
      window.clearTimeout(localNodeTimerRef.current);
      localNodeTimerRef.current = null;
    }
  }, []);

  useEffect(() => {
    if (nodesFromEvents) setNodes(nodesFromEvents);
  }, [nodesFromEvents]);

  const loadLocalNode = useCallback(async () => {
    try {
      const res = await fetch('/api/local-node');
      const st = res.ok ? ((await res.json()) as LocalNodeState) : null;
      const valid = st && st.available ? st : null;
      setLocalNode(valid);
      if (valid) setLocalNodeName(valid.name || '');
      if (localNodeTimerRef.current) window.clearTimeout(localNodeTimerRef.current);
      if (valid && valid.enabled && !valid.connected && active) {
        localNodeTimerRef.current = window.setTimeout(() => void loadLocalNode(), 1000);
      }
    } catch {
      setLocalNode(null);
    }
  }, [active]);

  const applyTailcatState = useCallback(
    (tr: TailcatStatus) => {
      setTailcat(tr);
      if (tr.state === 'ready') setQrBust(Date.now());
      if (tr.apiToken) setRemoteTokenVal(tr.apiToken);
      else if (!tr.hasToken || tr.tokenEnvRef) setRemoteTokenVal('');
      if (remoteTimerRef.current) window.clearTimeout(remoteTimerRef.current);
      if (tr.state === 'connecting' && active) {
        remoteTimerRef.current = window.setTimeout(async () => {
          try {
            applyTailcatState(await api<TailcatStatus>('/api/tailcat'));
          } catch {}
        }, 800);
      }
    },
    [active]
  );

  const applyTailscaleState = useCallback(
    (ts: TailscaleStatus) => {
      setTailscale(ts);
      if (ts.state === 'running') setTsQrBust(Date.now());
      if (ts.hostname !== undefined) setTsHostnameIn((prev) => prev || ts.hostname || '');
      if (ts.controlUrl !== undefined) setTsControlUrlIn((prev) => prev || ts.controlUrl || '');
      if (ts.apiToken) setRemoteTokenVal(ts.apiToken);
      else if (!ts.hasToken || ts.tokenEnvRef) setRemoteTokenVal('');
      if (tsTimerRef.current) window.clearTimeout(tsTimerRef.current);
      if ((ts.state === 'starting' || ts.state === 'needs_login') && active) {
        tsTimerRef.current = window.setTimeout(async () => {
          try {
            applyTailscaleState(await api<TailscaleStatus>('/api/tailscale'));
          } catch {}
        }, 2000);
      }
    },
    [active]
  );

  useEffect(() => {
    if (!active) {
      clearTimers();
      return;
    }
    setRemoteErr('');
    setTsErr('');
    setRemoteTokenIn('');
    setRemoteTokenVal('');
    setRemoteTokenShow(false);
    void loadLocalNode();
    api<{ nodes?: NodeInfo[] }>('/api/nodes')
      .then((d) => setNodes(d.nodes || []))
      .catch(() => setNodes([]));
    api<TailcatStatus>('/api/tailcat')
      .then(applyTailcatState)
      .catch((err) => applyTailcatState({ state: 'error', error: err instanceof Error ? err.message : '' }));
    api<TailscaleStatus>('/api/tailscale')
      .then(applyTailscaleState)
      .catch((err) => applyTailscaleState({ state: 'error', error: err instanceof Error ? err.message : '' }));

    return clearTimers;
  }, [active, clearTimers, loadLocalNode, applyTailcatState, applyTailscaleState]);

  const onlineNodeCount = nodes.filter((n) => n.connected).length;

  const resolveRemoteToken = useCallback(async (): Promise<string> => {
    if (remoteTokenVal) return remoteTokenVal;
    const d = await api<TailcatStatus>('/api/tailcat?reveal=1');
    const tok = d.apiToken || '';
    if (tok) setRemoteTokenVal(tok);
    return tok;
  }, [remoteTokenVal]);

  const toggleRemoteTokenShow = useCallback(async () => {
    if (remoteTokenShow) {
      setRemoteTokenShow(false);
      return;
    }
    try {
      const tok = await resolveRemoteToken();
      if (tok) setRemoteTokenShow(true);
      else onToast(t('copyPairLinkNoToken'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setRemoteErr(e.message);
    }
  }, [remoteTokenShow, resolveRemoteToken, onToast, t]);

  const copyFullPairingLink = useCallback(
    async (baseLink: string) => {
      if (pairCopyBusy) return;
      setPairCopyBusy(true);
      let full = '';
      try {
        const tok = await resolveRemoteToken();
        full = buildPairingLink(baseLink, tok);
      } catch {
        full = '';
      }
      if (!full) {
        setPairCopyBusy(false);
        onToast(t('copyPairLinkNoToken'));
        return;
      }
      try {
        await navigator.clipboard.writeText(full);
        onToast(t('pairLinkCopied'));
      } catch {
        onToast(t('copyFailed'));
      } finally {
        setPairCopyBusy(false);
      }
    },
    [pairCopyBusy, resolveRemoteToken, onToast, t]
  );

  const renderPairingLinkBox = ({
    boxId,
    codeId,
    copyBtnId,
    baseLink,
    hasToken,
    envRef,
  }: {
    boxId: string;
    codeId: string;
    copyBtnId?: string;
    baseLink: string;
    hasToken: boolean;
    envRef: string;
  }) => {
    const canIncludeToken = hasToken && !envRef;
    const revealed = canIncludeToken && remoteTokenShow ? buildPairingLink(baseLink, remoteTokenVal) : '';
    const displayText = canIncludeToken
      ? revealed || maskedPairingLink(baseLink)
      : stripLinkFragment(baseLink);
    const isMasked = canIncludeToken && !revealed;

    return (
      <div className="pair-sec" id={boxId}>
        <div className="pair-link-hd">
          <span className="pair-link-title">
            {t(canIncludeToken ? 'pairingLinkTitle' : 'tunnelAddressTitle')}
          </span>
          <span className={`mp${canIncludeToken ? ' ok' : ''}`}>
            {t(canIncludeToken ? 'pairingLinkWithToken' : 'pairingLinkNoToken')}
          </span>
        </div>
        <div id={codeId} className={`pair-link-text${isMasked ? ' masked' : ''}`}>
          {displayText}
        </div>
        {canIncludeToken ? (
          <div className="row-end" id={copyBtnId ? `${copyBtnId}Wrap` : undefined}>
            <button
              type="button"
              className="mp"
              onClick={() => void toggleRemoteTokenShow()}
            >
              {remoteTokenShow ? t('hide') : t('show')}
            </button>
            <button
              type="button"
              id={copyBtnId}
              className="mp go"
              disabled={pairCopyBusy}
              onClick={() => void copyFullPairingLink(baseLink)}
            >
              {t('copyPairLink')}
            </button>
          </div>
        ) : (
          <div className="set-card-desc">
            {envRef ? t('pairingLinkEnvNote', { ref: envRef }) : t('pairingLinkNoTokenNote')}
          </div>
        )}
      </div>
    );
  };

  return (
    <div id="sTabRemote" className="tab-pane tab-anim">
      {localNode && (
        <section className="set-card" id="localNodeBox">
          <div className="set-card-hd">
            <h2>
              <span>{t('localNode')}</span>{' '}
              <em id="localNodeId">{localNode.name ? `(${localNode.name})` : ''}</em>
            </h2>
            <div className="row-inline-sm">
              <span id="localNodeState" className={`mp${localNode.connected ? ' ok' : ''}`}>
                {({
                  disabled: t('disabled'),
                  connecting: t('nodeConnecting'),
                  connected: t('nodeConnected'),
                  waiting_auth: t('nodeWaitingAuth'),
                  error: t('nodeError'),
                } as Record<string, string>)[localNode.status] || localNode.status}
              </span>
              <button
                type="button"
                role="switch"
                aria-checked={localNode.enabled}
                aria-label={t('localNode')}
                className={`sw${localNode.enabled ? ' on' : ''}`}
                id="localNodeToggle"
                onClick={() => {
                  const next = !localNode.enabled;
                  const apply = async () => {
                    setRemoteErr('');
                    try {
                      const res = await fetch('/api/local-node', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ enabled: next }),
                      });
                      const d = (await res.json()) as LocalNodeState & { error?: string };
                      if (!res.ok) throw new Error(d.error || `HTTP ${res.status}`);
                      setLocalNode(d);
                      onToast(d.enabled ? t('localNodeEnabled') : t('localNodeDisabled'));
                      setTimeout(
                        () =>
                          api<{ nodes?: NodeInfo[] }>('/api/nodes')
                            .then((r) => setNodes(r.nodes || []))
                            .catch(() => {}),
                        350
                      );
                    } catch (e) {
                      setRemoteErr(e instanceof Error ? e.message : String(e));
                    }
                  };
                  if (!next) {
                    void apply();
                    return;
                  }
                  onConfirm({
                    title: t('localNodeConfirmTitle'),
                    text: t('localNodeConfirmDesc'),
                    okText: t('confirmEnable'),
                    okClass: 'go',
                    onOk: apply,
                  });
                }}
              />
            </div>
          </div>
          <div className="row-inline">
            <input
              id="localNodeNameIn"
              className="note-in"
              placeholder={t('localNodeNamePh')}
              autoComplete="off"
              value={localNodeName}
              onChange={(e) => setLocalNodeName(e.target.value)}
            />
            <button
              type="button"
              className="mp"
              id="localNodeNameSave"
              onClick={async () => {
                const name = localNodeName.trim();
                if (!name) {
                  setRemoteErr(t('nodeNameEmpty'));
                  return;
                }
                setRemoteErr('');
                try {
                  const res = await fetch('/api/local-node', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ name }),
                  });
                  const d = await res.json();
                  if (!res.ok) throw new Error(d.error || `HTTP ${res.status}`);
                  setLocalNode(d);
                  onToast(t('nodeNameSaved'));
                } catch (e) {
                  setRemoteErr(e instanceof Error ? e.message : String(e));
                }
              }}
            >
              {t('saveName')}
            </button>
          </div>
          <p className="set-card-desc">{t('localNodeDesc')}</p>
          <div className="perms" id="localNodePerms">
            {(
              [
                ['files', 'permFiles'],
                ['exec', 'permExec'],
                ['desktop', 'permDesktop'],
                ['agent', 'permAgent'],
              ] as const
            ).map(([perm, labelKey]) => {
              const checked = (localNode.permissions || []).includes(perm);
              return (
                <label key={perm} className="perm">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={async (e) => {
                      const cur = new Set(localNode.permissions || []);
                      if (e.target.checked) cur.add(perm);
                      else cur.delete(perm);
                      try {
                        const res = await fetch('/api/local-node', {
                          method: 'POST',
                          headers: { 'Content-Type': 'application/json' },
                          body: JSON.stringify({ permissions: [...cur] }),
                        });
                        const d = await res.json();
                        if (!res.ok) throw new Error(d.error || `HTTP ${res.status}`);
                        setLocalNode(d);
                      } catch (err) {
                        setRemoteErr(err instanceof Error ? err.message : String(err));
                      }
                    }}
                  />
                  <span>{t(labelKey)}</span>
                </label>
              );
            })}
          </div>
        </section>
      )}

      <section className="set-card">
        <div className="set-card-hd">
          <h2>
            <span>{t('tailcatTunnel')}</span>{' '}
            <em>
              ·{' '}
              <span id="remoteState">
                {tailcat
                  ? {
                      disabled: t('disabled'),
                      connecting: t('remoteConnecting'),
                      ready: t('remoteReady'),
                      error: t('remoteError'),
                    }[tailcat.state] || tailcat.state
                  : t('unreadState')}
              </span>
            </em>
          </h2>
          {(() => {
            const tcOn = !!tailcat && tailcat.state !== 'disabled' && tailcat.state !== 'error';
            return (
              <button
                type="button"
                role="switch"
                aria-checked={tcOn}
                aria-label={t('tailcatTunnel')}
                id="remoteToggle"
                className={`sw${tcOn ? ' on' : ''}`}
                disabled={remoteBusy}
                onClick={() => {
                  const enable = !tcOn;
                  const run = async () => {
                    setRemoteBusy(true);
                    setRemoteErr('');
                    try {
                      const body: Record<string, unknown> = { enabled: enable };
                      if (enable) body.apiToken = remoteTokenIn.trim();
                      applyTailcatState(await api<TailcatStatus>('/api/tailcat', { method: 'POST', body }));
                      onToast(enable ? t('remoteEnabledToast') : t('remoteDisabledToast'));
                    } catch (e) {
                      if (e instanceof Error && e.message !== 'unauthorized') setRemoteErr(e.message);
                    } finally {
                      setRemoteBusy(false);
                    }
                  };
                  if (enable) void run();
                  else {
                    onConfirm({
                      title: t('remoteDisableTitle'),
                      text: t('remoteDisableDesc'),
                      okText: t('confirmDisable'),
                      onOk: run,
                    });
                  }
                }}
              />
            );
          })()}
        </div>
        {tailcat?.state === 'ready' && (
          <>
            <img id="remoteQR" alt="" src={`/api/tailcat/qr.png?t=${qrBust}`} className="qr-box" />
            {tailcat.link &&
              renderPairingLinkBox({
                boxId: 'remoteLinkFld',
                codeId: 'remoteLink',
                copyBtnId: 'remoteCopy',
                baseLink: tailcat.link,
                hasToken: !!tailcat.hasToken,
                envRef: tailcat.tokenEnvRef || '',
              })}
          </>
        )}
        {(tailcat?.state === 'disabled' || tailcat?.state === 'error') && !tailcat.hasToken && (
          <div className="fld" id="remoteTokenFld">
            <label dangerouslySetInnerHTML={{ __html: t('accessTokenOptional') }} />
            <input
              id="remoteTokenIn"
              className="mono"
              placeholder={t('accessTokenPh')}
              autoComplete="off"
              value={remoteTokenIn}
              onChange={(e) => setRemoteTokenIn(e.target.value)}
            />
          </div>
        )}
        <p id="remoteHelp" className="set-card-desc">
          {tailcat?.state === 'ready'
            ? t(tailcat.tokenEnvRef ? 'remoteHelpEnvReady' : 'remoteHelpReady')
            : !tailcat || tailcat.state === 'disabled'
              ? t('remoteHelpOff')
              : tailcat.state === 'connecting'
                ? t('remoteHelpConnecting')
                : tailcat.error || t('remoteHelpFallback')}
        </p>
        {tailcat?.hasToken && (
          <div className="pair-sec" id="remoteTokBox">
            <div className="row-between row-wrap">
              <span id="remoteTokTitle" className="pair-link-title">
                {tailcat.tokenEnvRef ? t('accessTokenEnv') : t('accessTokenReusable')}
              </span>
              {!tailcat.tokenEnvRef && (
                <div id="remoteTokActs" className="row-inline-sm">
                  <button
                    type="button"
                    className="mp"
                    id="remoteTokEye"
                    onClick={() => void toggleRemoteTokenShow()}
                  >
                    {remoteTokenShow ? t('hide') : t('show')}
                  </button>
                  <button
                    type="button"
                    className="mp"
                    id="remoteTokCopy"
                    onClick={async () => {
                      try {
                        const tok = await resolveRemoteToken();
                        if (!tok) {
                          onToast(t('copyPairLinkNoToken'));
                          return;
                        }
                        await navigator.clipboard.writeText(tok);
                        onToast(t('tokenCopied'));
                      } catch {
                        onToast(t('copyFailed'));
                      }
                    }}
                  >
                    {t('copy')}
                  </button>
                  <button
                    type="button"
                    className="mp"
                    id="remoteTokReset"
                    onClick={() =>
                      onConfirm({
                        title: t('resetTokenTitle'),
                        text: t('resetTokenDesc'),
                        okText: t('confirmResetToken'),
                        onOk: async () => {
                          const r = await api<TailcatStatus>('/api/tailcat', {
                            method: 'POST',
                            body: { resetToken: true },
                          });
                          if (r.apiToken) {
                            setRemoteTokenVal(r.apiToken);
                            setRemoteTokenShow(true);
                          } else {
                            setRemoteTokenVal('');
                            setRemoteTokenShow(false);
                          }
                          applyTailcatState(r);
                          setTsQrBust(Date.now());
                          onToast(t('tokenResetToast'));
                        },
                      })
                    }
                  >
                    {t('resetToken')}
                  </button>
                </div>
              )}
            </div>
            {tailcat.tokenEnvRef ? (
              <div id="remoteTokEnv" className="set-card-desc">
                {t('accessTokenEnvHint', { ref: tailcat.tokenEnvRef })}
              </div>
            ) : (
              <>
                <div
                  id="remoteTokVal"
                  className={`pair-link-text${remoteTokenShow && remoteTokenVal ? '' : ' masked'}`}
                >
                  {remoteTokenShow && remoteTokenVal ? remoteTokenVal : '•'.repeat(24)}
                </div>
                <div className="set-card-desc">{t('accessTokenStandaloneHint')}</div>
              </>
            )}
          </div>
        )}
      </section>

      <section className="set-card" id="tailscaleBox">
        <div className="set-card-hd">
          <h2>
            <span>{t('tsTitle')}</span>{' '}
            <em>
              ·{' '}
              <span id="tsState">
                {tailscale
                  ? {
                      disabled: t('disabled'),
                      starting: t('tsStarting'),
                      needs_login: t('tsNeedsLogin'),
                      running: t('tsRunning'),
                      error: t('remoteError'),
                    }[tailscale.state] || tailscale.state
                  : t('unreadState')}
              </span>
            </em>
          </h2>
          {(() => {
            const tsOn = !!tailscale && tailscale.state !== 'disabled' && tailscale.state !== 'error';
            return (
              <button
                type="button"
                role="switch"
                aria-checked={tsOn}
                aria-label={t('tsTitle')}
                id="tsToggle"
                className={`sw${tsOn ? ' on' : ''}`}
                disabled={tsBusy}
                onClick={async () => {
                  const enable = !tsOn;
                  setTsBusy(true);
                  setTsErr('');
                  try {
                    const body: Record<string, unknown> = { enabled: enable };
                    if (enable) {
                      if (tsHostnameIn.trim()) body.hostname = tsHostnameIn.trim();
                      if (tsControlUrlIn.trim()) body.controlUrl = tsControlUrlIn.trim();
                      if (tsAuthKeyIn.trim()) body.authKey = tsAuthKeyIn.trim();
                      if (remoteTokenIn.trim()) body.apiToken = remoteTokenIn.trim();
                    }
                    const res = await api<TailscaleStatus>('/api/tailscale', { method: 'POST', body });
                    setTsAuthKeyIn('');
                    applyTailscaleState(res);
                    onToast(enable ? t('tsEnabledToast') : t('tsDisabledToast'));
                  } catch (e) {
                    if (e instanceof Error && e.message !== 'unauthorized') setTsErr(e.message);
                  } finally {
                    setTsBusy(false);
                  }
                }}
              />
            );
          })()}
        </div>

        {tailscale?.state === 'needs_login' && tailscale.authUrl && (
          <div className="note">
            <div className="row-inline-sm row-wrap">
              <a
                href={tailscale.authUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="mp go"
                id="tsOpenLogin"
              >
                {t('tsOpenLogin')}
              </a>
              <button
                type="button"
                className="mp"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(tailscale.authUrl || '');
                    onToast(t('tsLoginUrlCopied'));
                  } catch {
                    onToast(t('copyFailed'));
                  }
                }}
              >
                {t('tsCopyLoginUrl')}
              </button>
            </div>
          </div>
        )}

        {tailscale?.state === 'running' && (
          <>
            <img id="tsQR" alt="" src={`/api/tailscale/qr.png?t=${tsQrBust}`} className="qr-box" />
            <div className="row-between">
              <span className="set-card-desc">
                {(tailscale.dnsName || tailscale.ip) && (
                  <>
                    <code className="mono">{tailscale.dnsName || tailscale.ip}</code>
                    {tailscale.dnsName && tailscale.ip ? ` (${tailscale.ip})` : ''}
                  </>
                )}
              </span>
              <button
                type="button"
                className="mp bad"
                disabled={tsBusy}
                onClick={() =>
                  onConfirm({
                    title: t('tsLogoutTitle'),
                    text: t('tsLogoutDesc'),
                    okText: t('tsConfirmLogout'),
                    onOk: async () => {
                      setTsBusy(true);
                      try {
                        const res = await api<TailscaleStatus>('/api/tailscale', {
                          method: 'POST',
                          body: { logout: true },
                        });
                        applyTailscaleState(res);
                        onToast(t('tsLoggedOutToast'));
                      } finally {
                        setTsBusy(false);
                      }
                    },
                  })
                }
              >
                {t('tsLogout')}
              </button>
            </div>
            {tailscale.link &&
              renderPairingLinkBox({
                boxId: 'tsLinkFld',
                codeId: 'tsLink',
                baseLink: tailscale.link,
                hasToken: tailscale.hasToken ?? !!tailcat?.hasToken,
                envRef: tailscale.tokenEnvRef ?? tailcat?.tokenEnvRef ?? '',
              })}
          </>
        )}

        <p className="set-card-desc">
          {tailscale?.state === 'running'
            ? t('tsHelpReady', { tailnet: tailscale.tailnet || 'Tailscale' })
            : tailscale?.state === 'needs_login'
              ? t('tsHelpLogin')
              : tailscale?.state === 'starting'
                ? t('tsHelpStarting')
                : tailscale?.error || tsErr || t('tsHelpOff')}
        </p>

        <details className="ts-adv">
          <summary className="set-card-desc">{t('tsAdvanced')}</summary>
          <div className="ts-adv-body">
            <div className="fld">
              <label>{t('tsHostname')}</label>
              <input
                className="mono"
                placeholder="bothub-mac"
                value={tsHostnameIn}
                onChange={(e) => setTsHostnameIn(e.target.value)}
              />
            </div>
            <div className="fld">
              <label>{t('tsAuthKey')}</label>
              {tailscale?.authKeyEnvRef ? (
                <div className="set-card-desc">
                  {t('tsAuthKeyEnvHint', { ref: tailscale.authKeyEnvRef })}
                </div>
              ) : (
                <input
                  type="password"
                  className="mono"
                  placeholder={t('tsAuthKeyPh')}
                  autoComplete="off"
                  value={tsAuthKeyIn}
                  onChange={(e) => setTsAuthKeyIn(e.target.value)}
                />
              )}
            </div>
            <div className="fld">
              <label>{t('tsControlUrl')}</label>
              <input
                className="mono"
                placeholder={t('tsControlUrlPh')}
                autoComplete="off"
                value={tsControlUrlIn}
                onChange={(e) => setTsControlUrlIn(e.target.value)}
              />
            </div>
          </div>
        </details>
      </section>

      <section className="set-card" id="remoteNodesBox">
        <div className="set-card-hd">
          <h2>{t('connectedNodes')}</h2>
          <span className="mp" id="remoteNodeCount">
            {t('onlineCount', { n: onlineNodeCount })}
          </span>
        </div>
        <div id="remoteNodesList" className="stack6">
          {nodes.length === 0 ? (
            <div className="set-card-desc">{t('noRemoteNodes')}</div>
          ) : (
            nodes.map((n) => {
              const permLabel = (p: string) =>
                t(
                  ({
                    files: 'permFileShort',
                    exec: 'permExecShort',
                    desktop: 'permDeskShort',
                    agent: 'permAgentShort',
                  } as Record<string, string>)[p] || p
                );
              const perms = (n.permissions || []).map(permLabel).join(' · ') || t('noPerm');
              const meta = [n.os && n.arch ? `${n.os}/${n.arch}` : n.os || '', perms].filter(Boolean).join(' · ');
              const canDesk = n.connected && (n.permissions || []).includes('desktop');
              const hasAgentPerm =
                (n.permissions || []).includes('agent') ||
                (n.capabilities || []).includes('node.agent_exec');
              const installedHarnesses = (n.harnesses || []).filter((h) => h.installed);
              const workspaces = n.workspaces || [];
              const activeCount = (n.activeTaskIds || []).length;
              const isOutdated = typeof n.protocolVersion === 'number' && n.protocolVersion < CLIENT_PROTOCOL;
              return (
                <div key={n.id} className="node-row">
                  <div>
                    <div className="nm">{n.name || n.id}</div>
                    <div className="meta" title={isOutdated ? t('nodeOutdatedHint') : undefined}>
                      {isOutdated ? [meta, t('nodeOutdatedHint')].filter(Boolean).join(' · ') : meta}
                    </div>
                    {(hasAgentPerm || n.maxPermissionMode || installedHarnesses.length > 0 || workspaces.length > 0 || activeCount > 0) && (
                      <div className="node-caps-wrap">
                        {(hasAgentPerm || n.maxPermissionMode || activeCount > 0 || n.reconnecting) && (
                          <div className="node-tags-row">
                            {hasAgentPerm && <span className="task-pill device">{t('nodeCapAgent')}</span>}
                            {n.maxPermissionMode && (
                              <span className="mp">
                                {t('nodeMaxPerm', {
                                  mode:
                                    translate(lang, `permMode_${n.maxPermissionMode}`) ===
                                    `permMode_${n.maxPermissionMode}`
                                      ? n.maxPermissionMode
                                      : t(`permMode_${n.maxPermissionMode}`),
                                })}
                              </span>
                            )}
                            {activeCount > 0 && (
                              <span className="mp ok">{t('nodeActiveTasks', { n: activeCount })}</span>
                            )}
                            {n.reconnecting && <span className="mp">{t('nodeReconnecting')}</span>}
                          </div>
                        )}
                        {installedHarnesses.length > 0 && (
                          <div className="node-tags-row">
                            <span className="node-tags-lbl">{t('nodeHarnessesLbl')}</span>
                            {installedHarnesses.map((h) => (
                              <span
                                key={h.kind}
                                className="node-tag-mono"
                                title={h.authDetail || undefined}
                              >
                                {h.label || h.kind}
                                {h.authStatus === 'needs_login'
                                  ? ` · ${t('nodeHarnessNeedsLogin')}`
                                  : h.authStatus === 'ready'
                                    ? ` · ${t('nodeHarnessReady')}`
                                    : ''}
                              </span>
                            ))}
                          </div>
                        )}
                        {workspaces.length > 0 && (
                          <div className="node-tags-row">
                            <span className="node-tags-lbl">{t('nodeWorkspacesLbl')}</span>
                            {workspaces.map((ws) => (
                              <span key={ws.id || ws.path} className="node-tag-mono" title={ws.path}>
                                {ws.name || ws.path}
                                {ws.isGit && ws.defaultBranch ? ` (${ws.defaultBranch})` : ''}
                              </span>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                  <div className="row-inline-sm">
                    {isOutdated && (
                      <span className="mp bad" title={t('nodeOutdatedHint')}>
                        {t('nodeOutdatedTag')}
                      </span>
                    )}
                    {canDesk && (
                      <button type="button" className="mp" onClick={() => onOpenDesktopTarget(n.id)}>
                        {t('openDesktop')}
                      </button>
                    )}
                    <span className={`mp${n.connected ? ' ok' : ''}`}>{n.connected ? t('online') : t('offline')}</span>
                  </div>
                </div>
              );
            })
          )}
        </div>
      </section>

      {remoteErr && (
        <div id="remoteErr" className="msg-err">
          {remoteErr}
        </div>
      )}
    </div>
  );
};
