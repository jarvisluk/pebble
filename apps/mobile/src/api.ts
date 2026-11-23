import { tStatic } from './i18n';
import type { MentionCandidate, MessageMedia, SlashCommandItem } from './types';

export class UnauthorizedError extends Error {
  constructor() {
    super(tStatic('invalidTokenErr'));
    this.name = 'UnauthorizedError';
  }
}

export function extractTokenFromLink(raw: string): { cleanAddress: string; token: string } {
  const trimmed = raw.trim();
  const hashIdx = trimmed.indexOf('#');
  if (hashIdx === -1) {
    return { cleanAddress: trimmed, token: '' };
  }
  const base = trimmed.slice(0, hashIdx);
  const frag = trimmed.slice(hashIdx + 1);
  let token = '';
  for (const part of frag.split('&')) {
    const eq = part.indexOf('=');
    const k = eq >= 0 ? part.slice(0, eq) : part;
    const v = eq >= 0 ? part.slice(eq + 1) : '';
    if (decodeURIComponent(k) === 'token') {
      try {
        token = decodeURIComponent(v.replace(/\+/g, '%20')).trim();
      } catch {
        token = v.trim();
      }
      break;
    }
  }
  return { cleanAddress: base, token };
}

export function normalizeAddress(raw: string): {
  cleanAddress: string;
  token: string;
  isTailcat: boolean;
  isTailscale: boolean;
} {
  const { cleanAddress, token } = extractTokenFromLink(raw);
  const s = cleanAddress.trim();
  const lower = s.toLowerCase();
  if (lower.startsWith('tailcat://')) {
    return { cleanAddress: s, token, isTailcat: true, isTailscale: false };
  }
  if (lower.startsWith('tailscale://')) {
    return { cleanAddress: s, token, isTailcat: false, isTailscale: true };
  }
  const withProto = /^https?:\/\//i.test(s) ? s : `http://${s}`;
  return { cleanAddress: withProto.replace(/\/+$/, ''), token, isTailcat: false, isTailscale: false };
}

export async function apiRequest<T>(
  baseURL: string,
  token: string,
  path: string,
  opts: { method?: string; body?: unknown } = {}
): Promise<T> {
  const headers: Record<string, string> = {};
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(`${baseURL}${path}`, {
    method: opts.method || 'GET',
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  if (res.status === 401) {
    throw new UnauthorizedError();
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  return data as T;
}

export async function fetchCollabMessages<T = unknown>(
  baseURL: string,
  token: string,
  botId: string,
  peerId: string,
  limit = 200
): Promise<T[]> {
  const res = await apiRequest<{ messages?: T[] }>(
    baseURL,
    token,
    `/api/bots/${encodeURIComponent(botId)}/collab/${encodeURIComponent(peerId)}?limit=${limit}`
  );
  return res.messages || [];
}

export function getMediaSource(baseURL: string, token: string, url: string) {
  const fullUri = url.startsWith('http://') || url.startsWith('https://') ? url : `${baseURL}${url}`;
  return token ? { uri: fullUri, headers: { Authorization: `Bearer ${token}` } } : { uri: fullUri };
}

export async function fetchBotCommands(
  baseURL: string,
  token: string,
  botId: string
): Promise<SlashCommandItem[]> {
  const res = await apiRequest<{ commands?: SlashCommandItem[] }>(
    baseURL,
    token,
    `/api/bots/${encodeURIComponent(botId)}/commands`
  );
  return res.commands || [];
}

export async function fetchBotMentions(
  baseURL: string,
  token: string,
  botId: string,
  query: string,
  limit = 30
): Promise<MentionCandidate[]> {
  const res = await apiRequest<{ items?: MentionCandidate[] }>(
    baseURL,
    token,
    `/api/bots/${encodeURIComponent(botId)}/mentions?q=${encodeURIComponent(query)}&limit=${limit}`
  );
  return res.items || [];
}

export async function uploadBotAttachment(
  baseURL: string,
  token: string,
  botId: string,
  file: { uri: string; name: string; mimeType?: string }
): Promise<{ media: MessageMedia; workspacePath?: string }> {
  const form = new FormData();
  form.append('file', {
    uri: file.uri,
    name: file.name || 'file',
    type: file.mimeType || 'application/octet-stream',
  } as unknown as Blob);
  const headers: Record<string, string> = {};
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(`${baseURL}/api/bots/${encodeURIComponent(botId)}/attachments`, {
    method: 'POST',
    headers,
    body: form,
  });
  if (res.status === 401) {
    throw new UnauthorizedError();
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || `HTTP ${res.status}`);
  }
  return data as { media: MessageMedia; workspacePath?: string };
}

export async function requestMediaDownloadURL(
  baseURL: string,
  token: string,
  media: { id: string; url: string; name?: string }
): Promise<string> {
  const fullUri = media.url.startsWith('http://') || media.url.startsWith('https://') ? media.url : `${baseURL}${media.url}`;
  const params = new URLSearchParams();
  params.set('download', '1');
  if (media.name) {
    params.set('name', media.name);
  }
  if (token) {
    const res = await apiRequest<{ ticket: string }>(
      baseURL,
      token,
      `/api/media/${encodeURIComponent(media.id)}/ticket`,
      { method: 'POST' }
    );
    if (res.ticket) {
      params.set('ticket', res.ticket);
    }
  }
  const sep = fullUri.includes('?') ? '&' : '?';
  return `${fullUri}${sep}${params.toString()}`;
}

export interface SSEHandlers {
  onStatus: (online: boolean) => void;
  onEvent: (type: string, payload: unknown) => void;
  onUnauthorized?: () => void;
}

export function connectSSE(baseURL: string, token: string, handlers: SSEHandlers): () => void {
  let closed = false;
  let xhr: XMLHttpRequest | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let delay = 1000;

  const openStream = () => {
    if (closed) return;
    xhr = new XMLHttpRequest();
    let cursor = 0;
    let buffer = '';

    xhr.open('GET', `${baseURL}/api/events`, true);
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    }
    xhr.setRequestHeader('Accept', 'text/event-stream');

    const flushChunk = (text: string) => {
      if (!text) return;
      buffer += text.replace(/\r\n/g, '\n');
      let sep = buffer.indexOf('\n\n');
      while (sep !== -1) {
        const block = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        sep = buffer.indexOf('\n\n');

        let eventType = 'message';
        const dataLines: string[] = [];
        for (const line of block.split('\n')) {
          if (!line || line.startsWith(':')) continue;
          if (line.startsWith('event:')) {
            eventType = line.slice(6).trim();
          } else if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).trimStart());
          }
        }
        if (eventType === 'ready') {
          delay = 1000;
          handlers.onStatus(true);
          continue;
        }
        if (dataLines.length > 0) {
          try {
            const parsed = JSON.parse(dataLines.join('\n'));
            handlers.onEvent(eventType, parsed?.payload ?? parsed);
          } catch {
            // ignore malformed event payload
          }
        }
      }
    };

    xhr.onprogress = () => {
      if (closed || !xhr) return;
      const chunk = xhr.responseText.slice(cursor);
      cursor = xhr.responseText.length;
      flushChunk(chunk);
    };

    xhr.onreadystatechange = () => {
      if (closed || !xhr) return;
      if (xhr.readyState === XMLHttpRequest.HEADERS_RECEIVED && xhr.status === 401) {
        handlers.onStatus(false);
        handlers.onUnauthorized?.();
        closed = true;
        xhr.abort();
      }
    };

    const scheduleReconnect = () => {
      if (closed) return;
      handlers.onStatus(false);
      retryTimer = setTimeout(() => {
        delay = Math.min(delay * 1.8, 12000);
        openStream();
      }, delay);
    };

    xhr.onerror = scheduleReconnect;
    xhr.onload = scheduleReconnect;
    xhr.send();
  };

  openStream();

  return () => {
    closed = true;
    if (retryTimer) clearTimeout(retryTimer);
    if (xhr) {
      xhr.onprogress = null;
      xhr.onerror = null;
      xhr.onload = null;
      xhr.onreadystatechange = null;
      xhr.abort();
      xhr = null;
    }
  };
}
