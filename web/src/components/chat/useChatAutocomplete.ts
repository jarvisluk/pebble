import { useEffect, useMemo, useRef, useState } from 'react';
import type React from 'react';
import type { MentionCandidate, SlashCommandItem } from '../../types';
import { fetchBotCommands, fetchBotMentions } from '../../lib/api';

export interface AcContext {
  kind: 'slash' | 'mention';
  query: string;
  start: number;
  end: number;
}

export function useChatAutocomplete({
  botId,
  draft,
  cursorPos,
  inputRef,
  setDraft,
  setCursorPos,
}: {
  botId?: string;
  draft: string;
  cursorPos: number;
  inputRef: React.RefObject<HTMLTextAreaElement | null>;
  setDraft: (next: string) => void;
  setCursorPos: (pos: number) => void;
}) {
  const [commandsMap, setCommandsMap] = useState<Record<string, SlashCommandItem[]>>({});
  const [mentionItems, setMentionItems] = useState<MentionCandidate[]>([]);
  const [acIndex, setAcIndex] = useState(0);
  const [acDismissed, setAcDismissed] = useState(false);
  const acRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setMentionItems([]);
    setAcDismissed(false);
  }, [botId]);

  useEffect(() => {
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
  }, [botId]);

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
    if (!botId) return [];
    return commandsMap[botId] || [];
  }, [botId, commandsMap]);

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
    if (!acContext || acContext.kind !== 'mention' || !botId) {
      setMentionItems([]);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(() => {
      void fetchBotMentions(botId, acContext.query)
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
  }, [acContext, botId]);

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

  return {
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
  };
}
