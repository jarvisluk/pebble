import React from 'react';
import type {
  ActiveDesktopTarget,
  BotStatus,
  ChatMessage,
  HarnessInfo,
  PendingApproval,
  PendingQuestion,
  PermissionMode,
  ResolvedLang,
  ScreenState,
  StageKind,
  StageTab,
  TakeoverRequest,
} from '../types';
import { translate } from '../lib/i18n';
import { botColor, contrastText } from '../lib/theme';
import { ChatView } from './ChatView';
import { DesktopStage } from './DesktopStage';
import { WorkspaceFilesPane } from './WorkspaceFilesPane';

interface StageShellProps {
  deskOpen: boolean;
  deskChatOpen: boolean;
  stageKind: StageKind;
  stageTabs: StageTab[];
  activeStageTabId: string;
  canUseFiles: boolean;
  bots: BotStatus[];
  activeBot?: BotStatus;
  activeBotId: string | null;
  msgsMap: Record<string, ChatMessage[]>;
  seenIds: Set<string>;
  toolMap: Record<string, string>;
  takeovers: TakeoverRequest[];
  approvals: PendingApproval[];
  questions: PendingQuestion[];
  harnesses?: HarnessInfo[];
  lang: ResolvedLang;
  deskInputRef: React.RefObject<HTMLTextAreaElement | null>;
  stageInsert: { seq: number; text: string } | null;
  deskTarget: string;
  activeDeskTarget: ActiveDesktopTarget;
  screenSeq: number;
  liveScreen: ScreenState | null;
  filesOpenPath: string;
  filesOpenSeq: number;
  supportsWS: boolean;
  supportsHEVC: boolean;
  onUnsupportedCap: (cap: string) => void;
  setDeskOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setDeskChatOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setDeskTarget: React.Dispatch<React.SetStateAction<string>>;
  setStageKind: React.Dispatch<React.SetStateAction<StageKind>>;
  setActiveStageTabId: React.Dispatch<React.SetStateAction<string>>;
  setStageTabs: React.Dispatch<React.SetStateAction<StageTab[]>>;
  setCollabPeerId: React.Dispatch<React.SetStateAction<string | null>>;
  setCreateOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setStageInsert: React.Dispatch<React.SetStateAction<{ seq: number; text: string } | null>>;
  setTakeovers: React.Dispatch<React.SetStateAction<TakeoverRequest[]>>;
  closeAllOverlays: () => void;
  selectBot: (id: string) => Promise<void>;
  selectDesktopTarget: (next: string) => Promise<void>;
  applyActiveDesktopTarget: (next: ActiveDesktopTarget, notifyFallback?: boolean) => void;
  handleReplyApproval: (
    id: string,
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    reason?: string
  ) => Promise<void>;
  handleReplyQuestion: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
  handleChangePermissionMode: (botId: string, mode: PermissionMode) => Promise<void>;
  handleResolveTakeover: (to: TakeoverRequest) => Promise<void>;
  handleSendMessage: (text: string, mediaIds?: string[]) => Promise<void>;
  handleInterrupt: () => Promise<void>;
  onOpenPermissions: () => void;
  showToast: (msg: string) => void;
}

export const StageShell: React.FC<StageShellProps> = ({
  deskOpen,
  deskChatOpen,
  stageKind,
  stageTabs,
  activeStageTabId,
  canUseFiles,
  bots,
  activeBot,
  activeBotId,
  msgsMap,
  seenIds,
  toolMap,
  takeovers,
  approvals,
  questions,
  harnesses,
  lang,
  deskInputRef,
  stageInsert,
  deskTarget,
  activeDeskTarget,
  screenSeq,
  liveScreen,
  filesOpenPath,
  filesOpenSeq,
  supportsWS,
  supportsHEVC,
  onUnsupportedCap,
  setDeskOpen,
  setDeskChatOpen,
  setDeskTarget,
  setStageKind,
  setActiveStageTabId,
  setStageTabs,
  setCollabPeerId,
  setCreateOpen,
  setStageInsert,
  setTakeovers,
  closeAllOverlays,
  selectBot,
  selectDesktopTarget,
  applyActiveDesktopTarget,
  handleReplyApproval,
  handleReplyQuestion,
  handleChangePermissionMode,
  handleResolveTakeover,
  handleSendMessage,
  handleInterrupt,
  onOpenPermissions,
  showToast,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  return (
  <div
    id="deskShell"
    className={`desk-fs-shell stage-shell${deskChatOpen ? ' chat-open' : ''}`}
    data-surface={stageKind === 'desktop' ? 'dark' : 'adaptive'}
    hidden={!deskOpen}
  >
    {deskChatOpen && (
      <div className="desk-chat-shade" onClick={() => setDeskChatOpen(false)} />
    )}
    <aside id="deskChatSide" className="desk-chat-side" hidden={!deskChatOpen}>
      {deskChatOpen && (
        <ChatView
          hidden={false}
          viewState="active"
          viewDir="forward"
          viewAnim={false}
          bots={bots}
          activeBot={activeBot}
          messages={activeBotId ? msgsMap[activeBotId] || [] : []}
          seenIds={seenIds}
          toolName={activeBotId ? toolMap[activeBotId] : undefined}
          takeovers={takeovers}
          approvals={activeBotId ? approvals.filter((a) => a.botId === activeBotId) : []}
          questions={activeBotId ? questions.filter((q) => q.botId === activeBotId) : []}
          harnesses={harnesses}
          deskOpen={deskOpen}
          inDesktopSidebar
          lang={lang}
          inputRef={deskInputRef}
          pendingInsert={stageInsert}
          onReplyApproval={handleReplyApproval}
          onReplyQuestion={handleReplyQuestion}
          onChangePermissionMode={handleChangePermissionMode}
          onOpenMenu={() => {}}
          onToggleDesktop={() => {}}
          onOpenDesktopTarget={(target) => {
            setDeskTarget(target || 'local');
            setStageKind('desktop');
            setActiveStageTabId('desktop');
          }}
          onResolveTakeover={(to) => void handleResolveTakeover(to)}
          onOpenDetail={() => {}}
          onOpenCollab={(peerId) => {
            setCollabPeerId(peerId);
          }}
          onOpenCreate={() => {
            setDeskOpen(false);
            closeAllOverlays();
            setCreateOpen(true);
          }}
          onSelectBot={(id) => void selectBot(id)}
          onCloseDesktopChat={() => setDeskChatOpen(false)}
          onSendMessage={handleSendMessage}
          onInterrupt={handleInterrupt}
        />
      )}
    </aside>
    <div className="desk-fs-main">
      <header className="stage-topbar">
        <div className="stage-topbar-left">
          <button
            type="button"
            className={`stage-pill-btn${deskChatOpen ? ' on' : ''}`}
            title={t('deskChatSidebarTitle')}
            onClick={() => setDeskChatOpen((v) => !v)}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
            </svg>
            <span>{activeBot?.name || t('stageChatToggle')}</span>
          </button>
          <nav className="stage-tabs" aria-label={t('stageTabsAria')}>
            {canUseFiles && (
              <button
                type="button"
                className={`stage-tab${activeStageTabId === 'files' ? ' active' : ''}`}
                onClick={() => {
                  setStageKind('files');
                  setActiveStageTabId('files');
                }}
              >
                {t('stageFilesTab')}
              </button>
            )}
            <button
              type="button"
              className={`stage-tab${activeStageTabId === 'desktop' ? ' active' : ''}`}
              onClick={() => {
                setStageKind('desktop');
                setActiveStageTabId('desktop');
              }}
            >
              {t('stageDesktopTab')}
            </button>
            {stageTabs.map((tab) => (
              <div key={tab.id} className={`stage-tab-wrap${activeStageTabId === tab.id ? ' active' : ''}`}>
                <button
                  type="button"
                  className={`stage-tab${activeStageTabId === tab.id ? ' active' : ''}`}
                  onClick={() => {
                    setStageKind(tab.kind);
                    setActiveStageTabId(tab.id);
                  }}
                >
                  {tab.title}
                </button>
                {tab.closable !== false && (
                  <button
                    type="button"
                    className="stage-tab-x"
                    aria-label={t('close')}
                    onClick={() => {
                      setStageTabs((prev) => prev.filter((x) => x.id !== tab.id));
                      if (activeStageTabId === tab.id) {
                        const nextTab = canUseFiles ? 'files' : 'desktop';
                        setStageKind(nextTab);
                        setActiveStageTabId(nextTab);
                      }
                    }}
                  >
                    ×
                  </button>
                )}
              </div>
            ))}
          </nav>
        </div>
        <button
          type="button"
          className="stage-pill-btn stage-close-btn"
          title={t('stageClose')}
          aria-label={t('stageClose')}
          onClick={() => setDeskOpen(false)}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M6 6l12 12M18 6 6 18" />
          </svg>
        </button>
      </header>

      {deskOpen && (
        <>
          <div className="stage-pane-slot" hidden={activeStageTabId !== 'desktop'}>
            <DesktopStage
              open={deskOpen && activeStageTabId === 'desktop'}
              target={deskTarget}
              activeDeskTarget={activeDeskTarget}
              screenUpdateSeq={screenSeq}
              liveScreen={liveScreen}
              takeovers={takeovers}
              chatOpen={deskChatOpen}
              embeddedInStage
              activeBotName={activeBot?.name}
              lang={lang}
              supportsWS={supportsWS}
              supportsHEVC={supportsHEVC}
              onUnsupportedCap={onUnsupportedCap}
              onChangeTarget={(next) => void selectDesktopTarget(next)}
              onActiveTargetSync={(next) => applyActiveDesktopTarget(next)}
              onTakeoversChange={setTakeovers}
              onResolveTakeover={handleResolveTakeover}
              onToggleChat={() => setDeskChatOpen((v) => !v)}
              onOpenPermissions={onOpenPermissions}
              onClose={() => setDeskOpen(false)}
            />
          </div>
          {canUseFiles && (
            <div
              className="stage-pane-slot"
              hidden={activeStageTabId !== 'files'}
              style={
                activeBot
                  ? ({
                      '--bot-accent': botColor(activeBot.id, bots),
                      '--bot-accent-text': contrastText(botColor(activeBot.id, bots)),
                    } as React.CSSProperties)
                  : undefined
              }
            >
              <WorkspaceFilesPane
                botId={activeBotId || ''}
                initialPath={filesOpenPath}
                openSeq={filesOpenSeq}
                lang={lang}
                onQuotePath={(relPath) => {
                  setDeskChatOpen(true);
                  setStageInsert({ seq: Date.now(), text: `@${relPath} ` });
                }}
                onToast={showToast}
              />
            </div>
          )}
          {activeStageTabId !== 'desktop' && activeStageTabId !== 'files' && (
            <div className="ws-preview-empty">
              {t('stagePlaceholderDesc', { kind: stageKind })}
            </div>
          )}
        </>
      )}
    </div>
  </div>
  );
};
