import React, { useState } from 'react';
import type { LangPref, ResolvedLang, ServerInfo, ThemePref } from '../../types';
import { CLIENT_PROTOCOL } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { applyThemePref, getThemePref } from '../../lib/theme';

interface GeneralTabProps {
  lang: ResolvedLang;
  langPref: LangPref;
  serverInfo: ServerInfo | null;
  onLangChange: (pref: LangPref) => void;
  onReopenUnbox: () => void;
}

export const GeneralTab: React.FC<GeneralTabProps> = ({
  lang,
  langPref,
  serverInfo,
  onLangChange,
  onReopenUnbox,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [themePref, setThemePref] = useState<ThemePref>(() => getThemePref());

  return (
    <div id="sTabGeneral" className="tab-pane tab-anim">
      <section className="set-card">
        <div className="set-card-hd">
          <h2>{t('appearance')}</h2>
          <span className="set-card-desc">{t('appearanceDesc')}</span>
        </div>
        <div className="fld">
          <select
            id="sTheme"
            value={themePref}
            onChange={(e) => {
              const next = e.target.value as ThemePref;
              setThemePref(next);
              applyThemePref(next);
            }}
          >
            <option value="system">{t('followSystem')}</option>
            <option value="light">{t('themeLight')}</option>
            <option value="dark">{t('themeDark')}</option>
          </select>
        </div>
      </section>

      <section className="set-card">
        <div className="set-card-hd">
          <h2>{t('language')}</h2>
          <span className="set-card-desc">{t('languageDesc')}</span>
        </div>
        <div className="fld">
          <select
            id="sLang"
            value={langPref}
            onChange={(e) => onLangChange(e.target.value as LangPref)}
          >
            <option value="system">{t('followSystem')}</option>
            <option value="zh-CN">简体中文</option>{/* i18n:allow-cjk */}
            <option value="en">English</option>
          </select>
        </div>
      </section>

      <section className="set-card">
        <div className="set-card-hd">
          <h2>{t('ubCardTitle')}</h2>
          <button type="button" id="sReopenUnbox" className="mp" onClick={onReopenUnbox}>
            {t('ubReopenBtn')}
          </button>
        </div>
        <p className="set-card-desc">{t('ubCardDesc')}</p>
      </section>

      <section className="set-card">
        <div className="set-card-hd">
          <h2>{t('aboutSection')}</h2>
          <a
            id="sOpenGitHub"
            className="mp"
            href="https://github.com/jarvisluk/pebble"
            target="_blank"
            rel="noopener noreferrer"
          >
            {t('openSourceLinkBtn')}
          </a>
        </div>
        <div className="set-kv-list">
          <div className="set-kv-row">
            <span>{t('clientVersionLbl')}</span>
            <span>v{CLIENT_PROTOCOL}</span>
          </div>
          <div className="set-kv-row">
            <span>{t('serverVersionLbl')}</span>
            <span>{serverInfo?.version || '—'}</span>
          </div>
        </div>
        <p className="set-card-desc">{t('openSourceDesc')}</p>
      </section>
    </div>
  );
};
