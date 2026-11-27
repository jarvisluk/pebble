import React from 'react';
import type {
  BotStatus,
  MentionCandidate,
  PendingAttachment,
  PermissionMode,
  ResolvedLang,
  SlashCommandItem,
} from '../../types';
import { translate } from '../../lib/i18n';
import type { AcContext } from './useChatAutocomplete';

interface ChatComposerProps {
  composerWrapRef: React.RefObject<HTMLDivElement | null>;
  fileInputRef: React.RefObject<HTMLInputElement | null>;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  acRef: React.RefObject<HTMLDivElement | null>;
  inDesktopSidebar: boolean;
  activeBot?: BotStatus;
  lang: ResolvedLang;
  draft: string;
  dragging: boolean;
  attachments: PendingAttachment[];
  canSend: boolean;
  stopMode: boolean;
  availableModes: PermissionMode[];
  currentMode: PermissionMode;
  modeBusy: boolean;
  acContext: AcContext | null;
  acOpen: boolean;
  acIndex: number;
  acListLen: number;
  canChangeMode: boolean;
  filteredCommands: SlashCommandItem[];
  mentionItems: MentionCandidate[];
  onSetDraft: (next: string) => void;
  onSetCursorPos: (pos: number) => void;
  onSetDragging: (v: boolean) => void;
  onSetAcDismissed: (v: boolean) => void;
  onSetAcIndex: React.Dispatch<React.SetStateAction<number>>;
  onUploadFiles: (files: FileList | File[]) => void;
  onRemoveAttachment: (id: string) => void;
  onSend: () => Promise<void>;
  onSwitchMode: (next: PermissionMode) => Promise<void>;
  onApplySlashCommand: (cmd: SlashCommandItem) => void;
  onApplyMention: (item: MentionCandidate) => void;
  topDock?: React.ReactNode;
}

export const ChatComposer: React.FC<ChatComposerProps> = ({
  composerWrapRef,
  fileInputRef,
  inputRef,
  acRef,
  inDesktopSidebar,
  activeBot,
  lang,
  draft,
  dragging,
  attachments,
  canSend,
  stopMode,
  availableModes,
  currentMode,
  modeBusy,
  acContext,
  acOpen,
  acIndex,
  acListLen,
  canChangeMode,
  filteredCommands,
  mentionItems,
  onSetDraft,
  onSetCursorPos,
  onSetDragging,
  onSetAcDismissed,
  onSetAcIndex,
  onUploadFiles,
  onRemoveAttachment,
  onSend,
  onSwitchMode,
  onApplySlashCommand,
  onApplyMention,
  topDock,
}) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);

  return (
    <div ref={composerWrapRef} className="composer-wrap">
      {topDock}
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
                onClick={() => onRemoveAttachment(att.id)}
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
            onSetDragging(true);
          }
        }}
        onDragLeave={() => onSetDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          onSetDragging(false);
          if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
            onUploadFiles(e.dataTransfer.files);
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
              onUploadFiles(e.target.files);
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
            onSetAcDismissed(false);
            onSetDraft(e.target.value);
            onSetCursorPos(e.target.selectionStart ?? e.target.value.length);
          }}
          onClick={(e) => {
            onSetCursorPos((e.target as HTMLTextAreaElement).selectionStart ?? draft.length);
          }}
          onKeyUp={(e) => {
            if (!['ArrowUp', 'ArrowDown', 'Enter', 'Tab', 'Escape'].includes(e.key)) {
              onSetCursorPos((e.target as HTMLTextAreaElement).selectionStart ?? draft.length);
            }
          }}
          onPaste={(e) => {
            const files = Array.from(e.clipboardData?.files || []);
            if (files.length > 0) {
              e.preventDefault();
              onUploadFiles(files);
            }
          }}
          onKeyDown={(e) => {
            if (acOpen && !e.nativeEvent.isComposing) {
              if (e.key === 'ArrowDown') {
                e.preventDefault();
                onSetAcIndex((i) => (i + 1) % acListLen);
                return;
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault();
                onSetAcIndex((i) => (i - 1 + acListLen) % acListLen);
                return;
              }
              if (e.key === 'Escape') {
                e.preventDefault();
                onSetAcDismissed(true);
                return;
              }
              if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
                e.preventDefault();
                if (acContext?.kind === 'slash' && filteredCommands[acIndex]) {
                  onApplySlashCommand(filteredCommands[acIndex]);
                } else if (acContext?.kind === 'mention' && mentionItems[acIndex]) {
                  onApplyMention(mentionItems[acIndex]);
                }
                return;
              }
            }
            if (e.key === 'Tab' && e.shiftKey && !draft && !e.nativeEvent.isComposing && availableModes.length > 1) {
              e.preventDefault();
              const curIdx = availableModes.indexOf(currentMode);
              const nextMode = availableModes[(curIdx + 1) % availableModes.length];
              void onSwitchMode(nextMode);
              return;
            }
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              void onSend();
            }
          }}
        />
        {activeBot && availableModes.length > 1 && canChangeMode && (
          <select
            id={inDesktopSidebar ? undefined : 'composerModeSel'}
            className="composer-mode-sel"
            aria-label={t('permMode')}
            title={`${t('permMode')} (Shift+Tab)`}
            data-mode={currentMode}
            disabled={modeBusy}
            value={currentMode}
            onChange={(e) => void onSwitchMode(e.target.value as PermissionMode)}
          >
            {availableModes.map((m) => (
              <option key={m} value={m}>
                {t(`permMode_${m}`)}
              </option>
            ))}
          </select>
        )}
        <button
          id="sendBtn"
          type="button"
          className={`send${stopMode ? ' stop' : ''}`}
          disabled={!canSend}
          aria-label={stopMode ? t('stop') : t('send')}
          onClick={() => void onSend()}
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
                      onMouseEnter={() => onSetAcIndex(idx)}
                      onClick={() => onApplySlashCommand(cmd)}
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
                      onMouseEnter={() => onSetAcIndex(idx)}
                      onClick={() => onApplyMention(item)}
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
  );
};
