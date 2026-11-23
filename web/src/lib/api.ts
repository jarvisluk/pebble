import type { MentionCandidate, MessageMedia, SlashCommandItem } from '../types';

export class APIError extends Error {
  status: number;
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

let unauthorizedHandler: (() => void) | null = null;

export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn;
}

export async function api<T = unknown>(
  path: string,
  opts: { method?: string; body?: unknown } = {}
): Promise<T> {
  const res = await fetch(path, {
    method: opts.method,
    headers: opts.body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  if (res.status === 401 && path !== '/api/login') {
    unauthorizedHandler?.();
    throw new APIError('unauthorized', 401);
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new APIError(data.error || `HTTP ${res.status}`, res.status, data.code);
  }
  return data as T;
}

export async function fetchCollabMessages<T = unknown>(
  botId: string,
  peerId: string,
  limit = 200
): Promise<T[]> {
  const data = await api<{ messages?: T[] }>(
    `/api/bots/${encodeURIComponent(botId)}/collab/${encodeURIComponent(peerId)}?limit=${limit}`
  );
  return data.messages || [];
}

export async function fetchBotCommands(botId: string): Promise<SlashCommandItem[]> {
  const data = await api<{ commands?: SlashCommandItem[] }>(
    `/api/bots/${encodeURIComponent(botId)}/commands`
  );
  return data.commands || [];
}

export async function fetchBotMentions(
  botId: string,
  query: string,
  limit = 30
): Promise<MentionCandidate[]> {
  const data = await api<{ items?: MentionCandidate[] }>(
    `/api/bots/${encodeURIComponent(botId)}/mentions?q=${encodeURIComponent(query)}&limit=${limit}`
  );
  return data.items || [];
}

export async function uploadBotAttachment(
  botId: string,
  file: File
): Promise<{ media: MessageMedia; workspacePath?: string }> {
  const form = new FormData();
  form.append('file', file);
  const res = await fetch(`/api/bots/${encodeURIComponent(botId)}/attachments`, {
    method: 'POST',
    body: form,
  });
  if (res.status === 401) {
    unauthorizedHandler?.();
    throw new APIError('unauthorized', 401);
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new APIError(data.error || `HTTP ${res.status}`, res.status, data.code);
  }
  return data as { media: MessageMedia; workspacePath?: string };
}
