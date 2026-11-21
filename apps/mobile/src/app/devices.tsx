import { router } from 'expo-router';
import React, { useCallback, useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';

import { ModalHeader } from '../components/ModalHeader';
import { useI18n } from '../i18n';
import { useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';

export default function DevicesScreen() {
  const theme = useTheme();
  const { t } = useI18n();
  const permLabels: Record<string, string> = {
    files: t('permFiles'),
    exec: t('permExec'),
    desktop: t('permDesktop'),
  };
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
    if (!tailcat) return t('tailcatUnknown');
    if (tailcat.state === 'ready') return t('tailcatReady');
    if (tailcat.state === 'connecting') return t('tailcatConnecting');
    if (tailcat.state === 'disabled') return t('tailcatDisabled');
    return tailcat.error || t('tailcatErr');
  })();

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader title={t('devicesTunnel')} onClose={closeSheet} />
      <ScrollView
        contentContainerStyle={styles.scroll}
        refreshControl={
          <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={theme.accent} />
        }
      >
        <View style={[styles.card, { backgroundColor: theme.card }]}>
          <Text style={[styles.cardTitle, { color: theme.text }]}>{t('currentMobileConn')}</Text>
          {activeServer ? (
            <View style={styles.kvGroup}>
              <Text style={[styles.serverName, { color: theme.text }]}>{activeServer.label}</Text>
              <Text selectable style={[styles.addrTxt, { color: theme.mute, fontFamily: MONO_FONT }]}>
                {activeServer.address}
              </Text>
              <View style={styles.badgeRow}>
                <View style={[styles.badge, { backgroundColor: isTailcat ? theme.lilac : theme.s1 }]}>
                  <Text style={[styles.badgeTxt, { color: isTailcat ? theme.lilacInk : theme.text2 }]}>
                    {isTailcat ? t('tailcatE2E') : t('httpLan')}
                  </Text>
                </View>
                <View style={[styles.badge, { backgroundColor: online ? theme.sage : theme.s1 }]}>
                  <Text style={[styles.badgeTxt, { color: online ? theme.sageInk : theme.busy }]}>
                    {online ? t('streamConnected') : t('reconnecting')}
                  </Text>
                </View>
              </View>
            </View>
          ) : (
            <Text style={[styles.emptyTxt, { color: theme.mute }]}>{t('notConnectedHost')}</Text>
          )}
          <Pressable
            onPress={() => router.push('/connect')}
            style={[styles.manageBtn, { backgroundColor: theme.s1 }]}
          >
            <Text style={[styles.manageBtnTxt, { color: theme.text }]}>{t('manageConn')}</Text>
          </Pressable>
          {activeServer && (
            <Pressable
              onPress={() => router.push('/desktop?target=local')}
              style={[styles.manageBtn, { backgroundColor: theme.accentSoft }]}
            >
              <Text style={[styles.manageBtnTxt, { color: theme.accent }]}>{t('openHostDesktop')}</Text>
            </Pressable>
          )}
        </View>

        <View style={[styles.card, { backgroundColor: theme.card }]}>
          <Text style={[styles.cardTitle, { color: theme.text }]}>{t('hostRemoteAccess')}</Text>
          <View style={styles.rowBetween}>
            <Text style={[styles.lbl, { color: theme.text2 }]}>{t('tunnelState')}</Text>
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
            <Text style={[styles.lbl, { color: theme.text2 }]}>{t('tokenProtect')}</Text>
            <Text style={[styles.val, { color: tailcat?.hasToken ? theme.sageInk : theme.mute }]}>
              {tailcat?.hasToken ? t('tokenEnabled') : t('tokenUnset')}
            </Text>
          </View>
        </View>

        <View style={[styles.card, { backgroundColor: theme.card }]}>
          <View style={styles.rowBetween}>
            <Text style={[styles.cardTitle, { color: theme.text }]}>{t('connectedNodes')}</Text>
            <Text style={[styles.countTxt, { color: theme.mute }]}>{nodes.length}</Text>
          </View>

          {nodes.length === 0 ? (
            <Text style={[styles.emptyTxt, { color: theme.text2 }]}>
              {t('noRemoteNodes')}
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
                        {node.connected ? t('online') : t('offline')}
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
                            {permLabels[p] || p}
                          </Text>
                        </View>
                      ))}
                      {node.connected && node.permissions.includes('desktop') && (
                        <Pressable
                          onPress={() => router.push(`/desktop?target=${encodeURIComponent(node.id)}`)}
                          style={[styles.permPill, { backgroundColor: theme.accentSoft }]}
                        >
                          <Text style={[styles.permTxt, { color: theme.accent }]}>{t('openDesktop')}</Text>
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
