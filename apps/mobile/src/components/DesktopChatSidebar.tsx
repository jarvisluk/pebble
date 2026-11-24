import React, { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { BotAvatar } from './BotAvatar';
import { SendIcon, StopIcon, XIcon } from './Icons';
import { MarkdownText } from './MarkdownText';
import { QuestionCard } from './QuestionCard';
import { useI18n } from '../i18n';
import { formatClockTime, useHub } from '../store';
import { getBotPalette, useTheme } from '../theme';
import type { ChatMessage } from '../types';

interface DesktopChatSidebarProps {
  botId: string;
  pendingInsert?: { seq: number; text: string } | null;
  onSelectBot: (id: string) => void;
  onClose: () => void;
}

export function DesktopChatSidebar({ botId, pendingInsert, onSelectBot, onClose }: DesktopChatSidebarProps) {
  const theme = useTheme();
  const { t, lang } = useI18n();
  const insets = useSafeAreaInsets();
  const {
    bots,
    msgs,
    questions,
    setActiveChatBot,
    loadMessages,
    sendMessage,
    interruptBot,
    replyQuestion,
  } = useHub();

  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);

  const botIndex = useMemo(() => bots.findIndex((b) => b.id === botId), [bots, botId]);
  const bot = botIndex >= 0 ? bots[botIndex] : undefined;
  const botPalette = getBotPalette(botId, Math.max(0, botIndex), theme.dark, bot?.color);
  const messages = msgs[botId] || [];
  const botQuestions = useMemo(
    () => questions.filter((q) => q.botId === botId),
    [botId, questions]
  );

  useEffect(() => {
    if (!botId) return;
    setActiveChatBot(botId);
    void loadMessages(botId).catch(() => {});
    return () => setActiveChatBot(null);
  }, [botId, loadMessages, setActiveChatBot]);

  useEffect(() => {
    if (!pendingInsert?.text) return;
    setDraft((prev) => {
      const needsSpace = prev.length > 0 && !/\s$/.test(prev);
      return prev + (needsSpace ? ' ' : '') + pendingInsert.text;
    });
  }, [pendingInsert?.seq]);

  const stopMode = Boolean(bot?.busy) && !draft.trim();

  const handleSend = async () => {
    if (stopMode) {
      await interruptBot(botId).catch(() => {});
      return;
    }
    const txt = draft.trim();
    if (!txt || !bot) return;
    setDraft('');
    setSending(true);
    try {
      await sendMessage(botId, txt);
    } finally {
      setSending(false);
    }
  };

  const renderMessage = ({ item }: { item: ChatMessage }) => {
    if (item.role === 'system') {
      return (
        <Text style={[styles.sysTxt, { color: theme.mute }]}>
          {item.text}
        </Text>
      );
    }
    const isMe = item.role === 'user';
    return (
      <View style={[styles.msgRow, isMe ? styles.msgMe : styles.msgBot]}>
        <View
          style={[
            styles.bubble,
            {
              backgroundColor: isMe ? botPalette.soft : theme.s1,
            },
          ]}
        >
          <MarkdownText text={item.text} theme={theme} />
          <Text style={[styles.timeTxt, { color: theme.mute }]}>
            {formatClockTime(item.createdAt)}
          </Text>
        </View>
      </View>
    );
  };

  return (
    <KeyboardAvoidingView
      style={[
        styles.root,
        {
          backgroundColor: theme.card,
          borderRightColor: theme.s2,
          paddingTop: insets.top,
          paddingBottom: insets.bottom,
        },
      ]}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
    >
      <View style={[styles.header, { borderBottomColor: theme.s2 }]}>
        <View style={styles.headerLeft}>
          {bot ? <BotAvatar id={bot.id} size={28} index={Math.max(0, botIndex)} avatar={bot.avatar} color={bot.color} /> : null}
          <Text numberOfLines={1} style={[styles.title, { color: theme.text }]}>
            {bot?.name || t('deskChatSidebar')}
          </Text>
        </View>
        <Pressable onPress={onClose} style={[styles.closeBtn, { backgroundColor: theme.s1 }]}>
          <XIcon size={18} color={theme.text2} />
        </Pressable>
      </View>

      {bots.length > 1 && (
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          contentContainerStyle={styles.botChips}
          style={[styles.botBar, { borderBottomColor: theme.s2 }]}
        >
          {bots.map((b) => {
            const active = b.id === botId;
            return (
              <Pressable
                key={b.id}
                onPress={() => onSelectBot(b.id)}
                style={[
                  styles.botChip,
                  { backgroundColor: active ? theme.accentSoft : theme.s1 },
                ]}
              >
                <Text style={[styles.botChipTxt, { color: active ? theme.accent : theme.text2 }]}>
                  {b.name}
                </Text>
              </Pressable>
            );
          })}
        </ScrollView>
      )}

      <FlatList
        style={styles.list}
        contentContainerStyle={styles.listContent}
        data={messages}
        keyExtractor={(m) => m.id}
        renderItem={renderMessage}
        ListEmptyComponent={
          <Text style={[styles.emptyTxt, { color: theme.mute }]}>{t('noMsgsYet')}</Text>
        }
        ListFooterComponent={
          <View style={{ gap: 8 }}>
            {botQuestions.map((q) => (
              <QuestionCard
                key={q.id}
                question={q}
                accent={botPalette.accent}
                accentText={botPalette.text}
                accentSoft={botPalette.soft}
                onReply={replyQuestion}
              />
            ))}
            {bot?.busy ? (
              <View style={styles.busyRow}>
                <ActivityIndicator size="small" color={botPalette.accent} />
                <Text style={[styles.busyTxt, { color: theme.mute }]}>{t('processing')}</Text>
              </View>
            ) : null}
          </View>
        }
      />

      <View style={[styles.composer, { borderTopColor: theme.s2 }]}>
        <TextInput
          value={draft}
          onChangeText={setDraft}
          placeholder={t('sendMsgPh')}
          placeholderTextColor={theme.mute}
          multiline
          style={[styles.input, { backgroundColor: theme.s1, color: theme.text }]}
        />
        <Pressable
          disabled={sending || (!stopMode && !draft.trim())}
          onPress={() => void handleSend()}
          style={[
            styles.sendBtn,
            { backgroundColor: stopMode ? theme.text : botPalette.accent },
          ]}
        >
          {stopMode ? (
            <StopIcon size={16} color={theme.card} />
          ) : (
            <SendIcon size={16} color={botPalette.text} />
          )}
        </Pressable>
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, borderLeftWidth: 1 },
  header: {
    height: 48,
    paddingHorizontal: 12,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    borderBottomWidth: 1,
  },
  headerLeft: { flexDirection: 'row', alignItems: 'center', gap: 8, flex: 1, minWidth: 0 },
  title: { fontSize: 16, fontWeight: '700', flex: 1 },
  closeBtn: { width: 32, height: 32, borderRadius: 16, alignItems: 'center', justifyContent: 'center' },
  botBar: { maxHeight: 46, borderBottomWidth: 1 },
  botChips: { paddingHorizontal: 12, paddingVertical: 8, gap: 6, alignItems: 'center' },
  botChip: { borderRadius: 999, paddingHorizontal: 10, paddingVertical: 5 },
  botChipTxt: { fontSize: 13, fontWeight: '600' },
  list: { flex: 1 },
  listContent: { padding: 12, gap: 8 },
  msgRow: { flexDirection: 'row' },
  msgMe: { justifyContent: 'flex-end' },
  msgBot: { justifyContent: 'flex-start' },
  bubble: { maxWidth: '88%', borderRadius: 16, paddingHorizontal: 12, paddingVertical: 8, gap: 4 },
  timeTxt: { fontSize: 11 },
  sysTxt: { fontSize: 12, textAlign: 'center', marginVertical: 4 },
  emptyTxt: { fontSize: 14, textAlign: 'center', marginTop: 24 },
  busyRow: { flexDirection: 'row', alignItems: 'center', gap: 8, paddingVertical: 4 },
  busyTxt: { fontSize: 13 },
  composer: {
    padding: 10,
    borderTopWidth: 1,
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 8,
  },
  input: {
    flex: 1,
    maxHeight: 110,
    borderRadius: 18,
    paddingHorizontal: 14,
    paddingVertical: 9,
    fontSize: 15,
  },
  sendBtn: { width: 38, height: 38, borderRadius: 19, alignItems: 'center', justifyContent: 'center' },
});
