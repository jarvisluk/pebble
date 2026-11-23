import { useEffect, useState } from 'react';
import { AccessibilityInfo } from 'react-native';

import { normalizeBotAvatar } from '../theme';

const PEBBLE_VARIANT_SEEDS: Record<string, string> = {
  'pebble-1': 'pebble-v6',
  'pebble-2': 'pebble-v1',
  'pebble-3': 'pebble-v30',
  'pebble-4': 'pebble-v11',
  'pebble-5': 'pebble-v21',
  'pebble-6': 'pebble-v17',
  'pebble-7': 'pebble-v15',
};

export function resolvePebbleSeed(id: string, avatar?: string): string {
  const kind = normalizeBotAvatar(avatar);
  if (kind !== 'blob' && kind in PEBBLE_VARIANT_SEEDS) {
    return PEBBLE_VARIANT_SEEDS[kind];
  }
  return id || '?';
}

export interface PebbleVert {
  s: number;
  d: number;
  p: number;
}

export interface PebbleParams {
  n: number;
  verts: PebbleVert[];
  rot0: number;
  tilt: number;
  sx: number;
  corner: number;
  side: number;
  breath: number;
  blinkEvery: number;
  blinkOff: number;
  lookEvery: number;
  lookOff: number;
  lookSeed: number;
  bob: number;
}

export interface PebbleEyeRect {
  x: number;
  y: number;
  width: number;
  height: number;
  rx: number;
  cx: number;
  cy: number;
  lean: number;
}

export interface PebbleFrameData {
  pathD: string;
  bodyRotDeg: number;
  eyes: [PebbleEyeRect, PebbleEyeRect];
}

function hashStr(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  }
  return h >>> 0;
}

function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const lerp = (a: number, b: number, t: number) => a + (b - a) * t;

const CIRC: Record<number, number> = { 3: 54, 4: 49, 5: 46, 6: 45 };
const N_WEIGHTS: Array<[number, number]> = [
  [4, 0.6],
  [5, 0.25],
  [6, 0.15],
];

function pickN(q: number): number {
  let acc = 0;
  for (const [n, w] of N_WEIGHTS) {
    acc += w;
    if (q < acc) return n;
  }
  return N_WEIGHTS[N_WEIGHTS.length - 1][0];
}

const paramsCache = new Map<string, PebbleParams>();

export function pebbleParams(id: string): PebbleParams {
  const key = id || '?';
  const cached = paramsCache.get(key);
  if (cached) return cached;

  const r = mulberry32(hashStr(key));
  const n = pickN(r());
  const verts: PebbleVert[] = Array.from({ length: n }, () => ({
    s: 0.86 + r() * 0.24,
    d: (r() - 0.5) * 0.35,
    p: r() * 6.28,
  }));
  const _rr0 = r();
  void _rr0;
  const sign = r() < 0.5 ? -1 : 1;
  const tilt = (sign * lerp(6, 12, r())) / 57.3;
  const _sideR = r() < 0.5 ? -1 : 1;
  void _sideR;
  const P: PebbleParams = {
    n,
    verts,
    rot0: Math.PI / 2 - Math.PI / n,
    tilt,
    sx: lerp(0.95, 1.08, r()),
    corner: lerp(0.36, 0.44, r()),
    side: -sign,
    breath: 0.5 + r() * 0.4,
    blinkEvery: 2.8 + r() * 3.5,
    blinkOff: r() * 10,
    lookEvery: 2.2 + r() * 3,
    lookOff: r() * 10,
    lookSeed: hashStr(key + '#l'),
    bob: r() * 6.28,
  };
  if (paramsCache.size > 256) paramsCache.clear();
  paramsCache.set(key, P);
  return P;
}

function polyPath(V: Array<[number, number]>, c: number): string {
  const n = V.length;
  const pt = (a: [number, number]) => `${a[0].toFixed(2)} ${a[1].toFixed(2)}`;
  const cut = (a: [number, number], b: [number, number], k: number): [number, number] => [
    a[0] + (b[0] - a[0]) * k,
    a[1] + (b[1] - a[1]) * k,
  ];
  let d = '';
  for (let i = 0; i < n; i++) {
    const P = V[(i - 1 + n) % n];
    const v = V[i];
    const N = V[(i + 1) % n];
    d += (i ? 'L' : 'M') + pt(cut(v, P, c)) + 'Q' + pt(v) + ' ' + pt(cut(v, N, c));
  }
  return d + 'Z';
}

export function pebbleFrame(P: PebbleParams, t: number, busy: boolean, eyeOpen = 1): PebbleFrameData {
  const breathe = 1 + 0.018 * Math.sin(t * P.breath * Math.PI * (busy ? 2.6 : 1));
  const bobY = Math.sin(t * 1.2 + P.bob) * (busy ? 1.2 : 0.6);
  const asym = 0.35;

  let V: Array<[number, number]> = P.verts.map((v, i) => {
    const a = P.rot0 + (i / P.n) * Math.PI * 2 + v.d * asym * 0.5;
    const rr = (CIRC[P.n] ?? 49) * (1 + (v.s - 1) * asym) * breathe;
    return [Math.cos(a) * rr * P.sx, (Math.sin(a) * rr) / P.sx];
  });
  const xs = V.map((v) => v[0]);
  const ys = V.map((v) => v[1]);
  const ox = 50 - (Math.min(...xs) + Math.max(...xs)) / 2;
  const oy = 50 + bobY - (Math.min(...ys) + Math.max(...ys)) / 2;
  V = V.map((v) => [v[0] + ox, v[1] + oy]);

  const c = Math.min(0.5, P.corner);
  const bp = ((t + P.blinkOff) % P.blinkEvery) / P.blinkEvery;
  const blink = bp > 0.965 ? Math.max(0.12, Math.abs(Math.cos(((bp - 0.965) / 0.035) * Math.PI))) : 1;

  let lx: number;
  let ly: number;
  if (busy) {
    lx = Math.sin(t * 4.5) * 7;
    ly = -1;
  } else {
    const seg = (t + P.lookOff) / P.lookEvery;
    const k = Math.floor(seg);
    const f = seg - k;
    const tgt = (i: number): [number, number] => {
      const q = mulberry32((P.lookSeed ^ Math.imul(i + 1, 2654435761)) >>> 0);
      const r1 = q();
      const r2 = q();
      const r3 = q();
      return [P.side * (3 + r1 * 6) - (r2 < 0.2 ? P.side * 8 : 0), -2 + r3 * 4];
    };
    const a = tgt(k);
    const b = tgt(k + 1);
    const e = f < 0.85 ? 0 : (1 - Math.cos(((f - 0.85) / 0.15) * Math.PI)) / 2;
    lx = lerp(a[0], b[0], e);
    ly = lerp(a[1], b[1], e);
  }

  const w = 5;
  const h = 9.5 * Math.max(0.12, blink * eyeOpen);
  const gap = 19;
  const lean = -P.side * 14;
  const ex = 50 + lx;
  const ey = 47 + bobY + ly;

  const mkEye = (cx: number): PebbleEyeRect => ({
    x: cx - w,
    y: ey - h,
    width: w * 2,
    height: h * 2,
    rx: Math.min(w, h),
    cx,
    cy: ey,
    lean,
  });

  return {
    pathD: polyPath(V, c),
    bodyRotDeg: P.tilt * 57.3 + Math.sin(t * 0.6 + P.bob) * (busy ? 3 : 1.5),
    eyes: [mkEye(ex - gap / 2), mkEye(ex + gap / 2)],
  };
}

const listeners = new Set<(t: number) => void>();
let timer: ReturnType<typeof setInterval> | null = null;
let startMs = 0;
let reduceMotion = false;
let reduceChecked = false;

function ensureReduceChecked(): void {
  if (reduceChecked) return;
  reduceChecked = true;
  AccessibilityInfo.isReduceMotionEnabled()
    .then((enabled) => {
      reduceMotion = Boolean(enabled);
      if (reduceMotion && timer) {
        clearInterval(timer);
        timer = null;
        for (const fn of listeners) fn(0);
      }
    })
    .catch(() => {});
}

export function usePebbleClock(active: boolean): number {
  const [clock, setClock] = useState(0);

  useEffect(() => {
    if (!active) return;
    ensureReduceChecked();
    if (reduceMotion) {
      setClock(0);
      return;
    }
    const fn = (t: number) => setClock(t);
    listeners.add(fn);
    if (!timer) {
      startMs = Date.now();
      timer = setInterval(() => {
        const t = (Date.now() - startMs) / 1000;
        for (const cb of listeners) cb(t);
      }, 50); // ~20fps
    }
    return () => {
      listeners.delete(fn);
      if (listeners.size === 0 && timer) {
        clearInterval(timer);
        timer = null;
      }
    };
  }, [active]);

  return clock;
}
