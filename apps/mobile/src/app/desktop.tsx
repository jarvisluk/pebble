import { router, useLocalSearchParams } from 'expo-router';
import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Image,
  KeyboardAvoidingView,
  LayoutChangeEvent,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { ModalHeader } from '../components/ModalHeader';
import { useI18n } from '../i18n';
import { useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';
import type { DesktopFrame, DesktopTarget, ScreenState } from '../types';

export default function DesktopScreen() {
  const { target: initialTarget } = useLocalSearchParams<{ target?: string }>();
  const theme = useTheme();
  const { t } = useI18n();
  const {
    desktopTargets,
    takeovers,
    refreshDesktopTargets,
    fetchDesktopState,
    fetchDesktopFrame,
    sendDesktopInput,
    resolveTakeover,
  } = useHub();

  const [selectedTarget, setSelectedTarget] = useState(
    typeof initialTarget === 'string' && initialTarget ? initialTarget : 'local'
  );
  const [frame, setFrame] = useState<DesktopFrame | null>(null);
  const [screen, setScreen] = useState<ScreenState>({ target: 'local', owner: 'idle' });
  const [streaming, setStreaming] = useState(true);
  const [error, setError] = useState('');
  const [boxSize, setBoxSize] = useState({ width: 0, height: 0 });
  const [textDraft, setTextDraft] = useState('');
  const [showTextBar, setShowTextBar] = useState(false);

  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    void refreshDesktopTargets().catch(() => {});
    return () => {
      mountedRef.current = false;
    };
  }, [refreshDesktopTargets]);

  useEffect(() => {
    let stop = false;
    const pollState = async () => {
      try {
        const st = await fetchDesktopState(selectedTarget);
        if (!stop && st?.screen) setScreen(st.screen);
      } catch {}
    };
    void pollState();
    const timer = setInterval(pollState, 2000);
    return () => {
      stop = true;
      clearInterval(timer);
    };
  }, [fetchDesktopState, selectedTarget]);

  useEffect(() => {
    if (!streaming) return;
    let cancelled = false;
    const loop = async () => {
      while (!cancelled && mountedRef.current) {
        try {
          const next = await fetchDesktopFrame(selectedTarget);
          if (cancelled) break;
          setFrame(next);
          setError('');
        } catch (err: any) {
          if (cancelled) break;
          setError(err?.message || t('captureFailed'));
          await new Promise((r) => setTimeout(r, 1500));
        }
        await new Promise((r) => setTimeout(r, 140));
      }
    };
    void loop();
    return () => {
      cancelled = true;
    };
  }, [fetchDesktopFrame, selectedTarget, streaming, t]);

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
      if (!frame || !boxSize.width || !boxSize.height || !frame.imageWidth || !frame.imageHeight) {
        return null;
      }
      const scale = Math.min(boxSize.width / frame.imageWidth, boxSize.height / frame.imageHeight);
      const drawW = frame.imageWidth * scale;
      const drawH = frame.imageHeight * scale;
      const offX = (boxSize.width - drawW) / 2;
      const offY = (boxSize.height - drawH) / 2;
      const rx = (locX - offX) / drawW;
      const ry = (locY - offY) / drawH;
      if (rx < 0 || rx > 1 || ry < 0 || ry > 1) return null;
      return {
        x: (frame.logicalX || 0) + rx * frame.logicalWidth,
        y: (frame.logicalY || 0) + ry * frame.logicalHeight,
      };
    },
    [boxSize.height, boxSize.width, frame]
  );

  const sendInput = useCallback(
    async (payload: Record<string, unknown>) => {
      try {
        await sendDesktopInput({ target: selectedTarget, ...payload });
      } catch (err: any) {
        setError(err?.message || t('opFailed'));
      }
    },
    [selectedTarget, sendDesktopInput, t]
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
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader
        title={t('remoteDesktop')}
        onClose={closeSheet}
        rightLabel={streaming ? t('pause') : t('resume')}
        onRightPress={() => setStreaming((v) => !v)}
      />

      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <View style={styles.topStrip}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.targetRow}>
            {targets.map((item) => {
              const active = selectedTarget === item.id;
              return (
                <Pressable
                  key={item.id}
                  onPress={() => {
                    setSelectedTarget(item.id);
                    setFrame(null);
                  }}
                  style={[
                    styles.targetPill,
                    { backgroundColor: active ? theme.accentSoft : theme.card },
                  ]}
                >
                  <Text style={[styles.targetTxt, { color: active ? theme.accent : theme.text2 }]}>
                    {item.name}
                  </Text>
                </Pressable>
              );
            })}
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
          {frame?.jpegBase64 ? (
            <Pressable
              style={styles.touchLayer}
              onPress={(e) => onTapScreen(e.nativeEvent.locationX, e.nativeEvent.locationY, 'click')}
              onLongPress={(e) => onTapScreen(e.nativeEvent.locationX, e.nativeEvent.locationY, 'right_click')}
            >
              <Image
                source={{ uri: `data:image/jpeg;base64,${frame.jpegBase64}` }}
                style={styles.stageImage}
                resizeMode="contain"
              />
            </Pressable>
          ) : (
            <View style={styles.centerBox}>
              <ActivityIndicator size="small" color={theme.accent} />
              <Text style={styles.waitTxt}>{error || t('fetchingDesktop')}</Text>
            </View>
          )}
          {frame && error ? (
            <View style={styles.errFloat}>
              <Text style={styles.errFloatTxt}>{error}</Text>
            </View>
          ) : null}
        </View>

        <View style={[styles.controlsBar, { backgroundColor: theme.card }]}>
          {showTextBar && (
            <View style={styles.typeRow}>
              <TextInput
                value={textDraft}
                onChangeText={setTextDraft}
                placeholder={t('typeDesktopPh')}
                placeholderTextColor={theme.mute}
                onSubmitEditing={() => void onSendText()}
                style={[styles.typeInput, { backgroundColor: theme.s1, color: theme.text }]}
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
              style={[styles.keyBtn, { backgroundColor: showTextBar ? theme.accentSoft : theme.s1 }]}
            >
              <Text style={[styles.keyTxt, { color: showTextBar ? theme.accent : theme.text }]}>{t('aaInput')}</Text>
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
                style={[styles.keyBtn, { backgroundColor: theme.s1 }]}
              >
                <Text style={[styles.keyTxt, { color: theme.text2, fontFamily: MONO_FONT }]}>{item.label}</Text>
              </Pressable>
            ))}
          </ScrollView>
        </View>
      </KeyboardAvoidingView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
  topStrip: { paddingHorizontal: 14, paddingBottom: 8 },
  targetRow: { gap: 8 },
  targetPill: { borderRadius: 999, paddingHorizontal: 13, paddingVertical: 7 },
  targetTxt: { fontSize: 13, fontWeight: '700' },
  alertCard: {
    marginHorizontal: 14,
    marginBottom: 8,
    borderRadius: 16,
    paddingHorizontal: 12,
    paddingVertical: 10,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 10,
  },
  alertTxt: { flex: 1, fontSize: 13, fontWeight: '600' },
  alertBtn: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 7 },
  alertBtnTxt: { fontSize: 12.5, fontWeight: '700' },
  stageWrap: {
    flex: 1,
    marginHorizontal: 12,
    borderRadius: 20,
    backgroundColor: '#111110',
    overflow: 'hidden',
    position: 'relative',
  },
  touchLayer: { flex: 1 },
  stageImage: { width: '100%', height: '100%' },
  centerBox: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 10, padding: 24 },
  waitTxt: { color: '#a6a09a', fontSize: 13.5, textAlign: 'center' },
  errFloat: {
    position: 'absolute',
    top: 10,
    left: 10,
    right: 10,
    backgroundColor: 'rgba(59, 18, 18, 0.9)',
    borderRadius: 12,
    padding: 8,
  },
  errFloatTxt: { color: '#fecaca', fontSize: 12 },
  controlsBar: {
    marginTop: 8,
    marginHorizontal: 12,
    marginBottom: 14,
    borderRadius: 20,
    padding: 10,
    gap: 8,
  },
  typeRow: { flexDirection: 'row', gap: 8, alignItems: 'center' },
  typeInput: { flex: 1, borderRadius: 12, paddingHorizontal: 12, paddingVertical: 8, fontSize: 15 },
  keysRow: { gap: 8, alignItems: 'center' },
  keyBtn: { borderRadius: 999, paddingHorizontal: 12, paddingVertical: 7 },
  keyTxt: { fontSize: 12.5, fontWeight: '600' },
});
