import React from 'react';
import type {
  BotStatus,
  ChatMessage,
  PendingApproval,
  PendingQuestion,
  ResolvedLang,
} from '../../types';
import { translate } from '../../lib/i18n';
import { renderMarkdownHtml } from '../../lib/markdown';
import { botColor, countToolCalls, formatClockTime, formatDurationMs, formatStamp } from '../../lib/theme';
import { ApprovalCard } from '../ApprovalCard';
import { BotAvatar } from '../BotAvatar';
import { QuestionCard } from '../QuestionCard';
import { MessageAttachments } from './MessageAttachments';

export type TimelineEntry =
  | { kind: 'msg'; msg: ChatMessage }
  | { kind: 'collab'; id: string; peerId: string; items: ChatMessage[]; createdAt: string };

interface ChatMessageListProps {
  scrollRef: React.RefObject<HTMLDivElement | null>;
  inDesktopSidebar: boolean;
  bots: BotStatus[];
  activeBot?: BotStatus;
  messages: ChatMessage[];
  timelineItems: TimelineEntry[];
  seenIds: Set<string>;
  approvals: PendingApproval[];
  questions: PendingQuestion[];
  lang: ResolvedLang;
  onOpenCreate: () => void;
  onOpenCollab?: (peerId: string) => void;
  onPreviewImage: (media: import('../../types').MessageMedia) => void;
  onReplyApproval: (
    id: string,
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    reason?: string
  ) => Promise<void>;
  onReplyQuestion: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
}

export const ChatMessageList: React.FC<ChatMessageListProps> = ({
  scrollRef,
  inDesktopSidebar,
  bots,
  activeBot,
  messages,
  timelineItems,
  seenIds,
  approvals,
  questions,
  lang,
  onOpenCreate,
  onOpenCollab,
  onPreviewImage,
  onReplyApproval,
  onReplyQuestion,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  let lastTs = 0;

  return (
    <div ref={scrollRef} id={inDesktopSidebar ? undefined : 'scroll'} className="scroll">
      <div id={inDesktopSidebar ? undefined : 'thread'} className="thread">
        {!activeBot ? (
          <div className="hello">
            <h2>{t('emptyBotsTitle')}</h2>
            <p>{t('emptyBotsDesc')}</p>
            <div className="sugs">
              <button type="button" className="sug p" onClick={onOpenCreate}>
                <b>{t('createBotBtn')}</b>
                <span>{t('createBotBtnSub')}</span>
              </button>
            </div>
          </div>
        ) : messages.length === 0 && !activeBot.busy ? (
          <div className="hello">
            <BotAvatar id={activeBot.id} bots={bots} />
            <h2>{activeBot.name}</h2>
            <p>{activeBot.description || activeBot.workspace}</p>
          </div>
        ) : (
          <>
            {timelineItems.map((entry) => {
              const stampSource = entry.kind === 'collab' ? entry.createdAt : entry.msg.createdAt;
              const ts = Date.parse(stampSource) || 0;
              let stampEl: React.ReactNode = null;
              if (!lastTs || ts - lastTs > 300000) {
                const lbl = formatStamp(stampSource, lang);
                if (lbl) stampEl = <div className="stamp">{lbl}</div>;
              }
              lastTs = ts;

              if (entry.kind === 'collab') {
                const peerBot = bots.find((b) => b.id === entry.peerId);
                const peerName = peerBot?.name || entry.peerId || 'Bot';
                const fresh = entry.items.some((item) => !seenIds.has(item.id)) ? ' fresh' : '';
                return (
                  <React.Fragment key={entry.id}>
                    {stampEl}
                    <div className={`msg bot collab-row${fresh}`}>
                      <button
                        type="button"
                        className="collab-chip"
                        style={
                          entry.peerId
                            ? ({ '--bot-accent': botColor(entry.peerId, bots) } as React.CSSProperties)
                            : undefined
                        }
                        onClick={() => entry.peerId && onOpenCollab?.(entry.peerId)}
                      >
                        <BotAvatar id={entry.peerId} bots={bots} />
                        <span className="collab-chip-txt">
                          {t('collabWith', { peer: peerName })} · {t('collabCount', { count: entry.items.length })}
                        </span>
                        <svg
                          className="collab-chip-chev"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2.2"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="m9 18 6-6-6-6" />
                        </svg>
                      </button>
                    </div>
                  </React.Fragment>
                );
              }

              const m = entry.msg;
              const fresh = seenIds.has(m.id) ? '' : ' fresh';
              const toolCount = countToolCalls(m.tools);
              const durText = formatDurationMs(m.durationMs);
              const foot = [
                formatClockTime(m.createdAt),
                toolCount > 0 ? t('msgToolCalls', { count: toolCount }) : '',
                durText ? t('msgDuration', { dur: durText }) : '',
                m.costUsd ? `$${Number(m.costUsd).toFixed(4)}` : '',
              ]
                .filter(Boolean)
                .join(' · ');

              const mediaEl = (
                <MessageAttachments media={m.media} lang={lang} onPreviewImage={onPreviewImage} />
              );

              let body: React.ReactNode;
              if (m.role === 'user') {
                const via =
                  m.from && !['web', 'user', 'cli'].includes(m.from) ? (
                    <div className="label">{t('fromLabel', { from: m.from })}</div>
                  ) : null;
                body = (
                  <div className={`msg me${fresh}`}>
                    {via}
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
                      {mediaEl}
                    </div>
                  </div>
                );
              } else if (m.role === 'assistant') {
                const rt =
                  m.from && m.from.startsWith('routine:') ? (
                    <div className="label">{t('routineReceiptFrom', { name: m.from.slice(8) })}</div>
                  ) : null;
                body = (
                  <div className={`msg bot${fresh}`}>
                    {rt}
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
                      {mediaEl}
                    </div>
                    {foot && <div className="foot">{foot}</div>}
                  </div>
                );
              } else {
                const cls = m.from === 'error' ? 'err' : m.from === 'command' ? 'cmd' : '';
                body = (
                  <div className={`msg sys ${cls}${fresh}`}>
                    {cls === 'cmd' ? (
                      <div className="bubble">{m.text}</div>
                    ) : (
                      <div className="sys-t">{m.text}</div>
                    )}
                  </div>
                );
              }

              return (
                <React.Fragment key={m.id}>
                  {stampEl}
                  {body}
                </React.Fragment>
              );
            })}
            {activeBot.busy && (
              <div className="live">
                <BotAvatar id={activeBot.id} bots={bots} />
                <span className="shimmer">{t('processing')}</span>
              </div>
            )}
            {approvals.map((apr) => (
              <ApprovalCard key={apr.id} approval={apr} lang={lang} onReply={onReplyApproval} />
            ))}
            {questions.map((q) => (
              <QuestionCard key={q.id} question={q} lang={lang} onReply={onReplyQuestion} />
            ))}
          </>
        )}
      </div>
    </div>
  );
};
