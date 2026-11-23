import { Platform } from 'react-native';
import { requireOptionalNativeModule } from 'expo-modules-core';

interface NativeNotificationsModule {
  hasPermission(): boolean;
  requestPermission(): Promise<boolean>;
  updateBotActivity(botId: string, botName: string, statusText: string, stepCount: number): void;
  endBotActivity(botId: string, botName: string, summary: string, notify: boolean): void;
  showTakeoverAlert(id: string, reason: string, target: string): void;
  cancelTakeoverAlert(id: string): void;
}

const nativeMod =
  Platform.OS === 'android'
    ? requireOptionalNativeModule<NativeNotificationsModule>('BothubNotifications')
    : null;

export function hasNotificationPermission(): boolean {
  if (!nativeMod) return false;
  try {
    return nativeMod.hasPermission();
  } catch {
    return false;
  }
}

export async function requestNotificationPermission(): Promise<boolean> {
  if (!nativeMod) return false;
  try {
    return await nativeMod.requestPermission();
  } catch {
    return false;
  }
}

export function updateBotActivity(botId: string, botName: string, statusText: string, stepCount: number): void {
  if (!nativeMod) return;
  try {
    nativeMod.updateBotActivity(botId, botName, statusText, stepCount);
  } catch {}
}

export function endBotActivity(botId: string, botName: string, summary: string, notify: boolean): void {
  if (!nativeMod) return;
  try {
    nativeMod.endBotActivity(botId, botName, summary, notify);
  } catch {}
}

export function showTakeoverAlert(id: string, reason: string, target: string): void {
  if (!nativeMod) return;
  try {
    nativeMod.showTakeoverAlert(id, reason, target);
  } catch {}
}

export function cancelTakeoverAlert(id: string): void {
  if (!nativeMod) return;
  try {
    nativeMod.cancelTakeoverAlert(id);
  } catch {}
}
