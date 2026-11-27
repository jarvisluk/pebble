import React, { useState } from 'react';
import type { LangPref, ResolvedLang, ServerInfo, ThemePref, UpdateStatus } from '../../types';
import { CLIENT_PROTOCOL } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { applyThemePref, getThemePref } from '../../lib/theme';

interface GeneralTabProps {
  lang: ResolvedLang;
  langPref: LangPref;
  serverInfo: ServerInfo | null;
  updateStatus: UpdateStatus | null;
  updateChecking: boolean;
  canCheckUpdate: boolean;
  onLangChange: (pref: LangPref) => void;
  onCheckUpdates: () => Promise<void>;
  onSaveUpdateConfig: (patch: { autoCheck?: boolean; interval?: string; channel?: string }) => Promise<void>;
  onReopenUnbox: () => void;
}

function formatCheckedAt(iso: string | undefined, lang: ResolvedLang): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  try {
    return d.toLocaleString(lang === 'zh-CN' ? 'zh-CN' : 'en-US', {
      month: 'numeric',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return iso;
  }
}

export const GeneralTab: React.FC<GeneralTabProps> = ({
  lang,
  langPref,
  serverInfo,
  updateStatus,
  updateChecking,
  canCheckUpdate,
  onLangChange,
  onCheckUpdates,
  onSaveUpdateConfig,
  onReopenUnbox,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [themePref, setThemePref] = useState<ThemePref>(() => getThemePref());

  const isChecking = updateChecking || updateStatus?.state === 'checking';
  const latest = updateStatus?.latest;
  const hasUpdate = Boolean(updateStatus?.hasUpdate && latest?.version);
  const checkedText = updateStatus?.checkedAt ? formatCheckedAt(updateStatus.checkedAt, lang) : '';

  const errorText = (() => {
    if (updateStatus?.state !== 'error') return '';
    if (updateStatus.errorCode === 'rate_limited') return t('updateErrRateLimited');
    if (updateStatus.errorCode === 'network_error') return t('updateErrNetwork');
    return updateStatus.error || t('updateErrGeneric');
  })();

  const autoCheck = updateStatus?.config?.autoCheck ?? true;
  const interval = updateStatus?.config?.interval || '6h';
  const channel = updateStatus?.config?.channel || 'stable';

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

      {canCheckUpdate && (
        <section className="set-card">
          <div className="set-card-hd">
            <h2>{t('updateSection')}</h2>
            <button
              type="button"
              id="sCheckUpdate"
              className="mp"
              disabled={isChecking}
              onClick={() => void onCheckUpdates()}
            >
              {isChecking ? t('updateChecking') : t('updateCheckBtn')}
            </button>
          </div>

          <div className="update-status-row">
            <div className="update-status-main">
              {hasUpdate && latest ? (
                <>
                  <span className="mp ok">{t('updateAvailableTag', { version: latest.tag || `v${latest.version}` })}</span>
                  <a
                    id="sViewRelease"
                    className="mp go"
                    href={latest.url}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {t('updateViewRelease')}
                  </a>
                </>
              ) : errorText ? (
                <span className="update-err">{errorText}</span>
              ) : (
                <span className="update-meta">
                  {checkedText ? t('updateUpToDate') : t('updateNeverChecked')}
                </span>
              )}
            </div>
            {checkedText && (
              <span className="update-meta">{t('updateLastChecked', { time: checkedText })}</span>
            )}
          </div>

          {hasUpdate && latest?.notes && (
            <details className="ts-adv">
              <summary className="link-btn">{t('updateReleaseNotes')}</summary>
              <pre className="update-notes">{latest.notes}</pre>
            </details>
          )}

          {updateStatus && (
            <div className="update-cfg-list">
              <div className="update-toggle-row">
                <label htmlFor="sUpdateAuto">{t('updateAutoCheck')}</label>
                <button
                  type="button"
                  id="sUpdateAuto"
                  role="switch"
                  aria-checked={autoCheck}
                  className={`sw${autoCheck ? ' on' : ''}`}
                  onClick={() => void onSaveUpdateConfig({ autoCheck: !autoCheck })}
                />
              </div>

              {autoCheck && (
                <div className="fld">
                  <label htmlFor="sUpdateInterval">{t('updateInterval')}</label>
                  <select
                    id="sUpdateInterval"
                    value={interval}
                    onChange={(e) => void onSaveUpdateConfig({ interval: e.target.value })}
                  >
                    <option value="30m">{t('updateInterval_30m')}</option>
                    <option value="1h">{t('updateInterval_1h')}</option>
                    <option value="6h">{t('updateInterval_6h')}</option>
                    <option value="24h">{t('updateInterval_24h')}</option>
                  </select>
                </div>
              )}

              <div className="fld">
                <label htmlFor="sUpdateChannel">{t('updateChannel')}</label>
                <select
                  id="sUpdateChannel"
                  value={channel}
                  onChange={(e) => void onSaveUpdateConfig({ channel: e.target.value })}
                >
                  <option value="stable">{t('updateChannel_stable')}</option>
                  <option value="prerelease">{t('updateChannel_prerelease')}</option>
                </select>
              </div>
            </div>
          )}
        </section>
      )}

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
