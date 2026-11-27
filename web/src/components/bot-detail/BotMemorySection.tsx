import React, { useCallback, useEffect, useState } from 'react';
import type { MemoryResponse, MemoryStatusFilter, ResolvedLang, SettingsData } from '../../types';
import { translate } from '../../lib/i18n';
import {
  listMemories,
  MEMORY_KINDS,
  parseTagsInput,
  saveMemory,
  triggerMemoryDream,
} from '../../lib/memoryApi';
import { MemoryIndexEditor } from '../memory/MemoryIndexEditor';
import { MemoryItemRow } from '../memory/MemoryItemRow';

interface BotMemorySectionProps {
  open: boolean;
  botId: string;
  settings?: SettingsData | null;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
  onSettingsUpdated?: (s: SettingsData) => void;
}

const BOT_STATUS_TABS: readonly [MemoryStatusFilter, string][] = [
  ['active', 'memStatusActive'],
  ['candidate', 'memStatusCandidate'],
  ['all', 'memStatusAll'],
] as const;

export const BotMemorySection: React.FC<BotMemorySectionProps> = ({
  open,
  botId,
  lang,
  onToast,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [memData, setMemData] = useState<MemoryResponse | null>(null);
  const [memLoading, setMemLoading] = useState(false);
  const [memErr, setMemErr] = useState('');
  const [memStatusFilter, setMemStatusFilter] = useState<MemoryStatusFilter>('active');
  const [memQuery, setMemQuery] = useState('');
  const [indexRefreshSeq, setIndexRefreshSeq] = useState(0);

  const [memAddOpen, setMemAddOpen] = useState(false);
  const [memTitle, setMemTitle] = useState('');
  const [memKind, setMemKind] = useState('fact');
  const [memTags, setMemTags] = useState('');
  const [memText, setMemText] = useState('');
  const [memPin, setMemPin] = useState(false);
  const [memDaily, setMemDaily] = useState(false);
  const [memSaving, setMemSaving] = useState(false);
  const [dreamRunning, setDreamRunning] = useState(false);

  const loadMemories = useCallback(
    async (
      targetBotId: string,
      override?: { status?: MemoryStatusFilter; query?: string }
    ) => {
      if (!targetBotId) return;
      setMemLoading(true);
      setMemErr('');
      try {
        const st = override?.status ?? memStatusFilter;
        const q = (override?.query !== undefined ? override.query : memQuery).trim();
        const d = await listMemories({
          scope: 'bot',
          botId: targetBotId,
          status: st,
          query: q || undefined,
          limit: 50,
        });
        setMemData(d);
      } catch (e) {
        if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
      } finally {
        setMemLoading(false);
      }
    },
    [memStatusFilter, memQuery]
  );

  useEffect(() => {
    if (!open || !botId) return;
    setMemAddOpen(false);
    setMemStatusFilter('active');
    setMemQuery('');
    setMemErr('');
    void loadMemories(botId, { status: 'active', query: '' });
  }, [open, botId]);

  const refreshBotMemory = useCallback(async () => {
    await loadMemories(botId);
    setIndexRefreshSeq((s) => s + 1);
  }, [botId, loadMemories]);

  const handleSaveMemory = async () => {
    if (!botId) return;
    const text = memText.trim();
    if (!text) {
      setMemErr(t('memContentReq'));
      return;
    }
    setMemSaving(true);
    setMemErr('');
    try {
      if (memDaily) {
        await saveMemory({
          scope: 'bot',
          botId,
          text,
          daily: true,
        });
        onToast(t('memDailySaved'));
      } else {
        const tags = parseTagsInput(memTags);
        const res = await saveMemory({
          scope: 'bot',
          botId,
          kind: memKind,
          title: memTitle.trim() || undefined,
          text,
          tags: tags.length ? tags : undefined,
          pin: memPin || undefined,
        });
        const similarLen = res.similarExisting?.length ?? 0;
        onToast(similarLen > 0 ? t('memSimilarHint', { n: similarLen }) : t('botMemSaved'));
      }
      setMemTitle('');
      setMemTags('');
      setMemText('');
      setMemPin(false);
      setMemDaily(false);
      setMemAddOpen(false);
      await refreshBotMemory();
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    } finally {
      setMemSaving(false);
    }
  };

  const handleRunDream = async () => {
    if (!botId) return;
    setDreamRunning(true);
    setMemErr('');
    try {
      await triggerMemoryDream(botId);
      onToast(t('memDreamDone'));
      await refreshBotMemory();
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setMemErr(e.message);
    } finally {
      setDreamRunning(false);
    }
  };

  const candidateCount = memData?.counts?.candidate ?? 0;

  return (
    <>
      <div className="sec row-between">
        <span>{t('botMemory')}</span>
        <div className="row-inline-sm">
          <button
            type="button"
            id="pMemDreamBtn"
            className="mp"
            disabled={dreamRunning}
            onClick={() => void handleRunDream()}
          >
            {dreamRunning ? t('memRunningDream') : t('memBotDreamBtn')}
          </button>
          <button
            type="button"
            id="pMemAddToggle"
            className="mp go"
            onClick={() => setMemAddOpen((v) => !v)}
          >
            {t('addShort')}
          </button>
        </div>
      </div>

      <MemoryIndexEditor
        botId={botId}
        active={open && Boolean(botId)}
        refreshSeq={indexRefreshSeq}
        compact
        lang={lang}
        onToast={onToast}
      />

      {memAddOpen && (
        <div id="pMemAddBox" className="add-prov mem-add mb-8">
          <label className="perm perm-card">
            <input
              type="checkbox"
              id="pMemDaily"
              checked={memDaily}
              onChange={(e) => setMemDaily(e.target.checked)}
            />
            {t('memDailyLabel')}
          </label>
          {!memDaily && (
            <>
              <div className="fld">
                <label htmlFor="pMemTitle" dangerouslySetInnerHTML={{ __html: t('memTitleLbl') }} />
                <input
                  id="pMemTitle"
                  placeholder={t('memTitlePh')}
                  value={memTitle}
                  onChange={(e) => setMemTitle(e.target.value)}
                />
              </div>
              <div className="fld">
                <label htmlFor="pMemKind">{t('memType')}</label>
                <select id="pMemKind" value={memKind} onChange={(e) => setMemKind(e.target.value)}>
                  {MEMORY_KINDS.map(([k, lblKey]) => (
                    <option key={k} value={k}>
                      {t(lblKey)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="pMemTags" dangerouslySetInnerHTML={{ __html: t('tagsOptional') }} />
                <input
                  id="pMemTags"
                  placeholder={t('tagsPh')}
                  value={memTags}
                  onChange={(e) => setMemTags(e.target.value)}
                />
              </div>
            </>
          )}
          <div className="fld">
            <label htmlFor="pMemText">{t('memContent')}</label>
            <textarea
              id="pMemText"
              rows={3}
              className="ta-sm ta-font"
              placeholder={memDaily ? t('memDailyPh') : t('botMemPh')}
              value={memText}
              onChange={(e) => setMemText(e.target.value)}
            />
          </div>
          <div className="row-between row-wrap">
            {!memDaily ? (
              <label className="perm perm-card">
                <input
                  type="checkbox"
                  id="pMemPin"
                  checked={memPin}
                  onChange={(e) => setMemPin(e.target.checked)}
                />
                {t('memPinBtn')}
              </label>
            ) : (
              <span />
            )}
            <div className="row-inline-sm">
              <button
                type="button"
                id="pMemAddCancel"
                className="mp"
                disabled={memSaving}
                onClick={() => {
                  setMemAddOpen(false);
                  setMemErr('');
                }}
              >
                {t('cancel')}
              </button>
              <button
                type="button"
                id="pMemAddSubmit"
                className="mp go"
                disabled={memSaving}
                onClick={() => void handleSaveMemory()}
              >
                {memSaving ? t('saving') : t('save')}
              </button>
            </div>
          </div>
        </div>
      )}

      <div className="mem-panel-ctrl">
        <div className="mem-scope-seg self-start" role="group" aria-label={t('memStatusAria')}>
          {BOT_STATUS_TABS.map(([st, lblKey]) => (
            <button
              key={st}
              type="button"
              id={`pMemStatus_${st}`}
              className={`mp${memStatusFilter === st ? ' on' : ''}`}
              onClick={() => {
                setMemStatusFilter(st);
                void loadMemories(botId, { status: st });
              }}
            >
              <span>{t(lblKey)}</span>
              {st === 'candidate' && candidateCount > 0 && (
                <span className="mem-badge-n">{candidateCount}</span>
              )}
            </button>
          ))}
        </div>

        <div className="mem-panel-search">
          <input
            id="pMemQuery"
            className="note-in"
            placeholder={t('memSearchBotPh')}
            value={memQuery}
            onChange={(e) => setMemQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                e.preventDefault();
                void loadMemories(botId);
              }
            }}
          />
          {memQuery.trim() !== '' && (
            <button
              type="button"
              className="mp"
              onClick={() => {
                setMemQuery('');
                void loadMemories(botId, { query: '' });
              }}
            >
              {t('memClearSearch')}
            </button>
          )}
          <button
            type="button"
            className="mp"
            onClick={() => void loadMemories(botId)}
          >
            {t('search')}
          </button>
        </div>
      </div>

      <div id="pMemList" className="stack6">
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
              showStatus={memStatusFilter === 'all'}
              compact
              onMutated={refreshBotMemory}
              onToast={onToast}
              onError={setMemErr}
            />
          ))
        )}
      </div>
      <div id="pMemErr" className="msg-err">
        {memErr}
      </div>
    </>
  );
};
