import React from 'react';
import type { BotStatus, ChatMessage, PendingQuestion, ResolvedLang, SettingsTab } from '../types';
import { translate } from '../lib/i18n';
import { formatShortTime, plainPreviewText } from '../lib/theme';
import type { ViewDir, ViewState } from '../lib/useViewTransition';
import { BotAvatar } from './BotAvatar';

interface SidebarProps {
  open: boolean;
  view: 'chat' | 'settings';
  chatViewState: ViewState;
  settingsViewState: ViewState;
  viewDir: ViewDir;
  viewAnim: boolean;
  settingsTab: SettingsTab;
  bots: BotStatus[];
  activeBotId: string | null;
  lastMessages: Record<string, ChatMessage>;
  unread: Record<string, number>;
  toolUse: Record<string, string>;
  questions: PendingQuestion[];
  filter: string;
  lang: ResolvedLang;
  compatNotice?: { required: boolean; text: string } | null;
  filterInputRef: React.RefObject<HTMLInputElement | null>;
  onFilterChange: (v: string) => void;
  onDismissCompatNotice?: () => void;
  onSelectBot: (id: string) => void;
  onOpenCreate: () => void;
  onCollapse: () => void;
  onToggleSettings: () => void;
  onBackToChat: () => void;
  onSwitchSettingsTab: (tab: SettingsTab) => void;
}

export const Sidebar: React.FC<SidebarProps> = ({
  open,
  view,
  chatViewState,
  settingsViewState,
  viewDir,
  viewAnim,
  settingsTab,
  bots,
  activeBotId,
  lastMessages,
  unread,
  toolUse,
  questions,
  filter,
  lang,
  compatNotice,
  filterInputRef,
  onFilterChange,
  onDismissCompatNotice,
  onSelectBot,
  onOpenCreate,
  onCollapse,
  onToggleSettings,
  onBackToChat,
  onSwitchSettingsTab,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const q = filter.toLowerCase();
  const filteredBots = bots.filter(
    (b) => !q || `${b.name} ${b.id} ${b.description || ''}`.toLowerCase().includes(q)
  );

  return (
    <aside id="side" className={`side${open ? ' open' : ''}`}>
      <div className="view-deck">
        <div
          id="sideChat"
          className="side-pane view-pane"
          hidden={chatViewState === 'hidden'}
          aria-hidden={chatViewState !== 'active'}
          data-view-state={chatViewState}
          data-view-dir={viewDir}
          data-view-anim={viewAnim ? '1' : '0'}
        >
        <div className="side-hd">
          <h2>
            Bots<span id="botCount">{bots.length || ''}</span>
          </h2>
          <div className="side-acts">
            <button
              id="openCreate"
              type="button"
              className="ghost"
              aria-label={t('newBot')}
              title={t('newBot')}
              onClick={onOpenCreate}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </button>
            <button
              id="collapseSideBtn"
              type="button"
              className="ghost"
              aria-label={t('collapseSide')}
              title={t('collapseSideTitle')}
              onClick={onCollapse}
            >
              <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.9"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <rect x="3" y="4" width="18" height="16" rx="4" />
                <path d="M9 4v16" />
              </svg>
            </button>
          </div>
        </div>
        <label className="search">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <input
            ref={filterInputRef}
            id="filter"
            placeholder={t('searchBot')}
            autoComplete="off"
            value={filter}
            onChange={(e) => onFilterChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape' && filter) {
                e.stopPropagation();
                onFilterChange('');
              } else if (e.key === 'Enter' && filteredBots[0]) {
                onSelectBot(filteredBots[0].id);
              }
            }}
          />
        </label>
        <nav id="bots" className="list">
          {compatNotice && (
            <div className={`note compat-note${compatNotice.required ? ' compat-note-warn' : ''}`}>
              <div className="compat-note-row">
                <span>{compatNotice.text}</span>
                {!compatNotice.required && onDismissCompatNotice && (
                  <button type="button" className="mp" onClick={onDismissCompatNotice}>
                    {t('dismiss')}
                  </button>
                )}
              </div>
            </div>
          )}
          {filteredBots.length === 0 ? (
            <div className="none">{q ? t('noMatchedBots') : t('noBotsYetSide')}</div>
          ) : (
            filteredBots.map((b) => {
              const count = unread[b.id] || 0;
              const m = lastMessages[b.id];
              const tool = toolUse[b.id];
              const hasQuestion = questions.some((item) => item.botId === b.id);
              let previewNode: React.ReactNode;
              if (hasQuestion) {
                previewNode = (
                  <div className="pv run">
                    <span>{t('questionWaitingBadge')}</span>
                    {count > 0 && <i className="badge">{count}</i>}
                  </div>
                );
              } else if (b.busy) {
                previewNode = (
                  <div className="pv run">
                    <span>{t('processing')}</span>
                    {count > 0 && <i className="badge">{count}</i>}
                  </div>
                );
              } else if (!m) {
                previewNode = (
                  <div className="pv">
                    <span>{b.description || t('noMessages')}</span>
                    {count > 0 && <i className="badge">{count}</i>}
                  </div>
                );
              } else {
                const who =
                  m.role === 'user'
                    ? t('youPrefix')
                    : m.role === 'bot_out'
                      ? `→ ${(m.to || '').replace(/^bot:/, '')}: `
                      : '';
                const att = m.media && m.media.length > 0 ? m.media[0] : null;
                const attPreview = att
                  ? att.kind === 'file' || (att.kind !== 'image' && att.mimeType && !att.mimeType.startsWith('image/'))
                    ? `[${t('fileAltFallback')}] ${att.name || ''}`.trim()
                    : `[${t('imageAltFallback')}]`
                  : '';
                const previewTxt = plainPreviewText(m.text, lang) || attPreview;
                previewNode = (
                  <div className="pv">
                    <span>
                      {who}
                      {previewTxt}
                    </span>
                    {count > 0 && <i className="badge">{count}</i>}
                  </div>
                );
              }

              return (
                <button
                  key={b.id}
                  type="button"
                  className={`item${b.id === activeBotId ? ' on' : ''}`}
                  data-bot={b.id}
                  title={b.description || b.name}
                  onClick={() => onSelectBot(b.id)}
                >
                  <BotAvatar id={b.id} bots={bots} busy={Boolean(b.busy)} />
                  <div className="txt">
                    <div className="nm">
                      <span>{b.name}</span>
                      <time>{formatShortTime(m?.createdAt, lang)}</time>
                    </div>
                    {previewNode}
                  </div>
                </button>
              );
            })
          )}
        </nav>
      </div>

      <div
        id="sideSettings"
        className="side-pane view-pane"
        hidden={settingsViewState === 'hidden'}
        aria-hidden={settingsViewState !== 'active'}
        data-view-state={settingsViewState}
        data-view-dir={viewDir}
        data-view-anim={viewAnim ? '1' : '0'}
      >
        <div className="side-hd">
          <h2>{t('settings')}</h2>
        </div>
        <button type="button" id="settingsBack" className="snav-item snav-back" onClick={onBackToChat}>
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M15 18l-6-6 6-6" />
          </svg>
          <span>{t('backToChat')}</span>
        </button>
        <nav id="settingsTabs" className="list" role="tablist" aria-label={t('settingsTabs')}>
          <button
            type="button"
            className={`snav-item${settingsTab === 'general' ? ' on' : ''}`}
            role="tab"
            aria-selected={settingsTab === 'general'}
            data-stab="general"
            onClick={() => onSwitchSettingsTab('general')}
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M4 7h16M4 17h16M9 5v4M15 15v4" />
            </svg>
            <span>{t('tabGeneral')}</span>
          </button>
          <button
            type="button"
            className={`snav-item${settingsTab === 'models' ? ' on' : ''}`}
            role="tab"
            aria-selected={settingsTab === 'models'}
            data-stab="models"
            onClick={() => onSwitchSettingsTab('models')}
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M12 2 2 7l10 5 10-5-10-5z" />
              <path d="m2 12 10 5 10-5" />
              <path d="m2 17 10 5 10-5" />
            </svg>
            <span>{t('tabModels')}</span>
          </button>
          <button
            type="button"
            className={`snav-item${settingsTab === 'memory' ? ' on' : ''}`}
            role="tab"
            aria-selected={settingsTab === 'memory'}
            data-stab="memory"
            onClick={() => onSwitchSettingsTab('memory')}
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H19a1 1 0 0 1 1 1v18a1 1 0 0 1-1 1H6.5a1 1 0 0 1 0-5H20" />
            </svg>
            <span>{t('tabMemory')}</span>
          </button>
          <button
            type="button"
            className={`snav-item${settingsTab === 'desktop' ? ' on' : ''}`}
            role="tab"
            aria-selected={settingsTab === 'desktop'}
            data-stab="desktop"
            onClick={() => onSwitchSettingsTab('desktop')}
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
            <span>{t('tabDesktop')}</span>
          </button>
          <button
            type="button"
            className={`snav-item${settingsTab === 'remote' ? ' on' : ''}`}
            role="tab"
            aria-selected={settingsTab === 'remote'}
            data-stab="remote"
            onClick={() => onSwitchSettingsTab('remote')}
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <circle cx="12" cy="12" r="9" />
              <path d="M3.6 9h16.8M3.6 15h16.8" />
              <path d="M12 3a14.5 14.5 0 0 0 0 18 14.5 14.5 0 0 0 0-18z" />
            </svg>
            <span>{t('tabRemote')}</span>
          </button>
        </nav>
      </div>
      </div>

      <div className="side-ft">
        <button
          type="button"
          id="settingsBtn"
          className={`ghost${view === 'settings' ? ' active' : ''}`}
          aria-label={t('settings')}
          title={t('settingsTitle')}
          onClick={onToggleSettings}
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.9"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
        </button>
      </div>
    </aside>
  );
};
