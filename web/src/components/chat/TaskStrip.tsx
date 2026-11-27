import React, { useEffect, useMemo, useState } from 'react';
import type { BotTask, NodeInfo, ResolvedLang } from '../../types';
import { translate } from '../../lib/i18n';
import { formatDurationMs } from '../../lib/theme';
import {
  activeTasksForStrip,
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
  const [expanded, setExpanded] = useState(false);
  const [nowMs, setNowMs] = useState(() => Date.now());

  const activeTasks = useMemo(() => activeTasksForStrip(tasks, 6), [tasks]);

  useEffect(() => {
    if (activeTasks.length === 0) return;
    setNowMs(Date.now());
    const id = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [activeTasks.length]);

  if (activeTasks.length === 0) return null;

  const showSummary = activeTasks.length >= 2;
  const showList = !showSummary || expanded;

  return (
    <div className="task-strip-wrap">
      <div className="task-strip" role="region" aria-label={t('tasksActivityTitle')}>
        {showSummary && (
          <div className="task-strip-hd">
            <button
              type="button"
              className="task-strip-summary"
              onClick={() => setExpanded((v) => !v)}
            >
              <span className="task-dot pulse" />
              <span>{t('taskStripActiveOnly', { active: activeTasks.length })}</span>
            </button>
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
            {activeTasks.map((task) => {
              const tone = taskStatusTone(task.status);
              const remoteName = resolveRemoteDeviceName(task.deviceId, nodes);
              const liveTool = taskToolMap[task.id] || '';
              const elapsed = formatDurationMs(taskElapsedMs(task, nowMs));
              const statusKey = `taskStatus_${task.status}`;
              const statusLabel = translate(lang, statusKey) === statusKey ? task.status : t(statusKey);

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
                        <span className="task-dot pulse" />
                        <span>{statusLabel}</span>
                      </span>
                    </div>
                  </button>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};
