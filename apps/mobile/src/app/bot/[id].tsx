import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import { router, useLocalSearchParams } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';

import { BotAvatar } from '../../components/BotAvatar';
import { ChevronLeftIcon, DocIcon, MonitorIcon, SendIcon, StopIcon } from '../../components/Icons';
import { MarkdownText } from '../../components/MarkdownText';
import { ModalHeader } from '../../components/ModalHeader';
import { QuestionCard } from '../../components/QuestionCard';
import { confirmAction } from '../../confirm';
import { useI18n } from '../../i18n';
import { formatStamp, useHub } from '../../store';
import { getBotPalette, MONO_FONT, useTheme } from '../../theme';
import type {
  BotDetail,
  ChatMessage,
  MagpieMemoryList,
  RoutineDelivery,
  RoutineItem,
  RoutineRun,
} from '../../types';

const FIVE_MIN_MS = 5 * 60 * 1000;

export default function BotChatScreen() {
  const { id } = useLocalSearchParams<{ id: string | string[] }>();
  const botId = useMemo(() => decodeURIComponent(Array.isArray(id) ? id[0] || '' : id || ''), [id]);

  const theme = useTheme();
  const { t, lang } = useI18n();
  const insets = useSafeAreaInsets();
  const {
    baseURL,
    bots,
    msgs,
    tools,
    takeovers,
    questions,
    setActiveChatBot,
    loadMessages,
    sendMessage,
    interruptBot,
    resetBot,
    getBotDetail,
    updateBot,
    deleteBot,
    resolveTakeover,
    replyQuestion,
    fetchMemories,
    saveMemory,
    deleteMemory,
    routines,
    fetchRoutines,
    createRoutine,
    updateRoutine,
    deleteRoutine,
    runRoutine,
    fetchRoutineRuns,
  } = useHub();

  const botIndex = useMemo(() => bots.findIndex((b) => b.id === botId), [bots, botId]);
  const bot = botIndex >= 0 ? bots[botIndex] : undefined;
  const botPal = useMemo(() => getBotPalette(botId, botIndex, theme.dark), [botId, botIndex, theme.dark]);
  const chatList = msgs[botId] || [];
  const botQuestions = useMemo(() => questions.filter((q) => q.botId === botId), [questions, botId]);

  const slashCommands = useMemo(
    () => [
      { cmd: '/status', label: t('statusLabel'), autoSend: true },
      { cmd: '/bots', label: t('allBotsLabel'), autoSend: true },
      { cmd: '/ask ', label: t('collabLabel'), autoSend: false },
      { cmd: '/reset', label: t('resetSessionLabel'), autoSend: true },
    ],
    [t]
  );

  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detail, setDetail] = useState<BotDetail | null>(null);
  const [techExpanded, setTechExpanded] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editInst, setEditInst] = useState('');
  const [editPrivate, setEditPrivate] = useState(false);
  const [editSaving, setEditSaving] = useState(false);
  const [editErr, setEditErr] = useState('');
  const [sendErr, setSendErr] = useState('');

  const [botMem, setBotMem] = useState<MagpieMemoryList | null>(null);
  const [botMemLoading, setBotMemLoading] = useState(false);
  const [botMemShowAdd, setBotMemShowAdd] = useState(false);
  const [botMemText, setBotMemText] = useState('');
  const [botMemSaving, setBotMemSaving] = useState(false);

  const botRoutines = routines[botId] || [];
  const [rtLoading, setRtLoading] = useState(false);
  const [rtShowForm, setRtShowForm] = useState(false);
  const [rtEditId, setRtEditId] = useState('');
  const [rtName, setRtName] = useState('');
  const [rtMode, setRtMode] = useState<'interval' | 'daily' | 'cron' | 'webhook'>('interval');
  const [rtEvery, setRtEvery] = useState('60');
  const [rtDaily, setRtDaily] = useState('09:00');
  const [rtCron, setRtCron] = useState('0 9 * * 1-5');
  const [rtWebhookTrigger, setRtWebhookTrigger] = useState(false);
  const [rtWebhookSecret, setRtWebhookSecret] = useState('');
  const [rtWebhookSecretSet, setRtWebhookSecretSet] = useState(false);
  const [rtClearSecret, setRtClearSecret] = useState(false);
  const [rtWebhookUrl, setRtWebhookUrl] = useState('');
  const [rtDelivery, setRtDelivery] = useState<RoutineDelivery>('receipt');
  const [rtCopiedId, setRtCopiedId] = useState('');
  const [rtPrompt, setRtPrompt] = useState('');
  const [rtEnabled, setRtEnabled] = useState(true);
  const [rtSaving, setRtSaving] = useState(false);
  const [rtErr, setRtErr] = useState('');
  const [rtRunsOpenId, setRtRunsOpenId] = useState('');
  const [rtRunsMap, setRtRunsMap] = useState<Record<string, RoutineRun[]>>({});

  const listRef = useRef<FlatList<ChatMessage>>(null);
  const inputRef = useRef<React.ComponentRef<typeof TextInput>>(null);

  useEffect(() => {
    if (bots.length > 0 && !bot) {
      setDetailOpen(false);
      router.replace('/');
    }
  }, [bot, bots.length]);

  useEffect(() => {
    if (!botId) return;
    setActiveChatBot(botId);
    loadMessages(botId).catch(() => {});
    return () => {
      setActiveChatBot(null);
    };
  }, [botId, loadMessages, setActiveChatBot]);

  useEffect(() => {
    if (chatList.length === 0) return;
    const t = setTimeout(() => {
      listRef.current?.scrollToEnd({ animated: true });
    }, 60);
    return () => clearTimeout(t);
  }, [chatList.length, bot?.busy]);

  const loadBotMemories = useCallback(async () => {
    setBotMemLoading(true);
    try {
      const m = await fetchMemories({ scope: 'project', botId, limit: 30 });
      setBotMem(m);
    } catch {
      // keep previous state on transient failure
    } finally {
      setBotMemLoading(false);
    }
  }, [botId, fetchMemories]);

  const loadBotRoutines = useCallback(async () => {
    setRtLoading(true);
    try {
      await fetchRoutines(botId);
    } catch {
      // ignore transient error
    } finally {
      setRtLoading(false);
    }
  }, [botId, fetchRoutines]);

  const openRoutineEditor = (rt?: RoutineItem) => {
    setRtErr('');
    setRtEditId(rt?.id || '');
    setRtName(rt?.name || '');
    const hasEvery = (rt?.everyMinutes || 0) > 0;
    const hasDaily = !!rt?.dailyAt;
    const hasCron = !!rt?.cron;
    let mode: 'interval' | 'daily' | 'cron' | 'webhook' = 'interval';
    if (hasCron) mode = 'cron';
    else if (hasDaily && !hasEvery) mode = 'daily';
    else if (!hasEvery && !hasDaily && rt?.webhookTrigger) mode = 'webhook';
    setRtMode(mode);
    setRtEvery(hasEvery ? String(rt?.everyMinutes) : '60');
    setRtDaily(rt?.dailyAt || '09:00');
    setRtCron(rt?.cron || '0 9 * * 1-5');
    setRtWebhookTrigger(!!rt?.webhookTrigger || mode === 'webhook');
    setRtWebhookSecret('');
    setRtWebhookSecretSet(!!rt?.webhookSecretSet);
    setRtClearSecret(false);
    setRtWebhookUrl(rt?.webhookUrl || '');
    setRtDelivery(rt?.mainDelivery || 'receipt');
    setRtPrompt(rt?.prompt || '');
    setRtEnabled(rt ? rt.enabled : true);
    setRtShowForm(true);
  };

  const onSaveRoutine = async () => {
    const name = rtName.trim();
    const prompt = rtPrompt.trim();
    if (!name) {
      setRtErr(t('rtNameRequired'));
      return;
    }
    if (!prompt) {
      setRtErr(t('rtPromptRequired'));
      return;
    }
    const everyMinutes = rtMode === 'interval' ? parseInt(rtEvery, 10) : 0;
    const dailyAt = rtMode === 'daily' ? rtDaily.trim() : '';
    const cronExpr = rtMode === 'cron' ? rtCron.trim() : '';
    const webhookTrigger = rtMode === 'webhook' ? true : rtWebhookTrigger;
    if (rtMode === 'interval' && !(everyMinutes > 0)) {
      setRtErr(t('rtEveryPositive'));
      return;
    }
    if (rtMode === 'daily' && !/^\d{2}:\d{2}$/.test(dailyAt)) {
      setRtErr(t('rtTimeFormatErr'));
      return;
    }
    if (rtMode === 'cron' && !cronExpr) {
      setRtErr(t('rtCronRequired'));
      return;
    }
    setRtErr('');
    setRtSaving(true);
    try {
      const payload: Record<string, unknown> = {
        name,
        everyMinutes,
        dailyAt,
        cron: cronExpr,
        webhookTrigger,
        webhookUrl: rtWebhookUrl.trim(),
        prompt,
        mainDelivery: rtDelivery,
        enabled: rtEnabled,
      };
      if (rtClearSecret) {
        payload.clearWebhookSecret = true;
      } else if (rtWebhookSecret.trim()) {
        payload.webhookSecret = rtWebhookSecret.trim();
      }
      if (rtEditId) {
        await updateRoutine(botId, rtEditId, payload as any);
      } else {
        await createRoutine(botId, payload as any);
      }
      setRtShowForm(false);
    } catch (err: any) {
      setRtErr(err?.message || t('rtSaveFailed'));
    } finally {
      setRtSaving(false);
    }
  };

  const handleOpenDetail = useCallback(async () => {
    setTechExpanded(false);
    setBotMemShowAdd(false);
    setRtShowForm(false);
    setRtErr('');
    setEditErr('');
    const initSrc = detail || bot;
    setEditName(initSrc?.name || botId);
    setEditDesc(initSrc?.description || '');
    setEditInst(detail?.instructions || '');
    setEditPrivate(!!initSrc?.privateSessions);
    setDetailOpen(true);
    setDetailLoading(true);
    void loadBotMemories();
    void loadBotRoutines();
    try {
      const d = await getBotDetail(botId);
      setDetail(d);
      setEditName(d.name || botId);
      setEditDesc(d.description || '');
      setEditInst(d.instructions || '');
      setEditPrivate(!!d.privateSessions);
    } catch {
      // keep fallback bot info if detail fails
    } finally {
      setDetailLoading(false);
    }
  }, [bot, botId, detail, getBotDetail, loadBotMemories, loadBotRoutines]);

  const onSaveEdit = async () => {
    const trimmed = editName.trim();
    if (!trimmed) {
      setEditErr(t('nameRequired'));
      return;
    }
    setEditErr('');
    setEditSaving(true);
    try {
      await updateBot(botId, {
        name: trimmed,
        description: editDesc.trim(),
        instructions: editInst,
        privateSessions: editPrivate,
      });
      const fresh = await getBotDetail(botId);
      setDetail(fresh);
      setEditName(fresh.name || botId);
      setEditDesc(fresh.description || '');
      setEditInst(fresh.instructions || '');
      setEditPrivate(!!fresh.privateSessions);
    } catch (err: any) {
      setEditErr(err?.message || t('saveFailed'));
    } finally {
      setEditSaving(false);
    }
  };

  const handleDeleteConfirm = () => {
    if (bots.length <= 1) return;
    confirmAction(
      t('deleteBotTitle', { name: bot?.name || botId }),
      t('deleteBotDesc'),
      async () => {
        try {
          await deleteBot(botId);
          setDetailOpen(false);
          router.replace('/');
        } catch (err: any) {
          setEditErr(err?.message || t('delFailed'));
        }
      },
      true
    );
  };

  const handleSendText = useCallback(
    async (raw: string, restoreOnError = false) => {
      const trimmed = raw.trim();
      if (!trimmed || !botId) return;
      setSendErr('');
      setSending(true);
      if (Platform.OS !== 'web') {
        Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
      }
      try {
        await sendMessage(botId, trimmed);
      } catch (err: any) {
        if (restoreOnError) setInput(raw);
        setSendErr(err?.message || t('sendFailed'));
      } finally {
        setSending(false);
      }
    },
    [botId, sendMessage, t]
  );

  const onSubmitComposer = () => {
    const text = input;
    if (!text.trim() || sending) return;
    setInput('');
    void handleSendText(text, true);
  };

  const onInterrupt = async () => {
    if (!botId) return;
    if (Platform.OS !== 'web') {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium).catch(() => {});
    }
    try {
      await interruptBot(botId);
    } catch (err: any) {
      setSendErr(err?.message || t('stopFailed'));
    }
  };

  const onPickSlash = (cmd: string, autoSend: boolean) => {
    if (autoSend) {
      setInput('');
      void handleSendText(cmd, false);
    } else {
      setInput(cmd);
      inputRef.current?.focus();
    }
  };

  const handleResetConfirm = () => {
    confirmAction(
      t('resetSessionTitle'),
      t('resetSessionDesc', { name: bot?.name || botId }),
      async () => {
        try {
          await resetBot(botId);
          setDetailOpen(false);
          await loadMessages(botId);
        } catch (err: any) {
          setSendErr(err?.message || t('resetFailed'));
        }
      },
      true
    );
  };

  const suggestions = [
    {
      title: t('sugAskTitle'),
      sub: t('sugAskSub'),
      bg: theme.pink,
      ink: theme.pinkInk,
      onPress: () => void handleSendText(t('sugAskTitle'), false),
    },
    {
      title: t('statusLabel'),
      sub: t('sugStatusSub'),
      bg: theme.lilac,
      ink: theme.lilacInk,
      onPress: () => void handleSendText('/status', false),
    },
    {
      title: t('sugCollabTitle'),
      sub: '/ask ',
      bg: theme.sage,
      ink: theme.sageInk,
      onPress: () => {
        setInput('/ask ');
        inputRef.current?.focus();
      },
    },
    {
      title: t('sugListTitle'),
      sub: '/bots',
      bg: botPal.soft,
      ink: botPal.accent,
      onPress: () => void handleSendText('/bots', false),
    },
  ];

  const renderMessage = ({ item, index }: { item: ChatMessage; index: number }) => {
    const prev = index > 0 ? chatList[index - 1] : null;
    const curMs = Date.parse(item.createdAt || '');
    const prevMs = prev ? Date.parse(prev.createdAt || '') : 0;
    const showStamp =
      !prev ||
      (!Number.isNaN(curMs) && !Number.isNaN(prevMs) && curMs - prevMs > FIVE_MIN_MS);

    let bubble: React.ReactNode = null;
    if (item.role === 'user') {
      bubble = (
        <View style={styles.userRow}>
          <View style={[styles.userBubble, { backgroundColor: botPal.soft }]}>
            <Text selectable style={[styles.userText, { color: theme.text }]}>
              {item.text}
            </Text>
          </View>
        </View>
      );
    } else if (item.role === 'assistant') {
      const metaParts: string[] = [];
      if (item.durationMs && item.durationMs > 0) {
        metaParts.push(`${(item.durationMs / 1000).toFixed(1)}s`);
      }
      if (item.costUsd && item.costUsd > 0) {
        metaParts.push(`$${item.costUsd.toFixed(4)}`);
      }
      const routineLabel = item.from?.startsWith('routine:')
        ? t('routineReceiptFrom', { name: item.from.slice(8) })
        : '';
      bubble = (
        <View style={styles.assistantRow}>
          <View style={[styles.assistantBubble, { backgroundColor: theme.card }]}>
            {routineLabel ? (
              <Text style={[styles.botRelLabel, { color: theme.accent, marginBottom: 4 }]}>
                {routineLabel}
              </Text>
            ) : null}
            <MarkdownText text={item.text} theme={theme} />
            {metaParts.length > 0 && (
              <Text style={[styles.msgMeta, { color: theme.mute }]}>{metaParts.join(' · ')}</Text>
            )}
          </View>
        </View>
      );
    } else if (item.role === 'bot_out' || item.role === 'bot_in') {
      const label =
        item.role === 'bot_out'
          ? t('delegateTo', { peer: item.to || '' })
          : t('fromPeer', { peer: item.from || '' });
      bubble = (
        <View style={styles.assistantRow}>
          <View style={[styles.botRelBubble, { backgroundColor: theme.lilac }]}>
            <Text style={[styles.botRelLabel, { color: theme.lilacInk }]}>{label}</Text>
            <MarkdownText text={item.text} theme={theme} />
          </View>
        </View>
      );
    } else {
      const isErr = item.from === 'error';
      const isCmd = item.from === 'command';
      bubble = (
        <View style={styles.systemWrap}>
          <View style={[styles.systemCard, { backgroundColor: theme.s1 }]}>
            <Text
              selectable
              style={[
                styles.systemTxt,
                isCmd && { fontFamily: MONO_FONT, fontSize: 13.5 },
                { color: isErr ? theme.danger : theme.text2 },
              ]}
            >
              {item.text}
            </Text>
          </View>
        </View>
      );
    }

    return (
      <View>
        {showStamp && (
          <Text style={[styles.stamp, { color: theme.mute }]}>{formatStamp(item.createdAt, lang)}</Text>
        )}
        {bubble}
      </View>
    );
  };

  const showStop = !!bot?.busy && !input.trim();
  const activeDetail = detail || bot;
  const isDirty = Boolean(
    activeDetail &&
      (editName !== (activeDetail.name || botId) ||
        editDesc !== (activeDetail.description || '') ||
        editPrivate !== Boolean(activeDetail.privateSessions) ||
        editInst !== (detail?.instructions || ''))
  );

  return (
    <SafeAreaView style={[styles.safe, { backgroundColor: theme.page }]} edges={['top', 'left', 'right']}>
      <View style={[styles.header, { borderBottomColor: theme.s2 }]}>
        <Pressable
          accessibilityLabel={t('back')}
          onPress={() => (router.canGoBack() ? router.back() : router.replace('/'))}
          style={styles.backBtn}
          hitSlop={10}
        >
          <ChevronLeftIcon size={22} color={theme.text} />
        </Pressable>

        <BotAvatar id={botId} size={42} index={botIndex} busy={!!bot?.busy} ringColor={theme.page} />

        <View style={styles.headInfo}>
          <Text numberOfLines={1} style={[styles.headName, { color: theme.text }]}>
            {bot?.name || botId}
          </Text>
        </View>

        <Pressable
          accessibilityLabel={t('remoteDesktop')}
          onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0]?.target || 'local')}`)}
          style={[styles.docBtn, { backgroundColor: theme.s1, marginRight: 6 }]}
          hitSlop={8}
        >
          <MonitorIcon size={18} color={takeovers.length > 0 ? theme.danger : theme.text2} />
        </Pressable>
        <Pressable
          accessibilityLabel={t('botDetails')}
          onPress={handleOpenDetail}
          style={[styles.docBtn, { backgroundColor: theme.s1 }]}
          hitSlop={8}
        >
          <DocIcon size={18} color={theme.text2} />
        </Pressable>
      </View>

      {takeovers.length > 0 && (
        <View style={[styles.errBanner, { backgroundColor: theme.pink, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }]}>
          <Text numberOfLines={2} style={[styles.errTxt, { color: theme.pinkInk, flex: 1 }]}>
            {t('takeoverInline', { reason: takeovers[0].reason })}
          </Text>
          <Pressable
            onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0].target || 'local')}`)}
            style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5, backgroundColor: theme.card }}
          >
            <Text style={{ fontSize: 13, fontWeight: '700', color: theme.text }}>{t('handleAction')}</Text>
          </Pressable>
          <Pressable
            onPress={() => void resolveTakeover(takeovers[0].id)}
            style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5, backgroundColor: botPal.accent }}
          >
            <Text style={{ fontSize: 13, fontWeight: '700', color: botPal.text }}>{t('resumeShort')}</Text>
          </Pressable>
        </View>
      )}

      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        keyboardVerticalOffset={0}
      >
        <FlatList
          ref={listRef}
          data={chatList}
          keyExtractor={(item, idx) => item.id || `${idx}`}
          renderItem={renderMessage}
          contentContainerStyle={[
            styles.listContent,
            chatList.length === 0 && styles.emptyGrow,
          ]}
          ItemSeparatorComponent={() => <View style={{ height: 10 }} />}
          keyboardDismissMode="interactive"
          keyboardShouldPersistTaps="handled"
          onContentSizeChange={() => {
            if (chatList.length > 0) {
              listRef.current?.scrollToEnd({ animated: false });
            }
          }}
          ListEmptyComponent={
            <View style={styles.emptyWrap}>
              <BotAvatar id={botId} size={92} index={botIndex} />
              <Text style={[styles.emptyTitle, { color: theme.text }]}>{bot?.name || botId}</Text>
              <Text style={[styles.emptyDesc, { color: theme.text2 }]}>
                {bot?.description || t('emptyChatHint')}
              </Text>
              <View style={styles.suggestGrid}>
                {suggestions.map((s) => (
                  <Pressable
                    key={s.title}
                    onPress={s.onPress}
                    style={({ pressed }) => [
                      styles.suggestCard,
                      { backgroundColor: s.bg, opacity: pressed ? 0.85 : 1 },
                    ]}
                  >
                    <Text style={[styles.suggestTitle, { color: s.ink }]}>{s.title}</Text>
                    <Text style={[styles.suggestSub, { color: theme.text2 }]}>{s.sub}</Text>
                  </Pressable>
                ))}
              </View>
            </View>
          }
          ListFooterComponent={
            bot?.busy || botQuestions.length > 0 ? (
              <View>
                {bot?.busy ? (
                  <View style={[styles.busyFoot, { backgroundColor: theme.s1 }]}>
                    <ActivityIndicator size="small" color={theme.busy} />
                    <Text style={[styles.busyFootTxt, { color: theme.busy }]}>
                      {tools[botId] ? t('callingTool', { tool: tools[botId] }) : t('thinking')}
                    </Text>
                  </View>
                ) : null}
                {botQuestions.map((q) => (
                  <QuestionCard
                    key={q.id}
                    question={q}
                    accent={botPal.accent}
                    accentText={botPal.text}
                    accentSoft={botPal.soft}
                    onReply={replyQuestion}
                  />
                ))}
              </View>
            ) : undefined
          }
        />

        {sendErr ? (
          <View style={[styles.errBanner, { backgroundColor: theme.pink }]}>
            <Text style={[styles.errTxt, { color: theme.danger }]}>{sendErr}</Text>
          </View>
        ) : null}

        <View style={[styles.composerWrap, { paddingBottom: Math.max(insets.bottom, 10) }]}>
          {input.startsWith('/') && (
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              keyboardShouldPersistTaps="always"
              contentContainerStyle={styles.slashRow}
            >
              {slashCommands.map((c) => (
                <Pressable
                  key={c.cmd}
                  onPress={() => onPickSlash(c.cmd, c.autoSend)}
                  style={[styles.slashPill, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.slashCmd, { color: botPal.accent, fontFamily: MONO_FONT }]}>
                    {c.cmd.trim()}
                  </Text>
                  <Text style={[styles.slashLbl, { color: theme.text2 }]}>{c.label}</Text>
                </Pressable>
              ))}
            </ScrollView>
          )}

          <View style={[styles.composerCapsule, { backgroundColor: theme.card }]}>
            <TextInput
              ref={inputRef}
              value={input}
              onChangeText={setInput}
              placeholder={t('msgBotPh', { name: bot?.name || botId })}
              placeholderTextColor={theme.mute}
              multiline
              style={[styles.composerInput, { color: theme.text }]}
              maxLength={8000}
            />
            <Pressable
              accessibilityLabel={showStop ? t('stopOutput') : t('sendMsg')}
              disabled={!showStop && (!input.trim() || sending)}
              onPress={showStop ? onInterrupt : onSubmitComposer}
              style={[
                styles.sendBtn,
                {
                  backgroundColor: showStop
                    ? theme.text
                    : input.trim()
                      ? botPal.accent
                      : theme.s2,
                },
              ]}
            >
              {showStop ? (
                <StopIcon size={16} color={theme.page} />
              ) : (
                <SendIcon size={17} color={input.trim() ? botPal.text : theme.mute} />
              )}
            </Pressable>
          </View>
        </View>
      </KeyboardAvoidingView>

      <Modal
        visible={detailOpen}
        animationType="slide"
        presentationStyle={Platform.OS === 'ios' ? 'pageSheet' : 'fullScreen'}
        onRequestClose={() => {
          setDetailOpen(false);
        }}
      >
        <View style={[styles.modalRoot, { backgroundColor: theme.page }]}>
          <ModalHeader title={t('botDetails')} onClose={() => setDetailOpen(false)} />
          <ScrollView contentContainerStyle={styles.detailScroll} keyboardShouldPersistTaps="handled">
            {editErr ? (
              <View style={[styles.errBanner, { backgroundColor: theme.pink, marginHorizontal: 0 }]}>
                <Text style={[styles.errTxt, { color: theme.danger }]}>{editErr}</Text>
              </View>
            ) : null}
            <>
              <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                <View style={styles.detailTop}>
                  <BotAvatar id={botId} size={76} index={botIndex} />
                  <View style={[styles.detailTitleCol, { gap: 4 }]}>
                    <TextInput
                      value={editName}
                      onChangeText={setEditName}
                      placeholder={t('botNamePh')}
                      placeholderTextColor={theme.mute}
                      style={[
                        styles.detailName,
                        {
                          color: theme.text,
                          backgroundColor: theme.s1,
                          borderRadius: 10,
                          paddingHorizontal: 10,
                          paddingVertical: 6,
                        },
                      ]}
                    />
                    <Text style={[styles.detailId, { color: theme.mute, fontFamily: MONO_FONT }]}>
                      @{botId}
                    </Text>
                  </View>
                </View>
                <TextInput
                  value={editDesc}
                  onChangeText={setEditDesc}
                  placeholder={t('descPh')}
                  placeholderTextColor={theme.mute}
                  style={[styles.editInput, { backgroundColor: theme.s1, color: theme.text }]}
                />

                <View style={styles.statGrid}>
                  <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                    <Text style={[styles.statLbl, { color: theme.mute }]}>{t('statTurns')}</Text>
                    <Text style={[styles.statVal, { color: theme.text }]}>
                      {activeDetail?.session?.turnCount || 0}
                    </Text>
                  </View>
                  <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                    <Text style={[styles.statLbl, { color: theme.mute }]}>{t('statCost')}</Text>
                    <Text style={[styles.statVal, { color: theme.text }]}>
                      ${(activeDetail?.session?.totalCostUsd || 0).toFixed(4)}
                    </Text>
                  </View>
                </View>
              </View>

                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
                    <Text style={[styles.sectionLabel, { color: theme.mute }]}>{t('botMemory')}</Text>
                    <Pressable
                      onPress={() => setBotMemShowAdd((v) => !v)}
                      style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4, backgroundColor: botPal.soft }}
                    >
                      <Text style={{ fontSize: 13, fontWeight: '700', color: botPal.accent }}>
                        {botMemShowAdd ? t('collapse') : t('addShort')}
                      </Text>
                    </Pressable>
                  </View>
                  {botMemShowAdd && (
                    <View style={{ gap: 8, backgroundColor: theme.s1, borderRadius: 14, padding: 10 }}>
                      <TextInput
                        value={botMemText}
                        onChangeText={setBotMemText}
                        placeholder={t('botMemPh')}
                        placeholderTextColor={theme.mute}
                        multiline
                        textAlignVertical="top"
                        style={[styles.editInput, { minHeight: 72, backgroundColor: theme.card, color: theme.text }]}
                      />
                      <Pressable
                        disabled={botMemSaving}
                        onPress={async () => {
                          const txt = botMemText.trim();
                          if (!txt) return;
                          setBotMemSaving(true);
                          try {
                            await saveMemory({ scope: 'project', botId, kind: 'fact', text: txt });
                            setBotMemText('');
                            setBotMemShowAdd(false);
                            await loadBotMemories();
                          } catch (err: any) {
                            setEditErr(err?.message || t('saveBotMemFailed'));
                          } finally {
                            setBotMemSaving(false);
                          }
                        }}
                        style={{ borderRadius: 999, paddingVertical: 9, alignItems: 'center', backgroundColor: botPal.accent }}
                      >
                        <Text style={{ fontSize: 15, fontWeight: '700', color: botPal.text }}>
                          {botMemSaving ? t('saving') : t('saveMemory')}
                        </Text>
                      </Pressable>
                    </View>
                  )}
                  {botMemLoading ? (
                    <ActivityIndicator size="small" color={botPal.accent} />
                  ) : !botMem?.installed ? (
                    <Text style={[styles.instructionsTxt, { color: theme.mute }]}>{t('hostNoMagpie')}</Text>
                  ) : !botMem.available ? (
                    <Text style={[styles.instructionsTxt, { color: theme.danger }]}>{t('magpieNoResp')}</Text>
                  ) : (botMem.items || []).length === 0 ? (
                    <Text style={[styles.instructionsTxt, { color: theme.mute }]}>{t('noBotMemories')}</Text>
                  ) : (
                    <View style={{ gap: 8 }}>
                      {botMem.items.map((it) => (
                        <View key={it.id} style={{ backgroundColor: theme.s1, borderRadius: 14, padding: 10, gap: 6 }}>
                          <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
                            <Text style={{ fontSize: 12.5, fontWeight: '700', color: theme.sageInk }}>
                              {it.kind || 'memory'}
                            </Text>
                            <Pressable
                              onPress={() => {
                                confirmAction(
                                  t('deleteMemTitle'),
                                  t('deleteMemConfirm'),
                                  async () => {
                                    await deleteMemory(it.id);
                                    await loadBotMemories();
                                  },
                                  true
                                );
                              }}
                              style={{ borderRadius: 999, paddingHorizontal: 8, paddingVertical: 3, backgroundColor: theme.card }}
                            >
                              <Text style={{ fontSize: 12.5, fontWeight: '700', color: theme.danger }}>{t('delete')}</Text>
                            </Pressable>
                          </View>
                          <Text selectable style={[styles.instructionsTxt, { color: theme.text }]}>
                            {it.text}
                          </Text>
                        </View>
                      ))}
                    </View>
                  )}
                </View>

                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <Pressable
                    onPress={() => setTechExpanded((v) => !v)}
                    style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}
                  >
                    <Text style={[styles.sectionLabel, { color: theme.text2 }]}>{t('techDetails')}</Text>
                    <Text style={{ fontSize: 13, fontWeight: '700', color: theme.mute }}>
                      {techExpanded ? t('collapse') : '▾'}
                    </Text>
                  </Pressable>
                  {techExpanded && (
                    <View style={{ gap: 12, paddingTop: 4 }}>
                      <View style={styles.statGrid}>
                        <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                          <Text style={[styles.statLbl, { color: theme.mute }]}>{t('statHarness')}</Text>
                          <Text style={[styles.statVal, { color: theme.text }]}>
                            {activeDetail?.harness || '-'}
                          </Text>
                        </View>
                        <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                          <Text style={[styles.statLbl, { color: theme.mute }]}>{t('statModel')}</Text>
                          <Text numberOfLines={1} style={[styles.statVal, { color: theme.text }]}>
                            {activeDetail?.model || t('defaultTag')}
                          </Text>
                        </View>
                        <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                          <Text style={[styles.statLbl, { color: theme.mute }]}>{t('statArchived')}</Text>
                          <Text numberOfLines={1} style={[styles.statVal, { color: theme.text }]}>
                            {t('archivedCount', { n: activeDetail?.archivedSessions || 0 }) +
                              (activeDetail?.privateSessions ? t('privateTag') : '')}
                          </Text>
                        </View>
                      </View>
                      <View style={{ gap: 4 }}>
                        <Text style={[styles.sectionLabel, { color: theme.mute }]}>{t('workspaceDir')}</Text>
                        <Text selectable style={[styles.monoBlock, { color: theme.text2, fontFamily: MONO_FONT }]}>
                          {activeDetail?.workspace || '-'}
                        </Text>
                      </View>
                      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingTop: 4 }}>
                        <Text style={[styles.instructionsTxt, { color: theme.text, flex: 1 }]}>{t('privateSessions')}</Text>
                        <Switch
                          value={editPrivate}
                          onValueChange={setEditPrivate}
                          trackColor={{ false: theme.s2, true: botPal.accent }}
                        />
                      </View>
                      <View style={{ gap: 4 }}>
                        <Text style={[styles.sectionLabel, { color: theme.mute }]}>AGENTS.md</Text>
                        {detailLoading ? (
                          <ActivityIndicator size="small" color={botPal.accent} />
                        ) : (
                          <TextInput
                            value={editInst}
                            onChangeText={setEditInst}
                            placeholder={t('agentsPh')}
                            placeholderTextColor={theme.mute}
                            multiline
                            textAlignVertical="top"
                            style={[
                              styles.editInput,
                              styles.editTextarea,
                              { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT },
                            ]}
                          />
                        )}
                      </View>
                    </View>
                  )}
                </View>

                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
                    <Text style={[styles.sectionLabel, { color: theme.mute }]}>{t('routines')}</Text>
                    <Pressable
                      onPress={() => {
                        if (rtShowForm && !rtEditId) setRtShowForm(false);
                        else openRoutineEditor();
                      }}
                      style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4, backgroundColor: theme.accentSoft }}
                    >
                      <Text style={{ fontSize: 13, fontWeight: '700', color: theme.accent }}>
                        {rtShowForm && !rtEditId ? t('collapse') : t('addShort')}
                      </Text>
                    </Pressable>
                  </View>

                  {rtShowForm && (
                    <View style={{ gap: 8, backgroundColor: theme.s1, borderRadius: 14, padding: 10 }}>
                      <TextInput
                        value={rtName}
                        onChangeText={setRtName}
                        placeholder={t('rtNamePh')}
                        placeholderTextColor={theme.mute}
                        style={[styles.editInput, { backgroundColor: theme.card, color: theme.text }]}
                      />
                      <View style={{ flexDirection: 'row', gap: 6 }}>
                        {(['interval', 'daily', 'cron', 'webhook'] as const).map((m) => (
                          <Pressable
                            key={m}
                            onPress={() => {
                              setRtMode(m);
                              if (m === 'webhook') setRtWebhookTrigger(true);
                            }}
                            style={{
                              flex: 1,
                              borderRadius: 999,
                              paddingVertical: 7,
                              alignItems: 'center',
                              backgroundColor: rtMode === m ? theme.accent : theme.card,
                            }}
                          >
                            <Text style={{ fontSize: 13, fontWeight: '700', color: rtMode === m ? theme.accentText : theme.text2 }}>
                              {m === 'interval'
                                ? t('rtModeInterval')
                                : m === 'daily'
                                  ? t('rtModeDaily')
                                  : m === 'cron'
                                    ? t('rtModeCron')
                                    : t('rtModeWebhook')}
                            </Text>
                          </Pressable>
                        ))}
                      </View>
                      {rtMode === 'interval' ? (
                        <TextInput
                          value={rtEvery}
                          onChangeText={setRtEvery}
                          keyboardType="number-pad"
                          placeholder={t('rtEveryPh')}
                          placeholderTextColor={theme.mute}
                          style={[styles.editInput, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                        />
                      ) : rtMode === 'daily' ? (
                        <TextInput
                          value={rtDaily}
                          onChangeText={setRtDaily}
                          placeholder={t('rtDailyPh')}
                          placeholderTextColor={theme.mute}
                          style={[styles.editInput, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                        />
                      ) : rtMode === 'cron' ? (
                        <TextInput
                          value={rtCron}
                          onChangeText={setRtCron}
                          placeholder={t('rtCronPh')}
                          placeholderTextColor={theme.mute}
                          autoCapitalize="none"
                          style={[styles.editInput, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                        />
                      ) : null}
                      {rtMode !== 'webhook' && (
                        <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
                          <Text style={{ fontSize: 15, color: theme.text }}>{t('rtAllowWebhook')}</Text>
                          <Switch
                            value={rtWebhookTrigger}
                            onValueChange={setRtWebhookTrigger}
                            trackColor={{ false: theme.s2, true: theme.accent }}
                          />
                        </View>
                      )}
                      {(rtMode === 'webhook' || rtWebhookTrigger) && (
                        <>
                          <TextInput
                            value={rtWebhookSecret}
                            onChangeText={setRtWebhookSecret}
                            placeholder={rtWebhookSecretSet && !rtClearSecret ? t('rtWebhookSecretSetPh') : t('rtWebhookSecretPh')}
                            placeholderTextColor={theme.mute}
                            secureTextEntry
                            editable={!rtClearSecret}
                            autoCapitalize="none"
                            style={[styles.editInput, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                          />
                          {rtWebhookSecretSet && (
                            <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
                              <Text style={{ fontSize: 13.5, color: theme.text2 }}>{t('rtWebhookClearSecret')}</Text>
                              <Switch
                                value={rtClearSecret}
                                onValueChange={(v) => {
                                  setRtClearSecret(v);
                                  if (v) setRtWebhookSecret('');
                                }}
                                trackColor={{ false: theme.s2, true: theme.danger }}
                              />
                            </View>
                          )}
                        </>
                      )}
                      <TextInput
                        value={rtWebhookUrl}
                        onChangeText={setRtWebhookUrl}
                        placeholder={t('rtWebhookUrlPh')}
                        placeholderTextColor={theme.mute}
                        autoCapitalize="none"
                        keyboardType="url"
                        style={[styles.editInput, { backgroundColor: theme.card, color: theme.text, fontFamily: MONO_FONT }]}
                      />
                      <View style={{ flexDirection: 'row', gap: 6 }}>
                        {(
                          [
                            ['receipt', t('rtDeliveryReceipt')],
                            ['notify', t('rtDeliveryNotify')],
                            ['none', t('rtDeliveryNone')],
                          ] as const
                        ).map(([val, lbl]) => (
                          <Pressable
                            key={val}
                            onPress={() => setRtDelivery(val)}
                            style={{
                              flex: 1,
                              borderRadius: 999,
                              paddingVertical: 7,
                              alignItems: 'center',
                              backgroundColor: rtDelivery === val ? theme.accent : theme.card,
                            }}
                          >
                            <Text style={{ fontSize: 13, fontWeight: '700', color: rtDelivery === val ? theme.accentText : theme.text2 }}>
                              {lbl}
                            </Text>
                          </Pressable>
                        ))}
                      </View>
                      <TextInput
                        value={rtPrompt}
                        onChangeText={setRtPrompt}
                        placeholder={t('rtPromptPh')}
                        placeholderTextColor={theme.mute}
                        multiline
                        textAlignVertical="top"
                        style={[styles.editInput, { minHeight: 72, backgroundColor: theme.card, color: theme.text }]}
                      />
                      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' }}>
                        <Text style={{ fontSize: 15, color: theme.text }}>{t('rtEnabled')}</Text>
                        <Switch
                          value={rtEnabled}
                          onValueChange={setRtEnabled}
                          trackColor={{ false: theme.s2, true: theme.accent }}
                        />
                      </View>
                      <View style={{ flexDirection: 'row', gap: 8 }}>
                        <Pressable
                          onPress={() => {
                            setRtShowForm(false);
                            setRtErr('');
                          }}
                          style={{ flex: 1, borderRadius: 999, paddingVertical: 9, alignItems: 'center', backgroundColor: theme.card }}
                        >
                          <Text style={{ fontSize: 15, fontWeight: '700', color: theme.text2 }}>{t('cancel')}</Text>
                        </Pressable>
                        <Pressable
                          disabled={rtSaving}
                          onPress={() => void onSaveRoutine()}
                          style={{ flex: 1, borderRadius: 999, paddingVertical: 9, alignItems: 'center', backgroundColor: theme.accent }}
                        >
                          <Text style={{ fontSize: 15, fontWeight: '700', color: theme.accentText }}>
                            {rtSaving ? t('saving') : t('rtSaveBtn')}
                          </Text>
                        </Pressable>
                      </View>
                    </View>
                  )}

                  {rtErr ? (
                    <Text style={{ fontSize: 13.5, color: theme.danger }}>{rtErr}</Text>
                  ) : null}

                  {rtLoading && botRoutines.length === 0 ? (
                    <ActivityIndicator size="small" color={theme.accent} />
                  ) : botRoutines.length === 0 ? (
                    <Text style={[styles.instructionsTxt, { color: theme.mute }]}>{t('rtEmpty')}</Text>
                  ) : (
                    <View style={{ gap: 8 }}>
                      {botRoutines.map((rt) => {
                        const trig = [
                          rt.everyMinutes && rt.everyMinutes > 0 ? t('rtEveryShort', { n: rt.everyMinutes }) : '',
                          rt.dailyAt ? t('rtDailyShort', { time: rt.dailyAt }) : '',
                          rt.cron ? `Cron ${rt.cron}` : '',
                          rt.webhookTrigger ? t('rtWebhookTag') : '',
                        ]
                          .filter(Boolean)
                          .join(' · ');
                        const delivLbl =
                          rt.mainDelivery === 'notify'
                            ? t('rtDeliveryNotify')
                            : rt.mainDelivery === 'none'
                              ? t('rtDeliveryNone')
                              : t('rtDeliveryReceipt');
                        const statusTxt = rt.running
                          ? t('rtStatusRunning')
                          : rt.lastRun?.status === 'ok'
                            ? t('rtStatusOk')
                            : rt.lastRun?.status === 'silent'
                              ? t('rtStatusSilent')
                              : rt.lastRun?.status === 'error'
                                ? t('rtStatusError')
                                : '';
                        const runsOpen = rtRunsOpenId === rt.id;
                        const runList = rtRunsMap[rt.id] || [];
                        return (
                          <View key={rt.id} style={{ backgroundColor: theme.s1, borderRadius: 14, padding: 10, gap: 6 }}>
                            <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
                              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6, flex: 1 }}>
                                <Text style={{ fontSize: 16, fontWeight: '700', color: theme.text }} numberOfLines={1}>
                                  {rt.name}
                                </Text>
                                {statusTxt ? (
                                  <Text
                                    style={{
                                      fontSize: 12.5,
                                      fontWeight: '700',
                                      color: rt.lastRun?.status === 'error' && !rt.running ? theme.danger : theme.sageInk,
                                    }}
                                  >
                                    {statusTxt}
                                  </Text>
                                ) : null}
                              </View>
                              <Switch
                                value={rt.enabled}
                                onValueChange={(v) => {
                                  void updateRoutine(botId, rt.id, { enabled: v }).catch((err: any) =>
                                    setRtErr(err?.message || t('rtStatusUpdateFailed'))
                                  );
                                }}
                                trackColor={{ false: theme.s2, true: theme.accent }}
                              />
                            </View>
                            <Text style={{ fontSize: 13, color: theme.mute }}>
                              {[trig, delivLbl, rt.webhookUrl ? t('rtWebhookPushTag') : '', rt.model].filter(Boolean).join(' · ')}
                            </Text>
                            <Text selectable style={[styles.instructionsTxt, { color: theme.text }]}>
                              {rt.prompt}
                            </Text>
                            <View style={{ flexDirection: 'row', gap: 6, flexWrap: 'wrap' }}>
                              <Pressable
                                disabled={rt.running}
                                onPress={() => {
                                  setRtErr('');
                                  void runRoutine(botId, rt.id).catch((err: any) => setRtErr(err?.message || t('rtRunFailed')));
                                }}
                                style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4, backgroundColor: theme.card }}
                              >
                                <Text style={{ fontSize: 13, fontWeight: '700', color: theme.text }}>
                                  {rt.running ? t('rtRunningBtn') : t('rtRunNow')}
                                </Text>
                              </Pressable>
                              <Pressable
                                onPress={async () => {
                                  if (runsOpen) {
                                    setRtRunsOpenId('');
                                    return;
                                  }
                                  setRtRunsOpenId(rt.id);
                                  try {
                                    const list = await fetchRoutineRuns(botId, rt.id, 6);
                                    setRtRunsMap((prev) => ({ ...prev, [rt.id]: list }));
                                  } catch {
                                    // ignore
                                  }
                                }}
                                style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4, backgroundColor: theme.card }}
                              >
                                <Text style={{ fontSize: 13, fontWeight: '700', color: theme.text2 }}>
                                  {runsOpen ? t('rtRunsHide') : t('rtRunsBtn')}
                                </Text>
                              </Pressable>
                              <Pressable
                                onPress={() => openRoutineEditor(rt)}
                                style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4, backgroundColor: theme.card }}
                              >
                                <Text style={{ fontSize: 13, fontWeight: '700', color: theme.text2 }}>{t('edit')}</Text>
                              </Pressable>
                              {rt.webhookTrigger && rt.webhookPath ? (
                                <Pressable
                                  onPress={() => {
                                    const full = `${baseURL.replace(/\/$/, '')}${rt.webhookPath}`;
                                    void Clipboard.setStringAsync(full).then(() => {
                                      setRtCopiedId(rt.id);
                                      setTimeout(() => setRtCopiedId((cur) => (cur === rt.id ? '' : cur)), 1500);
                                    });
                                  }}
                                  style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4, backgroundColor: theme.card }}
                                >
                                  <Text style={{ fontSize: 13, fontWeight: '700', color: theme.accent }}>
                                    {rtCopiedId === rt.id ? t('rtWebhookCopied') : t('rtCopyWebhook')}
                                  </Text>
                                </Pressable>
                              ) : null}
                              <Pressable
                                onPress={() => {
                                  confirmAction(
                                    t('rtDeleteTitle', { name: rt.name }),
                                    t('rtDeleteDesc'),
                                    async () => {
                                      await deleteRoutine(botId, rt.id);
                                    },
                                    true
                                  );
                                }}
                                style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 4, backgroundColor: theme.card }}
                              >
                                <Text style={{ fontSize: 13, fontWeight: '700', color: theme.danger }}>{t('delete')}</Text>
                              </Pressable>
                            </View>
                            {runsOpen && (
                              <View style={{ gap: 4, paddingTop: 4 }}>
                                {runList.length === 0 ? (
                                  <Text style={{ fontSize: 13, color: theme.mute }}>{t('rtRunsEmpty')}</Text>
                                ) : (
                                  runList.map((r) => (
                                    <View key={r.id} style={{ backgroundColor: theme.card, borderRadius: 8, padding: 6, gap: 2 }}>
                                      <Text style={{ fontSize: 12.5, color: theme.mute }}>
                                        {`${r.status === 'ok' ? t('rtStatusOk') : r.status === 'silent' ? t('rtStatusSilent') : t('rtStatusError')} · ${r.trigger} · ${((r.durationMs || 0) / 1000).toFixed(1)}s`}
                                      </Text>
                                      {r.error ? (
                                        <Text style={{ fontSize: 13, color: theme.danger }}>{r.error}</Text>
                                      ) : r.output ? (
                                        <Text numberOfLines={3} style={{ fontSize: 13, color: theme.text2 }}>
                                          {r.output}
                                        </Text>
                                      ) : null}
                                    </View>
                                  ))
                                )}
                              </View>
                            )}
                          </View>
                        );
                      })}
                    </View>
                  )}
                </View>

                <Pressable
                  onPress={handleResetConfirm}
                  style={[styles.dangerBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.dangerBtnTxt, { color: theme.text }]}>{t('resetSessionBtn')}</Text>
                </Pressable>

                {isDirty ? (
                  <Pressable
                    disabled={editSaving || !editName.trim()}
                    onPress={() => void onSaveEdit()}
                    style={[styles.dangerBtn, { backgroundColor: botPal.accent, opacity: editSaving || !editName.trim() ? 0.5 : 1 }]}
                  >
                    <Text style={[styles.dangerBtnTxt, { color: botPal.text }]}>
                      {editSaving ? t('saving') : t('save')}
                    </Text>
                  </Pressable>
                ) : (
                  bots.length > 1 && (
                    <Pressable
                      onPress={handleDeleteConfirm}
                      style={[styles.dangerBtn, { backgroundColor: theme.pink }]}
                    >
                      <Text style={[styles.dangerBtnTxt, { color: theme.danger }]}>{t('deleteBotBtn')}</Text>
                    </Pressable>
                  )
                )}
              </>
          </ScrollView>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: { flex: 1 },
  flex: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  backBtn: {
    width: 34,
    height: 36,
    alignItems: 'center',
    justifyContent: 'center',
  },
  headInfo: {
    flex: 1,
    minWidth: 0,
    justifyContent: 'center',
  },
  headName: {
    fontSize: 17,
    fontWeight: '700',
  },
  docBtn: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
  },
  listContent: {
    paddingHorizontal: 14,
    paddingTop: 14,
    paddingBottom: 16,
  },
  emptyGrow: {
    flexGrow: 1,
    justifyContent: 'center',
  },
  emptyWrap: {
    alignItems: 'center',
    paddingVertical: 24,
    paddingHorizontal: 8,
    gap: 10,
  },
  emptyTitle: {
    fontSize: 22,
    fontWeight: '700',
    letterSpacing: -0.4,
    marginTop: 4,
  },
  emptyDesc: {
    fontSize: 16,
    textAlign: 'center',
    lineHeight: 22,
    marginBottom: 8,
  },
  suggestGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 10,
    width: '100%',
  },
  suggestCard: {
    width: '48%',
    flexGrow: 1,
    borderRadius: 20,
    padding: 14,
    gap: 4,
  },
  suggestTitle: {
    fontSize: 16,
    fontWeight: '700',
  },
  suggestSub: {
    fontSize: 13.5,
  },
  stamp: {
    fontSize: 12.5,
    textAlign: 'center',
    marginTop: 4,
    marginBottom: 8,
  },
  userRow: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
  },
  userBubble: {
    maxWidth: '82%',
    borderRadius: 18,
    borderBottomRightRadius: 6,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  userText: {
    fontSize: 17,
    lineHeight: 25,
  },
  assistantRow: {
    flexDirection: 'row',
    justifyContent: 'flex-start',
  },
  assistantBubble: {
    maxWidth: '88%',
    borderRadius: 18,
    borderBottomLeftRadius: 6,
    paddingHorizontal: 14,
    paddingVertical: 11,
    gap: 6,
  },
  msgMeta: {
    fontSize: 12.5,
    fontWeight: '500',
  },
  botRelBubble: {
    maxWidth: '88%',
    borderRadius: 18,
    paddingHorizontal: 14,
    paddingVertical: 11,
    gap: 6,
  },
  botRelLabel: {
    fontSize: 13,
    fontWeight: '700',
  },
  systemWrap: {
    alignItems: 'center',
  },
  systemCard: {
    maxWidth: '94%',
    borderRadius: 14,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  systemTxt: {
    fontSize: 15,
    lineHeight: 21,
  },
  busyFoot: {
    alignSelf: 'flex-start',
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    marginTop: 12,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 999,
  },
  busyFootTxt: {
    fontSize: 15,
    fontWeight: '600',
  },
  errBanner: {
    marginHorizontal: 14,
    marginBottom: 6,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 14,
  },
  errTxt: {
    fontSize: 15,
  },
  composerWrap: {
    paddingHorizontal: 12,
    paddingTop: 6,
    gap: 8,
  },
  slashRow: {
    gap: 8,
    paddingHorizontal: 2,
  },
  slashPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 12,
    paddingVertical: 7,
    borderRadius: 999,
  },
  slashCmd: {
    fontSize: 15,
    fontWeight: '700',
  },
  slashLbl: {
    fontSize: 13.5,
  },
  composerCapsule: {
    borderRadius: 24,
    paddingLeft: 16,
    paddingRight: 6,
    paddingVertical: 6,
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 8,
  },
  composerInput: {
    flex: 1,
    fontSize: 17,
    lineHeight: 25,
    maxHeight: 120,
    minHeight: 36,
    outlineWidth: 0,
    paddingTop: Platform.OS === 'ios' ? 7 : 6,
    paddingBottom: Platform.OS === 'ios' ? 7 : 6,
  },
  sendBtn: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
  },
  modalRoot: {
    flex: 1,
  },
  detailScroll: {
    padding: 16,
    gap: 12,
    paddingBottom: 36,
  },
  detailCard: {
    borderRadius: 20,
    padding: 16,
    gap: 12,
  },
  detailTop: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  detailTitleCol: {
    flex: 1,
    gap: 2,
  },
  detailName: {
    fontSize: 19,
    fontWeight: '700',
  },
  detailId: {
    fontSize: 15,
  },
  detailDesc: {
    fontSize: 16,
    lineHeight: 22,
  },
  statGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
  },
  statPill: {
    minWidth: '46%',
    flexGrow: 1,
    borderRadius: 14,
    paddingHorizontal: 12,
    paddingVertical: 9,
    gap: 2,
  },
  statLbl: {
    fontSize: 12.5,
    fontWeight: '600',
  },
  statVal: {
    fontSize: 16,
    fontWeight: '700',
  },
  sectionLabel: {
    fontSize: 13,
    fontWeight: '700',
  },
  monoBlock: {
    fontSize: 13.5,
    lineHeight: 20,
  },
  instructionsTxt: {
    fontSize: 16,
    lineHeight: 24,
  },
  dangerBtn: {
    borderRadius: 999,
    paddingVertical: 14,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 4,
  },
  dangerBtnTxt: {
    fontSize: 17,
    fontWeight: '700',
  },
  editInput: {
    borderRadius: 14,
    paddingHorizontal: 13,
    paddingVertical: 10,
    fontSize: 17,
  },
  editTextarea: {
    minHeight: 200,
    fontSize: 15,
    lineHeight: 21,
  },
});
