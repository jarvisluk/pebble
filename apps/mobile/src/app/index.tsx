import * as Haptics from 'expo-haptics';
import { router } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
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
import { ArrowUpDownIcon, PinIcon, SearchIcon, SettingsIcon } from '../components/Icons';
import { confirmAction } from '../confirm';
import { useI18n } from '../i18n';
import { type BotSortMode, loadBotListPrefs, saveBotSortMode, savePinnedBots } from '../storage';
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
    takeovers,
    activeDesktopTarget,
    usingStandby,
    questions,
    refreshBots,
    reconnect,
    resolveTakeover,
    deleteBot,
    toggleUnread,
    setActiveDesktopTarget,
  } = useHub();

  const [query, setQuery] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [manualOverride, setManualOverride] = useState(false);
  const [sortMode, setSortMode] = useState<BotSortMode>('recent');
  const [pinnedIds, setPinnedIds] = useState<string[]>([]);

  useEffect(() => {
    loadBotListPrefs()
      .then((prefs) => {
        setSortMode(prefs.sortMode);
        setPinnedIds(prefs.pinnedIds);
      })
      .catch(() => {});
  }, []);

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

  const orderedBots = useMemo(() => {
    const q = query.trim().toLowerCase();
    const indexed = bots
      .map((bot, idx) => ({ bot, idx }))
      .filter(
        ({ bot }) =>
          !q ||
          bot.name.toLowerCase().includes(q) ||
          bot.id.toLowerCase().includes(q) ||
          (bot.description || '').toLowerCase().includes(q)
      );

    indexed.sort((a, b) => {
      const aPin = pinnedIds.indexOf(a.bot.id);
      const bPin = pinnedIds.indexOf(b.bot.id);
      if (aPin !== -1 && bPin !== -1) return aPin - bPin;
      if (aPin !== -1) return -1;
      if (bPin !== -1) return 1;

      if (sortMode === 'recent') {
        const ta = last[a.bot.id]?.createdAt || '';
        const tb = last[b.bot.id]?.createdAt || '';
        if (ta !== tb) return tb.localeCompare(ta);
      }
      return a.idx - b.idx;
    });

    return indexed;
  }, [bots, last, pinnedIds, query, sortMode]);

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

  const togglePin = (botId: string) => {
    setPinnedIds((prev) => {
      const next = prev.includes(botId) ? prev.filter((id) => id !== botId) : [botId, ...prev];
      void savePinnedBots(next);
      return next;
    });
  };

  const openSortMenu = () => {
    const pick = (mode: BotSortMode) => {
      setSortMode(mode);
      void saveBotSortMode(mode);
    };
    Alert.alert(t('sortBots'), undefined, [
      {
        text: `${sortMode === 'recent' ? '✓ ' : ''}${t('sortByRecent')}`,
        onPress: () => pick('recent'),
      },
      {
        text: `${sortMode === 'manual' ? '✓ ' : ''}${t('sortManual')}`,
        onPress: () => pick('manual'),
      },
      { text: t('cancel'), style: 'cancel' },
    ]);
  };

  const openBotActions = (bot: BotStatus) => {
    if (Platform.OS !== 'web') {
      Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium).catch(() => {});
    }
    const pinned = pinnedIds.includes(bot.id);
    const isUnread = (unread[bot.id] || 0) > 0;
    const actions: Array<{ text: string; style?: 'default' | 'cancel' | 'destructive'; onPress?: () => void }> = [
      {
        text: t(pinned ? 'unpinBot' : 'pinBot'),
        onPress: () => togglePin(bot.id),
      },
      {
        text: t(isUnread ? 'markRead' : 'markUnread'),
        onPress: () => toggleUnread(bot.id),
      },
    ];
    if (bots.length > 1) {
      actions.push({
        text: t('delete'),
        style: 'destructive',
        onPress: () => {
          confirmAction(
            t('deleteBotTitle', { name: bot.name }),
            t('deleteBotDesc'),
            () => {
              void deleteBot(bot.id).catch(() => {});
            },
            true
          );
        },
      });
    }
    actions.push({ text: t('cancel'), style: 'cancel' });
    Alert.alert(bot.name, undefined, actions);
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

  const renderBotItem = ({ item }: { item: { bot: BotStatus; idx: number } }) => {
    const { bot, idx } = item;
    const lastMsg = last[bot.id];
    const unreadCount = unread[bot.id] || 0;
    const pinned = pinnedIds.includes(bot.id);
    const hasQuestion = questions.some((q) => q.botId === bot.id);

    let subtitleText = bot.description || bot.harness;
    let subtitleColor = theme.mute;
    if (hasQuestion) {
      subtitleText = t('questionWaitingBadge');
      subtitleColor = theme.busy;
    } else if (bot.busy) {
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
        onPress={() => openBot(bot.id)}
        onLongPress={() => openBotActions(bot)}
        style={({ pressed }) => [
          styles.botCard,
          { backgroundColor: pressed ? theme.sel : theme.card },
        ]}
      >
        <BotAvatar id={bot.id} size={46} index={idx} avatar={bot.avatar} color={bot.color} busy={!!bot.busy} ringColor={theme.card} />
        <View style={styles.botBody}>
          <View style={styles.botTopRow}>
            <View style={styles.botNameRow}>
              <Text numberOfLines={1} style={[styles.botName, { color: theme.text }]}>
                {bot.name}
              </Text>
              {pinned && <PinIcon size={12} color={theme.mute} />}
            </View>
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
        <Pressable
          accessibilityLabel={t('sortBots')}
          onPress={openSortMenu}
          style={({ pressed }) => [styles.iconBtn, pressed && { backgroundColor: theme.s1 }]}
        >
          <ArrowUpDownIcon size={18} color={theme.text2} />
        </Pressable>

        <Pressable
          accessibilityLabel={t('settings')}
          onPress={() => router.push(connState === 'connected' ? '/settings' : '/connect')}
          style={({ pressed }) => [styles.iconBtn, pressed && { backgroundColor: theme.s1 }]}
        >
          <SettingsIcon size={19} color={theme.text2} />
        </Pressable>
      </View>

      <View style={[styles.searchBox, { backgroundColor: theme.s1 }]}>
        <SearchIcon size={15} color={theme.mute} />
        <TextInput
          value={query}
          onChangeText={setQuery}
          placeholder={t('searchBot')}
          placeholderTextColor={theme.mute}
          style={[styles.searchInput, { color: theme.text }]}
          clearButtonMode="while-editing"
          returnKeyType="search"
        />
        {Platform.OS !== 'ios' && query.length > 0 && (
          <Pressable onPress={() => setQuery('')} hitSlop={8}>
            <Text style={[styles.clearTxt, { color: theme.mute }]}>{t('clear')}</Text>
          </Pressable>
        )}
      </View>

      {usingStandby && (
        <View style={[styles.toBanner, { backgroundColor: theme.card, borderWidth: 1, borderColor: theme.s2 }]}>
          <Text numberOfLines={1} style={[styles.toBannerTxt, { color: theme.text }]}>
            {t('standbyActiveBanner')}
          </Text>
          <View style={styles.toBannerActs}>
            <Pressable
              onPress={() => void reconnect()}
              style={[styles.toBannerBtn, { backgroundColor: theme.s1 }]}
            >
              <Text style={[styles.toBannerBtnTxt, { color: theme.text }]}>{t('retryPrimary')}</Text>
            </Pressable>
          </View>
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
        data={orderedBots}
        keyExtractor={(item) => item.bot.id}
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
    paddingTop: 6,
    paddingBottom: 6,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  iconBtn: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
  searchBox: {
    marginHorizontal: 16,
    marginBottom: 10,
    height: 36,
    borderRadius: 999,
    paddingHorizontal: 12,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  searchInput: {
    flex: 1,
    fontSize: 14,
    paddingVertical: 0,
  },
  clearTxt: {
    fontSize: 13,
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
    fontSize: 13,
    fontWeight: '600',
  },
  toBannerActs: {
    flexDirection: 'row',
    gap: 6,
  },
  toBannerBtn: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  toBannerBtnTxt: {
    fontSize: 12,
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
    paddingHorizontal: 12,
    paddingVertical: 11,
    borderRadius: 20,
  },
  botBody: {
    flex: 1,
    minWidth: 0,
    gap: 3,
  },
  botTopRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  botNameRow: {
    flex: 1,
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
  },
  botName: {
    fontSize: 15,
    fontWeight: '600',
    flexShrink: 1,
  },
  botTime: {
    fontSize: 12,
    fontWeight: '400',
  },
  botBottomRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  botPreview: {
    fontSize: 13,
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
    fontSize: 14,
    textAlign: 'center',
  },
  secondaryBtn: {
    borderRadius: 999,
    paddingHorizontal: 18,
    paddingVertical: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  secondaryBtnTxt: {
    fontSize: 14,
    fontWeight: '600',
  },
});
