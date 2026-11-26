import { useCallback, useEffect } from 'react';
import type React from 'react';
import type {
  ActiveDesktopTarget,
  BotDetail,
  BotStatus,
  BotTask,
  ChatMessage,
  NodeInfo,
  PendingApproval,
  PendingQuestion,
  ScreenState,
  SettingsData,
  StageOpenEvent,
  TakeoverRequest,
  UpdateStatus,
} from '../types';
import { isTaskTerminal, upsertTaskList } from '../lib/tasks';

export interface UseHubEventsOpts {
  esRef: React.MutableRefObject<EventSource | null>;
  lastEventIdRef: React.MutableRefObject<number>;
  activeBotIdRef: React.MutableRefObject<string | null>;
  deskOpenRef: React.MutableRefObject<boolean>;
  deskTargetRef: React.MutableRefObject<string>;
  panelOpenRef: React.MutableRefObject<boolean>;
  panelDetailRef: React.MutableRefObject<BotDetail | null>;
  unboxAutoPromptedRef: React.MutableRefObject<boolean>;
  loginOpenRef: React.MutableRefObject<boolean>;
  loadBots: () => Promise<void>;
  loadSettings: (fresh?: boolean) => Promise<SettingsData>;
  refreshNodes: () => Promise<void>;
  refreshTakeovers: () => Promise<void>;
  refreshActiveDesktopTarget: () => Promise<void>;
  refreshApprovals: () => Promise<void>;
  refreshQuestions: () => Promise<void>;
  refreshUpdateStatus: () => Promise<void>;
  removeBotLocally: (id: string) => void;
  applyActiveDesktopTarget: (next: ActiveDesktopTarget, notifyFallback?: boolean) => void;
  onStageOpenEvent: (botId: string, payload: StageOpenEvent) => void;
  setMsgsMap: React.Dispatch<React.SetStateAction<Record<string, ChatMessage[]>>>;
  setLastMap: React.Dispatch<React.SetStateAction<Record<string, ChatMessage>>>;
  setUnreadMap: React.Dispatch<React.SetStateAction<Record<string, number>>>;
  setToolMap: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  setBots: React.Dispatch<React.SetStateAction<BotStatus[]>>;
  setTasksMap: React.Dispatch<React.SetStateAction<Record<string, BotTask[]>>>;
  setTaskToolMap: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  setTaskChunksMap: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  setTaskEventSeqMap: React.Dispatch<React.SetStateAction<Record<string, number>>>;
  setNodesFromEvents: React.Dispatch<React.SetStateAction<NodeInfo[] | null>>;
  setLiveScreen: React.Dispatch<React.SetStateAction<ScreenState | null>>;
  setScreenSeq: React.Dispatch<React.SetStateAction<number>>;
  setTakeovers: React.Dispatch<React.SetStateAction<TakeoverRequest[]>>;
  setQuestions: React.Dispatch<React.SetStateAction<PendingQuestion[]>>;
  setApprovals: React.Dispatch<React.SetStateAction<PendingApproval[]>>;
  setRoutineRefreshSeq: React.Dispatch<React.SetStateAction<number>>;
  setUpdateStatus: React.Dispatch<React.SetStateAction<UpdateStatus | null>>;
  setUnboxStep: React.Dispatch<React.SetStateAction<1 | 2 | 3>>;
  setUnboxOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setBootReady: React.Dispatch<React.SetStateAction<boolean>>;
}

export function useHubEvents(opts: UseHubEventsOpts) {
  const {
    esRef,
    lastEventIdRef,
    activeBotIdRef,
    deskOpenRef,
    deskTargetRef,
    panelOpenRef,
    panelDetailRef,
    unboxAutoPromptedRef,
    loginOpenRef,
    loadBots,
    loadSettings,
    refreshNodes,
    refreshTakeovers,
    refreshActiveDesktopTarget,
    refreshApprovals,
    refreshQuestions,
    refreshUpdateStatus,
    removeBotLocally,
    applyActiveDesktopTarget,
    onStageOpenEvent,
    setMsgsMap,
    setLastMap,
    setUnreadMap,
    setToolMap,
    setBots,
    setTasksMap,
    setTaskToolMap,
    setTaskChunksMap,
    setTaskEventSeqMap,
    setNodesFromEvents,
    setLiveScreen,
    setScreenSeq,
    setTakeovers,
    setQuestions,
    setApprovals,
    setRoutineRefreshSeq,
    setUpdateStatus,
    setUnboxStep,
    setUnboxOpen,
    setBootReady,
  } = opts;

  const connectEvents = useCallback(() => {
    esRef.current?.close();
    const cursor = lastEventIdRef.current;
    const url = cursor > 0 ? `/api/events?since=${cursor}` : '/api/events';
    const es = new EventSource(url);
    esRef.current = es;
    const on = (type: string, fn: (data: any) => void) => {
      es.addEventListener(type, (e) => {
        const me = e as MessageEvent;
        try {
          const parsed = JSON.parse(me.data);
          const rawId = me.lastEventId ? Number(me.lastEventId) : parsed?.seq;
          if (typeof rawId === 'number' && Number.isFinite(rawId) && rawId > lastEventIdRef.current) {
            lastEventIdRef.current = rawId;
          }
          fn(parsed);
        } catch {}
      });
    };

    on('message', ({ payload: m }: { payload?: ChatMessage }) => {
      if (!m?.botId) return;
      setMsgsMap((prev) => {
        const list = prev[m.botId] || [];
        if (m.id && list.some((x) => x.id === m.id)) return prev;
        return { ...prev, [m.botId]: [...list, m] };
      });
      if (m.role !== 'system') {
        setLastMap((prev) => ({ ...prev, [m.botId]: m }));
        if (activeBotIdRef.current !== m.botId) {
          setUnreadMap((prev) => ({ ...prev, [m.botId]: (prev[m.botId] || 0) + 1 }));
        }
      }
    });

    on('message_updated', ({ payload: m }: { payload?: ChatMessage }) => {
      if (!m?.botId || !m.id) return;
      setMsgsMap((prev) => {
        const list = prev[m.botId];
        if (!list) return prev;
        const idx = list.findIndex((x) => x.id === m.id);
        if (idx < 0) return prev;
        const copy = [...list];
        copy[idx] = m;
        return { ...prev, [m.botId]: copy };
      });
      setLastMap((prev) => (prev[m.botId]?.id === m.id ? { ...prev, [m.botId]: m } : prev));
    });

    on('bot_status', ({ payload: st }: { payload?: BotStatus }) => {
      if (!st) return;
      setBots((prev) => {
        const idx = prev.findIndex((b) => b.id === st.id);
        if (idx >= 0) {
          const copy = [...prev];
          copy[idx] = st;
          return copy;
        }
        return [...prev, st];
      });
      if (!st.busy) {
        setToolMap((prev) => {
          const copy = { ...prev };
          delete copy[st.id];
          return copy;
        });
      }
    });

    on('tool_use', ({ payload: p }: { payload?: { botId?: string; tool?: string } }) => {
      if (!p?.botId) return;
      setToolMap((prev) => ({ ...prev, [p.botId!]: p.tool || '' }));
    });

    on('bot_created', ({ payload: st }: { payload?: BotStatus }) => {
      if (!st) return;
      setBots((prev) => (prev.some((b) => b.id === st.id) ? prev : [...prev, st]));
    });

    on('bot_deleted', ({ botId, payload }: { botId?: string; payload?: { id?: string } }) => {
      const id = botId || payload?.id;
      if (id) removeBotLocally(id);
    });

    on('nodes_changed', ({ payload }: { payload?: { nodes?: NodeInfo[] } }) => {
      setNodesFromEvents(payload?.nodes || []);
    });

    on('desktop', ({ payload }: { payload?: ScreenState }) => {
      if (payload && deskOpenRef.current && payload.target === deskTargetRef.current) {
        setLiveScreen(payload);
        setScreenSeq((n) => n + 1);
      }
    });

    on('desktop_target', ({ payload }: { payload?: ActiveDesktopTarget }) => {
      if (payload) {
        applyActiveDesktopTarget(payload, true);
      }
    });

    on('stage_open', ({ botId, payload }: { botId?: string; payload?: StageOpenEvent }) => {
      if (!payload?.kind) return;
      const targetBotId = payload.botId || botId || activeBotIdRef.current || '';
      if (!targetBotId) return;
      onStageOpenEvent(targetBotId, payload);
    });

    on('takeover', ({ payload }: { payload?: { items?: TakeoverRequest[] } }) => {
      setTakeovers(payload?.items || []);
    });

    on('questions', ({ payload }: { payload?: { items?: PendingQuestion[] } }) => {
      setQuestions(payload?.items || []);
    });

    on('approvals', ({ payload }: { payload?: { items?: PendingApproval[] } }) => {
      setApprovals(payload?.items || []);
    });

    on('routine_updated', ({ botId }: { botId?: string }) => {
      if (panelOpenRef.current && panelDetailRef.current?.id === botId) {
        setRoutineRefreshSeq((n) => n + 1);
      }
    });

    on('routine_run', ({ payload: run }: { payload?: { botId?: string } }) => {
      if (run?.botId && panelOpenRef.current && panelDetailRef.current?.id === run.botId) {
        setRoutineRefreshSeq((n) => n + 1);
      }
    });

    on('update_status', ({ payload }: { payload?: UpdateStatus }) => {
      if (payload) setUpdateStatus(payload);
    });

    on('task_created', ({ payload: task }: { payload?: BotTask }) => {
      if (!task?.id || !task.botId) return;
      setTasksMap((prev) => ({
        ...prev,
        [task.botId]: upsertTaskList(prev[task.botId] || [], task),
      }));
      setTaskEventSeqMap((prev) => ({ ...prev, [task.id]: (prev[task.id] || 0) + 1 }));
    });

    on('task_updated', ({ payload: task }: { payload?: BotTask }) => {
      if (!task?.id || !task.botId) return;
      setTasksMap((prev) => ({
        ...prev,
        [task.botId]: upsertTaskList(prev[task.botId] || [], task),
      }));
      if (task.status === 'queued') {
        setTaskChunksMap((prev) => {
          if (!prev[task.id]) return prev;
          const copy = { ...prev };
          delete copy[task.id];
          return copy;
        });
      }
      if (isTaskTerminal(task.status) || task.status !== 'running') {
        setTaskToolMap((prev) => {
          if (!prev[task.id]) return prev;
          const copy = { ...prev };
          delete copy[task.id];
          return copy;
        });
      }
      setTaskEventSeqMap((prev) => ({ ...prev, [task.id]: (prev[task.id] || 0) + 1 }));
    });

    on('task_completed', ({ payload: task }: { payload?: BotTask }) => {
      if (!task?.id || !task.botId) return;
      setTasksMap((prev) => ({
        ...prev,
        [task.botId]: upsertTaskList(prev[task.botId] || [], task),
      }));
      setTaskToolMap((prev) => {
        if (!prev[task.id]) return prev;
        const copy = { ...prev };
        delete copy[task.id];
        return copy;
      });
      setTaskChunksMap((prev) => {
        if (!prev[task.id]) return prev;
        const copy = { ...prev };
        delete copy[task.id];
        return copy;
      });
      setTaskEventSeqMap((prev) => ({ ...prev, [task.id]: (prev[task.id] || 0) + 1 }));
      if (task.kind === 'routine' && panelOpenRef.current && panelDetailRef.current?.id === task.botId) {
        setRoutineRefreshSeq((n) => n + 1);
      }
    });

    on('task_chunk', ({ payload: p }: { payload?: { taskId?: string; chunk?: string } }) => {
      if (!p?.taskId || !p.chunk) return;
      setTaskChunksMap((prev) => ({
        ...prev,
        [p.taskId!]: (prev[p.taskId!] || '') + p.chunk!,
      }));
    });

    on('task_tool_use', ({ payload: p }: { payload?: { taskId?: string; tool?: string } }) => {
      if (!p?.taskId) return;
      setTaskToolMap((prev) => ({ ...prev, [p.taskId!]: p.tool || '' }));
      setTaskEventSeqMap((prev) => ({ ...prev, [p.taskId!]: (prev[p.taskId!] || 0) + 1 }));
    });
  }, [removeBotLocally]);

  const maybeAutoOpenUnbox = useCallback((s: SettingsData | null) => {
    if (unboxAutoPromptedRef.current || loginOpenRef.current) return;
    if (s?.onboarding?.needsSetup) {
      unboxAutoPromptedRef.current = true;
      setUnboxStep(1);
      setUnboxOpen(true);
    }
  }, []);

  useEffect(() => {
    Promise.all([loadBots(), loadSettings()])
      .then(([, s]) => {
        connectEvents();
        void refreshNodes();
        void refreshTakeovers();
        void refreshActiveDesktopTarget();
        void refreshApprovals();
        void refreshQuestions();
        void refreshUpdateStatus();
        maybeAutoOpenUnbox(s);
        setBootReady(true);
      })
      .catch(() => {
        setBootReady(true);
      });
    return () => {
      esRef.current?.close();
    };
  }, []);

  return { connectEvents, maybeAutoOpenUnbox };
}
