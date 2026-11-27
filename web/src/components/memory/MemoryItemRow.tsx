import React, { useState } from 'react';
import type { MemoryItem, ResolvedLang } from '../../types';
import { translate } from '../../lib/i18n';
import {
  deleteMemory,
  formatMemoryStamp,
  MEMORY_KIND_LABEL_MAP,
  MEMORY_KINDS,
  parseTagsInput,
  patchMemory,
} from '../../lib/memoryApi';

interface MemoryItemRowProps {
  item: MemoryItem;
  lang: ResolvedLang;
  showScope?: boolean;
  showStatus?: boolean;
  compact?: boolean;
  onMutated: () => Promise<void>;
  onToast: (msg: string) => void;
  onError: (msg: string) => void;
}

export const MemoryItemRow: React.FC<MemoryItemRowProps> = ({
  item,
  lang,
  showScope = false,
  showStatus = false,
  compact = false,
  onMutated,
  onToast,
  onError,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState('');
  const [editKind, setEditKind] = useState('fact');
  const [editTags, setEditTags] = useState('');
  const [editText, setEditText] = useState('');
  const [editWhy, setEditWhy] = useState('');
  const [editPin, setEditPin] = useState(false);
  const [editErr, setEditErr] = useState('');

  const visibleTags = (item.tags || []).filter((tg) => !tg.startsWith('bot:'));
  const status = item.status || 'active';
  const isCandidate = status === 'candidate';
  const isArchivedOrSuperseded = status === 'archived' || status === 'superseded';
  const stamp = formatMemoryStamp(item.updatedAt || item.createdAt);

  const kindLabel = item.kind
    ? MEMORY_KIND_LABEL_MAP[item.kind]
      ? t(MEMORY_KIND_LABEL_MAP[item.kind])
      : item.kind
    : '';

  const openEdit = () => {
    setEditTitle(item.title || '');
    setEditKind(item.kind || 'fact');
    setEditTags(visibleTags.join(', '));
    setEditText(item.text || item.snippet || '');
    setEditWhy(item.why || '');
    setEditPin(Boolean(item.pin));
    setEditErr('');
    setEditing(true);
  };

  const runAction = async (fn: () => Promise<void>) => {
    setBusy(true);
    onError('');
    try {
      await fn();
      await onMutated();
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') onError(e.message);
    } finally {
      setBusy(false);
    }
  };

  const handleSaveEdit = async () => {
    const text = editText.trim();
    if (!text) {
      setEditErr(t('memContentReq'));
      return;
    }
    setBusy(true);
    setEditErr('');
    onError('');
    try {
      await patchMemory(item.id, {
        title: editTitle.trim(),
        kind: editKind,
        tags: parseTagsInput(editTags),
        text,
        why: editWhy.trim(),
        pin: editPin,
      });
      setEditing(false);
      onToast(t('memUpdatedToast'));
      await onMutated();
    } catch (e) {
      if (e instanceof Error && e.message !== 'unauthorized') setEditErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  const statusPill = (() => {
    if (!showStatus && status === 'active') return null;
    if (status === 'candidate') return <span className="mp warn">{t('memStatusCandidate')}</span>;
    if (status === 'superseded') return <span className="mp">{t('memStatusSuperseded')}</span>;
    if (status === 'archived') return <span className="mp">{t('memStatusArchived')}</span>;
    return <span className="mp ok">{t('memStatusActive')}</span>;
  })();

  const subMetaParts: string[] = [];
  if (item.validFrom) subMetaParts.push(t('memValidFrom', { date: item.validFrom.slice(0, 10) }));
  if (item.validUntil) subMetaParts.push(t('memValidUntil', { date: item.validUntil.slice(0, 10) }));
  if (item.supersededBy) subMetaParts.push(t('memSupersededBy', { id: item.supersededBy }));
  if (item.sourceAuthor) subMetaParts.push(t('memSourceAuthor', { author: item.sourceAuthor }));

  return (
    <div className="mem-row">
      <div className="mem-top">
        <div className="mem-meta">
          {item.pin && <span className="mp pin-pill">{t('memPinShort')}</span>}
          {kindLabel && <span className="mp ok">{kindLabel}</span>}
          {statusPill}
          {showScope && (item.scope || item.botId) && (
            <span className="mp">
              {item.scope === 'everywhere' || item.scope === 'global'
                ? t('memScopeEverywhere')
                : item.botId
                  ? `@${item.botId}`
                  : t('memScopeProject')}
            </span>
          )}
          {visibleTags.map((tg) => (
            <span key={tg} className="mp">
              #{tg}
            </span>
          ))}
          {stamp && <span className="mem-time">{stamp}</span>}
        </div>
        <div className="mem-acts">
          {isCandidate && (
            <>
              <button
                type="button"
                className="mp ok"
                disabled={busy}
                onClick={() =>
                  void runAction(async () => {
                    await patchMemory(item.id, { status: 'active' });
                    onToast(t('memApprovedToast'));
                  })
                }
              >
                {t('memApproveBtn')}
              </button>
              <button
                type="button"
                className="mp bad"
                disabled={busy}
                onClick={() =>
                  void runAction(async () => {
                    await deleteMemory(item.id, { dismiss: true });
                    onToast(t('memDismissedToast'));
                  })
                }
              >
                {t('memDismissBtn')}
              </button>
            </>
          )}
          <button
            type="button"
            className="mp"
            disabled={busy}
            onClick={() =>
              void runAction(async () => {
                const nextPin = !item.pin;
                await patchMemory(item.id, { pin: nextPin });
                onToast(t(nextPin ? 'memPinnedToast' : 'memUnpinnedToast'));
              })
            }
          >
            {item.pin ? t('memUnpinBtn') : t('memPinBtn')}
          </button>
          <button
            type="button"
            className="mp"
            disabled={busy}
            onClick={() => {
              if (editing) setEditing(false);
              else openEdit();
            }}
          >
            {editing ? t('cancel') : t('memEditBtn')}
          </button>
          {!isCandidate && (
            <button
              type="button"
              className="mp bad"
              disabled={busy}
              onClick={() =>
                void runAction(async () => {
                  await deleteMemory(item.id, isArchivedOrSuperseded ? { hard: true } : undefined);
                  onToast(t(isArchivedOrSuperseded ? 'memDeleted' : 'memArchivedToast'));
                })
              }
            >
              {isArchivedOrSuperseded ? t('memHardDeleteBtn') : t('memArchiveBtn')}
            </button>
          )}
        </div>
      </div>

      {editing ? (
        <div className="add-prov mem-add">
          <div className="fld">
            <label dangerouslySetInnerHTML={{ __html: t('memTitleLbl') }} />
            <input
              placeholder={t('memTitlePh')}
              value={editTitle}
              onChange={(e) => setEditTitle(e.target.value)}
            />
          </div>
          <div className={compact ? 'stack6' : 'two'}>
            <div className="fld">
              <label>{t('memType')}</label>
              <select value={editKind} onChange={(e) => setEditKind(e.target.value)}>
                {MEMORY_KINDS.map(([k, lblKey]) => (
                  <option key={k} value={k}>
                    {t(lblKey)}
                  </option>
                ))}
              </select>
            </div>
            <div className="fld">
              <label dangerouslySetInnerHTML={{ __html: t('tagsOptional') }} />
              <input
                placeholder={t('tagsPh')}
                value={editTags}
                onChange={(e) => setEditTags(e.target.value)}
              />
            </div>
          </div>
          <div className="fld">
            <label>{t('memContent')}</label>
            <textarea
              rows={3}
              className="ta-sm ta-font"
              value={editText}
              onChange={(e) => setEditText(e.target.value)}
            />
          </div>
          <div className="fld">
            <label dangerouslySetInnerHTML={{ __html: t('memWhyLbl') }} />
            <input
              placeholder={t('memWhyPh')}
              value={editWhy}
              onChange={(e) => setEditWhy(e.target.value)}
            />
          </div>
          <div className="row-between row-wrap">
            <label className="perm perm-card">
              <input
                type="checkbox"
                checked={editPin}
                onChange={(e) => setEditPin(e.target.checked)}
              />
              {t('memPinLabel')}
            </label>
            <div className="row-inline-sm">
              <button
                type="button"
                className="mp"
                disabled={busy}
                onClick={() => {
                  setEditing(false);
                  setEditErr('');
                }}
              >
                {t('cancel')}
              </button>
              <button
                type="button"
                className="mp go"
                disabled={busy}
                onClick={() => void handleSaveEdit()}
              >
                {busy ? t('saving') : t('save')}
              </button>
            </div>
          </div>
          {editErr && <div className="msg-err">{editErr}</div>}
        </div>
      ) : (
        <>
          {item.title && <div className="mem-title">{item.title}</div>}
          <div className="mem-txt">{item.text || item.snippet || ''}</div>
          {item.why && <div className="mem-why">{item.why}</div>}
          {subMetaParts.length > 0 && (
            <div className="mem-sub-meta">
              {subMetaParts.map((part, i) => (
                <span key={i}>{part}</span>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
};
