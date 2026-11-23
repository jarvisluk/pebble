import { router } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  BackHandler,
  KeyboardAvoidingView,
  Linking,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
} from 'react-native';

type SettingsTab = 'general' | 'models' | 'memory' | 'desktop' | 'remote';
const SETTINGS_TABS: Array<{ id: SettingsTab; labelKey: string }> = [
  { id: 'general', labelKey: 'tabGeneral' },
  { id: 'models', labelKey: 'tabModels' },
  { id: 'memory', labelKey: 'tabMemory' },
  { id: 'desktop', labelKey: 'tabDesktop' },
  { id: 'remote', labelKey: 'tabRemote' },
];

import { BotAvatar } from '../components/BotAvatar';
import { ModalHeader } from '../components/ModalHeader';
import { confirmAction } from '../confirm';
import { useI18n } from '../i18n';
import { resolveDefaultModel, useHub } from '../store';
import { MONO_FONT, useTheme, useThemePref } from '../theme';
import type { LanguagePreference, ThemePreference } from '../storage';
import type { DesktopPermissionItem, MagpieMemoryList } from '../types';

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
  const { width } = useWindowDimensions();
  const isWide = width >= 768;
  const [tab, setTab] = useState<SettingsTab>('general');
  const theme = useTheme();
  const { pref: themePref, setPref: setThemePref } = useThemePref();
  const { pref: langPref, setPref: setLangPref, t } = useI18n();
  const {
    activeServer,
    connState,
    online,
    nodes,
    takeovers,
    bots,
    settings,
    models,
    loadSettings,
    loadModels,
    refreshBots,
    saveSettings,
    connectMagpie,
    disconnectMagpie,
    openMagpie,
    setDesktopWorker,
    fetchDesktopPermissions,
    requestDesktopPermission,
    fetchMemories,
    saveMemory,
    deleteMemory,
  } = useHub();

  const statusDotColor = useMemo(() => {
    if (connState === 'connected' && online) return theme.sageInk;
    if (connState === 'connected' || connState === 'connecting') return theme.busy;
    if (connState === 'unauthorized' || connState === 'error') return theme.danger;
    return theme.mute;
  }, [connState, online, theme]);

  const statusText = useMemo(() => {
    if (!activeServer) return t('disconnected');
    if (connState === 'connecting') return `${activeServer.label} · ${t('connectingShort')}`;
    if (connState === 'unauthorized') return `${activeServer.label} · ${t('needsToken')}`;
    if (connState === 'error') return `${activeServer.label} · ${t('connLost')}`;
    if (connState === 'connected' && !online) return `${activeServer.label} · ${t('reconnecting')}`;
    if (connState === 'connected') return activeServer.label;
    return t('disconnected');
  }, [activeServer, connState, online, t]);

  const onlineNodes = nodes.filter((n) => n.connected).length;
  const [defaultHarnessDraft, setDefaultHarnessDraft] = useState('opencode');
  const [defaultHarnessTouched, setDefaultHarnessTouched] = useState(false);
  const [defaultModel, setDefaultModel] = useState('');
  const [defaultModelTouched, setDefaultModelTouched] = useState(false);
  const [defaultModelsDraft, setDefaultModelsDraft] = useState<Record<string, string>>({});
  const [defaultModelsTouched, setDefaultModelsTouched] = useState(false);
  const [perHarnessExpanded, setPerHarnessExpanded] = useState(false);
  const [sharedUserMd, setSharedUserMd] = useState('');
  const [sharedUserMdTouched, setSharedUserMdTouched] = useState(false);
  const [botDrafts, setBotDrafts] = useState<Record<string, BotDraft>>({});
  const [touchedBots, setTouchedBots] = useState<Set<string>>(() => new Set());
  const [loadingModelsFor, setLoadingModelsFor] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [busyAction, setBusyAction] = useState('');
  const [error, setError] = useState('');
  const [savedMsg, setSavedMsg] = useState('');


  const [memData, setMemData] = useState<MagpieMemoryList | null>(null);
  const [memLoading, setMemLoading] = useState(false);
  const [memQuery, setMemQuery] = useState('');
  const [memShowAdd, setMemShowAdd] = useState(false);
  const [memKind, setMemKind] = useState<'fact' | 'preference' | 'rule' | 'reference'>('fact');
  const [memText, setMemText] = useState('');
  const [memTags, setMemTags] = useState('');
  const [deskPerms, setDeskPerms] = useState<DesktopPermissionItem[]>([]);
  const [deskPermsLoading, setDeskPermsLoading] = useState(false);

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

  const loadPerms = useCallback(async () => {
    setDeskPermsLoading(true);
    try {
      const res = await fetchDesktopPermissions('local');
      setDeskPerms(res.items || []);
    } catch {
      setDeskPerms([]);
    } finally {
      setDeskPermsLoading(false);
    }
  }, [fetchDesktopPermissions]);

  const handleReqPerm = async (id: string, openSettings: boolean) => {
    setBusyAction(`perm:${id}`);
    setError('');
    setSavedMsg('');
    try {
      const res = await requestDesktopPermission('local', id, openSettings);
      if (res.items) setDeskPerms(res.items);
      if (res.openedSettings) setSavedMsg(t('deskPermOpened'));
      else if (res.prompted) setSavedMsg(t('deskPermPrompted'));
      setTimeout(() => { void loadPerms(); }, 1500);
    } catch (err: any) {
      setError(err?.message || t('opFailed'));
    } finally {
      setBusyAction('');
    }
  };

  useEffect(() => {
    loadSettings().catch(() => {});
    refreshBots().catch(() => {});
    void loadGlobalMemories('');
    void loadPerms();
  }, [loadSettings, refreshBots, loadPerms]);

  useEffect(() => {
    if (settings && !defaultHarnessTouched) {
      setDefaultHarnessDraft(settings.defaultHarness || settings.harnesses?.[0]?.id || 'opencode');
    }
    if (settings && !defaultModelTouched) {
      setDefaultModel((settings.defaultModel || '').trim());
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
  }, [defaultHarnessTouched, defaultModelTouched, defaultModelsTouched, deskTouched, settings, sharedUserMdTouched]);

  useEffect(() => {
    setBotDrafts((prev) => {
      let changed = false;
      const next: Record<string, BotDraft> = {};
      const validIds = new Set(bots.map((b) => b.id));
      for (const id of Object.keys(prev)) {
        if (!validIds.has(id)) changed = true;
      }
      for (const b of bots) {
        if (touchedBots.has(b.id) && prev[b.id]) {
          next[b.id] = prev[b.id];
          continue;
        }
        const target = {
          harness: b.harness,
          model: (b.modelInherited ? '' : b.model || '').trim(),
        };
        const cur = prev[b.id];
        if (!cur || cur.harness !== target.harness || cur.model !== target.model) {
          changed = true;
        }
        next[b.id] = target;
      }
      return changed ? next : prev;
    });
  }, [bots, touchedBots]);

  const harnessOptions = useMemo(() => {
    if (settings?.harnesses?.length) return settings.harnesses;
    return [
      { id: 'opencode', label: 'opencode', defaultModel: '', magpie: { connected: false } },
      { id: 'claude', label: 'claude', defaultModel: '', magpie: { connected: false } },
      { id: 'pi', label: 'pi', defaultModel: '', magpie: { connected: false } },
    ];
  }, [settings]);

  const primaryHarness = harnessOptions[0]?.id || 'opencode';

  const autoLoadedRef = useRef<Set<string>>(new Set());

  useEffect(() => {
    const needed = new Set<string>();
    if (primaryHarness) needed.add(primaryHarness);
    for (const b of bots) {
      const h = botDrafts[b.id]?.harness || b.harness;
      if (h) needed.add(h);
    }
    if (perHarnessExpanded) {
      for (const h of harnessOptions) needed.add(h.id);
    }
    for (const hId of needed) {
      if ((models[hId] || []).length > 0 || autoLoadedRef.current.has(hId)) continue;
      autoLoadedRef.current.add(hId);
      loadModels(hId).catch(() => {});
    }
  }, [botDrafts, bots, harnessOptions, loadModels, models, perHarnessExpanded, primaryHarness]);

  const connectedHarnesses = useMemo(
    () => harnessOptions.filter((h) => Boolean(h.magpie?.connected)),
    [harnessOptions]
  );

  const nativeHarnesses = useMemo(
    () => harnessOptions.filter((h) => !h.magpie?.connected),
    [harnessOptions]
  );

  const botUpdates = useMemo(() => {
    const out: { id: string; harness: string; model: string }[] = [];
    for (const b of bots) {
      if (!touchedBots.has(b.id)) continue;
      const draft = botDrafts[b.id];
      if (!draft) continue;
      const origModel = (b.modelInherited ? '' : b.model || '').trim();
      if (draft.harness !== b.harness || draft.model.trim() !== origModel) {
        out.push({ id: b.id, harness: draft.harness, model: draft.model.trim() });
      }
    }
    return out;
  }, [botDrafts, bots, touchedBots]);

  const cleanedDraftPerHarness = useMemo(() => cleanModelsMap(defaultModelsDraft), [defaultModelsDraft]);
  const cleanedBasePerHarness = useMemo(() => cleanModelsMap(settings?.defaultModels), [settings?.defaultModels]);

  const settingsChanged = Boolean(settings) && (
    (defaultHarnessTouched && defaultHarnessDraft !== (settings?.defaultHarness || harnessOptions[0]?.id || 'opencode')) ||
    (defaultModelTouched && defaultModel.trim() !== (settings?.defaultModel || '').trim()) ||
    (defaultModelsTouched && !mapsEqual(cleanedDraftPerHarness, cleanedBasePerHarness)) ||
    (sharedUserMdTouched && sharedUserMd.replace(/\s+$/, '') !== (settings?.sharedUserMd || '').replace(/\s+$/, ''))
  );
  const dirty = settingsChanged || botUpdates.length > 0;

  const doCloseSheet = useCallback(() => {
    if (router.canGoBack()) {
      router.back();
    } else {
      router.replace('/');
    }
  }, []);

  const onSave = useCallback(async (closeAfter = false) => {
    setError('');
    setSavedMsg('');
    setSaving(true);
    try {
      await saveSettings({
        settingsChanged,
        defaultHarness: defaultHarnessDraft,
        defaultModel: defaultModel.trim(),
        defaultModels: cleanedDraftPerHarness,
        sharedUserMd,
        botUpdates,
      });
      setDefaultHarnessTouched(false);
      setDefaultModelTouched(false);
      setDefaultModelsTouched(false);
      setSharedUserMdTouched(false);
      setTouchedBots(new Set());
      setBotDrafts({});
      setSavedMsg(t('settingsSaved'));
      if (closeAfter) {
        doCloseSheet();
      }
    } catch (err: any) {
      setError(err?.message || t('saveFailed'));
    } finally {
      setSaving(false);
    }
  }, [botUpdates, cleanedDraftPerHarness, defaultHarnessDraft, defaultModel, doCloseSheet, saveSettings, settingsChanged, sharedUserMd, t]);

  const requestClose = useCallback(() => {
    if (!dirty) {
      doCloseSheet();
      return;
    }
    Alert.alert(t('unsavedTitle'), t('unsavedDesc'), [
      { text: t('cancel'), style: 'cancel' },
      { text: t('discardChanges'), style: 'destructive', onPress: doCloseSheet },
      { text: t('saveAndLeave'), onPress: () => void onSave(true) },
    ]);
  }, [dirty, doCloseSheet, onSave, t]);

  useEffect(() => {
    const sub = BackHandler.addEventListener('hardwareBackPress', () => {
      if (!dirty) return false;
      requestClose();
      return true;
    });
    return () => sub.remove();
  }, [dirty, requestClose]);

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

  const onOpenMagpie = async () => {
    setError('');
    try {
      const webUrl = settings?.magpie?.webUrl;
      if (webUrl) await Linking.openURL(webUrl);
      else await openMagpie();
    } catch (err: any) {
      setError(err?.message || t('opFailed'));
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

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader
        title={t('settings')}
        onClose={requestClose}
        rightLabel={t('save')}
        onRightPress={() => void onSave(false)}
        rightDisabled={!dirty || saving}
        rightLoading={saving}
      />
      <KeyboardAvoidingView
        style={[styles.flex, isWide && { flexDirection: 'row' }]}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        {isWide ? (
          <View style={[styles.sideNav, { borderRightColor: theme.line }]}>
            {SETTINGS_TABS.map((item) => {
              const active = tab === item.id;
              return (
                <Pressable
                  key={item.id}
                  onPress={() => setTab(item.id)}
                  style={[styles.sideNavItem, active && { backgroundColor: theme.card }]}
                >
                  <Text style={[styles.sideNavTxt, { color: active ? theme.text : theme.text2 }]}>
                    {t(item.labelKey)}
                  </Text>
                </Pressable>
              );
            })}
          </View>
        ) : (
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={styles.tabStrip}
            style={{ flexGrow: 0 }}
          >
            {SETTINGS_TABS.map((item) => {
              const active = tab === item.id;
              return (
                <Pressable
                  key={item.id}
                  onPress={() => setTab(item.id)}
                  style={[
                    styles.pill,
                    { backgroundColor: active ? theme.accentSoft : theme.s1 },
                  ]}
                >
                  <Text style={[styles.pillTxt, { color: active ? theme.accent : theme.text2 }]}>
                    {t(item.labelKey)}
                  </Text>
                </Pressable>
              );
            })}
          </ScrollView>
        )}
        <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled" style={styles.flex}>
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

          {tab === 'general' && (
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
          )}

          {tab === 'remote' && (
          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('hostSection')}</Text>
            <Pressable onPress={() => router.push('/connect')} style={styles.rowBetween}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 8, flex: 1, minWidth: 0 }}>
                <View style={{ width: 8, height: 8, borderRadius: 4, backgroundColor: statusDotColor }} />
                <Text numberOfLines={1} style={[styles.hostTxt, { color: theme.text }]}>{statusText}</Text>
              </View>
              <Text style={[styles.hostTxt, { color: theme.mute }]}>{t('switchHost')}</Text>
            </Pressable>
            <Pressable
              onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0]?.target || 'local')}`)}
            >
              <Text style={[styles.hostTxt, { color: theme.accent }]}>
                {takeovers.length > 0 ? `${t('remoteDesktop')} (${takeovers.length})` : t('remoteDesktop')}
              </Text>
            </Pressable>
            <Pressable onPress={() => router.push('/devices')}>
              <Text style={[styles.hostTxt, { color: theme.accent }]}>
                {onlineNodes > 0 ? `${t('devicesTunnel')} (${onlineNodes})` : t('devicesTunnel')}
              </Text>
            </Pressable>
          </View>
          )}

          {tab === 'models' && (
          <>
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
            ) : connectedHarnesses.length === 0 ? (
              <Text style={[styles.noteTxt, { color: theme.mute }]}>
                {t('mpNoConnected')}
              </Text>
            ) : (
              <View style={styles.magpieList}>
                {connectedHarnesses.map((h) => {
                  return (
                    <View key={h.id} style={[styles.magpieRow, { flexDirection: 'column', alignItems: 'stretch' }]}>
                      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                        <View style={styles.magpieInfo}>
                          <Text style={[styles.magpieName, { color: theme.text }]}>{h.label || h.id}</Text>
                          <Text numberOfLines={1} style={[styles.magpieSub, { color: theme.mute, fontFamily: MONO_FONT }]}>
                            {`Magpie · ${h.magpie.model || t('mpConnected')}`}
                          </Text>
                        </View>
                        <View style={styles.magpieActs}>
                          <Pressable
                            disabled={busyAction === `magpie:${h.id}`}
                            onPress={() => onDisconnectMagpie(h.id)}
                            style={[styles.magpieBtn, { backgroundColor: theme.card }]}
                          >
                            <Text style={[styles.magpieBtnTxt, { color: theme.danger }]}>{t('mpRestore')}</Text>
                          </Pressable>
                        </View>
                      </View>
                    </View>
                  );
                })}
              </View>
            )}

            {mp?.installed && (
              <View style={styles.rowBetween}>
                <Text style={[styles.noteTxt, { color: theme.mute, flex: 1 }]}>{t('mpManageInApp')}</Text>
                <Pressable onPress={() => void onOpenMagpie()} style={[styles.magpieBtn, { backgroundColor: theme.accent }]}>
                  <Text style={[styles.magpieBtnTxt, { color: theme.accentText }]}>{mp?.webUrl ? t('mpOpenWeb') : t('mpOpenApp')}</Text>
                </Pressable>
              </View>
            )}
          </View>

          {nativeHarnesses.length > 0 && (
            <View style={[styles.card, { backgroundColor: theme.card }]}>
              <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('nativeClisTitle')}</Text>
              <Text style={[styles.noteTxt, { color: theme.mute }]}>{t('nativeClisIntro')}</Text>
              <View style={styles.magpieList}>
                {nativeHarnesses.map((h) => {
                  const nativeCands = models[h.id] || [];
                  const curVal = defaultModelsDraft[h.id] || '';
                  return (
                    <View key={h.id} style={[styles.magpieRow, { flexDirection: 'column', alignItems: 'stretch', gap: 8 }]}>
                      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
                        <View style={styles.magpieInfo}>
                          <Text style={[styles.magpieName, { color: theme.text }]}>{h.label || h.id}</Text>
                          <Text numberOfLines={1} style={[styles.magpieSub, { color: theme.mute, fontFamily: MONO_FONT }]}>
                            {`${t('mpNative')}${h.magpie?.nativeModel ? ` · ${h.magpie.nativeModel}` : ''}`}
                          </Text>
                        </View>
                        <View style={styles.magpieActs}>
                          <Pressable
                            disabled={loadingModelsFor === h.id}
                            onPress={() => void onLoadModels(h.id)}
                            style={[styles.smallBtn, { backgroundColor: theme.card }]}
                          >
                            <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                              {loadingModelsFor === h.id ? t('loading') : t('optionalModels')}
                            </Text>
                          </Pressable>
                          {mp?.installed && (
                            <Pressable
                              disabled={busyAction === `magpie:${h.id}`}
                              onPress={() => onConnectMagpie(h.id)}
                              style={[styles.smallBtn, { backgroundColor: theme.card }]}
                            >
                              {busyAction === `magpie:${h.id}` ? (
                                <ActivityIndicator size="small" color={theme.accent} />
                              ) : (
                                <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>{t('mpConnectAction')}</Text>
                              )}
                            </Pressable>
                          )}
                        </View>
                      </View>
                      <TextInput
                        value={curVal}
                        onChangeText={(val) => {
                          if (val === curVal) return;
                          setDefaultModelsTouched(true);
                          setSavedMsg('');
                          setDefaultModelsDraft((prev) => ({ ...prev, [h.id]: val }));
                        }}
                        placeholder={t('nativeModelPh')}
                        placeholderTextColor={theme.mute}
                        autoCapitalize="none"
                        autoCorrect={false}
                        style={[styles.input, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                      />
                      {nativeCands.length > 0 && (
                        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
                          {nativeCands.slice(0, 16).map((m) => (
                            <Pressable
                              key={m}
                              onPress={() => {
                                if (m === curVal) return;
                                setDefaultModelsTouched(true);
                                setSavedMsg('');
                                setDefaultModelsDraft((prev) => ({ ...prev, [h.id]: m }));
                              }}
                              style={[
                                styles.pill,
                                { backgroundColor: curVal.trim() === m ? theme.accentSoft : theme.card },
                              ]}
                            >
                              <Text
                                style={[
                                  styles.modelChipTxt,
                                  { color: curVal.trim() === m ? theme.accent : theme.text2, fontFamily: MONO_FONT },
                                ]}
                              >
                                {m}
                              </Text>
                            </Pressable>
                          ))}
                        </ScrollView>
                      )}
                    </View>
                  );
                })}
              </View>
            </View>
          )}

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
            <Text style={[styles.subLabel, { color: theme.text2 }]}>{t('defaultHarness')}</Text>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
              {harnessOptions.map((h) => (
                <Pressable
                  key={h.id}
                  onPress={() => {
                    if (h.id === defaultHarnessDraft) return;
                    setDefaultHarnessDraft(h.id);
                    setDefaultHarnessTouched(true);
                    setSavedMsg('');
                  }}
                  style={[
                    styles.pill,
                    { backgroundColor: defaultHarnessDraft === h.id ? theme.accentSoft : theme.s1 },
                  ]}
                >
                  <Text style={[styles.pillTxt, { color: defaultHarnessDraft === h.id ? theme.accent : theme.text }]}>
                    {h.label || h.id}
                  </Text>
                </Pressable>
              ))}
            </ScrollView>
            <TextInput
              value={defaultModel}
              onChangeText={(val) => {
                if (val === defaultModel) return;
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
                {harnessOptions.map((h) => {
                  const hModels = models[h.id] || [];
                  const curHVal = defaultModelsDraft[h.id] || '';
                  return (
                    <View key={h.id} style={styles.perHarnessItem}>
                      <View style={styles.rowBetween}>
                        <Text style={[styles.magpieSub, { color: theme.mute }]}>{h.label || h.id}</Text>
                        <Pressable
                          disabled={loadingModelsFor === h.id}
                          onPress={() => void onLoadModels(h.id)}
                          style={[styles.smallBtn, { backgroundColor: theme.card }]}
                        >
                          <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                            {loadingModelsFor === h.id ? t('loading') : t('optionalModels')}
                          </Text>
                        </Pressable>
                      </View>
                      <TextInput
                        value={curHVal}
                        onChangeText={(val) => {
                          if (val === curHVal) return;
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
                      {hModels.length > 0 && (
                        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
                          {hModels.slice(0, 20).map((m) => (
                            <Pressable
                              key={m}
                              onPress={() => {
                                setDefaultModelsTouched(true);
                                setSavedMsg('');
                                setDefaultModelsDraft((prev) => ({
                                  ...prev,
                                  [h.id]: curHVal.trim() === m ? '' : m,
                                }));
                              }}
                              style={[
                                styles.pill,
                                { backgroundColor: curHVal.trim() === m ? theme.accentSoft : theme.card },
                              ]}
                            >
                              <Text
                                style={[
                                  styles.modelChipTxt,
                                  { color: curHVal.trim() === m ? theme.accent : theme.text2, fontFamily: MONO_FONT },
                                ]}
                              >
                                {m}
                              </Text>
                            </Pressable>
                          ))}
                        </ScrollView>
                      )}
                    </View>
                  );
                })}
              </View>
            )}
            {candidateModels.length > 0 && (
              <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
                {candidateModels.slice(0, 12).map((m) => (
                  <Pressable
                    key={m}
                    onPress={() => {
                      if (m === defaultModel) return;
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
                        <BotAvatar id={b.id} size={36} index={index} avatar={b.avatar} color={b.color} ringColor={theme.s1} />
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
                                if (h.id === draft.harness) return;
                                setSavedMsg('');
                                setTouchedBots((prev) => {
                                  const next = new Set(prev);
                                  next.add(b.id);
                                  return next;
                                });
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

                      <View style={styles.rowBetween}>
                        <TextInput
                          value={draft.model}
                          onChangeText={(val) => {
                            if (val === draft.model) return;
                            setSavedMsg('');
                            setTouchedBots((prev) => {
                              const next = new Set(prev);
                              next.add(b.id);
                              return next;
                            });
                            setBotDrafts((prev) => ({
                              ...prev,
                              [b.id]: { ...draft, model: val },
                            }));
                          }}
                          placeholder={ph}
                          placeholderTextColor={theme.mute}
                          autoCapitalize="none"
                          autoCorrect={false}
                          style={[
                            styles.input,
                            { flex: 1, backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT },
                          ]}
                        />
                        <Pressable
                          disabled={loadingModelsFor === draft.harness}
                          onPress={() => void onLoadModels(draft.harness)}
                          style={[styles.smallBtn, { backgroundColor: theme.card }]}
                        >
                          <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                            {loadingModelsFor === draft.harness ? t('loading') : t('optionalModels')}
                          </Text>
                        </Pressable>
                      </View>
                      {(models[draft.harness] || []).length > 0 && (
                        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.pillRow}>
                          {(models[draft.harness] || []).slice(0, 24).map((m) => (
                            <Pressable
                              key={m}
                              onPress={() => {
                                const nextModel = draft.model.trim() === m ? '' : m;
                                if (nextModel === draft.model) return;
                                setSavedMsg('');
                                setTouchedBots((prev) => {
                                  const next = new Set(prev);
                                  next.add(b.id);
                                  return next;
                                });
                                setBotDrafts((prev) => ({
                                  ...prev,
                                  [b.id]: { ...draft, model: nextModel },
                                }));
                              }}
                              style={[
                                styles.pill,
                                { backgroundColor: draft.model.trim() === m ? theme.accentSoft : theme.card },
                              ]}
                            >
                              <Text
                                style={[
                                  styles.modelChipTxt,
                                  { color: draft.model.trim() === m ? theme.accent : theme.text2, fontFamily: MONO_FONT },
                                ]}
                              >
                                {m}
                              </Text>
                            </Pressable>
                          ))}
                        </ScrollView>
                      )}
                    </View>
                  );
                })}
              </View>
            )}
          </View>
          </>
          )}

          {tab === 'memory' && (
          <>
          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.rowBetween}>
              <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('globalSharedMem')}</Text>
              <Text style={[styles.magpieSub, { color: theme.mute, fontFamily: MONO_FONT }]}>_shared/USER.md</Text>
            </View>
            <TextInput
              value={sharedUserMd}
              onChangeText={(val) => {
                if (val === sharedUserMd) return;
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
          </>
          )}

          {tab === 'desktop' && (
          <>
          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <View style={styles.rowBetween}>
              <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('deskPermsTitle')}</Text>
              <View style={styles.magpieActs}>
                <Pressable
                  onPress={() => void loadPerms()}
                  disabled={deskPermsLoading}
                  style={[styles.smallBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>
                    {deskPermsLoading ? t('refreshing') : t('refresh')}
                  </Text>
                </Pressable>
                <Pressable
                  onPress={() => router.push('/desktop?target=local')}
                  style={[styles.smallBtn, { backgroundColor: theme.accentSoft }]}
                >
                  <Text style={[styles.smallBtnTxt, { color: theme.accent }]}>{t('openDesktop')}</Text>
                </Pressable>
              </View>
            </View>
            <Text style={[styles.noteTxt, { color: theme.mute }]}>{t('deskPermsDesc')}</Text>
            {deskPerms.length > 0 && (
              <View style={styles.magpieList}>
                {deskPerms.map((perm) => {
                  const stLabel =
                    perm.status === 'granted'
                      ? t('deskPermGranted')
                      : perm.status === 'denied'
                        ? t('deskPermDenied')
                        : perm.status === 'unsupported'
                          ? t('deskPermUnsupported')
                          : t('deskPermNotDetermined');
                  const badgeBg =
                    perm.status === 'granted' ? theme.sage : perm.status === 'denied' ? theme.pink : theme.s1;
                  const badgeColor =
                    perm.status === 'granted' ? theme.sageInk : perm.status === 'denied' ? theme.danger : theme.mute;
                  const busy = busyAction === `perm:${perm.id}`;
                  return (
                    <View key={perm.id} style={[styles.magpieRow, { flexDirection: 'column', alignItems: 'stretch', gap: 6 }]}>
                      <View style={styles.rowBetween}>
                        <Text style={[styles.magpieName, { color: theme.text, flex: 1 }]}>{perm.name}</Text>
                        <View style={[styles.smallBtn, { backgroundColor: badgeBg }]}>
                          <Text style={[styles.smallBtnTxt, { color: badgeColor }]}>{stLabel}</Text>
                        </View>
                      </View>
                      <Text style={[styles.noteTxt, { color: theme.mute }]}>{perm.description}</Text>
                      {((perm.canPrompt && perm.status !== 'granted') || perm.canOpenSettings) && (
                        <View style={{ flexDirection: 'row', gap: 8, flexWrap: 'wrap' }}>
                          {perm.canPrompt && perm.status !== 'granted' && (
                            <Pressable
                              disabled={busy}
                              onPress={() => void handleReqPerm(perm.id, false)}
                              style={[styles.smallBtn, { backgroundColor: theme.accent }]}
                            >
                              <Text style={[styles.smallBtnTxt, { color: theme.accentText }]}>{t('deskPermRequest')}</Text>
                            </Pressable>
                          )}
                          {perm.canOpenSettings && (
                            <Pressable
                              disabled={busy}
                              onPress={() => void handleReqPerm(perm.id, true)}
                              style={[styles.smallBtn, { backgroundColor: theme.card }]}
                            >
                              <Text style={[styles.smallBtnTxt, { color: theme.text2 }]}>{t('deskPermOpenSettings')}</Text>
                            </Pressable>
                          )}
                        </View>
                      )}
                    </View>
                  );
                })}
              </View>
            )}
          </View>

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.sectionTitle, { color: theme.text }]}>{t('deskWorkerTitle')}</Text>
            <View style={{ gap: 6, backgroundColor: theme.s1, borderRadius: 14, padding: 10 }}>
              <Text style={[styles.subLabel, { color: theme.text2 }]}>{t('deskWorkerHarnessLabel')}</Text>
              <View style={{ flexDirection: 'row', gap: 6, flexWrap: 'wrap' }}>
                {[{ id: '', label: t('deskWorkerFollowBot') }, ...(settings?.harnesses || [])].map((h) => (
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
              <Text style={[styles.subLabel, { color: theme.text2, marginTop: 4 }]}>{t('deskWorkerModelLabel')}</Text>
              <View style={{ flexDirection: 'row', gap: 8, alignItems: 'center' }}>
                <TextInput
                  value={deskModel}
                  onChangeText={(v) => {
                    setDeskModel(v);
                    setDeskTouched(true);
                  }}
                  placeholder={t('deskWorkerModelPh')}
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
                      setSavedMsg(t('deskWorkerSaved'));
                    } catch (err: any) {
                      setError(err?.message || t('saveFailed'));
                    } finally {
                      setBusyAction('');
                    }
                  }}
                  style={[styles.switchBtn, { backgroundColor: theme.accent }]}
                >
                  <Text style={[styles.switchBtnTxt, { color: theme.accentText }]}>
                    {busyAction === 'desk' ? t('saving') : t('save')}
                  </Text>
                </Pressable>
              </View>
            </View>
          </View>
          </>
          )}
        </ScrollView>
      </KeyboardAvoidingView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
  sideNav: { width: 220, borderRightWidth: StyleSheet.hairlineWidth, padding: 12, gap: 4 },
  sideNavItem: { borderRadius: 12, paddingHorizontal: 14, paddingVertical: 11 },
  sideNavTxt: { fontSize: 16, fontWeight: '600' },
  tabStrip: { paddingHorizontal: 16, paddingVertical: 8, gap: 8 },
  scroll: { padding: 16, gap: 12, paddingBottom: 36 },
  msgCard: { borderRadius: 16, paddingHorizontal: 14, paddingVertical: 10 },
  msgTxt: { fontSize: 15, fontWeight: '600' },
  card: { borderRadius: 22, padding: 16, gap: 12 },
  sectionTitle: { fontSize: 17, fontWeight: '700' },
  hostTxt: { fontSize: 17 },
  subLabel: { fontSize: 15, fontWeight: '600' },
  swatchRow: { flexDirection: 'row', gap: 10, flexWrap: 'wrap' },
  swatch: { width: 28, height: 28, borderRadius: 14 },
  noteTxt: { fontSize: 15, lineHeight: 21, flex: 1 },
  rowBetween: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 10 },
  segmentRow: { flexDirection: 'row', gap: 8 },
  segmentPill: { flex: 1, borderRadius: 999, paddingVertical: 8, alignItems: 'center', justifyContent: 'center' },
  segmentTxt: { fontSize: 15, fontWeight: '700' },
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
  magpieName: { fontSize: 16, fontWeight: '600' },
  magpieSub: { fontSize: 12.5 },
  magpieActs: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  magpieBtn: { borderRadius: 999, paddingHorizontal: 11, paddingVertical: 6 },
  magpieBtnTxt: { fontSize: 13.5, fontWeight: '700' },
  addProvBox: { borderRadius: 16, padding: 12, gap: 10 },
  addBtn: { borderRadius: 999, paddingVertical: 10, alignItems: 'center', justifyContent: 'center' },
  smallBtn: { borderRadius: 999, paddingHorizontal: 11, paddingVertical: 5 },
  smallBtnTxt: { fontSize: 13.5, fontWeight: '600' },
  input: { borderRadius: 14, paddingHorizontal: 13, paddingVertical: 10, fontSize: 17 },
  textArea: { minHeight: 110, fontSize: 15, lineHeight: 21 },
  perHarnessItem: { gap: 4 },
  pillRow: { gap: 8 },
  wrapRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  pill: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 7 },
  pillTxt: { fontSize: 15, fontWeight: '700' },
  modelChipTxt: { fontSize: 13.5, fontWeight: '600' },
  emptyNote: { fontSize: 15 },
  botList: { gap: 10 },
  botItem: { borderRadius: 18, padding: 13, gap: 10 },
  botHead: { flexDirection: 'row', alignItems: 'center', gap: 10 },
  botHeadTxt: { flex: 1 },
  botName: { fontSize: 17, fontWeight: '700' },
  botId: { fontSize: 13 },
  switchBtn: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 8 },
  switchBtnTxt: { fontSize: 15, fontWeight: '600' },
});
