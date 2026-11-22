import React from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { useI18n } from '../i18n';
import { useTheme } from '../theme';

interface ModalHeaderProps {
  title: string;
  onClose: () => void;
  closeLabel?: string;
  rightLabel?: string;
  onRightPress?: () => void;
  rightDisabled?: boolean;
  rightLoading?: boolean;
}

export function ModalHeader({
  title,
  onClose,
  closeLabel,
  rightLabel,
  onRightPress,
  rightDisabled,
  rightLoading,
}: ModalHeaderProps) {
  const theme = useTheme();
  const { t } = useI18n();
  const insets = useSafeAreaInsets();
  const resolvedCloseLabel = closeLabel ?? t('close');

  return (
    <View style={[styles.bar, { paddingTop: Math.max(insets.top, 12), borderBottomColor: theme.s2, backgroundColor: theme.page }]}>
      <Pressable onPress={onClose} style={[styles.sideBtn, { backgroundColor: theme.s1 }]} hitSlop={8}>
        <Text style={[styles.sideTxt, { color: theme.text2 }]}>{resolvedCloseLabel}</Text>
      </Pressable>
      <Text numberOfLines={1} style={[styles.title, { color: theme.text }]}>
        {title}
      </Text>
      {rightLabel ? (
        <Pressable
          disabled={rightDisabled || rightLoading}
          onPress={onRightPress}
          style={[
            styles.sideBtn,
            { backgroundColor: rightDisabled ? theme.s2 : theme.accent },
          ]}
          hitSlop={8}
        >
          {rightLoading ? (
            <ActivityIndicator size="small" color={theme.accentText} />
          ) : (
            <Text style={[styles.rightTxt, { color: rightDisabled ? theme.mute : theme.accentText }]}>
              {rightLabel}
            </Text>
          )}
        </Pressable>
      ) : (
        <View style={styles.placeholder} />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 16,
    paddingBottom: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  sideBtn: {
    minWidth: 58,
    height: 32,
    borderRadius: 999,
    paddingHorizontal: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sideTxt: {
    fontSize: 16,
    fontWeight: '600',
  },
  rightTxt: {
    fontSize: 16,
    fontWeight: '600',
  },
  title: {
    fontSize: 17,
    fontWeight: '700',
    letterSpacing: -0.3,
    flex: 1,
    textAlign: 'center',
    marginHorizontal: 8,
  },
  placeholder: {
    minWidth: 58,
  },
});
