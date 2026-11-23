import React, { useEffect, useRef } from 'react';
import type { BotStatus } from '../types';
import { botColor } from '../lib/theme';
import { pebbleFrame, pebbleParams, registerPebbleAvatar, resolvePebbleSeed } from '../lib/pebbleAvatar';

interface BotAvatarProps {
  id: string;
  bots: BotStatus[];
  avatar?: string;
  color?: string;
  className?: string;
  busy?: boolean;
}

export const BotAvatar: React.FC<BotAvatarProps> = ({
  id,
  bots,
  avatar,
  color: overrideColor,
  className = '',
  busy = false,
}) => {
  const found = bots.find((b) => b.id === id);
  const rawAvatar = avatar !== undefined ? avatar : found?.avatar;
  const seed = resolvePebbleSeed(id, rawAvatar);
  const color = botColor(id, bots, overrideColor);

  const wrapRef = useRef<HTMLDivElement>(null);
  const fxRef = useRef<SVGGElement>(null);
  const bodyRef = useRef<SVGGElement>(null);
  const pathRef = useRef<SVGPathElement>(null);
  const eyeLRef = useRef<SVGRectElement>(null);
  const eyeRRef = useRef<SVGRectElement>(null);
  const handleRef = useRef<ReturnType<typeof registerPebbleAvatar> | null>(null);

  const params = pebbleParams(seed);
  const initialFrame = pebbleFrame(params, 0, busy);

  useEffect(() => {
    const el = wrapRef.current;
    const fxGroup = fxRef.current;
    const bodyGroup = bodyRef.current;
    const pathEl = pathRef.current;
    const eyeL = eyeLRef.current;
    const eyeR = eyeRRef.current;
    if (!el || !fxGroup || !bodyGroup || !pathEl || !eyeL || !eyeR) return;

    const handle = registerPebbleAvatar({
      el,
      fxGroup,
      bodyGroup,
      pathEl,
      eyeL,
      eyeR,
      params,
      color,
      busy,
    });
    handleRef.current = handle;
    return () => {
      handle.destroy();
      handleRef.current = null;
    };
  }, [seed]);

  useEffect(() => {
    if (!handleRef.current) return;
    handleRef.current.update(params, color, busy);
  }, [params, color, busy]);

  return (
    <div ref={wrapRef} data-bot-id={id} className={`av ${className}`.trim()}>
      <svg viewBox="0 0 100 100" aria-hidden="true">
        <g ref={fxRef}>
          <g ref={bodyRef} transform={`rotate(${initialFrame.bodyRotDeg.toFixed(1)} 50 50)`}>
            <path ref={pathRef} d={initialFrame.pathD} fill={color} />
          </g>
          <rect
            ref={eyeLRef}
            x={initialFrame.eyes[0].x}
            y={initialFrame.eyes[0].y}
            width={initialFrame.eyes[0].width}
            height={initialFrame.eyes[0].height}
            rx={initialFrame.eyes[0].rx}
            transform={`rotate(${initialFrame.eyes[0].lean} ${initialFrame.eyes[0].cx} ${initialFrame.eyes[0].cy})`}
            fill="var(--eye)"
          />
          <rect
            ref={eyeRRef}
            x={initialFrame.eyes[1].x}
            y={initialFrame.eyes[1].y}
            width={initialFrame.eyes[1].width}
            height={initialFrame.eyes[1].height}
            rx={initialFrame.eyes[1].rx}
            transform={`rotate(${initialFrame.eyes[1].lean} ${initialFrame.eyes[1].cx} ${initialFrame.eyes[1].cy})`}
            fill="var(--eye)"
          />
        </g>
      </svg>
      {busy && <span className="busy" />}
    </div>
  );
};
