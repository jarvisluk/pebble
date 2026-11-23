import React, { useEffect, useRef } from 'react';
import { Animated, StyleSheet, View } from 'react-native';
import Svg, { G, Path, Rect } from 'react-native-svg';

import { pebbleFrame, pebbleParams, resolvePebbleSeed, usePebbleClock } from '../lib/pebbleAvatar';
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
  const seed = resolvePebbleSeed(id, avatar);
  const blob = getBotColor(id, index, color);
  const clock = usePebbleClock(true);

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
  const f = pebbleFrame(pebbleParams(seed), clock, busy);

  return (
    <View style={[styles.wrap, { width: size, height: size }]}>
      <Svg width="100%" height="100%" viewBox="0 0 100 100">
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
