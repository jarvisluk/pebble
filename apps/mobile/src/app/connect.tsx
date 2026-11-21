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
import { useI18n } from '../i18n';
import { useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';

export default function ConnectScreen() {
  const theme = useTheme();
  const { t } = useI18n();
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
    scan: paramScan,
  } = useLocalSearchParams<{
    address?: string;
    label?: string;
    token?: string;
    autoconnect?: string;
    scan?: string;
  }>();

  const scanLockRef = useRef(false);
  const handledParamRef = useRef<string | null>(null);
  const handledScanRef = useRef(false);
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
        setLocalError(err?.message || t('connFailed'));
      });
    }
  }, [addAndConnectServer, autoconnect, paramAddr, paramLabel, paramToken, t]);

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
      setLocalError(connError || t('connFailed'));
    }
  }, [connError, connState, submitting, t]);

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
        setLocalError(t('camPermReq'));
        return;
      }
    }
    scanLockRef.current = false;
    setScanning(true);
  };

  useEffect(() => {
    if (Platform.OS === 'web' || paramScan !== '1' || handledScanRef.current || !permission) return;
    handledScanRef.current = true;
    void (async () => {
      if (!permission.granted) {
        const res = await requestPermission();
        if (!res.granted) {
          setLocalError(t('camPermReq'));
          return;
        }
      }
      scanLockRef.current = false;
      setScanning(true);
    })();
  }, [paramScan, permission, requestPermission, t]);

  const onBarcodeScanned = (result: BarcodeScanningResult) => {
    if (scanLockRef.current) return;
    const raw = (result.data || '').trim();
    if (!raw) return;

    const lower = raw.toLowerCase();
    if (!lower.startsWith('tailcat://') && !lower.startsWith('http://') && !lower.startsWith('https://')) {
      setLocalError(t('invalidQr'));
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
      setLocalError(err?.message || t('connFailed'));
    });
  };

  const onSubmit = async () => {
    setLocalError('');
    if (!address.trim()) {
      setLocalError(t('enterAddrOrScan'));
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
      setLocalError(err?.message || t('connFailed'));
    }
  };

  const onPickServer = async (id: string) => {
    setLocalError('');
    setSubmitting(true);
    try {
      await selectServer(id);
    } catch (err: any) {
      setSubmitting(false);
      setLocalError(err?.message || t('switchFailed'));
    }
  };

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader title={t('connectHost')} onClose={closeSheet} />
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
                  <Text style={[styles.cardTitle, { color: theme.text }]}>{t('scanPairTitle')}</Text>
                  <Text style={[styles.cardSub, { color: theme.text2 }]}>
                    {t('scanPairDesc')}
                  </Text>
                </View>
                <Pressable
                  onPress={toggleScanner}
                  style={[styles.qrBtn, { backgroundColor: theme.accentSoft }]}
                >
                  <QrCodeIcon size={17} color={theme.accent} />
                  <Text style={[styles.qrBtnTxt, { color: theme.accent }]}>
                    {scanning ? t('collapse') : t('scanConnect')}
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
                  ? t('webPreviewNotice')
                  : t('expoGoNotice')}
              </Text>
            </View>
          )}

          <View style={[styles.card, { backgroundColor: theme.card }]}>
            <Text style={[styles.cardTitle, { color: theme.text }]}>{t('manualAddrTitle')}</Text>

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>{t('labelOptional')}</Text>
              <TextInput
                value={label}
                onChangeText={setLabel}
                placeholder={t('labelExamplePh')}
                placeholderTextColor={theme.mute}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>{t('connAddr')}</Text>
              <TextInput
                value={address}
                onChangeText={setAddress}
                placeholder="tailcat://connect/... or http://192.168.1.x:3789"
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
              <Text style={[styles.fieldHint, { color: parsedPreview?.token ? theme.sageInk : theme.mute }]}>
                {parsedPreview?.token
                  ? t('tokenRecognized')
                  : t('tokenAutoHint')}
              </Text>
            </View>

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>{t('tokenOptionalLbl')}</Text>
              <TextInput
                value={token}
                onChangeText={setToken}
                placeholder={t('tokenFillHint')}
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
                <Text style={[styles.submitTxt, { color: theme.accentText }]}>{t('connect')}</Text>
              )}
            </Pressable>
          </View>

          {servers.length > 0 && (
            <View style={[styles.card, { backgroundColor: theme.card }]}>
              <Text style={[styles.cardTitle, { color: theme.text }]}>{t('savedHosts')}</Text>
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
                              <Text style={[styles.curBadgeTxt, { color: theme.sageInk }]}>{t('current')}</Text>
                            </View>
                          )}
                        </View>
                        <Text numberOfLines={1} style={[styles.savedAddr, { color: theme.mute, fontFamily: MONO_FONT }]}>
                          {s.address}
                        </Text>
                      </Pressable>
                      <Pressable onPress={() => void removeServer(s.id)} hitSlop={8} style={styles.delBtn}>
                        <Text style={[styles.delTxt, { color: theme.danger }]}>{t('removeBtn')}</Text>
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
