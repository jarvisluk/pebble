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
  const pressRef = useRef<{ down: boolean; t0: number; t1: number }>({ down: false, t0: 0, t1: -10 });
  const fx = (() => {
    const st = pressRef.current;
    const now = clock;
    const d = st.down ? Math.min(1, Math.max(0, (now - st.t0) / 0.08)) : 0;
    const r = st.down ? 0 : Math.max(0, now - st.t1);
    if (!st.down && r > 1.4) return null;
    const b = st.down ? 0 : Math.exp(-5 * r) * Math.sin(r * 22);
    const hop = !st.down && r < 0.32 ? Math.sin((r / 0.32) * Math.PI) : 0;
    const eye = st.down ? 0 : r < 0.14 ? r / 0.14 : 1;
    return {
      sx: 1 + 0.08 * d - 0.07 * b,
      sy: 1 - 0.1 * d + 0.09 * b,
      dy: -5 * hop,
      eye,
    };
  })();
  const f = pebbleFrame(pebbleParams(seed), clock, busy, fx?.eye ?? 1);

  return (
    <View
      style={[styles.wrap, { width: size, height: size }]}
      onTouchStart={() => {
        pressRef.current = { down: true, t0: clock, t1: 0 };
      }}
      onTouchEnd={() => {
        if (pressRef.current.down) {
          pressRef.current.down = false;
          pressRef.current.t1 = clock;
        }
      }}
      onTouchCancel={() => {
        if (pressRef.current.down) {
          pressRef.current.down = false;
          pressRef.current.t1 = clock;
        }
      }}
    >
      <Svg width="100%" height="100%" viewBox="0 0 100 100">
        <G
          translateY={fx ? fx.dy : 0}
          scaleX={fx ? fx.sx : 1}
          scaleY={fx ? fx.sy : 1}
          originX={50}
          originY={90}
        >
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
