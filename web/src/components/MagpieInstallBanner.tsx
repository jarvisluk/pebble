import React, { useEffect, useRef, useState } from 'react';
import type { ResolvedLang, SetupTaskStatus } from '../types';
import { api } from '../lib/api';
import { translate } from '../lib/i18n';

interface MagpieInstallBannerProps {
  lang: ResolvedLang;
  hint?: string;
  installCmds?: { script?: string; brew?: string };
  onInstalled: () => Promise<void> | void;
  onToast?: (msg: string) => void;
}

export const MagpieInstallBanner: React.FC<MagpieInstallBannerProps> = ({
  lang,
  hint,
  installCmds,
  onInstalled,
  onToast,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [task, setTask] = useState<SetupTaskStatus | null>(null);
  const [showManual, setShowManual] = useState(false);
  const [copiedTag, setCopiedTag] = useState('');
  const [err, setErr] = useState('');
  const timerRef = useRef<number | null>(null);

  const stopPoll = () => {
    if (timerRef.current) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  const pollStatus = async () => {
    try {
      const st = await api<SetupTaskStatus>('/api/magpie/install');
      setTask(st);
      if (st.state === 'running') {
        stopPoll();
        timerRef.current = window.setTimeout(() => void pollStatus(), 1200);
        return;
      }
      if (st.state === 'done') {
        onToast?.(t('ubMagpieInstallDone'));
        await onInstalled();
      } else if (st.state === 'error' && st.error) {
        setErr(st.error);
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  useEffect(() => {
    void (async () => {
      try {
        const st = await api<SetupTaskStatus>('/api/magpie/install');
        if (st.state === 'running') {
          setTask(st);
          timerRef.current = window.setTimeout(() => void pollStatus(), 1200);
        }
      } catch {}
    })();
    return stopPoll;
  }, []);

  const startInstall = async () => {
    setErr('');
    try {
      const st = await api<SetupTaskStatus>('/api/magpie/install', { method: 'POST' });
      setTask(st);
      if (st.state === 'running') {
        stopPoll();
        timerRef.current = window.setTimeout(() => void pollStatus(), 1200);
      } else if (st.state === 'done') {
        onToast?.(t('ubMagpieInstallDone'));
        await onInstalled();
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    }
  };

  const copyCmd = (cmd: string, tag: string) => {
    navigator.clipboard?.writeText(cmd).then(() => {
      setCopiedTag(tag);
      setTimeout(() => setCopiedTag((c) => (c === tag ? '' : c)), 1200);
    });
  };

  const running = task?.state === 'running';
  const lastLog = task?.logs?.length ? task.logs[task.logs.length - 1] : '';

  return (
    <div className="set-stack">
      {hint && <div>{hint}</div>}
      <div className="row-inline row-wrap">
        <button type="button" className="mp go" disabled={running} onClick={() => void startInstall()}>
          {running ? t('ubMagpieInstalling') : t('ubMagpieOneClick')}
        </button>
        {(installCmds?.script || installCmds?.brew) && (
          <button type="button" className="mp" onClick={() => setShowManual((v) => !v)}>
            {showManual ? t('ubHideInstall') : t('ubMagpieManualToggle')}
          </button>
        )}
      </div>
      {running && lastLog && <div className="unbox-item-path">{lastLog}</div>}
      {err && <div className="msg-err">{err}</div>}
      {showManual && (
        <div className="unbox-install">
          {installCmds?.script && (
            <div className="cmd">
              <code>{installCmds.script}</code>
              <button type="button" onClick={() => copyCmd(installCmds.script!, 'mp-script')}>
                {copiedTag === 'mp-script' ? t('copied') : t('copy')}
              </button>
            </div>
          )}
          {installCmds?.brew && (
            <div className="cmd">
              <code>{installCmds.brew}</code>
              <button type="button" onClick={() => copyCmd(installCmds.brew!, 'mp-brew')}>
                {copiedTag === 'mp-brew' ? t('copied') : t('copy')}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
