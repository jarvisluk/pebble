import React from 'react';
import type {
  BotDetail,
  BotStatus,
  BotTask,
  ChatMessage,
  ConfirmOptions,
  NodeInfo,
  PendingApproval,
  PendingQuestion,
  ResolvedLang,
  SettingsData,
  TakeoverRequest,
} from '../types';
import { translate } from '../lib/i18n';
import { upsertTaskList } from '../lib/tasks';
import { BotDetailPanel } from './BotDetailPanel';
import { CollabPanel } from './CollabPanel';
import { ConfirmModal, CreateBotModal, LoginModal } from './Modals';
import { TaskInspectorDrawer } from './tasks/TaskInspectorDrawer';

interface AppOverlaysProps {
  shadeOpen: boolean;
  panelOpen: boolean;
  panelDetail: BotDetail | null;
  bots: BotStatus[];
  nodes: NodeInfo[];
  tasksMap: Record<string, BotTask[]>;
  taskToolMap: Record<string, string>;
  taskChunksMap: Record<string, string>;
  taskEventSeqMap: Record<string, number>;
  inspectedTaskId: string | null;
  approvals: PendingApproval[];
  questions: PendingQuestion[];
  takeovers: TakeoverRequest[];
  settings: SettingsData | null;
  modelsMap: Record<string, string[]>;
  routineRefreshSeq: number;
  lang: ResolvedLang;
  canBindDesktop: boolean;
  canIsolateBrowser: boolean;
  canUseFiles: boolean;
  activeBot?: BotStatus;
  activeBotId: string | null;
  msgsMap: Record<string, ChatMessage[]>;
  collabPeerId: string | null;
  createOpen: boolean;
  confirmOpts: ConfirmOptions | null;
  loginOpen: boolean;
  toastMsg: string;
  closeAllOverlays: () => void;
  setPanelOpen: React.Dispatch<React.SetStateAction<boolean>>;
  setTasksMap: React.Dispatch<React.SetStateAction<Record<string, BotTask[]>>>;
  setInspectedTaskId: React.Dispatch<React.SetStateAction<string | null>>;
  showToast: (msg: string) => void;
  setConfirmOpts: React.Dispatch<React.SetStateAction<ConfirmOptions | null>>;
  setBots: React.Dispatch<React.SetStateAction<BotStatus[]>>;
  removeBotLocally: (id: string) => void;
  loadBots: () => Promise<void>;
  loadModels: (harness: string, force?: boolean) => Promise<string[]>;
  openPanel: (botId?: string) => Promise<void>;
  setSettings: React.Dispatch<React.SetStateAction<SettingsData | null>>;
  selectBot: (id: string) => Promise<void>;
  onOpenBotFiles: (botId: string) => void;
  setCollabPeerId: React.Dispatch<React.SetStateAction<string | null>>;
  setCreateOpen: React.Dispatch<React.SetStateAction<boolean>>;
  onReplyApproval: (
    id: string,
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    reason?: string
  ) => Promise<void>;
  onReplyQuestion: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
  onResolveTakeover: (to: TakeoverRequest) => Promise<void>;
  onOpenDesktopTarget: (target: string) => void;
  onLoginSuccess: () => Promise<void>;
}

export const AppOverlays: React.FC<AppOverlaysProps> = ({
  shadeOpen,
  panelOpen,
  panelDetail,
  bots,
  nodes,
  tasksMap,
  taskToolMap,
  taskChunksMap,
  taskEventSeqMap,
  inspectedTaskId,
  approvals,
  questions,
  takeovers,
  settings,
  modelsMap,
  routineRefreshSeq,
  lang,
  canBindDesktop,
  canIsolateBrowser,
  canUseFiles,
  activeBot,
  activeBotId,
  msgsMap,
  collabPeerId,
  createOpen,
  confirmOpts,
  loginOpen,
  toastMsg,
  closeAllOverlays,
  setPanelOpen,
  setTasksMap,
  setInspectedTaskId,
  showToast,
  setConfirmOpts,
  setBots,
  removeBotLocally,
  loadBots,
  loadModels,
  openPanel,
  setSettings,
  selectBot,
  onOpenBotFiles,
  setCollabPeerId,
  setCreateOpen,
  onReplyApproval,
  onReplyQuestion,
  onResolveTakeover,
  onOpenDesktopTarget,
  onLoginSuccess,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const inspectedTask = inspectedTaskId
    ? Object.values(tasksMap)
        .flat()
        .find((item) => item.id === inspectedTaskId) || null
    : null;

  const handleTaskUpsert = (task: BotTask) => {
    setTasksMap((prev) => ({
      ...prev,
      [task.botId]: upsertTaskList(prev[task.botId] || [], task),
    }));
  };

  return (
    <>
    <div id="shade" className={`shade${shadeOpen ? ' open' : ''}`} onClick={closeAllOverlays} />

    <BotDetailPanel
      open={panelOpen}
      detail={panelDetail}
      bots={bots}
      tasks={panelDetail ? tasksMap[panelDetail.id] || [] : []}
      nodes={nodes}
      settings={settings}
      models={modelsMap}
      routineRefreshSeq={routineRefreshSeq}
      lang={lang}
      canBindDesktop={canBindDesktop}
      canIsolateBrowser={canIsolateBrowser}
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
      onTasksLoaded={(botId, list) => {
        setTasksMap((prev) => ({ ...prev, [botId]: list }));
      }}
      onTaskCreated={handleTaskUpsert}
      onInspectTask={(taskId) => setInspectedTaskId(taskId)}
      onSettingsUpdated={setSettings}
      onOpenFiles={(botId) => {
        if (!canUseFiles) return;
        setPanelOpen(false);
        if (botId !== activeBotId) void selectBot(botId);
        onOpenBotFiles(botId);
      }}
    />

    <TaskInspectorDrawer
      open={Boolean(inspectedTaskId)}
      task={inspectedTask}
      bots={bots}
      nodes={nodes}
      liveTool={inspectedTask ? taskToolMap[inspectedTask.id] : undefined}
      liveChunk={inspectedTask ? taskChunksMap[inspectedTask.id] || '' : ''}
      taskRefreshSeq={inspectedTask ? taskEventSeqMap[inspectedTask.id] || 0 : 0}
      approvals={approvals}
      questions={questions}
      takeovers={takeovers}
      lang={lang}
      onClose={() => setInspectedTaskId(null)}
      onToast={showToast}
      onTaskUpdated={handleTaskUpsert}
      onReplyApproval={onReplyApproval}
      onReplyQuestion={onReplyQuestion}
      onResolveTakeover={onResolveTakeover}
      onOpenDesktopTarget={(target) => {
        setInspectedTaskId(null);
        onOpenDesktopTarget(target);
      }}
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

    <ConfirmModal options={confirmOpts} lang={lang} onClose={() => setConfirmOpts(null)} />

    <LoginModal open={loginOpen} lang={lang} onSuccess={onLoginSuccess} />

    {toastMsg && <div className="toast">{toastMsg}</div>}
    </>
  );
};
