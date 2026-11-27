import React, { useCallback, useEffect, useRef, useState } from 'react';
import type {
  ActiveDesktopTarget,
  BotDetail,
  BotStatus,
  ChatMessage,
  ConfirmOptions,
  LangPref,
  NodeInfo,
  PendingApproval,
  PendingQuestion,
  PermissionMode,
  ResolvedLang,
  ScreenState,
  ServerInfo,
  SettingsData,
  SettingsTab,
  StageOpenEvent,
  StageRef,
  StageTab,
  TakeoverRequest,
  UpdateStatus,
} from './types';
import {
  checkForUpdates,
  CLIENT_PROTOCOL,
  fetchUpdateStatus,
  isUnsupportedCapabilityError,
  saveUpdateConfig,
  setUnauthorizedHandler,
} from './lib/api';
import { getLangPref, resolveLang, setLangPref, translate } from './lib/i18n';
import { installMacWindowDragBridge } from './lib/macShell';
import { useViewTransition } from './lib/useViewTransition';
import { useAppLayout } from './hooks/useAppLayout';
import { useHubData } from './hooks/useHubData';
import { useHubActions } from './hooks/useHubActions';
import { useHubEvents } from './hooks/useHubEvents';
import {
  type BotStageState,
  closeTab,
  createDesktopTab,
  createFilesTab,
  createNewTab,
  ensureNonEmptyStageState,
  replaceNewTabOrOpen,
  stageTabFromRef,
  upsertTab,
} from './lib/stageTabs';
import { Sidebar } from './components/Sidebar';
import { ChatView } from './components/ChatView';
import { AppOverlays } from './components/AppOverlays';
import { StageShell } from './components/StageShell';
import { SettingsView } from './components/settings/SettingsView';
import { UnboxPage } from './components/onboarding/UnboxPage';


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
  const modelsRef = useRef<Record<string, string[]>>({});
  const modelsInFlightRef = useRef<Record<string, Promise<string[]> | undefined>>({});
  const [nodesFromEvents, setNodesFromEvents] = useState<NodeInfo[] | null>(null);
  const [takeovers, setTakeovers] = useState<TakeoverRequest[]>([]);
  const [approvals, setApprovals] = useState<PendingApproval[]>([]);
  const [questions, setQuestions] = useState<PendingQuestion[]>([]);
  const [liveScreen, setLiveScreen] = useState<ScreenState | null>(null);
  const [screenSeq, setScreenSeq] = useState(0);
  const [serverInfo, setServerInfo] = useState<ServerInfo | null>(null);
  const [unsupportedCaps, setUnsupportedCaps] = useState<Set<string>>(() => new Set());
  const [compatDismissedProto, setCompatDismissedProto] = useState(0);
  const [updateStatus, setUpdateStatus] = useState<UpdateStatus | null>(null);
  const [updateChecking, setUpdateChecking] = useState(false);
  const [dismissedUpdateVer, setDismissedUpdateVer] = useState<string>(() => {
    try {
      return localStorage.getItem('pebble.dismissedUpdate') || '';
    } catch {
      return '';
    }
  });

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
  const [botStages, setBotStages] = useState<Record<string, BotStageState>>({});
  const [stageInsert, setStageInsert] = useState<{ seq: number; text: string } | null>(null);
  const [deskTarget, setDeskTarget] = useState('local');
  const [activeDeskTarget, setActiveDeskTarget] = useState<ActiveDesktopTarget>({
    target: 'local',
    temporary: false,
  });
  const [deskChatOpen, setDeskChatOpen] = useState(false);

  const [panelOpen, setPanelOpen] = useState(false);
  const [panelDetail, setPanelDetail] = useState<BotDetail | null>(null);
  const [collabPeerId, setCollabPeerId] = useState<string | null>(null);
  const [routineRefreshSeq, setRoutineRefreshSeq] = useState(0);

  const [createOpen, setCreateOpen] = useState(false);
  const [unboxOpen, setUnboxOpen] = useState(false);
  const [unboxStep, setUnboxStep] = useState<1 | 2 | 3>(1);
  const [bootReady, setBootReady] = useState(false);
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
  const activeDeskTargetRef = useRef(activeDeskTarget);
  const activeBotDesktopTargetRef = useRef('');
  const panelOpenRef = useRef(panelOpen);
  const panelDetailRef = useRef(panelDetail);
  const loginOpenRef = useRef(loginOpen);

  const inputRef = useRef<HTMLTextAreaElement>(null);
  const deskInputRef = useRef<HTMLTextAreaElement>(null);
  const filterInputRef = useRef<HTMLInputElement>(null);

  activeBotIdRef.current = activeBotId;
  viewRef.current = view;
  deskOpenRef.current = deskOpen;
  deskTargetRef.current = deskTarget;
  activeDeskTargetRef.current = activeDeskTarget;
  panelOpenRef.current = panelOpen;
  panelDetailRef.current = panelDetail;
  loginOpenRef.current = loginOpen;

  const showToast = useCallback((msg: string) => {
    setToastMsg(msg);
    if (toastTimerRef.current) window.clearTimeout(toastTimerRef.current);
    toastTimerRef.current = window.setTimeout(() => setToastMsg(''), 2600);
  }, []);

  const hasServerCap = useCallback(
    (cap: string) => {
      if (unsupportedCaps.has(cap)) return false;
      if (serverInfo) return Array.isArray(serverInfo.capabilities) && serverInfo.capabilities.includes(cap);
      return true;
    },
    [serverInfo, unsupportedCaps]
  );

  const markCapUnsupported = useCallback((cap: string) => {
    setUnsupportedCaps((prev) => {
      if (prev.has(cap)) return prev;
      const next = new Set(prev);
      next.add(cap);
      return next;
    });
  }, []);

  const canUseFiles = hasServerCap('stage.files');
  const canCheckUpdate = hasServerCap('update.check');

  const refreshUpdateStatus = useCallback(async () => {
    try {
      const st = await fetchUpdateStatus();
      setUpdateStatus(st);
    } catch (e) {
      if (isUnsupportedCapabilityError(e)) {
        markCapUnsupported('update.check');
      }
    }
  }, [markCapUnsupported]);

  const handleCheckUpdates = useCallback(async () => {
    setUpdateChecking(true);
    try {
      const st = await checkForUpdates();
      setUpdateStatus(st);
    } catch (e) {
      if (isUnsupportedCapabilityError(e)) {
        markCapUnsupported('update.check');
      } else if (e instanceof Error && e.message !== 'unauthorized') {
        showToast(e.message);
      }
    } finally {
      setUpdateChecking(false);
    }
  }, [markCapUnsupported, showToast]);

  const handleSaveUpdateConfig = useCallback(
    async (patch: { autoCheck?: boolean; interval?: string; channel?: string }) => {
      try {
        const st = await saveUpdateConfig(patch);
        setUpdateStatus(st);
        showToast(t('updateConfigSaved'));
      } catch (e) {
        if (isUnsupportedCapabilityError(e)) {
          markCapUnsupported('update.check');
        } else if (e instanceof Error && e.message !== 'unauthorized') {
          showToast(e.message);
        }
      }
    },
    [markCapUnsupported, showToast, t]
  );

  useEffect(() => {
    if (!canUseFiles) {
      setBotStages((prev) => {
        let changed = false;
        const next: Record<string, BotStageState> = {};
        for (const [bid, st] of Object.entries(prev)) {
          const filtered = st.tabs.filter((x) => x.kind !== 'files' && x.kind !== 'file');
          if (filtered.length !== st.tabs.length) {
            changed = true;
            next[bid] = ensureNonEmptyStageState({ ...st, tabs: filtered }, t);
          } else {
            next[bid] = st;
          }
        }
        return changed ? next : prev;
      });
    }
  }, [canUseFiles, t]);

  useEffect(() => {
    document.documentElement.style.removeProperty('--accent');
    document.documentElement.style.removeProperty('--accent-text');
    try {
      localStorage.removeItem('bothub.accent');
    } catch {}
    document.documentElement.lang = lang;
    setUnauthorizedHandler(() => {
      setUnsupportedCaps(new Set());
      setCompatDismissedProto(0);
      setLoginOpen(true);
    });
    const uninstallDrag = installMacWindowDragBridge();
    return () => {
      setUnauthorizedHandler(null);
      uninstallDrag();
    };
  }, []);

  const confirmLeaveSettingsThen = useCallback(
    (proceed: () => void, isTabSwitch = false) => {
      if (!settingsDirtyRef.current) {
        proceed();
        return;
      }
      setConfirmOpts({
        title: t('unsavedTitle'),
        text: t('unsavedDesc'),
        altText: t('discardChanges'),
        altClass: 'bad',
        onAlt: () => {
          settingsResetRef.current?.();
          proceed();
        },
        okText: isTabSwitch ? t('saveAndSwitch') : t('saveAndLeave'),
        okClass: 'go',
        onOk: async () => {
          const ok = await settingsSaveRef.current?.();
          if (ok !== false) proceed();
        },
      });
    },
    [t]
  );

  const leaveSettings = useCallback(() => {
    confirmLeaveSettingsThen(() => setView('chat'), false);
  }, [confirmLeaveSettingsThen]);

  const closeAllOverlays = useCallback(() => {
    setMobileSideOpen(false);
    setPanelOpen(false);
    setCollabPeerId(null);
    setCreateOpen(false);
  }, []);


  const {
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
  } = useHubData({
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
  });


  const {
    selectBot,
    openPanel,
    handleResolveTakeover,
    handleReplyApproval,
    handleChangePermissionMode,
    handleReplyQuestion,
    handleSendMessage,
    handleInterrupt,
  } = useHubActions({
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
  });

  const activeBot = bots.find((b) => b.id === activeBotId);
  activeBotDesktopTargetRef.current = activeBot?.desktopTarget || '';
  useEffect(() => {
    const bound = activeBot?.desktopTarget || '';
    if (bound) {
      setDeskTarget(bound);
    } else {
      setDeskTarget(activeDeskTargetRef.current.target || 'local');
    }
  }, [activeBotId, activeBot?.desktopTarget]);
  const shadeOpen = mobileSideOpen || panelOpen || Boolean(collabPeerId);


  const openStageShell = useCallback(() => {
    setPanelOpen(false);
    setMobileSideOpen(false);
    setCollabPeerId(null);
    setCreateOpen(false);
    if (!deskOpenRef.current) {
      setDeskChatOpen(window.innerWidth > 820);
      setDeskOpen(true);
    }
  }, []);

  const clearUnreadLatestStage = useCallback((botId: string) => {
    setBotStages((prev) => {
      const cur = prev[botId];
      if (!cur?.unreadLatestStageId) return prev;
      return { ...prev, [botId]: { ...cur, unreadLatestStageId: undefined } };
    });
  }, []);

  const openStageWithTab = useCallback(
    (botId: string, tab: StageTab, replaceNew = false) => {
      if (!botId) return;
      setBotStages((prev) => {
        const cur = prev[botId];
        const updated = replaceNew ? replaceNewTabOrOpen(cur, tab) : upsertTab(cur, tab, true);
        return {
          ...prev,
          [botId]: { ...updated, unreadLatestStageId: undefined },
        };
      });
      if (tab.kind === 'desktop' && tab.target) {
        setDeskTarget(tab.target);
      }
      openStageShell();
    },
    [openStageShell]
  );

  const openStageByRef = useCallback(
    (botId: string, ref: StageRef) => {
      if (!botId) return;
      const tab = stageTabFromRef(ref, t, botId);
      if (!canUseFiles && (tab.kind === 'files' || tab.kind === 'file')) {
        return;
      }
      openStageWithTab(botId, tab, false);
    },
    [canUseFiles, openStageWithTab, t]
  );

  const openLatestStageForBot = useCallback(
    (botId: string) => {
      const bot = bots.find((b) => b.id === botId);
      const cur = botStages[botId];
      if (bot?.latestStage) {
        const tab = stageTabFromRef(bot.latestStage, t, botId);
        if (canUseFiles || (tab.kind !== 'files' && tab.kind !== 'file')) {
          openStageWithTab(botId, tab, false);
          return;
        }
      }
      if (cur && cur.tabs.length > 0) {
        clearUnreadLatestStage(botId);
        openStageShell();
        return;
      }
      openStageWithTab(botId, createNewTab(t), false);
    },
    [botStages, bots, canUseFiles, clearUnreadLatestStage, openStageShell, openStageWithTab, t]
  );

  const handleStageOpenEvent = useCallback(
    (targetBotId: string, payload: StageOpenEvent) => {
      const tab = stageTabFromRef(payload, t, targetBotId);
      const alreadyViewingThisBot = deskOpenRef.current && activeBotIdRef.current === targetBotId;
      const shouldAutoActivate = alreadyViewingThisBot || Boolean(payload.autoOpen);

      setBotStages((prev) => {
        const cur = prev[targetBotId];
        const next = upsertTab(cur, tab, shouldAutoActivate || !cur?.activeTabId);
        return {
          ...prev,
          [targetBotId]: {
            ...next,
            unreadLatestStageId: shouldAutoActivate
              ? undefined
              : payload.id || tab.id || payload.createdAt || payload.kind,
          },
        };
      });

      if (alreadyViewingThisBot) {
        if (tab.kind === 'desktop' && tab.target) {
          setDeskTarget(tab.target);
        }
      } else if (payload.autoOpen) {
        if (targetBotId !== activeBotIdRef.current) {
          void selectBot(targetBotId);
        }
        if (tab.kind === 'desktop' && tab.target) {
          setDeskTarget(tab.target);
        }
        openStageShell();
      }
    },
    [openStageShell, selectBot, t]
  );

  const { connectEvents, maybeAutoOpenUnbox } = useHubEvents({
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
    refreshUpdateStatus,
    removeBotLocally,
    applyActiveDesktopTarget,
    onStageOpenEvent: handleStageOpenEvent,
    setMsgsMap,
    setLastMap,
    setUnreadMap,
    setToolMap,
    setBots,
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
  });

  const { toggleDesktopSidebar, handleSideResizeStart, handleSideResizeReset } = useAppLayout({
    sideWidth,
    confirmOpts,
    unboxOpen,
    createOpen,
    panelOpen,
    collabPeerId,
    mobileSideOpen,
    deskOpen,
    viewRef,
    filterInputRef,
    closeAllOverlays,
    leaveSettings,
    loadSettings,
    setSideCollapsed,
    setMobileSideOpen,
    setConfirmOpts,
    setUnboxOpen,
    setDeskOpen,
    setSettingsTab,
    setView,
    setSideWidth,
    setSideResizing,
  });

  if (unboxOpen) {
    return (
      <>
        <UnboxPage
          open
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
          onResetModels={resetModels}
        />
        {toastMsg && <div className="toast">{toastMsg}</div>}
      </>
    );
  }

  if (!bootReady && !loginOpen) {
    return (
      <div className="dsetup-page">
        <div className="dsetup-sub">{t('desktopSetupStarting')}</div>
      </div>
    );
  }

  const compatNotice =
    serverInfo && serverInfo.protocolVersion > CLIENT_PROTOCOL
      ? (() => {
          const required = (serverInfo.minClientProtocol ?? 1) > CLIENT_PROTOCOL;
          if (!required && compatDismissedProto === serverInfo.protocolVersion) return null;
          return {
            required,
            text: t(required ? 'clientUpgradeRequired' : 'clientUpdateHint'),
          };
        })()
      : null;

  const updateNotice =
    !compatNotice?.required &&
    updateStatus?.hasUpdate &&
    updateStatus.latest?.version &&
    updateStatus.latest.url &&
    dismissedUpdateVer !== updateStatus.latest.version
      ? {
          version: updateStatus.latest.version,
          url: updateStatus.latest.url,
          text: t('updateBannerText', { version: updateStatus.latest.tag || `v${updateStatus.latest.version}` }),
        }
      : null;

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
          takeovers={takeovers}
          approvals={approvals}
          questions={questions}
          filter={filter}
          lang={lang}
          compatNotice={compatNotice}
          updateNotice={updateNotice}
          filterInputRef={filterInputRef}
          onFilterChange={setFilter}
          onDismissCompatNotice={
            compatNotice && !compatNotice.required
              ? () => setCompatDismissedProto(serverInfo?.protocolVersion ?? 0)
              : undefined
          }
          onDismissUpdateNotice={
            updateNotice
              ? () => {
                  setDismissedUpdateVer(updateNotice.version);
                  try {
                    localStorage.setItem('pebble.dismissedUpdate', updateNotice.version);
                  } catch {}
                }
              : undefined
          }
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
            if (nextTab === settingsTab) {
              if (matchMedia('(max-width: 820px)').matches) closeAllOverlays();
              return;
            }
            confirmLeaveSettingsThen(() => {
              setSettingsTab(nextTab);
              if (matchMedia('(max-width: 820px)').matches) closeAllOverlays();
            }, true);
          }}
        />

        <div
          className={`side-resizer${sideResizing ? ' active' : ''}`}
          role="separator"
          aria-orientation="vertical"
          aria-label={t('resizeSidebarAria')}
          title={t('resizeSidebarTitle')}
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
            activeDeskTarget={activeDeskTarget}
            approvals={activeBotId ? approvals.filter((a) => a.botId === activeBotId) : []}
            questions={activeBotId ? questions.filter((q) => q.botId === activeBotId) : []}
            harnesses={settings?.harnesses}
            deskOpen={deskOpen}
            hasUnreadLatestStage={Boolean(activeBotId && botStages[activeBotId]?.unreadLatestStageId)}
            lang={lang}
            inputRef={inputRef}
            onReplyApproval={handleReplyApproval}
            onReplyQuestion={handleReplyQuestion}
            onChangePermissionMode={handleChangePermissionMode}
            onOpenMenu={() => {
              if (matchMedia('(min-width: 821px)').matches) {
                toggleDesktopSidebar(false);
              } else {
                setMobileSideOpen(true);
              }
            }}
            onToggleDesktop={() => {
              if (!activeBotId) return;
              const target = activeBot?.desktopTarget || activeDeskTarget.target || 'local';
              const tab = createDesktopTab(t, target);
              if (deskOpen && botStages[activeBotId]?.activeTabId === tab.id) {
                setDeskOpen(false);
              } else {
                openStageWithTab(activeBotId, tab, false);
              }
            }}
            onOpenStage={() => {
              if (!activeBotId) return;
              openLatestStageForBot(activeBotId);
            }}
            onOpenStageRef={(ref) => {
              if (!activeBotId) return;
              openStageByRef(activeBotId, ref);
            }}
            onOpenDesktopTarget={(target) => {
              const resolved = target || 'local';
              void selectDesktopTarget(resolved);
              if (activeBotId) {
                openStageWithTab(activeBotId, createDesktopTab(t, resolved), false);
              } else {
                setDeskTarget(resolved);
                openStageShell();
              }
            }}
            onSwitchBackLocal={() => void selectDesktopTarget('local')}
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
            onSendMessage={handleSendMessage}
            onInterrupt={handleInterrupt}
          />

          <SettingsView
            hidden={viewTx.stateOf('settings') === 'hidden'}
            viewState={viewTx.stateOf('settings')}
            viewDir={viewTx.dir}
            viewAnim={viewTx.animating}
            tab={settingsTab}
            bots={bots}
            settings={settings}
            serverInfo={serverInfo}
            updateStatus={updateStatus}
            updateChecking={updateChecking}
            canCheckUpdate={canCheckUpdate}
            models={modelsMap}
            nodesFromEvents={nodesFromEvents}
            lang={lang}
            langPref={langPref}
            onOpenMenu={() => setMobileSideOpen(true)}
            onLangChange={(pref) => {
              setLangPrefState(pref);
              setLang(setLangPref(pref));
            }}
            onCheckUpdates={handleCheckUpdates}
            onSaveUpdateConfig={handleSaveUpdateConfig}
            onToast={showToast}
            onConfirm={setConfirmOpts}
            onSettingsUpdated={setSettings}
            onReloadBots={loadBots}
            onLoadModels={loadModels}
            onResetModels={resetModels}
            onOpenDesktopTarget={(target) => {
              const resolved = target || 'local';
              setView('chat');
              if (activeBotId) {
                openStageWithTab(activeBotId, createDesktopTab(t, resolved), false);
              } else {
                setDeskTarget(resolved);
                openStageShell();
              }
            }}
            onReopenUnbox={() => {
              closeAllOverlays();
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


      <StageShell
        deskOpen={deskOpen}
        deskChatOpen={deskChatOpen}
        botStageState={activeBotId ? botStages[activeBotId] : undefined}
        canUseFiles={canUseFiles}
        bots={bots}
        activeBot={activeBot}
        activeBotId={activeBotId}
        msgsMap={msgsMap}
        seenIds={seenIdsRef.current}
        toolMap={toolMap}
        takeovers={takeovers}
        approvals={approvals}
        questions={questions}
        harnesses={settings?.harnesses}
        lang={lang}
        deskInputRef={deskInputRef}
        stageInsert={stageInsert}
        deskTarget={deskTarget}
        activeDeskTarget={activeDeskTarget}
        screenSeq={screenSeq}
        liveScreen={liveScreen}
        supportsWS={hasServerCap('desktop.ws_h264')}
        supportsHEVC={hasServerCap('desktop.ws_hevc')}
        onUnsupportedCap={markCapUnsupported}
        setDeskOpen={setDeskOpen}
        setDeskChatOpen={setDeskChatOpen}
        setDeskTarget={setDeskTarget}
        onSelectStageTab={(tabId) => {
          if (!activeBotId) return;
          setBotStages((prev) => {
            const cur = ensureNonEmptyStageState(prev[activeBotId], t);
            return {
              ...prev,
              [activeBotId]: { ...cur, activeTabId: tabId, unreadLatestStageId: undefined },
            };
          });
        }}
        onCloseStageTab={(tabId) => {
          if (!activeBotId) return;
          setBotStages((prev) => ({
            ...prev,
            [activeBotId]: closeTab(prev[activeBotId], tabId, t),
          }));
        }}
        onOpenStageTab={(tab, replaceNew) => {
          if (!activeBotId) return;
          openStageWithTab(activeBotId, tab, Boolean(replaceNew));
        }}
        onOpenStageRef={(ref) => {
          if (!activeBotId) return;
          openStageByRef(activeBotId, ref);
        }}
        setCollabPeerId={setCollabPeerId}
        setCreateOpen={setCreateOpen}
        setStageInsert={setStageInsert}
        setTakeovers={setTakeovers}
        closeAllOverlays={closeAllOverlays}
        selectBot={selectBot}
        selectDesktopTarget={selectDesktopTarget}
        applyActiveDesktopTarget={applyActiveDesktopTarget}
        handleReplyApproval={handleReplyApproval}
        handleReplyQuestion={handleReplyQuestion}
        handleChangePermissionMode={handleChangePermissionMode}
        handleResolveTakeover={handleResolveTakeover}
        handleSendMessage={handleSendMessage}
        handleInterrupt={handleInterrupt}
        onOpenPermissions={() => {
          setDeskOpen(false);
          setSettingsTab('desktop');
          setView('settings');
          void loadSettings();
        }}
        showToast={showToast}
      />


      <AppOverlays
        shadeOpen={shadeOpen}
        panelOpen={panelOpen}
        panelDetail={panelDetail}
        bots={bots}
        settings={settings}
        modelsMap={modelsMap}
        routineRefreshSeq={routineRefreshSeq}
        lang={lang}
        canBindDesktop={hasServerCap('bot.desktop_binding')}
        canIsolateBrowser={hasServerCap('bot.browser_isolation')}
        canUseFiles={canUseFiles}
        activeBot={activeBot}
        activeBotId={activeBotId}
        msgsMap={msgsMap}
        collabPeerId={collabPeerId}
        createOpen={createOpen}
        confirmOpts={confirmOpts}
        loginOpen={loginOpen}
        toastMsg={toastMsg}
        closeAllOverlays={closeAllOverlays}
        setPanelOpen={setPanelOpen}
        showToast={showToast}
        setConfirmOpts={setConfirmOpts}
        setBots={setBots}
        removeBotLocally={removeBotLocally}
        loadBots={loadBots}
        loadModels={loadModels}
        openPanel={openPanel}
        setSettings={setSettings}
        selectBot={selectBot}
        onOpenBotFiles={(botId) => openStageWithTab(botId, createFilesTab(t), false)}
        setCollabPeerId={setCollabPeerId}
        setCreateOpen={setCreateOpen}
        onLoginSuccess={async () => {
          setLoginOpen(false);
          setUnsupportedCaps(new Set());
          setCompatDismissedProto(0);
          connectEvents();
          await loadBots();
          void refreshTakeovers();
          void refreshActiveDesktopTarget();
          void refreshApprovals();
          void refreshQuestions();
          loadSettings()
            .then((s) => maybeAutoOpenUnbox(s))
            .catch(() => {});
        }}
      />
    </>
  );
}
