import React, { useEffect, useRef, useState } from 'react';
import type {
  ActiveDesktopTarget,
  DesktopFrame,
  DesktopHostStatus,
  DesktopTarget,
  DesktopWindow,
  ResolvedLang,
  ScreenState,
  TakeoverRequest,
} from '../types';
import { api } from '../lib/api';
import { translate } from '../lib/i18n';
import { useDesktopInput } from './desktop/useDesktopInput';
import { useDesktopStream } from './desktop/useDesktopStream';
import { isLocalTarget, type DesktopMode } from './desktop/wsH264Client';

interface DesktopStageProps {
  open: boolean;
  target: string;
  activeDeskTarget?: ActiveDesktopTarget;
  screenUpdateSeq: number;
  liveScreen: ScreenState | null;
  takeovers: TakeoverRequest[];
  chatOpen: boolean;
  embeddedInStage?: boolean;
  activeBotName?: string;
  lang: ResolvedLang;
  supportsWS?: boolean;
  supportsHEVC?: boolean;
  onUnsupportedCap?: (cap: string) => void;
  onChangeTarget: (target: string) => void;
  onActiveTargetSync?: (active: ActiveDesktopTarget) => void;
  onTakeoversChange: (items: TakeoverRequest[]) => void;
  onResolveTakeover: (to: TakeoverRequest) => Promise<void>;
  onToggleChat: () => void;
  onOpenPermissions?: () => void;
  onClose: () => void;
}

export const DesktopStage: React.FC<DesktopStageProps> = ({
  open,
  target,
  activeDeskTarget,
  screenUpdateSeq,
  liveScreen,
  takeovers,
  chatOpen,
  embeddedInStage = false,
  activeBotName,
  lang,
  supportsWS = true,
  supportsHEVC = true,
  onUnsupportedCap,
  onChangeTarget,
  onActiveTargetSync,
  onTakeoversChange,
  onResolveTakeover,
  onToggleChat,
  onOpenPermissions,
  onClose,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [mode, setMode] = useState<DesktopMode>('idle');
  const [centerText, setCenterText] = useState('');
  const [showRetry, setShowRetry] = useState(false);
  const [showCenterMsg, setShowCenterMsg] = useState(false);
  const [targets, setTargets] = useState<DesktopTarget[]>([]);
  const [windows, setWindows] = useState<DesktopWindow[]>([]);
  const [selectedWindowId, setSelectedWindowId] = useState('');
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [zoom1, setZoom1] = useState(false);
  const [typePopOpen, setTypePopOpen] = useState(false);
  const [typeDraft, setTypeDraft] = useState('');
  const [fps, setFps] = useState(0);
  const [rtt, setRtt] = useState(0);
  const [resLabel, setResLabel] = useState('—');
  const [activePreset, setActivePreset] = useState('');
  const [bitrateKbps, setBitrateKbps] = useState(0);
  const [screen, setScreen] = useState<ScreenState>({ target: 'local', owner: 'idle' });
  const [hostStatus, setHostStatus] = useState<DesktopHostStatus | null>(null);
  const [setupBusy, setSetupBusy] = useState(false);
  const [installCmdHint, setInstallCmdHint] = useState('');

  const stageRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const typeInRef = useRef<HTMLInputElement>(null);

  const pcRef = useRef<RTCPeerConnection | null>(null);
  const dcRef = useRef<RTCDataChannel | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const decoderRef = useRef<VideoDecoder | null>(null);
  const sessionRef = useRef('');
  const statsTimerRef = useRef<number | null>(null);
  const hostStatusRef = useRef<DesktopHostStatus | null>(null);
  const frameRef = useRef<DesktopFrame | null>(null);
  const modeRef = useRef(mode);
  const openRef = useRef(open);
  const targetRef = useRef(target);
  const selectedWindowIdRef = useRef(selectedWindowId);
  const windowsRef = useRef(windows);
  const lastMoveRef = useRef(0);
  const wheelAccRef = useRef({ dx: 0, dy: 0 });
  const wheelTimerRef = useRef<number | null>(null);
  const frameTimerRef = useRef<number | null>(null);
  const frameAbortRef = useRef<AbortController | null>(null);
  const streamGenRef = useRef(0);

  modeRef.current = mode;
  openRef.current = open;
  targetRef.current = target;
  selectedWindowIdRef.current = selectedWindowId;
  windowsRef.current = windows;

  useEffect(() => {
    if (liveScreen && open && liveScreen.target === target) {
      setScreen(liveScreen);
    }
  }, [liveScreen, open, screenUpdateSeq, target]);

  const { stopStreams, refreshWindows, startStream } = useDesktopStream({
    open,
    target,
    supportsWS,
    supportsHEVC,
    openRef,
    targetRef,
    selectedWindowIdRef,
    modeRef,
    streamGenRef,
    statsTimerRef,
    frameTimerRef,
    frameAbortRef,
    wheelTimerRef,
    wheelAccRef,
    wsRef,
    decoderRef,
    dcRef,
    pcRef,
    sessionRef,
    videoRef,
    canvasRef,
    frameRef,
    hostStatusRef,
    setMode,
    setCenterText,
    setShowRetry,
    setShowCenterMsg,
    setTargets,
    setWindows,
    setFps,
    setRtt,
    setResLabel,
    setActivePreset,
    setBitrateKbps,
    setScreen,
    setHostStatus,
    setInstallCmdHint,
    onUnsupportedCap,
    onChangeTarget,
    onActiveTargetSync,
    onTakeoversChange,
    t,
  });

  const { sendInput, mediaHandlers } = useDesktopInput({
    mode,
    openRef,
    modeRef,
    targetRef,
    selectedWindowIdRef,
    windowsRef,
    dcRef,
    wsRef,
    videoRef,
    canvasRef,
    frameRef,
    hostStatusRef,
    lastMoveRef,
    wheelAccRef,
    wheelTimerRef,
  });

  const activeTakeover = takeovers.find((to) => to.target === target) || takeovers[0] || null;
  const dotClass =
    mode === 'webrtc' || mode === 'ws-h264' || mode === 'frame'
      ? 'd-dot ok'
      : mode === 'error'
        ? 'd-dot err'
        : 'd-dot';
  const statusLabel =
    mode === 'webrtc'
      ? `WebRTC · ${fps || 0} fps`
      : mode === 'ws-h264'
        ? t('deskWsH264Label', { fps: fps || 0 })
        : mode === 'frame'
          ? t('deskFrameModeLabel', { fps: fps || 0 })
          : mode === 'sleeping'
            ? t('deskSleeping')
            : mode === 'error'
              ? t('deskBlocked')
              : t('deskConnectingShort');

  const submitText = () => {
    if (!typeDraft) return;
    sendInput({ kind: 'text', text: typeDraft });
    setTypeDraft('');
  };

  return (
    <section id="deskPane" className="desk-pane" aria-label={t('remoteDesktop')} hidden={!open}>
      <div ref={stageRef} id="deskStage" className="desk-stage">
        <div id="deskViewport" className={`desk-viewport${zoom1 ? ' zoom1' : ''}`}>
          <video
            ref={videoRef}
            id="deskVideo"
            className="desk-media"
            autoPlay
            muted
            playsInline
            tabIndex={0}
            data-desk-stage=""
            hidden={mode !== 'webrtc'}
            {...mediaHandlers}
          />
          <canvas
            ref={canvasRef}
            id="deskCanvas"
            className="desk-media"
            tabIndex={0}
            data-desk-stage=""
            hidden={mode !== 'ws-h264' && mode !== 'frame'}
            {...mediaHandlers}
          />
        </div>

        <div id="deskCenterMsg" className="d-center-msg" hidden={!showCenterMsg}>
          <div id="deskCenterText">{centerText}</div>
          {hostStatus?.installable && hostStatus?.supported === false && (
            <div className="desk-setup-box">
              <button
                type="button"
                id="deskSetupBtn"
                className="btn go"
                disabled={setupBusy}
                onClick={async () => {
                  setSetupBusy(true);
                  setCenterText(t('deskInstalling'));
                  try {
                    await api('/api/desktop/setup', { method: 'POST', body: { target: targetRef.current } });
                    void startStream();
                  } catch (err) {
                    setCenterText(err instanceof Error ? err.message : t('deskConnectFailed'));
                  } finally {
                    setSetupBusy(false);
                  }
                }}
              >
                {setupBusy ? t('deskInstalling') : t('deskSetupBtn')}
              </button>
              {installCmdHint && (
                <code className="desk-setup-cmd">
                  {installCmdHint}
                </code>
              )}
            </div>
          )}
          <div className="row-inline-sm row-wrap">
            {hostStatus?.screenRecording === 'denied' && onOpenPermissions && (
              <button
                type="button"
                id="deskGoPermsBtn"
                className="btn go"
                onClick={onOpenPermissions}
              >
                {t('deskGoPermissions')}
              </button>
            )}
            <button type="button" id="deskRetryBtn" className="btn" hidden={!showRetry} onClick={() => void startStream()}>
              {t('retryConnect')}
            </button>
          </div>
        </div>

        <div className="d-top-stack">
          <div className="d-top-row">
            <div className="d-pill tl">
            <div className="d-tl-row">
              <button
                type="button"
                id="deskStatusBtn"
                className="d-status-btn"
                title={t('deskToggleDetails')}
                onClick={() => setDetailsOpen((v) => !v)}
              >
                <span id="deskStatusDot" className={dotClass} />
                <span id="deskStatusLabel">{statusLabel}</span>
              </button>
              <select
                id="deskTarget"
                className="d-sel"
                aria-label={t('deskTarget')}
                value={target}
                onChange={(e) => {
                  setSelectedWindowId('');
                  onChangeTarget(e.target.value);
                }}
              >
                {targets.map((tr) => (
                  <option key={tr.id} value={tr.id}>
                    {tr.id === 'local' ? t('localDesktopName') : tr.name}
                  </option>
                ))}
                {target && !targets.some((tr) => tr.id === target) && (
                  <option key={target} value={target}>
                    {target}
                  </option>
                )}
              </select>
              <select
                id="deskWindow"
                className="d-sel"
                aria-label={t('deskWindow')}
                value={selectedWindowId}
                onChange={(e) => {
                  const wid = e.target.value;
                  setSelectedWindowId(wid);
                  const app = windows.find((w) => w.id === wid)?.app || '';
                  if (app) {
                    api('/api/desktop/focus', {
                      method: 'POST',
                      body: { target, app, windowId: wid },
                    }).catch(() => {});
                  }
                  if (mode === 'webrtc') void startStream();
                }}
              >
                <option value="">{t('entireScreen')}</option>
                {windows.map((w) => (
                  <option key={w.id} value={w.id}>
                    {(w.app || '') + (w.title ? `: ${w.title.slice(0, 22)}` : '')}
                  </option>
                ))}
              </select>
            </div>
            <dl id="deskDetails" className="d-details" hidden={!detailsOpen}>
              <dt>{t('deskMode')}</dt>
              <dd id="ddMode">
                {mode === 'webrtc'
                  ? 'WebRTC H.264'
                  : mode === 'ws-h264'
                    ? `${t('deskWsH264Mode')}${activePreset ? ` (${activePreset}${bitrateKbps ? ` · ${bitrateKbps}k` : ''})` : ''}`
                    : mode === 'frame'
                      ? t('deskFrameModeShort')
                      : mode}
              </dd>
              <dt>{t('deskLatency')}</dt>
              <dd id="ddRtt">{rtt ? `${rtt} ms` : '—'}</dd>
              <dt>{t('deskResolution')}</dt>
              <dd id="ddRes">{resLabel || '—'}</dd>
              <dt>{t('deskLockState')}</dt>
              <dd id="ddLock">{screen?.owner === 'human' ? t('deskHumanCtrl') : t('idle')}</dd>
              {hostStatus?.virtual && (
                <>
                  <dt>{t('deskPowerState')}</dt>
                  <dd id="ddPower">
                    {hostStatus.powerState === 'frozen'
                      ? t('deskPowerFrozen')
                      : hostStatus.powerState === 'stopped'
                        ? t('deskPowerStopped')
                        : t('deskPowerRunning')}
                  </dd>
                </>
              )}
            </dl>
          </div>

          {!embeddedInStage && (
          <div className="d-pill tr">
            <div className="d-tools">
              <button
                type="button"
                id="deskChatBtn"
                className={`d-btn d-chat-btn${chatOpen ? ' on' : ''}`}
                title={t('deskChatSidebarTitle')}
                onClick={onToggleChat}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                </svg>
                <span>{activeBotName ? activeBotName : t('deskChatSidebar')}</span>
              </button>
              <button
                type="button"
                id="deskCloseTopBtn"
                className="d-btn"
                title={t('closeDesktop')}
                onClick={onClose}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                  <path d="M6 6l12 12M18 6 6 18" />
                </svg>
              </button>
            </div>
          </div>
          )}
        </div>

          {activeDeskTarget?.temporary && (
            <div className="d-pill top-alert temp-target">
              <span className="text-wrap-word">
                {t('tempDesktopBanner', {
                  name: activeDeskTarget.name || activeDeskTarget.target,
                })}
              </span>
              <button type="button" className="btn" onClick={() => onChangeTarget('local')}>
                {t('switchBackLocal')}
              </button>
            </div>
          )}

          {activeTakeover && (
            <div id="deskAlert" className="d-pill top-alert takeover">
              <span id="deskAlertText" className="text-wrap-word">
                {t('deskAlertTakeover', { reason: activeTakeover.reason })}
              </span>
              <button type="button" id="deskAlertBtn" className="btn go" onClick={() => void onResolveTakeover(activeTakeover)}>
                {t('resumeAI')}
              </button>
            </div>
          )}
        </div>

        <div className="d-pill br">
          <div id="deskTypePop" className="d-type-pop" hidden={!typePopOpen}>
            <input
              ref={typeInRef}
              id="deskTypeIn"
              placeholder={t('deskTypePh')}
              autoComplete="off"
              value={typeDraft}
              onChange={(e) => setTypeDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                  e.preventDefault();
                  submitText();
                }
              }}
            />
            <button type="button" id="deskTypeSend" className="d-btn on" onClick={submitText}>
              {t('send')}
            </button>
          </div>
          <div className="d-tools">
            <button
              type="button"
              id="deskTypeBtn"
              className={`d-btn${typePopOpen ? ' on' : ''}`}
              title={t('textInput')}
              onClick={() => {
                setTypePopOpen((v) => {
                  const next = !v;
                  if (next) setTimeout(() => typeInRef.current?.focus(), 0);
                  return next;
                });
              }}
            >
              Aa
            </button>
            <button
              type="button"
              id="deskZoomBtn"
              className={`d-btn${zoom1 ? ' on' : ''}`}
              title={t('toggleZoom')}
              onClick={() => setZoom1((v) => !v)}
            >
              {zoom1 ? '1:1' : t('fitZoom')}
            </button>
            {hostStatus?.virtual && (
              <button
                type="button"
                id="deskPowerBtn"
                className="d-btn"
                title={hostStatus.powerState === 'running' ? t('deskSleepBtn') : t('deskWakeBtn')}
                onClick={async () => {
                  const act = hostStatus.powerState === 'running' ? 'freeze' : 'wake';
                  try {
                    const res = await api<{ status?: DesktopHostStatus }>('/api/desktop/power', {
                      method: 'POST',
                      body: { target: targetRef.current, action: act },
                    });
                    if (res.status) {
                      hostStatusRef.current = res.status;
                      setHostStatus(res.status);
                    }
                    if (act === 'freeze') {
                      stopStreams();
                      setMode('sleeping');
                      setCenterText(t('deskSleeping'));
                      setShowRetry(false);
                      setShowCenterMsg(true);
                    } else {
                      void startStream();
                    }
                  } catch {}
                }}
              >
                {hostStatus.powerState === 'running' ? t('deskSleepBtn') : t('deskWakeBtn')}
              </button>
            )}
            <button
              type="button"
              id="deskRefreshBtn"
              className="d-btn"
              title={t('refreshStream')}
              onClick={() => {
                void refreshWindows();
                void startStream();
              }}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <path d="M21 12a9 9 0 1 1-2.64-6.36" />
                <path d="M21 3v6h-6" />
              </svg>
            </button>
            <button
              type="button"
              id="deskFsBtn"
              className="d-btn"
              title={t('fullscreen')}
              onClick={() => {
                const el = stageRef.current;
                if (!el) return;
                if (document.fullscreenElement === el) document.exitFullscreen?.();
                else el.requestFullscreen?.();
              }}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" />
              </svg>
            </button>
            <button type="button" id="deskCloseBtn" className="d-btn" title={t('closeDesktop')} onClick={onClose}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <path d="M6 6l12 12M18 6 6 18" />
              </svg>
            </button>
          </div>
        </div>
      </div>
    </section>
  );
};
