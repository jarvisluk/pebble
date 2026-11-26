import React from 'react';
import type { ResolvedLang, StageKind } from '../../types';
import { translate } from '../../lib/i18n';
import { StageIcon } from './StageIcon';

interface StageNewTabPaneProps {
  canUseFiles: boolean;
  lang: ResolvedLang;
  onOpenFiles: () => void;
  onOpenDesktop: () => void;
}

interface LauncherItem {
  id: string;
  kind: StageKind;
  title: string;
  desc: string;
  onClick: () => void;
}

export const StageNewTabPane: React.FC<StageNewTabPaneProps> = ({
  canUseFiles,
  lang,
  onOpenFiles,
  onOpenDesktop,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const items: LauncherItem[] = [];
  if (canUseFiles) {
    items.push({
      id: 'files',
      kind: 'files',
      title: t('workspaceFiles'),
      desc: t('stageFilesTabDesc'),
      onClick: onOpenFiles,
    });
  }
  items.push({
    id: 'desktop',
    kind: 'desktop',
    title: t('remoteDesktop'),
    desc: t('stageDesktopTabDesc'),
    onClick: onOpenDesktop,
  });

  return (
    <div className="stage-new-pane">
      <div className="stage-new-inner">
        <div className="stage-new-hd">
          <h2>{t('stageNewTab')}</h2>
          <p>{t('stageNewTabSubtitle')}</p>
        </div>
        <div className="stage-launcher-list">
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              className="stage-launcher-row"
              onClick={item.onClick}
            >
              <span className="stage-launcher-ico">
                <StageIcon kind={item.kind} />
              </span>
              <span className="stage-launcher-main">
                <span className="stage-launcher-title">{item.title}</span>
                <span className="stage-launcher-desc">{item.desc}</span>
              </span>
              <span className="stage-launcher-chev">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="m9 18 6-6-6-6" />
                </svg>
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
};
