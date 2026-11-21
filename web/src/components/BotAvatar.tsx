import React from 'react';
import type { BotStatus } from '../types';
import { botColor, hashStr } from '../lib/theme';

interface BotAvatarProps {
  id: string;
  bots: BotStatus[];
  className?: string;
  busy?: boolean;
}

export const BotAvatar: React.FC<BotAvatarProps> = ({ id, bots, className = '', busy = false }) => {
  const h = hashStr(id || '?');
  const color = botColor(id, bots);
  const tilt = ((h >>> 4) % 25) - 12;
  const gx = ((h >>> 9) % 7) - 3;
  const gy = ((h >>> 12) % 5) - 2;
  const lean = (h >>> 15) % 2 ? -14 : 12;

  return (
    <div className={`av ${className}`.trim()}>
      <svg viewBox="0 0 100 100" aria-hidden="true">
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
      </svg>
      {busy && <span className="busy" />}
    </div>
  );
};
