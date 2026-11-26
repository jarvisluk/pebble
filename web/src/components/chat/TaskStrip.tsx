import React, { useEffect, useMemo, useState } from 'react';
import type { BotTask, NodeInfo, ResolvedLang } from '../../types';
import { translate } from '../../lib/i18n';
import { formatDurationMs } from '../../lib/theme';
import {
  isRecentTaskForStrip,
  isTaskActive,
  isTaskTerminal,
  resolveRemoteDeviceName,
  taskDisplayTitle,
  taskElapsedMs,
  taskStatusTone,
} from '../../lib/tasks';

interface TaskStripProps {
  tasks: BotTask[];
  taskToolMap: Record<string, string>;
  nodes: NodeInfo[];
  lang: ResolvedLang;
  onInspectTask: (taskId: string) => void;
}

export const TaskStrip: React.FC<TaskStripProps> = ({
  tasks,
  taskToolMap,
  nodes,
  lang,
  onInspectTask,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [expanded, setExpanded] = useState(true);
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(() => new Set());
  const [nowMs, setNowMs] = useState(() => Date.now());

  const visibleTasks = useMemo(
    () =>
      tasks
        .filter((task) => !dismissedIds.has(task.id) && isRecentTaskForStrip(task, nowMs))
        .slice(0, 6),
    [tasks, dismissedIds, nowMs]
  );

  const hasActive = visibleTasks.some((task) => isTaskActive(task.status));

  useEffect(() => {
    if (!hasActive) return;
    setNowMs(Date.now());
    const id = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [hasActive]);

  if (visibleTasks.length === 0) return null;

  const activeCount = visibleTasks.filter((task) => isTaskActive(task.status)).length;
  const showSummary = visibleTasks.length >= 2;
  const showList = !showSummary || expanded;

  const summaryText =
    activeCount > 0
      ? t('taskStripSummary', { active: activeCount, total: visibleTasks.length })
      : t('taskStripSummaryDone', { total: visibleTasks.length });

  return (
    <div className="task-strip-wrap">
      <div className="task-strip" role="region" aria-label={t('tasksActivityTitle')}>
        {showSummary && (
          <div className="task-strip-hd">
            <div className="task-strip-summary">
              {activeCount > 0 && <span className="task-dot pulse" />}
              <span>{summaryText}</span>
            </div>
            <button
              type="button"
              className="task-strip-toggle"
              aria-expanded={expanded}
              onClick={() => setExpanded((v) => !v)}
            >
              <span>{expanded ? t('taskStripCollapse') : t('taskStripExpand')}</span>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                <path d="m6 9 6 6 6-6" />
              </svg>
            </button>
          </div>
        )}

        {showList && (
          <div className="task-strip-list">
            {visibleTasks.map((task) => {
              const tone = taskStatusTone(task.status);
              const active = isTaskActive(task.status);
              const terminal = isTaskTerminal(task.status);
              const remoteName = resolveRemoteDeviceName(task.deviceId, nodes);
              const liveTool = active ? taskToolMap[task.id] : '';
              const elapsed = formatDurationMs(taskElapsedMs(task, nowMs));
              const statusKey = `taskStatus_${task.status}`;
              const statusLabel = translate(lang, statusKey) === statusKey ? task.status : t(statusKey);
              const kindKey = `taskKind_${task.kind}`;
              const kindLabel = translate(lang, kindKey) === kindKey ? task.kind : t(kindKey);

              return (
                <div key={task.id} className="task-card-wrap">
                  <button
                    type="button"
                    className="task-card"
                    onClick={() => onInspectTask(task.id)}
                  >
                    <div className="task-card-main">
                      <div className="task-card-top">
                        <span className="task-card-title">{taskDisplayTitle(task)}</span>
                        <span className="task-pill">{kindLabel}</span>
                        {remoteName && <span className="task-pill device">{remoteName}</span>}
                      </div>
                      <div className="task-card-meta">
                        {task.harness && <span>{task.harness}</span>}
                        {liveTool && (
                          <span className="task-card-tool">
                            {t('taskRunningTool', { tool: liveTool })}
                          </span>
                        )}
                        {elapsed && <span>{elapsed}</span>}
                      </div>
                    </div>
                    <div className="task-card-acts">
                      <span className={`task-pill ${tone}`}>
                        {active && <span className="task-dot pulse" />}
                        <span>{statusLabel}</span>
                      </span>
                    </div>
                  </button>
                  {terminal && (
                    <button
                      type="button"
                      className="task-dismiss"
                      aria-label={t('taskStripDismiss')}
                      title={t('taskStripDismiss')}
                      onClick={() => {
                        setDismissedIds((prev) => {
                          const next = new Set(prev);
                          next.add(task.id);
                          return next;
                        });
                      }}
                    >
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round">
                        <path d="M6 6l12 12M18 6 6 18" />
                      </svg>
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};
