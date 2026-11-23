import { hashStr, normalizeBotAvatar } from './theme';

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

export interface PebbleGaze {
  k: number;
  gx: number;
  gy: number;
  eye?: number;
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

export function pebbleFrame(
  P: PebbleParams,
  t: number,
  busy: boolean,
  g: PebbleGaze = { k: 0, gx: 0, gy: 0, eye: 1 }
): PebbleFrameData {
  const breathe = 1 + 0.018 * Math.sin(t * P.breath * Math.PI * (busy ? 2.6 : 1));
  const bobY = Math.sin(t * 1.2 + P.bob) * (busy ? 1.2 : 0.6);
  const asym = 0.35; // amp=0.5, preset.asym=0.35 -> 0.5 * 2 * 0.35

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

  const c = Math.min(0.5, P.corner); // round=0.5 -> *(0.5 + 0.5) = 1

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

  lx = lerp(lx, g.gx, g.k);
  ly = lerp(ly, g.gy, g.k);

  const w = 5;
  const h = 9.5 * Math.max(0.12, blink * (g.eye ?? 1)) * (1 + 0.08 * g.k);
  const gap = 19;
  const lean = -P.side * 14 * (1 - g.k);
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

  const bodyRotDeg = P.tilt * 57.3 + Math.sin(t * 0.6 + P.bob) * (busy ? 3 : 1.5);
  return {
    pathD: polyPath(V, c),
    bodyRotDeg,
    eyes: [mkEye(ex - gap / 2), mkEye(ex + gap / 2)],
  };
}

interface LiveEntry {
  key: string;
  el: HTMLDivElement;
  fxGroup: SVGGElement;
  bodyGroup: SVGGElement;
  pathEl: SVGPathElement;
  eyeL: SVGRectElement;
  eyeR: SVGRectElement;
  params: PebbleParams;
  color: string;
  busy: boolean;
  g: PebbleGaze;
}

interface PressState {
  down: boolean;
  t0: number;
  t1: number;
}

const live = new Set<LiveEntry>();
const press = new Map<string, PressState>();
const mouse = { x: 0, y: 0, on: false };
let listenersBound = false;
let rafId = 0;
let clock = 0;
let lastNow = 0;

const reduceQuery = typeof window !== 'undefined' && window.matchMedia
  ? window.matchMedia('(prefers-reduced-motion: reduce)')
  : null;

function isReducedMotion(): boolean {
  return Boolean(reduceQuery && reduceQuery.matches);
}

function hitZone(el: HTMLElement, x: number, y: number): boolean {
  const zone = (el.closest('.item, .chat-hd, .empty, .av') || el) as HTMLElement;
  const z = zone.getBoundingClientRect();
  return x >= z.left && x <= z.right && y >= z.top && y <= z.bottom;
}

function bindGlobalListeners(): void {
  if (listenersBound || typeof window === 'undefined') return;
  listenersBound = true;

  window.addEventListener('pointermove', (e) => {
    mouse.x = e.clientX;
    mouse.y = e.clientY;
    mouse.on = true;
    ensureTick();
  });

  document.documentElement.addEventListener('pointerleave', () => {
    mouse.on = false;
  });

  window.addEventListener('pointerdown', (e) => {
    const now = performance.now() / 1000;
    for (const entry of live) {
      if (hitZone(entry.el, e.clientX, e.clientY)) {
        press.set(entry.key, { down: true, t0: now, t1: 0 });
        ensureTick();
      }
    }
  });

  const release = () => {
    const now = performance.now() / 1000;
    let changed = false;
    for (const st of press.values()) {
      if (st.down) {
        st.down = false;
        st.t1 = now;
        changed = true;
      }
    }
    if (changed) ensureTick();
  };
  window.addEventListener('pointerup', release);
  window.addEventListener('pointercancel', release);
}

function fxOf(key: string): { sx: number; sy: number; dy: number; eye: number } | null {
  const st = press.get(key);
  if (!st) return null;
  const now = performance.now() / 1000;
  const d = st.down ? Math.min(1, (now - st.t0) / 0.08) : 0;
  const r = st.down ? 0 : now - st.t1;
  if (!st.down && r > 1.4) {
    press.delete(key);
    return null;
  }
  const b = st.down ? 0 : Math.exp(-5 * r) * Math.sin(r * 22);
  const hop = !st.down && r < 0.32 ? Math.sin((r / 0.32) * Math.PI) : 0;
  const eye = st.down ? 0 : r < 0.14 ? r / 0.14 : 1;
  return {
    sx: 1 + 0.08 * d - 0.07 * b,
    sy: 1 - 0.1 * d + 0.09 * b,
    dy: -5 * hop,
    eye,
  };
}

function updateGaze(dt: number): boolean {
  let moving = false;
  for (const a of live) {
    const g = a.g;
    const hit = mouse.on && hitZone(a.el, mouse.x, mouse.y);
    if (hit) {
      const r = a.el.getBoundingClientRect();
      if (r.width > 0) {
        const s = 100 / r.width;
        const dx = (mouse.x - (r.left + r.width / 2)) * s;
        const dy = (mouse.y - (r.top + r.height / 2)) * s;
        const d = Math.hypot(dx, dy) || 1;
        const m = Math.min(1, d / 60);
        const tx = (dx / d) * 9 * m;
        const ty = (dy / d) * 6 * m;
        g.gx += (tx - g.gx) * Math.min(1, dt * 14);
        g.gy += (ty - g.gy) * Math.min(1, dt * 14);
      }
    }
    const tk = hit ? 1 : 0;
    const nk = g.k + (tk - g.k) * Math.min(1, dt * 8);
    if (Math.abs(nk - g.k) > 1e-3 || hit) moving = true;
    g.k = nk;
  }
  return moving;
}

function applyEye(el: SVGRectElement, eye: PebbleEyeRect): void {
  el.setAttribute('x', eye.x.toFixed(2));
  el.setAttribute('y', eye.y.toFixed(2));
  el.setAttribute('width', eye.width.toFixed(2));
  el.setAttribute('height', eye.height.toFixed(2));
  el.setAttribute('rx', eye.rx.toFixed(2));
  el.setAttribute('transform', `rotate(${eye.lean.toFixed(2)} ${eye.cx.toFixed(2)} ${eye.cy.toFixed(2)})`);
}

function drawEntry(a: LiveEntry, t: number): void {
  const fx = fxOf(a.key);
  a.g.eye = fx ? fx.eye : 1;
  const f = pebbleFrame(a.params, t, a.busy, a.g);
  if (fx) {
    a.fxGroup.setAttribute(
      'transform',
      `translate(0 ${fx.dy.toFixed(2)}) translate(50 90) scale(${fx.sx.toFixed(3)} ${fx.sy.toFixed(3)}) translate(-50 -90)`
    );
  } else if (a.fxGroup.hasAttribute('transform')) {
    a.fxGroup.removeAttribute('transform');
  }
  a.bodyGroup.setAttribute('transform', `rotate(${f.bodyRotDeg.toFixed(1)} 50 50)`);
  a.pathEl.setAttribute('d', f.pathD);
  a.pathEl.setAttribute('fill', a.color);
  applyEye(a.eyeL, f.eyes[0]);
  applyEye(a.eyeR, f.eyes[1]);
}

function tick(now: number): void {
  if (live.size === 0) {
    rafId = 0;
    return;
  }
  const dt = lastNow ? Math.min(0.05, (now - lastNow) / 1000) : 0.016;
  lastNow = now;
  const reduce = isReducedMotion();
  const gazing = !reduce && updateGaze(dt);
  if (!reduce) {
    clock += dt;
    for (const a of live) drawEntry(a, clock);
    rafId = requestAnimationFrame(tick);
  } else if (gazing || press.size > 0) {
    for (const a of live) drawEntry(a, 0);
    rafId = requestAnimationFrame(tick);
  } else {
    rafId = 0;
  }
}

function ensureTick(): void {
  if (rafId !== 0 || live.size === 0 || typeof requestAnimationFrame === 'undefined') return;
  if (isReducedMotion() && press.size === 0) return;
  lastNow = performance.now();
  rafId = requestAnimationFrame(tick);
}

let seqCounter = 0;

export function registerPebbleAvatar(
  entry: Omit<LiveEntry, 'key' | 'g'>
): { update: (params: PebbleParams, color: string, busy: boolean) => void; destroy: () => void } {
  bindGlobalListeners();
  const key = `${entry.el.dataset.botId || '?'}|${++seqCounter}`;
  const full: LiveEntry = {
    ...entry,
    key,
    g: { k: 0, gx: 0, gy: 0, eye: 1 },
  };
  live.add(full);
  drawEntry(full, isReducedMotion() ? 0 : clock);
  ensureTick();

  return {
    update(params: PebbleParams, color: string, busy: boolean) {
      full.params = params;
      full.color = color;
      full.busy = busy;
      drawEntry(full, isReducedMotion() ? 0 : clock);
    },
    destroy() {
      live.delete(full);
      press.delete(key);
      if (live.size === 0 && rafId !== 0) {
        cancelAnimationFrame(rafId);
        rafId = 0;
      }
    },
  };
}
