import { CameraView, useCameraPermissions, type BarcodeScanningResult } from 'expo-camera';
import * as Haptics from 'expo-haptics';
import { router, useLocalSearchParams } from 'expo-router';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { isNativeTunnelAvailable } from '../../modules/bothub-tunnel';
import { normalizeAddress } from '../api';
import { QrCodeIcon } from '../components/Icons';
import { ModalHeader } from '../components/ModalHeader';
import { useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';

export default function ConnectScreen() {
  const theme = useTheme();
  const {
    servers,
    activeServer,
    connState,
    connError,
    addAndConnectServer,
    selectServer,
    removeServer,
  } = useHub();

  const [permission, requestPermission] = useCameraPermissions();
  const [scanning, setScanning] = useState(false);
  const [label, setLabel] = useState('');
  const [address, setAddress] = useState('');
  const [token, setToken] = useState('');
  const [localError, setLocalError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const {
    address: paramAddr,
    label: paramLabel,
    token: paramToken,
    autoconnect,
  } = useLocalSearchParams<{
    address?: string;
    label?: string;
    token?: string;
    autoconnect?: string;
  }>();

  const scanLockRef = useRef(false);
  const handledParamRef = useRef<string | null>(null);
  const tunnelAvailable = isNativeTunnelAvailable();

  useEffect(() => {
    const rawAddr = typeof paramAddr === 'string' ? paramAddr.trim() : '';
    if (!rawAddr || handledParamRef.current === rawAddr) return;
    handledParamRef.current = rawAddr;
    const norm = normalizeAddress(rawAddr);
    const lbl = typeof paramLabel === 'string' ? paramLabel.trim() : '';
    const tok = typeof paramToken === 'string' ? paramToken.trim() : '';
    if (lbl) setLabel(lbl);
    setAddress(norm.cleanAddress);
    if (norm.token || tok) setToken(tok || norm.token);
    setLocalError('');
    if (autoconnect === '1') {
      setSubmitting(true);
      addAndConnectServer({
        label: lbl || undefined,
        address: rawAddr,
        token: tok || norm.token || undefined,
      }).catch((err: any) => {
        setSubmitting(false);
        setLocalError(err?.message || '连接失败');
      });
    }
  }, [addAndConnectServer, autoconnect, paramAddr, paramLabel, paramToken]);

  useEffect(() => {
    if (activeServer && (connState === 'unauthorized' || connState === 'error')) {
      setLabel((prev) => prev || activeServer.label);
      setAddress((prev) => prev || activeServer.address);
    }
  }, [activeServer, connState]);

  useEffect(() => {
    if (!submitting) return;
    if (connState === 'connected') {
      setSubmitting(false);
      if (router.canGoBack()) {
        router.back();
      } else {
        router.replace('/');
      }
    } else if (connState === 'error' || connState === 'unauthorized') {
      setSubmitting(false);
      setLocalError(connError || '连接失败');
    }
  }, [connError, connState, submitting]);

  const parsedPreview = useMemo(() => {
    if (!address.trim()) return null;
    return normalizeAddress(address);
  }, [address]);

  const closeSheet = () => {
    if (router.canGoBack()) {
      router.back();
    } else {
      router.replace('/');
    }
  };

  const toggleScanner = async () => {
    setLocalError('');
    if (scanning) {
      setScanning(false);
      return;
    }
    if (!permission?.granted) {
      const res = await requestPermission();
      if (!res.granted) {
        setLocalError('需要相机权限才能扫描配对二维码');
        return;
      }
    }
    scanLockRef.current = false;
    setScanning(true);
  };

  const onBarcodeScanned = (result: BarcodeScanningResult) => {
    if (scanLockRef.current) return;
    const raw = (result.data || '').trim();
    if (!raw) return;

    const lower = raw.toLowerCase();
    if (!lower.startsWith('tailcat://') && !lower.startsWith('http://') && !lower.startsWith('https://')) {
      setLocalError('二维码不是有效的 Bot Hub 连接链接');
      return;
    }

    scanLockRef.current = true;
    if (Platform.OS !== 'web') {
      Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success).catch(() => {});
    }
    setScanning(false);
    const norm = normalizeAddress(raw);
    setAddress(norm.cleanAddress);
    if (norm.token) setToken(norm.token);
    setLocalError('');
    setSubmitting(true);
    addAndConnectServer({
      label: label.trim() || undefined,
      address: raw,
      token: norm.token || token.trim(),
    }).catch((err: any) => {
      setSubmitting(false);
      setLocalError(err?.message || '连接失败');
    });
  };

  const onSubmit = async () => {
    setLocalError('');
    if (!address.trim()) {
      setLocalError('请输入连接地址或扫描二维码');
      return;
    }
    setSubmitting(true);
    try {
      await addAndConnectServer({
        label: label.trim() || undefined,
        address: address.trim(),
        token: token.trim() || undefined,
      });
    } catch (err: any) {
      setSubmitting(false);
      setLocalError(err?.message || '连接失败');
    }
  };

  const onPickServer = async (id: string) => {
    setLocalError('');
    setSubmitting(true);
    try {
      await selectServer(id);
    } catch (err: any) {
      setSubmitting(false);
      setLocalError(err?.message || '切换失败');
    }
  };

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader title="连接主电脑" onClose={closeSheet} />
      <KeyboardAvoidingView
        style={styles.flex}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <ScrollView
          contentContainerStyle={styles.scroll}
          keyboardShouldPersistTaps="handled"
        >
          {Platform.OS !== 'web' && (
            <View style={[styles.card, { backgroundColor: theme.card }]}>
              <View style={styles.rowBetween}>
                <View style={styles.col}>
                  <Text style={[styles.cardTitle, { color: theme.text }]}>扫码配对</Text>
                  <Text style={[styles.cardSub, { color: theme.text2 }]}>
                    扫描电脑端「远程访问」二维码即可自动带入口令
                  </Text>
                </View>
                <Pressable
                  onPress={toggleScanner}
                  style={[styles.qrBtn, { backgroundColor: theme.accentSoft }]}
                >
                  <QrCodeIcon size={17} color={theme.accent} />
                  <Text style={[styles.qrBtnTxt, { color: theme.accent }]}>
                    {scanning ? '收起' : '扫码连接'}
                  </Text>
                </Pressable>
              </View>

              {scanning && (
                <View style={styles.camWrap}>
                  <CameraView
                    style={styles.cam}
                    facing="back"
                    barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
                    onBarcodeScanned={onBarcodeScanned}
                  />
                </View>
              )}
            </View>
          )}

          {!tunnelAvailable && (
            <View style={[styles.noticeCard, { backgroundColor: theme.s1 }]}>
              <Text style={[styles.noticeTxt, { color: theme.text2 }]}>
                {Platform.OS === 'web'
                  ? '当前为浏览器预览模式，请直接填写 http(s):// 地址；iOS / Android 原生构建支持扫码与 Tailcat 加密隧道。'
                  : 'Expo Go 不含 Tailcat 原生隧道模块；使用 npx expo run:ios 或 run:android 构建后即可扫码直连 tailcat://。'}
              </Text>
            </View>
          )}

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.cardTitle, { color: theme.text }]}>手动输入地址</Text>

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>名称（选填）</Text>
              <TextInput
                value={label}
                onChangeText={setLabel}
                placeholder="家里 Mac mini"
                placeholderTextColor={theme.mute}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>连接地址</Text>
              <TextInput
                value={address}
                onChangeText={setAddress}
                placeholder="tailcat://connect/... 或 http://192.168.1.x:3789"
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
              <Text style={[styles.fieldHint, { color: parsedPreview?.token ? theme.sageInk : theme.mute }]}>
                {parsedPreview?.token
                  ? '已从链接自动识别访问口令'
                  : '若粘贴带 #token= 的配对链接会自动填入'}
              </Text>
            </View>

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>访问口令（apiToken，选填）</Text>
              <TextInput
                value={token}
                onChangeText={setToken}
                placeholder="主电脑已开启鉴权时填写"
                placeholderTextColor={theme.mute}
                secureTextEntry
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
            </View>

            {localError ? (
              <Text style={[styles.errTxt, { color: theme.danger }]}>{localError}</Text>
            ) : null}

            <Pressable
              disabled={submitting}
              onPress={onSubmit}
              style={[styles.submitBtn, { backgroundColor: theme.accent, opacity: submitting ? 0.7 : 1 }]}
            >
              {submitting ? (
                <ActivityIndicator size="small" color={theme.accentText} />
              ) : (
                <Text style={[styles.submitTxt, { color: theme.accentText }]}>连接</Text>
              )}
            </Pressable>
          </View>

          {servers.length > 0 && (
            <View style={[styles.card, { backgroundColor: theme.card }]}>
              <Text style={[styles.cardTitle, { color: theme.text }]}>已保存的主电脑</Text>
              <View style={styles.savedList}>
                {servers.map((s) => {
                  const isCurrent = activeServer?.id === s.id;
                  return (
                    <View key={s.id} style={[styles.savedRow, { backgroundColor: theme.s1 }]}>
                      <Pressable onPress={() => void onPickServer(s.id)} style={styles.savedInfo}>
                        <View style={styles.savedHead}>
                          <Text numberOfLines={1} style={[styles.savedName, { color: theme.text }]}>
                            {s.label}
                          </Text>
                          {isCurrent && (
                            <View style={[styles.curBadge, { backgroundColor: theme.sage }]}>
                              <Text style={[styles.curBadgeTxt, { color: theme.sageInk }]}>当前</Text>
                            </View>
                          )}
                        </View>
                        <Text numberOfLines={1} style={[styles.savedAddr, { color: theme.mute, fontFamily: MONO_FONT }]}>
                          {s.address}
                        </Text>
                      </Pressable>
                      <Pressable onPress={() => void removeServer(s.id)} hitSlop={8} style={styles.delBtn}>
                        <Text style={[styles.delTxt, { color: theme.danger }]}>移除</Text>
                      </Pressable>
                    </View>
                  );
                })}
              </View>
            </View>
          )}
        </ScrollView>
      </KeyboardAvoidingView>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
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
  rowBetween: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  col: {
    flex: 1,
    gap: 3,
  },
  cardTitle: {
    fontSize: 16,
    fontWeight: '700',
  },
  cardSub: {
    fontSize: 13,
    lineHeight: 18,
  },
  qrBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingHorizontal: 13,
    paddingVertical: 9,
    borderRadius: 999,
  },
  qrBtnTxt: {
    fontSize: 13.5,
    fontWeight: '700',
  },
  camWrap: {
    height: 240,
    borderRadius: 18,
    overflow: 'hidden',
  },
  cam: {
    flex: 1,
  },
  noticeCard: {
    borderRadius: 16,
    paddingHorizontal: 14,
    paddingVertical: 11,
  },
  noticeTxt: {
    fontSize: 13,
    lineHeight: 18.5,
  },
  field: {
    gap: 6,
  },
  fieldLbl: {
    fontSize: 13,
    fontWeight: '600',
  },
  fieldHint: {
    fontSize: 12,
  },
  input: {
    borderRadius: 14,
    paddingHorizontal: 13,
    paddingVertical: 11,
    fontSize: 16,
  },
  errTxt: {
    fontSize: 13,
    lineHeight: 18,
  },
  submitBtn: {
    borderRadius: 999,
    paddingVertical: 13,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 2,
  },
  submitTxt: {
    fontSize: 15,
    fontWeight: '700',
  },
  savedList: {
    gap: 8,
  },
  savedRow: {
    borderRadius: 16,
    paddingHorizontal: 13,
    paddingVertical: 11,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  savedInfo: {
    flex: 1,
    minWidth: 0,
    gap: 3,
  },
  savedHead: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  savedName: {
    fontSize: 14.5,
    fontWeight: '600',
    flexShrink: 1,
  },
  curBadge: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 2,
  },
  curBadgeTxt: {
    fontSize: 11,
    fontWeight: '700',
  },
  savedAddr: {
    fontSize: 12,
  },
  delBtn: {
    paddingHorizontal: 6,
    paddingVertical: 4,
  },
  delTxt: {
    fontSize: 13,
    fontWeight: '600',
  },
});
