import React, { useEffect, useState } from 'react';
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import { useI18n } from '../i18n';
import { useTheme } from '../theme';
import type { PendingQuestion, QuestionField } from '../types';

interface Props {
  question: PendingQuestion;
  accent: string;
  accentText: string;
  accentSoft: string;
  onReply: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
}

function seedValues(fields: QuestionField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    if (f.type === 'multiselect') {
      out[f.key] = Array.isArray(f.default) ? f.default.map(String) : [];
    } else if (f.type === 'boolean') {
      if (typeof f.default === 'boolean') out[f.key] = f.default;
    } else if (f.default !== undefined && f.default !== null) {
      out[f.key] = String(f.default);
    } else {
      out[f.key] = '';
    }
  }
  return out;
}

export function QuestionCard({ question, accent, accentText, accentSoft, onReply }: Props) {
  const { t } = useI18n();
  const theme = useTheme();
  const [values, setValues] = useState<Record<string, unknown>>(() => seedValues(question.fields));
  const [customText, setCustomText] = useState<Record<string, string>>({});
  const [customActive, setCustomActive] = useState<Record<string, boolean>>({});
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setValues(seedValues(question.fields));
    setCustomText({});
    setCustomActive({});
    setSubmitting(false);
  }, [question.id]);

  const buildAnswers = (): Record<string, unknown> => {
    const out: Record<string, unknown> = {};
    for (const f of question.fields) {
      const raw = values[f.key];
      if (f.type === 'multiselect') {
        const list = Array.isArray(raw) ? [...(raw as string[])] : [];
        const c = (customText[f.key] || '').trim();
        if (f.custom && c && !list.includes(c)) list.push(c);
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

  const handleReply = async (cancelled: boolean) => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await onReply(question.id, cancelled ? {} : buildAnswers(), cancelled);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <View style={[styles.card, { backgroundColor: theme.card }]}>
      <View style={styles.hd}>
        <Text style={[styles.title, { color: theme.text }]}>{question.title || t('questionTitle')}</Text>
        <View style={[styles.badge, { backgroundColor: theme.s1 }]}>
          <Text style={[styles.badgeTxt, { color: theme.busy }]}>{t('questionWaitingBadge')}</Text>
        </View>
      </View>

      {question.fields.map((f) => {
        const isMulti = f.type === 'multiselect';
        const hasOpts = Array.isArray(f.options) && f.options.length > 0;
        const selectedList = Array.isArray(values[f.key]) ? (values[f.key] as string[]) : [];
        const selectedSingle = typeof values[f.key] === 'string' ? (values[f.key] as string) : '';

        return (
          <View key={f.key} style={styles.field}>
            {(f.title || isMulti) && (
              <View style={styles.fieldHd}>
                {f.title ? <Text style={[styles.fieldTitle, { color: theme.text }]}>{f.title}</Text> : null}
                {isMulti ? <Text style={[styles.fieldHint, { color: theme.mute }]}>{t('questionMultiHint')}</Text> : null}
              </View>
            )}
            {f.description ? <Text style={[styles.fieldDesc, { color: theme.text2 }]}>{f.description}</Text> : null}

            {f.type === 'boolean' ? (
              <View style={styles.boolRow}>
                {[
                  { val: true, lbl: t('questionYes') },
                  { val: false, lbl: t('questionNo') },
                ].map((b) => {
                  const active = values[f.key] === b.val;
                  return (
                    <Pressable
                      key={String(b.val)}
                      disabled={submitting}
                      onPress={() => setValues((p) => ({ ...p, [f.key]: b.val }))}
                      style={[
                        styles.opt,
                        styles.boolOpt,
                        { backgroundColor: active ? accentSoft : theme.s1, borderColor: active ? accent : 'transparent' },
                      ]}
                    >
                      <Text style={[styles.optLbl, { color: theme.text }]}>{b.lbl}</Text>
                    </Pressable>
                  );
                })}
              </View>
            ) : hasOpts ? (
              <View style={styles.opts}>
                {f.options!.map((opt) => {
                  const active = isMulti
                    ? selectedList.includes(opt.value)
                    : !customActive[f.key] && selectedSingle === opt.value;
                  return (
                    <Pressable
                      key={opt.value}
                      disabled={submitting}
                      onPress={() => {
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
                      style={[
                        styles.opt,
                        { backgroundColor: active ? accentSoft : theme.s1, borderColor: active ? accent : 'transparent' },
                      ]}
                    >
                      <Text style={[styles.optLbl, { color: theme.text }]}>{opt.label || opt.value}</Text>
                      {opt.description ? (
                        <Text style={[styles.optDesc, { color: theme.text2 }]}>{opt.description}</Text>
                      ) : null}
                    </Pressable>
                  );
                })}
                {f.custom ? (
                  <TextInput
                    editable={!submitting}
                    placeholder={t('questionCustomPlaceholder')}
                    placeholderTextColor={theme.mute}
                    value={customText[f.key] || ''}
                    onFocus={() => {
                      if (!isMulti) setCustomActive((p) => ({ ...p, [f.key]: true }));
                    }}
                    onChangeText={(v) => {
                      setCustomText((p) => ({ ...p, [f.key]: v }));
                      if (!isMulti) setCustomActive((p) => ({ ...p, [f.key]: v.length > 0 }));
                    }}
                    style={[
                      styles.input,
                      {
                        backgroundColor: theme.s1,
                        color: theme.text,
                        borderColor: !isMulti && customActive[f.key] ? accent : 'transparent',
                      },
                    ]}
                  />
                ) : null}
              </View>
            ) : (
              <TextInput
                editable={!submitting}
                keyboardType={f.type === 'number' || f.type === 'integer' ? 'numeric' : 'default'}
                placeholder={f.placeholder || t('questionInputPlaceholder')}
                placeholderTextColor={theme.mute}
                value={String(values[f.key] ?? '')}
                onChangeText={(v) => setValues((p) => ({ ...p, [f.key]: v }))}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, borderColor: 'transparent' }]}
              />
            )}
          </View>
        );
      })}

      <View style={styles.acts}>
        <Pressable
          disabled={submitting}
          onPress={() => void handleReply(true)}
          style={[styles.btn, { backgroundColor: theme.s1, opacity: submitting ? 0.5 : 1 }]}
        >
          <Text style={[styles.btnTxt, { color: theme.text }]}>{t('questionCancel')}</Text>
        </Pressable>
        <Pressable
          disabled={submitting || !canSubmit}
          onPress={() => void handleReply(false)}
          style={[styles.btn, { backgroundColor: accent, opacity: submitting || !canSubmit ? 0.5 : 1 }]}
        >
          <Text style={[styles.btnTxt, { color: accentText }]}>{t('questionSubmit')}</Text>
        </Pressable>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    borderRadius: 20,
    padding: 16,
    marginTop: 8,
    gap: 12,
  },
  hd: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  title: {
    fontSize: 16,
    fontWeight: '700',
    flex: 1,
  },
  badge: {
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: 999,
  },
  badgeTxt: {
    fontSize: 12,
    fontWeight: '600',
  },
  field: {
    gap: 8,
  },
  fieldHd: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  fieldTitle: {
    fontSize: 15,
    fontWeight: '600',
  },
  fieldHint: {
    fontSize: 12,
  },
  fieldDesc: {
    fontSize: 13,
    lineHeight: 19,
  },
  opts: {
    gap: 8,
  },
  boolRow: {
    flexDirection: 'row',
    gap: 8,
  },
  boolOpt: {
    flex: 1,
    alignItems: 'center',
  },
  opt: {
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderRadius: 12,
    borderWidth: 1,
    gap: 2,
  },
  optLbl: {
    fontSize: 15,
    fontWeight: '600',
  },
  optDesc: {
    fontSize: 12,
    lineHeight: 17,
  },
  input: {
    height: 40,
    borderRadius: 12,
    borderWidth: 1,
    paddingHorizontal: 12,
    fontSize: 14,
  },
  acts: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 8,
    paddingTop: 4,
  },
  btn: {
    height: 36,
    paddingHorizontal: 16,
    borderRadius: 999,
    alignItems: 'center',
    justifyContent: 'center',
  },
  btnTxt: {
    fontSize: 14,
    fontWeight: '600',
  },
});
