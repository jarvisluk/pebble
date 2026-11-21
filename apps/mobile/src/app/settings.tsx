import { router } from 'expo-router';
import React, { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';

import { BotAvatar } from '../components/BotAvatar';
import { ModalHeader } from '../components/ModalHeader';
import { confirmAction } from '../confirm';
import { resolveDefaultModel, useHub } from '../store';
import { MONO_FONT, useTheme, useThemePref } from '../theme';
import type { ThemePreference } from '../storage';

interface BotDraft {
  harness: string;
  model: string;
}

function cleanModelsMap(src: Record<string, string> | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(src || {})) {
    const trimmed = v.trim();
    if (trimmed) out[k] = trimmed;
  }
  return out;
}

function mapsEqual(a: Record<string, string>, b: Record<string, string>): boolean {
  const ka = Object.keys(a);
  const kb = Object.keys(b);
  if (ka.length !== kb.length) return false;
  return ka.every((k) => a[k] === b[k]);
}

export default function SettingsScreen() {
  const theme = useTheme();
  const { pref: themePref, setPref: setThemePref } = useThemePref();
  const {
    activeServer,
    bots,
    settings,
    models,
    loadSettings,
    loadModels,
    refreshBots,
    saveSettings,
    connectMagpie,
    disconnectMagpie,
    magpieProviderAction,
    setAllowLocalDesktop,
  } = useHub();

  const [defaultModel, setDefaultModel] = useState('');
  const [defaultModelTouched, setDefaultModelTouched] = useState(false);
  const [defaultModelsDraft, setDefaultModelsDraft] = useState<Record<string, string>>({});
  const [defaultModelsTouched, setDefaultModelsTouched] = useState(false);
  const [perHarnessExpanded, setPerHarnessExpanded] = useState(false);
  const [sharedUserMd, setSharedUserMd] = useState('');
  const [sharedUserMdTouched, setSharedUserMdTouched] = useState(false);
  const [botDrafts, setBotDrafts] = useState<Record<string, BotDraft>>({});
  const [loadingModelsFor, setLoadingModelsFor] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [busyAction, setBusyAction] = useState('');
  const [error, setError] = useState('');
  const [savedMsg, setSavedMsg] = useState('');

  const [showAddProv, setShowAddProv] = useState(false);
  const [provMode, setProvMode] = useState<'preset' | 'custom'>('preset');
  const [provPreset, setProvPreset] = useState('deepseek');
  const [provProtocol, setProvProtocol] = useState<'openai' | 'anthropic' | 'responses' | 'gemini'>('openai');
  const [provName, setProvName] = useState('');
  const [provUrl, setProvUrl] = useState('');
  const [provKey, setProvKey] = useState('');

  useEffect(() => {
    loadSettings().catch(() => {});
    refreshBots().catch(() => {});
  }, [loadSettings, refreshBots]);

  useEffect(() => {
    if (settings && !defaultModelTouched) {
      setDefaultModel(settings.defaultModel || '');
    }
    if (settings && !defaultModelsTouched) {
      const dm = cleanModelsMap(settings.defaultModels);
      setDefaultModelsDraft(dm);
      if (Object.keys(dm).length > 0) {
        setPerHarnessExpanded(true);
      }
    }
    if (settings && !sharedUserMdTouched) {
      setSharedUserMd(settings.sharedUserMd || '');
    }
    if (settings?.magpie?.presets?.length && !settings.magpie.presets.some((p) => p.id === provPreset)) {
      setProvPreset(settings.magpie.presets[0].id);
    }
  }, [defaultModelTouched, defaultModelsTouched, provPreset, settings, sharedUserMdTouched]);

  useEffect(() => {
    setBotDrafts((prev) => {
      let changed = false;
      const next = { ...prev };
      for (const b of bots) {
        if (!next[b.id]) {
          next[b.id] = {
            harness: b.harness,
            model: b.modelInherited ? '' : b.model || '',
          };
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, [bots]);

  const harnessOptions = useMemo(() => {
    if (settings?.harnesses?.length) return settings.harnesses;
    return [
      { id: 'opencode', label: 'opencode', defaultModel: '', magpie: { connected: false } },
      { id: 'claude', label: 'claude', defaultModel: '', magpie: { connected: false } },
      { id: 'pi', label: 'pi', defaultModel: '', magpie: { connected: false } },
    ];
  }, [settings]);

  const primaryHarness = harnessOptions[0]?.id || 'opencode';

  const botUpdates = useMemo(() => {
    const out: { id: string; harness: string; model: string }[] = [];
    for (const b of bots) {
      const draft = botDrafts[b.id];
      if (!draft) continue;
      const origModel = b.modelInherited ? '' : b.model || '';
      if (draft.harness !== b.harness || draft.model.trim() !== origModel) {
        out.push({ id: b.id, harness: draft.harness, model: draft.model.trim() });
      }
    }
    return out;
  }, [botDrafts, bots]);

  const cleanedDraftPerHarness = useMemo(() => cleanModelsMap(defaultModelsDraft), [defaultModelsDraft]);
  const cleanedBasePerHarness = useMemo(() => cleanModelsMap(settings?.defaultModels), [settings?.defaultModels]);

  const settingsChanged =
    defaultModel.trim() !== (settings?.defaultModel || '') ||
    !mapsEqual(cleanedDraftPerHarness, cleanedBasePerHarness) ||
    sharedUserMd.replace(/\s+$/, '') !== (settings?.sharedUserMd || '').replace(/\s+$/, '');
  const dirty = settingsChanged || botUpdates.length > 0;

  const closeSheet = () => {
    if (router.canGoBack()) {
      router.back();
    } else {
      router.replace('/');
    }
  };

  const onSave = async () => {
    setError('');
    setSavedMsg('');
    setSaving(true);
    try {
      await saveSettings({
        settingsChanged,
        defaultModel: defaultModel.trim(),
        defaultModels: cleanedDraftPerHarness,
        sharedUserMd,
        botUpdates,
      });
      setDefaultModelTouched(false);
      setDefaultModelsTouched(false);
      setSharedUserMdTouched(false);
      setBotDrafts({});
      setSavedMsg('已保存');
    } catch (err: any) {
      setError(err?.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const onConnectMagpie = (harnessId: string) => {
    const targetModel = defaultModel.trim() || settings?.harnesses.find((h) => h.id === harnessId)?.defaultModel || '';
    if (!targetModel) {
      setError(`请先在下方填写全局默认模型，再将 ${harnessId} 接入 Magpie`);
      return;
    }
    confirmAction(
      '使用 Magpie 接管',
      `确认运行 magpie ${harnessId} ${targetModel} 接管该 CLI？`,
      async () => {
        setError('');
        setBusyAction(`magpie:${harnessId}`);
        try {
          await connectMagpie(harnessId, targetModel);
          setSavedMsg(`${harnessId} 已接入 Magpie`);
        } catch (err: any) {
          setError(err?.message || '接入失败');
        } finally {
          setBusyAction('');
        }
      }
    );
  };

  const onDisconnectMagpie = (harnessId: string) => {
    confirmAction(
      '恢复原生 CLI 配置',
      `确认运行 magpie ${harnessId} default，撤回 Magpie 对 ${harnessId} 的接管？`,
      async () => {
        setError('');
        setBusyAction(`magpie:${harnessId}`);
        try {
          await disconnectMagpie(harnessId);
          setSavedMsg(`${harnessId} 已恢复原生配置`);
        } catch (err: any) {
          setError(err?.message || '恢复失败');
        } finally {
          setBusyAction('');
        }
      },
      true
    );
  };

  const onProviderAct = async (action: 'on' | 'off' | 'rm' | 'test', id: string) => {
    const run = async () => {
      setError('');
      setSavedMsg('');
      setBusyAction(`prov:${action}:${id}`);
      try {
        const res = await magpieProviderAction({ action, id });
        setSavedMsg(res.output || '操作成功');
      } catch (err: any) {
        setError(err?.message || '操作失败');
      } finally {
        setBusyAction('');
      }
    };
    if (action === 'rm') {
      confirmAction('移除模型源', `确认移除 ${id}？`, run, true);
    } else {
      await run();
    }
  };

  const onAddProvider = async () => {
    setError('');
    setBusyAction('prov:add');
    try {
      await magpieProviderAction({
        action: 'add',
        preset: provMode === 'preset' ? provPreset : undefined,
        name: provName.trim() || undefined,
        protocol: provMode === 'custom' ? provProtocol : undefined,
        url: provMode === 'custom' ? provUrl.trim() : undefined,
        key: provKey.trim() || undefined,
      });
      setProvKey('');
      setProvName('');
      setProvUrl('');
      setShowAddProv(false);
      setSavedMsg('已添加模型源');
    } catch (err: any) {
      setError(err?.message || '添加失败');
    } finally {
      setBusyAction('');
    }
  };

  const onLoadModels = async (hId: string) => {
    setError('');
    setLoadingModelsFor(hId);
    try {
      await loadModels(hId);
    } catch (err: any) {
      setError(err?.message || '加载模型列表失败');
    } finally {
      setLoadingModelsFor(null);
    }
  };

  const candidateModels = models[primaryHarness] || [];
  const mp = settings?.magpie;
  const providers = mp?.providers || [];
  const presets = mp?.presets || [
    { id: 'deepseek', name: 'DeepSeek' },
    { id: 'anthropic', name: 'Anthropic' },
    { id: 'openai', name: 'OpenAI' },
    { id: 'openrouter', name: 'OpenRouter' },
  ];

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader
        title="设置"
        onClose={closeSheet}
        rightLabel="保存"
        onRightPress={onSave}
        rightDisabled={!dirty || saving}
        rightLoading={saving}
      />
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
          {error ? (
            <View style={[styles.msgCard, { backgroundColor: theme.pink }]}>
              <Text style={[styles.msgTxt, { color: theme.danger }]}>{error}</Text>
            </View>
          ) : null}
          {savedMsg ? (
            <View style={[styles.msgCard, { backgroundColor: theme.sage }]}>
              <Text style={[styles.msgTxt, { color: theme.sageInk }]}>{savedMsg}</Text>
            </View>
          ) : null}

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.rowBetween}>
              <Text style={[styles.sectionTitle, { color: theme.text }]}>Magpie 原生接管</Text>
              <View
                style={[
                  styles.smallBtn,
                  {
                    backgroundColor: !mp?.installed
                      ? theme.s1
                      : mp.healthy
                        ? theme.sage
                        : theme.pink,
                  },
                ]}
              >
                <Text
                  style={[
                    styles.smallBtnTxt,
                    {
                      color: !mp?.installed
                        ? theme.mute
                        : mp.healthy
                          ? theme.sageInk
                          : theme.danger,
                    },
                  ]}
                >
                  {!mp?.installed ? '未安装' : mp.healthy ? '网关运行中' : '未运行'}
                </Text>
              </View>
            </View>

            {!mp?.installed ? (
              <Text style={[styles.noteTxt, { color: theme.text2 }]}>
                主电脑未安装 Magpie，各 CLI 当前使用自身独立配置。安装 Magpie 后可在此统一管理密钥与接入状态。
              </Text>
            ) : (
              <View style={styles.magpieList}>
                {harnessOptions.map((h) => (
                  <View key={h.id} style={[styles.magpieRow, { backgroundColor: theme.s1 }]}>
                    <View style={styles.magpieInfo}>
                      <Text style={[styles.magpieName, { color: theme.text }]}>{h.label || h.id}</Text>
                      <Text numberOfLines={1} style={[styles.magpieSub, { color: theme.mute, fontFamily: MONO_FONT }]}>
                        {h.magpie.connected
                          ? `Magpie · ${h.magpie.model || '已接入'}`
                          : `原生 CLI${h.magpie.nativeModel ? ` · ${h.magpie.nativeModel}` : ''}`}
                      </Text>
                    </View>
                    <View style={styles.magpieActs}>
                      <Pressable
                        disabled={busyAction === `magpie:${h.id}`}
                        onPress={() => onConnectMagpie(h.id)}
                        style={[styles.magpieBtn, { backgroundColor: h.magpie.connected ? theme.card : theme.accent }]}
                      >
                        {busyAction === `magpie:${h.id}` ? (
                          <ActivityIndicator size="small" color={h.magpie.connected ? theme.text : theme.accentText} />
                        ) : (
                          <Text style={[styles.magpieBtnTxt, { color: h.magpie.connected ? theme.text : theme.accentText }]}>
                            {h.magpie.connected ? '切换' : '接管'}
                          </Text>
                        )}
                      </Pressable>
                      {h.magpie.connected && (
                        <Pressable
                          disabled={busyAction === `magpie:${h.id}`}
                          onPress={() => onDisconnectMagpie(h.id)}
                          style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                        >
                          <Text style={[styles.magpieBtnTxt, { color: theme.danger }]}>恢复原生</Text>
                        </Pressable>
                      )}
                    </View>
                  </View>
                ))}
              </View>
            )}

            {mp?.installed && (
              <>
                <View style={styles.rowBetween}>
                  <Text style={[styles.subLabel, { color: theme.text2 }]}>
                    模型源（{providers.length}）
                  </Text>
                  <Pressable
                    onPress={() => setShowAddProv((v) => !v)}
                    style={[styles.smallBtn, { backgroundColor: theme.accentSoft }]}
                  >
                    <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>
                      {showAddProv ? '收起' : '+ 添加模型源'}
                    </Text>
                  </Pressable>
                </View>

                {showAddProv && (
                  <View style={[styles.addProvBox, { backgroundColor: theme.s1 }]}>
                    <View style={styles.segmentRow}>
                      {[
                        { key: 'preset' as const, label: '预设厂商' },
                        { key: 'custom' as const, label: '自定义接口' },
                      ].map((opt) => (
                        <Pressable
                          key={opt.key}
                          onPress={() => setProvMode(opt.key)}
                          style={[
                            styles.segmentPill,
                            { backgroundColor: provMode === opt.key ? theme.card : theme.s2 },
                          ]}
                        >
                          <Text
                            style={[
                              styles.segmentTxt,
                              { color: provMode === opt.key ? theme.accent : theme.text2 },
                            ]}
                          >
                            {opt.label}
                          </Text>
                        </Pressable>
                      ))}
                    </View>

                    {provMode === 'preset' ? (
                      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
                        {presets.map((p) => (
                          <Pressable
                            key={p.id}
                            onPress={() => setProvPreset(p.id)}
                            style={[
                              styles.pill,
                              { backgroundColor: provPreset === p.id ? theme.accentSoft : theme.card },
                            ]}
                          >
                            <Text style={[styles.pillTxt, { color: provPreset === p.id ? theme.accent : theme.text2 }]}>
                              {p.name}
                            </Text>
                          </Pressable>
                        ))}
                      </ScrollView>
                    ) : (
                      <>
                        <View style={styles.wrapRow}>
                          {(['openai', 'anthropic', 'responses', 'gemini'] as const).map((proto) => (
                            <Pressable
                              key={proto}
                              onPress={() => setProvProtocol(proto)}
                              style={[
                                styles.pill,
                                { backgroundColor: provProtocol === proto ? theme.accentSoft : theme.card },
                              ]}
                            >
                              <Text style={[styles.pillTxt, { color: provProtocol === proto ? theme.accent : theme.text2 }]}>
                                {proto}
                              </Text>
                            </Pressable>
                          ))}
                        </View>
                        <TextInput
                          value={provUrl}
                          onChangeText={setProvUrl}
                          placeholder="Base URL，例如 https://api.example.com/v1"
                          placeholderTextColor={theme.mute}
                          autoCapitalize="none"
                          autoCorrect={false}
                          style={[styles.input, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                        />
                      </>
                    )}

                    <TextInput
                      value={provName}
                      onChangeText={setProvName}
                      placeholder={provMode === 'custom' ? '名称（必填，如 my-relay）' : '别名（可选）'}
                      placeholderTextColor={theme.mute}
                      autoCapitalize="none"
                      autoCorrect={false}
                      style={[styles.input, { backgroundColor: theme.card, color: theme.text }]}
                    />
                    <TextInput
                      value={provKey}
                      onChangeText={setProvKey}
                      placeholder="API Key（sk-...）"
                      placeholderTextColor={theme.mute}
                      secureTextEntry
                      autoCapitalize="none"
                      autoCorrect={false}
                      style={[styles.input, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                    />
                    <Pressable
                      disabled={busyAction === 'prov:add'}
                      onPress={() => void onAddProvider()}
                      style={[styles.addBtn, { backgroundColor: theme.accent }]}
                    >
                      <Text style={[styles.magpieBtnTxt, { color: theme.accentText }]}>
                        {busyAction === 'prov:add' ? '添加中…' : '确认添加'}
                      </Text>
                    </Pressable>
                  </View>
                )}

                {providers.map((p) => (
                  <View key={p.id} style={[styles.magpieRow, { backgroundColor: theme.s1 }]}>
                    <View style={styles.magpieInfo}>
                      <Text style={[styles.magpieName, { color: theme.text }]}>
                        {p.name} · {p.enabled ? '启用' : '停用'}
                      </Text>
                      <Text numberOfLines={1} style={[styles.magpieSub, { color: theme.mute }]}>
                        {[p.endpoint, p.modelsInfo].filter(Boolean).join(' · ')}
                      </Text>
                    </View>
                    <View style={styles.magpieActs}>
                      <Pressable
                        onPress={() => void onProviderAct('test', p.id)}
                        style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                      >
                        <Text style={[styles.magpieBtnTxt, { color: theme.text2 }]}>测速</Text>
                      </Pressable>
                      <Pressable
                        onPress={() => void onProviderAct(p.enabled ? 'off' : 'on', p.id)}
                        style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                      >
                        <Text style={[styles.magpieBtnTxt, { color: theme.text2 }]}>
                          {p.enabled ? '停用' : '启用'}
                        </Text>
                      </Pressable>
                      <Pressable
                        onPress={() => void onProviderAct('rm', p.id)}
                        style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                      >
                        <Text style={[styles.magpieBtnTxt, { color: theme.danger }]}>移除</Text>
                      </Pressable>
                    </View>
                  </View>
                ))}
              </>
            )}
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.sectionTitle, { color: theme.text }]}>外观</Text>
            <View style={styles.segmentRow}>
              {(
                [
                  { key: 'system', label: '跟随系统' },
                  { key: 'light', label: '浅色' },
                  { key: 'dark', label: '深色' },
                ] as { key: ThemePreference; label: string }[]
              ).map((opt) => (
                <Pressable
                  key={opt.key}
                  onPress={() => setThemePref(opt.key)}
                  style={[
                    styles.segmentPill,
                    { backgroundColor: themePref === opt.key ? theme.accentSoft : theme.s1 },
                  ]}
                >
                  <Text
                    style={[
                      styles.segmentTxt,
                      { color: themePref === opt.key ? theme.accent : theme.text2 },
                    ]}
                  >
                    {opt.label}
                  </Text>
                </Pressable>
              ))}
            </View>
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.rowBetween}>
              <Text style={[styles.sectionTitle, { color: theme.text }]}>全局默认模型</Text>
              <View style={styles.magpieActs}>
                <Pressable
                  onPress={() => setPerHarnessExpanded((v) => !v)}
                  style={[styles.smallBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>
                    {perHarnessExpanded ? '收起' : '按后端设置'}
                  </Text>
                </Pressable>
                <Pressable
                  disabled={loadingModelsFor === primaryHarness}
                  onPress={() => void onLoadModels(primaryHarness)}
                  style={[styles.smallBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                    {loadingModelsFor === primaryHarness ? '加载中…' : '可选列表'}
                  </Text>
                </Pressable>
              </View>
            </View>
            <TextInput
              value={defaultModel}
              onChangeText={(val) => {
                setDefaultModelTouched(true);
                setSavedMsg('');
                setDefaultModel(val);
              }}
              placeholder="例如 magpie/claude-sonnet-4"
              placeholderTextColor={theme.mute}
              autoCapitalize="none"
              autoCorrect={false}
              style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
            />
            {perHarnessExpanded && (
              <View style={[styles.addProvBox, { backgroundColor: theme.s1 }]}>
                <Text style={[styles.subLabel, { color: theme.text2 }]}>
                  按 Harness 覆盖全局默认模型
                </Text>
                {harnessOptions.map((h) => (
                  <View key={h.id} style={styles.perHarnessItem}>
                    <Text style={[styles.magpieSub, { color: theme.mute }]}>{h.label || h.id}</Text>
                    <TextInput
                      value={defaultModelsDraft[h.id] || ''}
                      onChangeText={(val) => {
                        setDefaultModelsTouched(true);
                        setSavedMsg('');
                        setDefaultModelsDraft((prev) => ({ ...prev, [h.id]: val }));
                      }}
                      placeholder="留空跟随全局默认"
                      placeholderTextColor={theme.mute}
                      autoCapitalize="none"
                      autoCorrect={false}
                      style={[styles.input, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                    />
                  </View>
                ))}
              </View>
            )}
            {candidateModels.length > 0 && (
              <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
                {candidateModels.slice(0, 12).map((m) => (
                  <Pressable
                    key={m}
                    onPress={() => {
                      setDefaultModelTouched(true);
                      setSavedMsg('');
                      setDefaultModel(m);
                    }}
                    style={[
                      styles.pill,
                      { backgroundColor: defaultModel.trim() === m ? theme.accentSoft : theme.s1 },
                    ]}
                  >
                    <Text
                      style={[
                        styles.modelChipTxt,
                        { color: defaultModel.trim() === m ? theme.accent : theme.text2, fontFamily: MONO_FONT },
                      ]}
                    >
                      {m}
                    </Text>
                  </Pressable>
                ))}
              </ScrollView>
            )}
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.sectionTitle, { color: theme.text }]}>各 Bot 后端与模型覆盖</Text>
            {bots.length === 0 ? (
              <Text style={[styles.emptyNote, { color: theme.mute }]}>当前没有 Bot</Text>
            ) : (
              <View style={styles.botList}>
                {bots.map((b, index) => {
                  const draft = botDrafts[b.id] || {
                    harness: b.harness,
                    model: b.modelInherited ? '' : b.model || '',
                  };
                  const ph =
                    defaultModelsDraft[draft.harness]?.trim() ||
                    resolveDefaultModel(defaultModel.trim(), draft.harness) ||
                    '继承默认模型';
                  return (
                    <View key={b.id} style={[styles.botItem, { backgroundColor: theme.s1 }]}>
                      <View style={styles.botHead}>
                        <BotAvatar id={b.id} size={36} index={index} ringColor={theme.s1} />
                        <View style={styles.botHeadTxt}>
                          <Text style={[styles.botName, { color: theme.text }]}>{b.name}</Text>
                          <Text style={[styles.botId, { color: theme.mute, fontFamily: MONO_FONT }]}>
                            @{b.id}
                          </Text>
                        </View>
                      </View>

                      <View style={styles.wrapRow}>
                        {harnessOptions.map((h) => {
                          const active = draft.harness === h.id;
                          return (
                            <Pressable
                              key={h.id}
                              onPress={() => {
                                setSavedMsg('');
                                setBotDrafts((prev) => ({
                                  ...prev,
                                  [b.id]: { ...draft, harness: h.id },
                                }));
                              }}
                              style={[
                                styles.pill,
                                { backgroundColor: active ? theme.accentSoft : theme.card },
                              ]}
                            >
                              <Text style={[styles.pillTxt, { color: active ? theme.accent : theme.text2 }]}>
                                {h.label || h.id}
                              </Text>
                            </Pressable>
                          );
                        })}
                      </View>

                      <TextInput
                        value={draft.model}
                        onChangeText={(val) => {
                          setSavedMsg('');
                          setBotDrafts((prev) => ({
                            ...prev,
                            [b.id]: { ...draft, model: val },
                          }));
                        }}
                        placeholder={ph}
                        placeholderTextColor={theme.mute}
                        autoCapitalize="none"
                        autoCorrect={false}
                        style={[styles.input, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                      />
                    </View>
                  );
                })}
              </View>
            )}
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.rowBetween}>
              <Text style={[styles.sectionTitle, { color: theme.text }]}>全局共享记忆</Text>
              <Text style={[styles.magpieSub, { color: theme.mute, fontFamily: MONO_FONT }]}>_shared/USER.md</Text>
            </View>
            <TextInput
              value={sharedUserMd}
              onChangeText={(val) => {
                setSharedUserMdTouched(true);
                setSavedMsg('');
                setSharedUserMd(val);
              }}
              placeholder="记录你的常用偏好、身份背景或全局约定（所有 Bot 共读）…"
              placeholderTextColor={theme.mute}
              multiline
              textAlignVertical="top"
              style={[styles.input, styles.textArea, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
            />
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.sectionTitle, { color: theme.text }]}>远程与设备</Text>
            <View style={styles.rowBetween}>
              <Text style={[styles.noteTxt, { color: theme.text }]}>允许 Bot 操控本机桌面</Text>
              <Switch
                value={settings?.allowLocalDesktop !== false}
                onValueChange={(v) => {
                  void setAllowLocalDesktop(v).catch((err: any) => setError(err?.message || '更新失败'));
                }}
                trackColor={{ false: theme.s2, true: theme.accent }}
              />
            </View>
            <View style={styles.rowBetween}>
              <View style={styles.connCol}>
                <Text style={[styles.connLbl, { color: theme.text }]}>
                  {activeServer?.label || '未选择主电脑'}
                </Text>
                <Text numberOfLines={1} style={[styles.connAddr, { color: theme.mute, fontFamily: MONO_FONT }]}>
                  {activeServer?.address || '-'}
                </Text>
              </View>
              <View style={styles.magpieActs}>
                <Pressable
                  onPress={() => router.push('/devices')}
                  style={[styles.switchBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.switchBtnTxt, { color: theme.text }]}>配对与节点</Text>
                </Pressable>
                <Pressable
                  onPress={() => router.push('/connect')}
                  style={[styles.switchBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.switchBtnTxt, { color: theme.text }]}>切换主机</Text>
                </Pressable>
              </View>
            </View>
          </View>
        </ScrollView>
      </KeyboardAvoidingView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
  scroll: { padding: 16, gap: 12, paddingBottom: 36 },
  msgCard: { borderRadius: 16, paddingHorizontal: 14, paddingVertical: 10 },
  msgTxt: { fontSize: 13.5, fontWeight: '600' },
  card: { borderRadius: 22, padding: 16, gap: 12 },
  sectionTitle: { fontSize: 16, fontWeight: '700' },
  subLabel: { fontSize: 13.5, fontWeight: '600' },
  noteTxt: { fontSize: 13.5, lineHeight: 19, flex: 1 },
  rowBetween: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 10 },
  segmentRow: { flexDirection: 'row', gap: 8 },
  segmentPill: { flex: 1, borderRadius: 999, paddingVertical: 8, alignItems: 'center', justifyContent: 'center' },
  segmentTxt: { fontSize: 13, fontWeight: '700' },
  magpieList: { gap: 8 },
  magpieRow: {
    borderRadius: 14,
    paddingHorizontal: 12,
    paddingVertical: 10,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  magpieInfo: { flex: 1, minWidth: 0, gap: 2 },
  magpieName: { fontSize: 14, fontWeight: '600' },
  magpieSub: { fontSize: 11.5 },
  magpieActs: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  magpieBtn: { borderRadius: 999, paddingHorizontal: 11, paddingVertical: 6 },
  magpieBtnTxt: { fontSize: 12.5, fontWeight: '700' },
  addProvBox: { borderRadius: 16, padding: 12, gap: 10 },
  addBtn: { borderRadius: 999, paddingVertical: 10, alignItems: 'center', justifyContent: 'center' },
  smallBtn: { borderRadius: 999, paddingHorizontal: 11, paddingVertical: 5 },
  smallBtnTxt: { fontSize: 12.5, fontWeight: '600' },
  input: { borderRadius: 14, paddingHorizontal: 13, paddingVertical: 10, fontSize: 15 },
  textArea: { minHeight: 110, fontSize: 13, lineHeight: 19 },
  perHarnessItem: { gap: 4 },
  pillRow: { gap: 8 },
  wrapRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  pill: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 7 },
  pillTxt: { fontSize: 13, fontWeight: '700' },
  modelChipTxt: { fontSize: 12.5, fontWeight: '600' },
  emptyNote: { fontSize: 13.5 },
  botList: { gap: 10 },
  botItem: { borderRadius: 18, padding: 13, gap: 10 },
  botHead: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  botHeadTxt: { flex: 1 },
  botName: { fontSize: 15, fontWeight: '700' },
  botId: { fontSize: 12 },
  connCol: { flex: 1, minWidth: 0, gap: 2 },
  connLbl: { fontSize: 14.5, fontWeight: '600' },
  connAddr: { fontSize: 12 },
  switchBtn: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 8 },
  switchBtnTxt: { fontSize: 13, fontWeight: '600' },
});
