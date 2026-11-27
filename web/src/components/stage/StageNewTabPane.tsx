import React, { useMemo } from 'react';
import type { BotTask, NodeInfo, ResolvedLang, StageKind } from '../../types';
import { translate } from '../../lib/i18n';
import {
  isTaskActive,
  resolveRemoteDeviceName,
  taskDisplayTitle,
  taskStatusTone,
  tasksForStageLauncher,
} from '../../lib/tasks';
import { StageIcon } from './StageIcon';

interface StageNewTabPaneProps {
  canUseFiles: boolean;
  tasks: BotTask[];
  nodes: NodeInfo[];
  lang: ResolvedLang;
  onOpenFiles: () => void;
  onOpenDesktop: () => void;
  onOpenTask: (task: BotTask) => void;
}

interface LauncherItem {
  id: string;
  kind: StageKind;
  title: string;
  desc: string;
  onClick: () => void;
}

export const StageNewTabPane: React.FC<StageNewTabPaneProps> = ({
  canUseFiles,
  tasks,
  nodes,
  lang,
  onOpenFiles,
  onOpenDesktop,
  onOpenTask,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const items: LauncherItem[] = [];
  if (canUseFiles) {
    items.push({
      id: 'files',
      kind: 'files',
      title: t('workspaceFiles'),
      desc: t('stageFilesTabDesc'),
      onClick: onOpenFiles,
    });
  }
  items.push({
    id: 'desktop',
    kind: 'desktop',
    title: t('remoteDesktop'),
    desc: t('stageDesktopTabDesc'),
    onClick: onOpenDesktop,
  });

  const launcherTasks = useMemo(() => tasksForStageLauncher(tasks, 8), [tasks]);

  return (
    <div className="stage-new-pane">
      <div className="stage-new-inner">
        <div className="stage-new-hd">
          <h2>{t('stageNewTab')}</h2>
          <p>{t('stageNewTabSubtitle')}</p>
        </div>
        <div className="stage-launcher-list">
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              className="stage-launcher-row"
              onClick={item.onClick}
            >
              <span className="stage-launcher-ico">
                <StageIcon kind={item.kind} />
              </span>
              <span className="stage-launcher-main">
                <span className="stage-launcher-title">{item.title}</span>
                <span className="stage-launcher-desc">{item.desc}</span>
              </span>
              <span className="stage-launcher-chev">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="m9 18 6-6-6-6" />
                </svg>
              </span>
            </button>
          ))}
        </div>

        {launcherTasks.length > 0 && (
          <div className="stage-new-tasks">
            <div className="stage-new-sec">
              <span>{t('stageTasksSection')}</span>
              <span className="stage-new-sec-cnt">{launcherTasks.length}</span>
            </div>
            <div className="stage-launcher-list">
              {launcherTasks.map((task) => {
                const active = isTaskActive(task.status);
                const tone = taskStatusTone(task.status);
                const remoteName = resolveRemoteDeviceName(task.deviceId, nodes);
                const statusKey = `taskStatus_${task.status}`;
                const statusLbl =
                  translate(lang, statusKey) === statusKey ? task.status : t(statusKey);
                return (
                  <button
                    key={task.id}
                    type="button"
                    className="stage-launcher-row stage-launcher-task"
                    onClick={() => onOpenTask(task)}
                  >
                    <span className="stage-launcher-ico">
                      <StageIcon kind="task" />
                    </span>
                    <span className="stage-launcher-main">
                      <span className="stage-launcher-title">{taskDisplayTitle(task)}</span>
                      <span className="stage-launcher-desc">
                        {[remoteName || t('taskLocalDevice'), task.harness, statusLbl]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </span>
                    <span className={`task-pill ${tone}`}>
                      {active && <span className="task-dot pulse" />}
                      <span>{statusLbl}</span>
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
