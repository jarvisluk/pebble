import React, { useEffect, useMemo, useRef, useState } from 'react';
import type { BotStatus, ChatMessage, MessageMedia, ResolvedLang } from '../types';
import { fetchCollabMessages } from '../lib/api';
import { translate } from '../lib/i18n';
import { renderMarkdownHtml } from '../lib/markdown';
import { botColor, contrastText, formatStamp } from '../lib/theme';
import { BotAvatar } from './BotAvatar';
import { MessageAttachments } from './ChatView';

interface CollabPanelProps {
  open: boolean;
  bot: BotStatus | undefined;
  peerId: string | null;
  bots: BotStatus[];
  liveMessages: ChatMessage[];
  lang: ResolvedLang;
  onClose: () => void;
}

export const CollabPanel: React.FC<CollabPanelProps> = ({
  open,
  bot,
  peerId,
  bots,
  liveMessages,
  lang,
  onClose,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [history, setHistory] = useState<ChatMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const [previewImg, setPreviewImg] = useState<MessageMedia | null>(null);
  const bodyRef = useRef<HTMLDivElement>(null);

  const botId = bot?.id || '';
  const peerBot = useMemo(() => bots.find((b) => b.id === peerId), [bots, peerId]);
  const botLabel = bot?.name || botId;
  const peerLabel = peerBot?.name || peerId || '';

  useEffect(() => {
    if (!open || !botId || !peerId) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    fetchCollabMessages<ChatMessage>(botId, peerId, 200)
      .then((list) => {
        if (!cancelled) {
          setHistory(list);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setHistory([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open, botId, peerId]);

  useEffect(() => {
    if (!open) {
      setPreviewImg(null);
    }
  }, [open]);

  const items = useMemo(() => {
    if (!peerId) return [];
    const peerTag = `bot:${peerId}`;
    const map = new Map<string, ChatMessage>();
    for (const m of history) {
      map.set(m.id, m);
    }
    for (const m of liveMessages) {
      if ((m.role === 'bot_out' || m.role === 'bot_in') && (m.from === peerTag || m.to === peerTag)) {
        map.set(m.id, m);
      }
    }
    return Array.from(map.values()).sort((a, b) => {
      const ta = Date.parse(a.createdAt) || 0;
      const tb = Date.parse(b.createdAt) || 0;
      return ta - tb;
    });
  }, [history, liveMessages, peerId]);

  useEffect(() => {
    if (open && bodyRef.current) {
      bodyRef.current.scrollTop = bodyRef.current.scrollHeight;
    }
  }, [open, items.length]);

  const botAccentStyle: React.CSSProperties | undefined = bot
    ? ({
        '--bot-accent': botColor(bot.id, bots),
        '--bot-accent-text': contrastText(botColor(bot.id, bots)),
      } as React.CSSProperties)
    : undefined;

  let lastTs = 0;

  return (
    <>
      <aside
        id="collabPanel"
        className={`panel collab-panel${open ? ' open' : ''}`}
        aria-hidden={!open}
        style={botAccentStyle}
      >
        <div className="panel-hd">
          <div className="collab-hd-title">
            {peerId ? <BotAvatar id={peerId} bots={bots} /> : null}
            <h2>{peerId ? t('collabTitle', { bot: botLabel, peer: peerLabel }) : ''}</h2>
          </div>
          <button
            type="button"
            className="ghost"
            aria-label={t('close')}
            onClick={onClose}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round">
              <path d="M6 6l12 12M18 6 6 18" />
            </svg>
          </button>
        </div>

        <div ref={bodyRef} className="panel-body collab-body">
          {loading && items.length === 0 ? (
            <div className="collab-empty">{t('loading')}</div>
          ) : items.length === 0 ? (
            <div className="collab-empty">{t('collabEmpty')}</div>
          ) : (
            <div className="collab-list">
              {items.map((m) => {
                const ts = Date.parse(m.createdAt) || 0;
                let stampEl: React.ReactNode = null;
                if (!lastTs || ts - lastTs > 300000) {
                  const lbl = formatStamp(m.createdAt, lang);
                  if (lbl) stampEl = <div className="stamp">{lbl}</div>;
                }
                lastTs = ts;

                const out = m.role === 'bot_out';
                const label = out ? t('sentToPeer', { peer: peerLabel }) : t('peerReply', { peer: peerLabel });
                const foot = [
                  m.durationMs ? `${(m.durationMs / 1000).toFixed(1)}s` : '',
                  m.costUsd ? `$${Number(m.costUsd).toFixed(4)}` : '',
                ]
                  .filter(Boolean)
                  .join(' · ');

                const senderId = out ? botId : peerId || '';
                const senderColor = senderId ? botColor(senderId, bots) : undefined;
                return (
                  <React.Fragment key={m.id}>
                    {stampEl}
                    <div
                      className={`msg ${out ? 'me' : 'bot peer'}`}
                      style={senderColor ? ({ '--bot-accent': senderColor } as React.CSSProperties) : undefined}
                    >
                      <div className="label">
                        <BotAvatar id={senderId} bots={bots} />
                        {label}
                      </div>
                      <div className="bubble">
                        {m.text ? (
                          <div
                            dangerouslySetInnerHTML={{
                              __html: renderMarkdownHtml(m.text, t('copy'), {
                                details: t('mdDetails'),
                                image: t('mdImage'),
                              }),
                            }}
                          />
                        ) : null}
                        <MessageAttachments media={m.media} lang={lang} onPreviewImage={setPreviewImg} />
                      </div>
                      {foot && <div className="foot">{foot}</div>}
                    </div>
                  </React.Fragment>
                );
              })}
            </div>
          )}
        </div>
      </aside>

      {previewImg && (
        <div
          className="img-lightbox"
          role="dialog"
          aria-modal="true"
          onClick={() => setPreviewImg(null)}
        >
          <img
            src={previewImg.url}
            alt={previewImg.alt || previewImg.name || t('imageAltFallback')}
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}
    </>
  );
};
