import type {
  BotTask,
  BotTaskEvent,
  MentionCandidate,
  MessageMedia,
  NodeInfo,
  SlashCommandItem,
  SpawnTaskRequest,
  UpdateStatus,
  WorkspaceFileContent,
  WorkspaceListResult,
} from '../types';

export const CLIENT_PROTOCOL = 2;

export class APIError extends Error {
  status: number;
  code?: string;
  requiredCapability?: string;
  minProtocol?: number;

  constructor(
    message: string,
    status: number,
    code?: string,
    requiredCapability?: string,
    minProtocol?: number
  ) {
    super(message);
    this.status = status;
    this.code = code;
    this.requiredCapability = requiredCapability;
    this.minProtocol = minProtocol;
  }
}

export function isUnsupportedCapabilityError(err: unknown): boolean {
  return (
    err instanceof APIError &&
    (err.status === 404 ||
      err.status === 405 ||
      err.status === 501 ||
      err.code === 'capability_unsupported')
  );
}

let unauthorizedHandler: (() => void) | null = null;

export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn;
}

export async function api<T = unknown>(
  path: string,
  opts: { method?: string; body?: unknown; signal?: AbortSignal } = {}
): Promise<T> {
  const headers: Record<string, string> = {
    'X-Pebble-Protocol': String(CLIENT_PROTOCOL),
    'X-Pebble-Client': 'web',
  };
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  const res = await fetch(path, {
    method: opts.method,
    headers,
    signal: opts.signal,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  if (res.status === 401 && path !== '/api/login') {
    unauthorizedHandler?.();
    throw new APIError('unauthorized', 401);
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new APIError(
      data.error || `HTTP ${res.status}`,
      res.status,
      data.code,
      data.requiredCapability,
      data.minProtocol
    );
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
    headers: {
      'X-Pebble-Protocol': String(CLIENT_PROTOCOL),
      'X-Pebble-Client': 'web',
    },
    body: form,
  });
  if (res.status === 401) {
    unauthorizedHandler?.();
    throw new APIError('unauthorized', 401);
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new APIError(
      data.error || `HTTP ${res.status}`,
      res.status,
      data.code,
      data.requiredCapability,
      data.minProtocol
    );
  }
  return data as { media: MessageMedia; workspacePath?: string };
}

export async function fetchWorkspaceFiles(
  botId: string,
  path = '',
  showHidden = false
): Promise<WorkspaceListResult> {
  const qs = new URLSearchParams();
  if (path) qs.set('path', path);
  if (showHidden) qs.set('showHidden', '1');
  const q = qs.toString();
  return api<WorkspaceListResult>(
    `/api/bots/${encodeURIComponent(botId)}/files${q ? `?${q}` : ''}`
  );
}

export async function fetchWorkspaceFileContent(
  botId: string,
  path: string
): Promise<WorkspaceFileContent> {
  const qs = new URLSearchParams({ path });
  return api<WorkspaceFileContent>(
    `/api/bots/${encodeURIComponent(botId)}/files/content?${qs.toString()}`
  );
}

export async function requestWorkspaceFileTicket(
  botId: string,
  path: string
): Promise<{ ticket: string; path: string; expiresIn: number }> {
  return api<{ ticket: string; path: string; expiresIn: number }>(
    `/api/bots/${encodeURIComponent(botId)}/files/ticket`,
    { method: 'POST', body: { path } }
  );
}

export function workspaceFileRawURL(
  botId: string,
  path: string,
  ticket: string,
  download = false
): string {
  const qs = new URLSearchParams({ path, ticket });
  if (download) qs.set('download', '1');
  return `/api/bots/${encodeURIComponent(botId)}/files/raw?${qs.toString()}`;
}

export async function fetchUpdateStatus(): Promise<UpdateStatus> {
  return api<UpdateStatus>('/api/update');
}

export async function checkForUpdates(): Promise<UpdateStatus> {
  return api<UpdateStatus>('/api/update/check', { method: 'POST', body: {} });
}

export async function saveUpdateConfig(patch: {
  autoCheck?: boolean;
  interval?: string;
  channel?: string;
  proxy?: string;
}): Promise<UpdateStatus> {
  return api<UpdateStatus>('/api/update/config', { method: 'PATCH', body: patch });
}

export async function fetchNodes(): Promise<NodeInfo[]> {
  const data = await api<{ nodes?: NodeInfo[] }>('/api/nodes');
  return data.nodes || [];
}

export async function fetchBotTasks(
  botId: string,
  opts: { kind?: string; status?: string; limit?: number } = {}
): Promise<BotTask[]> {
  const qs = new URLSearchParams();
  if (opts.kind) qs.set('kind', opts.kind);
  if (opts.status) qs.set('status', opts.status);
  if (opts.limit && opts.limit > 0) qs.set('limit', String(opts.limit));
  const q = qs.toString();
  const data = await api<{ tasks?: BotTask[] }>(
    `/api/bots/${encodeURIComponent(botId)}/tasks${q ? `?${q}` : ''}`
  );
  return data.tasks || [];
}

export async function spawnBotTask(botId: string, req: SpawnTaskRequest): Promise<BotTask> {
  return api<BotTask>(`/api/bots/${encodeURIComponent(botId)}/tasks`, {
    method: 'POST',
    body: req,
  });
}

export async function fetchTask(taskId: string): Promise<BotTask> {
  return api<BotTask>(`/api/tasks/${encodeURIComponent(taskId)}`);
}

export async function fetchTaskEvents(
  taskId: string,
  opts: { afterSeq?: number; limit?: number } = {}
): Promise<BotTaskEvent[]> {
  const qs = new URLSearchParams();
  if (typeof opts.afterSeq === 'number' && opts.afterSeq > 0) {
    qs.set('afterSeq', String(opts.afterSeq));
  }
  if (opts.limit && opts.limit > 0) qs.set('limit', String(opts.limit));
  const q = qs.toString();
  const data = await api<{ events?: BotTaskEvent[] }>(
    `/api/tasks/${encodeURIComponent(taskId)}/events${q ? `?${q}` : ''}`
  );
  return data.events || [];
}

export async function sendToTask(taskId: string, message: string): Promise<BotTask> {
  return api<BotTask>(`/api/tasks/${encodeURIComponent(taskId)}/send`, {
    method: 'POST',
    body: { message },
  });
}

export async function cancelTask(taskId: string): Promise<BotTask> {
  return api<BotTask>(`/api/tasks/${encodeURIComponent(taskId)}/cancel`, {
    method: 'POST',
  });
}
