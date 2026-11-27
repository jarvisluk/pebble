import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type {
  BotStatus,
  BotTask,
  BotTaskEvent,
  ConfirmOptions,
  NodeInfo,
  PendingApproval,
  PendingQuestion,
  ResolvedLang,
  TakeoverRequest,
  TaskDiffResult,
} from '../../types';
import {
  cancelTask,
  fetchTask,
  fetchTaskDiff,
  fetchTaskEvents,
  rollbackTask,
  sendToTask,
} from '../../lib/api';
import { translate } from '../../lib/i18n';
import { renderMarkdownHtml } from '../../lib/markdown';
import { botColor, contrastText, formatClockTime, formatDurationMs } from '../../lib/theme';
import {
  isTaskActive,
  resolveRemoteDeviceName,
  taskDisplayTitle,
  taskElapsedMs,
  taskStatusTone,
} from '../../lib/tasks';
import { ApprovalCard } from '../ApprovalCard';
import { QuestionCard } from '../QuestionCard';

interface StageTaskPaneProps {
  taskId: string;
  task: BotTask | null;
  bots: BotStatus[];
  nodes: NodeInfo[];
  liveTool?: string;
  liveChunk?: string;
  taskRefreshSeq: number;
  approvals: PendingApproval[];
  questions: PendingQuestion[];
  takeovers: TakeoverRequest[];
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onConfirm: (opts: ConfirmOptions) => void;
  onTaskUpdated: (task: BotTask) => void;
  onReplyApproval: (
    id: string,
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    reason?: string
  ) => Promise<void>;
  onReplyQuestion: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
  onResolveTakeover: (to: TakeoverRequest) => Promise<void>;
  onOpenDesktopTarget: (target: string) => void;
}

export const StageTaskPane: React.FC<StageTaskPaneProps> = ({
  taskId,
  task,
  bots,
  nodes,
  liveTool,
  liveChunk = '',
  taskRefreshSeq,
  approvals,
  questions,
  takeovers,
  lang,
  onToast,
  onConfirm,
  onTaskUpdated,
  onReplyApproval,
  onReplyQuestion,
  onResolveTakeover,
  onOpenDesktopTarget,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [events, setEvents] = useState<BotTaskEvent[]>([]);
  const [loadingEvents, setLoadingEvents] = useState(false);
  const [liveTail, setLiveTail] = useState('');
  const [followUp, setFollowUp] = useState('');
  const [sending, setSending] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [diff, setDiff] = useState<TaskDiffResult | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);
  const [diffErr, setDiffErr] = useState('');
  const [rollingBack, setRollingBack] = useState(false);
  const [patchOpen, setPatchOpen] = useState(false);
  const [err, setErr] = useState('');
  const [nowMs, setNowMs] = useState(() => Date.now());

  const lastSeqRef = useRef(0);
  const prevGlobalChunkLenRef = useRef(0);

  const active = Boolean(task && isTaskActive(task.status));
  const hasWorktreeInfo = Boolean(
    task && (task.isolation === 'worktree' || task.checkpointRef || task.worktreePath)
  );

  useEffect(() => {
    if (!active) return;
    setNowMs(Date.now());
    const id = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [active]);

  const loadDiff = useCallback(async () => {
    if (!taskId) return;
    setDiffLoading(true);
    setDiffErr('');
    try {
      const res = await fetchTaskDiff(taskId);
      setDiff(res);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setDiffErr(e.message);
    } finally {
      setDiffLoading(false);
    }
  }, [taskId]);

  useEffect(() => {
    if (!taskId) {
      setEvents([]);
      setLiveTail('');
      setFollowUp('');
      setDiff(null);
      setDiffErr('');
      setPatchOpen(false);
      setErr('');
      lastSeqRef.current = 0;
      prevGlobalChunkLenRef.current = 0;
      return;
    }

    let cancelled = false;
    lastSeqRef.current = 0;
    prevGlobalChunkLenRef.current = liveChunk.length;
    setLiveTail('');
    setDiff(null);
    setDiffErr('');
    setPatchOpen(false);
    setErr('');
    setLoadingEvents(true);

    Promise.all([
      fetchTask(taskId).then((fresh) => {
        if (!cancelled) onTaskUpdated(fresh);
        return fresh;
      }),
      fetchTaskEvents(taskId, { limit: 500 }).then((evs) => {
        if (cancelled) return;
        setEvents(evs);
        const maxSeq = evs.reduce((acc, ev) => Math.max(acc, ev.seq), 0);
        lastSeqRef.current = maxSeq;
      }),
    ])
      .then(([fresh]) => {
        if (
          !cancelled &&
          fresh &&
          (fresh.isolation === 'worktree' || fresh.checkpointRef || fresh.worktreePath)
        ) {
          void loadDiff();
        }
      })
      .catch((e) => {
        if (!cancelled && e instanceof Error && e.message !== 'unauthorized') {
          setErr(e.message);
        }
      })
      .finally(() => {
        if (!cancelled) setLoadingEvents(false);
      });

    return () => {
      cancelled = true;
    };
  }, [taskId]);

  useEffect(() => {
    if (!taskId || taskRefreshSeq <= 0) return;
    let cancelled = false;
    fetchTaskEvents(taskId, { afterSeq: lastSeqRef.current, limit: 500 })
      .then((more) => {
        if (cancelled || more.length === 0) return;
        setEvents((prev) => {
          const existing = new Set(prev.map((x) => x.seq));
          const merged = [...prev];
          for (const ev of more) {
            if (!existing.has(ev.seq)) merged.push(ev);
          }
          return merged;
        });
        const maxSeq = more.reduce((acc, ev) => Math.max(acc, ev.seq), lastSeqRef.current);
        lastSeqRef.current = maxSeq;
        if (
          more.some(
            (ev) =>
              ev.eventType === 'message' ||
              (ev.eventType === 'status' &&
                typeof ev.payload === 'object' &&
                ev.payload !== null &&
                (ev.payload as Record<string, unknown>).status === 'completed')
          )
        ) {
          setLiveTail('');
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [taskId, taskRefreshSeq]);

  useEffect(() => {
    if (hasWorktreeInfo && !active) {
      void loadDiff();
    }
  }, [hasWorktreeInfo, active, task?.status, loadDiff]);

  useEffect(() => {
    if (!taskId) return;
    if (liveChunk.length > prevGlobalChunkLenRef.current) {
      const delta = liveChunk.slice(prevGlobalChunkLenRef.current);
      prevGlobalChunkLenRef.current = liveChunk.length;
      setLiveTail((prev) => prev + delta);
    } else if (liveChunk.length < prevGlobalChunkLenRef.current) {
      prevGlobalChunkLenRef.current = liveChunk.length;
      setLiveTail(liveChunk);
    }
  }, [taskId, liveChunk]);

  const botAccentStyle: React.CSSProperties | undefined = task
    ? ({
        '--bot-accent': botColor(task.botId, bots),
        '--bot-accent-text': contrastText(botColor(task.botId, bots)),
      } as React.CSSProperties)
    : undefined;

  const persistedTurnChunks = useMemo(() => {
    let lastAssistantIdx = -1;
    for (let i = events.length - 1; i >= 0; i--) {
      const ev = events[i];
      if (
        ev.eventType === 'message' &&
        typeof ev.payload === 'object' &&
        ev.payload !== null &&
        (ev.payload as Record<string, unknown>).role === 'assistant'
      ) {
        lastAssistantIdx = i;
        break;
      }
    }
    let text = '';
    for (let i = lastAssistantIdx + 1; i < events.length; i++) {
      const ev = events[i];
      if (ev.eventType === 'chunk' && typeof ev.payload === 'object' && ev.payload !== null) {
        const c = (ev.payload as Record<string, unknown>).chunk;
        if (typeof c === 'string') text += c;
      }
    }
    return text;
  }, [events]);

  const outputText = useMemo(() => {
    if (!task) return '';
    if (active) {
      return (persistedTurnChunks + liveTail).trim() || task.output || '';
    }
    return task.output || persistedTurnChunks || '';
  }, [task, active, persistedTurnChunks, liveTail]);

  const timelineEvents = useMemo(
    () => events.filter((ev) => ev.eventType !== 'chunk'),
    [events]
  );

  const matchingApproval = useMemo(
    () => (task ? approvals.find((a) => a.taskId === task.id) : undefined),
    [approvals, task]
  );

  const matchingQuestion = useMemo(
    () => (task ? questions.find((q) => q.taskId === task.id) : undefined),
    [questions, task]
  );

  const matchingTakeover = useMemo(
    () =>
      task
        ? takeovers.find(
            (to) =>
              to.taskId === task.id ||
              (task.kind === 'desktop' &&
                to.botId === task.botId &&
                (!task.deviceId || to.target === task.deviceId))
          )
        : undefined,
    [takeovers, task]
  );

  const handleCancel = async () => {
    if (!task || cancelling || !active) return;
    setCancelling(true);
    setErr('');
    try {
      const updated = await cancelTask(task.id);
      onTaskUpdated(updated);
      onToast(t('taskCancelledToast'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setCancelling(false);
    }
  };

  const handleRollback = () => {
    if (!task || rollingBack) return;
    onConfirm({
      title: t('taskRollbackConfirmTitle', { title: taskDisplayTitle(task) }),
      text: t('taskRollbackConfirmDesc'),
      okText: t('taskRollbackBtn'),
      okClass: 'bad',
      onOk: async () => {
        setRollingBack(true);
        setDiffErr('');
        try {
          await rollbackTask(task.id, false);
          onToast(t('taskRollbackDone'));
          const fresh = await fetchTask(task.id).catch(() => null);
          if (fresh) onTaskUpdated(fresh);
          await loadDiff();
        } catch (e) {
          if (e instanceof Error && e.message !== 'unauthorized') setDiffErr(e.message);
        } finally {
          setRollingBack(false);
        }
      },
    });
  };

  const handleSendFollowUp = async (e: React.FormEvent) => {
    e.preventDefault();
    const msg = followUp.trim();
    if (!task || !msg || sending || task.status === 'cancelled') return;
    setSending(true);
    setErr('');
    try {
      const updated = await sendToTask(task.id, msg);
      onTaskUpdated(updated);
      setFollowUp('');
      const more = await fetchTaskEvents(task.id, { afterSeq: lastSeqRef.current, limit: 200 });
      if (more.length > 0) {
        setEvents((prev) => [...prev, ...more]);
        lastSeqRef.current = more.reduce((acc, ev) => Math.max(acc, ev.seq), lastSeqRef.current);
      }
    } catch (e2) {
      if (e2 instanceof Error && e2.message !== 'unauthorized') setErr(e2.message);
    } finally {
      setSending(false);
    }
  };

  const formatEventText = (ev: BotTaskEvent): string => {
    const p =
      typeof ev.payload === 'object' && ev.payload !== null
        ? (ev.payload as Record<string, unknown>)
        : {};
    switch (ev.eventType) {
      case 'status': {
        const st = String(p.status || '');
        const lbl =
          translate(lang, `taskStatus_${st}`) === `taskStatus_${st}` ? st : t(`taskStatus_${st}`);
        return t('taskEventStatus', { status: lbl });
      }
      case 'tool_use':
        return t('taskEventToolUse', { tool: String(p.tool || '') });
      case 'approval':
        return String(
          p.text ||
            t('taskEventApproval', {
              decision: String(p.decision || ''),
              tool: String(p.toolName || ''),
            })
        );
      case 'question':
        return t('taskEventQuestion', { title: String(p.title || t('questionTitle')) });
      case 'takeover':
        return t('taskEventTakeover', { state: String(p.state || 'requested') });
      case 'error':
        return t('taskEventError', { error: String(p.error || '') });
      case 'message': {
        const role = String(p.role || '');
        const txt = String(p.text || '').trim();
        const prefix = role === 'user' ? t('taskEventUserMsg') : t('taskEventAssistantMsg');
        return txt ? `${prefix}: ${txt.slice(0, 140)}${txt.length > 140 ? '…' : ''}` : prefix;
      }
      default:
        return ev.eventType;
    }
  };

  const remoteDevice = task ? resolveRemoteDeviceName(task.deviceId, nodes) : '';
  const statusKey = task ? `taskStatus_${task.status}` : '';
  const statusLabel = task
    ? translate(lang, statusKey) === statusKey
      ? task.status
      : t(statusKey)
    : '';
  const kindKey = task ? `taskKind_${task.kind}` : '';
  const kindLabel = task
    ? translate(lang, kindKey) === kindKey
      ? task.kind
      : t(kindKey)
    : '';
  const tone = task ? taskStatusTone(task.status) : 'busy';
  const elapsed = task ? formatDurationMs(taskElapsedMs(task, nowMs)) : '';

  return (
    <div className="stage-file-wrap" style={botAccentStyle}>
      <div className="stage-task-card">
        <div className="stage-task-body">
          <div className="stage-task-inner">
            <div className="stage-task-hd">
              <div className="stage-task-hd-main">
                <div className="stage-task-title" title={task ? taskDisplayTitle(task) : taskId}>
                  {task ? taskDisplayTitle(task) : taskId}
                </div>
                {task && (
                  <div className="task-drawer-badges">
                    <span className={`task-pill ${tone}`}>
                      {active && <span className="task-dot pulse" />}
                      <span>{statusLabel}</span>
                    </span>
                    <span className="task-pill">{kindLabel}</span>
                    <span className={`task-pill${remoteDevice ? ' device' : ''}`}>
                      {remoteDevice || t('taskLocalDevice')}
                    </span>
                    {liveTool && active && (
                      <span className="task-pill">{t('taskRunningTool', { tool: liveTool })}</span>
                    )}
                  </div>
                )}
              </div>
              {task && active && (
                <button
                  type="button"
                  className="mp bad"
                  disabled={cancelling}
                  onClick={() => void handleCancel()}
                >
                  {t('taskCancelBtn')}
                </button>
              )}
            </div>

            {!task ? (
              <div className="ws-empty">{err || t('loadingEllipsis')}</div>
            ) : (
              <>
                {task.status === 'waiting_approval' &&
                  (matchingApproval ? (
                    <ApprovalCard approval={matchingApproval} lang={lang} onReply={onReplyApproval} />
                  ) : (
                    <div className="note">{t('taskWaitingApprovalHint')}</div>
                  ))}

                {task.status === 'waiting_question' &&
                  (matchingQuestion ? (
                    <QuestionCard question={matchingQuestion} lang={lang} onReply={onReplyQuestion} />
                  ) : (
                    <div className="note">{t('taskWaitingQuestionHint')}</div>
                  ))}

                {task.status === 'waiting_takeover' && (
                  <div className="task-drawer-takeover">
                    <div className="task-drawer-takeover-msg">
                      {matchingTakeover?.reason || t('taskWaitingTakeoverHint')}
                    </div>
                    <div className="task-drawer-takeover-acts">
                      <button
                        type="button"
                        className="btn"
                        onClick={() =>
                          onOpenDesktopTarget(matchingTakeover?.target || task.deviceId || 'local')
                        }
                      >
                        {t('taskOpenDesktop')}
                      </button>
                      {matchingTakeover && (
                        <button
                          type="button"
                          className="btn go"
                          onClick={() => void onResolveTakeover(matchingTakeover)}
                        >
                          {t('taskResumeAI')}
                        </button>
                      )}
                    </div>
                  </div>
                )}

                {task.error && <div className="task-drawer-err">{task.error}</div>}
                {err && <div className="msg-err">{err}</div>}

                <dl className="kv stage-task-kv">
                  <div>
                    <dt>{t('taskMetaDevice')}</dt>
                    <dd>{remoteDevice || t('taskLocalDevice')}</dd>
                  </div>
                  {task.harness && (
                    <div>
                      <dt>{t('taskMetaHarness')}</dt>
                      <dd>{task.harness}</dd>
                    </div>
                  )}
                  {task.model && (
                    <div>
                      <dt>{t('taskMetaModel')}</dt>
                      <dd>{task.model}</dd>
                    </div>
                  )}
                  {task.permissionMode && (
                    <div>
                      <dt>{t('taskMetaMode')}</dt>
                      <dd>
                        {translate(lang, `permMode_${task.permissionMode}`) ===
                        `permMode_${task.permissionMode}`
                          ? task.permissionMode
                          : t(`permMode_${task.permissionMode}`)}
                      </dd>
                    </div>
                  )}
                  {task.isolation && (
                    <div>
                      <dt>{t('taskMetaIsolation')}</dt>
                      <dd>{task.isolation}</dd>
                    </div>
                  )}
                  {(diff?.branch || task.branch) && (
                    <div>
                      <dt>{t('taskMetaBranch')}</dt>
                      <dd className="mono">{diff?.branch || task.branch}</dd>
                    </div>
                  )}
                  {task.workdir && (
                    <div>
                      <dt>{t('taskMetaWorkdir')}</dt>
                      <dd className="mono">{task.workdir}</dd>
                    </div>
                  )}
                  <div>
                    <dt>{t('taskMetaDelivery')}</dt>
                    <dd>{task.delivery}</dd>
                  </div>
                  {elapsed && (
                    <div>
                      <dt>{t('taskMetaDuration')}</dt>
                      <dd>{elapsed}</dd>
                    </div>
                  )}
                  {(task.costUsd || 0) > 0 && (
                    <div>
                      <dt>{t('taskMetaCost')}</dt>
                      <dd>${Number(task.costUsd).toFixed(4)}</dd>
                    </div>
                  )}
                  {((task.inputTokens || 0) > 0 || (task.outputTokens || 0) > 0) && (
                    <div>
                      <dt>{t('taskMetaTokens')}</dt>
                      <dd>{`${task.inputTokens || 0} / ${task.outputTokens || 0}`}</dd>
                    </div>
                  )}
                </dl>

                <div className="sec">{t('taskPromptLabel')}</div>
                <div className="task-drawer-prompt">{task.prompt}</div>

                <div className="sec">{t('taskOutputLabel')}</div>
                {outputText ? (
                  <div
                    className="md stage-task-output"
                    dangerouslySetInnerHTML={{
                      __html: renderMarkdownHtml(outputText, t('copy'), {
                        details: t('mdDetails'),
                        image: t('mdImage'),
                      }),
                    }}
                  />
                ) : (
                  <div className="task-drawer-empty">{t('taskNoOutput')}</div>
                )}

                {hasWorktreeInfo && (
                  <section className="stage-task-diff">
                    <div className="sec row-between">
                      <span>{t('taskDiffTitle')}</span>
                      <div className="row-inline-sm">
                        <button
                          type="button"
                          className="mp"
                          disabled={diffLoading}
                          onClick={() => void loadDiff()}
                        >
                          {t('taskDiffRefresh')}
                        </button>
                        {!active && (diff?.checkpointRef || task.checkpointRef) && (
                          <button
                            type="button"
                            className="mp bad"
                            disabled={rollingBack}
                            onClick={handleRollback}
                          >
                            {t('taskRollbackBtn')}
                          </button>
                        )}
                      </div>
                    </div>
                    {diffErr && <div className="msg-err">{diffErr}</div>}
                    {diffLoading && !diff ? (
                      <div className="task-drawer-empty">{t('loadingEllipsis')}</div>
                    ) : !diff || (diff.files.length === 0 && !diff.patch) ? (
                      <div className="task-drawer-empty">{t('taskDiffEmpty')}</div>
                    ) : (
                      <div className="stage-diff-box">
                        {diff.files.length > 0 && (
                          <div className="stage-diff-files">
                            {diff.files.map((f) => (
                              <div key={f.path} className="stage-diff-file">
                                <span className="stage-diff-st">{f.status || 'M'}</span>
                                <span className="stage-diff-path">{f.path}</span>
                                <span className="stage-diff-stat">
                                  {typeof f.additions === 'number' && f.additions > 0
                                    ? `+${f.additions}`
                                    : ''}
                                  {typeof f.deletions === 'number' && f.deletions > 0
                                    ? ` -${f.deletions}`
                                    : ''}
                                </span>
                              </div>
                            ))}
                          </div>
                        )}
                        {diff.patch && (
                          <>
                            <button
                              type="button"
                              className="mp"
                              onClick={() => setPatchOpen((v) => !v)}
                            >
                              {patchOpen ? t('taskDiffHidePatch') : t('taskDiffShowPatch')}
                            </button>
                            {patchOpen && <pre className="stage-diff-patch">{diff.patch}</pre>}
                          </>
                        )}
                      </div>
                    )}
                  </section>
                )}

                <div className="sec">{t('taskTimelineLabel')}</div>
                {loadingEvents && timelineEvents.length === 0 ? (
                  <div className="task-drawer-empty">{t('loadingEllipsis')}</div>
                ) : timelineEvents.length === 0 ? (
                  <div className="task-drawer-empty">{t('taskNoEvents')}</div>
                ) : (
                  <div className="task-timeline">
                    {timelineEvents.map((ev) => (
                      <div key={ev.seq} className={`task-ev${ev.eventType === 'error' ? ' err' : ''}`}>
                        <span className="task-ev-txt">{formatEventText(ev)}</span>
                        <span className="task-ev-time">{formatClockTime(ev.createdAt)}</span>
                      </div>
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        </div>

        {task && task.status !== 'cancelled' && (
          <form className="stage-task-foot" onSubmit={(e) => void handleSendFollowUp(e)}>
            <input
              type="text"
              placeholder={t('taskFollowUpPh')}
              disabled={sending}
              value={followUp}
              onChange={(e) => setFollowUp(e.target.value)}
            />
            <button type="submit" className="btn go" disabled={sending || !followUp.trim()}>
              {sending ? t('taskSending') : t('taskSendBtn')}
            </button>
          </form>
        )}
      </div>
    </div>
  );
};
