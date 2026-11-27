import React, { useEffect, useMemo, useState } from 'react';
import type { BotDetail, BotTask, NodeInfo, ResolvedLang } from '../../types';
import { fetchBotTasks } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { formatDurationMs, formatShortTime } from '../../lib/theme';
import {
  isTaskActive,
  resolveRemoteDeviceName,
  taskDisplayTitle,
  taskElapsedMs,
  taskStatusTone,
} from '../../lib/tasks';

type TaskKindFilter = 'all' | 'worker' | 'routine' | 'desktop';

interface BotTasksSectionProps {
  open: boolean;
  detail: BotDetail;
  tasks: BotTask[];
  nodes: NodeInfo[];
  lang: ResolvedLang;
  onTasksLoaded: (botId: string, tasks: BotTask[]) => void;
  onInspectTask: (taskId: string) => void;
}

export const BotTasksSection: React.FC<BotTasksSectionProps> = ({
  open,
  detail,
  tasks,
  nodes,
  lang,
  onTasksLoaded,
  onInspectTask,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [kindFilter, setKindFilter] = useState<TaskKindFilter>('all');
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (!open || !detail.id) return;
    setErr('');
    setLoading(true);
    fetchBotTasks(detail.id, { limit: 30 })
      .then((list) => onTasksLoaded(detail.id, list))
      .catch((e) => {
        if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
      })
      .finally(() => setLoading(false));
  }, [open, detail.id]);

  const filteredTasks = useMemo(() => {
    const base = kindFilter === 'all' ? tasks : tasks.filter((item) => item.kind === kindFilter);
    return base.slice(0, 6);
  }, [tasks, kindFilter]);

  return (
    <>
      <div className="sec row-between">
        <span>{t('tasksActivityTitle')}</span>
      </div>

      <div className="mem-filters mb-6">
        <div className="mem-scope-seg" role="tablist" aria-label={t('tasksActivityTitle')}>
          {(
            [
              ['all', 'taskFilterAll'],
              ['worker', 'taskFilterWorkers'],
              ['routine', 'taskFilterRoutines'],
              ['desktop', 'taskFilterDesktop'],
            ] as const
          ).map(([kind, labelKey]) => (
            <button
              key={kind}
              type="button"
              role="tab"
              aria-selected={kindFilter === kind}
              className={`mp${kindFilter === kind ? ' on' : ''}`}
              onClick={() => setKindFilter(kind)}
            >
              {t(labelKey)}
            </button>
          ))}
        </div>
      </div>

      <div id="pTaskList" className="stack6">
        {loading && filteredTasks.length === 0 ? (
          <div className="set-card-desc">{t('loadingEllipsis')}</div>
        ) : filteredTasks.length === 0 ? (
          <div className="set-card-desc">{t('tasksEmpty')}</div>
        ) : (
          filteredTasks.map((task) => {
            const tone = taskStatusTone(task.status);
            const active = isTaskActive(task.status);
            const remoteName = resolveRemoteDeviceName(task.deviceId, nodes);
            const statusKey = `taskStatus_${task.status}`;
            const statusLabel = translate(lang, statusKey) === statusKey ? task.status : t(statusKey);
            const kindKey = `taskKind_${task.kind}`;
            const kindLabel = translate(lang, kindKey) === kindKey ? task.kind : t(kindKey);
            const elapsed = formatDurationMs(taskElapsedMs(task));
            const cost = task.costUsd ? `$${Number(task.costUsd).toFixed(4)}` : '';
            const stamp = formatShortTime(task.finishedAt || task.createdAt, lang);
            return (
              <button
                key={task.id}
                type="button"
                className="task-act-row"
                onClick={() => onInspectTask(task.id)}
              >
                <div className="task-act-top">
                  <span className="task-act-title">{taskDisplayTitle(task)}</span>
                  <span className={`task-pill ${tone}`}>
                    {active && <span className="task-dot pulse" />}
                    <span>{statusLabel}</span>
                  </span>
                </div>
                <div className="task-act-meta">
                  <span className="task-pill">{kindLabel}</span>
                  {remoteName && <span className="task-pill device">{remoteName}</span>}
                  {task.harness && <span>{task.harness}</span>}
                  {elapsed && <span>{elapsed}</span>}
                  {cost && <span>{cost}</span>}
                  {stamp && <span>{stamp}</span>}
                </div>
              </button>
            );
          })
        )}
      </div>
      {err && <div className="msg-err mt-6">{err}</div>}
    </>
  );
};
