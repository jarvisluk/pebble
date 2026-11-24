import { CameraView, useCameraPermissions, type BarcodeScanningResult } from 'expo-camera';
import * as Clipboard from 'expo-clipboard';
import * as Haptics from 'expo-haptics';
import { router, useLocalSearchParams } from 'expo-router';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Animated,
  Easing,
  KeyboardAvoidingView,
  Linking,
  Platform,
  Pressable,
  ScrollView,
  Share,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

const DESKTOP_DOWNLOAD_MAC_URL = 'https://github.com/pebble-ai/bot-hub/releases/latest';
const DESKTOP_DOWNLOAD_WIN_URL = 'https://github.com/pebble-ai/bot-hub/releases/latest';
import { SafeAreaView } from 'react-native-safe-area-context';

import { isNativeTunnelAvailable, type TailscalePeer } from '../../modules/bothub-tunnel';
import { normalizeAddress } from '../api';
import { BotAvatar } from '../components/BotAvatar';
import { QrCodeIcon } from '../components/Icons';
import { ModalHeader } from '../components/ModalHeader';
import { confirmAction } from '../confirm';
import { useI18n } from '../i18n';
import { useHub } from '../store';
import { MONO_FONT, useTheme } from '../theme';

export function ConnectContent({ standalone = false }: { standalone?: boolean }) {
  const theme = useTheme();
  const { t } = useI18n();
  const {
    servers,
    activeServer,
    connState,
    connError,
    mobileTailscale,
    tsBinding,
    tsControlUrl: savedTsControlUrl,
    addAndConnectServer,
    selectServer,
    removeServer,
    bindTailscale,
    refreshMobileTailscale,
    logoutTailscale,
    reconnect,
  } = useHub();

  const [permission, requestPermission] = useCameraPermissions();
  const [scanning, setScanning] = useState(false);
  const [label, setLabel] = useState('');
  const [address, setAddress] = useState('');
  const [token, setToken] = useState('');
  const [standbyAddress, setStandbyAddress] = useState('');
  const [standbyToken, setStandbyToken] = useState('');
  const [localError, setLocalError] = useState('');
  const [infoHint, setInfoHint] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [tsAdvOpen, setTsAdvOpen] = useState(false);
  const [tsAuthKey, setTsAuthKey] = useState('');
  const [tsControlUrl, setTsControlUrl] = useState('');
  const [manualOpen, setManualOpen] = useState(false);
  const isFreshOnboarding = standalone && servers.length === 0 && !activeServer;
  const showFreshHero = isFreshOnboarding && !manualOpen;

  const pageAnim = useRef(new Animated.Value(1)).current;
  const pageDirRef = useRef<1 | -1>(1);
  const firstRenderRef = useRef(true);

  useEffect(() => {
    if (firstRenderRef.current) {
      firstRenderRef.current = false;
      return;
    }
    pageAnim.setValue(0);
    Animated.timing(pageAnim, {
      toValue: 1,
      duration: 240,
      easing: Easing.out(Easing.cubic),
      useNativeDriver: true,
    }).start();
  }, [showFreshHero, pageAnim]);

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
    if (savedTsControlUrl && !tsControlUrl) {
      setTsControlUrl(savedTsControlUrl);
    }
  }, [savedTsControlUrl, tsControlUrl]);

  useEffect(() => {
    if (!submitting) return;
    if (connState === 'connected') {
      setSubmitting(false);
      if (!standalone) {
        if (router.canGoBack()) {
          router.back();
        } else {
          router.replace('/');
        }
      }
    } else if (connState === 'error' || connState === 'unauthorized') {
      setSubmitting(false);
      setLocalError(connError || t('connFailed'));
    }
  }, [connError, connState, standalone, submitting, t]);

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
    if (
      !lower.startsWith('tailcat://') &&
      !lower.startsWith('tailscale://') &&
      !lower.startsWith('http://') &&
      !lower.startsWith('https://')
    ) {
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
        standbyAddress: standbyAddress.trim() || undefined,
        standbyToken: standbyToken.trim() || undefined,
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

  const onPickPeer = async (peer: TailscalePeer) => {
    setLocalError('');
    setInfoHint('');
    setLabel(peer.hostname);
    setAddress(peer.address);
    const saved = servers.find((s) => s.address === peer.address && s.token);
    if (saved) {
      setToken(saved.token);
      setSubmitting(true);
      try {
        await addAndConnectServer({ label: peer.hostname, address: peer.address, token: saved.token });
      } catch (err: any) {
        setSubmitting(false);
        setLocalError(err?.message || t('connFailed'));
      }
      return;
    }
    if (token.trim()) {
      setSubmitting(true);
      try {
        await addAndConnectServer({ label: peer.hostname, address: peer.address, token: token.trim() });
      } catch (err: any) {
        setSubmitting(false);
        setLocalError(err?.message || t('connFailed'));
      }
      return;
    }
    setInfoHint(t('tsPeerSelectedHint'));
  };

  const freshHero = (
    <ScrollView
      contentContainerStyle={styles.heroScroll}
      keyboardShouldPersistTaps="handled"
    >
      <View style={styles.heroInner}>
        <View style={styles.heroTop}>
          <BotAvatar id="assistant" size={84} color="#7aaa98" busy={submitting} />
          <Text style={[styles.heroTitle, { color: theme.text }]}>Pebble</Text>
          <Text style={[styles.heroSub, { color: theme.text2 }]}>{t('onboardHeroSub')}</Text>
        </View>

        {scanning && Platform.OS !== 'web' && (
          <View style={styles.camWrap}>
            <CameraView
              style={styles.cam}
              facing="back"
              barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
              onBarcodeScanned={onBarcodeScanned}
            />
          </View>
        )}

        <View style={styles.heroActions}>
          {Platform.OS !== 'web' && (
            <Pressable
              onPress={toggleScanner}
              style={[styles.heroPrimaryBtn, { backgroundColor: theme.accent }]}
            >
              <QrCodeIcon size={17} color={theme.accentText} />
              <Text style={[styles.heroPrimaryTxt, { color: theme.accentText }]}>
                {scanning ? t('collapse') : t('scanConnect')}
              </Text>
            </Pressable>
          )}
          <Pressable
            onPress={() => {
              setScanning(false);
              pageDirRef.current = 1;
              setManualOpen(true);
            }}
            style={[styles.heroSecondaryBtn, { backgroundColor: theme.card }]}
          >
            <Text style={[styles.heroSecondaryTxt, { color: theme.text }]}>
              {t('manualPairingBtn')}
            </Text>
          </Pressable>
        </View>

        {localError ? (
          <Text style={[styles.heroMsg, { color: theme.danger }]}>{localError}</Text>
        ) : infoHint ? (
          <Text style={[styles.heroMsg, { color: theme.sageInk }]}>{infoHint}</Text>
        ) : null}

        <View style={styles.heroFooter}>
          <Text style={[styles.heroFooterHint, { color: theme.mute }]}>{t('noDesktopYet')}</Text>
          <View style={styles.heroFooterLinks}>
            <Pressable onPress={() => void Linking.openURL(DESKTOP_DOWNLOAD_MAC_URL)} hitSlop={8}>
              <Text style={[styles.heroFooterLink, { color: theme.text2 }]}>macOS</Text>
            </Pressable>
            <Text style={[styles.heroFooterDot, { color: theme.mute }]}>·</Text>
            <Pressable onPress={() => void Linking.openURL(DESKTOP_DOWNLOAD_WIN_URL)} hitSlop={8}>
              <Text style={[styles.heroFooterLink, { color: theme.text2 }]}>Windows</Text>
            </Pressable>
            <Text style={[styles.heroFooterDot, { color: theme.mute }]}>·</Text>
            <Pressable
              onPress={() =>
                void Share.share({ message: DESKTOP_DOWNLOAD_MAC_URL, url: DESKTOP_DOWNLOAD_MAC_URL })
              }
              hitSlop={8}
            >
              <Text style={[styles.heroFooterLink, { color: theme.text2 }]}>{t('sendToComputer')}</Text>
            </Pressable>
          </View>
        </View>
      </View>
    </ScrollView>
  );

  const content = showFreshHero ? (
    freshHero
  ) : (
    <KeyboardAvoidingView
      style={styles.flex}
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
    >
      <ScrollView
        contentContainerStyle={styles.scroll}
        keyboardShouldPersistTaps="handled"
      >
        <View style={standalone ? styles.standaloneInner : styles.modalInner}>
          {standalone && (
            <View style={styles.gateHeader}>
              <View style={styles.rowBetween}>
                <View style={styles.col}>
                  <Text style={[styles.gateTitle, { color: theme.text }]}>Pebble</Text>
                  <Text style={[styles.gateSub, { color: theme.text2 }]}>{t('connectHost')}</Text>
                </View>
                {isFreshOnboarding && manualOpen && (
                  <Pressable
                    onPress={() => {
                      pageDirRef.current = -1;
                      setManualOpen(false);
                    }}
                    style={[styles.qrBtn, { backgroundColor: theme.card }]}
                  >
                    <Text style={[styles.qrBtnTxt, { color: theme.text }]}>{t('backBtn')}</Text>
                  </Pressable>
                )}
              </View>
            </View>
          )}

          {standalone && !!activeServer && (connState === 'unauthorized' || connState === 'error') && (
            <View style={[styles.card, { backgroundColor: theme.card }]}>
              <Text style={[styles.cardTitle, { color: theme.text }]}>
                {connState === 'unauthorized' ? t('tokenRequired') : t('cantConnectHost')}
              </Text>
              <Text style={[styles.cardSub, { color: theme.text2 }]}>
                {connState === 'error' && connError ? connError : t('onboardDesc')}
              </Text>
              <Pressable
                onPress={() => {
                  setLocalError('');
                  void reconnect();
                }}
                style={[styles.retryBtn, { backgroundColor: theme.s1 }]}
              >
                <Text style={[styles.retryBtnTxt, { color: theme.text2 }]}>{t('retry')}</Text>
              </Pressable>
            </View>
          )}

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

          <>
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
                placeholder="tailcat://… / tailscale://… / 192.168.1.x:3789"
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
              <Text style={[styles.fieldHint, { color: infoHint || parsedPreview?.token ? theme.sageInk : theme.mute }]}>
                {infoHint || (parsedPreview?.token ? t('tokenRecognized') : t('tokenAutoHint'))}
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

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>{t('standbyAddress')}</Text>
              <TextInput
                value={standbyAddress}
                onChangeText={setStandbyAddress}
                placeholder="tailcat://… / 192.168.x.x:3789"
                placeholderTextColor={theme.mute}
                autoCapitalize="none"
                autoCorrect={false}
                style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
              />
            </View>

            <View style={styles.field}>
              <Text style={[styles.fieldLbl, { color: theme.text2 }]}>{t('standbyToken')}</Text>
              <TextInput
                value={standbyToken}
                onChangeText={setStandbyToken}
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

          {tunnelAvailable && (
            <View style={[styles.card, { backgroundColor: theme.card }]}>
              <View style={styles.rowBetween}>
                <View style={styles.col}>
                  <Text style={[styles.cardTitle, { color: theme.text }]}>{t('tsNetwork')}</Text>
                  {mobileTailscale?.state === 'running' && (
                    <Text style={[styles.cardSub, { color: theme.sageInk }]}>
                      {t('tsBoundReady', { tailnet: mobileTailscale.tailnet || 'Tailscale' })}
                      {mobileTailscale.ip ? ` · ${mobileTailscale.ip}` : ''}
                    </Text>
                  )}
                </View>
                {mobileTailscale?.state === 'running' && (
                  <View style={{ flexDirection: 'row', gap: 8 }}>
                    <Pressable
                      onPress={() => void refreshMobileTailscale(true)}
                      style={[styles.qrBtn, { backgroundColor: theme.s1 }]}
                    >
                      <Text style={[styles.qrBtnTxt, { color: theme.text }]}>{t('tsRefreshPeers')}</Text>
                    </Pressable>
                    <Pressable
                      onPress={() =>
                        confirmAction(
                          t('tsUnlinkTitle'),
                          t('tsUnlinkDesc'),
                          () => logoutTailscale(),
                          true
                        )
                      }
                      style={[styles.qrBtn, { backgroundColor: theme.s1 }]}
                    >
                      <Text style={[styles.qrBtnTxt, { color: theme.danger }]}>{t('tsUnlink')}</Text>
                    </Pressable>
                  </View>
                )}
              </View>

              {mobileTailscale?.state === 'running' ? (
                (() => {
                  const peers = (mobileTailscale.peers || []).filter((p) => p.online);
                  if (peers.length === 0) {
                    return <Text style={[styles.cardSub, { color: theme.mute }]}>{t('tsNoPeers')}</Text>;
                  }
                  return (
                    <View style={styles.savedList}>
                      {peers.map((peer) => (
                        <Pressable
                          key={peer.id}
                          onPress={() => void onPickPeer(peer)}
                          style={[styles.savedRow, { backgroundColor: theme.s1 }]}
                        >
                          <View style={styles.savedInfo}>
                            <View style={styles.savedHead}>
                              <Text numberOfLines={1} style={[styles.savedName, { color: theme.text }]}>
                                {peer.hostname}
                              </Text>
                              {peer.isBotHub && (
                                <View style={[styles.curBadge, { backgroundColor: theme.lilac }]}>
                                  <Text style={[styles.curBadgeTxt, { color: theme.lilacInk }]}>
                                    {t('tsBotHubBadge')}
                                  </Text>
                                </View>
                              )}
                            </View>
                            <Text numberOfLines={1} style={[styles.savedAddr, { color: theme.mute, fontFamily: MONO_FONT }]}>
                              {peer.dnsName || peer.ip || peer.address}
                            </Text>
                          </View>
                        </Pressable>
                      ))}
                    </View>
                  );
                })()
              ) : mobileTailscale?.state === 'needs_login' ? (
                <View style={styles.rowBetween}>
                  <Text style={[styles.cardSub, { color: theme.accent }]}>{t('tsNeedsLogin')}</Text>
                  {mobileTailscale.authUrl ? (
                    <Pressable
                      onPress={() => void Linking.openURL(mobileTailscale.authUrl!)}
                      style={[styles.qrBtn, { backgroundColor: theme.accentSoft }]}
                    >
                      <Text style={[styles.qrBtnTxt, { color: theme.accent }]}>{t('tsOpenLogin')}</Text>
                    </Pressable>
                  ) : null}
                </View>
              ) : mobileTailscale?.state === 'starting' || tsBinding ? (
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10 }}>
                  <ActivityIndicator size="small" color={theme.accent} />
                  <Text style={[styles.cardSub, { color: theme.text2 }]}>{t('tsBinding')}</Text>
                </View>
              ) : (
                <>
                  {mobileTailscale?.state === 'error' && mobileTailscale.error ? (
                    <Text style={[styles.errTxt, { color: theme.danger }]}>{mobileTailscale.error}</Text>
                  ) : null}
                  <Pressable
                    onPress={() => {
                      void bindTailscale({ authKey: tsAuthKey, controlUrl: tsControlUrl }).then(() => {
                        setTsAuthKey('');
                      });
                    }}
                    style={[styles.qrBtn, { backgroundColor: theme.accentSoft, alignSelf: 'flex-start' }]}
                  >
                    <Text style={[styles.qrBtnTxt, { color: theme.accent }]}>{t('tsBindAccount')}</Text>
                  </Pressable>
                  <Pressable onPress={() => setTsAdvOpen((v) => !v)}>
                    <Text style={[styles.fieldHint, { color: theme.text2 }]}>{t('tsAdvanced')}</Text>
                  </Pressable>
                  {tsAdvOpen && (
                    <View style={{ gap: 10 }}>
                      <TextInput
                        value={tsAuthKey}
                        onChangeText={setTsAuthKey}
                        placeholder={t('tsAuthKeyPh')}
                        placeholderTextColor={theme.mute}
                        secureTextEntry
                        autoCapitalize="none"
                        autoCorrect={false}
                        style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
                      />
                      <TextInput
                        value={tsControlUrl}
                        onChangeText={setTsControlUrl}
                        placeholder={t('tsControlUrlPh')}
                        placeholderTextColor={theme.mute}
                        autoCapitalize="none"
                        autoCorrect={false}
                        style={[styles.input, { backgroundColor: theme.s1, color: theme.text, fontFamily: MONO_FONT }]}
                      />
                    </View>
                  )}
                </>
              )}
            </View>
          )}
          </>

          {servers.length > 0 && (
            <View style={[styles.card, { backgroundColor: theme.card }]}>
              <Text style={[styles.cardTitle, { color: theme.text }]}>{t('savedHosts')}</Text>
              <View style={styles.savedList}>
                {servers.map((s) => {
                  const isCurrent = activeServer?.id === s.id;
                  const onPickPeer = async (peer: TailscalePeer) => {
    setLocalError('');
    setInfoHint('');
    setLabel(peer.hostname);
    setAddress(peer.address);
    const saved = servers.find((s) => s.address === peer.address && s.token);
    if (saved) {
      setToken(saved.token);
      setSubmitting(true);
      try {
        await addAndConnectServer({ label: peer.hostname, address: peer.address, token: saved.token });
      } catch (err: any) {
        setSubmitting(false);
        setLocalError(err?.message || t('connFailed'));
      }
      return;
    }
    if (token.trim()) {
      setSubmitting(true);
      try {
        await addAndConnectServer({ label: peer.hostname, address: peer.address, token: token.trim() });
      } catch (err: any) {
        setSubmitting(false);
        setLocalError(err?.message || t('connFailed'));
      }
      return;
    }
    setInfoHint(t('tsPeerSelectedHint'));
  };

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
        </View>
      </ScrollView>
    </KeyboardAvoidingView>
  );

  const animatedContent = (
    <Animated.View
      style={[
        styles.flex,
        {
          opacity: pageAnim,
          transform: [
            {
              translateX: pageAnim.interpolate({
                inputRange: [0, 1],
                outputRange: [pageDirRef.current * 26, 0],
              }),
            },
          ],
        },
      ]}
    >
      {content}
    </Animated.View>
  );

  if (standalone) {
    return (
      <SafeAreaView style={[styles.root, { backgroundColor: theme.page }]} edges={['top', 'left', 'right', 'bottom']}>
        {animatedContent}
      </SafeAreaView>
    );
  }

  return (
    <View style={[styles.root, { backgroundColor: theme.page }]}>
      <ModalHeader title={t('connectHost')} onClose={closeSheet} />
      {animatedContent}
    </View>
  );
}

export default function ConnectScreen() {
  return <ConnectContent standalone={false} />;
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  flex: { flex: 1 },
  heroScroll: {
    flexGrow: 1,
    justifyContent: 'center',
    paddingHorizontal: 24,
    paddingVertical: 32,
  },
  heroInner: {
    width: '100%',
    maxWidth: 360,
    alignSelf: 'center',
    gap: 20,
  },
  heroTop: {
    alignItems: 'center',
    gap: 10,
    paddingBottom: 8,
  },
  heroTitle: {
    fontSize: 20,
    fontWeight: '700',
  },
  heroSub: {
    fontSize: 14,
    lineHeight: 20,
    textAlign: 'center',
  },
  heroActions: {
    gap: 10,
  },
  heroPrimaryBtn: {
    height: 48,
    borderRadius: 14,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
  },
  heroPrimaryTxt: {
    fontSize: 15,
    fontWeight: '600',
  },
  heroSecondaryBtn: {
    height: 48,
    borderRadius: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  heroSecondaryTxt: {
    fontSize: 15,
    fontWeight: '500',
  },
  heroMsg: {
    fontSize: 13,
    textAlign: 'center',
  },
  heroFooter: {
    alignItems: 'center',
    gap: 6,
    paddingTop: 18,
  },
  heroFooterHint: {
    fontSize: 12,
  },
  heroFooterLinks: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  heroFooterLink: {
    fontSize: 12,
    fontWeight: '500',
  },
  heroFooterDot: {
    fontSize: 12,
  },
  scroll: {
    padding: 16,
    paddingBottom: 36,
  },
  modalInner: {
    gap: 12,
  },
  standaloneInner: {
    width: '100%',
    maxWidth: 560,
    alignSelf: 'center',
    gap: 12,
  },
  gateHeader: {
    paddingHorizontal: 4,
    paddingTop: 6,
    paddingBottom: 2,
    gap: 2,
  },
  gateTitle: {
    fontSize: 28,
    fontWeight: '700',
    letterSpacing: -0.7,
  },
  gateSub: {
    fontSize: 16,
  },
  onboardSteps: {
    gap: 8,
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
    fontSize: 13,
    fontWeight: '700',
  },
  onboardStepTxt: {
    flex: 1,
    fontSize: 15,
    lineHeight: 21,
  },
  retryBtn: {
    alignSelf: 'flex-start',
    borderRadius: 999,
    paddingHorizontal: 18,
    paddingVertical: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  retryBtnTxt: {
    fontSize: 16,
    fontWeight: '600',
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
    fontSize: 17,
    fontWeight: '700',
  },
  cardSub: {
    fontSize: 15,
    lineHeight: 20,
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
    fontSize: 15,
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
    fontSize: 15,
    lineHeight: 21,
  },
  field: {
    gap: 6,
  },
  fieldLbl: {
    fontSize: 15,
    fontWeight: '600',
  },
  fieldHint: {
    fontSize: 13,
  },
  input: {
    borderRadius: 14,
    paddingHorizontal: 13,
    paddingVertical: 11,
    fontSize: 17,
  },
  errTxt: {
    fontSize: 15,
    lineHeight: 20,
  },
  submitBtn: {
    borderRadius: 999,
    paddingVertical: 13,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 2,
  },
  submitTxt: {
    fontSize: 17,
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
    fontSize: 16,
    fontWeight: '600',
    flexShrink: 1,
  },
  curBadge: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 2,
  },
  curBadgeTxt: {
    fontSize: 12,
    fontWeight: '700',
  },
  savedAddr: {
    fontSize: 13,
  },
  delBtn: {
    paddingHorizontal: 6,
    paddingVertical: 4,
  },
  delTxt: {
    fontSize: 15,
    fontWeight: '600',
  },
});
