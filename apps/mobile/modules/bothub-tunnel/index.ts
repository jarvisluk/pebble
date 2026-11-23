import { requireNativeViewManager, requireOptionalNativeModule } from 'expo-modules-core';
import type { Ref } from 'react';
import type { ViewProps } from 'react-native';

interface NativeBothubTunnel {
  startTunnel(link: string, token: string): Promise<string>;
  stopTunnel(): Promise<void>;
  tailscaleStart(stateDir: string, hostname: string, controlUrl: string, authKey: string): Promise<string>;
  tailscaleStatus(includePeers: boolean): Promise<string>;
  tailscaleLogout(): Promise<string>;
}

export interface TailscalePeer {
  id: string;
  hostname: string;
  dnsName?: string;
  ip?: string;
  os?: string;
  address: string;
  online: boolean;
  isBotHub: boolean;
}

export interface MobileTailscaleState {
  state: 'disabled' | 'starting' | 'needs_login' | 'running' | 'error' | string;
  authUrl?: string;
  hostname?: string;
  dnsName?: string;
  ip?: string;
  tailnet?: string;
  link?: string;
  error?: string;
  peers?: TailscalePeer[];
}

const native = requireOptionalNativeModule<NativeBothubTunnel>('BothubTunnel');

export function isNativeTunnelAvailable(): boolean {
  return native != null;
}

export async function startTunnel(link: string, token = ''): Promise<{ url: string; token?: string }> {
  if (!native) {
    throw new Error('当前运行环境不含 Tailcat 原生模块（需使用开发构建版本），暂只支持直接输入 http(s):// 地址');
  }
  const raw = await native.startTunnel(link, token);
  let parsed: { url?: string; token?: string; error?: string };
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error(raw || '隧道启动失败');
  }
  if (parsed.error) {
    throw new Error(parsed.error);
  }
  if (!parsed.url) {
    throw new Error('隧道未返回本地端口');
  }
  return { url: parsed.url, token: parsed.token };
}

export async function stopTunnel(): Promise<void> {
  if (!native) return;
  await native.stopTunnel();
}

function parseTS(raw: string): MobileTailscaleState {
  try {
    return JSON.parse(raw) as MobileTailscaleState;
  } catch {
    return { state: 'error', error: raw || 'tailscale parse failed' };
  }
}

export async function tailscaleStart(opts: {
  stateDir?: string;
  hostname?: string;
  controlUrl?: string;
  authKey?: string;
} = {}): Promise<MobileTailscaleState> {
  if (!native) {
    throw new Error('当前运行环境不含原生隧道模块');
  }
  const raw = await native.tailscaleStart(
    opts.stateDir || '',
    opts.hostname || '',
    opts.controlUrl || '',
    opts.authKey || ''
  );
  return parseTS(raw);
}

export async function tailscaleStatus(includePeers = false): Promise<MobileTailscaleState> {
  if (!native) {
    return { state: 'disabled' };
  }
  const raw = await native.tailscaleStatus(includePeers);
  return parseTS(raw);
}

export async function tailscaleLogout(): Promise<MobileTailscaleState> {
  if (!native) {
    return { state: 'disabled' };
  }
  const raw = await native.tailscaleLogout();
  return parseTS(raw);
}

export interface H264MetaEvent {
  imageWidth: number;
  imageHeight: number;
  logicalX: number;
  logicalY: number;
  logicalWidth: number;
  logicalHeight: number;
  preset?: string;
  bitrateKbps?: number;
}

export interface H264StatusEvent {
  connected: boolean;
  fps: number;
}

export interface H264ErrorEvent {
  code?: string;
  error: string;
}

export interface BothubH264ViewProps extends ViewProps {
  ref?: Ref<BothubH264ViewRef>;
  url: string;
  token: string;
  preset: 'auto' | 'ultra' | 'high' | 'medium' | 'low';
  active: boolean;
  onMeta?: (e: { nativeEvent: H264MetaEvent }) => void;
  onStatus?: (e: { nativeEvent: H264StatusEvent }) => void;
  onStreamError?: (e: { nativeEvent: H264ErrorEvent }) => void;
}

export type BothubH264ViewRef = {
  sendInput?: (eventJson: string) => Promise<void>;
};

export const BothubH264View = requireNativeViewManager<BothubH264ViewProps>('BothubTunnel');
