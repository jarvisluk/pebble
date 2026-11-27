import assert from 'node:assert/strict';
import test from 'node:test';
import { activeTasksForStrip, tasksForStageLauncher } from './tasks.ts';

test('activeTasksForStrip returns only active tasks and yields empty when idle', () => {
  const list = [
    { id: 't1', botId: 'b1', kind: 'worker', prompt: 'p1', status: 'completed', deviceId: 'local', delivery: 'receipt', createdAt: '2026-11-26T10:00:00Z' },
    { id: 't2', botId: 'b1', kind: 'worker', prompt: 'p2', status: 'running', deviceId: 'local', delivery: 'receipt', createdAt: '2026-11-26T10:01:00Z' },
    { id: 't3', botId: 'b1', kind: 'worker', prompt: 'p3', status: 'waiting_approval', deviceId: 'mac-mini', delivery: 'receipt', createdAt: '2026-11-26T10:02:00Z' },
    { id: 't4', botId: 'b1', kind: 'routine', prompt: 'p4', status: 'failed', deviceId: 'local', delivery: 'receipt', createdAt: '2026-11-26T10:03:00Z' },
  ];

  assert.deepEqual(
    activeTasksForStrip(list).map((x) => x.id),
    ['t2', 't3']
  );

  const idleOnly = list.filter((x) => x.status === 'completed' || x.status === 'failed');
  assert.equal(activeTasksForStrip(idleOnly).length, 0);
});

test('tasksForStageLauncher puts active tasks first followed by recent terminal tasks up to limit', () => {
  const list = [
    { id: 'done1', botId: 'b1', kind: 'worker', prompt: 'd1', status: 'completed', deviceId: 'local', delivery: 'receipt', createdAt: '2026-11-26T10:05:00Z' },
    { id: 'run1', botId: 'b1', kind: 'worker', prompt: 'r1', status: 'running', deviceId: 'local', delivery: 'receipt', createdAt: '2026-11-26T10:01:00Z' },
  ];
  assert.deepEqual(
    tasksForStageLauncher(list, 8).map((x) => x.id),
    ['run1', 'done1']
  );
});
