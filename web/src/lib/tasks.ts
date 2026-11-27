import type { BotTask, NodeInfo } from '../types';

export function isTaskTerminal(status?: string): boolean {
  return status === 'completed' || status === 'failed' || status === 'cancelled';
}

export function isTaskWaiting(status?: string): boolean {
  return (
    status === 'waiting_approval' ||
    status === 'waiting_question' ||
    status === 'waiting_takeover'
  );
}

export function isTaskActive(status?: string): boolean {
  return !isTaskTerminal(status);
}

export type TaskStatusTone = 'ok' | 'busy' | 'bad';

export function taskStatusTone(status?: string): TaskStatusTone {
  switch (status) {
    case 'running':
    case 'completed':
      return 'ok';
    case 'failed':
    case 'cancelled':
      return 'bad';
    default:
      return 'busy';
  }
}

export function taskElapsedMs(task: BotTask, nowMs = Date.now()): number {
  if (isTaskTerminal(task.status) && task.durationMs && task.durationMs > 0) {
    return task.durationMs;
  }
  const startRaw = task.startedAt || task.createdAt;
  const start = Date.parse(startRaw);
  if (Number.isNaN(start)) {
    return task.durationMs || 0;
  }
  if (isTaskTerminal(task.status) && task.finishedAt) {
    const end = Date.parse(task.finishedAt);
    if (!Number.isNaN(end) && end >= start) {
      return end - start;
    }
  }
  return Math.max(0, nowMs - start);
}

export function activeTasksForStrip(tasks: BotTask[], limit = 6): BotTask[] {
  return tasks.filter((task) => isTaskActive(task.status)).slice(0, limit);
}

export function tasksForStageLauncher(tasks: BotTask[], limit = 8): BotTask[] {
  const active = tasks.filter((t) => isTaskActive(t.status));
  const recent = tasks.filter((t) => isTaskTerminal(t.status));
  return [...active, ...recent].slice(0, limit);
}

export function taskDisplayTitle(task: BotTask): string {
  const title = (task.title || task.routineName || '').trim();
  if (title) return title;
  const line = (task.prompt || '').trim().split(/\r?\n/)[0] || task.id;
  return line.length > 80 ? `${line.slice(0, 77)}…` : line;
}

export function resolveRemoteDeviceName(deviceId: string | undefined, nodes: NodeInfo[]): string {
  if (!deviceId || deviceId === 'local') return '';
  const found = nodes.find((n) => n.id === deviceId);
  return found?.name || deviceId;
}

export function upsertTaskList(list: BotTask[], task: BotTask): BotTask[] {
  const idx = list.findIndex((t) => t.id === task.id);
  const next = idx >= 0 ? list.map((t, i) => (i === idx ? task : t)) : [task, ...list];
  return next.sort((a, b) => {
    const ta = Date.parse(a.createdAt) || 0;
    const tb = Date.parse(b.createdAt) || 0;
    return tb - ta;
  });
}
