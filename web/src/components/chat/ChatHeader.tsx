import React from 'react';
import type {
  ActiveDesktopTarget,
  BotStatus,
  ResolvedLang,
  TakeoverRequest,
} from '../../types';
import { translate } from '../../lib/i18n';
import { BotAvatar } from '../BotAvatar';

interface ChatHeaderProps {
  inDesktopSidebar: boolean;
  bots: BotStatus[];
  activeBot?: BotStatus;
  takeovers: TakeoverRequest[];
  activeDeskTarget?: ActiveDesktopTarget;
  deskOpen: boolean;
  lang: ResolvedLang;
  onOpenMenu: () => void;
  onToggleDesktop: () => void;
  onOpenFiles?: () => void;
  onOpenDesktopTarget: (target: string) => void;
  onSwitchBackLocal?: () => void;
  onResolveTakeover: (to: TakeoverRequest) => void;
  onOpenDetail: () => void;
  onSelectBot?: (id: string) => void;
  onCloseDesktopChat?: () => void;
}

export const ChatHeader: React.FC<ChatHeaderProps> = ({
  inDesktopSidebar,
  bots,
  activeBot,
  takeovers,
  activeDeskTarget,
  deskOpen,
  lang,
  onOpenMenu,
  onToggleDesktop,
  onOpenFiles,
  onOpenDesktopTarget,
  onSwitchBackLocal,
  onResolveTakeover,
  onOpenDetail,
  onSelectBot,
  onCloseDesktopChat,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const firstTakeover = takeovers[0] || null;

  return (
    <>
    <div className="chat-hd">
      {!inDesktopSidebar && (
        <button
          id="menuBtn"
          type="button"
          className="ghost menu"
          aria-label={t('expandSide')}
          title={t('expandSideTitle')}
          onClick={onOpenMenu}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M4 7h16M4 12h16M4 17h10" />
          </svg>
        </button>
      )}
      <div id={inDesktopSidebar ? undefined : 'hdAv'}>
        {activeBot ? <BotAvatar id={activeBot.id} bots={bots} /> : null}
      </div>
      <div className="who">
        {inDesktopSidebar && bots.length > 0 ? (
          <select
            id="deskChatBotSel"
            className="desk-chat-bot-sel"
            aria-label={t('deskSwitchBot')}
            value={activeBot?.id || ''}
            onChange={(e) => {
              if (e.target.value) onSelectBot?.(e.target.value);
            }}
          >
            {bots.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        ) : (
          <h1 id={inDesktopSidebar ? undefined : 'hdName'}>{activeBot ? activeBot.name : 'Pebble'}</h1>
        )}
      </div>
      <div className="hd-acts">
        {inDesktopSidebar ? (
          <button
            id="deskChatCloseBtn"
            type="button"
            className="ghost"
            aria-label={t('close')}
            title={t('close')}
            onClick={onCloseDesktopChat}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <path d="M6 6l12 12M18 6 6 18" />
            </svg>
          </button>
        ) : (
          <>
            {onOpenFiles && (
              <button
                id="filesBtn"
                type="button"
                className="ghost"
                aria-label={t('workspaceFiles')}
                title={t('workspaceFiles')}
                disabled={!activeBot}
                onClick={onOpenFiles}
              >
                <svg
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.9"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
                </svg>
              </button>
            )}
            <button
              id="deskBtn"
              type="button"
              className={`ghost${deskOpen ? ' active' : ''}`}
              aria-label={t('remoteDesktop')}
              title={t('remoteDesktop')}
              onClick={onToggleDesktop}
            >
              <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.9"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <rect x="2" y="4" width="20" height="14" rx="2.5" />
                <path d="M8 21h8M12 18v3" />
              </svg>
              <span id="deskDot" className="desk-dot" hidden={takeovers.length === 0} />
            </button>
            <button
              id="detailBtn"
              type="button"
              className="ghost"
              aria-label={t('details')}
              title={t('details')}
              disabled={!activeBot}
              onClick={onOpenDetail}
            >
              <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.9"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
                <path d="M14 3v5h5M9 13h6M9 17h4" />
              </svg>
            </button>
          </>
        )}
      </div>
    </div>

    {!inDesktopSidebar && activeDeskTarget?.temporary && (
      <div className="to-banner temp-target">
        <span className="to-banner-txt">
          {t('tempDesktopBanner', {
            name: activeDeskTarget.name || activeDeskTarget.target,
          })}
        </span>
        <div className="to-banner-acts">
          <button
            type="button"
            className="btn"
            onClick={() => onOpenDesktopTarget(activeDeskTarget.target)}
          >
            {t('openDesktop')}
          </button>
          {onSwitchBackLocal && (
            <button type="button" className="btn" onClick={onSwitchBackLocal}>
              {t('switchBackLocal')}
            </button>
          )}
        </div>
      </div>
    )}

    {firstTakeover && (
      <div id="takeoverBanner" className="to-banner">
        <span id="takeoverBannerText" className="to-banner-txt">
          {t('takeoverBanner', {
            bot: firstTakeover.botName || firstTakeover.botId || 'Bot',
            target: firstTakeover.target === 'local' ? t('localHost') : firstTakeover.target,
            reason: firstTakeover.reason,
          })}
        </span>
        <div className="to-banner-acts">
          <button
            type="button"
            id="toBannerOpen"
            className="btn"
            onClick={() => onOpenDesktopTarget(firstTakeover.target || 'local')}
          >
            {t('openDesktop')}
          </button>
          <button
            type="button"
            id="toBannerResolve"
            className="btn go"
            onClick={() => onResolveTakeover(firstTakeover)}
          >
            {t('resumeAI')}
          </button>
        </div>
      </div>
    )}
    </>
  );
};
