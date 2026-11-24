import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type {
  ActiveDesktopTarget,
  BotStatus,
  ChatMessage,
  MentionCandidate,
  MessageMedia,
  PendingAttachment,
  PendingQuestion,
  ResolvedLang,
  SlashCommandItem,
  TakeoverRequest,
} from '../types';
import { fetchBotCommands, fetchBotMentions, uploadBotAttachment } from '../lib/api';
import { translate } from '../lib/i18n';
import { renderMarkdownHtml } from '../lib/markdown';
import { botColor, contrastText, countToolCalls, formatClockTime, formatDurationMs, formatStamp } from '../lib/theme';
import type { ViewDir, ViewState } from '../lib/useViewTransition';
import { BotAvatar } from './BotAvatar';
import { QuestionCard } from './QuestionCard';

export function isImageMedia(m: MessageMedia): boolean {
  if (m.kind === 'image') return true;
  if (m.kind === 'file') return false;
  return Boolean(m.mimeType && m.mimeType.startsWith('image/'));
}

export function formatFileSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export const MessageAttachments: React.FC<{
  media?: MessageMedia[];
  lang: ResolvedLang;
  onPreviewImage: (img: MessageMedia) => void;
}> = ({ media, lang, onPreviewImage }) => {
  if (!media || media.length === 0) return null;
  const t = (k: string) => translate(lang, k);
  const images = media.filter(isImageMedia);
  const files = media.filter((m) => !isImageMedia(m));

  return (
    <>
      {images.length > 0 && (
        <div className={`msg-media${images.length === 1 ? ' single' : ''}`}>
          {images.map((img) => (
            <button
              key={img.id}
              type="button"
              className="msg-img-btn"
              onClick={() => onPreviewImage(img)}
            >
              <img
                className="msg-img"
                src={img.url}
                alt={img.alt || img.name || t('imageAltFallback')}
                loading="lazy"
              />
            </button>
          ))}
        </div>
      )}
      {files.length > 0 && (
        <div className="msg-files">
          {files.map((f) => {
            const name = f.name || f.id || t('fileAltFallback');
            const sizeStr = formatFileSize(f.size);
            const href = `${f.url}?download=1&name=${encodeURIComponent(name)}`;
            return (
              <a
                key={f.id}
                className="msg-file-card"
                href={href}
                download={name}
                title={t('downloadFile')}
              >
                <svg
                  className="msg-file-ico"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.9"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
                  <path d="M14 3v5h5" />
                </svg>
                <span className="msg-file-meta">
                  <span className="msg-file-name">{name}</span>
                  {sizeStr ? <span className="msg-file-size">{sizeStr}</span> : null}
                </span>
                <svg
                  className="msg-file-dl"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M12 4v12m0 0-4-4m4 4 4-4M4 20h16" />
                </svg>
              </a>
            );
          })}
        </div>
      )}
    </>
  );
};

interface ChatViewProps {
  hidden: boolean;
  viewState: ViewState;
  viewDir: ViewDir;
  viewAnim: boolean;
  bots: BotStatus[];
  activeBot: BotStatus | undefined;
  messages: ChatMessage[];
  seenIds: Set<string>;
  toolName?: string;
  takeovers: TakeoverRequest[];
  activeDeskTarget?: ActiveDesktopTarget;
  questions: PendingQuestion[];
  deskOpen: boolean;
  inDesktopSidebar?: boolean;
  lang: ResolvedLang;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  pendingInsert?: { seq: number; text: string } | null;
  onOpenMenu: () => void;
  onToggleDesktop: () => void;
  onOpenFiles?: () => void;
  onOpenDesktopTarget: (target: string) => void;
  onSwitchBackLocal?: () => void;
  onResolveTakeover: (to: TakeoverRequest) => void;
  onReplyQuestion: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
  onOpenDetail: () => void;
  onOpenCollab?: (peerId: string) => void;
  onOpenCreate: () => void;
  onSelectBot?: (id: string) => void;
  onCloseDesktopChat?: () => void;
  onSendMessage: (text: string, mediaIds?: string[]) => Promise<void>;
  onInterrupt: () => Promise<void>;
}

export const ChatView: React.FC<ChatViewProps> = ({
  hidden,
  viewState,
  viewDir,
  viewAnim,
  bots,
  activeBot,
  messages,
  seenIds,
  toolName,
  takeovers,
  activeDeskTarget,
  questions,
  deskOpen,
  inDesktopSidebar = false,
  lang,
  inputRef,
  pendingInsert,
  onOpenMenu,
  onToggleDesktop,
  onOpenFiles,
  onOpenDesktopTarget,
  onSwitchBackLocal,
  onResolveTakeover,
  onReplyQuestion,
  onOpenDetail,
  onOpenCollab,
  onOpenCreate,
  onSelectBot,
  onCloseDesktopChat,
  onSendMessage,
  onInterrupt,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [draft, setDraft] = useState('');
  const [cursorPos, setCursorPos] = useState(0);
  const [attachments, setAttachments] = useState<PendingAttachment[]>([]);
  const [dragging, setDragging] = useState(false);
  const [commandsMap, setCommandsMap] = useState<Record<string, SlashCommandItem[]>>({});
  const [mentionItems, setMentionItems] = useState<MentionCandidate[]>([]);
  const [acIndex, setAcIndex] = useState(0);
  const [acDismissed, setAcDismissed] = useState(false);
  const [previewImg, setPreviewImg] = useState<MessageMedia | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const acRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setPreviewImg(null);
    setAttachments((prev) => {
      prev.forEach((a) => {
        if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
      });
      return [];
    });
    setMentionItems([]);
    setAcDismissed(false);
  }, [activeBot?.id]);

  useEffect(() => {
    const botId = activeBot?.id;
    if (!botId) return;
    let cancelled = false;
    void fetchBotCommands(botId)
      .then((cmds) => {
        if (!cancelled) {
          setCommandsMap((prev) => ({ ...prev, [botId]: cmds }));
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [activeBot?.id]);

  useEffect(() => {
    if (!previewImg) return;
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        setPreviewImg(null);
      }
    };
    document.addEventListener('keydown', onEsc, true);
    return () => document.removeEventListener('keydown', onEsc, true);
  }, [previewImg]);

  const timelineItems = useMemo(() => {
    type TimelineEntry =
      | { kind: 'msg'; msg: ChatMessage }
      | { kind: 'collab'; id: string; peerId: string; items: ChatMessage[]; createdAt: string };
    const entries: TimelineEntry[] = [];
    for (const m of messages) {
      if (m.role === 'bot_out' || m.role === 'bot_in') {
        const peerId = (m.role === 'bot_out' ? m.to || '' : m.from || '').replace(/^bot:/, '');
        const last = entries[entries.length - 1];
        if (last && last.kind === 'collab' && last.peerId === peerId) {
          last.items.push(m);
        } else {
          entries.push({
            kind: 'collab',
            id: `collab-${m.id}`,
            peerId,
            items: [m],
            createdAt: m.createdAt,
          });
        }
      } else {
        entries.push({ kind: 'msg', msg: m });
      }
    }
    return entries;
  }, [messages]);

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
      if (!target.closest('.ac-pop, #input')) {
        setAcDismissed(true);
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
  }, [messages, questions.length, activeBot?.id, activeBot?.busy, toolName, seenIds]);

  const acContext = useMemo(() => {
    if (acDismissed) return null;
    const before = draft.slice(0, Math.min(cursorPos, draft.length));
    const slashMatch = /^\/([^\s]*)$/.exec(before);
    if (slashMatch) {
      return {
        kind: 'slash' as const,
        query: slashMatch[1],
        start: 0,
        end: before.length,
      };
    }
    const mentionMatch = /(?:^|\s)@([^\s]*)$/.exec(before);
    if (mentionMatch) {
      const tokenStart = before.length - mentionMatch[1].length - 1;
      return {
        kind: 'mention' as const,
        query: mentionMatch[1],
        start: tokenStart,
        end: before.length,
      };
    }
    return null;
  }, [draft, cursorPos, acDismissed]);

  const botCommands = useMemo(() => {
    if (!activeBot) return [];
    return commandsMap[activeBot.id] || [];
  }, [activeBot, commandsMap]);

  const filteredCommands = useMemo(() => {
    if (!acContext || acContext.kind !== 'slash') return [];
    const q = acContext.query.toLowerCase();
    return botCommands.filter(
      (c) =>
        !q ||
        c.name.toLowerCase().includes(q) ||
        (c.description && c.description.toLowerCase().includes(q))
    );
  }, [acContext, botCommands]);

  useEffect(() => {
    if (!acContext || acContext.kind !== 'mention' || !activeBot) {
      setMentionItems([]);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void fetchBotMentions(activeBot.id, acContext.query)
        .then((items) => {
          if (!cancelled) setMentionItems(items);
        })
        .catch(() => {
          if (!cancelled) setMentionItems([]);
        });
    }, 120);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [acContext, activeBot?.id]);

  const acOpen = Boolean(
    acContext &&
      ((acContext.kind === 'slash' && filteredCommands.length > 0) ||
        (acContext.kind === 'mention' && mentionItems.length > 0))
  );

  const acListLen = acContext?.kind === 'slash' ? filteredCommands.length : mentionItems.length;

  useEffect(() => {
    setAcIndex(0);
  }, [acContext?.kind, acContext?.query, acListLen]);

  useEffect(() => {
    if (!acOpen || !acRef.current) return;
    const activeEl = acRef.current.querySelector<HTMLElement>('.ac-item.active');
    activeEl?.scrollIntoView({ block: 'nearest' });
  }, [acIndex, acOpen]);

  const applySlashCommand = (cmd: SlashCommandItem) => {
    const immediateNoArg = new Set(['status', 'bots', 'reset', 'compact', 'clear', 'cost', 'context']);
    const nextText = immediateNoArg.has(cmd.name) && !cmd.argumentHint ? `/${cmd.name}` : `/${cmd.name} `;
    setAcDismissed(true);
    setDraft(nextText);
    setCursorPos(nextText.length);
    setTimeout(() => {
      const el = inputRef.current;
      if (el) {
        el.focus();
        el.setSelectionRange(nextText.length, nextText.length);
      }
    }, 0);
  };

  const applyMention = (item: MentionCandidate) => {
    if (!acContext) return;
    const inserted = `@${item.value} `;
    const nextText = draft.slice(0, acContext.start) + inserted + draft.slice(acContext.end);
    const nextCursor = acContext.start + inserted.length;
    setAcDismissed(true);
    setDraft(nextText);
    setCursorPos(nextCursor);
    setTimeout(() => {
      const el = inputRef.current;
      if (el) {
        el.focus();
        el.setSelectionRange(nextCursor, nextCursor);
      }
    }, 0);
  };

  const handleUploadFiles = (fileList: FileList | File[]) => {
    const botId = activeBot?.id;
    if (!botId) return;
    const files = Array.from(fileList);
    for (const file of files) {
      const localId = `tmp_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;
      const isImg = file.type.startsWith('image/');
      const previewUrl = isImg ? URL.createObjectURL(file) : undefined;
      setAttachments((prev) => [
        ...prev,
        {
          id: localId,
          name: file.name || 'file',
          kind: isImg ? 'image' : 'file',
          previewUrl,
          uploading: true,
        },
      ]);
      void uploadBotAttachment(botId, file)
        .then((res) => {
          setAttachments((prev) =>
            prev.map((a) =>
              a.id === localId
                ? {
                    ...a,
                    media: res.media,
                    name: res.media.name || a.name,
                    kind: res.media.kind === 'image' ? 'image' : 'file',
                    uploading: false,
                  }
                : a
            )
          );
        })
        .catch((err) => {
          setAttachments((prev) =>
            prev.map((a) =>
              a.id === localId
                ? {
                    ...a,
                    uploading: false,
                    error: err instanceof Error ? err.message : t('uploadFailed'),
                  }
                : a
            )
          );
        });
    }
  };

  const removeAttachment = (id: string) => {
    setAttachments((prev) => {
      const target = prev.find((a) => a.id === id);
      if (target?.previewUrl) URL.revokeObjectURL(target.previewUrl);
      return prev.filter((a) => a.id !== id);
    });
  };

  const readyMediaIds = attachments.filter((a) => a.media?.id && !a.uploading && !a.error).map((a) => a.media!.id);
  const hasUploading = attachments.some((a) => a.uploading);
  const hasContent = Boolean(draft.trim()) || readyMediaIds.length > 0;
  const stopMode = Boolean(activeBot?.busy) && !hasContent;
  const canSend = stopMode || (hasContent && !hasUploading && Boolean(activeBot));

  const handleSend = async () => {
    if (stopMode) {
      await onInterrupt();
      return;
    }
    const text = draft.trim();
    if ((!text && readyMediaIds.length === 0) || hasUploading || !activeBot) return;
    const sentMediaIds = [...readyMediaIds];
    setDraft('');
    setCursorPos(0);
    setAcDismissed(false);
    setAttachments((prev) => {
      prev.forEach((a) => {
        if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
      });
      return [];
    });
    await onSendMessage(text, sentMediaIds);
  };

  useEffect(() => {
    if (!pendingInsert || !pendingInsert.text) return;
    setDraft((prev) => {
      const needsSpace = prev.length > 0 && !/\s$/.test(prev);
      return prev + (needsSpace ? ' ' : '') + pendingInsert.text;
    });
    window.requestAnimationFrame(() => inputRef.current?.focus());
  }, [pendingInsert?.seq]);

  const firstTakeover = takeovers[0] || null;
  let lastTs = 0;

  return (
    <div
      id={inDesktopSidebar ? 'deskChatView' : 'chatView'}
      className={`main-pane${inDesktopSidebar ? ' desk-chat-pane' : ' view-pane'}`}
      hidden={inDesktopSidebar ? false : hidden}
      aria-hidden={inDesktopSidebar ? false : viewState !== 'active'}
      data-view-state={inDesktopSidebar ? 'active' : viewState}
      data-view-dir={inDesktopSidebar ? undefined : viewDir}
      data-view-anim={inDesktopSidebar ? '0' : viewAnim ? '1' : '0'}
      style={botAccentStyle}
    >
      <div className="chat-hd">
        {!inDesktopSidebar && (
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
        )}
        <div id={inDesktopSidebar ? undefined : 'hdAv'}>
          {activeBot ? <BotAvatar id={activeBot.id} bots={bots} /> : null}
        </div>
        <div className="who">
          {inDesktopSidebar && bots.length > 0 ? (
            <select
              id="deskChatBotSel"
              className="desk-chat-bot-sel"
              aria-label={t('deskSwitchBot')}
              value={activeBot?.id || ''}
              onChange={(e) => {
                if (e.target.value) onSelectBot?.(e.target.value);
              }}
            >
              {bots.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.name}
                </option>
              ))}
            </select>
          ) : (
            <h1 id={inDesktopSidebar ? undefined : 'hdName'}>{activeBot ? activeBot.name : 'Pebble'}</h1>
          )}
        </div>
        <div className="hd-acts">
          {inDesktopSidebar ? (
            <button
              id="deskChatCloseBtn"
              type="button"
              className="ghost"
              aria-label={t('close')}
              title={t('close')}
              onClick={onCloseDesktopChat}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <path d="M6 6l12 12M18 6 6 18" />
              </svg>
            </button>
          ) : (
            <>
              {onOpenFiles && (
                <button
                  id="filesBtn"
                  type="button"
                  className="ghost"
                  aria-label={t('workspaceFiles')}
                  title={t('workspaceFiles')}
                  disabled={!activeBot}
                  onClick={onOpenFiles}
                >
                  <svg
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.9"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
                  </svg>
                </button>
              )}
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
            </>
          )}
        </div>
      </div>

      {!inDesktopSidebar && activeDeskTarget?.temporary && (
        <div className="to-banner temp-target">
          <span className="to-banner-txt">
            {t('tempDesktopBanner', {
              name: activeDeskTarget.name || activeDeskTarget.target,
            })}
          </span>
          <div className="to-banner-acts">
            <button
              type="button"
              className="btn"
              onClick={() => onOpenDesktopTarget(activeDeskTarget.target)}
            >
              {t('openDesktop')}
            </button>
            {onSwitchBackLocal && (
              <button type="button" className="btn" onClick={onSwitchBackLocal}>
                {t('switchBackLocal')}
              </button>
            )}
          </div>
        </div>
      )}

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
                      <MessageAttachments media={m.media} lang={lang} onPreviewImage={setPreviewImg} />
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
                              <div dangerouslySetInnerHTML={{ __html: renderMarkdownHtml(m.text, t('copy')) }} />
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
                              <div dangerouslySetInnerHTML={{ __html: renderMarkdownHtml(m.text, t('copy')) }} />
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
                  {questions.map((q) => (
                    <QuestionCard key={q.id} question={q} lang={lang} onReply={onReplyQuestion} />
                  ))}
                </>
              )}
            </div>
          </div>

          <div className="composer-wrap">
            {attachments.length > 0 && (
              <div className="composer-attachments">
                {attachments.map((att) => (
                  <div
                    key={att.id}
                    className={`att-chip${att.kind === 'image' ? ' is-img' : ''}${att.error ? ' is-err' : ''}`}
                    title={att.error || att.name}
                  >
                    {att.kind === 'image' && (att.previewUrl || att.media?.url) ? (
                      <img src={att.previewUrl || att.media?.url} alt={att.name} className="att-thumb" />
                    ) : (
                      <span className="att-file-body">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
                          <path d="M14 3v5h5" />
                        </svg>
                        <span className="att-name">{att.name}</span>
                      </span>
                    )}
                    {att.uploading && <span className="att-status">{t('uploading')}</span>}
                    <button
                      type="button"
                      className="att-rm"
                      aria-label={t('removeAttachment')}
                      onClick={() => removeAttachment(att.id)}
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
            )}
            <div
              className={`composer${dragging ? ' dragging' : ''}`}
              onDragOver={(e) => {
                if (e.dataTransfer?.types.includes('Files')) {
                  e.preventDefault();
                  setDragging(true);
                }
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(e) => {
                e.preventDefault();
                setDragging(false);
                if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
                  handleUploadFiles(e.dataTransfer.files);
                }
              }}
            >
              <input
                ref={fileInputRef}
                type="file"
                multiple
                hidden
                onChange={(e) => {
                  if (e.target.files && e.target.files.length > 0) {
                    handleUploadFiles(e.target.files);
                    e.target.value = '';
                  }
                }}
              />
              <button
                id="plusBtn"
                type="button"
                className="circle"
                aria-label={t('attachFile')}
                title={t('attachFile')}
                disabled={!activeBot}
                onClick={() => fileInputRef.current?.click()}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21.44 11.05l-8.49 8.49a5 5 0 0 1-7.07-7.07l8.49-8.49a3.5 3.5 0 0 1 4.95 4.95l-8.49 8.49a2 2 0 0 1-2.83-2.83l7.78-7.78" />
                </svg>
              </button>
              <textarea
                ref={inputRef}
                id="input"
                rows={1}
                placeholder={activeBot ? t('msgBotPh', { name: activeBot.name }) : t('msgPh')}
                value={draft}
                onChange={(e) => {
                  setAcDismissed(false);
                  setDraft(e.target.value);
                  setCursorPos(e.target.selectionStart ?? e.target.value.length);
                }}
                onClick={(e) => {
                  setCursorPos((e.target as HTMLTextAreaElement).selectionStart ?? draft.length);
                }}
                onKeyUp={(e) => {
                  if (!['ArrowUp', 'ArrowDown', 'Enter', 'Tab', 'Escape'].includes(e.key)) {
                    setCursorPos((e.target as HTMLTextAreaElement).selectionStart ?? draft.length);
                  }
                }}
                onPaste={(e) => {
                  const files = Array.from(e.clipboardData?.files || []);
                  if (files.length > 0) {
                    e.preventDefault();
                    handleUploadFiles(files);
                  }
                }}
                onKeyDown={(e) => {
                  if (acOpen && !e.nativeEvent.isComposing) {
                    if (e.key === 'ArrowDown') {
                      e.preventDefault();
                      setAcIndex((i) => (i + 1) % acListLen);
                      return;
                    }
                    if (e.key === 'ArrowUp') {
                      e.preventDefault();
                      setAcIndex((i) => (i - 1 + acListLen) % acListLen);
                      return;
                    }
                    if (e.key === 'Escape') {
                      e.preventDefault();
                      setAcDismissed(true);
                      return;
                    }
                    if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
                      e.preventDefault();
                      if (acContext?.kind === 'slash' && filteredCommands[acIndex]) {
                        applySlashCommand(filteredCommands[acIndex]);
                      } else if (acContext?.kind === 'mention' && mentionItems[acIndex]) {
                        applyMention(mentionItems[acIndex]);
                      }
                      return;
                    }
                  }
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
              {acOpen && (
                <div ref={acRef} className="ac-pop open" role="listbox">
                  {acContext?.kind === 'slash'
                    ? filteredCommands.map((cmd, idx) => {
                        const srcKey =
                          cmd.source === 'hub'
                            ? 'cmdSourceHub'
                            : cmd.source === 'skill'
                              ? 'cmdSourceSkill'
                              : cmd.source === 'workflow'
                                ? 'cmdSourceWorkflow'
                                : 'cmdSourceHarness';
                        return (
                          <button
                            key={`${cmd.source}-${cmd.name}`}
                            type="button"
                            role="option"
                            aria-selected={idx === acIndex}
                            className={`ac-item${idx === acIndex ? ' active' : ''}`}
                            onMouseEnter={() => setAcIndex(idx)}
                            onClick={() => applySlashCommand(cmd)}
                          >
                            <span className="ac-main">
                              <span className="ac-title">
                                /{cmd.name}
                                {cmd.argumentHint ? <span className="ac-hint"> {cmd.argumentHint}</span> : null}
                              </span>
                              {cmd.description ? <span className="ac-desc">{cmd.description}</span> : null}
                            </span>
                            <span className={`ac-tag src-${cmd.source}`}>{t(srcKey)}</span>
                          </button>
                        );
                      })
                    : mentionItems.map((item, idx) => {
                        const kindKey =
                          item.kind === 'bot'
                            ? 'mentionBot'
                            : item.kind === 'agent'
                              ? 'mentionAgent'
                              : 'mentionFile';
                        return (
                          <button
                            key={`${item.kind}-${item.value}`}
                            type="button"
                            role="option"
                            aria-selected={idx === acIndex}
                            className={`ac-item${idx === acIndex ? ' active' : ''}`}
                            onMouseEnter={() => setAcIndex(idx)}
                            onClick={() => applyMention(item)}
                          >
                            <span className="ac-main">
                              <span className="ac-title">
                                {item.kind === 'file' ? item.value : `@${item.value}`}
                              </span>
                              {item.description && item.description !== item.value ? (
                                <span className="ac-desc">{item.description}</span>
                              ) : null}
                            </span>
                            <span className={`ac-tag kind-${item.kind}`}>{t(kindKey)}</span>
                          </button>
                        );
                      })}
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
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
    </div>
  );
};
