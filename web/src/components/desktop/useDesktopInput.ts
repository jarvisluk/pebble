import { useCallback } from 'react';
import type React from 'react';
import type { DesktopFrame, DesktopHostStatus, DesktopWindow } from '../../types';
import { api } from '../../lib/api';
import type { DesktopMode } from './wsH264Client';

export interface UseDesktopInputOpts {
  mode: DesktopMode;
  openRef: React.MutableRefObject<boolean>;
  modeRef: React.MutableRefObject<DesktopMode>;
  targetRef: React.MutableRefObject<string>;
  selectedWindowIdRef: React.MutableRefObject<string>;
  windowsRef: React.MutableRefObject<DesktopWindow[]>;
  dcRef: React.MutableRefObject<RTCDataChannel | null>;
  wsRef: React.MutableRefObject<WebSocket | null>;
  videoRef: React.RefObject<HTMLVideoElement | null>;
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  frameRef: React.MutableRefObject<DesktopFrame | null>;
  hostStatusRef: React.MutableRefObject<DesktopHostStatus | null>;
  lastMoveRef: React.MutableRefObject<number>;
  wheelAccRef: React.MutableRefObject<{ dx: number; dy: number }>;
  wheelTimerRef: React.MutableRefObject<number | null>;
}

export function useDesktopInput(opts: UseDesktopInputOpts) {
  const {
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
  } = opts;

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

  return { sendInput, mediaHandlers };
}
