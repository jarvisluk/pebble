import { requireOptionalNativeModule } from 'expo-modules-core';

interface NativeBothubTunnel {
  startTunnel(link: string, token: string): Promise<string>;
  stopTunnel(): Promise<void>;
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
