import React, { useEffect, useState } from 'react';
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';

import { useI18n } from '../i18n';
import {
  BOT_AVATARS,
  BOT_COLORS,
  getBotColor,
  MONO_FONT,
  normalizeBotAvatar,
  normalizeBotHex,
  useTheme,
} from '../theme';
import { BotAvatar } from './BotAvatar';

interface BotAppearancePickerProps {
  botId: string;
  botIndex?: number;
  avatar: string;
  color: string;
  accent?: string;
  onAvatarChange: (next: string) => void;
  onColorChange: (next: string) => void;
}

export function BotAppearancePicker({
  botId,
  botIndex = -1,
  avatar,
  color,
  accent,
  onAvatarChange,
  onColorChange,
}: BotAppearancePickerProps) {
  const theme = useTheme();
  const { t } = useI18n();
  const [hexDraft, setHexDraft] = useState(color);

  useEffect(() => {
    setHexDraft(color);
  }, [color]);

  const effectiveAvatar = normalizeBotAvatar(avatar);
  const effectiveColor = getBotColor(botId, botIndex, color);
  const ring = accent || theme.accent;
  const hasCustom = effectiveAvatar !== 'blob' || Boolean(normalizeBotHex(color));

  return (
    <View style={[styles.wrap, { backgroundColor: theme.s1 }]}>
      <View style={styles.topRow}>
        <Text style={[styles.lbl, { color: theme.text2 }]}>{t('avatarShape')}</Text>
        {hasCustom && (
          <Pressable
            onPress={() => {
              onAvatarChange('');
              onColorChange('');
            }}
          >
            <Text style={[styles.resetTxt, { color: ring }]}>{t('resetDefault')}</Text>
          </Pressable>
        )}
      </View>

      <View style={styles.avGrid}>
        {BOT_AVATARS.map((kind) => {
          const active = effectiveAvatar === kind;
          return (
            <Pressable
              key={kind}
              onPress={() => onAvatarChange(kind === 'blob' ? '' : kind)}
              style={[
                styles.avOpt,
                {
                  backgroundColor: theme.card,
                  borderColor: active ? ring : 'transparent',
                },
              ]}
            >
              <BotAvatar id={botId} size={30} index={botIndex} avatar={kind} color={effectiveColor} />
            </Pressable>
          );
        })}
      </View>

      <Text style={[styles.lbl, { color: theme.text2 }]}>{t('botColorLabel')}</Text>
      <View style={styles.colorRow}>
        {BOT_COLORS.map((hex) => {
          const active = normalizeBotHex(color) === hex;
          return (
            <Pressable
              key={hex}
              onPress={() => onColorChange(hex)}
              style={[
                styles.swatch,
                {
                  backgroundColor: hex,
                  borderColor: active ? theme.text : 'transparent',
                },
              ]}
            />
          );
        })}
        <TextInput
          value={hexDraft}
          onChangeText={(txt) => {
            setHexDraft(txt);
            const norm = normalizeBotHex(txt);
            if (norm) onColorChange(norm);
            else if (!txt.trim()) onColorChange('');
          }}
          placeholder="#rrggbb"
          placeholderTextColor={theme.mute}
          autoCapitalize="none"
          autoCorrect={false}
          maxLength={7}
          style={[
            styles.hexInput,
            {
              backgroundColor: theme.card,
              color: theme.text,
              fontFamily: MONO_FONT,
            },
          ]}
        />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    borderRadius: 12,
    padding: 12,
    gap: 10,
  },
  topRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  lbl: {
    fontSize: 13,
    fontWeight: '600',
  },
  resetTxt: {
    fontSize: 13,
    fontWeight: '600',
  },
  avGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
  },
  avOpt: {
    width: '22.5%',
    aspectRatio: 1,
    borderRadius: 10,
    borderWidth: 2,
    alignItems: 'center',
    justifyContent: 'center',
  },
  colorRow: {
    flexDirection: 'row',
    alignItems: 'center',
    flexWrap: 'wrap',
    gap: 8,
  },
  swatch: {
    width: 26,
    height: 26,
    borderRadius: 13,
    borderWidth: 2,
  },
  hexInput: {
    height: 30,
    minWidth: 82,
    paddingHorizontal: 8,
    borderRadius: 8,
    fontSize: 13,
  },
});
