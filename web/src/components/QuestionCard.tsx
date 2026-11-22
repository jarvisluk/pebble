import React, { useEffect, useState } from 'react';
import type { PendingQuestion, QuestionField, ResolvedLang } from '../types';
import { translate } from '../lib/i18n';

interface QuestionCardProps {
  question: PendingQuestion;
  lang: ResolvedLang;
  onReply: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
}

function initialValues(fields: QuestionField[]): {
  values: Record<string, unknown>;
  custom: Record<string, string>;
  customActive: Record<string, boolean>;
} {
  const values: Record<string, unknown> = {};
  const custom: Record<string, string> = {};
  const customActive: Record<string, boolean> = {};
  for (const f of fields) {
    if (f.type === 'multiselect') {
      values[f.key] = Array.isArray(f.default) ? f.default.map(String) : [];
    } else if (f.type === 'boolean') {
      if (typeof f.default === 'boolean') values[f.key] = f.default;
    } else if (f.default !== undefined && f.default !== null) {
      values[f.key] = String(f.default);
    } else {
      values[f.key] = '';
    }
  }
  return { values, custom, customActive };
}

export const QuestionCard: React.FC<QuestionCardProps> = ({ question, lang, onReply }) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [values, setValues] = useState<Record<string, unknown>>(() => initialValues(question.fields).values);
  const [customText, setCustomText] = useState<Record<string, string>>({});
  const [customActive, setCustomActive] = useState<Record<string, boolean>>({});
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    const init = initialValues(question.fields);
    setValues(init.values);
    setCustomText(init.custom);
    setCustomActive(init.customActive);
    setSubmitting(false);
  }, [question.id]);

  const buildAnswers = (): Record<string, unknown> => {
    const out: Record<string, unknown> = {};
    for (const f of question.fields) {
      const raw = values[f.key];
      if (f.type === 'multiselect') {
        const list = Array.isArray(raw) ? [...(raw as string[])] : [];
        const c = (customText[f.key] || '').trim();
        if (f.custom && c && !list.includes(c)) {
          list.push(c);
        }
        out[f.key] = list;
      } else if (f.type === 'boolean') {
        out[f.key] = Boolean(raw);
      } else if (f.type === 'number' || f.type === 'integer') {
        const s = String(raw ?? '').trim();
        if (s !== '') {
          const n = f.type === 'integer' ? parseInt(s, 10) : Number(s);
          if (!Number.isNaN(n)) out[f.key] = n;
        }
      } else if (f.options && f.options.length > 0 && customActive[f.key]) {
        out[f.key] = (customText[f.key] || '').trim();
      } else {
        out[f.key] = String(raw ?? '');
      }
    }
    return out;
  };

  const canSubmit = question.fields.every((f) => {
    if (!f.required) return true;
    const raw = values[f.key];
    if (f.type === 'multiselect') {
      const len = Array.isArray(raw) ? raw.length : 0;
      const c = (customText[f.key] || '').trim();
      return len > 0 || (Boolean(f.custom) && c.length > 0);
    }
    if (f.type === 'boolean') return typeof raw === 'boolean';
    if (f.options && f.options.length > 0 && customActive[f.key]) {
      return (customText[f.key] || '').trim().length > 0;
    }
    return String(raw ?? '').trim().length > 0;
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    try {
      await onReply(question.id, buildAnswers(), false);
    } finally {
      setSubmitting(false);
    }
  };

  const handleCancel = async () => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await onReply(question.id, {}, true);
    } finally {
      setSubmitting(false);
    }
  };

  const title = question.title || t('questionTitle');

  return (
    <form className="q-card fresh" onSubmit={(e) => void handleSubmit(e)}>
      <div className="q-hd">
        <span className="q-title">{title}</span>
        <span className="q-badge">{t('questionWaitingBadge')}</span>
      </div>

      <div className="q-fields">
        {question.fields.map((f) => {
          const isMulti = f.type === 'multiselect';
          const hasOpts = Array.isArray(f.options) && f.options.length > 0;
          const selectedList = Array.isArray(values[f.key]) ? (values[f.key] as string[]) : [];
          const selectedSingle = typeof values[f.key] === 'string' ? (values[f.key] as string) : '';

          return (
            <div key={f.key} className="q-field">
              {(f.title || isMulti) && (
                <div className="q-field-hd">
                  {f.title && <span className="q-field-title">{f.title}</span>}
                  {isMulti && <span className="q-field-hint">{t('questionMultiHint')}</span>}
                </div>
              )}
              {f.description && <div className="q-field-desc">{f.description}</div>}

              {f.type === 'boolean' ? (
                <div className="q-bool">
                  <button
                    type="button"
                    disabled={submitting}
                    className={`q-opt${values[f.key] === true ? ' on' : ''}`}
                    onClick={() => setValues((p) => ({ ...p, [f.key]: true }))}
                  >
                    <span className="q-opt-lbl">{t('questionYes')}</span>
                  </button>
                  <button
                    type="button"
                    disabled={submitting}
                    className={`q-opt${values[f.key] === false ? ' on' : ''}`}
                    onClick={() => setValues((p) => ({ ...p, [f.key]: false }))}
                  >
                    <span className="q-opt-lbl">{t('questionNo')}</span>
                  </button>
                </div>
              ) : hasOpts ? (
                <div className="q-opts">
                  {f.options!.map((opt) => {
                    const active = isMulti
                      ? selectedList.includes(opt.value)
                      : !customActive[f.key] && selectedSingle === opt.value;
                    return (
                      <button
                        key={opt.value}
                        type="button"
                        disabled={submitting}
                        className={`q-opt${active ? ' on' : ''}`}
                        onClick={() => {
                          if (isMulti) {
                            setValues((p) => {
                              const cur = Array.isArray(p[f.key]) ? (p[f.key] as string[]) : [];
                              const next = cur.includes(opt.value)
                                ? cur.filter((x) => x !== opt.value)
                                : [...cur, opt.value];
                              return { ...p, [f.key]: next };
                            });
                          } else {
                            setCustomActive((p) => ({ ...p, [f.key]: false }));
                            setValues((p) => ({ ...p, [f.key]: opt.value }));
                          }
                        }}
                      >
                        <span className="q-opt-lbl">{opt.label || opt.value}</span>
                        {opt.description && <span className="q-opt-desc">{opt.description}</span>}
                      </button>
                    );
                  })}
                  {f.custom && (
                    <input
                      type="text"
                      className={`q-input${!isMulti && customActive[f.key] ? ' on' : ''}`}
                      disabled={submitting}
                      placeholder={t('questionCustomPlaceholder')}
                      value={customText[f.key] || ''}
                      onFocus={() => {
                        if (!isMulti) setCustomActive((p) => ({ ...p, [f.key]: true }));
                      }}
                      onChange={(e) => {
                        const v = e.target.value;
                        setCustomText((p) => ({ ...p, [f.key]: v }));
                        if (!isMulti) setCustomActive((p) => ({ ...p, [f.key]: v.length > 0 }));
                      }}
                    />
                  )}
                </div>
              ) : (
                <input
                  type={f.type === 'number' || f.type === 'integer' ? 'number' : 'text'}
                  step={f.type === 'integer' ? '1' : f.type === 'number' ? 'any' : undefined}
                  className="q-input"
                  disabled={submitting}
                  placeholder={f.placeholder || t('questionInputPlaceholder')}
                  value={String(values[f.key] ?? '')}
                  onChange={(e) => setValues((p) => ({ ...p, [f.key]: e.target.value }))}
                />
              )}
            </div>
          );
        })}
      </div>

      <div className="q-acts">
        <button type="button" className="btn" disabled={submitting} onClick={() => void handleCancel()}>
          {t('questionCancel')}
        </button>
        <button type="submit" className="btn go" disabled={submitting || !canSubmit}>
          {t('questionSubmit')}
        </button>
      </div>
    </form>
  );
};
