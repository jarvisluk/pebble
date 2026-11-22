import React from 'react';
import type { BotStatus, ResolvedLang } from '../types';
import { translate } from '../lib/i18n';
import { BOT_AVATARS, BOT_COLORS, botColor, normalizeBotHex } from '../lib/theme';
import { BotAvatar } from './BotAvatar';

interface BotAppearancePickerProps {
  botId: string;
  bots: BotStatus[];
  avatar: string;
  color: string;
  lang: ResolvedLang;
  onAvatarChange: (next: string) => void;
  onColorChange: (next: string) => void;
}

const AVATAR_LABEL_KEY: Record<(typeof BOT_AVATARS)[number], string> = {
  blob: 'avBlob',
  squircle: 'avSquircle',
  cat: 'avCat',
  bear: 'avBear',
  cloud: 'avCloud',
  ghost: 'avGhost',
  star: 'avStar',
  sprout: 'avSprout',
};

export const BotAppearancePicker: React.FC<BotAppearancePickerProps> = ({
  botId,
  bots,
  avatar,
  color,
  lang,
  onAvatarChange,
  onColorChange,
}) => {
  const t = (k: string) => translate(lang, k);
  const effectiveAvatar = avatar.trim().toLowerCase() || 'blob';
  const effectiveColor = botColor(botId, bots, color);
  const hasCustom = Boolean(effectiveAvatar !== 'blob' || normalizeBotHex(color));

  return (
    <div className="bot-appear">
      <div className="row-between">
        <span className="bot-appear-lbl">{t('avatarShape')}</span>
        {hasCustom && (
          <button
            type="button"
            className="link-btn"
            onClick={() => {
              onAvatarChange('');
              onColorChange('');
            }}
          >
            {t('restoreDefault')}
          </button>
        )}
      </div>
      <div className="av-grid" role="radiogroup" aria-label={t('avatarShape')}>
        {BOT_AVATARS.map((kind) => {
          const active = effectiveAvatar === kind;
          const label = t(AVATAR_LABEL_KEY[kind]);
          return (
            <button
              key={kind}
              type="button"
              role="radio"
              aria-checked={active}
              aria-label={label}
              title={label}
              className={`av-opt${active ? ' on' : ''}`}
              onClick={() => onAvatarChange(kind === 'blob' ? '' : kind)}
            >
              <BotAvatar id={botId} bots={bots} avatar={kind} color={effectiveColor} />
            </button>
          );
        })}
      </div>
      <div className="bot-appear-lbl">{t('botColorLabel')}</div>
      <div className="accent-row">
        {BOT_COLORS.map((hex) => {
          const active = normalizeBotHex(color) === hex;
          return (
            <button
              key={hex}
              type="button"
              className={`swatch${active ? ' on' : ''}`}
              style={{ backgroundColor: hex }}
              aria-label={hex}
              title={hex}
              onClick={() => onColorChange(hex)}
            />
          );
        })}
        <input
          type="color"
          className="accent-pick"
          aria-label={t('botColorLabel')}
          value={effectiveColor}
          onChange={(e) => onColorChange(e.target.value.toLowerCase())}
        />
      </div>
    </div>
  );
};
