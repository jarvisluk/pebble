import * as Haptics from 'expo-haptics';
import { router } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Platform,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { BotAvatar } from '../components/BotAvatar';
import { PlusIcon, SearchIcon, SettingsIcon, XIcon } from '../components/Icons';
import { useI18n } from '../i18n';
import { formatShortTime, plainPreview, useHub } from '../store';
import { useTheme } from '../theme';
import type { BotStatus } from '../types';
import { ConnectContent } from './connect';

export default function BotsScreen() {
  const theme = useTheme();
  const { t, lang } = useI18n();
  const {
    ready,
    activeServer,
    connState,
    bots,
    last,
    unread,
    tools,
    takeovers,
    activeDesktopTarget,
    questions,
    refreshBots,
    reconnect,
    resolveTakeover,
    setActiveDesktopTarget,
  } = useHub();

  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [manualOverride, setManualOverride] = useState(false);

  useEffect(() => {
    if (connState === 'connected') {
      setManualOverride(false);
    }
  }, [connState]);

  const showGate =
    !ready ||
    !activeServer ||
    connState === 'unauthorized' ||
    (connState === 'error' && bots.length === 0) ||
    (connState === 'connecting' && bots.length === 0);

  const filteredBots = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return bots;
    return bots.filter(
      (b) =>
        b.name.toLowerCase().includes(q) ||
        b.id.toLowerCase().includes(q) ||
        (b.description || '').toLowerCase().includes(q)
    );
  }, [bots, query]);

  const onRefresh = useCallback(async () => {
    if (connState !== 'connected') {
      await reconnect().catch(() => {});
      return;
    }
    setRefreshing(true);
    try {
      await refreshBots();
    } catch {
      // keep current state on transient refresh errors
    } finally {
      setRefreshing(false);
    }
  }, [connState, reconnect, refreshBots]);

  const openBot = (botId: string) => {
    if (Platform.OS !== 'web') {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light).catch(() => {});
    }
    router.push(`/bot/${encodeURIComponent(botId)}`);
  };

  if (showGate) {
    if (!ready || (!!activeServer && connState === 'connecting' && bots.length === 0 && !manualOverride)) {
      return (
        <SafeAreaView style={[styles.safe, { backgroundColor: theme.page }]} edges={['top', 'left', 'right', 'bottom']}>
          <View style={styles.centerBox}>
            <ActivityIndicator size="small" color={theme.accent} />
            <Text style={[styles.centerTxt, { color: theme.text2 }]}>
              {connState === 'connecting' ? t('connectingHost') : t('loading')}
            </Text>
            {ready && !!activeServer && (
              <Pressable
                onPress={() => setManualOverride(true)}
                style={[styles.secondaryBtn, { backgroundColor: theme.s1, marginTop: 8 }]}
              >
                <Text style={[styles.secondaryBtnTxt, { color: theme.text2 }]}>{t('switchHost')}</Text>
              </Pressable>
            )}
          </View>
        </SafeAreaView>
      );
    }
    return <ConnectContent standalone />;
  }

  const renderBotItem = ({ item, index }: { item: BotStatus; index: number }) => {
    const lastMsg = last[item.id];
    const unreadCount = unread[item.id] || 0;
    const toolName = tools[item.id];
    const hasQuestion = questions.some((q) => q.botId === item.id);

    let subtitleText = item.description || item.harness;
    let subtitleColor = theme.mute;
    if (hasQuestion) {
      subtitleText = t('questionWaitingBadge');
      subtitleColor = theme.busy;
    } else if (item.busy) {
      subtitleText = t('processing');
      subtitleColor = theme.busy;
    } else if (lastMsg) {
      const prefix = lastMsg.role === 'user' ? t('youPrefix') : '';
      const firstAtt = lastMsg.media && lastMsg.media.length > 0 ? lastMsg.media[0] : null;
      const attText = firstAtt
        ? firstAtt.kind === 'file' || (firstAtt.kind !== 'image' && firstAtt.mimeType && !firstAtt.mimeType.startsWith('image/'))
          ? `[${t('fileAltFallback')}] ${firstAtt.name || ''}`.trim()
          : `[${t('imageAltFallback')}]`
        : '';
      const rawText = lastMsg.text || attText;
      subtitleText = plainPreview(prefix + rawText, t('codeTag')) || ' ';
      subtitleColor = theme.text2;
    }

    return (
      <Pressable
        onPress={() => openBot(item.id)}
        style={({ pressed }) => [
          styles.botCard,
          { backgroundColor: pressed ? theme.sel : theme.card },
        ]}
      >
        <BotAvatar id={item.id} size={50} index={index} avatar={item.avatar} color={item.color} busy={!!item.busy} ringColor={theme.card} />
        <View style={styles.botBody}>
          <View style={styles.botTopRow}>
            <Text numberOfLines={1} style={[styles.botName, { color: theme.text }]}>
              {item.name}
            </Text>
            <Text style={[styles.botTime, { color: theme.mute }]}>
              {formatShortTime(lastMsg?.createdAt, lang)}
            </Text>
          </View>
          <View style={styles.botBottomRow}>
            <Text numberOfLines={1} style={[styles.botPreview, { color: subtitleColor }]}>
              {subtitleText}
            </Text>
            {unreadCount > 0 && (
              <View style={[styles.unreadPill, { backgroundColor: theme.lilac }]}>
                <Text style={[styles.unreadTxt, { color: theme.lilacInk }]}>
                  {unreadCount > 99 ? '99+' : unreadCount}
                </Text>
              </View>
            )}
          </View>
        </View>
      </Pressable>
    );
  };

  return (
    <SafeAreaView style={[styles.safe, { backgroundColor: theme.page }]} edges={['top', 'left', 'right']}>
      <View style={styles.header}>
        <Text style={[styles.title, { color: theme.text }]}>Bots</Text>

        <View style={styles.headerActions}>
          <Pressable
            accessibilityLabel={t('searchBot')}
            onPress={() => {
              if (searching) setQuery('');
              setSearching((v) => !v);
            }}
            style={[styles.iconBtn, { backgroundColor: theme.s1 }]}
          >
            {searching ? <XIcon size={19} color={theme.text} /> : <SearchIcon size={19} color={theme.text} />}
          </Pressable>
          <Pressable
            accessibilityLabel={t('newBot')}
            onPress={() => router.push(connState === 'connected' ? '/create-bot' : '/connect')}
            style={[styles.iconBtn, { backgroundColor: theme.s1 }]}
          >
            <PlusIcon size={19} color={theme.text} />
          </Pressable>
          <Pressable
            accessibilityLabel={t('settings')}
            onPress={() => router.push(connState === 'connected' ? '/settings' : '/connect')}
            style={[styles.iconBtn, { backgroundColor: theme.s1 }]}
          >
            <SettingsIcon size={19} color={theme.text} />
          </Pressable>
        </View>
      </View>

      {searching && (
        <View style={[styles.searchBox, { backgroundColor: theme.s1 }]}>
          <SearchIcon size={16} color={theme.mute} />
          <TextInput
            value={query}
            onChangeText={setQuery}
            placeholder={t('searchBot')}
            placeholderTextColor={theme.mute}
            style={[styles.searchInput, { color: theme.text }]}
            autoFocus
            clearButtonMode="while-editing"
            returnKeyType="search"
          />
          {Platform.OS !== 'ios' && query.length > 0 && (
            <Pressable onPress={() => setQuery('')} hitSlop={8}>
              <Text style={[styles.clearTxt, { color: theme.mute }]}>{t('clear')}</Text>
            </Pressable>
          )}
        </View>
      )}

      {activeDesktopTarget.temporary && (
        <View style={[styles.toBanner, { backgroundColor: theme.card, borderWidth: 1, borderColor: theme.s2 }]}>
          <Text numberOfLines={1} style={[styles.toBannerTxt, { color: theme.text }]}>
            {t('tempDesktopBanner', { name: activeDesktopTarget.name || activeDesktopTarget.target })}
          </Text>
          <View style={styles.toBannerActs}>
            <Pressable
              onPress={() => router.push(`/desktop?target=${encodeURIComponent(activeDesktopTarget.target || 'local')}`)}
              style={[styles.toBannerBtn, { backgroundColor: theme.s1 }]}
            >
              <Text style={[styles.toBannerBtnTxt, { color: theme.text }]}>{t('desktopShort')}</Text>
            </Pressable>
            <Pressable
              onPress={() => void setActiveDesktopTarget('local')}
              style={[styles.toBannerBtn, { backgroundColor: theme.s1 }]}
            >
              <Text style={[styles.toBannerBtnTxt, { color: theme.text }]}>{t('switchBackLocal')}</Text>
            </Pressable>
          </View>
        </View>
      )}

      {takeovers.length > 0 && (
        <View style={[styles.toBanner, { backgroundColor: theme.pink }]}>
          <Text numberOfLines={2} style={[styles.toBannerTxt, { color: theme.pinkInk }]}>
            {t('takeoverBanner', { bot: takeovers[0].botName || takeovers[0].botId || 'Bot', reason: takeovers[0].reason })}
          </Text>
          <View style={styles.toBannerActs}>
            <Pressable
              onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0].target || 'local')}`)}
              style={[styles.toBannerBtn, { backgroundColor: theme.card }]}
            >
              <Text style={[styles.toBannerBtnTxt, { color: theme.text }]}>{t('desktopShort')}</Text>
            </Pressable>
            <Pressable
              onPress={() => void resolveTakeover(takeovers[0].id)}
              style={[styles.toBannerBtn, { backgroundColor: theme.accent }]}
            >
              <Text style={[styles.toBannerBtnTxt, { color: theme.accentText }]}>{t('resumeAI')}</Text>
            </Pressable>
          </View>
        </View>
      )}

      <FlatList
        data={filteredBots}
        keyExtractor={(item) => item.id}
        renderItem={renderBotItem}
        contentContainerStyle={styles.listContent}
        ItemSeparatorComponent={() => <View style={{ height: 8 }} />}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={onRefresh}
            tintColor={theme.accent}
          />
        }
        ListEmptyComponent={
          <View style={styles.centerBox}>
            <Text style={[styles.centerTxt, { color: theme.mute }]}>
              {query.trim() ? t('noMatchedBots') : t('noBotsYet')}
            </Text>
          </View>
        }
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: {
    flex: 1,
  },
  header: {
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 10,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 10,
  },
  title: {
    fontSize: 28,
    fontWeight: '700',
    letterSpacing: -0.7,
  },
  headerActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  iconBtn: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: 'center',
    justifyContent: 'center',
    position: 'relative',
  },
  searchBox: {
    marginHorizontal: 16,
    marginBottom: 12,
    height: 40,
    borderRadius: 999,
    paddingHorizontal: 14,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  searchInput: {
    flex: 1,
    fontSize: 17,
    paddingVertical: 0,
  },
  clearTxt: {
    fontSize: 15,
    fontWeight: '600',
  },
  toBanner: {
    marginHorizontal: 16,
    marginBottom: 10,
    borderRadius: 18,
    paddingHorizontal: 12,
    paddingVertical: 10,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  toBannerTxt: {
    flex: 1,
    fontSize: 15,
    fontWeight: '600',
  },
  toBannerActs: {
    flexDirection: 'row',
    gap: 6,
  },
  toBannerBtn: {
    borderRadius: 999,
    paddingHorizontal: 11,
    paddingVertical: 6,
  },
  toBannerBtnTxt: {
    fontSize: 13.5,
    fontWeight: '700',
  },
  listContent: {
    paddingHorizontal: 16,
    paddingBottom: 28,
  },
  botCard: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    borderRadius: 20,
  },
  botBody: {
    flex: 1,
    minWidth: 0,
    gap: 4,
  },
  botTopRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  botName: {
    fontSize: 17,
    fontWeight: '600',
    flex: 1,
  },
  botTime: {
    fontSize: 13,
    fontWeight: '500',
  },
  botBottomRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  botPreview: {
    fontSize: 15,
    flex: 1,
  },
  unreadPill: {
    minWidth: 20,
    height: 20,
    borderRadius: 10,
    paddingHorizontal: 6,
    alignItems: 'center',
    justifyContent: 'center',
  },
  unreadTxt: {
    fontSize: 12,
    fontWeight: '700',
  },
  centerBox: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 32,
    gap: 10,
  },
  centerTxt: {
    fontSize: 16,
    textAlign: 'center',
  },
  secondaryBtn: {
    borderRadius: 999,
    paddingHorizontal: 18,
    paddingVertical: 11,
    alignItems: 'center',
    justifyContent: 'center',
  },
  secondaryBtnTxt: {
    fontSize: 17,
    fontWeight: '600',
  },
});
