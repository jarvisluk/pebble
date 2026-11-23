import { router, Stack, usePathname } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import React, { useEffect } from 'react';
import { Platform } from 'react-native';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { I18nProvider } from '../i18n';
import { HubProvider, useHub } from '../store';
import { ThemeProvider, useTheme } from '../theme';

function ConnectionGuard() {
  const pathname = usePathname();
  const { ready, activeServer, connState, bots } = useHub();

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
            <Stack.Screen name="desktop" options={{ presentation: sheetPresentation }} />
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
