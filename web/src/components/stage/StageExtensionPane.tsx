import React from 'react';
import type { ResolvedLang, StageTab } from '../../types';
import { translate } from '../../lib/i18n';
import { StageIcon } from './StageIcon';

interface StageExtensionPaneProps {
  tab: StageTab;
  lang: ResolvedLang;
}

function safeExternalUrl(raw?: string): string {
  const s = (raw || '').trim();
  if (!s) return '';
  try {
    const u = new URL(s);
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.toString() : '';
  } catch {
    return '';
  }
}

export const StageExtensionPane: React.FC<StageExtensionPaneProps> = ({ tab, lang }) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const safeUrl = safeExternalUrl(tab.url);
  const kindKey =
    tab.kind === 'browser'
      ? 'stageExtensionKind_browser'
      : tab.kind === 'artifact'
        ? 'stageExtensionKind_artifact'
        : tab.kind === 'mcp-app'
          ? 'stageExtensionKind_mcp_app'
          : '';
  const kindLabel = kindKey ? t(kindKey) : tab.kind;

  return (
    <div className="stage-ext-pane">
      <div className="stage-ext-card">
        <div className="stage-ext-ico">
          <StageIcon kind={tab.kind} />
        </div>
        <div className="stage-ext-kind">{kindLabel}</div>
        <div className="stage-ext-title">{tab.title}</div>
        {(tab.subtitle || tab.resourceUri || tab.path) && (
          <div className="stage-ext-sub">{tab.subtitle || tab.resourceUri || tab.path}</div>
        )}
        <p className="stage-ext-desc">{t('stagePlaceholderDesc', { kind: tab.kind })}</p>
        {safeUrl && (
          <a className="btn go" href={safeUrl} target="_blank" rel="noopener noreferrer">
            {t('stageOpenAction')}
          </a>
        )}
      </div>
    </div>
  );
};
