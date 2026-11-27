import assert from 'node:assert/strict';
import test from 'node:test';
import {
  closeTab,
  createDesktopTab,
  createFileTab,
  createFilesTab,
  createNewTab,
  createTaskTab,
  ensureNonEmptyStageState,
  replaceNewTabOrOpen,
  stageTabFromRef,
  upsertTab,
} from './stageTabs.ts';

const t = (k) => k;

test('stageTabFromRef normalizes files+path to file tab and preserves desktop/task/extension tabs', () => {
  const fileTab = stageTabFromRef({ kind: 'files', path: 'memory/notes.md' }, t, 'assistant');
  assert.equal(fileTab.kind, 'file');
  assert.equal(fileTab.id, 'file:memory/notes.md');
  assert.equal(fileTab.title, 'notes.md');
  assert.equal(fileTab.subtitle, 'memory/notes.md');

  const dirTab = stageTabFromRef({ kind: 'files' }, t, 'assistant');
  assert.equal(dirTab.kind, 'files');
  assert.equal(dirTab.id, 'files');

  const deskTab = stageTabFromRef({ kind: 'desktop', target: 'node-1' }, t, 'assistant');
  assert.equal(deskTab.kind, 'desktop');
  assert.equal(deskTab.id, 'desktop:node-1');
  assert.equal(deskTab.target, 'node-1');

  const taskTab = stageTabFromRef({ kind: 'task', taskId: 'tsk_42', title: 'Run tests' }, t, 'assistant');
  assert.equal(taskTab.kind, 'task');
  assert.equal(taskTab.id, 'task:tsk_42');
  assert.equal(taskTab.taskId, 'tsk_42');
  assert.equal(taskTab.title, 'Run tests');

  const directTaskTab = createTaskTab({ id: 'tsk_99', prompt: 'Refactor stage pane\nSecond line', kind: 'worker', deviceId: 'mac-mini' }, t);
  assert.equal(directTaskTab.id, 'task:tsk_99');
  assert.equal(directTaskTab.taskId, 'tsk_99');
  assert.equal(directTaskTab.title, 'Refactor stage pane');

  const extTab = stageTabFromRef({ id: 'b1', kind: 'browser', title: 'Docs', url: 'https://example.com' }, t);
  assert.equal(extTab.kind, 'browser');
  assert.equal(extTab.subtitle, 'https://example.com');
});

test('replaceNewTabOrOpen replaces active new tab or focuses existing singleton tab', () => {
  const n1 = createNewTab(t, '1');
  let st = ensureNonEmptyStageState({ tabs: [n1], activeTabId: n1.id }, t);

  const files = createFilesTab(t);
  st = replaceNewTabOrOpen(st, files);
  assert.equal(st.tabs.length, 1);
  assert.equal(st.activeTabId, 'files');

  const n2 = createNewTab(t, '2');
  st = upsertTab(st, n2, true);
  assert.equal(st.tabs.length, 2);
  assert.equal(st.activeTabId, n2.id);

  // Opening already-open 'files' from 'new' removes the 'new' tab and focuses 'files'.
  st = replaceNewTabOrOpen(st, createFilesTab(t));
  assert.equal(st.tabs.length, 1);
  assert.equal(st.activeTabId, 'files');
});

test('closeTab selects neighbor and falls back to new tab when closing the last tab', () => {
  let st = upsertTab(undefined, createFilesTab(t), true);
  st = upsertTab(st, createFileTab('a.md'), true);
  st = upsertTab(st, createDesktopTab(t), true);

  st = closeTab(st, 'desktop', t);
  assert.deepEqual(
    st.tabs.map((x) => x.id),
    ['files', 'file:a.md']
  );
  assert.equal(st.activeTabId, 'file:a.md');

  st = closeTab(st, 'file:a.md', t);
  assert.equal(st.activeTabId, 'files');

  st = closeTab(st, 'files', t);
  assert.equal(st.tabs.length, 1);
  assert.equal(st.tabs[0].kind, 'new');
  assert.equal(st.activeTabId, st.tabs[0].id);
});
