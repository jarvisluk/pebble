import React, { useEffect, useRef } from 'react';
import { Animated, StyleSheet, View } from 'react-native';
import Svg, { Ellipse, G, Path } from 'react-native-svg';

import { getBotColor, useTheme } from '../theme';

function fnv1a(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  }
  return h >>> 0;
}

interface BotAvatarProps {
  id: string;
  size: number;
  index?: number;
  busy?: boolean;
  ringColor?: string;
}

export function BotAvatar({ id, size, index = -1, busy = false, ringColor }: BotAvatarProps) {
  const theme = useTheme();
  const h = fnv1a(id || '?');
  const blob = getBotColor(id, index);
  const tilt = ((h >>> 4) % 25) - 12;
  const gx = ((h >>> 9) % 7) - 3;
  const gy = ((h >>> 12) % 5) - 2;
  const lean = (h >>> 15) % 2 ? -14 : 12;

  const pulse = useRef(new Animated.Value(1)).current;
  useEffect(() => {
    if (!busy) {
      pulse.setValue(1);
      return;
    }
    const anim = Animated.loop(
      Animated.sequence([
        Animated.timing(pulse, { toValue: 0.65, duration: 600, useNativeDriver: true }),
        Animated.timing(pulse, { toValue: 1, duration: 600, useNativeDriver: true }),
      ])
    );
    anim.start();
    return () => anim.stop();
  }, [busy, pulse]);

  const dotSize = Math.max(9, Math.round(size * 0.24));

  return (
    <View style={[styles.wrap, { width: size, height: size }]}>
      <Svg width="100%" height="100%" viewBox="0 0 100 100">
        <G transform={`rotate(${tilt} 50 50)`}>
          <Path d="M52 9C80 9 95 28 93 55C91 81 72 93 47 91C21 89 5 72 7 46C9 22 26 9 52 9Z" fill={blob} />
          <Ellipse cx={39 + gx} cy={45 + gy} rx={6} ry={10.5} transform={`rotate(${lean} ${39 + gx} ${45 + gy})`} fill={theme.eye} />
          <Ellipse cx={62 + gx} cy={43 + gy} rx={6} ry={10.5} transform={`rotate(${lean} ${62 + gx} ${43 + gy})`} fill={theme.eye} />
        </G>
      </Svg>
      {busy && (
        <Animated.View
          style={[
            styles.dot,
            {
              width: dotSize,
              height: dotSize,
              borderRadius: dotSize / 2,
              backgroundColor: theme.busy,
              borderColor: ringColor || theme.card,
              transform: [{ scale: pulse }],
              opacity: pulse,
            },
          ]}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    position: 'relative',
    alignItems: 'center',
    justifyContent: 'center',
  },
  dot: {
    position: 'absolute',
    right: 1,
    bottom: 1,
    borderWidth: 2,
  },
});
