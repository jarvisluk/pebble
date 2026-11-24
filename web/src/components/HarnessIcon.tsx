import React from 'react';

import opencodeSvg from '../assets/harnesses/opencode.svg?raw';
import codebuddyIcon from '../assets/harnesses/codebuddy.svg';
import qoderSvg from '../assets/harnesses/qoder.svg?raw';
import codexIcon from '../assets/harnesses/codex.svg';
import claudeIcon from '../assets/harnesses/claude.svg';
import grokSvg from '../assets/harnesses/grok.svg?raw';
import traeIcon from '../assets/harnesses/trae.svg';
import piSvg from '../assets/harnesses/pi.svg?raw';
import dshIcon from '../assets/harnesses/dsh.svg';
import agyIcon from '../assets/harnesses/agy.svg';

type IconEntry = { kind: 'img'; src: string } | { kind: 'svg'; raw: string };

const ICON_MAP: Record<string, IconEntry> = {
  opencode: { kind: 'svg', raw: opencodeSvg },
  codebuddy: { kind: 'img', src: codebuddyIcon },
  qoder: { kind: 'svg', raw: qoderSvg },
  codex: { kind: 'img', src: codexIcon },
  claude: { kind: 'img', src: claudeIcon },
  grok: { kind: 'svg', raw: grokSvg },
  trae: { kind: 'img', src: traeIcon },
  pi: { kind: 'svg', raw: piSvg },
  dsh: { kind: 'img', src: dshIcon },
  agy: { kind: 'img', src: agyIcon },
};

interface HarnessIconProps {
  id: string;
}

export const HarnessIcon: React.FC<HarnessIconProps> = ({ id }) => {
  const entry = ICON_MAP[id];
  return (
    <span className="harness-icon" aria-hidden="true">
      {!entry ? (
        <span className="harness-icon-fallback">{id.slice(0, 1).toUpperCase()}</span>
      ) : entry.kind === 'svg' ? (
        <span className="harness-icon-svg" dangerouslySetInnerHTML={{ __html: entry.raw }} />
      ) : (
        <img src={entry.src} alt="" width={20} height={20} className="harness-icon-img" />
      )}
    </span>
  );
};
