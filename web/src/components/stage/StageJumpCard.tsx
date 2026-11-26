import React from 'react';
import type { ResolvedLang, StageRef } from '../../types';
import { translate } from '../../lib/i18n';
import { normalizeStageKind } from '../../lib/stageTabs';
import { StageIcon } from './StageIcon';

interface StageJumpCardProps {
  stageRef: StageRef;
  lang: ResolvedLang;
  onOpen: (ref: StageRef) => void;
}

export const StageJumpCard: React.FC<StageJumpCardProps> = ({ stageRef, lang, onOpen }) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const kind = normalizeStageKind(stageRef.kind, stageRef.path);

  const title =
    (stageRef.title || '').trim() ||
    (kind === 'files'
      ? t('stageFilesTab')
      : kind === 'desktop'
        ? t('stageDesktopTab')
        : stageRef.path?.split('/').pop() || stageRef.kind);

  const subtitle =
    (stageRef.subtitle || '').trim() ||
    stageRef.path ||
    stageRef.url ||
    stageRef.resourceUri ||
    (stageRef.target && stageRef.target !== 'local' ? stageRef.target : t('stageCardSubmitted'));

  return (
    <button type="button" className="stage-jump-card" onClick={() => onOpen(stageRef)}>
      <span className="stage-jump-ico">
        <StageIcon kind={kind} />
      </span>
      <span className="stage-jump-main">
        <span className="stage-jump-title">{title}</span>
        {subtitle && <span className="stage-jump-sub">{subtitle}</span>}
      </span>
      <span className="stage-jump-act">
        <span>{t('stageOpenAction')}</span>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round">
          <path d="m9 18 6-6-6-6" />
        </svg>
      </span>
    </button>
  );
};
