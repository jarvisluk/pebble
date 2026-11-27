import type React from 'react';
import type { DesktopFrame } from '../../types';

export type DesktopMode = 'idle' | 'connecting' | 'webrtc' | 'ws-h264' | 'frame' | 'error' | 'sleeping';

export function isLocalTarget(tgt: string): boolean {
  return !tgt || tgt === 'local' || tgt.startsWith('virtual:') || tgt.startsWith('local:virtual:');
}

export function extractAvcCodecFromAnnexB(data: Uint8Array): string | null {
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
}

export interface WsH264ConnectOpts {
  allowHevcFallback: boolean;
  supportsHEVC: boolean;
  openRef: React.MutableRefObject<boolean>;
  targetRef: React.MutableRefObject<string>;
  streamGenRef: React.MutableRefObject<number>;
  wsRef: React.MutableRefObject<WebSocket | null>;
  decoderRef: React.MutableRefObject<VideoDecoder | null>;
  statsTimerRef: React.MutableRefObject<number | null>;
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  frameRef: React.MutableRefObject<DesktopFrame | null>;
  modeRef: React.MutableRefObject<DesktopMode>;
  stopStreams: () => void;
  startFramePolling: () => void;
  restartWithH264: () => void;
  onUnsupportedCap?: (cap: string) => void;
  setMode: React.Dispatch<React.SetStateAction<DesktopMode>>;
  setCenterText: (s: string) => void;
  setShowRetry: (v: boolean) => void;
  setShowCenterMsg: (v: boolean) => void;
  setFps: React.Dispatch<React.SetStateAction<number>>;
  setResLabel: (s: string) => void;
  setActivePreset: (s: string) => void;
  setBitrateKbps: (v: number) => void;
  t: (k: string, vars?: Record<string, string | number>) => string;
}

export async function connectWsH264Stream(opts: WsH264ConnectOpts): Promise<void> {
  const {
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
    restartWithH264,
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
  } = opts;

  if (!openRef.current) return;
  stopStreams();
  const gen = ++streamGenRef.current;

  let preferHevc = false;
  if (allowHevcFallback && supportsHEVC) {
    try {
      const sup = await VideoDecoder.isConfigSupported({
        codec: 'hev1.1.6.L153.B0',
        hardwareAcceleration: 'prefer-hardware',
      });
      preferHevc = Boolean(sup?.supported);
    } catch {}
  }
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
  let fallbackTriggered = false;
  let streamFps = 60;
  let streamCodec: 'h264' | 'hevc' = preferHevc ? 'hevc' : 'h264';
  let curCodec = '';
  let curWidth = 0;
  let curHeight = 0;

  const handleWsFailure = (code?: string, errMsg?: string) => {
    if (wsRef.current !== ws || !openRef.current || gen !== streamGenRef.current) return;
    if (code === 'screen_recording_denied') {
      setMode('error');
      setCenterText(t('deskScreenRecordingDenied'));
      setShowRetry(false);
      setShowCenterMsg(true);
      return;
    }
    if (!hadFrame && !fallbackTriggered) {
      fallbackTriggered = true;
      if (code === 'capability_unsupported') {
        onUnsupportedCap?.('desktop.ws_h264');
      }
      if (preferHevc && allowHevcFallback) {
        restartWithH264();
      } else {
        startFramePolling();
      }
      return;
    }
    if (modeRef.current !== 'error') {
      setMode('error');
      setCenterText(errMsg || t('deskConnectFailed'));
      setShowRetry(true);
      setShowCenterMsg(true);
    }
  };

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
        if (!hadFrame) {
          handleWsFailure('decoder_error');
        } else {
          requestKeyframe();
        }
      },
    });
    try {
      dec.configure({
        codec: targetCodec,
        codedWidth: width,
        codedHeight: height,
        hardwareAcceleration: 'prefer-hardware',
        optimizeForLatency: true,
      });
    } catch {
      handleWsFailure('codec_unsupported');
      return;
    }
    decoderRef.current = dec;
    curCodec = targetCodec;
    curWidth = width;
    curHeight = height;
    needKeyframe = true;
  };

  statsTimerRef.current = window.setInterval(() => {
    if (wsRef.current !== ws || document.hidden) return;
    const next = frameCount;
    frameCount = 0;
    setFps((prev) => (prev === next ? prev : next));
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
          ensureDecoder(
            msg.imageWidth || 1280,
            msg.imageHeight || 720,
            streamCodec === 'hevc' ? 'hev1.1.6.L153.B0' : undefined
          );
        } else if (msg.type === 'bitrate') {
          if (typeof msg.preset === 'string') setActivePreset(msg.preset);
          if (typeof msg.bitrateKbps === 'number') setBitrateKbps(msg.bitrateKbps);
        } else if (msg.type === 'error') {
          handleWsFailure(msg.code, msg.error);
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
    handleWsFailure(undefined, t('deskConnectFailed'));
  };

  ws.onclose = () => {
    handleWsFailure(undefined, t('deskConnectFailed'));
  };
}
