import React from 'react';
import type { ResolvedLang, StageTab } from '../../types';
import { translate } from '../../lib/i18n';
import { StageIcon } from './StageIcon';

interface StageTabBarProps {
  tabs: StageTab[];
  activeTabId: string;
  chatOpen: boolean;
  botName?: string;
  lang: ResolvedLang;
  onBack: () => void;
  onToggleChat: () => void;
  onSelectTab: (id: string) => void;
  onCloseTab: (id: string) => void;
  onNewTab: () => void;
}

export const StageTabBar: React.FC<StageTabBarProps> = ({
  tabs,
  activeTabId,
  chatOpen,
  lang,
  onBack,
  onToggleChat,
  onSelectTab,
  onCloseTab,
  onNewTab,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  return (
    <header className="stage-topbar">
      <div className="stage-topbar-left">
        <button
          type="button"
          className="stage-icon-btn"
          title={t('backToChat')}
          aria-label={t('backToChat')}
          onClick={onBack}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M15 18l-6-6 6-6" />
          </svg>
        </button>

        <button
          type="button"
          className={`stage-icon-btn stage-chat-btn${chatOpen ? ' on' : ''}`}
          title={t('deskChatSidebarTitle')}
          aria-label={t('deskChatSidebarTitle')}
          onClick={onToggleChat}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            <rect x="3" y="4" width="18" height="16" rx="4" />
            <path d="M9 4v16" />
          </svg>
        </button>

        <span className="stage-topbar-sep" aria-hidden="true" />

        <nav className="stage-tab-strip" aria-label={t('stageTabsAria')}>
          {tabs.map((tab, idx) => {
            const active = tab.id === activeTabId;
            const nextActive = idx + 1 < tabs.length && tabs[idx + 1].id === activeTabId;
            return (
              <React.Fragment key={tab.id}>
                <div
                  className={`stage-tab-pill${active ? ' active' : ''}`}
                  title={tab.subtitle || tab.title}
                >
                  <button
                    type="button"
                    className="stage-tab-main"
                    onClick={() => onSelectTab(tab.id)}
                  >
                    <StageIcon kind={tab.kind} className="stage-tab-ico" />
                    <span className="stage-tab-title">{tab.title}</span>
                  </button>
                  {tab.closable !== false && (
                    <button
                      type="button"
                      className="stage-tab-close"
                      aria-label={t('stageCloseTab')}
                      title={t('stageCloseTab')}
                      onClick={(e) => {
                        e.stopPropagation();
                        onCloseTab(tab.id);
                      }}
                    >
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round">
                        <path d="M7 7l10 10M17 7 7 17" />
                      </svg>
                    </button>
                  )}
                </div>
                {idx < tabs.length - 1 && !active && !nextActive && (
                  <span className="stage-tab-sep" aria-hidden="true" />
                )}
              </React.Fragment>
            );
          })}
          <button
            type="button"
            className="stage-tab-add"
            title={t('stageOpenNewTab')}
            aria-label={t('stageOpenNewTab')}
            onClick={onNewTab}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round">
              <path d="M12 5v14M5 12h14" />
            </svg>
          </button>
        </nav>
      </div>
    </header>
  );
};
