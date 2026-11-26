import { useCallback } from 'react';
import type React from 'react';
import type {
  BotDetail,
  BotStatus,
  ChatMessage,
  PendingApproval,
  PendingQuestion,
  PermissionMode,
  TakeoverRequest,
} from '../types';
import { api } from '../lib/api';

export interface UseHubActionsOpts {
  t: (k: string, vars?: Record<string, string | number>) => string;
  showToast: (msg: string) => void;
  activeBotId: string | null;
  viewRef: React.MutableRefObject<'chat' | 'settings'>;
  settingsDirtyRef: React.MutableRefObject<boolean>;
  panelDetailRef: React.MutableRefObject<BotDetail | null>;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  closeAllOverlays: () => void;
  confirmLeaveSettingsThen: (proceed: () => void, isTabSwitch?: boolean) => void;
  loadMessages: (id: string, initial?: boolean) => Promise<void>;
  loadBotTasks: (id: string) => Promise<void>;
  loadBots: () => Promise<void>;
  refreshApprovals: () => Promise<void>;
  refreshQuestions: () => Promise<void>;
  setActiveBotId: React.Dispatch<React.SetStateAction<string | null>>;
  setUnreadMap: React.Dispatch<React.SetStateAction<Record<string, number>>>;
  setView: React.Dispatch<React.SetStateAction<'chat' | 'settings'>>;
  setPanelDetail: React.Dispatch<React.SetStateAction<BotDetail | null>>;
  setPanelOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setTakeovers: React.Dispatch<React.SetStateAction<TakeoverRequest[]>>;
  setApprovals: React.Dispatch<React.SetStateAction<PendingApproval[]>>;
  setBots: React.Dispatch<React.SetStateAction<BotStatus[]>>;
  setQuestions: React.Dispatch<React.SetStateAction<PendingQuestion[]>>;
  setMsgsMap: React.Dispatch<React.SetStateAction<Record<string, ChatMessage[]>>>;
}

export function useHubActions(opts: UseHubActionsOpts) {
  const {
    t,
    showToast,
    activeBotId,
    viewRef,
    settingsDirtyRef,
    panelDetailRef,
    inputRef,
    closeAllOverlays,
    confirmLeaveSettingsThen,
    loadMessages,
    loadBotTasks,
    loadBots,
    refreshApprovals,
    refreshQuestions,
    setActiveBotId,
    setUnreadMap,
    setView,
    setPanelDetail,
    setPanelOpen,
    setTakeovers,
    setApprovals,
    setBots,
    setQuestions,
    setMsgsMap,
  } = opts;

  const selectBot = async (id: string) => {
    const go = async () => {
      setActiveBotId(id);
      setUnreadMap((prev) => ({ ...prev, [id]: 0 }));
      try {
        localStorage.setItem('bothub.active', id);
      } catch {}
      closeAllOverlays();
      if (viewRef.current === 'settings') setView('chat');
      await Promise.all([loadMessages(id, true), loadBotTasks(id)]);
      if (matchMedia('(min-width: 821px)').matches) inputRef.current?.focus();
    };
    if (viewRef.current === 'settings' && settingsDirtyRef.current) {
      confirmLeaveSettingsThen(() => void go(), false);
      return;
    }
    await go();
  };

  const openPanel = async (botId?: string) => {
    const targetId = botId || activeBotId;
    if (!targetId) return;
    try {
      const d = await api<BotDetail>(`/api/bots/${encodeURIComponent(targetId)}`);
      setPanelDetail(d);
      setPanelOpen(true);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') showToast(e.message);
    }
  };

  const handleResolveTakeover = async (to: TakeoverRequest) => {
    try {
      const res = await api<{ items?: TakeoverRequest[] }>('/api/desktop/takeover/resolve', {
        method: 'POST',
        body: { id: to.id, note: t('takeoverDoneNote') },
      });
      setTakeovers(res.items || []);
      showToast(t('resumedAI'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') showToast(e.message);
    }
  };

  const handleReplyApproval = async (
    id: string,
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    reason?: string
  ) => {
    try {
      const res = await api<{ items?: PendingApproval[] }>(`/api/approvals/${encodeURIComponent(id)}/reply`, {
        method: 'POST',
        body: { decision, optionId, reason },
      });
      setApprovals(res.items || []);
    } catch (e) {
      await refreshApprovals();
      if (e instanceof Error && e.message !== 'unauthorized') {
        showToast(e.message);
        throw e;
      }
    }
  };

  const handleChangePermissionMode = async (botId: string, mode: PermissionMode) => {
    try {
      const st = await api<BotStatus>(`/api/bots/${encodeURIComponent(botId)}`, {
        method: 'PATCH',
        body: { permissionMode: mode },
      });
      setBots((prev) => prev.map((b) => (b.id === st.id ? { ...b, ...st } : b)));
      if (panelDetailRef.current?.id === st.id) {
        setPanelDetail((prev) => (prev ? { ...prev, permissionMode: st.permissionMode } : prev));
      }
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') showToast(e.message);
    }
  };

  const handleReplyQuestion = async (id: string, answers: Record<string, unknown>, cancelled: boolean) => {
    try {
      const res = await api<{ items?: PendingQuestion[] }>(`/api/questions/${encodeURIComponent(id)}/reply`, {
        method: 'POST',
        body: { answers, cancelled },
      });
      setQuestions(res.items || []);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') showToast(e.message);
      await refreshQuestions();
    }
  };

  const handleSendMessage = useCallback(
    async (text: string, mediaIds?: string[]) => {
      if (!activeBotId) return;
      const pushLocal = (from: string, msgText: string) => {
        const msg: ChatMessage = {
          id: `${from}_${Date.now()}`,
          botId: activeBotId,
          role: 'system',
          from,
          text: msgText,
          createdAt: new Date().toISOString(),
        };
        setMsgsMap((prev) => ({ ...prev, [activeBotId]: [...(prev[activeBotId] || []), msg] }));
      };
      try {
        const body: Record<string, unknown> = { message: text, source: 'web' };
        if (mediaIds && mediaIds.length > 0) {
          body.mediaIds = mediaIds;
        }
        const data = await api<{ text?: string }>(
          `/api/bots/${encodeURIComponent(activeBotId)}/message`,
          { method: 'POST', body }
        );
        if ((/^\/(bots|status|reset|mode)$/.test(text) || /^\/mode\s+/.test(text)) && data.text) {
          pushLocal('command', data.text);
        }
        if (text === '/reset' || text === '/mode' || /^\/mode\s+/.test(text)) void loadBots();
      } catch (e) {
        if (
          e instanceof Error &&
          e.message !== 'unauthorized' &&
          !/interrupted$/.test(e.message)
        ) {
          pushLocal('error', e.message);
        }
      }
    },
    [activeBotId, loadBots]
  );

  const handleInterrupt = useCallback(async () => {
    if (!activeBotId) return;
    try {
      const r = await api<{ interrupted?: boolean }>(
        `/api/bots/${encodeURIComponent(activeBotId)}/interrupt`,
        { method: 'POST' }
      );
      if (!r.interrupted) showToast(t('notRunning'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') showToast(e.message);
    }
  }, [activeBotId, showToast, t]);

  return {
    selectBot,
    openPanel,
    handleResolveTakeover,
    handleReplyApproval,
    handleChangePermissionMode,
    handleReplyQuestion,
    handleSendMessage,
    handleInterrupt,
  };
}
