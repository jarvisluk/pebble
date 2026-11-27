import React, { useState } from 'react';
import type { ResolvedLang } from '../../types';
import { translate } from '../../lib/i18n';
import { MEMORY_KINDS, parseTagsInput, saveMemory } from '../../lib/memoryApi';

interface MemoryAddFormProps {
  lang: ResolvedLang;
  onSaved: (similarCount: number) => Promise<void>;
  onCancel: () => void;
  onError: (msg: string) => void;
}

export const MemoryAddForm: React.FC<MemoryAddFormProps> = ({
  lang,
  onSaved,
  onCancel,
  onError,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [title, setTitle] = useState('');
  const [kind, setKind] = useState('fact');
  const [tags, setTags] = useState('');
  const [text, setText] = useState('');
  const [why, setWhy] = useState('');
  const [pin, setPin] = useState(false);
  const [saving, setSaving] = useState(false);

  const handleSubmit = async () => {
    const trimmed = text.trim();
    if (!trimmed) {
      onError(t('memContentReq'));
      return;
    }
    const parsedTags = parseTagsInput(tags);
    setSaving(true);
    onError('');
    try {
      const res = await saveMemory({
        scope: 'global',
        kind,
        title: title.trim() || undefined,
        text: trimmed,
        why: why.trim() || undefined,
        tags: parsedTags.length ? parsedTags : undefined,
        pin: pin || undefined,
      });
      setTitle('');
      setTags('');
      setText('');
      setWhy('');
      setPin(false);
      await onSaved(res.similarExisting?.length ?? 0);
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div id="sMemAddBox" className="add-prov mem-add">
      <div className="fld">
        <label htmlFor="sMemTitle" dangerouslySetInnerHTML={{ __html: t('memTitleLbl') }} />
        <input
          id="sMemTitle"
          placeholder={t('memTitlePh')}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
      </div>
      <div className="two">
        <div className="fld">
          <label htmlFor="sMemKind">{t('memType')}</label>
          <select id="sMemKind" value={kind} onChange={(e) => setKind(e.target.value)}>
            {MEMORY_KINDS.map(([k, lblKey]) => (
              <option key={k} value={k}>
                {t(lblKey)}
              </option>
            ))}
          </select>
        </div>
        <div className="fld">
          <label htmlFor="sMemTags" dangerouslySetInnerHTML={{ __html: t('tagsOptional') }} />
          <input
            id="sMemTags"
            placeholder={t('tagsPh')}
            value={tags}
            onChange={(e) => setTags(e.target.value)}
          />
        </div>
      </div>
      <div className="fld">
        <label htmlFor="sMemText">{t('memContent')}</label>
        <textarea
          id="sMemText"
          rows={3}
          className="ta-md ta-font"
          placeholder={t('globalMemPh')}
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
      </div>
      <div className="fld">
        <label htmlFor="sMemWhy" dangerouslySetInnerHTML={{ __html: t('memWhyLbl') }} />
        <input
          id="sMemWhy"
          placeholder={t('memWhyPh')}
          value={why}
          onChange={(e) => setWhy(e.target.value)}
        />
      </div>
      <div className="row-between row-wrap">
        <label className="perm perm-card">
          <input
            type="checkbox"
            id="sMemPin"
            checked={pin}
            onChange={(e) => setPin(e.target.checked)}
          />
          {t('memPinLabel')}
        </label>
        <div className="row-inline-sm">
          <button
            type="button"
            id="sMemAddCancel"
            className="mp"
            disabled={saving}
            onClick={onCancel}
          >
            {t('cancel')}
          </button>
          <button
            type="button"
            id="sMemAddSubmit"
            className="mp go"
            disabled={saving}
            onClick={() => void handleSubmit()}
          >
            {saving ? t('saving') : t('saveMemory')}
          </button>
        </div>
      </div>
    </div>
  );
};
