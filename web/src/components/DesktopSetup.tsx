import React, { useCallback, useEffect, useRef, useState } from 'react';
import { resolveLang, translate } from '../lib/i18n';
import { applyThemePref, getThemePref } from '../lib/theme';

interface DesktopHarnessItem {
  id: string;
  label: string;
  installed: boolean;
}

interface DesktopStateResponse {
  mode?: string;
  link?: string;
  status?: string;
  dataDir?: string;
  harnesses?: DesktopHarnessItem[];
}

interface WailsWebKitBridge {
  messageHandlers?: {
    external?: {
      postMessage: (msg: string) => void;
    };
  };
}

export default function DesktopSetup() {
  const [lang] = useState(() => resolveLang());
  const t = useCallback(
    (key: string, vars?: Record<string, string | number>) => translate(lang, key, vars),
    [lang],
  );

  const [dataDir, setDataDir] = useState('');
  const [harnesses, setHarnesses] = useState<DesktopHarnessItem[] | null>(null);
  const [link, setLink] = useState('');
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);

  const linkTouchedRef = useRef(false);
  const pollTimerRef = useRef<number | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    applyThemePref(getThemePref());
    document.documentElement.lang = lang;
    return () => {
      mountedRef.current = false;
      if (pollTimerRef.current !== null) {
        window.clearTimeout(pollTimerRef.current);
      }
    };
  }, [lang]);

  const refresh = useCallback(async (): Promise<boolean> => {
    const res = await fetch('/api/desktop');
    const s = (await res.json()) as DesktopStateResponse;
    if (!mountedRef.current) return false;
    setDataDir(s.dataDir || '');
    setHarnesses(Array.isArray(s.harnesses) ? s.harnesses : []);
    if (!linkTouchedRef.current && s.link) {
      setLink((prev) => prev || s.link || '');
    }
    const st = s.status || '';
    setStatus(st);
    const isBusy = st === '正在启动…' || st === 'Starting…';
    setBusy(isBusy);
    return isBusy;
  }, []);

  useEffect(() => {
    refresh().catch(() => {});
  }, [refresh]);

  const schedulePoll = useCallback(() => {
    if (pollTimerRef.current !== null) {
      window.clearTimeout(pollTimerRef.current);
    }
    pollTimerRef.current = window.setTimeout(async () => {
      try {
        const stillBusy = await refresh();
        if (stillBusy && mountedRef.current) {
          schedulePoll();
        }
      } catch {
        if (mountedRef.current) {
          setBusy(false);
        }
      }
    }, 700);
  }, [refresh]);

  const start = async (body: { mode: 'local' | 'remote'; link?: string }) => {
    setStatus(t('desktopSetupStarting'));
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
        setBusy(false);
      }
    }
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

  const installed = harnesses ? harnesses.filter((h) => h.installed) : [];
  const harnessSummary =
    harnesses === null
      ? t('desktopSetupDetecting')
      : installed.length > 0
        ? t('desktopSetupDetectedPrefix') + installed.map((h) => h.label).join(lang === 'en' ? ', ' : '、')
        : t('desktopSetupNoneDetected');

  const trimmedLink = link.trim();
  const hasToken = trimmedLink.includes('#token=') || trimmedLink.includes('token=');
  const isError = status.startsWith('启动失败') || status.startsWith('Start failed');

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
          <div className="dsetup-meta">
            <span className={`dsetup-meta-lbl${installed.length > 0 ? ' ok' : ''}`}>{harnessSummary}</span>
            <button type="button" className="mp" onClick={() => refresh().catch(() => {})}>
              {t('desktopSetupRecheck')}
            </button>
          </div>
          <div className="dsetup-actions">
            <button
              type="button"
              className="btn go"
              disabled={busy}
              onClick={() => start({ mode: 'local' })}
            >
              {t('desktopSetupLocalBtn')}
            </button>
          </div>
        </section>

        <section className="dsetup-card">
          <div className="dsetup-card-hd">
            <h2>{t('desktopSetupRemoteTitle')}</h2>
          </div>
          <p className="dsetup-desc">{t('desktopSetupRemoteDesc')}</p>
          <textarea
            className="dsetup-textarea"
            value={link}
            onChange={(e) => {
              linkTouchedRef.current = true;
              setLink(e.target.value);
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
              disabled={busy}
              onClick={() => start({ mode: 'remote', link: trimmedLink })}
            >
              {t('desktopSetupRemoteBtn')}
            </button>
          </div>
        </section>

        <p className={`dsetup-status${isError ? ' warn' : ''}`}>{status}</p>
      </main>
    </div>
  );
}
