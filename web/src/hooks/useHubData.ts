import { useCallback } from 'react';
import type React from 'react';
import type {
  ActiveDesktopTarget,
  BotDetail,
  BotStatus,
  ChatMessage,
  PendingApproval,
  PendingQuestion,
  ServerInfo,
  SettingsData,
  TakeoverRequest,
} from '../types';
import { api } from '../lib/api';

export interface UseHubDataOpts {
  t: (k: string, vars?: Record<string, string | number>) => string;
  showToast: (msg: string) => void;
  activeBotIdRef: React.MutableRefObject<string | null>;
  activeBotDesktopTargetRef: React.MutableRefObject<string>;
  activeDeskTargetRef: React.MutableRefObject<ActiveDesktopTarget>;
  panelOpenRef: React.MutableRefObject<boolean>;
  panelDetailRef: React.MutableRefObject<BotDetail | null>;
  seenIdsRef: React.MutableRefObject<Set<string>>;
  modelsRef: React.MutableRefObject<Record<string, string[]>>;
  modelsInFlightRef: React.MutableRefObject<Record<string, Promise<string[]> | undefined>>;
  setBots: React.Dispatch<React.SetStateAction<BotStatus[]>>;
  setActiveBotId: React.Dispatch<React.SetStateAction<string | null>>;
  setMsgsMap: React.Dispatch<React.SetStateAction<Record<string, ChatMessage[]>>>;
  setLastMap: React.Dispatch<React.SetStateAction<Record<string, ChatMessage>>>;
  setUnreadMap: React.Dispatch<React.SetStateAction<Record<string, number>>>;
  setToolMap: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  setSettings: React.Dispatch<React.SetStateAction<SettingsData | null>>;
  setModelsMap: React.Dispatch<React.SetStateAction<Record<string, string[]>>>;
  setTakeovers: React.Dispatch<React.SetStateAction<TakeoverRequest[]>>;
  setApprovals: React.Dispatch<React.SetStateAction<PendingApproval[]>>;
  setQuestions: React.Dispatch<React.SetStateAction<PendingQuestion[]>>;
  setServerInfo: React.Dispatch<React.SetStateAction<ServerInfo | null>>;
  setActiveDeskTarget: React.Dispatch<React.SetStateAction<ActiveDesktopTarget>>;
  setDeskTarget: React.Dispatch<React.SetStateAction<string>>;
  setPanelOpen: React.Dispatch<React.SetStateAction<boolean>>;
}

export function useHubData(opts: UseHubDataOpts) {
  const {
    t,
    showToast,
    activeBotIdRef,
    activeBotDesktopTargetRef,
    activeDeskTargetRef,
    panelOpenRef,
    panelDetailRef,
    seenIdsRef,
    modelsRef,
    modelsInFlightRef,
    setBots,
    setActiveBotId,
    setMsgsMap,
    setLastMap,
    setUnreadMap,
    setToolMap,
    setSettings,
    setModelsMap,
    setTakeovers,
    setApprovals,
    setQuestions,
    setServerInfo,
    setActiveDeskTarget,
    setDeskTarget,
    setPanelOpen,
  } = opts;

  const loadMessages = useCallback(
    async (id: string, initial = false) => {
      try {
        const data = await api<{ messages?: ChatMessage[] }>(
          `/api/bots/${encodeURIComponent(id)}/messages?limit=200`
        );
        const list = data.messages || [];
        setMsgsMap((prev) => ({ ...prev, [id]: list }));
        if (list.length > 0) {
          setLastMap((prev) => ({ ...prev, [id]: list[list.length - 1] }));
        }
        if (initial) {
          list.forEach((m) => seenIdsRef.current.add(m.id));
        }
      } catch (e) {
        if (e instanceof Error && e.message !== 'unauthorized') showToast(e.message);
      }
    },
    [showToast]
  );

  const loadBots = useCallback(async () => {
    const data = await api<{ bots?: BotStatus[]; server?: ServerInfo }>('/api/bots');
    setServerInfo(data.server ?? null);
    const list = data.bots || [];
    setBots(list);
    let saved: string | null = null;
    try {
      saved = localStorage.getItem('bothub.active');
    } catch {}
    let nextActive = activeBotIdRef.current;
    if (!list.some((b) => b.id === nextActive)) {
      nextActive = list.some((b) => b.id === saved) ? saved : list[0]?.id || null;
      setActiveBotId(nextActive);
    }
    if (nextActive) {
      await loadMessages(nextActive, true);
    }
    await Promise.all(
      list
        .filter((b) => b.id !== nextActive)
        .map(async (b) => {
          try {
            const d = await api<{ messages?: ChatMessage[] }>(
              `/api/bots/${encodeURIComponent(b.id)}/messages?limit=1`
            );
            const m = d.messages?.[d.messages.length - 1];
            if (m) setLastMap((prev) => ({ ...prev, [b.id]: m }));
          } catch {}
        })
    );
  }, [loadMessages]);

  const loadSettings = useCallback(async (fresh = false) => {
    const s = await api<SettingsData>(fresh ? '/api/settings?fresh=1' : '/api/settings');
    setSettings(s);
    return s;
  }, []);

  const loadModels = useCallback(async (harness: string, force = false): Promise<string[]> => {
    if (!harness) return [];
    const existing = modelsRef.current[harness];
    if (!force && existing && existing.length > 0) return existing;
    const inflight = modelsInFlightRef.current[harness];
    if (!force && inflight) {
      return inflight;
    }
    const req = api<{ models?: string[] }>(`/api/models?harness=${encodeURIComponent(harness)}`)
      .then((d) => {
        const list = d.models || [];
        if (list.length > 0 || force || modelsRef.current[harness] === undefined) {
          modelsRef.current = { ...modelsRef.current, [harness]: list };
          setModelsMap(modelsRef.current);
        }
        return modelsRef.current[harness] || list;
      })
      .catch(() => modelsRef.current[harness] || [])
      .finally(() => {
        if (modelsInFlightRef.current[harness] === req) {
          delete modelsInFlightRef.current[harness];
        }
      });
    modelsInFlightRef.current[harness] = req;
    return req;
  }, []);

  const resetModels = useCallback(() => {
    modelsRef.current = {};
    modelsInFlightRef.current = {};
    setModelsMap({});
  }, []);

  const refreshTakeovers = useCallback(async () => {
    try {
      const d = await api<{ items?: TakeoverRequest[] }>('/api/desktop/takeovers');
      setTakeovers(d.items || []);
    } catch {}
  }, []);

  const applyActiveDesktopTarget = useCallback(
    (next: ActiveDesktopTarget, notifyFallback = false) => {
      const prev = activeDeskTargetRef.current;
      const normalized: ActiveDesktopTarget = {
        target: next?.target || 'local',
        name: next?.name,
        temporary: !!next?.temporary,
        expiresAt: next?.expiresAt,
      };
      setActiveDeskTarget(normalized);
      if (!activeBotDesktopTargetRef.current) {
        setDeskTarget(normalized.target);
      }
      if (notifyFallback && prev.temporary && !normalized.temporary && normalized.target === 'local') {
        showToast(t('deskTargetFallback'));
      }
    },
    [showToast, t]
  );

  const refreshActiveDesktopTarget = useCallback(async () => {
    try {
      const d = await api<{ activeTarget?: ActiveDesktopTarget }>('/api/desktop/targets');
      if (d.activeTarget) {
        applyActiveDesktopTarget(d.activeTarget);
      }
    } catch {}
  }, [applyActiveDesktopTarget]);

  const selectDesktopTarget = useCallback(
    async (nextTarget: string) => {
      const want = nextTarget || 'local';
      setDeskTarget(want);
      try {
        const res = await api<{ activeTarget?: ActiveDesktopTarget }>('/api/desktop/target', {
          method: 'PUT',
          body: { target: want },
        });
        if (res.activeTarget) {
          applyActiveDesktopTarget(res.activeTarget);
          if (res.activeTarget.temporary) {
            showToast(t('deskTargetSwitched', { name: res.activeTarget.name || res.activeTarget.target }));
          } else {
            showToast(t('deskTargetFallback'));
          }
        }
      } catch (e) {
        if (e instanceof Error && e.message !== 'unauthorized') showToast(e.message);
      }
    },
    [applyActiveDesktopTarget, showToast, t]
  );

  const refreshQuestions = useCallback(async () => {
    try {
      const d = await api<{ items?: PendingQuestion[] }>('/api/questions');
      setQuestions(d.items || []);
    } catch {}
  }, []);

  const refreshApprovals = useCallback(async () => {
    try {
      const d = await api<{ items?: PendingApproval[] }>('/api/approvals');
      setApprovals(d.items || []);
    } catch {}
  }, []);

  const removeBotLocally = useCallback(
    (id: string) => {
      setBots((prev) => {
        const nextList = prev.filter((b) => b.id !== id);
        if (panelDetailRef.current?.id === id && panelOpenRef.current) {
          setPanelOpen(false);
        }
        if (activeBotIdRef.current === id) {
          const nextActive = nextList[0]?.id || null;
          setActiveBotId(nextActive);
          try {
            if (nextActive) localStorage.setItem('bothub.active', nextActive);
            else localStorage.removeItem('bothub.active');
          } catch {}
          if (nextActive) void loadMessages(nextActive, true);
        }
        return nextList;
      });
      setMsgsMap((prev) => {
        const copy = { ...prev };
        delete copy[id];
        return copy;
      });
      setLastMap((prev) => {
        const copy = { ...prev };
        delete copy[id];
        return copy;
      });
      setUnreadMap((prev) => {
        const copy = { ...prev };
        delete copy[id];
        return copy;
      });
      setToolMap((prev) => {
        const copy = { ...prev };
        delete copy[id];
        return copy;
      });
    },
    [loadMessages]
  );


  return {
    loadMessages,
    loadBots,
    loadSettings,
    loadModels,
    resetModels,
    refreshTakeovers,
    applyActiveDesktopTarget,
    refreshActiveDesktopTarget,
    selectDesktopTarget,
    refreshQuestions,
    refreshApprovals,
    removeBotLocally,
  };
}
