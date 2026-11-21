import { router } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';

import { ModalHeader } from '../components/ModalHeader';
import { useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';

const PERM_LABELS: Record<string, string> = {
  files: '文件',
  exec: '命令',
  desktop: '桌面',
};

export default function DevicesScreen() {
  const theme = useTheme();
  const {
    activeServer,
    connState,
    isTailcat,
    online,
    nodes,
    tailcat,
    refreshNodes,
    refreshTailcat,
  } = useHub();

  const [refreshing, setRefreshing] = useState(false);

  const loadAll = useCallback(async () => {
    if (connState !== 'connected') return;
    await Promise.all([
      refreshNodes().catch(() => {}),
      refreshTailcat().catch(() => {}),
    ]);
  }, [connState, refreshNodes, refreshTailcat]);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  const onRefresh = async () => {
    setRefreshing(true);
    try {
      await loadAll();
    } finally {
      setRefreshing(false);
    }
  };

  const closeSheet = () => {
    if (router.canGoBack()) {
      router.back();
    } else {
      router.replace('/');
    }
  };

  const tailcatStateText = (() => {
    if (!tailcat) return '未读取';
    if (tailcat.state === 'ready') return '已就绪';
    if (tailcat.state === 'connecting') return '正在连接中继';
    if (tailcat.state === 'disabled') return '未开启';
    return tailcat.error || '连接异常';
  })();

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader title="设备与隧道" onClose={closeSheet} />
      <ScrollView
        contentContainerStyle={styles.scroll}
        refreshControl={
          <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={theme.accent} />
        }
      >
        <View style={[styles.card, { backgroundColor: theme.card }]}>
          <Text style={[styles.cardTitle, { color: theme.text }]}>当前手机连接</Text>
          {activeServer ? (
            <View style={styles.kvGroup}>
              <Text style={[styles.serverName, { color: theme.text }]}>{activeServer.label}</Text>
              <Text selectable style={[styles.addrTxt, { color: theme.mute, fontFamily: MONO_FONT }]}>
                {activeServer.address}
              </Text>
              <View style={styles.badgeRow}>
                <View style={[styles.badge, { backgroundColor: isTailcat ? theme.lilac : theme.s1 }]}>
                  <Text style={[styles.badgeTxt, { color: isTailcat ? theme.lilacInk : theme.text2 }]}>
                    {isTailcat ? 'Tailcat 端到端加密隧道' : 'HTTP 局域网/直连'}
                  </Text>
                </View>
                <View style={[styles.badge, { backgroundColor: online ? theme.sage : theme.s1 }]}>
                  <Text style={[styles.badgeTxt, { color: online ? theme.sageInk : theme.busy }]}>
                    {online ? '实时流已连接' : '重连中'}
                  </Text>
                </View>
              </View>
            </View>
          ) : (
            <Text style={[styles.emptyTxt, { color: theme.mute }]}>尚未连接主电脑</Text>
          )}
          <Pressable
            onPress={() => router.push('/connect')}
            style={[styles.manageBtn, { backgroundColor: theme.s1 }]}
          >
            <Text style={[styles.manageBtnTxt, { color: theme.text }]}>管理连接</Text>
          </Pressable>
          {activeServer && (
            <Pressable
              onPress={() => router.push('/desktop?target=local')}
              style={[styles.manageBtn, { backgroundColor: theme.accentSoft }]}
            >
              <Text style={[styles.manageBtnTxt, { color: theme.accent }]}>打开主电脑远程桌面</Text>
            </Pressable>
          )}
        </View>

        <View style={[styles.card, { backgroundColor: theme.card }]}>
          <Text style={[styles.cardTitle, { color: theme.text }]}>主电脑远程访问（Tailcat）</Text>
          <View style={styles.rowBetween}>
            <Text style={[styles.lbl, { color: theme.text2 }]}>隧道状态</Text>
            <Text
              style={[
                styles.val,
                {
                  color:
                    tailcat?.state === 'ready'
                      ? theme.sageInk
                      : tailcat?.state === 'error'
                        ? theme.danger
                        : theme.text,
                },
              ]}
            >
              {tailcatStateText}
            </Text>
          </View>
          <View style={styles.rowBetween}>
            <Text style={[styles.lbl, { color: theme.text2 }]}>口令保护</Text>
            <Text style={[styles.val, { color: tailcat?.hasToken ? theme.sageInk : theme.mute }]}>
              {tailcat?.hasToken ? '已启用访问口令保护' : '未配置 apiToken'}
            </Text>
          </View>
        </View>

        <View style={[styles.card, { backgroundColor: theme.card }]}>
          <View style={styles.rowBetween}>
            <Text style={[styles.cardTitle, { color: theme.text }]}>已接入的远端电脑节点</Text>
            <Text style={[styles.countTxt, { color: theme.mute }]}>{nodes.length}</Text>
          </View>

          {nodes.length === 0 ? (
            <Text style={[styles.emptyTxt, { color: theme.text2 }]}>
              暂无远端电脑节点接入。在另一台电脑打开 Bot Hub 桌面端并开启「允许主电脑操控本机」即可出现。
            </Text>
          ) : (
            <View style={styles.nodeList}>
              {nodes.map((node) => (
                <View key={node.id} style={[styles.nodeItem, { backgroundColor: theme.s1 }]}>
                  <View style={styles.rowBetween}>
                    <Text style={[styles.nodeName, { color: theme.text }]}>{node.name}</Text>
                    <View
                      style={[
                        styles.statePill,
                        { backgroundColor: node.connected ? theme.sage : theme.s2 },
                      ]}
                    >
                      <Text
                        style={[
                          styles.statePillTxt,
                          { color: node.connected ? theme.sageInk : theme.mute },
                        ]}
                      >
                        {node.connected ? '在线' : '离线'}
                      </Text>
                    </View>
                  </View>

                  <Text style={[styles.nodeMeta, { color: theme.text2, fontFamily: MONO_FONT }]}>
                    {`${node.os}/${node.arch}${node.hostname ? ` · ${node.hostname}` : ''}`}
                  </Text>

                  {node.permissions?.length > 0 && (
                    <View style={styles.permRow}>
                      {node.permissions.map((p) => (
                        <View key={p} style={[styles.permPill, { backgroundColor: theme.card }]}>
                          <Text style={[styles.permTxt, { color: theme.text2 }]}>
                            {PERM_LABELS[p] || p}
                          </Text>
                        </View>
                      ))}
                      {node.connected && node.permissions.includes('desktop') && (
                        <Pressable
                          onPress={() => router.push(`/desktop?target=${encodeURIComponent(node.id)}`)}
                          style={[styles.permPill, { backgroundColor: theme.accentSoft }]}
                        >
                          <Text style={[styles.permTxt, { color: theme.accent }]}>打开桌面</Text>
                        </Pressable>
                      )}
                    </View>
                  )}
                </View>
              ))}
            </View>
          )}
        </View>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  scroll: {
    padding: 16,
    gap: 12,
    paddingBottom: 36,
  },
  card: {
    borderRadius: 22,
    padding: 16,
    gap: 12,
  },
  cardTitle: {
    fontSize: 16,
    fontWeight: '700',
  },
  kvGroup: {
    gap: 6,
  },
  serverName: {
    fontSize: 16,
    fontWeight: '700',
  },
  addrTxt: {
    fontSize: 12.5,
  },
  badgeRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
    marginTop: 2,
  },
  badge: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  badgeTxt: {
    fontSize: 12,
    fontWeight: '700',
  },
  manageBtn: {
    borderRadius: 999,
    paddingVertical: 11,
    alignItems: 'center',
    justifyContent: 'center',
  },
  manageBtnTxt: {
    fontSize: 14,
    fontWeight: '600',
  },
  rowBetween: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 10,
  },
  lbl: {
    fontSize: 14,
  },
  val: {
    fontSize: 14,
    fontWeight: '600',
  },
  countTxt: {
    fontSize: 13,
    fontWeight: '700',
  },
  emptyTxt: {
    fontSize: 13.5,
    lineHeight: 20,
  },
  nodeList: {
    gap: 10,
  },
  nodeItem: {
    borderRadius: 18,
    padding: 13,
    gap: 8,
  },
  nodeName: {
    fontSize: 15,
    fontWeight: '700',
  },
  statePill: {
    borderRadius: 999,
    paddingHorizontal: 9,
    paddingVertical: 3,
  },
  statePillTxt: {
    fontSize: 11.5,
    fontWeight: '700',
  },
  nodeMeta: {
    fontSize: 12,
  },
  permRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 6,
  },
  permPill: {
    borderRadius: 999,
    paddingHorizontal: 9,
    paddingVertical: 4,
  },
  permTxt: {
    fontSize: 11.5,
    fontWeight: '600',
  },
});
