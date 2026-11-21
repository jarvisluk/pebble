import React, { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { BotStatus, ChatMessage, ResolvedLang, TakeoverRequest } from '../types';
import { translate } from '../lib/i18n';
import { renderMarkdownHtml } from '../lib/markdown';
import { botColor, contrastText, formatStamp } from '../lib/theme';
import { BotAvatar } from './BotAvatar';

interface ChatViewProps {
  hidden: boolean;
  bots: BotStatus[];
  activeBot: BotStatus | undefined;
  messages: ChatMessage[];
  seenIds: Set<string>;
  toolName?: string;
  takeovers: TakeoverRequest[];
  deskOpen: boolean;
  lang: ResolvedLang;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  onOpenMenu: () => void;
  onToggleDesktop: () => void;
  onOpenDesktopTarget: (target: string) => void;
  onResolveTakeover: (to: TakeoverRequest) => void;
  onOpenDetail: () => void;
  onOpenCreate: () => void;
  onSendMessage: (text: string) => Promise<void>;
  onInterrupt: () => Promise<void>;
  children?: React.ReactNode;
}

export const ChatView: React.FC<ChatViewProps> = ({
  hidden,
  bots,
  activeBot,
  messages,
  seenIds,
  toolName,
  takeovers,
  deskOpen,
  lang,
  inputRef,
  onOpenMenu,
  onToggleDesktop,
  onOpenDesktopTarget,
  onResolveTakeover,
  onOpenDetail,
  onOpenCreate,
  onSendMessage,
  onInterrupt,
  children,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [draft, setDraft] = useState('');
  const [popOpen, setPopOpen] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  const plusRef = useRef<HTMLButtonElement>(null);

  const botAccentStyle: React.CSSProperties | undefined = activeBot
    ? ({
        '--bot-accent': botColor(activeBot.id, bots),
        '--bot-accent-text': contrastText(botColor(activeBot.id, bots)),
      } as React.CSSProperties)
    : undefined;

  useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null;
      if (!target) return;
      if (!target.closest('#pop, #plusBtn')) {
        setPopOpen(false);
      }
      const copyBtn = target.closest<HTMLElement>('[data-copy]');
      if (copyBtn && copyBtn.closest('#thread')) {
        const code = copyBtn.closest('.code')?.querySelector('code')?.textContent || copyBtn.dataset.copy || '';
        navigator.clipboard?.writeText(code).then(() => {
          copyBtn.textContent = t('copied');
          setTimeout(() => {
            copyBtn.textContent = t('copy');
          }, 1200);
        });
      }
    };
    document.addEventListener('click', onDocClick);
    return () => document.removeEventListener('click', onDocClick);
  }, [lang]);

  useLayoutEffect(() => {
    const ta = inputRef.current;
    if (!ta) return;
    ta.style.height = 'auto';
    if (draft) {
      ta.style.height = `${Math.min(ta.scrollHeight, 220)}px`;
    }
  }, [draft, inputRef]);

  useEffect(() => {
    const sc = scrollRef.current;
    if (!sc) return;
    const nearBottom = sc.scrollHeight - sc.scrollTop - sc.clientHeight < 160;
    if (nearBottom) {
      sc.scrollTop = sc.scrollHeight;
    }
    messages.forEach((m) => seenIds.add(m.id));
  }, [messages, activeBot?.id, activeBot?.busy, toolName, seenIds]);

  const stopMode = Boolean(activeBot?.busy) && !draft.trim();
  const canSend = stopMode || (Boolean(draft.trim()) && Boolean(activeBot));

  const handleSend = async () => {
    if (stopMode) {
      await onInterrupt();
      return;
    }
    const text = draft.trim();
    if (!text || !activeBot) return;
    setDraft('');
    await onSendMessage(text);
  };

  const fillComposer = (val: string) => {
    setPopOpen(false);
    setDraft(val);
    setTimeout(() => {
      const el = inputRef.current;
      if (el) {
        el.focus();
        el.setSelectionRange(val.length, val.length);
      }
    }, 0);
  };

  const firstTakeover = takeovers[0] || null;
  let lastTs = 0;

  return (
    <div id="chatView" className="main-pane" hidden={hidden} style={botAccentStyle}>
      <div className="chat-hd">
        <button
          id="menuBtn"
          type="button"
          className="ghost menu"
          aria-label={t('expandSide')}
          title={t('expandSideTitle')}
          onClick={onOpenMenu}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M4 7h16M4 12h16M4 17h10" />
          </svg>
        </button>
        <div id="hdAv">{activeBot ? <BotAvatar id={activeBot.id} bots={bots} /> : null}</div>
        <div className="who">
          <h1 id="hdName">{activeBot ? activeBot.name : 'bot-hub'}</h1>
        </div>
        <div className="hd-acts">
          <button
            id="deskBtn"
            type="button"
            className={`ghost${deskOpen ? ' active' : ''}`}
            aria-label={t('remoteDesktop')}
            title={t('remoteDesktop')}
            onClick={onToggleDesktop}
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <rect x="2" y="4" width="20" height="14" rx="2.5" />
              <path d="M8 21h8M12 18v3" />
            </svg>
            <span id="deskDot" className="desk-dot" hidden={takeovers.length === 0} />
          </button>
          <button
            id="detailBtn"
            type="button"
            className="ghost"
            aria-label={t('details')}
            title={t('details')}
            disabled={!activeBot}
            onClick={onOpenDetail}
          >
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.9"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
              <path d="M14 3v5h5M9 13h6M9 17h4" />
            </svg>
          </button>
        </div>
      </div>

      {firstTakeover && (
        <div id="takeoverBanner" className="to-banner">
          <span id="takeoverBannerText" className="to-banner-txt">
            {t('takeoverBanner', {
              bot: firstTakeover.botName || firstTakeover.botId || 'Bot',
              target: firstTakeover.target === 'local' ? t('localHost') : firstTakeover.target,
              reason: firstTakeover.reason,
            })}
          </span>
          <div className="to-banner-acts">
            <button
              type="button"
              id="toBannerOpen"
              className="btn"
              onClick={() => onOpenDesktopTarget(firstTakeover.target || 'local')}
            >
              {t('openDesktop')}
            </button>
            <button
              type="button"
              id="toBannerResolve"
              className="btn go"
              onClick={() => onResolveTakeover(firstTakeover)}
            >
              {t('resumeAI')}
            </button>
          </div>
        </div>
      )}

      <div className="main-split">
        <div className="chat-col">
          <div ref={scrollRef} id="scroll" className="scroll">
            <div id="thread" className="thread">
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
                  <div className="sugs">
                    <button
                      type="button"
                      className="sug p"
                      onClick={() => fillComposer(t('sugAskRolePrompt'))}
                    >
                      <b>{t('sugAskRole')}</b>
                      <span>{t('sugAskRoleSub')}</span>
                    </button>
                    <button type="button" className="sug l" onClick={() => fillComposer('/status')}>
                      <b>{t('sugStatus')}</b>
                      <span>{t('sugStatusSub')}</span>
                    </button>
                    <button type="button" className="sug g" onClick={() => fillComposer('/ask ')}>
                      <b>{t('sugCollab')}</b>
                      <span>{t('sugCollabSub')}</span>
                    </button>
                  </div>
                </div>
              ) : (
                <>
                  {messages.map((m) => {
                    const ts = Date.parse(m.createdAt) || 0;
                    let stampEl: React.ReactNode = null;
                    if (!lastTs || ts - lastTs > 300000) {
                      const lbl = formatStamp(m.createdAt, lang);
                      if (lbl) stampEl = <div className="stamp">{lbl}</div>;
                    }
                    lastTs = ts;

                    const fresh = seenIds.has(m.id) ? '' : ' fresh';
                    const foot = [
                      m.durationMs ? `${(m.durationMs / 1000).toFixed(1)}s` : '',
                      m.costUsd ? `$${Number(m.costUsd).toFixed(4)}` : '',
                    ]
                      .filter(Boolean)
                      .join(' · ');

                    let body: React.ReactNode;
                    if (m.role === 'user') {
                      const via =
                        m.from && !['web', 'user', 'cli'].includes(m.from) ? (
                          <div className="label">{t('fromLabel', { from: m.from })}</div>
                        ) : null;
                      body = (
                        <div className={`msg me${fresh}`}>
                          {via}
                          <div
                            className="bubble"
                            dangerouslySetInnerHTML={{ __html: renderMarkdownHtml(m.text, t('copy')) }}
                          />
                        </div>
                      );
                    } else if (m.role === 'assistant') {
                      const rt =
                        m.from && m.from.startsWith('routine:') ? (
                          <div className="label">定时回执 · {m.from.slice(8)}</div>
                        ) : null;
                      body = (
                        <div className={`msg bot${fresh}`}>
                          {rt}
                          <div
                            className="bubble"
                            dangerouslySetInnerHTML={{ __html: renderMarkdownHtml(m.text, t('copy')) }}
                          />
                          {foot && <div className="foot">{foot}</div>}
                        </div>
                      );
                    } else if (m.role === 'bot_out' || m.role === 'bot_in') {
                      const out = m.role === 'bot_out';
                      const peer = (out ? m.to || '' : m.from || '').replace(/^bot:/, '');
                      const label = out ? t('sentToPeer', { peer }) : t('peerReply', { peer });
                      body = (
                        <div className={`msg bot peer${fresh}`}>
                          <div className="label">
                            <BotAvatar id={peer} bots={bots} />
                            {label}
                          </div>
                          <div
                            className="bubble"
                            dangerouslySetInnerHTML={{ __html: renderMarkdownHtml(m.text, t('copy')) }}
                          />
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
                      <span className="shimmer">
                        {toolName ? t('callingToolEllipsis', { tool: toolName }) : t('thinking')}
                      </span>
                    </div>
                  )}
                </>
              )}
            </div>
          </div>

          <div className="composer-wrap">
            <div className="composer">
              <button
                ref={plusRef}
                id="plusBtn"
                type="button"
                className="circle"
                aria-label={t('slashCmds')}
                title={t('slashCmds')}
                onClick={() => setPopOpen((v) => !v)}
              >
                /
              </button>
              <textarea
                ref={inputRef}
                id="input"
                rows={1}
                placeholder={activeBot ? t('msgBotPh', { name: activeBot.name }) : t('msgPh')}
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    void handleSend();
                  }
                }}
              />
              <button
                id="sendBtn"
                type="button"
                className={`send${stopMode ? ' stop' : ''}`}
                disabled={!canSend}
                aria-label={stopMode ? t('stop') : t('send')}
                onClick={() => void handleSend()}
              >
                {stopMode ? (
                  <svg viewBox="0 0 24 24" fill="currentColor">
                    <rect x="7" y="7" width="10" height="10" rx="2" />
                  </svg>
                ) : (
                  <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2.4"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <path d="M12 19V5M5 12l7-7 7 7" />
                  </svg>
                )}
              </button>
              <div ref={popRef} id="pop" className={`pop${popOpen ? ' open' : ''}`}>
                <button type="button" onClick={() => fillComposer('/status')}>
                  /status <span>{t('cmdStatusDesc')}</span>
                </button>
                <button type="button" onClick={() => fillComposer('/bots')}>
                  /bots <span>{t('cmdBotsDesc')}</span>
                </button>
                <button type="button" onClick={() => fillComposer('/ask ')}>
                  /ask &lt;bot&gt; … <span>{t('cmdAskDesc')}</span>
                </button>
                <button type="button" onClick={() => fillComposer('/reset')}>
                  /reset <span>{t('cmdResetDesc')}</span>
                </button>
              </div>
            </div>
          </div>
        </div>
        {children}
      </div>
    </div>
  );
};
