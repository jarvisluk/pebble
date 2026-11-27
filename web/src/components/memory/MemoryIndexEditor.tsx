import React, { useCallback, useEffect, useRef, useState } from 'react';
import type { ResolvedLang } from '../../types';
import { translate } from '../../lib/i18n';
import { countLines, fetchMemoryIndex, saveMemoryIndex } from '../../lib/memoryApi';

interface MemoryIndexEditorProps {
  scope?: 'global';
  botId?: string;
  active: boolean;
  refreshSeq?: number;
  compact?: boolean;
  lang: ResolvedLang;
  onToast: (msg: string) => void;
}

export const MemoryIndexEditor: React.FC<MemoryIndexEditorProps> = ({
  scope = 'global',
  botId,
  active,
  refreshSeq = 0,
  compact = false,
  lang,
  onToast,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const defaultMax = botId ? 150 : 100;

  const [expanded, setExpanded] = useState(false);
  const [baseContent, setBaseContent] = useState('');
  const [draft, setDraft] = useState('');
  const [maxLines, setMaxLines] = useState(defaultMax);
  const [serverLines, setServerLines] = useState(0);
  const [touched, setTouched] = useState(false);
  const touchedRef = useRef(false);
  touchedRef.current = touched;
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  const loadIndex = useCallback(
    async (preserveDirty = false) => {
      setLoading(true);
      setErr('');
      try {
        const doc = await fetchMemoryIndex(botId ? { botId } : { scope });
        const nextContent = doc.content ?? '';
        setBaseContent(nextContent);
        setMaxLines(doc.maxLines > 0 ? doc.maxLines : defaultMax);
        setServerLines(typeof doc.lines === 'number' ? doc.lines : countLines(nextContent));
        if (!preserveDirty) {
          setDraft(nextContent);
          setTouched(false);
        }
      } catch (e) {
        if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
      } finally {
        setLoading(false);
      }
    },
    [botId, scope, defaultMax]
  );

  useEffect(() => {
    if (!active) return;
    setExpanded(false);
    setTouched(false);
    void loadIndex(false);
  }, [active, botId, scope, loadIndex]);

  useEffect(() => {
    if (!active || refreshSeq <= 0) return;
    void loadIndex(touchedRef.current);
  }, [refreshSeq, active, loadIndex]);

  const isDirty = touched && draft !== baseContent;
  const liveLines = touched ? countLines(draft) : serverLines || countLines(baseContent);
  const overBudget = maxLines > 0 && liveLines > maxLines;

  const handleSave = async () => {
    setSaving(true);
    setErr('');
    try {
      const res = await saveMemoryIndex(
        botId ? { botId, content: draft } : { scope, content: draft }
      );
      const nextMax = res.maxLines > 0 ? res.maxLines : maxLines;
      const nextLines = typeof res.lines === 'number' ? res.lines : countLines(draft);
      setBaseContent(draft);
      setMaxLines(nextMax);
      setServerLines(nextLines);
      setTouched(false);
      onToast(t('memIndexSaved'));
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setErr(e.message);
    } finally {
      setSaving(false);
    }
  };

  const budgetPill = (
    <span className={`mp${overBudget ? ' bad' : ''}`}>
      {loading && !expanded ? t('loading') : t('memIndexLines', { lines: liveLines, max: maxLines })}
    </span>
  );

  const editorBody = expanded && (
    <div className="mem-idx-box">
      <div className="fld">
        <textarea
          rows={compact ? 7 : 10}
          className={compact ? 'ta-md' : 'ta-lg'}
          spellCheck={false}
          placeholder={t('memIndexPh')}
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
            setTouched(true);
          }}
        />
      </div>
      <div className="row-between row-wrap">
        <button
          type="button"
          className="mp"
          disabled={loading}
          onClick={() => void loadIndex(false)}
        >
          {t('refresh')}
        </button>
        <div className="row-inline-sm">
          {isDirty && (
            <button
              type="button"
              className="mp"
              disabled={saving}
              onClick={() => {
                setDraft(baseContent);
                setTouched(false);
                setErr('');
              }}
            >
              {t('cancel')}
            </button>
          )}
          <button
            type="button"
            className="mp go"
            disabled={!isDirty || saving}
            onClick={() => void handleSave()}
          >
            {saving ? t('saving') : t('save')}
          </button>
        </div>
      </div>
      {err && <div className="msg-err">{err}</div>}
    </div>
  );

  if (compact) {
    return (
      <div className="mem-idx-panel">
        <div className="mem-idx-hd">
          <div className="row-inline-sm row-wrap">
            <span className="mem-idx-title">{t('memIndexBotTitle')}</span>
            {budgetPill}
          </div>
          <button
            type="button"
            className="mp"
            aria-expanded={expanded}
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? t('memIndexCollapse') : t('memIndexExpand')}
          </button>
        </div>
        {!expanded && err && <div className="msg-err">{err}</div>}
        {editorBody}
      </div>
    );
  }

  return (
    <section className="set-card" id="sMemIndexCard">
      <div className="set-card-hd">
        <div>
          <h2>{t('memIndexGlobalTitle')}</h2>
          <span className="set-card-desc">{t('memIndexGlobalDesc')}</span>
        </div>
        <div className="mem-card-hd-acts">
          {budgetPill}
          <button
            type="button"
            id="sMemIndexToggle"
            className="mp"
            aria-expanded={expanded}
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? t('memIndexCollapse') : t('memIndexExpand')}
          </button>
        </div>
      </div>
      {!expanded && err && <div className="msg-err">{err}</div>}
      {editorBody}
    </section>
  );
};
