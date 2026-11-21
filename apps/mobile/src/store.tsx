import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { AppState } from 'react-native';

import { startTunnel, stopTunnel } from '../modules/bothub-tunnel';
import { apiRequest, connectSSE, normalizeAddress, UnauthorizedError } from './api';
import { tStatic, type ResolvedLang } from './i18n';
import { loadSavedServers, saveServers } from './storage';
import type {
  BotDetail,
  BotStatus,
  ChatMessage,
  DesktopFrame,
  DesktopTarget,
  MagpieMemoryList,
  NodeInfo,
  ScreenState,
  ServerProfile,
  SettingsData,
  TailcatStatus,
  TakeoverRequest,
} from './types';

export type ConnState = 'idle' | 'connecting' | 'connected' | 'unauthorized' | 'error';

export function resolveDefaultModel(g: string, h: string): string {
  if (!g) return '';
  if (g.startsWith('magpie/')) return h === 'claude' ? g.slice(7) : g;
  if (!g.includes('/')) return g;
  return h === '' || h === 'opencode' || h === 'pi' ? g : '';
}

export function formatShortTime(iso?: string, lang: ResolvedLang = 'zh-CN'): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const today = new Date();
  const y = new Date(today);
  y.setDate(today.getDate() - 1);
  if (d.toDateString() === today.toDateString()) {
    return d.toLocaleTimeString(lang === 'en' ? 'en-US' : 'zh-CN', { hour: '2-digit', minute: '2-digit' });
  }
  if (d.toDateString() === y.toDateString()) return lang === 'en' ? 'Yesterday' : '昨天';
  return `${d.getMonth() + 1}/${d.getDate()}`;
}

export function formatStamp(iso?: string, lang: ResolvedLang = 'zh-CN'): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const t = d.toLocaleTimeString(lang === 'en' ? 'en-US' : 'zh-CN', { hour: '2-digit', minute: '2-digit' });
  const today = new Date();
  const y = new Date(today);
  y.setDate(today.getDate() - 1);
  if (d.toDateString() === today.toDateString()) return `${lang === 'en' ? 'Today' : '今天'} ${t}`;
  if (d.toDateString() === y.toDateString()) return `${lang === 'en' ? 'Yesterday' : '昨天'} ${t}`;
  return lang === 'en' ? `${d.getMonth() + 1}/${d.getDate()} ${t}` : `${d.getMonth() + 1}月${d.getDate()}日 ${t}`;
}

export function plainPreview(s: string, codeTag = '[代码]'): string {
  return String(s || '')
    .replace(/```[\s\S]*?```/g, codeTag)
    .replace(/[#>*`_\[\]]/g, '')
    .replace(/\s+/g, ' ')
    .trim();
}

interface HubContextValue {
  ready: boolean;
  servers: ServerProfile[];
  activeServer: ServerProfile | null;
  connState: ConnState;
  connError: string;
  baseURL: string;
  authToken: string;
  isTailcat: boolean;
  online: boolean;
  bots: BotStatus[];
  msgs: Record<string, ChatMessage[]>;
  last: Record<string, ChatMessage>;
  unread: Record<string, number>;
  tools: Record<string, string>;
  settings: SettingsData | null;
  models: Record<string, string[]>;
  nodes: NodeInfo[];
  tailcat: TailcatStatus | null;
  takeovers: TakeoverRequest[];
  desktopTargets: DesktopTarget[];
  setActiveChatBot: (botId: string | null) => void;
  addAndConnectServer: (input: { label?: string; address: string; token?: string }) => Promise<boolean>;
  selectServer: (id: string) => Promise<void>;
  updateServerToken: (id: string, token: string) => Promise<void>;
  removeServer: (id: string) => Promise<void>;
  reconnect: () => Promise<void>;
  refreshBots: () => Promise<void>;
  loadMessages: (botId: string) => Promise<void>;
  sendMessage: (botId: string, text: string) => Promise<void>;
  interruptBot: (botId: string) => Promise<boolean>;
  resetBot: (botId: string) => Promise<void>;
  getBotDetail: (botId: string) => Promise<BotDetail>;
  createBot: (body: { id: string; name: string; description?: string; harness?: string; model?: string; instructions?: string; privateSessions?: boolean }) => Promise<BotStatus>;
  updateBot: (botId: string, body: { name?: string; description?: string; instructions?: string; privateSessions?: boolean }) => Promise<BotStatus>;
  deleteBot: (botId: string) => Promise<void>;
  fetchMemories: (opts: { scope?: 'global' | 'project' | 'all'; botId?: string; query?: string; limit?: number }) => Promise<MagpieMemoryList>;
  saveMemory: (body: { scope?: 'global' | 'project'; botId?: string; kind?: string; text: string; tags?: string[] }) => Promise<void>;
  deleteMemory: (id: string) => Promise<void>;
  loadSettings: () => Promise<SettingsData>;
  loadModels: (harness: string) => Promise<string[]>;
  saveSettings: (opts: {
    settingsChanged: boolean;
    defaultModel: string;
    defaultModels: Record<string, string>;
    sharedUserMd: string;
    botUpdates: { id: string; harness: string; model: string }[];
  }) => Promise<void>;
  connectMagpie: (harnessId: string, model: string) => Promise<SettingsData>;
  disconnectMagpie: (harnessId: string) => Promise<SettingsData>;
  magpieProviderAction: (body: {
    action: 'add' | 'on' | 'off' | 'rm' | 'refresh' | 'test';
    id?: string;
    preset?: string;
    name?: string;
    url?: string;
    protocol?: string;
    key?: string;
  }) => Promise<{ ok: boolean; output?: string }>;
  refreshNodes: () => Promise<void>;
  refreshTailcat: () => Promise<TailcatStatus>;
  refreshDesktopTargets: () => Promise<DesktopTarget[]>;
  fetchDesktopState: (target: string) => Promise<{ screen: ScreenState; takeovers: TakeoverRequest[] }>;
  fetchDesktopFrame: (target: string, windowId?: string) => Promise<DesktopFrame>;
  sendDesktopInput: (body: Record<string, unknown>) => Promise<void>;
  resolveTakeover: (id: string, note?: string) => Promise<void>;
}

const HubContext = createContext<HubContextValue | null>(null);

export function HubProvider({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false);
  const [servers, setServers] = useState<ServerProfile[]>([]);
  const [activeServerId, setActiveServerId] = useState<string | null>(null);
  const [connState, setConnState] = useState<ConnState>('idle');
  const [connError, setConnError] = useState('');
  const [baseURL, setBaseURL] = useState('');
  const [authToken, setAuthToken] = useState('');
  const [isTailcat, setIsTailcat] = useState(false);
  const [online, setOnline] = useState(false);

  const [bots, setBots] = useState<BotStatus[]>([]);
  const [msgs, setMsgs] = useState<Record<string, ChatMessage[]>>({});
  const [last, setLast] = useState<Record<string, ChatMessage>>({});
  const [unread, setUnread] = useState<Record<string, number>>({});
  const [tools, setTools] = useState<Record<string, string>>({});
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [models, setModels] = useState<Record<string, string[]>>({});
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [tailcat, setTailcat] = useState<TailcatStatus | null>(null);
  const [takeovers, setTakeovers] = useState<TakeoverRequest[]>([]);
  const [desktopTargets, setDesktopTargets] = useState<DesktopTarget[]>([]);

  const activeChatRef = useRef<string | null>(null);
  const sseCloseRef = useRef<(() => void) | null>(null);
  const connSeqRef = useRef(0);
  const baseURLRef = useRef('');
  const authTokenRef = useRef('');
  const serversRef = useRef<ServerProfile[]>([]);
  const activeServerIdRef = useRef<string | null>(null);

  baseURLRef.current = baseURL;
  authTokenRef.current = authToken;
  serversRef.current = servers;
  activeServerIdRef.current = activeServerId;

  const setActiveChatBot = useCallback((botId: string | null) => {
    activeChatRef.current = botId;
    if (botId) {
      setUnread((prev) => {
        if (!prev[botId]) return prev;
        const next = { ...prev };
        delete next[botId];
        return next;
      });
    }
  }, []);

  const removeBotState = useCallback((botId: string) => {
    setBots((prev) => prev.filter((b) => b.id !== botId));
    setMsgs((prev) => {
      if (!(botId in prev)) return prev;
      const copy = { ...prev };
      delete copy[botId];
      return copy;
    });
    setLast((prev) => {
      if (!(botId in prev)) return prev;
      const copy = { ...prev };
      delete copy[botId];
      return copy;
    });
    setUnread((prev) => {
      if (!(botId in prev)) return prev;
      const copy = { ...prev };
      delete copy[botId];
      return copy;
    });
    setTools((prev) => {
      if (!(botId in prev)) return prev;
      const copy = { ...prev };
      delete copy[botId];
      return copy;
    });
  }, []);

  const pushIncomingMessage = useCallback((botId: string, msg: ChatMessage) => {
    setMsgs((prev) => {
      const list = prev[botId] || [];
      if (msg.id && list.some((x) => x.id === msg.id)) return prev;
      return { ...prev, [botId]: [...list, msg] };
    });
    if (msg.role !== 'system') {
      setLast((prev) => ({ ...prev, [botId]: msg }));
      if (activeChatRef.current !== botId) {
        setUnread((prev) => ({ ...prev, [botId]: (prev[botId] || 0) + 1 }));
      }
    }
  }, []);

  const startStream = useCallback(
    (url: string, tok: string) => {
      sseCloseRef.current?.();
      sseCloseRef.current = connectSSE(url, tok, {
        onStatus: (ok) => setOnline(ok),
        onUnauthorized: () => {
          setConnState('unauthorized');
          setConnError(tStatic('needsToken'));
        },
        onEvent: (type, payload: any) => {
          if (type === 'message' && payload?.botId) {
            pushIncomingMessage(payload.botId, payload as ChatMessage);
          } else if (type === 'bot_status' && payload?.id) {
            const st = payload as BotStatus;
            setBots((prev) => {
              const i = prev.findIndex((b) => b.id === st.id);
              if (i >= 0) {
                const copy = [...prev];
                copy[i] = st;
                return copy;
              }
              return [...prev, st];
            });
            if (!st.busy) {
              setTools((prev) => {
                if (!prev[st.id]) return prev;
                const copy = { ...prev };
                delete copy[st.id];
                return copy;
              });
            }
          } else if (type === 'tool_use' && payload?.botId) {
            setTools((prev) => ({ ...prev, [payload.botId]: String(payload.tool || '') }));
          } else if (type === 'bot_created' && payload?.id) {
            const st = payload as BotStatus;
            setBots((prev) => (prev.some((b) => b.id === st.id) ? prev : [...prev, st]));
          } else if (type === 'bot_deleted') {
            const bid = payload?.id || payload?.botId;
            if (bid) removeBotState(String(bid));
          } else if (type === 'nodes_changed' && Array.isArray(payload?.nodes)) {
            setNodes(payload.nodes);
          } else if (type === 'takeover' && Array.isArray(payload?.items)) {
            setTakeovers(payload.items);
          }
        },
      });
    },
    [pushIncomingMessage, removeBotState]
  );

  const connectProfile = useCallback(
    async (profile: ServerProfile, allServers: ServerProfile[]) => {
      const seq = ++connSeqRef.current;
      sseCloseRef.current?.();
      sseCloseRef.current = null;
      setOnline(false);
      setConnState('connecting');
      setConnError('');

      const norm = normalizeAddress(profile.address);
      let resolvedToken = profile.token || norm.token;
      let resolvedURL = norm.cleanAddress;

      try {
        if (norm.isTailcat) {
          const tRes = await startTunnel(norm.cleanAddress, resolvedToken);
          if (seq !== connSeqRef.current) return;
          resolvedURL = tRes.url;
          if (tRes.token && !resolvedToken) {
            resolvedToken = tRes.token;
          }
        } else {
          await stopTunnel().catch(() => {});
        }

        if (seq !== connSeqRef.current) return;

        // Persist extracted token if new.
        if (resolvedToken !== profile.token || norm.cleanAddress !== profile.address) {
          const updated = allServers.map((s) =>
            s.id === profile.id ? { ...s, address: norm.cleanAddress, token: resolvedToken } : s
          );
          setServers(updated);
          await saveServers(updated, profile.id);
        }

        setIsTailcat(norm.isTailcat);
        setBaseURL(resolvedURL);
        setAuthToken(resolvedToken);

        const data = await apiRequest<{ bots: BotStatus[] }>(resolvedURL, resolvedToken, '/api/bots');
        if (seq !== connSeqRef.current) return;

        const botList = data.bots || [];
        setBots(botList);
        setMsgs({});
        setLast({});
        setUnread({});
        setTools({});
        setModels({});
        setConnState('connected');
        setConnError('');

        // Fetch previews & settings & nodes in parallel.
        Promise.all(
          botList.map(async (b) => {
            try {
              const res = await apiRequest<{ messages: ChatMessage[] }>(
                resolvedURL,
                resolvedToken,
                `/api/bots/${encodeURIComponent(b.id)}/messages?limit=1`
              );
              const m = res.messages?.[res.messages.length - 1];
              if (m && seq === connSeqRef.current) {
                setLast((prev) => ({ ...prev, [b.id]: m }));
              }
            } catch {
              // ignore preview errors
            }
          })
        ).catch(() => {});

        apiRequest<SettingsData>(resolvedURL, resolvedToken, '/api/settings')
          .then((s) => seq === connSeqRef.current && setSettings(s))
          .catch(() => {});
        apiRequest<{ nodes: NodeInfo[] }>(resolvedURL, resolvedToken, '/api/nodes')
          .then((n) => seq === connSeqRef.current && setNodes(n.nodes || []))
          .catch(() => {});
        apiRequest<TailcatStatus>(resolvedURL, resolvedToken, '/api/tailcat')
          .then((t) => seq === connSeqRef.current && setTailcat(t))
          .catch(() => {});
        apiRequest<{ items: TakeoverRequest[] }>(resolvedURL, resolvedToken, '/api/desktop/takeovers')
          .then((t) => seq === connSeqRef.current && setTakeovers(t.items || []))
          .catch(() => {});

        startStream(resolvedURL, resolvedToken);
      } catch (err: any) {
        if (seq !== connSeqRef.current) return;
        setBaseURL(resolvedURL);
        setAuthToken(resolvedToken);
        setIsTailcat(norm.isTailcat);
        if (err instanceof UnauthorizedError) {
          setConnState('unauthorized');
          setConnError(err.message);
        } else {
          setConnState('error');
          setConnError(err?.message || tStatic('connFailed'));
        }
      }
    },
    [startStream]
  );

  // Initial load from secure store
  useEffect(() => {
    let mounted = true;
    loadSavedServers().then(({ servers: saved, activeId }) => {
      if (!mounted) return;
      setServers(saved);
      const target = saved.find((s) => s.id === activeId) || saved[0] || null;
      setActiveServerId(target?.id || null);
      setReady(true);
      if (target) {
        connectProfile(target, saved);
      }
    });
    return () => {
      mounted = false;
      sseCloseRef.current?.();
    };
  }, [connectProfile]);

  // Resume SSE & refresh when returning to foreground
  useEffect(() => {
    const sub = AppState.addEventListener('change', (nextState) => {
      if (nextState !== 'active') return;
      const curId = activeServerIdRef.current;
      const curServer = serversRef.current.find((s) => s.id === curId);
      const url = baseURLRef.current;
      const tok = authTokenRef.current;
      if (!curServer || !url) return;

      apiRequest<{ bots: BotStatus[] }>(url, tok, '/api/bots')
        .then((d) => {
          setBots(d.bots || []);
          startStream(url, tok);
          if (activeChatRef.current) {
            const bid = activeChatRef.current;
            apiRequest<{ messages: ChatMessage[] }>(url, tok, `/api/bots/${encodeURIComponent(bid)}/messages?limit=200`)
              .then((m) => setMsgs((prev) => ({ ...prev, [bid]: m.messages || [] })))
              .catch(() => {});
          }
        })
        .catch(() => {
          connectProfile(curServer, serversRef.current);
        });
    });
    return () => sub.remove();
  }, [connectProfile, startStream]);

  const addAndConnectServer = useCallback(
    async (input: { label?: string; address: string; token?: string }) => {
      const norm = normalizeAddress(input.address);
      if (!norm.cleanAddress) throw new Error(tStatic('enterValidAddr'));
      const tok = (input.token || norm.token || '').trim();
      const existing = serversRef.current.find((s) => s.address === norm.cleanAddress);
      const label = (input.label || '').trim() || (norm.isTailcat ? tStatic('remoteHostDefault') : norm.cleanAddress.replace(/^https?:\/\//, ''));
      const target: ServerProfile = existing
        ? { ...existing, label: (input.label || '').trim() || existing.label, token: tok || existing.token }
        : { id: `${Date.now()}`, label, address: norm.cleanAddress, token: tok };

      const nextList = existing
        ? serversRef.current.map((s) => (s.id === existing.id ? target : s))
        : [target, ...serversRef.current];

      setServers(nextList);
      setActiveServerId(target.id);
      await saveServers(nextList, target.id);
      await connectProfile(target, nextList);
      return true;
    },
    [connectProfile]
  );

  const selectServer = useCallback(
    async (id: string) => {
      const target = serversRef.current.find((s) => s.id === id);
      if (!target) return;
      setActiveServerId(id);
      await saveServers(serversRef.current, id);
      await connectProfile(target, serversRef.current);
    },
    [connectProfile]
  );

  const updateServerToken = useCallback(
    async (id: string, token: string) => {
      const nextList = serversRef.current.map((s) => (s.id === id ? { ...s, token: token.trim() } : s));
      setServers(nextList);
      await saveServers(nextList, activeServerIdRef.current);
      const target = nextList.find((s) => s.id === id);
      if (target && id === activeServerIdRef.current) {
        await connectProfile(target, nextList);
      }
    },
    [connectProfile]
  );

  const removeServer = useCallback(
    async (id: string) => {
      const nextList = serversRef.current.filter((s) => s.id !== id);
      const removingActive = activeServerIdRef.current === id;
      const nextActive = removingActive ? nextList[0]?.id || null : activeServerIdRef.current;
      setServers(nextList);
      setActiveServerId(nextActive);
      await saveServers(nextList, nextActive);
      if (removingActive) {
        if (nextActive) {
          const target = nextList.find((s) => s.id === nextActive)!;
          await connectProfile(target, nextList);
        } else {
          connSeqRef.current++;
          sseCloseRef.current?.();
          sseCloseRef.current = null;
          await stopTunnel().catch(() => {});
          setBaseURL('');
          setAuthToken('');
          setBots([]);
          setMsgs({});
          setLast({});
          setConnState('idle');
          setConnError('');
        }
      }
    },
    [connectProfile]
  );

  const reconnect = useCallback(async () => {
    const target = serversRef.current.find((s) => s.id === activeServerIdRef.current);
    if (target) await connectProfile(target, serversRef.current);
  }, [connectProfile]);

  const refreshBots = useCallback(async () => {
    if (!baseURLRef.current) return;
    const d = await apiRequest<{ bots: BotStatus[] }>(baseURLRef.current, authTokenRef.current, '/api/bots');
    setBots(d.bots || []);
  }, []);

  const loadMessages = useCallback(async (botId: string) => {
    if (!baseURLRef.current) return;
    const d = await apiRequest<{ messages: ChatMessage[] }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/bots/${encodeURIComponent(botId)}/messages?limit=200`
    );
    const list = d.messages || [];
    setMsgs((prev) => ({ ...prev, [botId]: list }));
    const m = list[list.length - 1];
    if (m) setLast((prev) => ({ ...prev, [botId]: m }));
  }, []);

  const sendMessage = useCallback(
    async (botId: string, text: string) => {
      const data = await apiRequest<{ text?: string }>(
        baseURLRef.current,
        authTokenRef.current,
        `/api/bots/${encodeURIComponent(botId)}/message`,
        { method: 'POST', body: { message: text, source: 'mobile' } }
      );
      if (/^\/(bots|status|reset)$/.test(text) && data.text) {
        pushIncomingMessage(botId, {
          id: `cmd_${Date.now()}`,
          botId,
          role: 'system',
          from: 'command',
          text: data.text,
          createdAt: new Date().toISOString(),
        });
      }
      if (text === '/reset') {
        await refreshBots();
      }
    },
    [pushIncomingMessage, refreshBots]
  );

  const interruptBot = useCallback(async (botId: string) => {
    const r = await apiRequest<{ interrupted: boolean }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/bots/${encodeURIComponent(botId)}/interrupt`,
      { method: 'POST' }
    );
    return !!r.interrupted;
  }, []);

  const resetBot = useCallback(
    async (botId: string) => {
      await apiRequest(baseURLRef.current, authTokenRef.current, `/api/bots/${encodeURIComponent(botId)}/reset`, {
        method: 'POST',
      });
      await refreshBots();
    },
    [refreshBots]
  );

  const getBotDetail = useCallback(async (botId: string) => {
    return apiRequest<BotDetail>(baseURLRef.current, authTokenRef.current, `/api/bots/${encodeURIComponent(botId)}`);
  }, []);

  const createBot = useCallback(
    async (body: { id: string; name: string; description?: string; harness?: string; model?: string; instructions?: string; privateSessions?: boolean }) => {
      const st = await apiRequest<BotStatus>(baseURLRef.current, authTokenRef.current, '/api/bots', {
        method: 'POST',
        body,
      });
      setBots((prev) => (prev.some((b) => b.id === st.id) ? prev : [...prev, st]));
      return st;
    },
    []
  );

  const updateBot = useCallback(
    async (botId: string, body: { name?: string; description?: string; instructions?: string; privateSessions?: boolean }) => {
      const st = await apiRequest<BotStatus>(
        baseURLRef.current,
        authTokenRef.current,
        `/api/bots/${encodeURIComponent(botId)}`,
        { method: 'PATCH', body }
      );
      setBots((prev) => prev.map((b) => (b.id === st.id ? st : b)));
      return st;
    },
    []
  );

  const deleteBot = useCallback(
    async (botId: string) => {
      await apiRequest(baseURLRef.current, authTokenRef.current, `/api/bots/${encodeURIComponent(botId)}`, {
        method: 'DELETE',
      });
      removeBotState(botId);
    },
    [removeBotState]
  );

  const fetchMemories = useCallback(
    async (opts: { scope?: 'global' | 'project' | 'all'; botId?: string; query?: string; limit?: number }) => {
      const q = new URLSearchParams();
      if (opts.scope) q.set('scope', opts.scope);
      if (opts.botId) q.set('botId', opts.botId);
      if (opts.query?.trim()) q.set('query', opts.query.trim());
      if (opts.limit) q.set('limit', String(opts.limit));
      return apiRequest<MagpieMemoryList>(
        baseURLRef.current,
        authTokenRef.current,
        `/api/memory?${q.toString()}`
      );
    },
    []
  );

  const saveMemory = useCallback(
    async (body: { scope?: 'global' | 'project'; botId?: string; kind?: string; text: string; tags?: string[] }) => {
      await apiRequest(baseURLRef.current, authTokenRef.current, '/api/memory', {
        method: 'POST',
        body,
      });
    },
    []
  );

  const deleteMemory = useCallback(async (id: string) => {
    await apiRequest(baseURLRef.current, authTokenRef.current, `/api/memory/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    });
  }, []);

  const loadSettings = useCallback(async () => {
    const s = await apiRequest<SettingsData>(baseURLRef.current, authTokenRef.current, '/api/settings');
    setSettings(s);
    return s;
  }, []);

  const loadModels = useCallback(async (harness: string) => {
    const d = await apiRequest<{ models: string[] }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/models?harness=${encodeURIComponent(harness)}`
    );
    const list = d.models || [];
    setModels((prev) => ({ ...prev, [harness]: list }));
    return list;
  }, []);

  const saveSettings = useCallback(
    async (opts: {
      settingsChanged: boolean;
      defaultModel: string;
      defaultModels: Record<string, string>;
      sharedUserMd: string;
      botUpdates: { id: string; harness: string; model: string }[];
    }) => {
      if (opts.settingsChanged) {
        const s = await apiRequest<SettingsData>(baseURLRef.current, authTokenRef.current, '/api/settings', {
          method: 'PUT',
          body: {
            defaultModel: opts.defaultModel,
            defaultModels: opts.defaultModels,
            sharedUserMd: opts.sharedUserMd,
          },
        });
        setSettings(s);
      }
      for (const u of opts.botUpdates) {
        const st = await apiRequest<BotStatus>(
          baseURLRef.current,
          authTokenRef.current,
          `/api/bots/${encodeURIComponent(u.id)}`,
          { method: 'PATCH', body: { harness: u.harness, model: u.model } }
        );
        setBots((prev) => prev.map((b) => (b.id === st.id ? st : b)));
      }
      await loadSettings();
      await refreshBots();
    },
    [loadSettings, refreshBots]
  );

  const connectMagpie = useCallback(async (harnessId: string, model: string) => {
    const s = await apiRequest<SettingsData>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/harnesses/${encodeURIComponent(harnessId)}/magpie`,
      { method: 'POST', body: { action: 'connect', model } }
    );
    setSettings(s);
    setModels({});
    return s;
  }, []);

  const disconnectMagpie = useCallback(async (harnessId: string) => {
    const s = await apiRequest<SettingsData>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/harnesses/${encodeURIComponent(harnessId)}/magpie`,
      { method: 'POST', body: { action: 'disconnect' } }
    );
    setSettings(s);
    setModels({});
    return s;
  }, []);

  const magpieProviderAction = useCallback(
    async (body: {
      action: 'add' | 'on' | 'off' | 'rm' | 'refresh' | 'test';
      id?: string;
      preset?: string;
      name?: string;
      url?: string;
      protocol?: string;
      key?: string;
    }) => {
      const res = await apiRequest<{ ok: boolean; output?: string }>(
        baseURLRef.current,
        authTokenRef.current,
        '/api/magpie/providers',
        { method: 'POST', body }
      );
      setModels({});
      await loadSettings().catch(() => {});
      return res;
    },
    [loadSettings]
  );

  const refreshNodes = useCallback(async () => {
    if (!baseURLRef.current) return;
    const d = await apiRequest<{ nodes: NodeInfo[] }>(baseURLRef.current, authTokenRef.current, '/api/nodes');
    setNodes(d.nodes || []);
  }, []);

  const refreshTailcat = useCallback(async () => {
    const t = await apiRequest<TailcatStatus>(baseURLRef.current, authTokenRef.current, '/api/tailcat');
    setTailcat(t);
    return t;
  }, []);

  const refreshDesktopTargets = useCallback(async () => {
    const d = await apiRequest<{ targets: DesktopTarget[]; takeovers: TakeoverRequest[] }>(
      baseURLRef.current,
      authTokenRef.current,
      '/api/desktop/targets'
    );
    const list = d.targets || [];
    setDesktopTargets(list);
    setTakeovers(d.takeovers || []);
    return list;
  }, []);

  const fetchDesktopState = useCallback(async (target: string) => {
    const d = await apiRequest<{ screen: ScreenState; takeovers: TakeoverRequest[] }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/desktop/state?target=${encodeURIComponent(target)}`
    );
    setTakeovers(d.takeovers || []);
    return d;
  }, []);

  const fetchDesktopFrame = useCallback(async (target: string, windowId = '') => {
    return apiRequest<DesktopFrame>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/desktop/frame?target=${encodeURIComponent(target)}&windowId=${encodeURIComponent(windowId)}&maxDim=1280&quality=72`
    );
  }, []);

  const sendDesktopInput = useCallback(async (body: Record<string, unknown>) => {
    await apiRequest(baseURLRef.current, authTokenRef.current, '/api/desktop/input', {
      method: 'POST',
      body,
    });
  }, []);

  const resolveTakeover = useCallback(async (id: string, note?: string) => {
    const res = await apiRequest<{ ok: boolean; items: TakeoverRequest[] }>(
      baseURLRef.current,
      authTokenRef.current,
      '/api/desktop/takeover/resolve',
      { method: 'POST', body: { id, note: note || tStatic('takeoverResolvedNote') } }
    );
    setTakeovers(res.items || []);
  }, []);

  const activeServer = servers.find((s) => s.id === activeServerId) || null;

  return (
    <HubContext.Provider
      value={{
        ready,
        servers,
        activeServer,
        connState,
        connError,
        baseURL,
        authToken,
        isTailcat,
        online,
        bots,
        msgs,
        last,
        unread,
        tools,
        settings,
        models,
        nodes,
        tailcat,
        takeovers,
        desktopTargets,
        setActiveChatBot,
        addAndConnectServer,
        selectServer,
        updateServerToken,
        removeServer,
        reconnect,
        refreshBots,
        loadMessages,
        sendMessage,
        interruptBot,
        resetBot,
        getBotDetail,
        createBot,
        updateBot,
        deleteBot,
        fetchMemories,
        saveMemory,
        deleteMemory,
        loadSettings,
        loadModels,
        saveSettings,
        connectMagpie,
        disconnectMagpie,
        magpieProviderAction,
        refreshNodes,
        refreshTailcat,
        refreshDesktopTargets,
        fetchDesktopState,
        fetchDesktopFrame,
        sendDesktopInput,
        resolveTakeover,
      }}
    >
      {children}
    </HubContext.Provider>
  );
}

export function useHub(): HubContextValue {
  const ctx = useContext(HubContext);
  if (!ctx) throw new Error('useHub must be used within HubProvider');
  return ctx;
}
