import React, { useCallback, useEffect, useRef, useState } from 'react';
import type {
  BotDetail,
  BotStatus,
  ChatMessage,
  ConfirmOptions,
  LangPref,
  NodeInfo,
  PendingQuestion,
  ResolvedLang,
  ScreenState,
  SettingsData,
  SettingsTab,
  TakeoverRequest,
} from './types';
import { api, setUnauthorizedHandler } from './lib/api';
import { getLangPref, resolveLang, setLangPref, translate } from './lib/i18n';
import { installMacWindowDragBridge } from './lib/macShell';
import { useViewTransition } from './lib/useViewTransition';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import { DesktopStage } from './components/DesktopStage';
import { SettingsView } from './components/SettingsView';
import { BotDetailPanel } from './components/BotDetailPanel';
import { CollabPanel } from './components/CollabPanel';
import { UnboxModal } from './components/UnboxModal';
import { ConfirmModal, CreateBotModal, LoginModal } from './components/Modals';

const VIEW_ORDER = ['chat', 'settings'] as const;

export default function App() {
  const [langPref, setLangPrefState] = useState<LangPref>(() => getLangPref());
  const [lang, setLang] = useState<ResolvedLang>(() => resolveLang());
  const t = useCallback(
    (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars),
    [lang]
  );

  const [bots, setBots] = useState<BotStatus[]>([]);
  const [activeBotId, setActiveBotId] = useState<string | null>(null);
  const [msgsMap, setMsgsMap] = useState<Record<string, ChatMessage[]>>({});
  const [lastMap, setLastMap] = useState<Record<string, ChatMessage>>({});
  const [unreadMap, setUnreadMap] = useState<Record<string, number>>({});
  const [toolMap, setToolMap] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState('');
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [modelsMap, setModelsMap] = useState<Record<string, string[]>>({});
  const [nodesFromEvents, setNodesFromEvents] = useState<NodeInfo[] | null>(null);
  const [takeovers, setTakeovers] = useState<TakeoverRequest[]>([]);
  const [questions, setQuestions] = useState<PendingQuestion[]>([]);
  const [liveScreen, setLiveScreen] = useState<ScreenState | null>(null);
  const [screenSeq, setScreenSeq] = useState(0);

  const [view, setView] = useState<'chat' | 'settings'>('chat');
  const viewTx = useViewTransition(view, VIEW_ORDER);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('general');
  const settingsDirtyRef = useRef(false);
  const settingsSaveRef = useRef<(() => Promise<boolean>) | null>(null);
  const settingsResetRef = useRef<(() => void) | null>(null);
  const [sideCollapsed, setSideCollapsed] = useState<boolean>(() => {
    try {
      return localStorage.getItem('bothub.sideCollapsed') === '1' && matchMedia('(min-width: 821px)').matches;
    } catch {
      return false;
    }
  });
  const [sideWidth, setSideWidth] = useState<number | null>(() => {
    try {
      const raw = localStorage.getItem('bothub.sideWidth');
      if (!raw) return null;
      const n = Number(raw);
      if (!Number.isFinite(n)) return null;
      return Math.min(480, Math.max(220, Math.round(n)));
    } catch {
      return null;
    }
  });
  const [sideResizing, setSideResizing] = useState(false);
  const [mobileSideOpen, setMobileSideOpen] = useState(false);

  const [deskOpen, setDeskOpen] = useState(false);
  const [deskTarget, setDeskTarget] = useState('local');

  const [panelOpen, setPanelOpen] = useState(false);
  const [panelDetail, setPanelDetail] = useState<BotDetail | null>(null);
  const [collabPeerId, setCollabPeerId] = useState<string | null>(null);
  const [routineRefreshSeq, setRoutineRefreshSeq] = useState(0);

  const [createOpen, setCreateOpen] = useState(false);
  const [unboxOpen, setUnboxOpen] = useState(false);
  const [unboxStep, setUnboxStep] = useState<1 | 2 | 3>(1);
  const [loginOpen, setLoginOpen] = useState(false);
  const [confirmOpts, setConfirmOpts] = useState<ConfirmOptions | null>(null);
  const [toastMsg, setToastMsg] = useState('');

  const seenIdsRef = useRef<Set<string>>(new Set());
  const toastTimerRef = useRef<number | null>(null);
  const unboxAutoPromptedRef = useRef(false);
  const esRef = useRef<EventSource | null>(null);
  const activeBotIdRef = useRef(activeBotId);
  const viewRef = useRef(view);
  const deskOpenRef = useRef(deskOpen);
  const deskTargetRef = useRef(deskTarget);
  const panelOpenRef = useRef(panelOpen);
  const panelDetailRef = useRef(panelDetail);
  const loginOpenRef = useRef(loginOpen);

  const inputRef = useRef<HTMLTextAreaElement>(null);
  const filterInputRef = useRef<HTMLInputElement>(null);

  activeBotIdRef.current = activeBotId;
  viewRef.current = view;
  deskOpenRef.current = deskOpen;
  deskTargetRef.current = deskTarget;
  panelOpenRef.current = panelOpen;
  panelDetailRef.current = panelDetail;
  loginOpenRef.current = loginOpen;

  const showToast = useCallback((msg: string) => {
    setToastMsg(msg);
    if (toastTimerRef.current) window.clearTimeout(toastTimerRef.current);
    toastTimerRef.current = window.setTimeout(() => setToastMsg(''), 2600);
  }, []);

  useEffect(() => {
    document.documentElement.style.removeProperty('--accent');
    document.documentElement.style.removeProperty('--accent-text');
    try {
      localStorage.removeItem('bothub.accent');
    } catch {}
    document.documentElement.lang = lang;
    setUnauthorizedHandler(() => setLoginOpen(true));
    const uninstallDrag = installMacWindowDragBridge();
    return () => {
      setUnauthorizedHandler(null);
      uninstallDrag();
    };
  }, []);

  const leaveSettings = useCallback(() => {
    if (!settingsDirtyRef.current) {
      setView('chat');
      return;
    }
    setConfirmOpts({
      title: t('unsavedTitle'),
      text: t('unsavedDesc'),
      altText: t('discardChanges'),
      altClass: 'bad',
      onAlt: () => {
        settingsResetRef.current?.();
        setView('chat');
      },
      okText: t('saveAndLeave'),
      okClass: 'go',
      onOk: async () => {
        const ok = await settingsSaveRef.current?.();
        if (ok !== false) setView('chat');
      },
    });
  }, [t]);

  const closeAllOverlays = useCallback(() => {
    setMobileSideOpen(false);
    setPanelOpen(false);
    setCollabPeerId(null);
    setCreateOpen(false);
  }, []);

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
    const data = await api<{ bots?: BotStatus[] }>('/api/bots');
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
    let existing: string[] | undefined;
    setModelsMap((prev) => {
      existing = prev[harness];
      return prev;
    });
    if (!force && existing && existing.length > 0) return existing;
    try {
      const d = await api<{ models?: string[] }>(`/api/models?harness=${encodeURIComponent(harness)}`);
      const list = d.models || [];
      setModelsMap((prev) => ({ ...prev, [harness]: list }));
      return list;
    } catch {
      return [];
    }
  }, []);

  const refreshTakeovers = useCallback(async () => {
    try {
      const d = await api<{ items?: TakeoverRequest[] }>('/api/desktop/takeovers');
      setTakeovers(d.items || []);
    } catch {}
  }, []);

  const refreshQuestions = useCallback(async () => {
    try {
      const d = await api<{ items?: PendingQuestion[] }>('/api/questions');
      setQuestions(d.items || []);
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

    on('takeover', ({ payload }: { payload?: { items?: TakeoverRequest[] } }) => {
      setTakeovers(payload?.items || []);
    });

    on('questions', ({ payload }: { payload?: { items?: PendingQuestion[] } }) => {
      setQuestions(payload?.items || []);
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
        void refreshQuestions();
        maybeAutoOpenUnbox(s);
      })
      .catch(() => {});
    return () => {
      esRef.current?.close();
    };
  }, []);

  const toggleDesktopSidebar = useCallback((forceCollapsed: boolean | null = null) => {
    setSideCollapsed((prev) => {
      const next = forceCollapsed !== null ? forceCollapsed : !prev;
      try {
        if (next) localStorage.setItem('bothub.sideCollapsed', '1');
        else localStorage.removeItem('bothub.sideCollapsed');
      } catch {}
      return next;
    });
  }, []);

  useEffect(() => {
    const mq = window.matchMedia('(max-width: 820px)');
    const onMq = (e: MediaQueryListEvent) => {
      if (!e.matches) setMobileSideOpen(false);
    };
    mq.addEventListener('change', onMq);

    const onKeyDown = (e: KeyboardEvent) => {
      if (document.activeElement?.hasAttribute('data-desk-stage')) return;
      if (e.key === 'Escape') {
        if (confirmOpts) setConfirmOpts(null);
        else if (unboxOpen) setUnboxOpen(false);
        else if (createOpen || panelOpen || collabPeerId || mobileSideOpen) closeAllOverlays();
        else if (viewRef.current === 'settings') setView('chat');
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        if (viewRef.current !== 'settings') filterInputRef.current?.focus();
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b' && matchMedia('(min-width: 821px)').matches) {
        e.preventDefault();
        if (viewRef.current !== 'settings') toggleDesktopSidebar();
      }
      if ((e.metaKey || e.ctrlKey) && e.key === ',') {
        e.preventDefault();
        if (viewRef.current === 'settings') leaveSettings();
        else {
          closeAllOverlays();
          setDeskOpen(false);
          setSettingsTab('general');
          setView('settings');
          void loadSettings();
        }
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      mq.removeEventListener('change', onMq);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [closeAllOverlays, collabPeerId, confirmOpts, createOpen, leaveSettings, loadSettings, mobileSideOpen, panelOpen, toggleDesktopSidebar, unboxOpen]);

  const selectBot = async (id: string) => {
    setActiveBotId(id);
    setUnreadMap((prev) => ({ ...prev, [id]: 0 }));
    try {
      localStorage.setItem('bothub.active', id);
    } catch {}
    closeAllOverlays();
    if (view === 'settings') setView('chat');
    await loadMessages(id, true);
    if (matchMedia('(min-width: 821px)').matches) inputRef.current?.focus();
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

  const activeBot = bots.find((b) => b.id === activeBotId);
  const shadeOpen = mobileSideOpen || panelOpen || Boolean(collabPeerId);

  const handleSideResizeStart = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (e.button !== 0) return;
      e.preventDefault();
      const sideEl = e.currentTarget.previousElementSibling as HTMLElement | null;
      const startWidth = sideWidth ?? (sideEl ? Math.round(sideEl.getBoundingClientRect().width) : 300);
      const startX = e.clientX;
      let latest = startWidth;

      setSideResizing(true);
      document.body.classList.add('resizing-side');

      const onMove = (ev: MouseEvent) => {
        const maxW = Math.min(480, Math.floor(window.innerWidth * 0.45));
        const next = Math.min(maxW, Math.max(220, Math.round(startWidth + (ev.clientX - startX))));
        latest = next;
        setSideWidth(next);
      };
      const onUp = () => {
        window.removeEventListener('mousemove', onMove);
        window.removeEventListener('mouseup', onUp);
        document.body.classList.remove('resizing-side');
        setSideResizing(false);
        try {
          localStorage.setItem('bothub.sideWidth', String(latest));
        } catch {}
      };
      window.addEventListener('mousemove', onMove);
      window.addEventListener('mouseup', onUp);
    },
    [sideWidth]
  );

  const handleSideResizeReset = useCallback(() => {
    setSideWidth(null);
    try {
      localStorage.removeItem('bothub.sideWidth');
    } catch {}
  }, []);

  return (
    <>
      <div
        id="app"
        className={`app${sideCollapsed ? ' side-collapsed' : ''}${view === 'settings' || viewTx.leaving === 'settings' ? ' in-settings' : ''}${deskOpen ? ' desk-open' : ''}`}
        style={sideWidth != null ? ({ '--side-w': `${sideWidth}px` } as React.CSSProperties) : undefined}
      >
        <Sidebar
          open={mobileSideOpen}
          view={view}
          chatViewState={viewTx.stateOf('chat')}
          settingsViewState={viewTx.stateOf('settings')}
          viewDir={viewTx.dir}
          viewAnim={viewTx.animating}
          settingsTab={settingsTab}
          bots={bots}
          activeBotId={activeBotId}
          lastMessages={lastMap}
          unread={unreadMap}
          toolUse={toolMap}
          questions={questions}
          filter={filter}
          lang={lang}
          filterInputRef={filterInputRef}
          onFilterChange={setFilter}
          onSelectBot={(id) => void selectBot(id)}
          onOpenCreate={() => {
            closeAllOverlays();
            setCreateOpen(true);
            void loadSettings().catch(() => {});
          }}
          onCollapse={() => {
            if (matchMedia('(max-width: 820px)').matches) closeAllOverlays();
            else toggleDesktopSidebar(true);
          }}
          onToggleSettings={() => {
            if (view === 'settings') {
              leaveSettings();
            } else {
              closeAllOverlays();
              setDeskOpen(false);
              setSettingsTab('general');
              setView('settings');
              void loadSettings();
            }
          }}
          onBackToChat={leaveSettings}
          onSwitchSettingsTab={(nextTab) => {
            setSettingsTab(nextTab);
            if (matchMedia('(max-width: 820px)').matches) closeAllOverlays();
          }}
        />

        <div
          className={`side-resizer${sideResizing ? ' active' : ''}`}
          role="separator"
          aria-orientation="vertical"
          aria-label="调整侧边栏宽度"
          title="拖动调整宽度，双击重置"
          onMouseDown={handleSideResizeStart}
          onDoubleClick={handleSideResizeReset}
        />

        <main className="main">
          <div className="view-deck">
            <ChatView
              hidden={viewTx.stateOf('chat') === 'hidden'}
              viewState={viewTx.stateOf('chat')}
              viewDir={viewTx.dir}
              viewAnim={viewTx.animating}
              bots={bots}
            activeBot={activeBot}
            messages={activeBotId ? msgsMap[activeBotId] || [] : []}
            seenIds={seenIdsRef.current}
            toolName={activeBotId ? toolMap[activeBotId] : undefined}
            takeovers={takeovers}
            questions={activeBotId ? questions.filter((q) => q.botId === activeBotId) : []}
            deskOpen={deskOpen}
            lang={lang}
            inputRef={inputRef}
            onReplyQuestion={handleReplyQuestion}
            onOpenMenu={() => {
              if (matchMedia('(min-width: 821px)').matches) {
                toggleDesktopSidebar(false);
              } else {
                setMobileSideOpen(true);
              }
            }}
            onToggleDesktop={() => setDeskOpen((v) => !v)}
            onOpenDesktopTarget={(target) => {
              setDeskTarget(target || 'local');
              setDeskOpen(true);
            }}
            onResolveTakeover={(to) => void handleResolveTakeover(to)}
            onOpenDetail={() => void openPanel()}
            onOpenCollab={(peerId) => {
              setPanelOpen(false);
              setCollabPeerId(peerId);
            }}
            onOpenCreate={() => {
              closeAllOverlays();
              setCreateOpen(true);
            }}
            onSendMessage={async (text, mediaIds) => {
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
                if (/^\/(bots|status|reset)$/.test(text) && data.text) {
                  pushLocal('command', data.text);
                }
                if (text === '/reset') void loadBots();
              } catch (e) {
                if (
                  e instanceof Error &&
                  e.message !== 'unauthorized' &&
                  !/interrupted$/.test(e.message)
                ) {
                  pushLocal('error', e.message);
                }
              }
            }}
            onInterrupt={async () => {
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
            }}
          >
            <DesktopStage
              open={deskOpen}
              target={deskTarget}
              screenUpdateSeq={screenSeq}
              liveScreen={liveScreen}
              takeovers={takeovers}
              lang={lang}
              onChangeTarget={setDeskTarget}
              onTakeoversChange={setTakeovers}
              onResolveTakeover={handleResolveTakeover}
              onClose={() => setDeskOpen(false)}
            />
          </ChatView>

          <SettingsView
            hidden={viewTx.stateOf('settings') === 'hidden'}
            viewState={viewTx.stateOf('settings')}
            viewDir={viewTx.dir}
            viewAnim={viewTx.animating}
            tab={settingsTab}
            bots={bots}
            settings={settings}
            models={modelsMap}
            nodesFromEvents={nodesFromEvents}
            lang={lang}
            langPref={langPref}
            onOpenMenu={() => setMobileSideOpen(true)}
            onLangChange={(pref) => {
              setLangPrefState(pref);
              setLang(setLangPref(pref));
            }}
            onToast={showToast}
            onConfirm={setConfirmOpts}
            onSettingsUpdated={setSettings}
            onReloadBots={loadBots}
            onLoadModels={loadModels}
            onResetModels={() => setModelsMap({})}
            onOpenDesktopTarget={(target) => {
              setView('chat');
              setDeskTarget(target || 'local');
              setDeskOpen(true);
            }}
            onReopenUnbox={() => {
              setView('chat');
              setUnboxStep(1);
              setUnboxOpen(true);
            }}
            onDirtyChange={(dirty) => {
              settingsDirtyRef.current = dirty;
            }}
            saveRef={settingsSaveRef}
            resetRef={settingsResetRef}
          />
          </div>
        </main>
      </div>

      <div id="shade" className={`shade${shadeOpen ? ' open' : ''}`} onClick={closeAllOverlays} />

      <BotDetailPanel
        open={panelOpen}
        detail={panelDetail}
        bots={bots}
        settings={settings}
        models={modelsMap}
        routineRefreshSeq={routineRefreshSeq}
        lang={lang}
        onClose={() => setPanelOpen(false)}
        onToast={showToast}
        onConfirm={setConfirmOpts}
        onBotUpdated={(st) => {
          setBots((prev) => prev.map((b) => (b.id === st.id ? { ...b, ...st } : b)));
        }}
        onBotDeleted={removeBotLocally}
        onReloadBots={loadBots}
        onLoadModels={loadModels}
        onRefreshDetail={(id) => openPanel(id)}
      />

      <CollabPanel
        open={Boolean(collabPeerId)}
        bot={activeBot}
        peerId={collabPeerId}
        bots={bots}
        liveMessages={activeBot ? msgsMap[activeBot.id] || [] : []}
        lang={lang}
        onClose={() => setCollabPeerId(null)}
      />

      <CreateBotModal
        open={createOpen}
        bots={bots}
        settings={settings}
        models={modelsMap}
        lang={lang}
        onClose={() => setCreateOpen(false)}
        onCreated={async (st) => {
          setCreateOpen(false);
          setBots((prev) => (prev.some((b) => b.id === st.id) ? prev : [...prev, st]));
          showToast(t('botOnlineToast', { name: st.name }));
          await selectBot(st.id);
        }}
        onLoadModels={loadModels}
      />

      <UnboxModal
        open={unboxOpen}
        initialStep={unboxStep}
        bots={bots}
        settings={settings}
        models={modelsMap}
        lang={lang}
        onClose={() => setUnboxOpen(false)}
        onToast={showToast}
        onSettingsUpdated={setSettings}
        onLoadSettings={loadSettings}
        onLoadModels={loadModels}
        onReloadBots={loadBots}
        onBotUpdated={(st) => {
          setBots((prev) => prev.map((b) => (b.id === st.id ? { ...b, ...st } : b)));
        }}
        onResetModels={() => setModelsMap({})}
      />

      <ConfirmModal options={confirmOpts} lang={lang} onClose={() => setConfirmOpts(null)} />

      <LoginModal
        open={loginOpen}
        lang={lang}
        onSuccess={async () => {
          setLoginOpen(false);
          connectEvents();
          await loadBots();
          void refreshTakeovers();
          loadSettings()
            .then((s) => maybeAutoOpenUnbox(s))
            .catch(() => {});
        }}
      />

      {toastMsg && <div className="toast">{toastMsg}</div>}
    </>
  );
}
