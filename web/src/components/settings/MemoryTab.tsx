import React, { useCallback, useEffect, useState } from 'react';
import type { MemoryResponse, ResolvedLang, SettingsData } from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import { MagpieInstallBanner } from '../MagpieInstallBanner';

interface MemoryTabProps {
  active: boolean;
  settings: SettingsData | null;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onAfterMagpieInstall: (fresh: SettingsData) => void;
  onDirtyChange: (dirty: boolean) => void;
  saveRef: React.MutableRefObject<(() => Promise<boolean>) | null>;
  resetRef: React.MutableRefObject<(() => void) | null>;
}

const MEM_KINDS = [
  ['fact', 'kindFact'],
  ['preference', 'kindPreference'],
  ['rule', 'kindRule'],
  ['reference', 'kindReference'],
  ['profile', 'kindProfile'],
  ['procedure', 'kindProcedure'],
  ['episode', 'kindEpisode'],
] as const;

export const MemoryTab: React.FC<MemoryTabProps> = ({
  active,
  settings,
  lang,
  onToast,
  onSettingsUpdated,
  onAfterMagpieInstall,
  onDirtyChange,
  saveRef,
  resetRef,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const baseUserMd = settings?.sharedUserMd || '';
  const [sharedUserMd, setSharedUserMd] = useState(baseUserMd);
  const [userMdTouched, setUserMdTouched] = useState(false);
  const [userMdSaving, setUserMdSaving] = useState(false);
  const [userMdErr, setUserMdErr] = useState('');

  const [memData, setMemData] = useState<MemoryResponse | null>(null);
  const [memStatus, setMemStatus] = useState('');
  const [memStatusClass, setMemStatusClass] = useState('');
  const [memScope, setMemScope] = useState<'global' | 'all'>('global');
  const [memFilterKind, setMemFilterKind] = useState('');
  const [memQuery, setMemQuery] = useState('');
  const [memAddOpen, setMemAddOpen] = useState(false);
  const [memKind, setMemKind] = useState('fact');
  const [memTags, setMemTags] = useState('');
  const [memText, setMemText] = useState('');
  const [memErr, setMemErr] = useState('');

  const isDirty = Boolean(
    settings &&
      userMdTouched &&
      sharedUserMd.replace(/\s+$/, '') !== baseUserMd.replace(/\s+$/, '')
  );

  useEffect(() => {
    if (!userMdTouched) setSharedUserMd(baseUserMd);
  }, [baseUserMd, userMdTouched]);

  useEffect(() => {
    onDirtyChange(isDirty);
  }, [isDirty, onDirtyChange]);

  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const resetUserMd = useCallback(() => {
    setUserMdTouched(false);
    setSharedUserMd(baseUserMd);
    setUserMdErr('');
  }, [baseUserMd]);

  const saveUserMd = useCallback(async (): Promise<boolean> => {
    setUserMdSaving(true);
    setUserMdErr('');
    try {
      const next = await api<SettingsData>('/api/settings', {
        method: 'PUT',
        body: { sharedUserMd },
      });
      onSettingsUpdated(next);
      setUserMdTouched(false);
      onToast(translate(lang, 'saved'));
      return true;
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setUserMdErr(e.message);
      return false;
    } finally {
      setUserMdSaving(false);
    }
  }, [lang, sharedUserMd, onSettingsUpdated, onToast]);

  useEffect(() => {
    saveRef.current = saveUserMd;
    resetRef.current = resetUserMd;
    return () => {
      saveRef.current = null;
      resetRef.current = null;
    };
  }, [saveRef, resetRef, saveUserMd, resetUserMd]);

  const loadGlobalMemories = useCallback(
    async (override?: { scope?: 'global' | 'all'; kind?: string; query?: string }) => {
      setMemErr('');
      setMemStatus(translate(lang, 'loading'));
      setMemStatusClass('');
      try {
        const sc = override?.scope ?? memScope;
        const kd = override?.kind !== undefined ? override.kind : memFilterKind;
        const q = (override?.query !== undefined ? override.query : memQuery).trim();
        const params = new URLSearchParams({ scope: sc, limit: '30' });
        if (q) params.set('query', q);
        if (kd) params.set('kind', kd);
        const d = await api<MemoryResponse>(`/api/memory?${params.toString()}`);
        setMemData(d);
        if (!d.installed) {
          setMemStatus(translate(lang, 'notLoaded'));
          setMemStatusClass('');
        } else if (!d.available) {
          setMemStatus(translate(lang, 'notConnected'));
          setMemStatusClass('bad');
        } else {
          const count = (d.items || []).length;
          setMemStatus(
            q && d.semantic?.active
              ? translate(lang, 'memSemanticMatches', { n: count })
              : translate(lang, 'itemsCount', { n: count })
          );
          setMemStatusClass('ok');
        }
      } catch (e) {
        setMemStatus(translate(lang, 'failed'));
        setMemStatusClass('bad');
        if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
      }
    },
    [lang, memScope, memFilterKind, memQuery]
  );

  useEffect(() => {
    if (active) void loadGlobalMemories();
  }, [active]);

  const mp = settings?.magpie;

  return (
    <div id="sTabMemory" className="tab-pane tab-anim">
      <section className="set-card">
        <div className="set-card-hd">
          <div>
            <h2>{t('globalSharedMem')}</h2>
            <span className="set-card-desc">{t('globalSharedMemDesc')}</span>
          </div>
          <button
            type="button"
            id="sUserMdSave"
            className="mp go"
            disabled={!isDirty || userMdSaving}
            onClick={() => void saveUserMd()}
          >
            {userMdSaving ? t('saving') : t('save')}
          </button>
        </div>
        <div className="fld">
          <textarea
            id="sUserMd"
            rows={8}
            className="ta-lg"
            spellCheck={false}
            placeholder={t('userMdPh')}
            value={sharedUserMd}
            onChange={(e) => {
              const nextVal = e.target.value;
              if (nextVal === sharedUserMd) return;
              setUserMdTouched(true);
              setSharedUserMd(nextVal);
            }}
          />
        </div>
        {userMdErr && <div className="msg-err">{userMdErr}</div>}
      </section>

      <section className="set-card" id="sMemCard">
        <div className="set-card-hd">
          <h2>{t('magpieGlobalMem')}</h2>
          <div className="row-inline-sm row-wrap">
            {memData?.installed && memData.semantic && (
              <span id="sMemSemanticBadge" className={`mp ${memData.semantic.active ? 'ok' : ''}`}>
                {t(memData.semantic.active ? 'memSemanticOn' : 'memSemanticOff')}
              </span>
            )}
            <span id="sMemStatus" className={`mp ${memStatusClass}`}>
              {memStatus || t('notLoaded')}
            </span>
            <button type="button" id="sMemRefresh" className="mp" onClick={() => void loadGlobalMemories()}>
              {t('refresh')}
            </button>
            <button
              type="button"
              id="sMemAddToggle"
              className="mp go"
              onClick={() => setMemAddOpen((v) => !v)}
            >
              {t('addMemoryBtn')}
            </button>
          </div>
        </div>
        <p className="set-card-desc">{t('magpieGlobalMemDesc')}</p>
        {memData?.installed && memData.semantic && !memData.semantic.active && (
          <div className="mem-semantic-hint">
            <span>{t('memSemanticHintOff')}</span>
            <button
              type="button"
              className="mp"
              onClick={async () => {
                try {
                  await api('/api/magpie/open', { method: 'POST' });
                } catch (e) {
                  if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
                }
              }}
            >
              {t('openMagpie')}
            </button>
          </div>
        )}
        {memAddOpen && (
          <div id="sMemAddBox" className="add-prov mem-add">
            <div className="two">
              <div className="fld">
                <label>{t('memType')}</label>
                <select id="sMemKind" value={memKind} onChange={(e) => setMemKind(e.target.value)}>
                  {MEM_KINDS.map(([k, lblKey]) => (
                    <option key={k} value={k}>
                      {t(lblKey)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label dangerouslySetInnerHTML={{ __html: t('tagsOptional') }} />
                <input
                  id="sMemTags"
                  placeholder={t('tagsPh')}
                  value={memTags}
                  onChange={(e) => setMemTags(e.target.value)}
                />
              </div>
            </div>
            <div className="fld">
              <label>{t('memContent')}</label>
              <textarea
                id="sMemText"
                rows={3}
                className="ta-md ta-font"
                placeholder={t('globalMemPh')}
                value={memText}
                onChange={(e) => setMemText(e.target.value)}
              />
            </div>
            <div className="row-end-sm">
              <button
                type="button"
                id="sMemAddCancel"
                className="mp"
                onClick={() => {
                  setMemAddOpen(false);
                  setMemErr('');
                }}
              >
                {t('cancel')}
              </button>
              <button
                type="button"
                id="sMemAddSubmit"
                className="mp go"
                onClick={async () => {
                  const text = memText.trim();
                  if (!text) {
                    setMemErr(t('memContentReq'));
                    return;
                  }
                  const tags = memTags
                    .split(/[,，]/)
                    .map((s) => s.trim())
                    .filter(Boolean);
                  setMemErr('');
                  try {
                    await api('/api/memory', {
                      method: 'POST',
                      body: { scope: 'global', kind: memKind, text, tags },
                    });
                    setMemText('');
                    setMemTags('');
                    setMemAddOpen(false);
                    onToast(t('globalMemSaved'));
                    await loadGlobalMemories();
                  } catch (e) {
                    if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
                  }
                }}
              >
                {t('saveMemory')}
              </button>
            </div>
          </div>
        )}

        <div className="mem-filters">
          <div className="mem-scope-seg" role="group" aria-label={t('memScopeAria')}>
            <button
              type="button"
              id="sMemScopeGlobal"
              className={`mp${memScope === 'global' ? ' on' : ''}`}
              onClick={() => {
                setMemScope('global');
                void loadGlobalMemories({ scope: 'global' });
              }}
            >
              {t('memScopeGlobal')}
            </button>
            <button
              type="button"
              id="sMemScopeAll"
              className={`mp${memScope === 'all' ? ' on' : ''}`}
              onClick={() => {
                setMemScope('all');
                void loadGlobalMemories({ scope: 'all' });
              }}
            >
              {t('memScopeAll')}
            </button>
          </div>
          <select
            id="sMemFilterKind"
            aria-label={t('memType')}
            value={memFilterKind}
            onChange={(e) => {
              const next = e.target.value;
              setMemFilterKind(next);
              void loadGlobalMemories({ kind: next });
            }}
          >
            <option value="">{t('memKindAll')}</option>
            {MEM_KINDS.map(([k, lblKey]) => (
              <option key={k} value={k}>
                {t(lblKey)}
              </option>
            ))}
          </select>
        </div>
        <div className="mem-bar">
          <input
            id="sMemQuery"
            className="note-in"
            placeholder={memData?.semantic?.active ? t('searchSemanticMemPh') : t('searchGlobalMemPh')}
            value={memQuery}
            onChange={(e) => setMemQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                e.preventDefault();
                void loadGlobalMemories();
              }
            }}
          />
          {memQuery.trim() !== '' && (
            <button
              type="button"
              id="sMemClearBtn"
              className="mp"
              onClick={() => {
                setMemQuery('');
                void loadGlobalMemories({ query: '' });
              }}
            >
              {t('memClearSearch')}
            </button>
          )}
          <button type="button" id="sMemSearchBtn" className="mp" onClick={() => void loadGlobalMemories()}>
            {t('search')}
          </button>
        </div>
        <div id="sMemList" className="stack6">
          {!memData || !memData.installed ? (
            <div className="note">
              <MagpieInstallBanner
                lang={lang}
                hint={t('memNotInstalled')}
                installCmds={mp?.install}
                onToast={onToast}
                onInstalled={async () => {
                  const fresh = await api<SettingsData>('/api/settings?fresh=1');
                  onAfterMagpieInstall(fresh);
                  await loadGlobalMemories();
                }}
              />
            </div>
          ) : !memData.available ? (
            <div className="set-card-desc">
              {t('memUnavailable')}
              {memData.error ? `: ${memData.error}` : ''}
            </div>
          ) : !(memData.items || []).length ? (
            <div className="set-card-desc">{t('memEmpty')}</div>
          ) : (
            (memData.items || []).map((it) => {
              const tags = (it.tags || []).filter((tag) => !tag.startsWith('bot:'));
              const tm = it.updatedAt ? it.updatedAt.replace('T', ' ').slice(0, 16) : '';
              return (
                <div key={it.id} className="mem-row">
                  <div className="mem-top">
                    <div className="mem-meta">
                      {it.kind && <span className="mp ok">{it.kind}</span>}
                      {memScope === 'all' && it.scope && (
                        <span className="mp">
                          {it.scope === 'everywhere' || it.scope === 'global'
                            ? t('memScopeEverywhere')
                            : t('memScopeProject')}
                        </span>
                      )}
                      {tags.map((tg) => (
                        <span key={tg} className="mp">
                          #{tg}
                        </span>
                      ))}
                      {tm && <span className="mem-time">{tm}</span>}
                    </div>
                    <button
                      type="button"
                      className="mp bad"
                      onClick={async () => {
                        setMemErr('');
                        try {
                          await api(`/api/memory/${encodeURIComponent(it.id)}`, { method: 'DELETE' });
                          onToast(t('memDeleted'));
                          await loadGlobalMemories();
                        } catch (e) {
                          if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
                        }
                      }}
                    >
                      {t('delBtn')}
                    </button>
                  </div>
                  <div className="mem-txt">{it.text}</div>
                </div>
              );
            })
          )}
        </div>
        <div id="sMemErr" className="msg-err">
          {memErr}
        </div>
      </section>
    </div>
  );
};
