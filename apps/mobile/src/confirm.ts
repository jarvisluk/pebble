import { Alert, Platform } from 'react-native';

import { tStatic } from './i18n';

export function confirmAction(
  title: string,
  message: string,
  onConfirm: () => void | Promise<void>,
  destructive = false
) {
  if (Platform.OS === 'web') {
    const ok = typeof window !== 'undefined' && typeof window.confirm === 'function'
      ? window.confirm(`${title}\n\n${message}`)
      : true;
    if (ok) {
      void onConfirm();
    }
    return;
  }
  Alert.alert(title, message, [
    { text: tStatic('cancel'), style: 'cancel' },
    {
      text: tStatic('confirm'),
      style: destructive ? 'destructive' : 'default',
      onPress: () => {
        void onConfirm();
      },
    },
  ]);
}
