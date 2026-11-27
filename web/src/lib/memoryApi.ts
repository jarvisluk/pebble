import type {
  MemoryDreamResponse,
  MemoryImportResponse,
  MemoryIndexDoc,
  MemoryKind,
  MemoryPatchResponse,
  MemoryResponse,
  MemorySaveResponse,
  MemoryStatus,
  MemoryStatusFilter,
  MemoryReindexResponse,
} from '../types';
import { api } from './api';

export const MEMORY_KINDS: readonly [MemoryKind, string][] = [
  ['fact', 'kindFact'],
  ['preference', 'kindPreference'],
  ['rule', 'kindRule'],
  ['decision', 'kindDecision'],
  ['reference', 'kindReference'],
  ['profile', 'kindProfile'],
  ['procedure', 'kindProcedure'],
  ['episode', 'kindEpisode'],
] as const;

export const MEMORY_KIND_LABEL_MAP: Record<string, string> = Object.fromEntries(MEMORY_KINDS);

export function parseTagsInput(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split(/[,，]/)) { // i18n:allow-cjk
    const tag = part.trim();
    if (!tag || seen.has(tag)) continue;
    seen.add(tag);
    out.push(tag);
  }
  return out;
}

export function formatMemoryStamp(iso?: string): string {
  if (!iso) return '';
  return iso.replace('T', ' ').slice(0, 16);
}

export function countLines(text: string): number {
  if (!text) return 0;
  return text.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n').length;
}

export interface ListMemoriesParams {
  scope?: 'global' | 'bot' | 'all';
  botId?: string;
  query?: string;
  kind?: string;
  tags?: string;
  status?: MemoryStatusFilter;
  limit?: number;
}

export async function listMemories(opts: ListMemoriesParams = {}): Promise<MemoryResponse> {
  const params = new URLSearchParams();
  if (opts.scope) params.set('scope', opts.scope);
  if (opts.botId) params.set('botId', opts.botId);
  if (opts.query?.trim()) params.set('query', opts.query.trim());
  if (opts.kind) params.set('kind', opts.kind);
  if (opts.tags) params.set('tags', opts.tags);
  if (opts.status) params.set('status', opts.status);
  params.set('limit', String(opts.limit ?? 50));
  return api<MemoryResponse>(`/api/memory?${params.toString()}`);
}

export interface SaveMemoryPayload {
  id?: string;
  scope?: 'global' | 'bot';
  botId?: string;
  kind?: string;
  title?: string;
  text: string;
  why?: string;
  tags?: string[];
  pin?: boolean;
  status?: string;
  validFrom?: string;
  daily?: boolean;
}

export async function saveMemory(body: SaveMemoryPayload): Promise<MemorySaveResponse> {
  return api<MemorySaveResponse>('/api/memory', {
    method: 'POST',
    body,
  });
}

export interface PatchMemoryPayload {
  title?: string;
  text?: string;
  kind?: string;
  tags?: string[];
  why?: string;
  pin?: boolean;
  status?: string;
  validFrom?: string;
  validUntil?: string;
}

export async function patchMemory(id: string, body: PatchMemoryPayload): Promise<MemoryPatchResponse> {
  return api<MemoryPatchResponse>(`/api/memory/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body,
  });
}

export async function deleteMemory(
  id: string,
  opts?: { hard?: boolean; dismiss?: boolean }
): Promise<{ ok?: boolean }> {
  const params = new URLSearchParams();
  if (opts?.hard) params.set('hard', '1');
  if (opts?.dismiss) params.set('dismiss', '1');
  const qs = params.toString();
  return api<{ ok?: boolean }>(`/api/memory/${encodeURIComponent(id)}${qs ? `?${qs}` : ''}`, {
    method: 'DELETE',
  });
}

export async function fetchMemoryIndex(opts: {
  scope?: 'global';
  botId?: string;
}): Promise<MemoryIndexDoc> {
  const params = new URLSearchParams();
  if (opts.botId) params.set('botId', opts.botId);
  else params.set('scope', opts.scope || 'global');
  return api<MemoryIndexDoc>(`/api/memory/index?${params.toString()}`);
}

export async function saveMemoryIndex(body: {
  scope?: 'global';
  botId?: string;
  content: string;
}): Promise<MemoryIndexDoc> {
  return api<MemoryIndexDoc>('/api/memory/index', {
    method: 'PUT',
    body,
  });
}

export async function fetchMemoryStatus(botId?: string): Promise<MemoryStatus> {
  const qs = botId ? `?botId=${encodeURIComponent(botId)}` : '';
  return api<MemoryStatus>(`/api/memory/status${qs}`);
}

export async function triggerMemoryDream(botId?: string): Promise<MemoryDreamResponse> {
  return api<MemoryDreamResponse>('/api/memory/dream', {
    method: 'POST',
    body: botId ? { botId } : {},
  });
}

export async function triggerMemoryReindex(): Promise<MemoryReindexResponse> {
  return api<MemoryReindexResponse>('/api/memory/reindex', {
    method: 'POST',
    body: {},
  });
}

export async function importFromMagpie(body: {
  mode: 'bots_only' | 'all';
  botId?: string;
}): Promise<MemoryImportResponse> {
  return api<MemoryImportResponse>('/api/memory/import/magpie', {
    method: 'POST',
    body,
  });
}
