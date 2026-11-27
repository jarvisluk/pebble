import React from 'react';
import type {
  ActiveDesktopTarget,
  BotStatus,
  BotTask,
  ChatMessage,
  ConfirmOptions,
  HarnessInfo,
  NodeInfo,
  PendingApproval,
  PendingQuestion,
  PermissionMode,
  ResolvedLang,
  ScreenState,
  StageRef,
  StageTab,
  TakeoverRequest,
} from '../types';
import { translate } from '../lib/i18n';
import {
  type BotStageState,
  createDesktopTab,
  createFileTab,
  createFilesTab,
  createNewTab,
  createTaskTab,
  ensureNonEmptyStageState,
} from '../lib/stageTabs';
import { botColor, contrastText } from '../lib/theme';
import { ChatView } from './ChatView';
import { DesktopStage } from './DesktopStage';
import { WorkspaceFilesPane } from './WorkspaceFilesPane';
import { StageExtensionPane } from './stage/StageExtensionPane';
import { StageFilePreviewPane } from './stage/StageFilePreviewPane';
import { StageNewTabPane } from './stage/StageNewTabPane';
import { StageTabBar } from './stage/StageTabBar';
import { StageTaskPane } from './stage/StageTaskPane';

interface StageShellProps {
  deskOpen: boolean;
  deskChatOpen: boolean;
  botStageState?: BotStageState;
  canUseFiles: boolean;
  bots: BotStatus[];
  activeBot?: BotStatus;
  activeBotId: string | null;
  msgsMap: Record<string, ChatMessage[]>;
  tasksMap: Record<string, BotTask[]>;
  taskToolMap: Record<string, string>;
  taskChunksMap: Record<string, string>;
  taskEventSeqMap: Record<string, number>;
  nodes: NodeInfo[];
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
  supportsWS: boolean;
  supportsHEVC: boolean;
  onUnsupportedCap: (cap: string) => void;
  setDeskOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setDeskChatOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setDeskTarget: React.Dispatch<React.SetStateAction<string>>;
  onSelectStageTab: (tabId: string) => void;
  onCloseStageTab: (tabId: string) => void;
  onOpenStageTab: (tab: StageTab, replaceNew?: boolean) => void;
  onOpenStageRef: (ref: StageRef) => void;
  setCollabPeerId: React.Dispatch<React.SetStateAction<string | null>>;
  setCreateOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setStageInsert: React.Dispatch<React.SetStateAction<{ seq: number; text: string } | null>>;
  setTakeovers: React.Dispatch<React.SetStateAction<TakeoverRequest[]>>;
  onConfirm: React.Dispatch<React.SetStateAction<ConfirmOptions | null>>;
  onTaskUpdated: (task: BotTask) => void;
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
  onInspectTask: (taskId: string) => void;
  onOpenPermissions: () => void;
  showToast: (msg: string) => void;
}

export const StageShell: React.FC<StageShellProps> = ({
  deskOpen,
  deskChatOpen,
  botStageState,
  canUseFiles,
  bots,
  activeBot,
  activeBotId,
  msgsMap,
  tasksMap,
  taskToolMap,
  taskChunksMap,
  taskEventSeqMap,
  nodes,
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
  supportsWS,
  supportsHEVC,
  onUnsupportedCap,
  setDeskOpen,
  setDeskChatOpen,
  setDeskTarget,
  onSelectStageTab,
  onCloseStageTab,
  onOpenStageTab,
  onOpenStageRef,
  setCollabPeerId,
  setCreateOpen,
  setStageInsert,
  setTakeovers,
  onConfirm,
  onTaskUpdated,
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
  onInspectTask,
  onOpenPermissions,
  showToast,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const stageState = ensureNonEmptyStageState(botStageState, t);
  const activeTab =
    stageState.tabs.find((x) => x.id === stageState.activeTabId) || stageState.tabs[0];
  const hasDesktopTab = stageState.tabs.some((x) => x.kind === 'desktop');
  const activeDesktopTarget =
    (activeTab?.kind === 'desktop' && activeTab.target) || deskTarget || 'local';
  const activeBotTasks = activeBotId ? tasksMap[activeBotId] || [] : [];
  const activeTaskId =
    activeTab?.kind === 'task'
      ? activeTab.taskId ||
        (activeTab.id.startsWith('task:') ? activeTab.id.slice('task:'.length) : activeTab.id)
      : '';
  const activeTask = activeTaskId
    ? Object.values(tasksMap)
        .flat()
        .find((item) => item.id === activeTaskId) || null
    : null;

  const botAccentStyle: React.CSSProperties | undefined = activeBot
    ? ({
        '--bot-accent': botColor(activeBot.id, bots),
        '--bot-accent-text': contrastText(botColor(activeBot.id, bots)),
      } as React.CSSProperties)
    : undefined;

  return (
    <div
      id="deskShell"
      className={`desk-fs-shell stage-shell${deskChatOpen ? ' chat-open' : ''}`}
      data-surface="adaptive"
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
            tasks={activeBotId ? tasksMap[activeBotId] || [] : []}
            taskToolMap={taskToolMap}
            nodes={nodes}
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
              const resolved = target || 'local';
              setDeskTarget(resolved);
              onOpenStageTab(createDesktopTab(t, resolved), false);
            }}
            onOpenStageRef={onOpenStageRef}
            onResolveTakeover={(to) => void handleResolveTakeover(to)}
            onOpenDetail={() => {}}
            onInspectTask={onInspectTask}
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

      <div className="desk-fs-main" style={botAccentStyle}>
        <StageTabBar
          tabs={stageState.tabs}
          activeTabId={activeTab.id}
          chatOpen={deskChatOpen}
          botName={activeBot?.name}
          lang={lang}
          onBack={() => setDeskOpen(false)}
          onToggleChat={() => setDeskChatOpen((v) => !v)}
          onSelectTab={onSelectStageTab}
          onCloseTab={onCloseStageTab}
          onNewTab={() => onOpenStageTab(createNewTab(t), false)}
        />

        {deskOpen && (
          <div className="stage-body">
            {hasDesktopTab && (
              <div className="stage-pane-slot stage-desktop-slot" hidden={activeTab.kind !== 'desktop'}>
                <DesktopStage
                  open={deskOpen && activeTab.kind === 'desktop'}
                  target={activeDesktopTarget}
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
            )}

            {activeTab.kind === 'new' && (
              <div className="stage-pane-slot">
                <StageNewTabPane
                  canUseFiles={canUseFiles}
                  tasks={activeBotTasks}
                  nodes={nodes}
                  lang={lang}
                  onOpenFiles={() => onOpenStageTab(createFilesTab(t), true)}
                  onOpenDesktop={() => {
                    const target = activeBot?.desktopTarget || activeDeskTarget.target || 'local';
                    onOpenStageTab(createDesktopTab(t, target), true);
                  }}
                  onOpenTask={(task) => onOpenStageTab(createTaskTab(task, t), true)}
                />
              </div>
            )}

            {activeTab.kind === 'files' && canUseFiles && (
              <div className="stage-pane-slot">
                <WorkspaceFilesPane
                  botId={activeBotId || ''}
                  initialDir={activeTab.path || ''}
                  selectedFilePath=""
                  lang={lang}
                  onOpenFileTab={(relPath) => onOpenStageTab(createFileTab(relPath), false)}
                />
              </div>
            )}

            {activeTab.kind === 'file' && canUseFiles && (
              <div className="stage-pane-slot">
                <StageFilePreviewPane
                  botId={activeBotId || ''}
                  filePath={activeTab.path || ''}
                  lang={lang}
                  onQuotePath={(relPath) => {
                    setDeskChatOpen(true);
                    setStageInsert({ seq: Date.now(), text: `@${relPath} ` });
                  }}
                  onToast={showToast}
                />
              </div>
            )}

            {activeTab.kind === 'task' && activeTaskId && (
              <div className="stage-pane-slot">
                <StageTaskPane
                  taskId={activeTaskId}
                  task={activeTask}
                  bots={bots}
                  nodes={nodes}
                  liveTool={taskToolMap[activeTaskId]}
                  liveChunk={taskChunksMap[activeTaskId] || ''}
                  taskRefreshSeq={taskEventSeqMap[activeTaskId] || 0}
                  approvals={approvals}
                  questions={questions}
                  takeovers={takeovers}
                  lang={lang}
                  onToast={showToast}
                  onConfirm={onConfirm}
                  onTaskUpdated={onTaskUpdated}
                  onReplyApproval={handleReplyApproval}
                  onReplyQuestion={handleReplyQuestion}
                  onResolveTakeover={handleResolveTakeover}
                  onOpenDesktopTarget={(target) => {
                    const resolved = target || 'local';
                    void selectDesktopTarget(resolved);
                    onOpenStageTab(createDesktopTab(t, resolved), false);
                  }}
                />
              </div>
            )}

            {activeTab.kind !== 'new' &&
              activeTab.kind !== 'files' &&
              activeTab.kind !== 'file' &&
              activeTab.kind !== 'desktop' &&
              activeTab.kind !== 'task' && (
                <div className="stage-pane-slot">
                  <StageExtensionPane tab={activeTab} lang={lang} />
                </div>
              )}
          </div>
        )}
      </div>
    </div>
  );
};
