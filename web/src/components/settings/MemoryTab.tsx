import React, { useCallback, useEffect, useRef, useState } from 'react';
import type {
  MemoryResponse,
  MemoryStatus,
  MemoryStatusFilter,
  ResolvedLang,
  SettingsData,
} from '../../types';
import { api } from '../../lib/api';
import { translate } from '../../lib/i18n';
import {
  fetchMemoryStatus,
  listMemories,
  MEMORY_KINDS,
} from '../../lib/memoryApi';
import { MemoryAddForm } from '../memory/MemoryAddForm';
import { MemoryIndexEditor } from '../memory/MemoryIndexEditor';
import { MemoryItemRow } from '../memory/MemoryItemRow';
import { MemorySettingsCard } from '../memory/MemorySettingsCard';

interface MemoryTabProps {
  active: boolean;
  settings: SettingsData | null;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onSettingsUpdated: (s: SettingsData) => void;
  onAfterMagpieInstall?: (fresh: SettingsData) => void;
  onDirtyChange: (dirty: boolean) => void;
  saveRef: React.MutableRefObject<(() => Promise<boolean>) | null>;
  resetRef: React.MutableRefObject<(() => void) | null>;
}

const STATUS_TABS: readonly [MemoryStatusFilter, string][] = [
  ['active', 'memStatusActive'],
  ['candidate', 'memStatusCandidate'],
  ['superseded', 'memStatusSuperseded'],
  ['archived', 'memStatusArchived'],
  ['all', 'memStatusAll'],
] as const;

export const MemoryTab: React.FC<MemoryTabProps> = ({
  active,
  settings,
  lang,
  onToast,
  onSettingsUpdated,
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

  const [memConfigDirty, setMemConfigDirty] = useState(false);
  const memConfigSaveRef = useRef<(() => Promise<boolean>) | null>(null);
  const memConfigResetRef = useRef<(() => void) | null>(null);

  const [memData, setMemData] = useState<MemoryResponse | null>(null);
  const [memStatusData, setMemStatusData] = useState<MemoryStatus | null>(null);
  const [memLoading, setMemLoading] = useState(false);
  const [memStatus, setMemStatus] = useState('');
  const [memStatusClass, setMemStatusClass] = useState('');
  const [memScope, setMemScope] = useState<'global' | 'all'>('global');
  const [memStatusFilter, setMemStatusFilter] = useState<MemoryStatusFilter>('active');
  const [memFilterKind, setMemFilterKind] = useState('');
  const [memQuery, setMemQuery] = useState('');
  const [indexRefreshSeq, setIndexRefreshSeq] = useState(0);

  const [memAddOpen, setMemAddOpen] = useState(false);
  const [memErr, setMemErr] = useState('');

  const userMdDirty = Boolean(
    settings &&
      userMdTouched &&
      sharedUserMd.replace(/\s+$/, '') !== baseUserMd.replace(/\s+$/, '')
  );
  const anyDirty = userMdDirty || memConfigDirty;

  useEffect(() => {
    if (!userMdTouched) setSharedUserMd(baseUserMd);
  }, [baseUserMd, userMdTouched]);

  useEffect(() => {
    onDirtyChange(anyDirty);
  }, [anyDirty, onDirtyChange]);

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

  const saveAllDirty = useCallback(async (): Promise<boolean> => {
    let ok = true;
    if (userMdDirty) {
      ok = (await saveUserMd()) && ok;
    }
    if (memConfigDirty && memConfigSaveRef.current) {
      ok = (await memConfigSaveRef.current()) && ok;
    }
    return ok;
  }, [userMdDirty, memConfigDirty, saveUserMd]);

  const resetAll = useCallback(() => {
    resetUserMd();
    memConfigResetRef.current?.();
  }, [resetUserMd]);

  useEffect(() => {
    saveRef.current = saveAllDirty;
    resetRef.current = resetAll;
    return () => {
      saveRef.current = null;
      resetRef.current = null;
    };
  }, [saveRef, resetRef, saveAllDirty, resetAll]);

  const loadGlobalMemories = useCallback(
    async (override?: {
      scope?: 'global' | 'all';
      status?: MemoryStatusFilter;
      kind?: string;
      query?: string;
    }) => {
      setMemErr('');
      setMemLoading(true);
      setMemStatus(translate(lang, 'loading'));
      setMemStatusClass('');
      try {
        const sc = override?.scope ?? memScope;
        const st = override?.status ?? memStatusFilter;
        const kd = override?.kind !== undefined ? override.kind : memFilterKind;
        const q = (override?.query !== undefined ? override.query : memQuery).trim();
        const d = await listMemories({
          scope: sc,
          status: st,
          kind: kd || undefined,
          query: q || undefined,
          limit: 50,
        });
        setMemData(d);
        if (d.available === false) {
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
      } finally {
        setMemLoading(false);
      }
    },
    [lang, memScope, memStatusFilter, memFilterKind, memQuery]
  );

  const loadMemoryStatus = useCallback(async () => {
    try {
      const st = await fetchMemoryStatus();
      setMemStatusData(st);
    } catch {
      // Status endpoint may be unavailable on older servers; keep UI resilient.
    }
  }, []);

  const refreshAll = useCallback(async () => {
    await Promise.all([loadGlobalMemories(), loadMemoryStatus()]);
    setIndexRefreshSeq((s) => s + 1);
  }, [loadGlobalMemories, loadMemoryStatus]);

  useEffect(() => {
    if (!active) return;
    void loadGlobalMemories();
    void loadMemoryStatus();
  }, [active]);

  const counts = memData?.counts ?? memStatusData?.counts;
  const candidateCount = counts?.candidate ?? 0;
  const semanticInfo = memData?.semantic ?? memStatusData?.semantic;

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
            disabled={!userMdDirty || userMdSaving}
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

      <MemoryIndexEditor
        scope="global"
        active={active}
        refreshSeq={indexRefreshSeq}
        lang={lang}
        onToast={onToast}
      />

      <section className="set-card" id="sMemCard">
        <div className="set-card-hd">
          <div>
            <h2>{t('magpieGlobalMem')}</h2>
            <span className="set-card-desc">{t('magpieGlobalMemDesc')}</span>
          </div>
          <div className="mem-card-hd-acts">
            {semanticInfo?.active && (
              <span id="sMemSemanticBadge" className="mp ok">
                {t('memSemanticOn')}
              </span>
            )}
            <span id="sMemStatus" className={`mp ${memStatusClass}`}>
              {memStatus || t('notLoaded')}
            </span>
            <button
              type="button"
              id="sMemRefresh"
              className="mp"
              disabled={memLoading}
              onClick={() => void refreshAll()}
            >
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

        {memAddOpen && (
          <MemoryAddForm
            lang={lang}
            onError={setMemErr}
            onCancel={() => {
              setMemAddOpen(false);
              setMemErr('');
            }}
            onSaved={async (similarLen) => {
              setMemAddOpen(false);
              onToast(
                similarLen > 0 ? t('memSimilarHint', { n: similarLen }) : t('globalMemSaved')
              );
              await refreshAll();
            }}
          />
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
            {MEMORY_KINDS.map(([k, lblKey]) => (
              <option key={k} value={k}>
                {t(lblKey)}
              </option>
            ))}
          </select>
        </div>

        <div className="mem-scope-seg self-start" role="group" aria-label={t('memStatusAria')}>
          {STATUS_TABS.map(([st, lblKey]) => (
            <button
              key={st}
              type="button"
              id={`sMemStatus_${st}`}
              className={`mp${memStatusFilter === st ? ' on' : ''}`}
              onClick={() => {
                setMemStatusFilter(st);
                void loadGlobalMemories({ status: st });
              }}
            >
              <span>{t(lblKey)}</span>
              {st === 'candidate' && candidateCount > 0 && (
                <span className="mem-badge-n">{candidateCount}</span>
              )}
            </button>
          ))}
        </div>

        <div className="mem-bar">
          <input
            id="sMemQuery"
            className="note-in"
            placeholder={semanticInfo?.active ? t('searchSemanticMemPh') : t('searchGlobalMemPh')}
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
          <button
            type="button"
            id="sMemSearchBtn"
            className="mp"
            onClick={() => void loadGlobalMemories()}
          >
            {t('search')}
          </button>
        </div>

        <div id="sMemList" className="stack6">
          {memLoading && !memData ? (
            <div className="set-card-desc">{t('loadingEllipsis')}</div>
          ) : memData?.available === false ? (
            <div className="set-card-desc">
              {t('memUnavailable')}
              {memData.error ? `: ${memData.error}` : ''}
            </div>
          ) : !(memData?.items || []).length ? (
            <div className="set-card-desc">{t('memEmpty')}</div>
          ) : (
            (memData?.items || []).map((it) => (
              <MemoryItemRow
                key={it.id}
                item={it}
                lang={lang}
                showScope={memScope === 'all'}
                showStatus={memStatusFilter === 'all'}
                onMutated={refreshAll}
                onToast={onToast}
                onError={setMemErr}
              />
            ))
          )}
        </div>
        <div id="sMemErr" className="msg-err">
          {memErr}
        </div>
      </section>

      <MemorySettingsCard
        settings={settings}
        memStatus={memStatusData}
        magpieInfo={memData?.magpie}
        lang={lang}
        onToast={onToast}
        onSettingsUpdated={onSettingsUpdated}
        onRefreshAll={refreshAll}
        onConfigDirtyChange={setMemConfigDirty}
        saveConfigRef={memConfigSaveRef}
        resetConfigRef={memConfigResetRef}
      />
    </div>
  );
};
