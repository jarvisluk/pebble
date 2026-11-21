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
import { confirmAction } from '../confirm';
import { useI18n } from '../i18n';
import { resolveDefaultModel, useHub } from '../store';
import { ACCENT_PRESETS, DEFAULT_ACCENT, MONO_FONT, useTheme, useThemePref } from '../theme';
import type { LanguagePreference, ThemePreference } from '../storage';
import type { MagpieMemoryList } from '../types';

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
  const { pref: themePref, setPref: setThemePref, accent, setAccent } = useThemePref();
  const { pref: langPref, setPref: setLangPref, t } = useI18n();
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
    setDesktopWorker,
    fetchMemories,
    saveMemory,
    deleteMemory,
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

  const [memData, setMemData] = useState<MagpieMemoryList | null>(null);
  const [memLoading, setMemLoading] = useState(false);
  const [memQuery, setMemQuery] = useState('');
  const [memShowAdd, setMemShowAdd] = useState(false);
  const [memKind, setMemKind] = useState<'fact' | 'preference' | 'rule' | 'reference'>('fact');
  const [memText, setMemText] = useState('');
  const [memTags, setMemTags] = useState('');

  const [deskHarness, setDeskHarness] = useState('');
  const [deskModel, setDeskModel] = useState('');
  const [deskTouched, setDeskTouched] = useState(false);

  const loadGlobalMemories = async (q = memQuery) => {
    setMemLoading(true);
    try {
      const res = await fetchMemories({ scope: 'global', query: q, limit: 30 });
      setMemData(res);
    } catch (err: any) {
      setError(err?.message || t('opFailed'));
    } finally {
      setMemLoading(false);
    }
  };

  useEffect(() => {
    loadSettings().catch(() => {});
    refreshBots().catch(() => {});
    void loadGlobalMemories('');
  }, [loadSettings, refreshBots]);

  useEffect(() => {
    if (settings && !defaultModelTouched) {
      setDefaultModel(settings.defaultModel || '');
    }
    if (settings && !deskTouched) {
      setDeskHarness(settings.desktopHarness || '');
      setDeskModel(settings.desktopModel || '');
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
      setSavedMsg(t('settingsSaved'));
    } catch (err: any) {
      setError(err?.message || t('saveFailed'));
    } finally {
      setSaving(false);
    }
  };

  const onConnectMagpie = (harnessId: string) => {
    const targetModel = defaultModel.trim() || settings?.harnesses.find((h) => h.id === harnessId)?.defaultModel || '';
    if (!targetModel) {
      setError(t('pickMagpieModel'));
      return;
    }
    confirmAction(
      t('mpConnect'),
      `magpie ${harnessId} ${targetModel}`,
      async () => {
        setError('');
        setBusyAction(`magpie:${harnessId}`);
        try {
          await connectMagpie(harnessId, targetModel);
          setSavedMsg(t('connectedMagpie', { label: harnessId }));
        } catch (err: any) {
          setError(err?.message || t('connectFailed'));
        } finally {
          setBusyAction('');
        }
      }
    );
  };

  const onDisconnectMagpie = (harnessId: string) => {
    confirmAction(
      t('restoreNativeTitle'),
      t('restoreNativeDesc', { label: harnessId }),
      async () => {
        setError('');
        setBusyAction(`magpie:${harnessId}`);
        try {
          await disconnectMagpie(harnessId);
          setSavedMsg(t('restoredNative', { label: harnessId }));
        } catch (err: any) {
          setError(err?.message || t('restoreFailed'));
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
        setSavedMsg(res.output || t('opSuccess'));
      } catch (err: any) {
        setError(err?.message || t('opFailed'));
      } finally {
        setBusyAction('');
      }
    };
    if (action === 'rm') {
      confirmAction(t('rmProviderTitle'), t('rmProviderDesc', { id }), run, true);
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
      setSavedMsg(t('providerAdded'));
    } catch (err: any) {
      setError(err?.message || t('addFailed'));
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
      setError(err?.message || t('loadModelsFailed'));
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
        title={t('settings')}
        onClose={closeSheet}
        rightLabel={t('save')}
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
              <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('magpieTakeover')}</Text>
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
                  {!mp?.installed ? t('notInstalled') : mp.healthy ? t('gatewayOk') : t('gatewayDown')}
                </Text>
              </View>
            </View>

            {!mp?.installed ? (
              <Text style={[styles.noteTxt, { color: theme.text2 }]}>
                {t('hostNoMagpieGlobal')}
              </Text>
            ) : (
              <View style={styles.magpieList}>
                {harnessOptions.map((h) => (
                  <View key={h.id} style={[styles.magpieRow, { backgroundColor: theme.s1 }]}>
                    <View style={styles.magpieInfo}>
                      <Text style={[styles.magpieName, { color: theme.text }]}>{h.label || h.id}</Text>
                      <Text numberOfLines={1} style={[styles.magpieSub, { color: theme.mute, fontFamily: MONO_FONT }]}>
                        {h.magpie.connected
                          ? `Magpie · ${h.magpie.model || t('mpConnected')}`
                          : `${t('mpNative')}${h.magpie.nativeModel ? ` · ${h.magpie.nativeModel}` : ''}`}
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
                            {h.magpie.connected ? t('mpSwitch') : t('mpConnect')}
                          </Text>
                        )}
                      </Pressable>
                      {h.magpie.connected && (
                        <Pressable
                          disabled={busyAction === `magpie:${h.id}`}
                          onPress={() => onDisconnectMagpie(h.id)}
                          style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                        >
                          <Text style={[styles.magpieBtnTxt, { color: theme.danger }]}>{t('mpRestore')}</Text>
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
                    {t('providersTitle', { n: providers.length })}
                  </Text>
                  <Pressable
                    onPress={() => setShowAddProv((v) => !v)}
                    style={[styles.smallBtn, { backgroundColor: theme.accentSoft }]}
                  >
                    <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>
                      {showAddProv ? t('collapse') : t('addProvider')}
                    </Text>
                  </Pressable>
                </View>

                {showAddProv && (
                  <View style={[styles.addProvBox, { backgroundColor: theme.s1 }]}>
                    <View style={styles.segmentRow}>
                      {[
                        { key: 'preset' as const, label: t('presetTab') },
                        { key: 'custom' as const, label: t('customTab') },
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
                          placeholder={t('baseUrlPh')}
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
                      placeholder={t('aliasOptionalPh')}
                      placeholderTextColor={theme.mute}
                      autoCapitalize="none"
                      autoCorrect={false}
                      style={[styles.input, { backgroundColor: theme.card, color: theme.text }]}
                    />
                    <TextInput
                      value={provKey}
                      onChangeText={setProvKey}
                      placeholder={t('apiKeyPh')}
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
                        {busyAction === 'prov:add' ? t('adding') : t('confirmAdd')}
                      </Text>
                    </Pressable>
                  </View>
                )}

                {providers.map((p) => (
                  <View key={p.id} style={[styles.magpieRow, { backgroundColor: theme.s1 }]}>
                    <View style={styles.magpieInfo}>
                      <Text style={[styles.magpieName, { color: theme.text }]}>
                        {p.name} · {p.enabled ? t('enabled') : t('paused')}
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
                        <Text style={[styles.magpieBtnTxt, { color: theme.text2 }]}>{t('testBtn')}</Text>
                      </Pressable>
                      <Pressable
                        onPress={() => void onProviderAct(p.enabled ? 'off' : 'on', p.id)}
                        style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                      >
                        <Text style={[styles.magpieBtnTxt, { color: theme.text2 }]}>
                          {p.enabled ? t('paused') : t('enabled')}
                        </Text>
                      </Pressable>
                      <Pressable
                        onPress={() => void onProviderAct('rm', p.id)}
                        style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                      >
                        <Text style={[styles.magpieBtnTxt, { color: theme.danger }]}>{t('removeBtn')}</Text>
                      </Pressable>
                    </View>
                  </View>
                ))}
              </>
            )}
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('appearance')}</Text>
            <View style={styles.segmentRow}>
              {(
                [
                  { key: 'system', label: t('followSystem') },
                  { key: 'light', label: t('themeLight') },
                  { key: 'dark', label: t('themeDark') },
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

            <View style={styles.rowBetween}>
              <Text style={[styles.subLabel, { color: theme.text2 }]}>{t('appAccent')}</Text>
              <Pressable onPress={() => setAccent(DEFAULT_ACCENT)}>
                <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>{t('resetDefault')}</Text>
              </Pressable>
            </View>
            <Text style={[styles.magpieSub, { color: theme.mute }]}>{t('appAccentDesc')}</Text>
            <View style={styles.swatchRow}>
              {ACCENT_PRESETS.map((hex) => {
                const selected = accent === hex;
                return (
                  <Pressable
                    key={hex}
                    onPress={() => setAccent(hex)}
                    style={[
                      styles.swatch,
                      { backgroundColor: hex },
                      selected && { borderWidth: 3, borderColor: theme.text },
                    ]}
                  />
                );
              })}
            </View>

            <Text style={[styles.subLabel, { color: theme.text2 }]}>{t('language')}</Text>
            <View style={styles.segmentRow}>
              {(
                [
                  { key: 'system', label: t('followSystem') },
                  { key: 'zh-CN', label: '简体中文' },
                  { key: 'en', label: 'English' },
                ] as { key: LanguagePreference; label: string }[]
              ).map((opt) => (
                <Pressable
                  key={opt.key}
                  onPress={() => setLangPref(opt.key)}
                  style={[
                    styles.segmentPill,
                    { backgroundColor: langPref === opt.key ? theme.accentSoft : theme.s1 },
                  ]}
                >
                  <Text
                    style={[
                      styles.segmentTxt,
                      { color: langPref === opt.key ? theme.accent : theme.text2 },
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
              <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('globalDefaultModel')}</Text>
              <View style={styles.magpieActs}>
                <Pressable
                  onPress={() => setPerHarnessExpanded((v) => !v)}
                  style={[styles.smallBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>
                    {perHarnessExpanded ? t('collapse') : t('perHarness')}
                  </Text>
                </Pressable>
                <Pressable
                  disabled={loadingModelsFor === primaryHarness}
                  onPress={() => void onLoadModels(primaryHarness)}
                  style={[styles.smallBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                    {loadingModelsFor === primaryHarness ? t('loading') : t('optionalModels')}
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
              placeholder="magpie/claude-sonnet-4"
              placeholderTextColor={theme.mute}
              autoCapitalize="none"
              autoCorrect={false}
              style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
            />
            {perHarnessExpanded && (
              <View style={[styles.addProvBox, { backgroundColor: theme.s1 }]}>
                <Text style={[styles.subLabel, { color: theme.text2 }]}>
                  {t('perHarnessTitle')}
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
                      placeholder={t('followGlobalPh')}
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
            <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('botModelsSection')}</Text>
            {bots.length === 0 ? (
              <Text style={[styles.emptyNote, { color: theme.mute }]}>{t('noBotsInSettings')}</Text>
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
                    t('inheritDefaultPh');
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
              <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('globalSharedMem')}</Text>
              <Text style={[styles.magpieSub, { color: theme.mute, fontFamily: MONO_FONT }]}>_shared/USER.md</Text>
            </View>
            <TextInput
              value={sharedUserMd}
              onChangeText={(val) => {
                setSharedUserMdTouched(true);
                setSavedMsg('');
                setSharedUserMd(val);
              }}
              placeholder={t('userMdPh')}
              placeholderTextColor={theme.mute}
              multiline
              textAlignVertical="top"
              style={[styles.input, styles.textArea, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
            />
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.rowBetween}>
              <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('magpieGlobalMem')}</Text>
              <View style={styles.magpieActs}>
                <Pressable
                  onPress={() => void loadGlobalMemories(memQuery)}
                  style={[styles.smallBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                    {memLoading ? t('refreshing') : t('refresh')}
                  </Text>
                </Pressable>
                <Pressable
                  onPress={() => setMemShowAdd((v) => !v)}
                  style={[styles.smallBtn, { backgroundColor: theme.accentSoft }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>
                    {memShowAdd ? t('collapse') : t('addShort')}
                  </Text>
                </Pressable>
              </View>
            </View>

            {memShowAdd && (
              <View style={[styles.addProvBox, { backgroundColor: theme.s1 }]}>
                <View style={styles.wrapRow}>
                  {(['fact', 'preference', 'rule', 'reference'] as const).map((k) => (
                    <Pressable
                      key={k}
                      onPress={() => setMemKind(k)}
                      style={[
                        styles.pill,
                        { backgroundColor: memKind === k ? theme.accentSoft : theme.card },
                      ]}
                    >
                      <Text style={[styles.pillTxt, { color: memKind === k ? theme.accent : theme.text2 }]}>
                        {k}
                      </Text>
                    </Pressable>
                  ))}
                </View>
                <TextInput
                  value={memTags}
                  onChangeText={setMemTags}
                  placeholder={t('tagsPh')}
                  placeholderTextColor={theme.mute}
                  autoCapitalize="none"
                  style={[styles.input, { backgroundColor: theme.card, color: theme.text }]}
                />
                <TextInput
                  value={memText}
                  onChangeText={setMemText}
                  placeholder={t('globalMemPh')}
                  placeholderTextColor={theme.mute}
                  multiline
                  textAlignVertical="top"
                  style={[styles.input, styles.textArea, { minHeight: 80, backgroundColor: theme.card, color: theme.text }]}
                />
                <Pressable
                  disabled={busyAction === 'mem:save'}
                  onPress={async () => {
                    const txt = memText.trim();
                    if (!txt) {
                      setError(t('memContentReq'));
                      return;
                    }
                    setError('');
                    setBusyAction('mem:save');
                    try {
                      const tags = memTags
                        .split(/[,，]/)
                        .map((s) => s.trim())
                        .filter(Boolean);
                      await saveMemory({ scope: 'global', kind: memKind, text: txt, tags });
                      setMemText('');
                      setMemTags('');
                      setMemShowAdd(false);
                      setSavedMsg(t('globalMemSaved'));
                      await loadGlobalMemories(memQuery);
                    } catch (err: any) {
                      setError(err?.message || t('saveMemFailed'));
                    } finally {
                      setBusyAction('');
                    }
                  }}
                  style={[styles.addBtn, { backgroundColor: theme.accent }]}
                >
                  <Text style={[styles.magpieBtnTxt, { color: theme.accentText }]}>
                    {busyAction === 'mem:save' ? t('saving') : t('saveMemory')}
                  </Text>
                </Pressable>
              </View>
            )}

            <View style={styles.rowBetween}>
              <TextInput
                value={memQuery}
                onChangeText={setMemQuery}
                onSubmitEditing={() => void loadGlobalMemories(memQuery)}
                placeholder={t('searchGlobalMemPh')}
                placeholderTextColor={theme.mute}
                returnKeyType="search"
                style={[styles.input, { flex: 1, backgroundColor: theme.s1, color: theme.text }]}
              />
              <Pressable
                onPress={() => void loadGlobalMemories(memQuery)}
                style={[styles.magpieBtn, { backgroundColor: theme.s1 }]}
              >
                <Text style={[styles.magpieBtnTxt, { color: theme.text2 }]}>{t('search')}</Text>
              </Pressable>
            </View>

            {!memData?.installed ? (
              <Text style={[styles.emptyNote, { color: theme.mute }]}>{t('hostNoMagpieGlobal')}</Text>
            ) : !memData.available ? (
              <Text style={[styles.emptyNote, { color: theme.danger }]}>
                {t('magpieNoResp')}{memData.error ? `: ${memData.error}` : ''}
              </Text>
            ) : (memData.items || []).length === 0 ? (
              <Text style={[styles.emptyNote, { color: theme.mute }]}>{t('noGlobalMem')}</Text>
            ) : (
              <View style={styles.magpieList}>
                {memData.items.map((it) => (
                  <View key={it.id} style={[styles.addProvBox, { backgroundColor: theme.s1, gap: 6 }]}>
                    <View style={styles.rowBetween}>
                      <View style={[styles.wrapRow, { flex: 1 }]}>
                        {it.kind ? (
                          <View style={[styles.smallBtn, { backgroundColor: theme.sage }]}>
                            <Text style={[styles.smallBtnTxt, { color: theme.sageInk }]}>{it.kind}</Text>
                          </View>
                        ) : null}
                        {(it.tags || [])
                          .filter((tag) => !tag.startsWith('bot:'))
                          .map((tag) => (
                            <View key={tag} style={[styles.smallBtn, { backgroundColor: theme.card }]}>
                              <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>#{tag}</Text>
                            </View>
                          ))}
                      </View>
                      <Pressable
                        onPress={() => {
                          confirmAction(
                            t('deleteMemTitle'),
                            t('deleteMemConfirm'),
                            async () => {
                              try {
                                await deleteMemory(it.id);
                                await loadGlobalMemories(memQuery);
                              } catch (err: any) {
                                setError(err?.message || t('delFailed'));
                              }
                            },
                            true
                          );
                        }}
                        style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                      >
                        <Text style={[styles.magpieBtnTxt, { color: theme.danger }]}>{t('delete')}</Text>
                      </Pressable>
                    </View>
                    <Text selectable style={[styles.noteTxt, { color: theme.text }]}>
                      {it.text}
                    </Text>
                  </View>
                ))}
              </View>
            )}
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('remoteDevicesTitle')}</Text>
            <View style={{ gap: 6, backgroundColor: theme.s1, borderRadius: 14, padding: 10 }}>
              <Text style={[styles.subLabel, { color: theme.text2 }]}>桌面子进程 Harness（留空跟随当前 Bot）</Text>
              <View style={{ flexDirection: 'row', gap: 6, flexWrap: 'wrap' }}>
                {[{ id: '', label: '跟随 Bot' }, ...(settings?.harnesses || [])].map((h) => (
                  <Pressable
                    key={h.id || 'auto'}
                    onPress={() => {
                      setDeskHarness(h.id);
                      setDeskTouched(true);
                    }}
                    style={[
                      styles.switchBtn,
                      { backgroundColor: deskHarness === h.id ? theme.accent : theme.card },
                    ]}
                  >
                    <Text
                      style={[
                        styles.switchBtnTxt,
                        { color: deskHarness === h.id ? theme.accentText : theme.text2 },
                      ]}
                    >
                      {h.label}
                    </Text>
                  </Pressable>
                ))}
              </View>
              <Text style={[styles.subLabel, { color: theme.text2, marginTop: 4 }]}>桌面子进程模型（建议轻量视觉模型）</Text>
              <View style={{ flexDirection: 'row', gap: 8, alignItems: 'center' }}>
                <TextInput
                  value={deskModel}
                  onChangeText={(v) => {
                    setDeskModel(v);
                    setDeskTouched(true);
                  }}
                  placeholder="留空沿用 Bot 默认"
                  placeholderTextColor={theme.mute}
                  autoCapitalize="none"
                  style={[
                    styles.input,
                    { flex: 1, backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT },
                  ]}
                />
                <Pressable
                  disabled={busyAction === 'desk'}
                  onPress={async () => {
                    setBusyAction('desk');
                    setError('');
                    try {
                      await setDesktopWorker(deskHarness, deskModel);
                      setDeskTouched(false);
                      setSavedMsg('已保存桌面子进程配置');
                    } catch (err: any) {
                      setError(err?.message || '保存失败');
                    } finally {
                      setBusyAction('');
                    }
                  }}
                  style={[styles.switchBtn, { backgroundColor: theme.accent }]}
                >
                  <Text style={[styles.switchBtnTxt, { color: theme.accentText }]}>
                    {busyAction === 'desk' ? '保存中' : '保存'}
                  </Text>
                </Pressable>
              </View>
            </View>
            <View style={styles.rowBetween}>
              <View style={styles.connCol}>
                <Text style={[styles.connLbl, { color: theme.text }]}>
                  {activeServer?.label || t('noHostSelected')}
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
                  <Text style={[styles.switchBtnTxt, { color: theme.text }]}>{t('pairingNodes')}</Text>
                </Pressable>
                <Pressable
                  onPress={() => router.push('/connect')}
                  style={[styles.switchBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.switchBtnTxt, { color: theme.text }]}>{t('switchHost')}</Text>
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
  swatchRow: { flexDirection: 'row', gap: 10, flexWrap: 'wrap' },
  swatch: { width: 28, height: 28, borderRadius: 14 },
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
