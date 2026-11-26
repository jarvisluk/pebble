import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type {
  ActiveDesktopTarget,
  BotStatus,
  BotTask,
  ChatMessage,
  HarnessInfo,
  MessageMedia,
  NodeInfo,
  PendingApproval,
  PendingAttachment,
  PendingQuestion,
  PermissionMode,
  ResolvedLang,
  StageRef,
  TakeoverRequest,
} from '../types';
import { uploadBotAttachment } from '../lib/api';
import { translate } from '../lib/i18n';
import { botColor, contrastText } from '../lib/theme';
import type { ViewDir, ViewState } from '../lib/useViewTransition';

import { ChatComposer } from './chat/ChatComposer';
import { ChatHeader } from './chat/ChatHeader';
import { ChatMessageList, type TimelineEntry } from './chat/ChatMessageList';
import { TaskStrip } from './chat/TaskStrip';
import { isImageMedia, formatFileSize, MessageAttachments } from './chat/MessageAttachments';
import { useChatAutocomplete } from './chat/useChatAutocomplete';

export { isImageMedia, formatFileSize, MessageAttachments };

interface ChatViewProps {
  hidden: boolean;
  viewState: ViewState;
  viewDir: ViewDir;
  viewAnim: boolean;
  bots: BotStatus[];
  activeBot: BotStatus | undefined;
  messages: ChatMessage[];
  tasks?: BotTask[];
  taskToolMap?: Record<string, string>;
  nodes?: NodeInfo[];
  seenIds: Set<string>;
  toolName?: string;
  takeovers: TakeoverRequest[];
  activeDeskTarget?: ActiveDesktopTarget;
  approvals: PendingApproval[];
  questions: PendingQuestion[];
  harnesses?: HarnessInfo[];
  deskOpen: boolean;
  hasUnreadLatestStage?: boolean;
  inDesktopSidebar?: boolean;
  lang: ResolvedLang;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  pendingInsert?: { seq: number; text: string } | null;
  onOpenMenu: () => void;
  onToggleDesktop: () => void;
  onOpenStage?: () => void;
  onOpenStageRef?: (ref: StageRef) => void;
  onOpenDesktopTarget: (target: string) => void;
  onSwitchBackLocal?: () => void;
  onResolveTakeover: (to: TakeoverRequest) => void;
  onReplyApproval: (
    id: string,
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    reason?: string
  ) => Promise<void>;
  onReplyQuestion: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
  onChangePermissionMode?: (botId: string, mode: PermissionMode) => Promise<void>;
  onOpenDetail: () => void;
  onInspectTask?: (taskId: string) => void;
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
  tasks = [],
  taskToolMap = {},
  nodes = [],
  seenIds,
  toolName,
  takeovers,
  activeDeskTarget,
  approvals,
  questions,
  harnesses,
  deskOpen,
  hasUnreadLatestStage = false,
  inDesktopSidebar = false,
  lang,
  inputRef,
  pendingInsert,
  onOpenMenu,
  onToggleDesktop,
  onOpenStage,
  onOpenStageRef,
  onOpenDesktopTarget,
  onSwitchBackLocal,
  onResolveTakeover,
  onReplyApproval,
  onReplyQuestion,
  onChangePermissionMode,
  onOpenDetail,
  onInspectTask,
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
  const {
    acRef,
    acContext,
    filteredCommands,
    mentionItems,
    acOpen,
    acIndex,
    setAcIndex,
    setAcDismissed,
    applySlashCommand,
    applyMention,
  } = useChatAutocomplete({
    botId: activeBot?.id,
    draft,
    cursorPos,
    inputRef,
    setDraft,
    setCursorPos,
  });
  const [previewImg, setPreviewImg] = useState<MessageMedia | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const chatColRef = useRef<HTMLDivElement>(null);
  const composerWrapRef = useRef<HTMLDivElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);


  const acListLen = acContext?.kind === 'slash' ? filteredCommands.length : mentionItems.length;

  useEffect(() => {
    setPreviewImg(null);
    setAttachments((prev) => {
      prev.forEach((a) => {
        if (a.previewUrl) URL.revokeObjectURL(a.previewUrl);
      });
      return [];
    });
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

  useLayoutEffect(() => {
    const el = composerWrapRef.current;
    const col = chatColRef.current;
    if (!el || !col) return;
    let prevH = 0;
    const sync = () => {
      const h = Math.ceil(el.getBoundingClientRect().height);
      const sc = scrollRef.current;
      const nearBottom = sc ? sc.scrollHeight - sc.scrollTop - sc.clientHeight < 160 : false;
      col.style.setProperty('--composer-h', `${h}px`);
      if (h > prevH && nearBottom && sc) {
        sc.scrollTop = sc.scrollHeight;
      }
      prevH = h;
    };
    sync();
    const ro = new ResizeObserver(sync);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useEffect(() => {
    const sc = scrollRef.current;
    if (!sc) return;
    const nearBottom = sc.scrollHeight - sc.scrollTop - sc.clientHeight < 160;
    if (nearBottom) {
      sc.scrollTop = sc.scrollHeight;
    }
    messages.forEach((m) => seenIds.add(m.id));
  }, [messages, approvals.length, questions.length, activeBot?.id, activeBot?.busy, toolName, seenIds]);

  const availableModes = useMemo<PermissionMode[]>(() => {
    const hInfo = harnesses?.find((h) => h.id === activeBot?.harness);
    const modes = hInfo?.permissions?.modes;
    if (modes && modes.length > 0) return modes;
    return ['auto', 'edit', 'ask', 'plan'];
  }, [harnesses, activeBot?.harness]);

  const currentMode: PermissionMode =
    activeBot?.permissionMode && availableModes.includes(activeBot.permissionMode)
      ? activeBot.permissionMode
      : availableModes[0] || 'auto';

  const [modeBusy, setModeBusy] = useState(false);

  const switchMode = async (next: PermissionMode) => {
    if (!activeBot || !onChangePermissionMode || next === currentMode || modeBusy) return;
    setModeBusy(true);
    try {
      await onChangePermissionMode(activeBot.id, next);
    } finally {
      setModeBusy(false);
    }
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
      <ChatHeader
        inDesktopSidebar={inDesktopSidebar}
        bots={bots}
        activeBot={activeBot}
        takeovers={takeovers}
        activeDeskTarget={activeDeskTarget}
        deskOpen={deskOpen}
        hasUnreadLatestStage={hasUnreadLatestStage}
        lang={lang}
        onOpenMenu={onOpenMenu}
        onToggleDesktop={onToggleDesktop}
        onOpenStage={onOpenStage}
        onOpenDesktopTarget={onOpenDesktopTarget}
        onSwitchBackLocal={onSwitchBackLocal}
        onResolveTakeover={onResolveTakeover}
        onOpenDetail={onOpenDetail}
        onSelectBot={onSelectBot}
        onCloseDesktopChat={onCloseDesktopChat}
      />

      <div className="main-split">
        <div ref={chatColRef} className="chat-col">
          {activeBot && onInspectTask && (
            <TaskStrip
              tasks={tasks}
              taskToolMap={taskToolMap}
              nodes={nodes}
              lang={lang}
              onInspectTask={onInspectTask}
            />
          )}
          <ChatMessageList
            scrollRef={scrollRef}
            inDesktopSidebar={inDesktopSidebar}
            bots={bots}
            activeBot={activeBot}
            messages={messages}
            timelineItems={timelineItems}
            seenIds={seenIds}
            approvals={approvals}
            questions={questions}
            lang={lang}
            onOpenCreate={onOpenCreate}
            onOpenCollab={onOpenCollab}
            onOpenStageRef={onOpenStageRef}
            onPreviewImage={setPreviewImg}
            onReplyApproval={onReplyApproval}
            onReplyQuestion={onReplyQuestion}
          />

          <ChatComposer
            composerWrapRef={composerWrapRef}
            fileInputRef={fileInputRef}
            inputRef={inputRef}
            acRef={acRef}
            inDesktopSidebar={inDesktopSidebar}
            activeBot={activeBot}
            lang={lang}
            draft={draft}
            dragging={dragging}
            attachments={attachments}
            canSend={canSend}
            stopMode={stopMode}
            availableModes={availableModes}
            currentMode={currentMode}
            modeBusy={modeBusy}
            acContext={acContext}
            acOpen={acOpen}
            acIndex={acIndex}
            acListLen={acListLen}
            canChangeMode={Boolean(onChangePermissionMode)}
            filteredCommands={filteredCommands}
            mentionItems={mentionItems}
            onSetDraft={setDraft}
            onSetCursorPos={setCursorPos}
            onSetDragging={setDragging}
            onSetAcDismissed={setAcDismissed}
            onSetAcIndex={setAcIndex}
            onUploadFiles={handleUploadFiles}
            onRemoveAttachment={removeAttachment}
            onSend={handleSend}
            onSwitchMode={switchMode}
            onApplySlashCommand={applySlashCommand}
            onApplyMention={applyMention}
          />
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
