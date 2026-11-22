import React from 'react';
import type { BotStatus } from '../types';
import { botColor, hashStr } from '../lib/theme';

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
  const shape = (avatar !== undefined ? avatar : found?.avatar || '').trim().toLowerCase() || 'blob';
  const color = botColor(id, bots, overrideColor);

  const h = hashStr(id || '?');
  const tilt = ((h >>> 4) % 25) - 12;
  const gx = ((h >>> 9) % 7) - 3;
  const gy = ((h >>> 12) % 5) - 2;
  const lean = (h >>> 15) % 2 ? -14 : 12;

  const renderShape = () => {
    switch (shape) {
      case 'squircle':
        return (
          <>
            <path
              d="M50 10C78 10 90 22 90 50C90 78 78 90 50 90C22 90 10 78 10 50C10 22 22 10 50 10Z"
              fill={color}
            />
            <ellipse cx={38} cy={48} rx={5.5} ry={9.5} fill="var(--eye)" />
            <ellipse cx={62} cy={48} rx={5.5} ry={9.5} fill="var(--eye)" />
          </>
        );
      case 'cat':
        return (
          <>
            <path
              d="M20 34C18 16 24 10 38 22C46 19 54 19 62 22C76 10 82 16 80 34C88 44 90 58 86 70C80 86 66 91 50 91C34 91 20 86 14 70C10 58 12 44 20 34Z"
              fill={color}
            />
            <ellipse cx={38} cy={55} rx={5.5} ry={9} fill="var(--eye)" />
            <ellipse cx={62} cy={55} rx={5.5} ry={9} fill="var(--eye)" />
          </>
        );
      case 'bear':
        return (
          <>
            <path
              d="M26 35C15 33 11 21 19 14C27 7 38 14 39 23C46 21 54 21 61 23C62 14 73 7 81 14C89 21 85 33 74 35C85 45 89 60 84 73C77 86 64 91 50 91C36 91 23 86 16 73C11 60 15 45 26 35Z"
              fill={color}
            />
            <ellipse cx={38} cy={55} rx={5.5} ry={9} fill="var(--eye)" />
            <ellipse cx={62} cy={55} rx={5.5} ry={9} fill="var(--eye)" />
          </>
        );
      case 'cloud':
        return (
          <>
            <path
              d="M28 80C15 80 8 70 9 58C10 47 18 40 26 39C27 24 39 14 54 15C67 16 77 25 78 38C87 40 93 49 92 60C91 72 82 80 69 80Z"
              fill={color}
            />
            <ellipse cx={39} cy={53} rx={5.2} ry={8.5} fill="var(--eye)" />
            <ellipse cx={61} cy={53} rx={5.2} ry={8.5} fill="var(--eye)" />
          </>
        );
      case 'ghost':
        return (
          <>
            <path
              d="M50 10C74 10 88 26 88 52L88 84C88 89 82 91 78 87C73 82 68 82 63 87C58 92 53 92 50 87C47 92 42 92 37 87C32 82 27 82 22 87C18 91 12 89 12 84L12 52C12 26 26 10 50 10Z"
              fill={color}
            />
            <ellipse cx={38} cy={46} rx={5.5} ry={9.5} fill="var(--eye)" />
            <ellipse cx={62} cy={46} rx={5.5} ry={9.5} fill="var(--eye)" />
          </>
        );
      case 'star':
        return (
          <>
            <path
              d="M50 8C55 8 59 23 64 27C69 31 87 28 90 34C93 40 78 51 77 57C76 63 85 80 79 85C73 90 58 79 50 79C42 79 27 90 21 85C15 80 24 63 23 57C22 51 7 40 10 34C13 28 31 31 36 27C41 23 45 8 50 8Z"
              fill={color}
            />
            <ellipse cx={40} cy={49} rx={5} ry={8.5} fill="var(--eye)" />
            <ellipse cx={60} cy={49} rx={5} ry={8.5} fill="var(--eye)" />
          </>
        );
      case 'sprout':
        return (
          <>
            <path d="M50 28C48 16 36 9 25 13C26 23 37 29 50 28Z" fill={color} />
            <path d="M50 28C53 15 65 8 76 12C75 23 63 29 50 28Z" fill={color} />
            <path
              d="M50 28C66 28 84 40 84 62C84 82 69 92 50 92C31 92 16 82 16 62C16 40 34 28 50 28Z"
              fill={color}
            />
            <ellipse cx={39} cy={59} rx={5.2} ry={8.8} fill="var(--eye)" />
            <ellipse cx={61} cy={59} rx={5.2} ry={8.8} fill="var(--eye)" />
          </>
        );
      default:
        return (
          <g transform={`rotate(${tilt} 50 50)`}>
            <path
              d="M52 9C80 9 95 28 93 55C91 81 72 93 47 91C21 89 5 72 7 46C9 22 26 9 52 9Z"
              fill={color}
            />
            <ellipse
              cx={39 + gx}
              cy={45 + gy}
              rx={6}
              ry={10.5}
              transform={`rotate(${lean} ${39 + gx} ${45 + gy})`}
              fill="var(--eye)"
            />
            <ellipse
              cx={62 + gx}
              cy={43 + gy}
              rx={6}
              ry={10.5}
              transform={`rotate(${lean} ${62 + gx} ${43 + gy})`}
              fill="var(--eye)"
            />
          </g>
        );
    }
  };

  return (
    <div className={`av ${className}`.trim()}>
      <svg viewBox="0 0 100 100" aria-hidden="true">
        {renderShape()}
      </svg>
      {busy && <span className="busy" />}
    </div>
  );
};
