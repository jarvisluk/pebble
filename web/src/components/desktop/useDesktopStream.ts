import { useCallback, useEffect } from 'react';
import type React from 'react';
import type {
  ActiveDesktopTarget,
  DesktopFrame,
  DesktopHostStatus,
  DesktopTarget,
  DesktopWindow,
  ScreenState,
  TakeoverRequest,
} from '../../types';
import { api, APIError, isUnsupportedCapabilityError } from '../../lib/api';
import { connectWsH264Stream, isLocalTarget, type DesktopMode } from './wsH264Client';

export interface UseDesktopStreamOpts {
  open: boolean;
  target: string;
  supportsWS: boolean;
  supportsHEVC: boolean;
  openRef: React.MutableRefObject<boolean>;
  targetRef: React.MutableRefObject<string>;
  selectedWindowIdRef: React.MutableRefObject<string>;
  modeRef: React.MutableRefObject<DesktopMode>;
  streamGenRef: React.MutableRefObject<number>;
  statsTimerRef: React.MutableRefObject<number | null>;
  frameTimerRef: React.MutableRefObject<number | null>;
  frameAbortRef: React.MutableRefObject<AbortController | null>;
  wheelTimerRef: React.MutableRefObject<number | null>;
  wheelAccRef: React.MutableRefObject<{ dx: number; dy: number }>;
  wsRef: React.MutableRefObject<WebSocket | null>;
  decoderRef: React.MutableRefObject<VideoDecoder | null>;
  dcRef: React.MutableRefObject<RTCDataChannel | null>;
  pcRef: React.MutableRefObject<RTCPeerConnection | null>;
  sessionRef: React.MutableRefObject<string>;
  videoRef: React.RefObject<HTMLVideoElement | null>;
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  frameRef: React.MutableRefObject<DesktopFrame | null>;
  hostStatusRef: React.MutableRefObject<DesktopHostStatus | null>;
  setMode: React.Dispatch<React.SetStateAction<DesktopMode>>;
  setCenterText: (s: string) => void;
  setShowRetry: (v: boolean) => void;
  setShowCenterMsg: (v: boolean) => void;
  setTargets: (items: DesktopTarget[]) => void;
  setWindows: (items: DesktopWindow[]) => void;
  setFps: React.Dispatch<React.SetStateAction<number>>;
  setRtt: (v: number) => void;
  setResLabel: (s: string) => void;
  setActivePreset: (s: string) => void;
  setBitrateKbps: (v: number) => void;
  setScreen: (s: ScreenState) => void;
  setHostStatus: (s: DesktopHostStatus | null) => void;
  setInstallCmdHint: (s: string) => void;
  onUnsupportedCap?: (cap: string) => void;
  onChangeTarget: (target: string) => void;
  onActiveTargetSync?: (active: ActiveDesktopTarget) => void;
  onTakeoversChange: (items: TakeoverRequest[]) => void;
  t: (k: string, vars?: Record<string, string | number>) => string;
}

export function useDesktopStream(opts: UseDesktopStreamOpts) {
  const {
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
  } = opts;

  const stopStreams = useCallback(() => {
    streamGenRef.current += 1;
    if (statsTimerRef.current) {
      window.clearInterval(statsTimerRef.current);
      statsTimerRef.current = null;
    }
    if (frameTimerRef.current) {
      window.clearTimeout(frameTimerRef.current);
      frameTimerRef.current = null;
    }
    if (frameAbortRef.current) {
      try {
        frameAbortRef.current.abort();
      } catch {}
      frameAbortRef.current = null;
    }
    if (wheelTimerRef.current) {
      window.clearTimeout(wheelTimerRef.current);
      wheelTimerRef.current = null;
      wheelAccRef.current = { dx: 0, dy: 0 };
    }
    if (wsRef.current) {
      try {
        wsRef.current.onclose = null;
        wsRef.current.onerror = null;
        wsRef.current.onmessage = null;
        wsRef.current.close();
      } catch {}
      wsRef.current = null;
    }
    if (decoderRef.current) {
      try {
        if (decoderRef.current.state !== 'closed') decoderRef.current.close();
      } catch {}
      decoderRef.current = null;
    }
    if (dcRef.current) {
      try {
        dcRef.current.close();
      } catch {}
      dcRef.current = null;
    }
    if (pcRef.current) {
      try {
        pcRef.current.close();
      } catch {}
      pcRef.current = null;
    }
    if (sessionRef.current) {
      const sid = sessionRef.current;
      sessionRef.current = '';
      api('/api/desktop/stream/close', {
        method: 'POST',
        body: { target: targetRef.current, session: sid },
      }).catch(() => {});
    }
    if (videoRef.current) {
      videoRef.current.srcObject = null;
    }
  }, []);

  const refreshTargets = useCallback(async () => {
    try {
      const d = await api<{
        targets?: DesktopTarget[];
        activeTarget?: ActiveDesktopTarget;
        takeovers?: TakeoverRequest[];
      }>('/api/desktop/targets');
      const list = d.targets || [];
      setTargets(list);
      if (d.activeTarget && onActiveTargetSync) {
        onActiveTargetSync(d.activeTarget);
      } else if (list.length > 0 && !list.some((tr) => tr.id === targetRef.current)) {
        onChangeTarget(list[0].id);
      }
      if (d.takeovers) onTakeoversChange(d.takeovers);
    } catch {}
  }, [onActiveTargetSync, onChangeTarget, onTakeoversChange]);

  const refreshWindows = useCallback(async () => {
    try {
      const d = await api<{ windows?: DesktopWindow[] }>(
        `/api/desktop/windows?target=${encodeURIComponent(targetRef.current)}`
      );
      setWindows(d.windows || []);
    } catch {
      setWindows([]);
    }
  }, []);

  const startFramePolling = useCallback(() => {
    if (!openRef.current) return;
    stopStreams();
    const gen = ++streamGenRef.current;
    setActivePreset('');
    setBitrateKbps(0);
    setMode('connecting');
    setCenterText(t('deskConnecting'));
    setShowRetry(false);
    setShowCenterMsg(true);

    let hadFrame = false;
    let frameCount = 0;
    let consecErrors = 0;

    statsTimerRef.current = window.setInterval(() => {
      if (gen !== streamGenRef.current || (document.hidden && !document.hasFocus())) return;
      const next = frameCount;
      frameCount = 0;
      setFps((prev) => (prev === next ? prev : next));
    }, 1000);

    const drawFrame = (fr: DesktopFrame): Promise<void> =>
      new Promise((resolve, reject) => {
        if (!fr.jpegBase64) {
          resolve();
          return;
        }
        const img = new Image();
        img.onload = () => {
          if (!openRef.current || gen !== streamGenRef.current) {
            resolve();
            return;
          }
          const w = img.naturalWidth || fr.imageWidth || 1280;
          const h = img.naturalHeight || fr.imageHeight || 720;
          const canvas = canvasRef.current;
          if (canvas) {
            if (canvas.width !== w || canvas.height !== h) {
              canvas.width = w;
              canvas.height = h;
            }
            const ctx = canvas.getContext('2d');
            ctx?.drawImage(img, 0, 0, canvas.width, canvas.height);
          }
          frameRef.current = {
            ...fr,
            imageWidth: w,
            imageHeight: h,
            logicalX: fr.logicalX || 0,
            logicalY: fr.logicalY || 0,
            logicalWidth: fr.logicalWidth || 1440,
            logicalHeight: fr.logicalHeight || 900,
          };
          setResLabel(`${w}×${h}`);
          resolve();
        };
        img.onerror = () => reject(new Error('frame decode failed'));
        img.src = `data:image/jpeg;base64,${fr.jpegBase64}`;
      });

    const tick = async () => {
      if (!openRef.current || gen !== streamGenRef.current) return;
      if (hadFrame && document.hidden && !document.hasFocus()) {
        frameTimerRef.current = window.setTimeout(() => void tick(), 1000);
        return;
      }
      const ctrl = new AbortController();
      frameAbortRef.current = ctrl;
      const t0 = performance.now();
      let failed = false;
      try {
        const qs = `target=${encodeURIComponent(targetRef.current)}&windowId=${encodeURIComponent(
          selectedWindowIdRef.current
        )}&maxDim=1280&quality=72`;
        const fr = await api<DesktopFrame>(`/api/desktop/frame?${qs}`, { signal: ctrl.signal });
        if (!openRef.current || gen !== streamGenRef.current) return;
        await drawFrame(fr);
        if (!openRef.current || gen !== streamGenRef.current) return;
        consecErrors = 0;
        frameCount += 1;
        hadFrame = true;
        setRtt(Math.round(performance.now() - t0));
        setMode('frame');
        setShowCenterMsg(false);
      } catch (err) {
        if (!openRef.current || gen !== streamGenRef.current) return;
        if (err instanceof DOMException && err.name === 'AbortError') return;
        if (err instanceof Error && err.name === 'AbortError') return;
        failed = true;
        consecErrors += 1;
        if (err instanceof APIError && err.code === 'screen_recording_denied') {
          setMode('error');
          setCenterText(t('deskScreenRecordingDenied'));
          setShowRetry(true);
          setShowCenterMsg(true);
          return;
        }
        if (!hadFrame && consecErrors >= 3) {
          setMode('error');
          setCenterText(err instanceof Error ? err.message : t('deskConnectFailed'));
          setShowRetry(true);
          setShowCenterMsg(true);
        }
      } finally {
        if (frameAbortRef.current === ctrl) frameAbortRef.current = null;
      }
      if (!openRef.current || gen !== streamGenRef.current) return;
      const elapsed = performance.now() - t0;
      const delay = failed ? 1500 : Math.max(120, Math.round(350 - elapsed));
      frameTimerRef.current = window.setTimeout(() => void tick(), delay);
    };

    void tick();
  }, [stopStreams, t]);

  const startWsH264Stream = useCallback(
    async (allowHevcFallback = true) => {
      if (!openRef.current) return;
      if (!isLocalTarget(targetRef.current) || !supportsWS || typeof VideoDecoder === 'undefined') {
        startFramePolling();
        return;
      }
      await connectWsH264Stream({
        allowHevcFallback,
        supportsHEVC,
        openRef,
        targetRef,
        streamGenRef,
        wsRef,
        decoderRef,
        statsTimerRef,
        canvasRef,
        frameRef,
        modeRef,
        stopStreams,
        startFramePolling,
        restartWithH264: () => {
          void startWsH264Stream(false);
        },
        onUnsupportedCap,
        setMode,
        setCenterText,
        setShowRetry,
        setShowCenterMsg,
        setFps,
        setResLabel,
        setActivePreset,
        setBitrateKbps,
        t,
      });
    },
    [onUnsupportedCap, startFramePolling, stopStreams, supportsHEVC, supportsWS, t]
  );

  const startStream = useCallback(async () => {
    stopStreams();
    if (!openRef.current) return;
    const gen = ++streamGenRef.current;
    setMode('connecting');
    setCenterText(t('deskConnecting'));
    setShowRetry(false);
    setShowCenterMsg(true);

    try {
      const st = await api<{
        status?: DesktopHostStatus;
        screen?: ScreenState;
        takeovers?: TakeoverRequest[];
      }>(`/api/desktop/state?target=${encodeURIComponent(targetRef.current)}`);
      if (!openRef.current || gen !== streamGenRef.current) return;
      hostStatusRef.current = st.status || null;
      setHostStatus(st.status || null);
      setInstallCmdHint(st.status?.installCommand || '');
      setScreen(st.screen || { target: targetRef.current, owner: 'idle' });
      if (st.takeovers) onTakeoversChange(st.takeovers);
      if (st.status?.screenRecording === 'denied') {
        setMode('error');
        setCenterText(
          t('deskScreenRecordingDenied') +
            (st.status?.accessibility === 'denied' ? t('deskAccessibilityHint') : '')
        );
        setShowRetry(true);
        setShowCenterMsg(true);
        return;
      }
      if (st.status && st.status.supported === false && st.status.installable) {
        setMode('error');
        setCenterText(
          t('deskMissingDepsHint', { deps: (st.status.missingDeps || []).join(', ') || 'Xvfb, openbox' })
        );
        setShowRetry(true);
        setShowCenterMsg(true);
        return;
      }
      void refreshWindows();
    } catch (err) {
      if (!openRef.current || gen !== streamGenRef.current) return;
      if (isUnsupportedCapabilityError(err)) {
        startFramePolling();
        return;
      }
      setMode('error');
      setCenterText(err instanceof Error ? err.message : t('deskUnreachable'));
      setShowRetry(true);
      setShowCenterMsg(true);
      return;
    }

    if (!openRef.current || gen !== streamGenRef.current) return;
    if (!hostStatusRef.current?.webrtcAvailable || typeof RTCPeerConnection === 'undefined') {
      if (isLocalTarget(targetRef.current)) {
        void startWsH264Stream(true);
      } else {
        startFramePolling();
      }
      return;
    }

    try {
      const pc = new RTCPeerConnection();
      pcRef.current = pc;
      pc.addTransceiver('video', { direction: 'recvonly' });
      const dc = pc.createDataChannel('input', { ordered: true });
      dcRef.current = dc;

      const timer = window.setTimeout(() => {
        if (pcRef.current === pc && modeRef.current !== 'webrtc') {
          startWsH264Stream();
        }
      }, 2500);

      pc.ontrack = (e) => {
        const rx = e.receiver as RTCRtpReceiver & { playoutDelayHint?: number };
        if (rx && 'playoutDelayHint' in rx) rx.playoutDelayHint = 0;
        const vid = videoRef.current;
        if (vid) {
          vid.srcObject = e.streams[0] || new MediaStream([e.track]);
          vid.play().catch(() => {});
        }
      };
      pc.onconnectionstatechange = () => {
        if (pcRef.current !== pc) return;
        if (pc.connectionState === 'connected') {
          window.clearTimeout(timer);
          setMode('webrtc');
          setShowCenterMsg(false);
        } else if (pc.connectionState === 'failed' || pc.connectionState === 'disconnected') {
          window.clearTimeout(timer);
          startWsH264Stream();
        }
      };

      await pc.setLocalDescription(await pc.createOffer());
      if (!openRef.current || gen !== streamGenRef.current || pcRef.current !== pc) return;
      await new Promise<void>((resolve) => {
        if (pc.iceGatheringState === 'complete') {
          resolve();
          return;
        }
        const done = () => {
          pc.removeEventListener('icegatheringstatechange', onS);
          window.clearTimeout(gt);
          resolve();
        };
        const onS = () => {
          if (pc.iceGatheringState === 'complete') done();
        };
        pc.addEventListener('icegatheringstatechange', onS);
        const gt = window.setTimeout(done, 450);
      });
      const offerSdp = pc.localDescription?.sdp || '';
      if (!openRef.current || gen !== streamGenRef.current || pcRef.current !== pc) return;
      if (!/^a=candidate:/m.test(offerSdp)) {
        window.clearTimeout(timer);
        void startWsH264Stream();
        return;
      }

      const ans = await api<{ session?: string; sdp: string }>('/api/desktop/stream/offer', {
        method: 'POST',
        body: {
          target: targetRef.current,
          scope: selectedWindowIdRef.current,
          sdp: offerSdp,
          fps: 60,
          maxDim: 3840,
          bitrate: 35_000_000,
        },
      });
      if (pcRef.current !== pc || !openRef.current || gen !== streamGenRef.current) {
        if (ans.session) {
          api('/api/desktop/stream/close', {
            method: 'POST',
            body: { target: targetRef.current, session: ans.session },
          }).catch(() => {});
        }
        return;
      }
      sessionRef.current = ans.session || '';
      await pc.setRemoteDescription({ type: 'answer', sdp: ans.sdp });

      let lastFrames = 0;
      let lastPackets = 0;
      let lastLost = 0;
      statsTimerRef.current = window.setInterval(async () => {
        if (pcRef.current !== pc) return;
        const stats = await pc.getStats();
        let curRtt = 0;
        let curLossPct = 0;
        stats.forEach((r) => {
          if (r.type === 'inbound-rtp' && r.kind === 'video') {
            const v = r as RTCInboundRtpStreamStats & {
              framesDecoded?: number;
              frameWidth?: number;
              frameHeight?: number;
              packetsReceived?: number;
              packetsLost?: number;
            };
            const decoded = v.framesDecoded || 0;
            setFps(Math.max(0, decoded - lastFrames));
            lastFrames = decoded;
            if (v.frameWidth && v.frameHeight) setResLabel(`${v.frameWidth}×${v.frameHeight}`);
            const rx = v.packetsReceived || 0;
            const lost = v.packetsLost || 0;
            const dRx = Math.max(0, rx - lastPackets);
            const dLost = Math.max(0, lost - lastLost);
            lastPackets = rx;
            lastLost = lost;
            if (dRx + dLost > 0) {
              curLossPct = (dLost * 100) / (dRx + dLost);
            }
          }
          if (
            r.type === 'candidate-pair' &&
            (r as RTCIceCandidatePairStats).state === 'succeeded' &&
            typeof (r as RTCIceCandidatePairStats).currentRoundTripTime === 'number'
          ) {
            curRtt = Math.round(((r as RTCIceCandidatePairStats).currentRoundTripTime || 0) * 1000);
            setRtt(curRtt);
          }
        });
        if (dc.readyState === 'open') {
          try {
            dc.send(JSON.stringify({ type: 'stats', rttMs: curRtt, lossPct: curLossPct }));
          } catch {}
        }
      }, 1000);
    } catch {
      if (!openRef.current || gen !== streamGenRef.current) return;
      void startWsH264Stream();
    }
  }, [onTakeoversChange, refreshWindows, startFramePolling, startWsH264Stream, stopStreams, t]);

  useEffect(() => {
    if (!open) {
      stopStreams();
      setMode('idle');
      return;
    }
    let cancelled = false;
    void refreshTargets().then(() => {
      if (!cancelled && openRef.current) {
        void startStream();
      }
    });
    return () => {
      cancelled = true;
      stopStreams();
    };
  }, [open, target]);

  return {
    stopStreams,
    refreshTargets,
    refreshWindows,
    startFramePolling,
    startStream,
  };
}
