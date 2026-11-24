import * as Clipboard from 'expo-clipboard';
import * as DocumentPicker from 'expo-document-picker';
import * as Haptics from 'expo-haptics';
import { router, useLocalSearchParams } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Image,
  KeyboardAvoidingView,
  Linking,
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

import { BotAppearancePicker } from '../../components/BotAppearancePicker';
import { BotAvatar } from '../../components/BotAvatar';
import { ChevronLeftIcon, DocIcon, FolderIcon, MonitorIcon, PaperclipIcon, SendIcon, StopIcon } from '../../components/Icons';
import { MarkdownText } from '../../components/MarkdownText';
import { ModalHeader } from '../../components/ModalHeader';
import { QuestionCard } from '../../components/QuestionCard';
import {
  fetchBotCommands,
  fetchBotMentions,
  fetchCollabMessages,
  getMediaSource,
  requestMediaDownloadURL,
  uploadBotAttachment,
} from '../../api';
import { confirmAction } from '../../confirm';
import { useI18n } from '../../i18n';
import { countToolCalls, formatClockTime, formatDurationMs, formatStamp, useHub } from '../../store';
import { getBotPalette, MONO_FONT, useTheme } from '../../theme';
import type {
  BotDetail,
  ChatMessage,
  MagpieMemoryList,
  MentionCandidate,
  MessageMedia,
  PendingAttachment,
  RoutineDelivery,
  RoutineItem,
  RoutineRun,
  SlashCommandItem,
} from '../../types';

const FIVE_MIN_MS = 5 * 60 * 1000;

type TimelineEntry =
  | { kind: 'msg'; id: string; msg: ChatMessage; createdAt: string }
  | { kind: 'collab'; id: string; peerId: string; items: ChatMessage[]; createdAt: string };

export default function BotChatScreen() {
  const { id } = useLocalSearchParams<{ id: string | string[] }>();
  const botId = useMemo(() => decodeURIComponent(Array.isArray(id) ? id[0] || '' : id || ''), [id]);

  const theme = useTheme();
  const { t, lang } = useI18n();
  const insets = useSafeAreaInsets();
  const {
    baseURL,
    authToken,
    bots,
    msgs,
    tools,
    takeovers,
    activeDesktopTarget,
    pendingStageOpen,
    usingStandby,
    questions,
    setActiveChatBot,
    setActiveDesktopTarget,
    reconnect,
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
    installMagpie,
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
  const chatList = msgs[botId] || [];
  const botQuestions = useMemo(() => questions.filter((q) => q.botId === botId), [questions, botId]);

  const [input, setInput] = useState('');
  const [commands, setCommands] = useState<SlashCommandItem[]>([]);
  const [mentions, setMentions] = useState<MentionCandidate[]>([]);
  const [acDismissed, setAcDismissed] = useState(false);
  const [pendingAttachments, setPendingAttachments] = useState<PendingAttachment[]>([]);

  useEffect(() => {
    if (!pendingStageOpen) return;
    const targetBot = pendingStageOpen.botId || botId;
    if (pendingStageOpen.kind === 'files') {
      const qs = new URLSearchParams({ tab: 'files', botId: targetBot });
      if (pendingStageOpen.path) qs.set('path', pendingStageOpen.path);
      router.push(`/desktop?${qs.toString()}`);
    } else if (pendingStageOpen.kind === 'desktop') {
      router.push(`/desktop?tab=desktop&botId=${encodeURIComponent(targetBot)}`);
    }
  }, [pendingStageOpen?.seq]);

  useEffect(() => {
    setPendingAttachments([]);
    setMentions([]);
    setAcDismissed(false);
    if (!botId || !baseURL) return;
    let cancelled = false;
    fetchBotCommands(baseURL, authToken, botId)
      .then((list) => {
        if (!cancelled) setCommands(list);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [botId, baseURL, authToken]);

  const slashQuery = useMemo(() => {
    if (acDismissed || !input.startsWith('/')) return null;
    const rest = input.slice(1);
    if (/\s/.test(rest)) return null;
    return rest;
  }, [input, acDismissed]);

  const filteredCommands = useMemo(() => {
    if (slashQuery === null) return [];
    const q = slashQuery.toLowerCase();
    return commands.filter(
      (c) => !q || c.name.toLowerCase().includes(q) || (c.description && c.description.toLowerCase().includes(q))
    );
  }, [slashQuery, commands]);

  const activeMention = useMemo(() => {
    if (acDismissed || slashQuery !== null) return null;
    const m = /(?:^|\s)@([^\s]*)$/.exec(input);
    if (!m) return null;
    return {
      query: m[1],
      start: input.length - m[1].length - 1,
    };
  }, [input, acDismissed, slashQuery]);

  useEffect(() => {
    if (!activeMention || !botId || !baseURL) {
      setMentions([]);
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      fetchBotMentions(baseURL, authToken, botId, activeMention.query)
        .then((items) => {
          if (!cancelled) setMentions(items);
        })
        .catch(() => {
          if (!cancelled) setMentions([]);
        });
    }, 120);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [activeMention, botId, baseURL, authToken]);

  const [sending, setSending] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detail, setDetail] = useState<BotDetail | null>(null);
  const [techExpanded, setTechExpanded] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editAvatar, setEditAvatar] = useState('');
  const [editColor, setEditColor] = useState('');
  const [appearOpen, setAppearOpen] = useState(false);
  const [editInst, setEditInst] = useState('');
  const [editPrivate, setEditPrivate] = useState(false);
  const [editSaving, setEditSaving] = useState(false);
  const [editErr, setEditErr] = useState('');
  const [sendErr, setSendErr] = useState('');
  const [collabPeerId, setCollabPeerId] = useState<string | null>(null);
  const [collabHistory, setCollabHistory] = useState<ChatMessage[]>([]);
  const [collabLoading, setCollabLoading] = useState(false);
  const [previewImg, setPreviewImg] = useState<MessageMedia | null>(null);
  const botPal = useMemo(
    () => getBotPalette(botId, botIndex, theme.dark, detailOpen ? editColor || bot?.color : bot?.color),
    [botId, botIndex, theme.dark, detailOpen, editColor, bot?.color]
  );

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

  const listRef = useRef<FlatList<TimelineEntry>>(null);
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
    setEditAvatar(initSrc?.avatar || '');
    setEditColor(initSrc?.color || '');
    setAppearOpen(false);
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
      setEditAvatar(d.avatar || '');
      setEditColor(d.color || '');
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
        avatar: editAvatar,
        color: editColor,
        instructions: editInst,
        privateSessions: editPrivate,
      });
      const fresh = await getBotDetail(botId);
      setDetail(fresh);
      setEditName(fresh.name || botId);
      setEditDesc(fresh.description || '');
      setEditAvatar(fresh.avatar || '');
      setEditColor(fresh.color || '');
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
    async (raw: string, restoreOnError = false, mediaIds: string[] = []) => {
      const trimmed = raw.trim();
      if ((!trimmed && mediaIds.length === 0) || !botId) return;
      setSendErr('');
      setSending(true);
      if (Platform.OS !== 'web') {
        Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
      }
      try {
        await sendMessage(botId, trimmed, mediaIds);
      } catch (err: any) {
        if (restoreOnError) setInput(raw);
        setSendErr(err?.message || t('sendFailed'));
      } finally {
        setSending(false);
      }
    },
    [botId, sendMessage, t]
  );

  const readyMediaIds = useMemo(
    () => pendingAttachments.filter((a) => !a.uploading && !a.error && a.media?.id).map((a) => a.media!.id),
    [pendingAttachments]
  );
  const hasUploading = useMemo(() => pendingAttachments.some((a) => a.uploading), [pendingAttachments]);

  const onSubmitComposer = () => {
    const text = input;
    if ((!text.trim() && readyMediaIds.length === 0) || hasUploading || sending) return;
    const mediaIds = [...readyMediaIds];
    setInput('');
    setPendingAttachments([]);
    setAcDismissed(false);
    void handleSendText(text, true, mediaIds);
  };

  const onPickAttachment = async () => {
    if (!botId || !baseURL) return;
    try {
      const res = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
      if (res.canceled || !res.assets) return;
      for (const asset of res.assets) {
        const localId = `tmp_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`;
        const isImg = Boolean(asset.mimeType && asset.mimeType.startsWith('image/'));
        setPendingAttachments((prev) => [
          ...prev,
          { id: localId, name: asset.name || 'file', kind: isImg ? 'image' : 'file', uri: asset.uri, uploading: true },
        ]);
        void uploadBotAttachment(baseURL, authToken, botId, {
          uri: asset.uri,
          name: asset.name || 'file',
          mimeType: asset.mimeType,
        })
          .then((uploaded) => {
            setPendingAttachments((prev) =>
              prev.map((a) =>
                a.id === localId
                  ? {
                      ...a,
                      media: uploaded.media,
                      name: uploaded.media.name || a.name,
                      kind: uploaded.media.kind === 'image' ? 'image' : 'file',
                      uploading: false,
                    }
                  : a
              )
            );
          })
          .catch((err) => {
            setPendingAttachments((prev) =>
              prev.map((a) =>
                a.id === localId
                  ? { ...a, uploading: false, error: err?.message || t('uploadFailed') }
                  : a
              )
            );
          });
      }
    } catch (err: any) {
      setSendErr(err?.message || t('uploadFailed'));
    }
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

  const onPickSlash = (cmd: SlashCommandItem) => {
    const immediate = new Set(['status', 'bots', 'reset', 'compact', 'clear', 'cost', 'context']);
    const nextText = immediate.has(cmd.name) && !cmd.argumentHint ? `/${cmd.name}` : `/${cmd.name} `;
    setAcDismissed(true);
    setInput(nextText);
    inputRef.current?.focus();
  };

  const onPickMention = (item: MentionCandidate) => {
    if (!activeMention) return;
    const nextText = input.slice(0, activeMention.start) + `@${item.value} `;
    setAcDismissed(true);
    setInput(nextText);
    inputRef.current?.focus();
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

  useEffect(() => {
    if (!collabPeerId || !botId || !baseURL) return;
    let cancelled = false;
    setCollabLoading(true);
    fetchCollabMessages<ChatMessage>(baseURL, authToken, botId, collabPeerId, 200)
      .then((res) => {
        if (!cancelled) setCollabHistory(res);
      })
      .catch(() => {
        if (!cancelled) setCollabHistory([]);
      })
      .finally(() => {
        if (!cancelled) setCollabLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [collabPeerId, botId, baseURL, authToken]);

  const collabItems = useMemo(() => {
    if (!collabPeerId) return [];
    const peerTag = `bot:${collabPeerId}`;
    const map = new Map<string, ChatMessage>();
    for (const m of collabHistory) map.set(m.id, m);
    for (const m of chatList) {
      if ((m.role === 'bot_out' || m.role === 'bot_in') && (m.from === peerTag || m.to === peerTag)) {
        map.set(m.id, m);
      }
    }
    return Array.from(map.values()).sort((a, b) => (Date.parse(a.createdAt) || 0) - (Date.parse(b.createdAt) || 0));
  }, [collabHistory, chatList, collabPeerId]);

  const timelineEntries = useMemo<TimelineEntry[]>(() => {
    const out: TimelineEntry[] = [];
    for (const m of chatList) {
      if (m.role === 'bot_out' || m.role === 'bot_in') {
        const rawPeer = m.role === 'bot_out' ? m.to || '' : m.from || '';
        const peerId = rawPeer.replace(/^bot:/, '');
        const last = out[out.length - 1];
        if (last && last.kind === 'collab' && last.peerId === peerId) {
          last.items.push(m);
        } else {
          out.push({ kind: 'collab', id: `collab_${m.id}`, peerId, items: [m], createdAt: m.createdAt });
        }
      } else {
        out.push({ kind: 'msg', id: m.id, msg: m, createdAt: m.createdAt });
      }
    }
    return out;
  }, [chatList]);

  const isImageAttachment = (m: MessageMedia) => {
    if (m.kind === 'image') return true;
    if (m.kind === 'file') return false;
    return Boolean(m.mimeType && m.mimeType.startsWith('image/'));
  };

  const formatBytes = (bytes?: number) => {
    if (!bytes || bytes <= 0) return '';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  const onOpenFile = async (f: MessageMedia) => {
    try {
      const url = await requestMediaDownloadURL(baseURL, authToken, f);
      await Linking.openURL(url);
    } catch {
      setSendErr(t('downloadFailed'));
    }
  };

  const renderMediaGrid = (media?: MessageMedia[]) => {
    if (!media || media.length === 0) return null;
    const images = media.filter(isImageAttachment);
    const files = media.filter((m) => !isImageAttachment(m));
    return (
      <View style={styles.attachWrap}>
        {images.length > 0 && (
          <View style={styles.mediaGrid}>
            {images.map((img) => (
              <Pressable key={img.id} onPress={() => setPreviewImg(img)} style={styles.mediaItem}>
                <Image
                  source={getMediaSource(baseURL, authToken, img.url)}
                  style={images.length === 1 ? styles.mediaImgSingle : styles.mediaImgMulti}
                  resizeMode="cover"
                />
              </Pressable>
            ))}
          </View>
        )}
        {files.length > 0 && (
          <View style={styles.fileList}>
            {files.map((f) => {
              const name = f.name || f.id || t('fileAltFallback');
              const sz = formatBytes(f.size);
              return (
                <Pressable
                  key={f.id}
                  onPress={() => void onOpenFile(f)}
                  style={({ pressed }) => [
                    styles.fileCard,
                    { backgroundColor: pressed ? theme.s2 : theme.s1 },
                  ]}
                >
                  <DocIcon size={18} color={theme.text2} />
                  <View style={styles.fileMeta}>
                    <Text numberOfLines={1} style={[styles.fileName, { color: theme.text }]}>
                      {name}
                    </Text>
                    {sz ? (
                      <Text style={[styles.fileSize, { color: theme.mute }]}>{sz}</Text>
                    ) : null}
                  </View>
                </Pressable>
              );
            })}
          </View>
        )}
      </View>
    );
  };

  const renderTimelineEntry = ({ item: entry, index }: { item: TimelineEntry; index: number }) => {
    const prev = index > 0 ? timelineEntries[index - 1] : null;
    const curMs = Date.parse(entry.createdAt || '');
    const prevMs = prev ? Date.parse(prev.createdAt || '') : 0;
    const showStamp =
      !prev ||
      (!Number.isNaN(curMs) && !Number.isNaN(prevMs) && curMs - prevMs > FIVE_MIN_MS);

    let bubble: React.ReactNode = null;
    if (entry.kind === 'collab') {
      const peerIdx = bots.findIndex((b) => b.id === entry.peerId);
      const peerBot = peerIdx >= 0 ? bots[peerIdx] : undefined;
      const peerName = peerBot?.name || entry.peerId || 'Bot';
      bubble = (
        <View style={styles.assistantRow}>
          <Pressable
            onPress={() => entry.peerId && setCollabPeerId(entry.peerId)}
            style={({ pressed }) => [
              styles.collabChip,
              { backgroundColor: theme.lilac, opacity: pressed ? 0.85 : 1 },
            ]}
          >
            <BotAvatar
              id={entry.peerId}
              size={18}
              index={peerIdx >= 0 ? peerIdx : 0}
              avatar={peerBot?.avatar}
              color={peerBot?.color}
            />
            <Text style={[styles.collabChipTxt, { color: theme.lilacInk }]}>
              {t('collabWith', { peer: peerName })} · {t('collabCount', { count: entry.items.length })} ›
            </Text>
          </Pressable>
        </View>
      );
    } else {
      const item = entry.msg;
      if (item.role === 'user') {
        bubble = (
          <View style={styles.userRow}>
            <View style={[styles.userBubble, { backgroundColor: botPal.soft }]}>
              {item.text ? (
                <Text selectable style={[styles.userText, { color: theme.text }]}>
                  {item.text}
                </Text>
              ) : null}
              {renderMediaGrid(item.media)}
            </View>
          </View>
        );
      } else if (item.role === 'assistant') {
        const metaParts: string[] = [];
        const clock = formatClockTime(item.createdAt);
        if (clock) metaParts.push(clock);
        const toolCount = countToolCalls(item.tools);
        if (toolCount > 0) metaParts.push(t('msgToolCalls', { count: toolCount }));
        const durText = formatDurationMs(item.durationMs);
        if (durText) metaParts.push(t('msgDuration', { dur: durText }));
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
              {item.text ? <MarkdownText text={item.text} theme={theme} /> : null}
              {renderMediaGrid(item.media)}
              {metaParts.length > 0 && (
                <Text style={[styles.msgMeta, { color: theme.mute }]}>{metaParts.join(' · ')}</Text>
              )}
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
    }

    return (
      <View>
        {showStamp && (
          <Text style={[styles.stamp, { color: theme.mute }]}>{formatStamp(entry.createdAt, lang)}</Text>
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
        editAvatar !== (activeDetail.avatar || '') ||
        editColor !== (activeDetail.color || '') ||
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

        <BotAvatar id={botId} size={42} index={botIndex} avatar={bot?.avatar} color={bot?.color} busy={!!bot?.busy} ringColor={theme.page} />

        <View style={styles.headInfo}>
          <Text numberOfLines={1} style={[styles.headName, { color: theme.text }]}>
            {bot?.name || botId}
          </Text>
        </View>

        <Pressable
          accessibilityLabel={t('workspaceFiles')}
          onPress={() => router.push(`/desktop?tab=files&botId=${encodeURIComponent(botId)}`)}
          style={[styles.docBtn, { backgroundColor: theme.s1, marginRight: 6 }]}
          hitSlop={8}
        >
          <FolderIcon size={18} color={theme.text2} />
        </Pressable>
        <Pressable
          accessibilityLabel={t('remoteDesktop')}
          onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0]?.target || 'local')}&botId=${encodeURIComponent(botId)}`)}
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

      {usingStandby && (
        <View style={[styles.errBanner, { backgroundColor: theme.card, borderBottomWidth: 1, borderBottomColor: theme.s2, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }]}>
          <Text numberOfLines={1} style={[styles.errTxt, { color: theme.text, flex: 1 }]}>
            {t('standbyActiveBanner')}
          </Text>
          <Pressable
            onPress={() => void reconnect()}
            style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5, backgroundColor: theme.s1 }}
          >
            <Text style={{ fontSize: 13, fontWeight: '700', color: theme.text }}>{t('retryPrimary')}</Text>
          </Pressable>
        </View>
      )}

      {activeDesktopTarget.temporary && (
        <View style={[styles.errBanner, { backgroundColor: theme.card, borderBottomWidth: 1, borderBottomColor: theme.s2, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }]}>
          <Text numberOfLines={1} style={[styles.errTxt, { color: theme.text, flex: 1 }]}>
            {t('tempDesktopBanner', { name: activeDesktopTarget.name || activeDesktopTarget.target })}
          </Text>
          <Pressable
            onPress={() => router.push(`/desktop?target=${encodeURIComponent(activeDesktopTarget.target || 'local')}`)}
            style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5, backgroundColor: theme.s1 }}
          >
            <Text style={{ fontSize: 13, fontWeight: '700', color: theme.text }}>{t('desktopShort')}</Text>
          </Pressable>
          <Pressable
            onPress={() => void setActiveDesktopTarget('local')}
            style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5, backgroundColor: theme.s1 }}
          >
            <Text style={{ fontSize: 13, fontWeight: '700', color: theme.text }}>{t('switchBackLocal')}</Text>
          </Pressable>
        </View>
      )}

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
          data={timelineEntries}
          keyExtractor={(item, idx) => item.id || `${idx}`}
          renderItem={renderTimelineEntry}
          contentContainerStyle={[
            styles.listContent,
            timelineEntries.length === 0 && styles.emptyGrow,
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
              <BotAvatar id={botId} size={92} index={botIndex} avatar={bot?.avatar} color={bot?.color} />
              <Text style={[styles.emptyTitle, { color: theme.text }]}>{bot?.name || botId}</Text>
              <Text style={[styles.emptyDesc, { color: theme.text2 }]}>
                {bot?.description || t('emptyChatHint')}
              </Text>
            </View>
          }
          ListFooterComponent={
            bot?.busy || botQuestions.length > 0 ? (
              <View>
                {bot?.busy ? (
                  <View style={[styles.busyFoot, { backgroundColor: theme.s1 }]}>
                    <ActivityIndicator size="small" color={theme.busy} />
                    <Text style={[styles.busyFootTxt, { color: theme.busy }]}>
                      {t('processing')}
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
          {slashQuery !== null && filteredCommands.length > 0 && (
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              keyboardShouldPersistTaps="always"
              contentContainerStyle={styles.slashRow}
            >
              {filteredCommands.slice(0, 15).map((c) => (
                <Pressable
                  key={`${c.source}:${c.name}`}
                  onPress={() => onPickSlash(c)}
                  style={[styles.slashPill, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.slashCmd, { color: botPal.accent, fontFamily: MONO_FONT }]}>
                    /{c.name}
                  </Text>
                  {c.description ? (
                    <Text style={[styles.slashLbl, { color: theme.text2 }]} numberOfLines={1}>
                      {c.description}
                    </Text>
                  ) : null}
                </Pressable>
              ))}
            </ScrollView>
          )}

          {activeMention && mentions.length > 0 && (
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              keyboardShouldPersistTaps="always"
              contentContainerStyle={styles.slashRow}
            >
              {mentions.slice(0, 15).map((m) => (
                <Pressable
                  key={`${m.kind}:${m.value}`}
                  onPress={() => onPickMention(m)}
                  style={[styles.slashPill, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.slashCmd, { color: botPal.accent, fontFamily: MONO_FONT }]}>
                    {m.kind === 'file' ? m.value : `@${m.value}`}
                  </Text>
                  <Text style={[styles.slashLbl, { color: theme.text2 }]}>
                    {m.kind === 'bot' ? t('mentionBot') : m.kind === 'agent' ? t('mentionAgent') : t('mentionFile')}
                  </Text>
                </Pressable>
              ))}
            </ScrollView>
          )}

          {pendingAttachments.length > 0 && (
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              keyboardShouldPersistTaps="always"
              contentContainerStyle={styles.slashRow}
            >
              {pendingAttachments.map((att) => (
                <View
                  key={att.id}
                  style={[
                    styles.attChip,
                    { backgroundColor: theme.card, borderColor: att.error ? theme.danger : theme.s2 },
                  ]}
                >
                  {att.kind === 'image' ? (
                    <Image source={{ uri: att.uri }} style={styles.attThumb} />
                  ) : (
                    <DocIcon size={16} color={theme.text2} />
                  )}
                  <Text style={[styles.attName, { color: theme.text }]} numberOfLines={1}>
                    {att.name}
                  </Text>
                  {att.uploading ? <ActivityIndicator size="small" color={theme.mute} /> : null}
                  <Pressable
                    onPress={() => setPendingAttachments((prev) => prev.filter((a) => a.id !== att.id))}
                    hitSlop={6}
                  >
                    <Text style={[styles.attRmTxt, { color: theme.text2 }]}>×</Text>
                  </Pressable>
                </View>
              ))}
            </ScrollView>
          )}

          <View style={[styles.composerCapsule, { backgroundColor: theme.card }]}>
            <Pressable
              accessibilityLabel={t('attachFile')}
              onPress={() => void onPickAttachment()}
              style={[styles.attachBtn, { backgroundColor: theme.s1 }]}
            >
              <PaperclipIcon size={18} color={theme.text2} />
            </Pressable>
            <TextInput
              ref={inputRef}
              value={input}
              onChangeText={(txt) => {
                setAcDismissed(false);
                setInput(txt);
              }}
              placeholder={t('msgBotPh', { name: bot?.name || botId })}
              placeholderTextColor={theme.mute}
              multiline
              style={[styles.composerInput, { color: theme.text }]}
              maxLength={8000}
            />
            <Pressable
              accessibilityLabel={showStop ? t('stopOutput') : t('sendMsg')}
              disabled={!showStop && ((!input.trim() && readyMediaIds.length === 0) || hasUploading || sending)}
              onPress={showStop ? onInterrupt : onSubmitComposer}
              style={[
                styles.sendBtn,
                {
                  backgroundColor: showStop
                    ? theme.text
                    : (input.trim() || readyMediaIds.length > 0) && !hasUploading
                      ? botPal.accent
                      : theme.s2,
                },
              ]}
            >
              {showStop ? (
                <StopIcon size={16} color={theme.page} />
              ) : (
                <SendIcon
                  size={17}
                  color={(input.trim() || readyMediaIds.length > 0) && !hasUploading ? botPal.text : theme.mute}
                />
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
                  <Pressable onPress={() => setAppearOpen((v) => !v)}>
                    <BotAvatar id={botId} size={76} index={botIndex} avatar={editAvatar} color={editColor} />
                  </Pressable>
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
                    <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
                      <Text style={[styles.detailId, { color: theme.mute, fontFamily: MONO_FONT }]}>
                        @{botId}
                      </Text>
                      <Pressable
                        onPress={() => setAppearOpen((v) => !v)}
                        style={{
                          borderRadius: 999,
                          paddingHorizontal: 10,
                          paddingVertical: 3,
                          backgroundColor: appearOpen ? botPal.accent : botPal.soft,
                        }}
                      >
                        <Text style={{ fontSize: 12, fontWeight: '700', color: appearOpen ? botPal.text : botPal.accent }}>
                          {t('appearance')}
                        </Text>
                      </Pressable>
                    </View>
                  </View>
                </View>
                {appearOpen && (
                  <BotAppearancePicker
                    botId={botId}
                    botIndex={botIndex}
                    avatar={editAvatar}
                    color={editColor}
                    accent={botPal.accent}
                    onAvatarChange={setEditAvatar}
                    onColorChange={setEditColor}
                  />
                )}
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
                    <View style={{ gap: 8 }}>
                      <Text style={[styles.instructionsTxt, { color: theme.mute }]}>{t('hostNoMagpie')}</Text>
                      <Pressable
                        onPress={() => {
                          void installMagpie()
                            .then(() => loadBotMemories())
                            .catch(() => {});
                        }}
                        style={{ borderRadius: 999, paddingVertical: 8, paddingHorizontal: 14, alignSelf: 'flex-start', backgroundColor: botPal.accent }}
                      >
                        <Text style={{ fontSize: 13, fontWeight: '700', color: botPal.text }}>
                          {t('mpInstallOnHost')}
                        </Text>
                      </Pressable>
                    </View>
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
                        <Pressable
                          onPress={() => {
                            setDetailOpen(false);
                            router.push(`/desktop?tab=files&botId=${encodeURIComponent(botId)}`);
                          }}
                        >
                          <Text style={[styles.monoBlock, { color: theme.text2, fontFamily: MONO_FONT, textDecorationLine: 'underline' }]}>
                            {activeDetail?.workspace || '-'}
                          </Text>
                        </Pressable>
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

      <Modal visible={!!collabPeerId} animationType="slide" onRequestClose={() => setCollabPeerId(null)}>
        <SafeAreaView style={[styles.safe, { backgroundColor: theme.page }]} edges={['top', 'left', 'right', 'bottom']}>
          <ModalHeader
            title={
              collabPeerId
                ? t('collabTitle', {
                    bot: bot?.name || botId,
                    peer: bots.find((b) => b.id === collabPeerId)?.name || collabPeerId,
                  })
                : ''
            }
            onClose={() => setCollabPeerId(null)}
          />
          <ScrollView contentContainerStyle={{ padding: 16, gap: 10 }}>
            {collabLoading && collabItems.length === 0 ? (
              <ActivityIndicator size="small" color={theme.accent} style={{ marginTop: 32 }} />
            ) : collabItems.length === 0 ? (
              <Text style={{ textAlign: 'center', color: theme.mute, marginTop: 32, fontSize: 15 }}>
                {t('collabEmpty')}
              </Text>
            ) : (
              collabItems.map((m) => {
                const isOut = m.role === 'bot_out';
                const peerName = bots.find((b) => b.id === collabPeerId)?.name || collabPeerId || '';
                const label = isOut ? t('delegateTo', { peer: peerName }) : t('fromPeer', { peer: peerName });
                return (
                  <View key={m.id} style={isOut ? styles.userRow : styles.assistantRow}>
                    <View
                      style={[
                        styles.botRelBubble,
                        { backgroundColor: isOut ? botPal.soft : theme.lilac },
                      ]}
                    >
                      <Text style={[styles.botRelLabel, { color: isOut ? theme.text2 : theme.lilacInk }]}>
                        {label}
                      </Text>
                      {m.text ? <MarkdownText text={m.text} theme={theme} /> : null}
                      {renderMediaGrid(m.media)}
                    </View>
                  </View>
                );
              })
            )}
          </ScrollView>
        </SafeAreaView>
      </Modal>

      <Modal visible={!!previewImg} transparent animationType="fade" onRequestClose={() => setPreviewImg(null)}>
        <Pressable style={styles.lightboxBackdrop} onPress={() => setPreviewImg(null)}>
          {previewImg ? (
            <Image
              source={getMediaSource(baseURL, authToken, previewImg.url)}
              style={styles.lightboxImg}
              resizeMode="contain"
            />
          ) : null}
        </Pressable>
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
  collabChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 999,
  },
  collabChipTxt: {
    fontSize: 13,
    fontWeight: '600',
  },
  mediaGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
    marginTop: 4,
  },
  mediaItem: {
    borderRadius: 12,
    overflow: 'hidden',
  },
  mediaImgSingle: {
    width: 240,
    height: 180,
    borderRadius: 12,
  },
  mediaImgMulti: {
    width: 120,
    height: 120,
    borderRadius: 12,
  },
  attachWrap: {
    gap: 6,
    marginTop: 4,
  },
  fileList: {
    gap: 6,
  },
  fileCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 12,
    minWidth: 180,
  },
  fileMeta: {
    flex: 1,
    minWidth: 0,
  },
  fileName: {
    fontSize: 14,
    fontWeight: '600',
  },
  fileSize: {
    fontSize: 12,
  },
  lightboxBackdrop: {
    flex: 1,
    backgroundColor: 'rgba(0,0,0,0.88)',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 16,
  },
  lightboxImg: {
    width: '100%',
    height: '85%',
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
    paddingLeft: 6,
    paddingRight: 6,
    paddingVertical: 6,
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 8,
  },
  attachBtn: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
  },
  attChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 8,
    paddingVertical: 6,
    borderRadius: 12,
    borderWidth: 1,
    maxWidth: 200,
  },
  attThumb: {
    width: 28,
    height: 28,
    borderRadius: 6,
  },
  attName: {
    fontSize: 13,
    maxWidth: 110,
  },
  attRmTxt: {
    fontSize: 18,
    lineHeight: 18,
    paddingHorizontal: 2,
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
