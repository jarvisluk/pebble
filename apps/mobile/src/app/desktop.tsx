import { router, useLocalSearchParams } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  LayoutChangeEvent,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import {
  BothubH264View,
  type BothubH264ViewRef,
  type H264MetaEvent,
} from '../../modules/bothub-tunnel';
import { DesktopChatSidebar } from '../components/DesktopChatSidebar';
import { XIcon } from '../components/Icons';
import { useI18n } from '../i18n';
import { useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';
import type { DesktopTarget, ScreenState } from '../types';

export default function DesktopScreen() {
  const { target: initialTarget } = useLocalSearchParams<{ target?: string }>();
  const theme = useTheme();
  const { t } = useI18n();
  const insets = useSafeAreaInsets();
  const { width: winWidth } = useWindowDimensions();
  const isWide = winWidth >= 768;

  const {
    baseURL,
    authToken,
    bots,
    desktopTargets,
    takeovers,
    refreshDesktopTargets,
    fetchDesktopState,
    sendDesktopInput,
    resolveTakeover,
  } = useHub();

  const [selectedTarget, setSelectedTarget] = useState(
    typeof initialTarget === 'string' && initialTarget ? initialTarget : 'local'
  );
  const [meta, setMeta] = useState<H264MetaEvent | null>(null);
  const [connected, setConnected] = useState(false);
  const [fps, setFps] = useState(0);
  const [screen, setScreen] = useState<ScreenState>({ target: 'local', owner: 'idle' });
  const [streaming, setStreaming] = useState(true);
  const [error, setError] = useState('');
  const [boxSize, setBoxSize] = useState({ width: 0, height: 0 });
  const [textDraft, setTextDraft] = useState('');
  const [showTextBar, setShowTextBar] = useState(false);
  const [chatOpen, setChatOpen] = useState(false);
  const [chatBotId, setChatBotId] = useState('');

  const h264Ref = useRef<BothubH264ViewRef | null>(null);

  const resolvedBotId = useMemo(() => {
    if (chatBotId && bots.some((b) => b.id === chatBotId)) return chatBotId;
    return bots[0]?.id || '';
  }, [bots, chatBotId]);

  useEffect(() => {
    void refreshDesktopTargets().catch(() => {});
  }, [refreshDesktopTargets]);

  useEffect(() => {
    let stop = false;
    const pollState = async () => {
      try {
        const st = await fetchDesktopState(selectedTarget);
        if (stop) return;
        if (st?.screen) setScreen(st.screen);
        if (st?.status?.screenRecording === 'denied') {
          setStreaming(false);
          setError(t('deskScreenRecordingDenied'));
        }
      } catch {}
    };
    void pollState();
    const timer = setInterval(pollState, 2000);
    return () => {
      stop = true;
      clearInterval(timer);
    };
  }, [fetchDesktopState, selectedTarget, t]);

  const wsURL = useMemo(() => {
    if (!baseURL) return '';
    const wsBase = baseURL.replace(/^https:\/\//i, 'wss://').replace(/^http:\/\//i, 'ws://');
    return `${wsBase}/api/desktop/stream/ws?target=${encodeURIComponent(selectedTarget)}&codec=hevc`;
  }, [baseURL, selectedTarget]);

  const activeTakeover = useMemo(
    () => takeovers.find((item) => item.target === selectedTarget) || takeovers[0] || null,
    [selectedTarget, takeovers]
  );

  const targets: DesktopTarget[] = useMemo(() => {
    if (desktopTargets.length > 0) return desktopTargets;
    return [{ id: 'local', name: t('localDesktopName'), local: true, supported: true, screen }];
  }, [desktopTargets, screen, t]);

  const onStageLayout = (e: LayoutChangeEvent) => {
    const { width, height } = e.nativeEvent.layout;
    setBoxSize({ width, height });
  };

  const mapTouchToLogical = useCallback(
    (locX: number, locY: number): { x: number; y: number } | null => {
      if (!meta || !boxSize.width || !boxSize.height || !meta.imageWidth || !meta.imageHeight) {
        return null;
      }
      const scale = Math.min(boxSize.width / meta.imageWidth, boxSize.height / meta.imageHeight);
      const drawW = meta.imageWidth * scale;
      const drawH = meta.imageHeight * scale;
      const offX = (boxSize.width - drawW) / 2;
      const offY = (boxSize.height - drawH) / 2;
      const rx = (locX - offX) / drawW;
      const ry = (locY - offY) / drawH;
      if (rx < 0 || rx > 1 || ry < 0 || ry > 1) return null;
      return {
        x: (meta.logicalX || 0) + rx * meta.logicalWidth,
        y: (meta.logicalY || 0) + ry * meta.logicalHeight,
      };
    },
    [boxSize.height, boxSize.width, meta]
  );

  const sendInput = useCallback(
    async (payload: Record<string, unknown>) => {
      try {
        if (connected && h264Ref.current?.sendInput) {
          await h264Ref.current.sendInput(JSON.stringify(payload));
          return;
        }
        await sendDesktopInput({ target: selectedTarget, ...payload });
      } catch (err: any) {
        setError(err?.message || t('opFailed'));
      }
    },
    [connected, selectedTarget, sendDesktopInput, t]
  );

  const onTapScreen = (locX: number, locY: number, kind: 'click' | 'right_click') => {
    const pt = mapTouchToLogical(locX, locY);
    if (pt) {
      void sendInput({ kind, x: pt.x, y: pt.y });
    }
  };

  const onSendText = async () => {
    const txt = textDraft;
    if (!txt) return;
    setTextDraft('');
    await sendInput({ kind: 'type', text: txt });
  };

  const closeSheet = () => {
    if (router.canGoBack()) router.back();
    else router.replace('/');
  };

  return (
    <View style={[styles.root, { backgroundColor: '#111110' }]}>
      <View style={styles.mainArea}>
        <KeyboardAvoidingView
          style={[styles.stageCol, { paddingTop: insets.top + 6, paddingBottom: insets.bottom }]}
          behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        >
          <View style={styles.topStrip}>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.targetRow}>
              <Pressable onPress={closeSheet} style={[styles.pillBtn, { backgroundColor: '#1f1d1b' }]}>
                <XIcon size={16} color="#f3f1ee" />
              </Pressable>

              {targets.map((item) => {
                const active = selectedTarget === item.id;
                return (
                  <Pressable
                    key={item.id}
                    onPress={() => {
                      setSelectedTarget(item.id);
                      setMeta(null);
                      setConnected(false);
                    }}
                    style={[
                      styles.targetPill,
                      { backgroundColor: active ? theme.accent : '#1f1d1b' },
                    ]}
                  >
                    <Text style={[styles.targetTxt, { color: active ? theme.accentText : '#f3f1ee' }]}>
                      {item.id === 'local' ? t('localDesktopName') : item.name}
                    </Text>
                  </Pressable>
                );
              })}

              {connected && (
                <View style={[styles.targetPill, { backgroundColor: '#1f1d1b' }]}>
                  <Text style={[styles.targetTxt, { color: '#a6a09a', fontFamily: MONO_FONT }]}>
                    {`H.264 · ${fps} fps`}
                  </Text>
                </View>
              )}

              <Pressable
                onPress={() => setStreaming((v) => !v)}
                style={[styles.targetPill, { backgroundColor: '#1f1d1b' }]}
              >
                <Text style={[styles.targetTxt, { color: '#f3f1ee' }]}>
                  {streaming ? t('pause') : t('resume')}
                </Text>
              </Pressable>

              <Pressable
                onPress={() => setChatOpen((v) => !v)}
                style={[
                  styles.targetPill,
                  { backgroundColor: chatOpen ? theme.accent : '#1f1d1b' },
                ]}
              >
                <Text style={[styles.targetTxt, { color: chatOpen ? theme.accentText : '#f3f1ee' }]}>
                  {t('deskChatSidebar')}
                </Text>
              </Pressable>
            </ScrollView>
          </View>

          {activeTakeover ? (
            <View style={[styles.alertCard, { backgroundColor: theme.pink }]}>
              <Text style={[styles.alertTxt, { color: theme.pinkInk }]}>
                {t('takeoverBanner', { bot: activeTakeover.botName || activeTakeover.botId, reason: activeTakeover.reason })}
              </Text>
              <Pressable
                onPress={() => void resolveTakeover(activeTakeover.id)}
                style={[styles.alertBtn, { backgroundColor: theme.accent }]}
              >
                <Text style={[styles.alertBtnTxt, { color: theme.accentText }]}>{t('doneResumeAI')}</Text>
              </Pressable>
            </View>
          ) : null}

          <View style={styles.stageWrap} onLayout={onStageLayout}>
            {Platform.OS !== 'web' && wsURL ? (
              <Pressable
                style={styles.touchLayer}
                onPress={(e) => onTapScreen(e.nativeEvent.locationX, e.nativeEvent.locationY, 'click')}
                onLongPress={(e) => onTapScreen(e.nativeEvent.locationX, e.nativeEvent.locationY, 'right_click')}
              >
                <BothubH264View
                  ref={h264Ref as any}
                  style={styles.stageImage}
                  url={wsURL}
                  token={authToken}
                  preset="auto"
                  active={streaming}
                  onMeta={(ev) => {
                    setMeta(ev.nativeEvent);
                    setError('');
                  }}
                  onStatus={(ev) => {
                    setConnected(ev.nativeEvent.connected);
                    setFps(ev.nativeEvent.fps);
                  }}
                  onStreamError={(ev) => {
                    const msg = ev.nativeEvent.error || t('captureFailed');
                    setError(msg);
                    if (ev.nativeEvent.code === 'screen_recording_denied') {
                      setStreaming(false);
                    }
                  }}
                />
              </Pressable>
            ) : (
              <View style={styles.centerBox}>
                <ActivityIndicator size="small" color={theme.accent} />
                <Text style={styles.waitTxt}>{error || t('deskH264Connecting')}</Text>
              </View>
            )}
            {!connected && Platform.OS !== 'web' && (
              <View style={[styles.centerBox, StyleSheet.absoluteFill]} pointerEvents="none">
                <ActivityIndicator size="small" color={theme.accent} />
                <Text style={styles.waitTxt}>{error || t('deskH264Connecting')}</Text>
              </View>
            )}
            {connected && error ? (
              <View style={styles.errFloat}>
                <Text style={styles.errFloatTxt}>{error}</Text>
              </View>
            ) : null}
          </View>

          <View style={[styles.controlsBar, { backgroundColor: '#1f1d1b' }]}>
            {showTextBar && (
              <View style={styles.typeRow}>
                <TextInput
                  value={textDraft}
                  onChangeText={setTextDraft}
                  placeholder={t('typeDesktopPh')}
                  placeholderTextColor="#a6a09a"
                  onSubmitEditing={() => void onSendText()}
                  style={[styles.typeInput, { backgroundColor: '#2c2926', color: '#f3f1ee' }]}
                />
                <Pressable
                  onPress={() => void onSendText()}
                  style={[styles.keyBtn, { backgroundColor: theme.accent }]}
                >
                  <Text style={[styles.keyTxt, { color: theme.accentText }]}>{t('send')}</Text>
                </Pressable>
              </View>
            )}

            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.keysRow}>
              <Pressable
                onPress={() => setShowTextBar((v) => !v)}
                style={[styles.keyBtn, { backgroundColor: showTextBar ? theme.accent : '#2c2926' }]}
              >
                <Text style={[styles.keyTxt, { color: showTextBar ? theme.accentText : '#f3f1ee' }]}>{t('aaInput')}</Text>
              </Pressable>
              {[
                { label: t('keyEnter'), body: { kind: 'key', key: 'Return' } },
                { label: t('keyBackspace'), body: { kind: 'key', key: 'Backspace' } },
                { label: 'Esc', body: { kind: 'key', key: 'Escape' } },
                { label: 'Tab', body: { kind: 'key', key: 'Tab' } },
                { label: t('keyScrollUp'), body: { kind: 'scroll', deltaY: 8 } },
                { label: t('keyScrollDown'), body: { kind: 'scroll', deltaY: -8 } },
              ].map((item) => (
                <Pressable
                  key={item.label}
                  onPress={() => void sendInput(item.body)}
                  style={[styles.keyBtn, { backgroundColor: '#2c2926' }]}
                >
                  <Text style={[styles.keyTxt, { color: '#ccc7c1', fontFamily: MONO_FONT }]}>{item.label}</Text>
                </Pressable>
              ))}
            </ScrollView>
          </View>
        </KeyboardAvoidingView>

        {isWide && chatOpen && resolvedBotId ? (
          <View style={styles.sideInline}>
            <DesktopChatSidebar
              botId={resolvedBotId}
              onSelectBot={setChatBotId}
              onClose={() => setChatOpen(false)}
            />
          </View>
        ) : null}
      </View>

      {!isWide && chatOpen && resolvedBotId ? (
        <View style={[StyleSheet.absoluteFill, styles.drawerWrap]}>
          <Pressable style={styles.drawerShade} onPress={() => setChatOpen(false)} />
          <View style={[styles.drawerPanel, { width: Math.min(380, winWidth * 0.9) }]}>
            <DesktopChatSidebar
              botId={resolvedBotId}
              onSelectBot={setChatBotId}
              onClose={() => setChatOpen(false)}
            />
          </View>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  mainArea: { flex: 1, flexDirection: 'row' },
  stageCol: { flex: 1, minWidth: 0 },
  sideInline: { width: 360 },
  drawerWrap: { flexDirection: 'row', justifyContent: 'flex-end', zIndex: 20 },
  drawerShade: { position: 'absolute', top: 0, right: 0, bottom: 0, left: 0, backgroundColor: 'rgba(0,0,0,0.45)' },
  drawerPanel: { height: '100%' },
  topStrip: { paddingHorizontal: 10, paddingBottom: 6 },
  targetRow: { gap: 8, alignItems: 'center' },
  pillBtn: { width: 34, height: 34, borderRadius: 17, alignItems: 'center', justifyContent: 'center' },
  targetPill: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 7 },
  targetTxt: { fontSize: 13.5, fontWeight: '700' },
  alertCard: {
    marginHorizontal: 12,
    marginBottom: 8,
    borderRadius: 14,
    paddingHorizontal: 12,
    paddingVertical: 8,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 10,
  },
  alertTxt: { flex: 1, fontSize: 14, fontWeight: '600' },
  alertBtn: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 6 },
  alertBtnTxt: { fontSize: 13, fontWeight: '700' },
  stageWrap: {
    flex: 1,
    backgroundColor: '#111110',
    overflow: 'hidden',
    position: 'relative',
  },
  touchLayer: { flex: 1 },
  stageImage: { width: '100%', height: '100%' },
  centerBox: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 10, padding: 24 },
  waitTxt: { color: '#a6a09a', fontSize: 15, textAlign: 'center' },
  errFloat: {
    position: 'absolute',
    top: 10,
    left: 10,
    right: 10,
    backgroundColor: 'rgba(59, 18, 18, 0.9)',
    borderRadius: 12,
    padding: 8,
  },
  errFloatTxt: { color: '#fecaca', fontSize: 13 },
  controlsBar: {
    marginTop: 6,
    marginHorizontal: 10,
    marginBottom: 8,
    borderRadius: 18,
    padding: 8,
    gap: 8,
  },
  typeRow: { flexDirection: 'row', gap: 8, alignItems: 'center' },
  typeInput: { flex: 1, borderRadius: 12, paddingHorizontal: 12, paddingVertical: 8, fontSize: 16 },
  keysRow: { gap: 8, alignItems: 'center' },
  keyBtn: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 7 },
  keyTxt: { fontSize: 13, fontWeight: '600' },
});
