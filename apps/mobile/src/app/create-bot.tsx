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

import { BotAvatar } from '../components/BotAvatar';
import { ModalHeader } from '../components/ModalHeader';
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
  const { settings, loadSettings, createBot } = useHub();

  const [name, setName] = useState('');
  const [botId, setBotId] = useState('');
  const [idTouched, setIdTouched] = useState(false);
  const [description, setDescription] = useState('');
  const [harness, setHarness] = useState('opencode');
  const [model, setModel] = useState('');
  const [instructions, setInstructions] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!settings) {
      loadSettings().catch(() => {});
    }
  }, [loadSettings, settings]);

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
    return resolved || '留空继承默认模型';
  }, [harness, settings?.defaultModel]);

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
      setError('请填写 Bot 名称');
      return;
    }
    if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]*$/.test(cleanId)) {
      setError('ID 仅支持字母、数字、连字符和下划线，且需以字母或数字开头');
      return;
    }

    setSubmitting(true);
    try {
      const created = await createBot({
        id: cleanId,
        name: cleanName,
        description: description.trim() || undefined,
        harness,
        model: model.trim() || undefined,
        instructions: instructions.trim() || undefined,
      });
      router.replace(`/bot/${encodeURIComponent(created.id)}`);
    } catch (err: any) {
      setError(err?.message || '创建失败');
      setSubmitting(false);
    }
  };

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader title="新建 Bot" onClose={closeSheet} />
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
          <View style={styles.avatarPreview}>
            <BotAvatar id={botId.trim() || name.trim() || 'new'} size={68} />
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>名称</Text>
              <TextInput
                value={name}
                onChangeText={onChangeName}
                placeholder="代码助手"
                placeholderTextColor={theme.mute}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>ID</Text>
              <TextInput
                value={botId}
                onChangeText={onChangeId}
                placeholder="coder-1（英文、数字、-、_）"
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>简介（选填）</Text>
              <TextInput
                value={description}
                onChangeText={setDescription}
                placeholder="负责某个仓库或日常事务"
                placeholderTextColor={theme.mute}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>执行后端</Text>
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
              <Text style={[styles.lbl, { color: theme.text2 }]}>模型（选填）</Text>
              <TextInput
                value={model}
                onChangeText={setModel}
                placeholder={defaultModelPlaceholder}
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.lbl, { color: theme.text2 }]}>指令 AGENTS.md（选填）</Text>
              <TextInput
                value={instructions}
                onChangeText={setInstructions}
                placeholder="给这个 Bot 的长期角色设定与工作约束…"
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
                <Text style={[styles.createTxt, { color: theme.accentText }]}>创建</Text>
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
    fontSize: 13,
    fontWeight: '600',
  },
  input: {
    borderRadius: 14,
    paddingHorizontal: 13,
    paddingVertical: 11,
    fontSize: 16,
  },
  textarea: {
    minHeight: 96,
    lineHeight: 22,
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
    fontSize: 13.5,
    fontWeight: '700',
  },
  errTxt: {
    fontSize: 13,
    lineHeight: 18,
  },
  createBtn: {
    borderRadius: 999,
    paddingVertical: 13,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 2,
  },
  createTxt: {
    fontSize: 15,
    fontWeight: '700',
  },
});
