import React, { useEffect, useRef } from 'react';
import { Animated, StyleSheet, View } from 'react-native';
import Svg, { Ellipse, G, Path, Rect } from 'react-native-svg';

import { pebbleFrame, pebbleParams, usePebbleClock } from '../lib/pebbleAvatar';
import { getBotColor, useTheme } from '../theme';

interface BotAvatarProps {
  id: string;
  size: number;
  index?: number;
  avatar?: string;
  color?: string;
  busy?: boolean;
  ringColor?: string;
}

export function BotAvatar({ id, size, index = -1, avatar, color, busy = false, ringColor }: BotAvatarProps) {
  const theme = useTheme();
  const shape = (avatar || '').trim().toLowerCase() || 'blob';
  const blob = getBotColor(id, index, color);
  const isDefaultBlob =
    shape !== 'squircle' &&
    shape !== 'cat' &&
    shape !== 'bear' &&
    shape !== 'cloud' &&
    shape !== 'ghost' &&
    shape !== 'star' &&
    shape !== 'sprout';
  const clock = usePebbleClock(isDefaultBlob);

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

  const renderShape = () => {
    switch (shape) {
      case 'squircle':
        return (
          <>
            <Path d="M50 10C78 10 90 22 90 50C90 78 78 90 50 90C22 90 10 78 10 50C10 22 22 10 50 10Z" fill={blob} />
            <Ellipse cx={38} cy={48} rx={5.5} ry={9.5} fill={theme.eye} />
            <Ellipse cx={62} cy={48} rx={5.5} ry={9.5} fill={theme.eye} />
          </>
        );
      case 'cat':
        return (
          <>
            <Path d="M20 34C18 16 24 10 38 22C46 19 54 19 62 22C76 10 82 16 80 34C88 44 90 58 86 70C80 86 66 91 50 91C34 91 20 86 14 70C10 58 12 44 20 34Z" fill={blob} />
            <Ellipse cx={38} cy={55} rx={5.5} ry={9} fill={theme.eye} />
            <Ellipse cx={62} cy={55} rx={5.5} ry={9} fill={theme.eye} />
          </>
        );
      case 'bear':
        return (
          <>
            <Path d="M26 35C15 33 11 21 19 14C27 7 38 14 39 23C46 21 54 21 61 23C62 14 73 7 81 14C89 21 85 33 74 35C85 45 89 60 84 73C77 86 64 91 50 91C36 91 23 86 16 73C11 60 15 45 26 35Z" fill={blob} />
            <Ellipse cx={38} cy={55} rx={5.5} ry={9} fill={theme.eye} />
            <Ellipse cx={62} cy={55} rx={5.5} ry={9} fill={theme.eye} />
          </>
        );
      case 'cloud':
        return (
          <>
            <Path d="M28 80C15 80 8 70 9 58C10 47 18 40 26 39C27 24 39 14 54 15C67 16 77 25 78 38C87 40 93 49 92 60C91 72 82 80 69 80Z" fill={blob} />
            <Ellipse cx={39} cy={53} rx={5.2} ry={8.5} fill={theme.eye} />
            <Ellipse cx={61} cy={53} rx={5.2} ry={8.5} fill={theme.eye} />
          </>
        );
      case 'ghost':
        return (
          <>
            <Path d="M50 10C74 10 88 26 88 52L88 84C88 89 82 91 78 87C73 82 68 82 63 87C58 92 53 92 50 87C47 92 42 92 37 87C32 82 27 82 22 87C18 91 12 89 12 84L12 52C12 26 26 10 50 10Z" fill={blob} />
            <Ellipse cx={38} cy={46} rx={5.5} ry={9.5} fill={theme.eye} />
            <Ellipse cx={62} cy={46} rx={5.5} ry={9.5} fill={theme.eye} />
          </>
        );
      case 'star':
        return (
          <>
            <Path d="M50 8C55 8 59 23 64 27C69 31 87 28 90 34C93 40 78 51 77 57C76 63 85 80 79 85C73 90 58 79 50 79C42 79 27 90 21 85C15 80 24 63 23 57C22 51 7 40 10 34C13 28 31 31 36 27C41 23 45 8 50 8Z" fill={blob} />
            <Ellipse cx={40} cy={49} rx={5} ry={8.5} fill={theme.eye} />
            <Ellipse cx={60} cy={49} rx={5} ry={8.5} fill={theme.eye} />
          </>
        );
      case 'sprout':
        return (
          <>
            <Path d="M50 28C48 16 36 9 25 13C26 23 37 29 50 28Z" fill={blob} />
            <Path d="M50 28C53 15 65 8 76 12C75 23 63 29 50 28Z" fill={blob} />
            <Path d="M50 28C66 28 84 40 84 62C84 82 69 92 50 92C31 92 16 82 16 62C16 40 34 28 50 28Z" fill={blob} />
            <Ellipse cx={39} cy={59} rx={5.2} ry={8.8} fill={theme.eye} />
            <Ellipse cx={61} cy={59} rx={5.2} ry={8.8} fill={theme.eye} />
          </>
        );
      default: {
        const f = pebbleFrame(pebbleParams(id || '?'), clock, busy);
        return (
          <>
            <G rotation={f.bodyRotDeg} originX={50} originY={50}>
              <Path d={f.pathD} fill={blob} />
            </G>
            <Rect
              x={f.eyes[0].x}
              y={f.eyes[0].y}
              width={f.eyes[0].width}
              height={f.eyes[0].height}
              rx={f.eyes[0].rx}
              rotation={f.eyes[0].lean}
              originX={f.eyes[0].cx}
              originY={f.eyes[0].cy}
              fill={theme.eye}
            />
            <Rect
              x={f.eyes[1].x}
              y={f.eyes[1].y}
              width={f.eyes[1].width}
              height={f.eyes[1].height}
              rx={f.eyes[1].rx}
              rotation={f.eyes[1].lean}
              originX={f.eyes[1].cx}
              originY={f.eyes[1].cy}
              fill={theme.eye}
            />
          </>
        );
      }
    }
  };

  return (
    <View style={[styles.wrap, { width: size, height: size }]}>
      <Svg width="100%" height="100%" viewBox="0 0 100 100">
        {renderShape()}
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
