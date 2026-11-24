import React, { useCallback, useEffect, useRef, useState } from 'react';
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
import { api, APIError } from '../lib/api';
import { translate } from '../lib/i18n';

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
  onChangeTarget,
  onActiveTargetSync,
  onTakeoversChange,
  onResolveTakeover,
  onToggleChat,
  onOpenPermissions,
  onClose,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [mode, setMode] = useState<'idle' | 'connecting' | 'webrtc' | 'ws-h264' | 'error' | 'sleeping'>('idle');
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

  const stopStreams = useCallback(() => {
    streamGenRef.current += 1;
    if (statsTimerRef.current) {
      window.clearInterval(statsTimerRef.current);
      statsTimerRef.current = null;
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

  const startWsH264Stream = useCallback(async () => {
    if (!openRef.current) return;
    stopStreams();
    const gen = ++streamGenRef.current;
    if (typeof VideoDecoder === 'undefined') {
      setMode('error');
      setCenterText(t('deskNoWebCodecs'));
      setShowRetry(false);
      setShowCenterMsg(true);
      return;
    }

    let preferHevc = false;
    try {
      const sup = await VideoDecoder.isConfigSupported({
        codec: 'hev1.1.6.L153.B0',
        hardwareAcceleration: 'prefer-hardware',
      });
      preferHevc = Boolean(sup?.supported);
    } catch {}
    if (!openRef.current || gen !== streamGenRef.current) return;

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsURL = `${proto}//${window.location.host}/api/desktop/stream/ws?target=${encodeURIComponent(
      targetRef.current
    )}&codec=${preferHevc ? 'hevc' : 'h264'}`;

    const ws = new WebSocket(wsURL);
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;

    let needKeyframe = true;
    let frameCount = 0;
    let tsCounter = 0;
    let hadFrame = false;
    let streamFps = 60;
    let streamCodec: 'h264' | 'hevc' = preferHevc ? 'hevc' : 'h264';
    let curCodec = '';
    let curWidth = 0;
    let curHeight = 0;

    const requestKeyframe = (congested = false) => {
      needKeyframe = true;
      if (ws.readyState === WebSocket.OPEN) {
        try {
          ws.send(JSON.stringify({ type: 'keyframe' }));
          if (congested) {
            ws.send(JSON.stringify({ type: 'congestion' }));
          }
        } catch {}
      }
    };

    const extractAvcCodecFromAnnexB = (data: Uint8Array): string | null => {
      const n = data.length;
      for (let i = 0; i + 4 < n; i++) {
        let scLen = 0;
        if (data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 1) {
          scLen = 3;
        } else if (i + 5 < n && data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 0 && data[i + 3] === 1) {
          scLen = 4;
        }
        if (!scLen) continue;
        const start = i + scLen;
        if (start + 3 < n && (data[start] & 0x1f) === 7) {
          const hex = (b: number) => b.toString(16).padStart(2, '0').toUpperCase();
          return `avc1.${hex(data[start + 1])}${hex(data[start + 2])}${hex(data[start + 3])}`;
        }
        i = start;
      }
      return null;
    };

    const ensureDecoder = (width: number, height: number, codecStr?: string) => {
      const targetCodec =
        codecStr ||
        (streamCodec === 'hevc' ? 'hev1.1.6.L153.B0' : curCodec || 'avc1.640033');
      if (
        decoderRef.current &&
        decoderRef.current.state === 'configured' &&
        curCodec === targetCodec &&
        curWidth === width &&
        curHeight === height
      ) {
        return;
      }
      if (decoderRef.current && decoderRef.current.state !== 'closed') {
        try {
          decoderRef.current.close();
        } catch {}
      }
      const dec = new VideoDecoder({
        output: (vf) => {
          const canvas = canvasRef.current;
          if (canvas) {
            if (canvas.width !== vf.displayWidth || canvas.height !== vf.displayHeight) {
              canvas.width = vf.displayWidth;
              canvas.height = vf.displayHeight;
            }
            const ctx = canvas.getContext('2d');
            ctx?.drawImage(vf, 0, 0, canvas.width, canvas.height);
          }
          vf.close();
          frameCount++;
          if (!hadFrame) {
            hadFrame = true;
            setMode('ws-h264');
            setShowCenterMsg(false);
          }
        },
        error: () => {
          requestKeyframe();
        },
      });
      dec.configure({
        codec: targetCodec,
        codedWidth: width,
        codedHeight: height,
        hardwareAcceleration: 'prefer-hardware',
        optimizeForLatency: true,
      });
      decoderRef.current = dec;
      curCodec = targetCodec;
      curWidth = width;
      curHeight = height;
      needKeyframe = true;
    };

    statsTimerRef.current = window.setInterval(() => {
      if (wsRef.current !== ws) return;
      setFps(frameCount);
      frameCount = 0;
    }, 1000);

    ws.onmessage = (ev) => {
      if (wsRef.current !== ws || !openRef.current) return;
      if (typeof ev.data === 'string') {
        try {
          const msg = JSON.parse(ev.data) as {
            type?: string;
            codec?: string;
            preset?: string;
            fps?: number;
            bitrateKbps?: number;
            imageWidth?: number;
            imageHeight?: number;
            logicalX?: number;
            logicalY?: number;
            logicalWidth?: number;
            logicalHeight?: number;
            backend?: string;
            code?: string;
            error?: string;
          };
          if (msg.type === 'meta') {
            streamFps = msg.fps && msg.fps > 0 ? msg.fps : 60;
            streamCodec = msg.codec === 'hevc' ? 'hevc' : 'h264';
            frameRef.current = {
              jpegBase64: '',
              imageWidth: msg.imageWidth || 1280,
              imageHeight: msg.imageHeight || 720,
              logicalX: msg.logicalX || 0,
              logicalY: msg.logicalY || 0,
              logicalWidth: msg.logicalWidth || 1440,
              logicalHeight: msg.logicalHeight || 900,
            };
            setResLabel(`${msg.imageWidth || 0}×${msg.imageHeight || 0}`);
            setActivePreset(msg.preset || '');
            setBitrateKbps(msg.bitrateKbps || 0);
            setMode('ws-h264');
            ensureDecoder(
              msg.imageWidth || 1280,
              msg.imageHeight || 720,
              streamCodec === 'hevc' ? 'hev1.1.6.L153.B0' : undefined
            );
          } else if (msg.type === 'bitrate') {
            if (typeof msg.preset === 'string') setActivePreset(msg.preset);
            if (typeof msg.bitrateKbps === 'number') setBitrateKbps(msg.bitrateKbps);
          } else if (msg.type === 'error') {
            setMode('error');
            setCenterText(msg.error || t('deskConnectFailed'));
            setShowRetry(msg.code !== 'screen_recording_denied');
            setShowCenterMsg(true);
          }
        } catch {}
        return;
      }

      if (ev.data instanceof ArrayBuffer) {
        const bytes = new Uint8Array(ev.data);
        if (bytes.length < 2) return;
        const kind = bytes[0];
        if (kind !== 0x02 && kind !== 0x03 && kind !== 0x04 && kind !== 0x05) return;
        const isKey = kind === 0x02 || kind === 0x04;
        const isHevcPkt = kind === 0x04 || kind === 0x05;
        if (needKeyframe && !isKey) return;
        const payload = bytes.subarray(1);
        if (isKey) {
          const mw = frameRef.current?.imageWidth || curWidth || 1280;
          const mh = frameRef.current?.imageHeight || curHeight || 720;
          if (isHevcPkt) {
            streamCodec = 'hevc';
            if (curCodec !== 'hev1.1.6.L153.B0') {
              ensureDecoder(mw, mh, 'hev1.1.6.L153.B0');
            }
          } else {
            const spsCodec = extractAvcCodecFromAnnexB(payload);
            if (spsCodec && spsCodec !== curCodec) {
              ensureDecoder(mw, mh, spsCodec);
            }
          }
        }
        const dec = decoderRef.current;
        if (!dec || dec.state !== 'configured') return;
        if (!isKey && dec.decodeQueueSize > 3) {
          requestKeyframe(true);
          return;
        }
        needKeyframe = false;
        tsCounter += Math.round(1_000_000 / Math.max(1, streamFps));
        try {
          dec.decode(
            new EncodedVideoChunk({
              type: isKey ? 'key' : 'delta',
              timestamp: tsCounter,
              data: payload,
            })
          );
        } catch {
          requestKeyframe();
        }
      }
    };

    ws.onerror = () => {
      if (wsRef.current !== ws || !openRef.current) return;
      if (modeRef.current !== 'error') {
        setMode('error');
        setCenterText(t('deskConnectFailed'));
        setShowRetry(true);
        setShowCenterMsg(true);
      }
    };

    ws.onclose = () => {
      if (wsRef.current !== ws || !openRef.current) return;
      if (modeRef.current !== 'error') {
        setMode('error');
        setCenterText(t('deskConnectFailed'));
        setShowRetry(true);
        setShowCenterMsg(true);
      }
    };
  }, [stopStreams, t]);

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
      setMode('error');
      setCenterText(err instanceof Error ? err.message : t('deskUnreachable'));
      setShowRetry(true);
      setShowCenterMsg(true);
      return;
    }

    if (!openRef.current || gen !== streamGenRef.current) return;
    if (!hostStatusRef.current?.webrtcAvailable || typeof RTCPeerConnection === 'undefined') {
      void startWsH264Stream();
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
  }, [onTakeoversChange, refreshWindows, startWsH264Stream, stopStreams, t]);

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

  const sendInput = useCallback((ev: Record<string, unknown>, ephemeral = false) => {
    if (!openRef.current) return;
    const dc = dcRef.current;
    if (modeRef.current === 'webrtc' && dc && dc.readyState === 'open') {
      if (ephemeral && dc.bufferedAmount > 65536) return;
      const dcMsg: Record<string, unknown> = { type: ev.kind };
      for (const k of ['x', 'y', 'button', 'dx', 'dy', 'code', 'key', 'shift', 'ctrl', 'alt', 'meta', 'text']) {
        if (ev[k] !== undefined && ev[k] !== false) dcMsg[k] = ev[k];
      }
      dc.send(JSON.stringify(dcMsg));
      return;
    }
    const ws = wsRef.current;
    const winApp = windowsRef.current.find((w) => w.id === selectedWindowIdRef.current)?.app || '';
    if (modeRef.current === 'ws-h264' && ws && ws.readyState === WebSocket.OPEN) {
      if (ephemeral && ws.bufferedAmount > 65536) return;
      ws.send(
        JSON.stringify({
          type: 'input',
          event: { windowId: selectedWindowIdRef.current, app: winApp, ...ev },
        })
      );
      return;
    }
    if (ephemeral) return;
    api('/api/desktop/input', {
      method: 'POST',
      body: { target: targetRef.current, windowId: selectedWindowIdRef.current, app: winApp, ...ev },
    }).catch(() => {});
  }, []);

  const mapCoords = (e: React.MouseEvent<HTMLElement>): { x: number; y: number } | null => {
    const el = mode === 'webrtc' ? videoRef.current : canvasRef.current;
    if (!el) return null;
    const mw =
      el instanceof HTMLVideoElement
        ? el.videoWidth
        : frameRef.current?.imageWidth || (el as HTMLCanvasElement).width;
    const mh =
      el instanceof HTMLVideoElement
        ? el.videoHeight
        : frameRef.current?.imageHeight || (el as HTMLCanvasElement).height;
    if (!mw || !mh) return null;
    const r = el.getBoundingClientRect();
    if (!r.width || !r.height) return null;
    const scale = Math.min(r.width / mw, r.height / mh);
    const w = mw * scale;
    const h = mh * scale;
    const rx = (e.clientX - r.left - (r.width - w) / 2) / w;
    const ry = (e.clientY - r.top - (r.height - h) / 2) / h;
    if (rx < 0 || rx > 1 || ry < 0 || ry > 1) return null;
    const f = frameRef.current;
    const s = hostStatusRef.current;
    const b =
      f && (f.logicalWidth || 0) > 0
        ? { x: f.logicalX || 0, y: f.logicalY || 0, w: f.logicalWidth || 1440, h: f.logicalHeight || 900 }
        : s && (s.logicalWidth || 0) > 0
          ? { x: s.logicalX || 0, y: s.logicalY || 0, w: s.logicalWidth || 1440, h: s.logicalHeight || 900 }
          : { x: 0, y: 0, w: 1440, h: 900 };
    return { x: b.x + rx * b.w, y: b.y + ry * b.h };
  };

  const btnName = (b: number) => (b === 2 ? 'right' : b === 1 ? 'middle' : 'left');

  const mediaHandlers = {
    onMouseDown: (e: React.MouseEvent<HTMLElement>) => {
      e.currentTarget.focus();
      if (mode !== 'webrtc' && mode !== 'ws-h264') return;
      e.preventDefault();
      const p = mapCoords(e);
      if (p) sendInput({ kind: 'down', x: p.x, y: p.y, button: btnName(e.button) });
    },
    onMouseUp: (e: React.MouseEvent<HTMLElement>) => {
      if (mode !== 'webrtc' && mode !== 'ws-h264') return;
      e.preventDefault();
      const p = mapCoords(e);
      if (p) sendInput({ kind: 'up', x: p.x, y: p.y, button: btnName(e.button) });
    },
    onMouseMove: (e: React.MouseEvent<HTMLElement>) => {
      if (mode !== 'webrtc' && mode !== 'ws-h264') return;
      const now = performance.now();
      if (now - lastMoveRef.current < 16) return;
      lastMoveRef.current = now;
      const p = mapCoords(e);
      if (p) sendInput({ kind: 'move', x: p.x, y: p.y }, true);
    },
    onClick: (e: React.MouseEvent<HTMLElement>) => {
      if (mode === 'webrtc' || mode === 'ws-h264') return;
      e.preventDefault();
      e.currentTarget.focus();
      const p = mapCoords(e);
      if (p) sendInput({ kind: 'click', x: p.x, y: p.y });
    },
    onDoubleClick: (e: React.MouseEvent<HTMLElement>) => {
      if (mode === 'webrtc' || mode === 'ws-h264') return;
      e.preventDefault();
      const p = mapCoords(e);
      if (p) sendInput({ kind: 'double_click', x: p.x, y: p.y });
    },
    onContextMenu: (e: React.MouseEvent<HTMLElement>) => {
      e.preventDefault();
      if (mode === 'webrtc' || mode === 'ws-h264') return;
      const p = mapCoords(e);
      if (p) sendInput({ kind: 'right_click', x: p.x, y: p.y });
    },
    onWheel: (e: React.WheelEvent<HTMLElement>) => {
      const scale = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? 400 : 1;
      wheelAccRef.current.dx += e.deltaX * scale;
      wheelAccRef.current.dy += e.deltaY * scale;
      if (!wheelTimerRef.current) {
        wheelTimerRef.current = window.setTimeout(() => {
          wheelTimerRef.current = null;
          const { dx, dy } = wheelAccRef.current;
          wheelAccRef.current = { dx: 0, dy: 0 };
          if (dx || dy) sendInput({ kind: 'wheel', dx, dy }, modeRef.current === 'webrtc');
        }, 60);
      }
    },
    onKeyDown: (e: React.KeyboardEvent<HTMLElement>) => {
      if (e.nativeEvent.isComposing || ['Meta', 'Control', 'Alt', 'Shift'].includes(e.key)) return;
      e.preventDefault();
      e.stopPropagation();
      if (mode === 'webrtc') {
        if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) {
          sendInput({ kind: 'text', text: e.key });
        } else {
          sendInput({
            kind: 'keydown',
            key: e.key,
            code: e.code,
            shift: e.shiftKey,
            ctrl: e.ctrlKey,
            alt: e.altKey,
            meta: e.metaKey,
          });
        }
        return;
      }
      if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) {
        sendInput({ kind: 'type', text: e.key });
      } else {
        const mods = [
          e.metaKey && 'cmd',
          e.ctrlKey && 'ctrl',
          e.altKey && 'alt',
          e.shiftKey && 'shift',
          e.key,
        ]
          .filter(Boolean)
          .join('+');
        sendInput({ kind: 'key', key: mods, code: e.code });
      }
    },
    onKeyUp: (e: React.KeyboardEvent<HTMLElement>) => {
      if (mode !== 'webrtc' || e.nativeEvent.isComposing || ['Meta', 'Control', 'Alt', 'Shift'].includes(e.key)) {
        return;
      }
      if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey) return;
      e.preventDefault();
      e.stopPropagation();
      sendInput({
        kind: 'keyup',
        key: e.key,
        code: e.code,
        shift: e.shiftKey,
        ctrl: e.ctrlKey,
        alt: e.altKey,
        meta: e.metaKey,
      });
    },
    onPaste: (e: React.ClipboardEvent<HTMLElement>) => {
      const txt = e.clipboardData?.getData('text/plain');
      if (txt) {
        e.preventDefault();
        sendInput({ kind: 'text', text: txt });
      }
    },
  };

  const activeTakeover = takeovers.find((to) => to.target === target) || takeovers[0] || null;
  const dotClass =
    mode === 'webrtc' || mode === 'ws-h264' ? 'd-dot ok' : mode === 'error' ? 'd-dot err' : 'd-dot';
  const statusLabel =
    mode === 'webrtc'
      ? `WebRTC · ${fps || 0} fps`
      : mode === 'ws-h264'
        ? t('deskWsH264Label', { fps: fps || 0 })
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
            hidden={mode !== 'ws-h264'}
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
