import React, { useEffect, useMemo, useState } from 'react';
import type {
  BotDetail,
  BotTask,
  BotTaskDelivery,
  NodeInfo,
  ResolvedLang,
  SettingsData,
} from '../../types';
import { fetchBotTasks, spawnBotTask } from '../../lib/api';
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
  settings: SettingsData | null;
  models: Record<string, string[]>;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onTasksLoaded: (botId: string, tasks: BotTask[]) => void;
  onTaskCreated: (task: BotTask) => void;
  onInspectTask: (taskId: string) => void;
  onLoadModels: (harness: string, force?: boolean) => Promise<unknown>;
}

export const BotTasksSection: React.FC<BotTasksSectionProps> = ({
  open,
  detail,
  tasks,
  nodes,
  settings,
  models,
  lang,
  onToast,
  onTasksLoaded,
  onTaskCreated,
  onInspectTask,
  onLoadModels,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [kindFilter, setKindFilter] = useState<TaskKindFilter>('all');
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState('');
  const [formOpen, setFormOpen] = useState(false);
  const [title, setTitle] = useState('');
  const [prompt, setPrompt] = useState('');
  const [deviceId, setDeviceId] = useState('local');
  const [harness, setHarness] = useState('');
  const [model, setModel] = useState('');
  const [delivery, setDelivery] = useState<BotTaskDelivery>('receipt');
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open || !detail.id) return;
    setFormOpen(false);
    setErr('');
    setLoading(true);
    fetchBotTasks(detail.id, { limit: 50 })
      .then((list) => onTasksLoaded(detail.id, list))
      .catch((e) => {
        if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
      })
      .finally(() => setLoading(false));
  }, [open, detail.id]);

  const connectedNodes = useMemo(() => nodes.filter((n) => n.connected), [nodes]);
  const selectedNode = useMemo(
    () => (deviceId !== 'local' ? connectedNodes.find((n) => n.id === deviceId) : undefined),
    [connectedNodes, deviceId]
  );

  const remoteLacksAgentPerm = Boolean(
    selectedNode && !(selectedNode.permissions || []).includes('agent')
  );

  const harnessOptions = useMemo(() => {
    if (selectedNode?.harnesses && selectedNode.harnesses.length > 0) {
      const installed = selectedNode.harnesses.filter((h) => h.installed);
      if (installed.length > 0) {
        return installed.map((h) => ({ id: h.kind, label: h.label || h.kind }));
      }
    }
    return (settings?.harnesses || []).map((h) => ({ id: h.id, label: h.label }));
  }, [selectedNode, settings?.harnesses]);

  const effectiveHarness = harness || detail.harness || settings?.defaultHarness || 'opencode';
  const modelList = models[effectiveHarness] || [];

  const filteredTasks = useMemo(() => {
    if (kindFilter === 'all') return tasks;
    return tasks.filter((item) => item.kind === kindFilter);
  }, [tasks, kindFilter]);

  const openNewForm = () => {
    setErr('');
    setTitle('');
    setPrompt('');
    setDeviceId('local');
    setHarness('');
    setModel('');
    setDelivery('receipt');
    setFormOpen(true);
    if (detail.harness) void onLoadModels(detail.harness);
  };

  const handleSpawn = async () => {
    const cleanPrompt = prompt.trim();
    if (!cleanPrompt) {
      setErr(t('taskPromptRequired'));
      return;
    }
    setSubmitting(true);
    setErr('');
    try {
      const created = await spawnBotTask(detail.id, {
        kind: 'worker',
        title: title.trim() || undefined,
        prompt: cleanPrompt,
        deviceId: deviceId || 'local',
        harness: harness || undefined,
        model: model.trim() || undefined,
        delivery,
      });
      onTaskCreated(created);
      onToast(t('taskCreatedToast'));
      setFormOpen(false);
      onInspectTask(created.id);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <div className="sec row-between">
        <span>{t('tasksActivityTitle')}</span>
        <button
          type="button"
          id="pTaskAddToggle"
          className="mp go"
          onClick={() => {
            if (!formOpen) openNewForm();
            else setFormOpen(false);
          }}
        >
          {t('newTaskBtn')}
        </button>
      </div>

      {formOpen && (
        <div id="pTaskFormBox" className="add-prov mem-add mb-8">
          <div className="fld">
            <label>{t('taskTitleLabel')}</label>
            <input
              id="pTaskTitle"
              placeholder={t('taskTitlePh')}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>
          <div className="fld">
            <label>{t('taskPromptLabel')}</label>
            <textarea
              id="pTaskPrompt"
              rows={3}
              className="ta-sm ta-font"
              placeholder={t('taskPromptPh')}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
            />
          </div>
          <div className="two">
            <div className="fld">
              <label>{t('taskDeviceLabel')}</label>
              <select
                id="pTaskDevice"
                value={deviceId}
                onChange={(e) => {
                  setDeviceId(e.target.value);
                  setHarness('');
                  setModel('');
                }}
              >
                <option value="local">{t('taskDeviceLocal')}</option>
                {connectedNodes.map((n) => (
                  <option key={n.id} value={n.id}>
                    {n.name || n.id}
                  </option>
                ))}
              </select>
            </div>
            <div className="fld">
              <label>{t('taskDeliveryLabel')}</label>
              <select
                id="pTaskDelivery"
                value={delivery}
                onChange={(e) => setDelivery(e.target.value as BotTaskDelivery)}
              >
                <option value="receipt">{t('taskDeliveryReceipt')}</option>
                <option value="wake">{t('taskDeliveryWake')}</option>
                <option value="silent">{t('taskDeliverySilent')}</option>
              </select>
            </div>
          </div>
          {remoteLacksAgentPerm && (
            <div className="set-card-desc">{t('taskDeviceNoAgentWarn')}</div>
          )}
          <div className="two">
            <div className="fld">
              <label>{t('statHarness')}</label>
              <select
                id="pTaskHarness"
                value={harness}
                onChange={(e) => {
                  const next = e.target.value;
                  setHarness(next);
                  setModel('');
                  void onLoadModels(next || detail.harness || settings?.defaultHarness || 'opencode');
                }}
              >
                <option value="">
                  {t('taskHarnessFollowBot', { harness: detail.harness || settings?.defaultHarness || 'opencode' })}
                </option>
                {harnessOptions.map((h) => (
                  <option key={h.id} value={h.id}>
                    {h.label}
                  </option>
                ))}
              </select>
            </div>
            <div className="fld">
              <label>{t('model')}</label>
              <input
                id="pTaskModel"
                className="mono"
                list="pTaskModelsList"
                placeholder={t('rtModelPh')}
                spellCheck={false}
                value={model}
                onFocus={() => void onLoadModels(effectiveHarness)}
                onChange={(e) => setModel(e.target.value)}
              />
              <datalist id="pTaskModelsList">
                {modelList.map((m) => (
                  <option key={m} value={m} />
                ))}
              </datalist>
            </div>
          </div>
          <div className="row-end">
            <button
              type="button"
              className="mp"
              onClick={() => {
                setFormOpen(false);
                setErr('');
              }}
            >
              {t('cancel')}
            </button>
            <button
              type="button"
              id="pTaskSubmit"
              className="mp go"
              disabled={submitting}
              onClick={() => void handleSpawn()}
            >
              {t('create')}
            </button>
          </div>
        </div>
      )}

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
