export class UnauthorizedError extends Error {
  constructor() {
    super('访问令牌无效或未填写');
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

export function normalizeAddress(raw: string): { cleanAddress: string; token: string; isTailcat: boolean } {
  const { cleanAddress, token } = extractTokenFromLink(raw);
  const s = cleanAddress.trim();
  if (s.toLowerCase().startsWith('tailcat://')) {
    return { cleanAddress: s, token, isTailcat: true };
  }
  const withProto = /^https?:\/\//i.test(s) ? s : `http://${s}`;
  return { cleanAddress: withProto.replace(/\/+$/, ''), token, isTailcat: false };
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
