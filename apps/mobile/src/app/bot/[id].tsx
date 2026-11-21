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
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView, useSafeAreaInsets } from 'react-native-safe-area-context';

import { BotAvatar } from '../../components/BotAvatar';
import { ChevronLeftIcon, DocIcon, MonitorIcon, SendIcon, StopIcon } from '../../components/Icons';
import { MarkdownText } from '../../components/MarkdownText';
import { ModalHeader } from '../../components/ModalHeader';
import { confirmAction } from '../../confirm';
import { formatStamp, useHub } from '../../store';
import { MONO_FONT, useTheme } from '../../theme';
import type { BotDetail, ChatMessage } from '../../types';

const FIVE_MIN_MS = 5 * 60 * 1000;

const SLASH_COMMANDS = [
  { cmd: '/status', label: '会话状态', autoSend: true },
  { cmd: '/bots', label: '所有 Bot', autoSend: true },
  { cmd: '/ask ', label: '跨 Bot 协作', autoSend: false },
  { cmd: '/reset', label: '重置会话', autoSend: true },
];

export default function BotChatScreen() {
  const { id } = useLocalSearchParams<{ id: string | string[] }>();
  const botId = useMemo(() => decodeURIComponent(Array.isArray(id) ? id[0] || '' : id || ''), [id]);

  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const {
    bots,
    msgs,
    tools,
    takeovers,
    setActiveChatBot,
    loadMessages,
    sendMessage,
    interruptBot,
    resetBot,
    getBotDetail,
    updateBot,
    deleteBot,
    resolveTakeover,
  } = useHub();

  const botIndex = useMemo(() => bots.findIndex((b) => b.id === botId), [bots, botId]);
  const bot = botIndex >= 0 ? bots[botIndex] : undefined;
  const chatList = msgs[botId] || [];

  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detail, setDetail] = useState<BotDetail | null>(null);
  const [editMode, setEditMode] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDesc, setEditDesc] = useState('');
  const [editInst, setEditInst] = useState('');
  const [editSaving, setEditSaving] = useState(false);
  const [editErr, setEditErr] = useState('');
  const [sendErr, setSendErr] = useState('');

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

  const handleOpenDetail = useCallback(async () => {
    setEditMode(false);
    setEditErr('');
    setDetailOpen(true);
    setDetailLoading(true);
    try {
      const d = await getBotDetail(botId);
      setDetail(d);
    } catch {
      // keep fallback bot info if detail fails
    } finally {
      setDetailLoading(false);
    }
  }, [botId, getBotDetail]);

  const enterEditMode = () => {
    const src = detail || bot;
    setEditName(src?.name || botId);
    setEditDesc(src?.description || '');
    setEditInst(detail?.instructions || '');
    setEditErr('');
    setEditMode(true);
  };

  const onSaveEdit = async () => {
    const trimmed = editName.trim();
    if (!trimmed) {
      setEditErr('名称不能为空');
      return;
    }
    setEditErr('');
    setEditSaving(true);
    try {
      await updateBot(botId, {
        name: trimmed,
        description: editDesc.trim(),
        instructions: editInst,
      });
      const fresh = await getBotDetail(botId);
      setDetail(fresh);
      setEditMode(false);
    } catch (err: any) {
      setEditErr(err?.message || '保存失败');
    } finally {
      setEditSaving(false);
    }
  };

  const handleDeleteConfirm = () => {
    if (bots.length <= 1) return;
    confirmAction(
      `删除 ${bot?.name || botId}？`,
      '将从配置中移除此 Bot 并停止其队列，磁盘上的 workspace 与历史文件会保留。',
      async () => {
        try {
          await deleteBot(botId);
          setDetailOpen(false);
          router.replace('/');
        } catch (err: any) {
          setEditErr(err?.message || '删除失败');
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
        setSendErr(err?.message || '发送失败');
      } finally {
        setSending(false);
      }
    },
    [botId, sendMessage]
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
      setSendErr(err?.message || '停止失败');
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
      '重置会话',
      `将清空「${bot?.name || botId}」的当前上下文并开启新会话，此操作不可撤销。`,
      async () => {
        try {
          await resetBot(botId);
          setDetailOpen(false);
          await loadMessages(botId);
        } catch (err: any) {
          setSendErr(err?.message || '重置失败');
        }
      },
      true
    );
  };

  const suggestions = [
    {
      title: '你能做什么？',
      sub: '了解能力与工作目录',
      bg: theme.pink,
      ink: theme.pinkInk,
      onPress: () => void handleSendText('你能做什么？', false),
    },
    {
      title: '会话状态',
      sub: '/status',
      bg: theme.lilac,
      ink: theme.lilacInk,
      onPress: () => void handleSendText('/status', false),
    },
    {
      title: '让别的 Bot 帮忙',
      sub: '/ask ',
      bg: theme.sage,
      ink: theme.sageInk,
      onPress: () => {
        setInput('/ask ');
        inputRef.current?.focus();
      },
    },
    {
      title: '列出所有 Bot',
      sub: '/bots',
      bg: theme.accentSoft,
      ink: theme.accent,
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
          <View style={[styles.userBubble, { backgroundColor: theme.accentSoft }]}>
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
      bubble = (
        <View style={styles.assistantRow}>
          <View style={[styles.assistantBubble, { backgroundColor: theme.card }]}>
            <MarkdownText text={item.text} theme={theme} />
            {metaParts.length > 0 && (
              <Text style={[styles.msgMeta, { color: theme.mute }]}>{metaParts.join(' · ')}</Text>
            )}
          </View>
        </View>
      );
    } else if (item.role === 'bot_out' || item.role === 'bot_in') {
      const label = item.role === 'bot_out' ? `委托给 @${item.to || ''}` : `来自 @${item.from || ''}`;
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
                isCmd && { fontFamily: MONO_FONT, fontSize: 12.5 },
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
          <Text style={[styles.stamp, { color: theme.mute }]}>{formatStamp(item.createdAt)}</Text>
        )}
        {bubble}
      </View>
    );
  };

  const showStop = !!bot?.busy && !input.trim();
  const activeDetail = detail || bot;

  return (
    <SafeAreaView style={[styles.safe, { backgroundColor: theme.page }]} edges={['top', 'left', 'right']}>
      <View style={[styles.header, { borderBottomColor: theme.s2 }]}>
        <Pressable
          accessibilityLabel="返回"
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
          accessibilityLabel="远程桌面"
          onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0]?.target || 'local')}`)}
          style={[styles.docBtn, { backgroundColor: theme.s1, marginRight: 6 }]}
          hitSlop={8}
        >
          <MonitorIcon size={18} color={takeovers.length > 0 ? theme.danger : theme.text2} />
        </Pressable>
        <Pressable
          accessibilityLabel="Bot 详情"
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
            人工接管：{takeovers[0].reason}
          </Text>
          <Pressable
            onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0].target || 'local')}`)}
            style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5, backgroundColor: theme.card }}
          >
            <Text style={{ fontSize: 12, fontWeight: '700', color: theme.text }}>处理</Text>
          </Pressable>
          <Pressable
            onPress={() => void resolveTakeover(takeovers[0].id)}
            style={{ borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5, backgroundColor: theme.accent }}
          >
            <Text style={{ fontSize: 12, fontWeight: '700', color: theme.accentText }}>恢复</Text>
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
                {bot?.description || '直接发消息，或试试下面的指令'}
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
            bot?.busy ? (
              <View style={[styles.busyFoot, { backgroundColor: theme.s1 }]}>
                <ActivityIndicator size="small" color={theme.busy} />
                <Text style={[styles.busyFootTxt, { color: theme.busy }]}>
                  {tools[botId] ? `正在调用 ${tools[botId]}…` : '正在思考…'}
                </Text>
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
              {SLASH_COMMANDS.map((c) => (
                <Pressable
                  key={c.cmd}
                  onPress={() => onPickSlash(c.cmd, c.autoSend)}
                  style={[styles.slashPill, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.slashCmd, { color: theme.accent, fontFamily: MONO_FONT }]}>
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
              placeholder={`发消息给 ${bot?.name || botId}…`}
              placeholderTextColor={theme.mute}
              multiline
              style={[styles.composerInput, { color: theme.text }]}
              maxLength={8000}
            />
            <Pressable
              accessibilityLabel={showStop ? '停止输出' : '发送消息'}
              disabled={!showStop && (!input.trim() || sending)}
              onPress={showStop ? onInterrupt : onSubmitComposer}
              style={[
                styles.sendBtn,
                {
                  backgroundColor: showStop
                    ? theme.text
                    : input.trim()
                      ? theme.accent
                      : theme.s2,
                },
              ]}
            >
              {showStop ? (
                <StopIcon size={16} color={theme.page} />
              ) : (
                <SendIcon size={17} color={input.trim() ? theme.accentText : theme.mute} />
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
          setEditMode(false);
          setDetailOpen(false);
        }}
      >
        <View style={[styles.modalRoot, { backgroundColor: theme.page }]}>
          {editMode ? (
            <ModalHeader
              title="编辑 Bot"
              closeLabel="取消"
              onClose={() => {
                setEditMode(false);
                setEditErr('');
              }}
              rightLabel="保存"
              onRightPress={() => void onSaveEdit()}
              rightDisabled={!editName.trim() || editSaving}
              rightLoading={editSaving}
            />
          ) : (
            <ModalHeader
              title="Bot 详情"
              onClose={() => setDetailOpen(false)}
              rightLabel="编辑"
              onRightPress={enterEditMode}
              rightDisabled={detailLoading}
            />
          )}
          <ScrollView contentContainerStyle={styles.detailScroll} keyboardShouldPersistTaps="handled">
            {editErr ? (
              <View style={[styles.errBanner, { backgroundColor: theme.pink, marginHorizontal: 0 }]}>
                <Text style={[styles.errTxt, { color: theme.danger }]}>{editErr}</Text>
              </View>
            ) : null}
            {editMode ? (
              <>
                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <Text style={[styles.sectionLabel, { color: theme.mute }]}>名称</Text>
                  <TextInput
                    value={editName}
                    onChangeText={setEditName}
                    placeholder="Bot 名称"
                    placeholderTextColor={theme.mute}
                    style={[styles.editInput, { backgroundColor: theme.s1, color: theme.text }]}
                  />
                  <Text style={[styles.sectionLabel, { color: theme.mute }]}>简介（可选）</Text>
                  <TextInput
                    value={editDesc}
                    onChangeText={setEditDesc}
                    placeholder="一句话说明它负责什么"
                    placeholderTextColor={theme.mute}
                    style={[styles.editInput, { backgroundColor: theme.s1, color: theme.text }]}
                  />
                </View>

                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <Text style={[styles.sectionLabel, { color: theme.mute }]}>AGENTS.md</Text>
                  <TextInput
                    value={editInst}
                    onChangeText={setEditInst}
                    placeholder="# Bot 指令…"
                    placeholderTextColor={theme.mute}
                    multiline
                    textAlignVertical="top"
                    style={[
                      styles.editInput,
                      styles.editTextarea,
                      { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT },
                    ]}
                  />
                </View>
              </>
            ) : (
              <>
                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <View style={styles.detailTop}>
                    <BotAvatar id={botId} size={76} index={botIndex} />
                    <View style={styles.detailTitleCol}>
                      <Text style={[styles.detailName, { color: theme.text }]}>
                        {activeDetail?.name || botId}
                      </Text>
                      <Text style={[styles.detailId, { color: theme.mute, fontFamily: MONO_FONT }]}>
                        @{botId}
                      </Text>
                    </View>
                  </View>
                  {activeDetail?.description ? (
                    <Text style={[styles.detailDesc, { color: theme.text2 }]}>
                      {activeDetail.description}
                    </Text>
                  ) : null}

                  <View style={styles.statGrid}>
                    <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                      <Text style={[styles.statLbl, { color: theme.mute }]}>轮次</Text>
                      <Text style={[styles.statVal, { color: theme.text }]}>
                        {activeDetail?.session?.turnCount || 0}
                      </Text>
                    </View>
                    <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                      <Text style={[styles.statLbl, { color: theme.mute }]}>开销</Text>
                      <Text style={[styles.statVal, { color: theme.text }]}>
                        ${(activeDetail?.session?.totalCostUsd || 0).toFixed(4)}
                      </Text>
                    </View>
                    <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                      <Text style={[styles.statLbl, { color: theme.mute }]}>后端</Text>
                      <Text style={[styles.statVal, { color: theme.text }]}>
                        {activeDetail?.harness || '-'}
                      </Text>
                    </View>
                    <View style={[styles.statPill, { backgroundColor: theme.s1 }]}>
                      <Text style={[styles.statLbl, { color: theme.mute }]}>模型</Text>
                      <Text numberOfLines={1} style={[styles.statVal, { color: theme.text }]}>
                        {activeDetail?.model || '默认'}
                      </Text>
                    </View>
                  </View>
                </View>

                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <Text style={[styles.sectionLabel, { color: theme.mute }]}>工作目录</Text>
                  <Text selectable style={[styles.monoBlock, { color: theme.text2, fontFamily: MONO_FONT }]}>
                    {activeDetail?.workspace || '-'}
                  </Text>
                </View>

                <View style={[styles.detailCard, { backgroundColor: theme.card }]}>
                  <Text style={[styles.sectionLabel, { color: theme.mute }]}>AGENTS.md</Text>
                  {detailLoading ? (
                    <ActivityIndicator size="small" color={theme.accent} />
                  ) : (
                    <Text selectable style={[styles.instructionsTxt, { color: theme.text2 }]}>
                      {detail?.instructions?.trim() ? detail.instructions : '未配置指令'}
                    </Text>
                  )}
                </View>

                <Pressable
                  onPress={handleResetConfirm}
                  style={[styles.dangerBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.dangerBtnTxt, { color: theme.text }]}>重置会话</Text>
                </Pressable>

                {bots.length > 1 && (
                  <Pressable
                    onPress={handleDeleteConfirm}
                    style={[styles.dangerBtn, { backgroundColor: theme.pink }]}
                  >
                    <Text style={[styles.dangerBtnTxt, { color: theme.danger }]}>删除 Bot</Text>
                  </Pressable>
                )}
              </>
            )}
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
    fontSize: 16,
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
    fontSize: 20,
    fontWeight: '700',
    letterSpacing: -0.4,
    marginTop: 4,
  },
  emptyDesc: {
    fontSize: 14,
    textAlign: 'center',
    lineHeight: 20,
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
    fontSize: 14.5,
    fontWeight: '700',
  },
  suggestSub: {
    fontSize: 12.5,
  },
  stamp: {
    fontSize: 11.5,
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
    fontSize: 15,
    lineHeight: 22,
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
    fontSize: 11.5,
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
    fontSize: 12,
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
    fontSize: 13,
    lineHeight: 19,
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
    fontSize: 13,
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
    fontSize: 13,
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
    fontSize: 13,
    fontWeight: '700',
  },
  slashLbl: {
    fontSize: 12.5,
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
    fontSize: 16,
    lineHeight: 22,
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
    fontSize: 13,
  },
  detailDesc: {
    fontSize: 14,
    lineHeight: 20,
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
    fontSize: 11.5,
    fontWeight: '600',
  },
  statVal: {
    fontSize: 14.5,
    fontWeight: '700',
  },
  sectionLabel: {
    fontSize: 12,
    fontWeight: '700',
  },
  monoBlock: {
    fontSize: 12.5,
    lineHeight: 18,
  },
  instructionsTxt: {
    fontSize: 14,
    lineHeight: 21,
  },
  dangerBtn: {
    borderRadius: 999,
    paddingVertical: 14,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 4,
  },
  dangerBtnTxt: {
    fontSize: 15,
    fontWeight: '700',
  },
  editInput: {
    borderRadius: 14,
    paddingHorizontal: 13,
    paddingVertical: 10,
    fontSize: 15,
  },
  editTextarea: {
    minHeight: 200,
    fontSize: 13,
    lineHeight: 19,
  },
});
