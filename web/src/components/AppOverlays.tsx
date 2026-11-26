import React from 'react';
import type {
  BotDetail,
  BotStatus,
  ChatMessage,
  ConfirmOptions,
  ResolvedLang,
  SettingsData,
} from '../types';
import { translate } from '../lib/i18n';
import { BotDetailPanel } from './BotDetailPanel';
import { CollabPanel } from './CollabPanel';
import { ConfirmModal, CreateBotModal, LoginModal } from './Modals';

interface AppOverlaysProps {
  shadeOpen: boolean;
  panelOpen: boolean;
  panelDetail: BotDetail | null;
  bots: BotStatus[];
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
  onLoginSuccess: () => Promise<void>;
}

export const AppOverlays: React.FC<AppOverlaysProps> = ({
  shadeOpen,
  panelOpen,
  panelDetail,
  bots,
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
  onLoginSuccess,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  return (
    <>
    <div id="shade" className={`shade${shadeOpen ? ' open' : ''}`} onClick={closeAllOverlays} />

    <BotDetailPanel
      open={panelOpen}
      detail={panelDetail}
      bots={bots}
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
      onSettingsUpdated={setSettings}
      onOpenFiles={(botId) => {
        if (!canUseFiles) return;
        setPanelOpen(false);
        if (botId !== activeBotId) void selectBot(botId);
        onOpenBotFiles(botId);
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
