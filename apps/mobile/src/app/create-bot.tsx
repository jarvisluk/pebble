import { router } from 'expo-router';
import React, { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { BotAppearancePicker } from '../components/BotAppearancePicker';
import { BotAvatar } from '../components/BotAvatar';
import { ModalHeader } from '../components/ModalHeader';
import { useI18n } from '../i18n';
import { resolveDefaultModel, useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';

function toSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

export default function CreateBotScreen() {
  const theme = useTheme();
  const { t } = useI18n();
  const { settings, loadSettings, createBot, models, loadModels } = useHub();

  const [name, setName] = useState('');
  const [botId, setBotId] = useState('');
  const [idTouched, setIdTouched] = useState(false);
  const [description, setDescription] = useState('');
  const [avatar, setAvatar] = useState('');
  const [color, setColor] = useState('');
  const [harness, setHarness] = useState('opencode');
  const [model, setModel] = useState('');
  const [loadingModels, setLoadingModels] = useState(false);
  const [instructions, setInstructions] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!settings) {
      loadSettings().catch(() => {});
    }
  }, [loadSettings, settings]);

  useEffect(() => {
    if ((models[harness] || []).length > 0) return;
    let alive = true;
    setLoadingModels(true);
    loadModels(harness)
      .catch(() => {})
      .finally(() => {
        if (alive) setLoadingModels(false);
      });
    return () => {
      alive = false;
    };
  }, [harness, loadModels, models]);

  const harnessOptions = useMemo(() => {
    if (settings?.harnesses?.length) return settings.harnesses;
    return [
      { id: 'opencode', label: 'opencode', defaultModel: '', magpie: { connected: false } },
      { id: 'claude', label: 'claude', defaultModel: '', magpie: { connected: false } },
      { id: 'pi', label: 'pi', defaultModel: '', magpie: { connected: false } },
    ];
  }, [settings]);

  const defaultModelPlaceholder = useMemo(() => {
    const resolved = resolveDefaultModel(settings?.defaultModel || '', harness);
    return resolved || t('inheritDefaultPh');
  }, [harness, settings?.defaultModel, t]);

  const closeSheet = () => {
    if (router.canGoBack()) {
      router.back();
    } else {
      router.replace('/');
    }
  };

  const onChangeName = (val: string) => {
    setName(val);
    if (!idTouched) {
      setBotId(toSlug(val));
    }
  };

  const onChangeId = (val: string) => {
    setIdTouched(true);
    setBotId(val);
  };

  const onCreate = async () => {
    setError('');
    const cleanName = name.trim();
    const cleanId = botId.trim();
    if (!cleanName) {
      setError(t('fillBotName'));
      return;
    }
    if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]*$/.test(cleanId)) {
      setError(t('idInvalid'));
      return;
    }

    setSubmitting(true);
    try {
      const created = await createBot({
        id: cleanId,
        name: cleanName,
        description: description.trim() || undefined,
        avatar: avatar || undefined,
        color: color || undefined,
        harness,
        model: model.trim() || undefined,
        instructions: instructions.trim() || undefined,
      });
      router.replace(`/bot/${encodeURIComponent(created.id)}`);
    } catch (err: any) {
      setError(err?.message || t('createFailed'));
      setSubmitting(false);
    }
  };

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader title={t('newBot')} onClose={closeSheet} />
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
          <View style={styles.avatarPreview}>
            <BotAvatar id={botId.trim() || name.trim() || 'new'} size={68} avatar={avatar} color={color} />
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>{t('botName')}</Text>
              <TextInput
                value={name}
                onChangeText={onChangeName}
                placeholder={t('botNameExamplePh')}
                placeholderTextColor={theme.mute}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>ID</Text>
              <TextInput
                value={botId}
                onChangeText={onChangeId}
                placeholder={t('botIdPh')}
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>{t('descOptional')}</Text>
              <TextInput
                value={description}
                onChangeText={setDescription}
                placeholder={t('botDescCreatePh')}
                placeholderTextColor={theme.mute}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text }]}
              />
            </View>

            <BotAppearancePicker
              botId={botId.trim() || name.trim() || 'new'}
              avatar={avatar}
              color={color}
              onAvatarChange={setAvatar}
              onColorChange={setColor}
            />

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>{t('harnessBackend')}</Text>
              <View style={styles.wrapRow}>
                {harnessOptions.map((h) => {
                  const active = harness === h.id;
                  return (
                    <Pressable
                      key={h.id}
                      onPress={() => setHarness(h.id)}
                      style={[
                        styles.pill,
                        { backgroundColor: active ? theme.accentSoft : theme.s1 },
                      ]}
                    >
                      <Text style={[styles.pillTxt, { color: active ? theme.accent : theme.text2 }]}>
                        {h.label || h.id}
                      </Text>
                    </Pressable>
                  );
                })}
              </View>
            </View>

            <View style={styles.field}>
              <View style={styles.rowBetween}>
                <Text style={[styles.lbl, { color: theme.text2 }]}>{t('modelOptional')}</Text>
                <Pressable
                  disabled={loadingModels}
                  onPress={() => {
                    setLoadingModels(true);
                    loadModels(harness)
                      .catch((err: any) => setError(err?.message || t('loadModelsFailed')))
                      .finally(() => setLoadingModels(false));
                  }}
                  style={[styles.smallBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                    {loadingModels ? t('loading') : t('optionalModels')}
                  </Text>
                </Pressable>
              </View>
              <TextInput
                value={model}
                onChangeText={setModel}
                placeholder={defaultModelPlaceholder}
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
              {(models[harness] || []).length > 0 && (
                <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
                  {(models[harness] || []).slice(0, 32).map((m) => (
                    <Pressable
                      key={m}
                      onPress={() => setModel(model.trim() === m ? '' : m)}
                      style={[
                        styles.pill,
                        { backgroundColor: model.trim() === m ? theme.accentSoft : theme.s1 },
                      ]}
                    >
                      <Text
                        style={[
                          styles.modelChipTxt,
                          { color: model.trim() === m ? theme.accent : theme.text2, fontFamily: MONO_FONT },
                        ]}
                      >
                        {m}
                      </Text>
                    </Pressable>
                  ))}
                </ScrollView>
              )}
            </View>

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>{t('agentsCreateLbl')}</Text>
              <TextInput
                value={instructions}
                onChangeText={setInstructions}
                placeholder={t('agentsCreatePh')}
                placeholderTextColor={theme.mute}
                multiline
                textAlignVertical="top"
                style={[styles.input, styles.textarea, { backgroundColor: theme.s1, color: theme.text }]}
              />
            </View>

            {error ? <Text style={[styles.errTxt, { color: theme.danger }]}>{error}</Text> : null}

            <Pressable
              disabled={submitting}
              onPress={onCreate}
              style={[styles.createBtn, { backgroundColor: theme.accent, opacity: submitting ? 0.7 : 1 }]}
            >
              {submitting ? (
                <ActivityIndicator size="small" color={theme.accentText} />
              ) : (
                <Text style={[styles.createTxt, { color: theme.accentText }]}>{t('create')}</Text>
              )}
            </Pressable>
          </View>
        </ScrollView>
      </KeyboardAvoidingView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
  scroll: {
    padding: 16,
    gap: 14,
    paddingBottom: 36,
  },
  avatarPreview: {
    alignItems: 'center',
    paddingTop: 4,
  },
  card: {
    borderRadius: 22,
    padding: 16,
    gap: 14,
  },
  field: {
    gap: 6,
  },
  lbl: {
    fontSize: 15,
    fontWeight: '600',
  },
  input: {
    borderRadius: 14,
    paddingHorizontal: 13,
    paddingVertical: 11,
    fontSize: 17,
  },
  textarea: {
    minHeight: 96,
    lineHeight: 25,
  },
  wrapRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
  },
  pill: {
    borderRadius: 999,
    paddingHorizontal: 14,
    paddingVertical: 8,
  },
  pillTxt: {
    fontSize: 15,
    fontWeight: '700',
  },
  rowBetween: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  smallBtn: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  smallBtnTxt: {
    fontSize: 13,
    fontWeight: '600',
  },
  pillRow: {
    gap: 8,
    paddingVertical: 2,
  },
  modelChipTxt: {
    fontSize: 13,
  },
  errTxt: {
    fontSize: 15,
    lineHeight: 20,
  },
  createBtn: {
    borderRadius: 999,
    paddingVertical: 13,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 2,
  },
  createTxt: {
    fontSize: 17,
    fontWeight: '700',
  },
});
