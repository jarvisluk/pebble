import { router, Stack, usePathname } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import React, { useEffect } from 'react';
import { Linking, Platform } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { I18nProvider } from '../i18n';
import { HubProvider, useHub } from '../store';
import { ThemeProvider, useTheme } from '../theme';

function ConnectionGuard() {
  const pathname = usePathname();
  const { ready, activeServer, connState, bots, interruptBot } = useHub();

  useEffect(() => {
    const handleUrl = (raw: string | null | undefined) => {
      if (!raw || !raw.startsWith('bothub://')) return;
      const stripped = raw.slice('bothub://'.length);
      const [pathPart, queryPart] = stripped.split('?');
      const segs = pathPart.split('/').filter(Boolean);
      const head = (segs[0] || '').toLowerCase();
      if (head === 'bot' && segs[1]) {
        router.push(`/bot/${encodeURIComponent(segs[1])}`);
      } else if (head === 'desktop') {
        const params = new URLSearchParams(queryPart || '');
        const target = params.get('target') || 'local';
        router.push(`/desktop?target=${encodeURIComponent(target)}`);
      } else if (head === 'interrupt' && segs[1]) {
        interruptBot(decodeURIComponent(segs[1])).catch(() => {});
      }
    };
    Linking.getInitialURL().then(handleUrl).catch(() => {});
    const sub = Linking.addEventListener('url', (ev) => handleUrl(ev.url));
    return () => sub.remove();
  }, [interruptBot]);

  useEffect(() => {
    if (!ready || pathname === '/') return;
    const blocked =
      !activeServer ||
      connState === 'unauthorized' ||
      (connState === 'error' && bots.length === 0);
    if (blocked && (pathname !== '/connect' || !activeServer)) {
      router.replace('/');
    }
  }, [activeServer, bots.length, connState, pathname, ready]);

  return null;
}

function RootContent() {
  const theme = useTheme();
  const sheetPresentation = Platform.OS === 'ios' ? 'pageSheet' : 'modal';

  return (
    <GestureHandlerRootView style={{ flex: 1, backgroundColor: theme.page }}>
      <SafeAreaProvider>
        <HubProvider>
          <ConnectionGuard />
          <StatusBar style={theme.dark ? 'light' : 'dark'} />
          <Stack
            screenOptions={{
              headerShown: false,
              contentStyle: { backgroundColor: theme.page },
            }}
          >
            <Stack.Screen name="index" />
            <Stack.Screen name="bot/[id]" />
            <Stack.Screen name="connect" options={{ presentation: sheetPresentation }} />
            <Stack.Screen name="create-bot" options={{ presentation: sheetPresentation }} />
            <Stack.Screen name="settings" options={{ presentation: sheetPresentation }} />
            <Stack.Screen name="devices" options={{ presentation: sheetPresentation }} />
            <Stack.Screen name="desktop" options={{ presentation: 'fullScreenModal' }} />
          </Stack>
        </HubProvider>
      </SafeAreaProvider>
    </GestureHandlerRootView>
  );
}

export default function RootLayout() {
  return (
    <I18nProvider>
      <ThemeProvider>
        <RootContent />
      </ThemeProvider>
    </I18nProvider>
  );
}
