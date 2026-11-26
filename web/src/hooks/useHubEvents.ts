import { useCallback, useEffect } from 'react';
import type React from 'react';
import type {
  ActiveDesktopTarget,
  BotDetail,
  BotStatus,
  ChatMessage,
  NodeInfo,
  PendingApproval,
  PendingQuestion,
  ScreenState,
  SettingsData,
  StageKind,
  StageOpenEvent,
  StageTab,
  TakeoverRequest,
} from '../types';

export interface UseHubEventsOpts {
  esRef: React.MutableRefObject<EventSource | null>;
  activeBotIdRef: React.MutableRefObject<string | null>;
  deskOpenRef: React.MutableRefObject<boolean>;
  deskTargetRef: React.MutableRefObject<string>;
  panelOpenRef: React.MutableRefObject<boolean>;
  panelDetailRef: React.MutableRefObject<BotDetail | null>;
  unboxAutoPromptedRef: React.MutableRefObject<boolean>;
  loginOpenRef: React.MutableRefObject<boolean>;
  loadBots: () => Promise<void>;
  loadSettings: (fresh?: boolean) => Promise<SettingsData>;
  refreshTakeovers: () => Promise<void>;
  refreshActiveDesktopTarget: () => Promise<void>;
  refreshApprovals: () => Promise<void>;
  refreshQuestions: () => Promise<void>;
  selectBot: (id: string) => Promise<void>;
  removeBotLocally: (id: string) => void;
  applyActiveDesktopTarget: (next: ActiveDesktopTarget, notifyFallback?: boolean) => void;
  setMsgsMap: React.Dispatch<React.SetStateAction<Record<string, ChatMessage[]>>>;
  setLastMap: React.Dispatch<React.SetStateAction<Record<string, ChatMessage>>>;
  setUnreadMap: React.Dispatch<React.SetStateAction<Record<string, number>>>;
  setToolMap: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  setBots: React.Dispatch<React.SetStateAction<BotStatus[]>>;
  setNodesFromEvents: React.Dispatch<React.SetStateAction<NodeInfo[] | null>>;
  setLiveScreen: React.Dispatch<React.SetStateAction<ScreenState | null>>;
  setScreenSeq: React.Dispatch<React.SetStateAction<number>>;
  setStageKind: React.Dispatch<React.SetStateAction<StageKind>>;
  setActiveStageTabId: React.Dispatch<React.SetStateAction<string>>;
  setFilesOpenPath: React.Dispatch<React.SetStateAction<string>>;
  setFilesOpenSeq: React.Dispatch<React.SetStateAction<number>>;
  setDeskOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setStageTabs: React.Dispatch<React.SetStateAction<StageTab[]>>;
  setTakeovers: React.Dispatch<React.SetStateAction<TakeoverRequest[]>>;
  setQuestions: React.Dispatch<React.SetStateAction<PendingQuestion[]>>;
  setApprovals: React.Dispatch<React.SetStateAction<PendingApproval[]>>;
  setRoutineRefreshSeq: React.Dispatch<React.SetStateAction<number>>;
  setUnboxStep: React.Dispatch<React.SetStateAction<1 | 2 | 3>>;
  setUnboxOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setBootReady: React.Dispatch<React.SetStateAction<boolean>>;
}

export function useHubEvents(opts: UseHubEventsOpts) {
  const {
    esRef,
    activeBotIdRef,
    deskOpenRef,
    deskTargetRef,
    panelOpenRef,
    panelDetailRef,
    unboxAutoPromptedRef,
    loginOpenRef,
    loadBots,
    loadSettings,
    refreshTakeovers,
    refreshActiveDesktopTarget,
    refreshApprovals,
    refreshQuestions,
    selectBot,
    removeBotLocally,
    applyActiveDesktopTarget,
    setMsgsMap,
    setLastMap,
    setUnreadMap,
    setToolMap,
    setBots,
    setNodesFromEvents,
    setLiveScreen,
    setScreenSeq,
    setStageKind,
    setActiveStageTabId,
    setFilesOpenPath,
    setFilesOpenSeq,
    setDeskOpen,
    setStageTabs,
    setTakeovers,
    setQuestions,
    setApprovals,
    setRoutineRefreshSeq,
    setUnboxStep,
    setUnboxOpen,
    setBootReady,
  } = opts;

  const connectEvents = useCallback(() => {
    esRef.current?.close();
    const es = new EventSource('/api/events');
    esRef.current = es;
    const on = (type: string, fn: (data: any) => void) => {
      es.addEventListener(type, (e) => {
        try {
          fn(JSON.parse((e as MessageEvent).data));
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
      const targetBotId = payload.botId || botId;
      if (targetBotId && targetBotId !== activeBotIdRef.current) {
        void selectBot(targetBotId);
      }
      if (payload.kind === 'files') {
        setStageKind('files');
        setActiveStageTabId('files');
        setFilesOpenPath(payload.path || '');
        setFilesOpenSeq((n) => n + 1);
        setDeskOpen(true);
        return;
      }
      if (payload.kind === 'desktop') {
        setStageKind('desktop');
        setActiveStageTabId('desktop');
        setDeskOpen(true);
        return;
      }
      const tabId = payload.id || `${payload.kind}:${Date.now()}`;
      const newTab: StageTab = {
        id: tabId,
        kind: payload.kind,
        title: payload.title || payload.kind,
        botId: targetBotId,
        path: payload.path,
        url: payload.url,
        resourceUri: payload.resourceUri,
        closable: payload.closable !== false,
      };
      setStageTabs((prev) => {
        const idx = prev.findIndex((x) => x.id === tabId);
        if (idx >= 0) {
          const copy = [...prev];
          copy[idx] = newTab;
          return copy;
        }
        return [...prev, newTab];
      });
      setStageKind(payload.kind);
      setActiveStageTabId(tabId);
      setDeskOpen(true);
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
        void refreshTakeovers();
        void refreshActiveDesktopTarget();
        void refreshApprovals();
        void refreshQuestions();
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
