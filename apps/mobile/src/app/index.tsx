import * as Haptics from 'expo-haptics';
import { router } from 'expo-router';
import React, { useCallback, useMemo, useState } from 'react';
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
import { GlobeIcon, MonitorIcon, PlusIcon, SearchIcon, SettingsIcon } from '../components/Icons';
import { formatShortTime, plainPreview, useHub } from '../store';
import { useTheme } from '../theme';
import type { BotStatus } from '../types';

export default function BotsScreen() {
  const theme = useTheme();
  const {
    ready,
    activeServer,
    connState,
    connError,
    online,
    bots,
    last,
    unread,
    tools,
    nodes,
    takeovers,
    refreshBots,
    reconnect,
    resolveTakeover,
  } = useHub();

  const [query, setQuery] = useState('');
  const [refreshing, setRefreshing] = useState(false);

  const onlineNodesCount = useMemo(() => nodes.filter((n) => n.connected).length, [nodes]);

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

  const statusDotColor = useMemo(() => {
    if (connState === 'connected' && online) return theme.sageInk;
    if (connState === 'connected' || connState === 'connecting') return theme.busy;
    if (connState === 'unauthorized' || connState === 'error') return theme.danger;
    return theme.mute;
  }, [connState, online, theme]);

  const statusText = useMemo(() => {
    if (!activeServer) return '未连接';
    if (connState === 'connecting') return `${activeServer.label} · 连接中`;
    if (connState === 'unauthorized') return `${activeServer.label} · 需要口令`;
    if (connState === 'error') return `${activeServer.label} · 连接断开`;
    if (connState === 'connected' && !online) return `${activeServer.label} · 重连中`;
    if (connState === 'connected') return activeServer.label;
    return '未连接';
  }, [activeServer, connState, online]);

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

  const needsFirstPairing = ready && !activeServer;
  const showDisconnectedCard =
    ready && !!activeServer && (connState === 'unauthorized' || (connState === 'error' && bots.length === 0));

  const renderBotItem = ({ item, index }: { item: BotStatus; index: number }) => {
    const lastMsg = last[item.id];
    const unreadCount = unread[item.id] || 0;
    const toolName = tools[item.id];

    let subtitleText = item.description || item.harness;
    let subtitleColor = theme.mute;
    if (item.busy) {
      subtitleText = toolName ? `正在调用 ${toolName}…` : '正在思考…';
      subtitleColor = theme.busy;
    } else if (lastMsg) {
      const prefix = lastMsg.role === 'user' ? '你: ' : '';
      subtitleText = plainPreview(prefix + lastMsg.text) || ' ';
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
        <BotAvatar id={item.id} size={50} index={index} busy={!!item.busy} ringColor={theme.card} />
        <View style={styles.botBody}>
          <View style={styles.botTopRow}>
            <Text numberOfLines={1} style={[styles.botName, { color: theme.text }]}>
              {item.name}
            </Text>
            <Text style={[styles.botTime, { color: theme.mute }]}>
              {formatShortTime(lastMsg?.createdAt)}
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
        <View style={styles.titleBlock}>
          <Text style={[styles.title, { color: theme.text }]}>Bots</Text>
          <Pressable
            onPress={() => router.push('/connect')}
            style={[styles.serverPill, { backgroundColor: theme.s1 }]}
          >
            <View style={[styles.statusDot, { backgroundColor: statusDotColor }]} />
            <Text numberOfLines={1} style={[styles.serverPillTxt, { color: theme.text2 }]}>
              {statusText}
            </Text>
          </Pressable>
        </View>

        <View style={styles.headerActions}>
          <Pressable
            accessibilityLabel="新建 Bot"
            onPress={() => router.push(connState === 'connected' ? '/create-bot' : '/connect')}
            style={[styles.iconBtn, { backgroundColor: theme.s1 }]}
          >
            <PlusIcon size={19} color={theme.text} />
          </Pressable>
          <Pressable
            accessibilityLabel="远程桌面"
            onPress={() =>
              router.push(
                connState === 'connected'
                  ? `/desktop?target=${encodeURIComponent(takeovers[0]?.target || 'local')}`
                  : '/connect'
              )
            }
            style={[styles.iconBtn, { backgroundColor: theme.s1 }]}
          >
            <MonitorIcon size={19} color={theme.text} />
            {takeovers.length > 0 && (
              <View style={[styles.nodeBadge, { backgroundColor: theme.danger }]}>
                <Text style={styles.nodeBadgeTxt}>{takeovers.length}</Text>
              </View>
            )}
          </Pressable>
          <Pressable
            accessibilityLabel="设备与隧道"
            onPress={() => router.push('/devices')}
            style={[styles.iconBtn, { backgroundColor: theme.s1 }]}
          >
            <GlobeIcon size={19} color={theme.text} />
            {onlineNodesCount > 0 && (
              <View style={[styles.nodeBadge, { backgroundColor: theme.sageInk }]}>
                <Text style={styles.nodeBadgeTxt}>{onlineNodesCount}</Text>
              </View>
            )}
          </Pressable>
          <Pressable
            accessibilityLabel="设置"
            onPress={() => router.push(connState === 'connected' ? '/settings' : '/connect')}
            style={[styles.iconBtn, { backgroundColor: theme.s1 }]}
          >
            <SettingsIcon size={19} color={theme.text} />
          </Pressable>
        </View>
      </View>

      {!needsFirstPairing && (
        <View style={[styles.searchBox, { backgroundColor: theme.s1 }]}>
          <SearchIcon size={16} color={theme.mute} />
          <TextInput
            value={query}
            onChangeText={setQuery}
            placeholder="搜索 Bot"
            placeholderTextColor={theme.mute}
            style={[styles.searchInput, { color: theme.text }]}
            clearButtonMode="while-editing"
            returnKeyType="search"
          />
          {Platform.OS !== 'ios' && query.length > 0 && (
            <Pressable onPress={() => setQuery('')} hitSlop={8}>
              <Text style={[styles.clearTxt, { color: theme.mute }]}>清除</Text>
            </Pressable>
          )}
        </View>
      )}

      {takeovers.length > 0 && (
        <View style={[styles.toBanner, { backgroundColor: theme.pink }]}>
          <Text numberOfLines={2} style={[styles.toBannerTxt, { color: theme.pinkInk }]}>
            人工接管（{takeovers[0].botName || takeovers[0].botId}）：{takeovers[0].reason}
          </Text>
          <View style={styles.toBannerActs}>
            <Pressable
              onPress={() => router.push(`/desktop?target=${encodeURIComponent(takeovers[0].target || 'local')}`)}
              style={[styles.toBannerBtn, { backgroundColor: theme.card }]}
            >
              <Text style={[styles.toBannerBtnTxt, { color: theme.text }]}>桌面</Text>
            </Pressable>
            <Pressable
              onPress={() => void resolveTakeover(takeovers[0].id)}
              style={[styles.toBannerBtn, { backgroundColor: theme.accent }]}
            >
              <Text style={[styles.toBannerBtnTxt, { color: theme.accentText }]}>恢复 AI</Text>
            </Pressable>
          </View>
        </View>
      )}

      {!ready || (connState === 'connecting' && bots.length === 0) ? (
        <View style={styles.centerBox}>
          <ActivityIndicator size="small" color={theme.accent} />
          <Text style={[styles.centerTxt, { color: theme.text2 }]}>
            {connState === 'connecting' ? '正在连接主电脑…' : '加载中…'}
          </Text>
        </View>
      ) : needsFirstPairing ? (
        <View style={styles.cardWrap}>
          <View style={[styles.onboardCard, { backgroundColor: theme.card }]}>
            <Text style={[styles.onboardTitle, { color: theme.text }]}>连接主电脑</Text>
            <View style={styles.onboardSteps}>
              <View style={[styles.onboardStepRow, { backgroundColor: theme.s1 }]}>
                <View style={[styles.stepBadge, { backgroundColor: theme.card }]}>
                  <Text style={[styles.stepBadgeTxt, { color: theme.text }]}>1</Text>
                </View>
                <Text style={[styles.onboardStepTxt, { color: theme.text2 }]}>
                  在电脑端打开 Bot Hub，完成开箱引导或在设置中开启远程访问
                </Text>
              </View>
              <View style={[styles.onboardStepRow, { backgroundColor: theme.s1 }]}>
                <View style={[styles.stepBadge, { backgroundColor: theme.card }]}>
                  <Text style={[styles.stepBadgeTxt, { color: theme.text }]}>2</Text>
                </View>
                <Text style={[styles.onboardStepTxt, { color: theme.text2 }]}>
                  扫描二维码或输入连接地址
                </Text>
              </View>
            </View>
            <View style={styles.onboardBtns}>
              <Pressable
                onPress={() => router.push(Platform.OS === 'web' ? '/connect' : '/connect?scan=1')}
                style={[styles.primaryBtn, { backgroundColor: theme.accent }]}
              >
                <Text style={[styles.primaryBtnTxt, { color: theme.accentText }]}>扫码连接</Text>
              </Pressable>
              <Pressable
                onPress={() => router.push('/connect')}
                style={[styles.secondaryBtn, { backgroundColor: theme.s1 }]}
              >
                <Text style={[styles.secondaryBtnTxt, { color: theme.text2 }]}>手动输入地址</Text>
              </Pressable>
            </View>
          </View>
        </View>
      ) : showDisconnectedCard ? (
        <View style={styles.cardWrap}>
          <View style={[styles.onboardCard, { backgroundColor: theme.card }]}>
            <Text style={[styles.onboardTitle, { color: theme.text }]}>
              {connState === 'unauthorized'
                ? '需要访问令牌'
                : connState === 'error'
                  ? '无法连接主电脑'
                  : '连接主电脑'}
            </Text>
            <Text style={[styles.onboardDesc, { color: theme.text2 }]}>
              {connState === 'error' && connError
                ? connError
                : '在电脑端打开「远程访问」，扫码即可通过加密隧道直连，或输入局域网地址。'}
            </Text>
            <View style={styles.onboardBtns}>
              <Pressable
                onPress={() => router.push('/connect')}
                style={[styles.primaryBtn, { backgroundColor: theme.accent }]}
              >
                <Text style={[styles.primaryBtnTxt, { color: theme.accentText }]}>
                  {connState === 'unauthorized' ? '更新访问口令' : '扫码或输入地址'}
                </Text>
              </Pressable>
              {activeServer && (
                <Pressable
                  onPress={() => void reconnect()}
                  style={[styles.secondaryBtn, { backgroundColor: theme.s1 }]}
                >
                  <Text style={[styles.secondaryBtnTxt, { color: theme.text2 }]}>重试</Text>
                </Pressable>
              )}
            </View>
          </View>
        </View>
      ) : (
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
                {query.trim() ? '没有匹配的 Bot' : '还没有 Bot，点右上角 + 创建一个'}
              </Text>
            </View>
          }
        />
      )}
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
  titleBlock: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 28,
    fontWeight: '700',
    letterSpacing: -0.7,
  },
  serverPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 10,
    paddingVertical: 5,
    borderRadius: 999,
    maxWidth: 160,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  serverPillTxt: {
    fontSize: 12,
    fontWeight: '600',
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
  nodeBadge: {
    position: 'absolute',
    top: -3,
    right: -3,
    minWidth: 16,
    height: 16,
    borderRadius: 8,
    paddingHorizontal: 4,
    alignItems: 'center',
    justifyContent: 'center',
  },
  nodeBadgeTxt: {
    color: '#ffffff',
    fontSize: 10,
    fontWeight: '700',
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
    fontSize: 16,
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
    paddingHorizontal: 11,
    paddingVertical: 6,
  },
  toBannerBtnTxt: {
    fontSize: 12.5,
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
    fontSize: 16,
    fontWeight: '600',
    flex: 1,
  },
  botTime: {
    fontSize: 12,
    fontWeight: '500',
  },
  botBottomRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 8,
  },
  botPreview: {
    fontSize: 13.5,
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
    fontSize: 11,
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
  cardWrap: {
    paddingHorizontal: 16,
    paddingTop: 12,
  },
  onboardCard: {
    borderRadius: 22,
    padding: 20,
    gap: 10,
  },
  onboardTitle: {
    fontSize: 18,
    fontWeight: '700',
    letterSpacing: -0.3,
  },
  onboardDesc: {
    fontSize: 14,
    lineHeight: 20,
  },
  onboardSteps: {
    gap: 8,
    marginTop: 2,
  },
  onboardStepRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    borderRadius: 12,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  stepBadge: {
    width: 24,
    height: 24,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  stepBadgeTxt: {
    fontSize: 12,
    fontWeight: '700',
  },
  onboardStepTxt: {
    flex: 1,
    fontSize: 13.5,
    lineHeight: 19,
  },
  onboardBtns: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 10,
    marginTop: 6,
  },
  primaryBtn: {
    borderRadius: 999,
    paddingHorizontal: 18,
    paddingVertical: 11,
    alignItems: 'center',
    justifyContent: 'center',
  },
  primaryBtnTxt: {
    fontSize: 15,
    fontWeight: '600',
  },
  secondaryBtn: {
    borderRadius: 999,
    paddingHorizontal: 18,
    paddingVertical: 11,
    alignItems: 'center',
    justifyContent: 'center',
  },
  secondaryBtnTxt: {
    fontSize: 15,
    fontWeight: '600',
  },
});
