import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { AppState, Linking } from 'react-native';

import {
  isNativeTunnelAvailable,
  startTunnel,
  stopTunnel,
  tailscaleLogout,
  tailscaleStart,
  tailscaleStatus,
  type MobileTailscaleState,
} from '../modules/bothub-tunnel';
import {
  cancelTakeoverAlert,
  endBotActivity,
  showTakeoverAlert,
  updateBotActivity,
} from '../modules/bothub-notifications';
import { apiRequest, connectSSE, normalizeAddress, UnauthorizedError } from './api';
import { tStatic, type ResolvedLang } from './i18n';
import { loadSavedServers, loadTailscaleBindPref, saveServers, saveTailscaleBindPref } from './storage';
import type {
  ActiveDesktopTarget,
  BotDetail,
  BotStatus,
  ChatMessage,
  DesktopFrame,
  DesktopPermissionRequestResponse,
  DesktopPermissionsResponse,
  DesktopTarget,
  FallbackHost,
  HostTailscaleStatus,
  MagpieMemoryList,
  NodeInfo,
  PendingQuestion,
  RoutineItem,
  RoutineRun,
  RoutineUpsertInput,
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

export function formatClockTime(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
}

export function countToolCalls(tools?: string[]): number {
  if (!tools || tools.length === 0) return 0;
  let total = 0;
  for (const item of tools) {
    const idx = item.lastIndexOf('\u00d7');
    if (idx >= 0) {
      const n = Number.parseInt(item.slice(idx + 1), 10);
      total += Number.isFinite(n) && n > 0 ? n : 1;
    } else {
      total += 1;
    }
  }
  return total;
}

export function formatDurationMs(ms?: number): string {
  if (!ms || ms <= 0) return '';
  const sec = ms / 1000;
  if (sec < 60) return `${sec.toFixed(1)}s`;
  let mins = Math.floor(sec / 60);
  let rem = Math.round(sec % 60);
  if (rem === 60) {
    mins += 1;
    rem = 0;
  }
  return `${mins}m ${rem}s`;
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
  isTailscale: boolean;
  online: boolean;
  usingStandby: boolean;
  bots: BotStatus[];
  msgs: Record<string, ChatMessage[]>;
  last: Record<string, ChatMessage>;
  unread: Record<string, number>;
  tools: Record<string, string>;
  settings: SettingsData | null;
  models: Record<string, string[]>;
  nodes: NodeInfo[];
  tailcat: TailcatStatus | null;
  mobileTailscale: MobileTailscaleState | null;
  hostTailscale: HostTailscaleStatus | null;
  tsBinding: boolean;
  tsControlUrl: string;
  takeovers: TakeoverRequest[];
  questions: PendingQuestion[];
  desktopTargets: DesktopTarget[];
  activeDesktopTarget: ActiveDesktopTarget;
  setActiveDesktopTarget: (target: string) => Promise<void>;
  setActiveChatBot: (botId: string | null) => void;
  addAndConnectServer: (input: { label?: string; address: string; token?: string; standbyAddress?: string; standbyToken?: string }) => Promise<boolean>;
  selectServer: (id: string) => Promise<void>;
  updateServerToken: (id: string, token: string) => Promise<void>;
  removeServer: (id: string) => Promise<void>;
  reconnect: () => Promise<void>;
  refreshBots: () => Promise<void>;
  loadMessages: (botId: string) => Promise<void>;
  sendMessage: (botId: string, text: string, mediaIds?: string[]) => Promise<void>;
  interruptBot: (botId: string) => Promise<boolean>;
  resetBot: (botId: string) => Promise<void>;
  getBotDetail: (botId: string) => Promise<BotDetail>;
  createBot: (body: { id: string; name: string; description?: string; avatar?: string; color?: string; harness?: string; model?: string; instructions?: string; privateSessions?: boolean }) => Promise<BotStatus>;
  updateBot: (botId: string, body: { name?: string; description?: string; avatar?: string; color?: string; instructions?: string; privateSessions?: boolean }) => Promise<BotStatus>;
  deleteBot: (botId: string) => Promise<void>;
  toggleUnread: (botId: string) => void;
  fetchMemories: (opts: { scope?: 'global' | 'project' | 'all'; botId?: string; query?: string; kind?: string; limit?: number }) => Promise<MagpieMemoryList>;
  saveMemory: (body: { scope?: 'global' | 'project'; botId?: string; kind?: string; text: string; tags?: string[] }) => Promise<void>;
  deleteMemory: (id: string) => Promise<void>;
  routines: Record<string, RoutineItem[]>;
  fetchRoutines: (botId: string) => Promise<RoutineItem[]>;
  createRoutine: (botId: string, body: RoutineUpsertInput) => Promise<RoutineItem>;
  updateRoutine: (botId: string, routineId: string, body: Partial<RoutineUpsertInput>) => Promise<RoutineItem>;
  deleteRoutine: (botId: string, routineId: string) => Promise<void>;
  runRoutine: (botId: string, routineId: string) => Promise<void>;
  fetchRoutineRuns: (botId: string, routineId?: string, limit?: number) => Promise<RoutineRun[]>;
  loadSettings: () => Promise<SettingsData>;
  loadModels: (harness: string) => Promise<string[]>;
  saveSettings: (opts: {
    settingsChanged: boolean;
    defaultHarness: string;
    defaultModel: string;
    defaultModels: Record<string, string>;
    sharedUserMd: string;
    botUpdates: { id: string; harness: string; model: string }[];
  }) => Promise<void>;
  connectMagpie: (harnessId: string, model: string) => Promise<SettingsData>;
  disconnectMagpie: (harnessId: string) => Promise<SettingsData>;
  openMagpie: () => Promise<void>;
  installMagpie: () => Promise<void>;
  setDesktopWorker: (harness: string, model: string) => Promise<SettingsData>;
  refreshNodes: () => Promise<void>;
  refreshTailcat: () => Promise<TailcatStatus>;
  refreshHostTailscale: () => Promise<HostTailscaleStatus>;
  refreshMobileTailscale: (includePeers?: boolean) => Promise<MobileTailscaleState>;
  bindTailscale: (opts?: { authKey?: string; controlUrl?: string }) => Promise<MobileTailscaleState>;
  logoutTailscale: () => Promise<void>;
  refreshDesktopTargets: () => Promise<DesktopTarget[]>;
  fetchDesktopState: (target: string) => Promise<{
    screen: ScreenState;
    takeovers: TakeoverRequest[];
    status?: { screenRecording?: string; accessibility?: string };
  }>;
  fetchDesktopPermissions: (target?: string) => Promise<DesktopPermissionsResponse>;
  requestDesktopPermission: (target: string, permission: string, openSettings?: boolean) => Promise<DesktopPermissionRequestResponse>;
  fetchDesktopFrame: (target: string, windowId?: string) => Promise<DesktopFrame>;
  sendDesktopInput: (body: Record<string, unknown>) => Promise<void>;
  resolveTakeover: (id: string, note?: string) => Promise<void>;
  replyQuestion: (id: string, answers: Record<string, unknown>, cancelled: boolean) => Promise<void>;
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
  const [isTailscale, setIsTailscale] = useState(false);
  const [online, setOnline] = useState(false);
  const [usingStandby, setUsingStandby] = useState(false);

  const [bots, setBots] = useState<BotStatus[]>([]);
  const [msgs, setMsgs] = useState<Record<string, ChatMessage[]>>({});
  const [last, setLast] = useState<Record<string, ChatMessage>>({});
  const [unread, setUnread] = useState<Record<string, number>>({});
  const [tools, setTools] = useState<Record<string, string>>({});
  const [settings, setSettings] = useState<SettingsData | null>(null);
  const [models, setModels] = useState<Record<string, string[]>>({});
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [tailcat, setTailcat] = useState<TailcatStatus | null>(null);
  const [mobileTailscale, setMobileTailscale] = useState<MobileTailscaleState | null>(null);
  const [hostTailscale, setHostTailscale] = useState<HostTailscaleStatus | null>(null);
  const [tsBinding, setTsBinding] = useState(false);
  const [tsControlUrl, setTsControlUrl] = useState('');
  const tsControlUrlRef = useRef('');
  const mobileTsRef = useRef<MobileTailscaleState | null>(null);
  tsControlUrlRef.current = tsControlUrl;
  mobileTsRef.current = mobileTailscale;
  const [takeovers, setTakeovers] = useState<TakeoverRequest[]>([]);
  const [questions, setQuestions] = useState<PendingQuestion[]>([]);
  const [desktopTargets, setDesktopTargets] = useState<DesktopTarget[]>([]);
  const [activeDesktopTarget, setActiveDesktopTargetState] = useState<ActiveDesktopTarget>({
    target: 'local',
    temporary: false,
  });
  const [routines, setRoutines] = useState<Record<string, RoutineItem[]>>({});

  const activeChatRef = useRef<string | null>(null);
  const sseCloseRef = useRef<(() => void) | null>(null);
  const connSeqRef = useRef(0);
  const baseURLRef = useRef('');
  const authTokenRef = useRef('');
  const serversRef = useRef<ServerProfile[]>([]);
  const activeServerIdRef = useRef<string | null>(null);
  const botsRef = useRef<BotStatus[]>([]);
  const botStepsRef = useRef<Record<string, number>>({});
  const knownTakeoversRef = useRef<Set<string>>(new Set());

  baseURLRef.current = baseURL;
  authTokenRef.current = authToken;
  serversRef.current = servers;
  activeServerIdRef.current = activeServerId;
  botsRef.current = bots;

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
          } else if (type === 'message_updated' && payload?.botId && payload?.id) {
            const m = payload as ChatMessage;
            setMsgs((prev) => {
              const list = prev[m.botId];
              if (!list) return prev;
              const idx = list.findIndex((x) => x.id === m.id);
              if (idx < 0) return prev;
              const copy = [...list];
              copy[idx] = m;
              return { ...prev, [m.botId]: copy };
            });
            setLast((prev) => (prev[m.botId]?.id === m.id ? { ...prev, [m.botId]: m } : prev));
          } else if (type === 'bot_status' && payload?.id) {
            const st = payload as BotStatus;
            const prevBusy = botsRef.current.find((b) => b.id === st.id)?.busy;
            setBots((prev) => {
              const i = prev.findIndex((b) => b.id === st.id);
              if (i >= 0) {
                const copy = [...prev];
                copy[i] = st;
                return copy;
              }
              return [...prev, st];
            });
            if (st.busy && !prevBusy) {
              botStepsRef.current[st.id] = 0;
              updateBotActivity(st.id, st.name || st.id, '正在思考', 0);
            } else if (!st.busy && prevBusy) {
              delete botStepsRef.current[st.id];
              endBotActivity(st.id, st.name || st.id, '任务已完成', AppState.currentState !== 'active');
            }
            if (!st.busy) {
              setTools((prev) => {
                if (!prev[st.id]) return prev;
                const copy = { ...prev };
                delete copy[st.id];
                return copy;
              });
            }
          } else if (type === 'tool_use' && payload?.botId) {
            const bid = String(payload.botId);
            const toolName = String(payload.tool || '');
            setTools((prev) => ({ ...prev, [bid]: toolName }));
            const nextSteps = (botStepsRef.current[bid] || 0) + 1;
            botStepsRef.current[bid] = nextSteps;
            const b = botsRef.current.find((x) => x.id === bid);
            updateBotActivity(
              bid,
              b?.name || bid,
              toolName ? `正在运行 ${toolName}` : '正在运行工具',
              nextSteps
            );
          } else if (type === 'bot_created' && payload?.id) {
            const st = payload as BotStatus;
            setBots((prev) => (prev.some((b) => b.id === st.id) ? prev : [...prev, st]));
          } else if (type === 'bot_deleted') {
            const bid = payload?.id || payload?.botId;
            if (bid) removeBotState(String(bid));
          } else if (type === 'nodes_changed' && Array.isArray(payload?.nodes)) {
            setNodes(payload.nodes);
          } else if (type === 'desktop_target' && payload?.target) {
            setActiveDesktopTargetState({
              target: String(payload.target || 'local'),
              name: payload.name ? String(payload.name) : undefined,
              temporary: Boolean(payload.temporary),
              expiresAt: payload.expiresAt ? String(payload.expiresAt) : undefined,
            });
          } else if (type === 'takeover' && Array.isArray(payload?.items)) {
            const items = payload.items as TakeoverRequest[];
            setTakeovers(items);
            const nextIds = new Set(items.map((it) => it.id));
            for (const prevId of knownTakeoversRef.current) {
              if (!nextIds.has(prevId)) cancelTakeoverAlert(prevId);
            }
            for (const it of items) {
              if (!knownTakeoversRef.current.has(it.id)) {
                showTakeoverAlert(it.id, it.reason || '', it.target || 'local');
              }
            }
            knownTakeoversRef.current = nextIds;
          } else if (type === 'questions' && Array.isArray(payload?.items)) {
            setQuestions(payload.items);
          } else if ((type === 'routine_updated' || type === 'routine_run') && payload?.botId) {
            const rbid = String(payload.botId);
            apiRequest<{ routines: RoutineItem[] }>(url, tok, `/api/bots/${encodeURIComponent(rbid)}/routines`)
              .then((res) => setRoutines((prev) => ({ ...prev, [rbid]: res.routines || [] })))
              .catch(() => {});
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
      setUsingStandby(false);
      setConnState('connecting');
      setConnError('');

      const tryEndpoint = async (rawAddr: string, rawTok: string, isStandby: boolean) => {
        const norm = normalizeAddress(rawAddr);
        let resolvedToken = rawTok || norm.token;
        let resolvedURL = norm.cleanAddress;

        try {
          if (norm.isTailscale) {
            if (mobileTsRef.current?.state !== 'running') {
              const st = await tailscaleStart({ controlUrl: tsControlUrlRef.current });
              setMobileTailscale(st);
              if (st.state === 'needs_login' && st.authUrl) {
                void Linking.openURL(st.authUrl).catch(() => {});
              }
            }
            const tRes = await startTunnel(norm.cleanAddress, resolvedToken);
            if (seq !== connSeqRef.current) return;
            resolvedURL = tRes.url;
            if (tRes.token && !resolvedToken) {
              resolvedToken = tRes.token;
            }
          } else if (norm.isTailcat) {
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

          if (!isStandby && (resolvedToken !== profile.token || norm.cleanAddress !== profile.address)) {
            const updated = allServers.map((s) =>
              s.id === profile.id ? { ...s, address: norm.cleanAddress, token: resolvedToken } : s
            );
            setServers(updated);
            await saveServers(updated, profile.id);
          }

          setIsTailcat(norm.isTailcat);
          setIsTailscale(norm.isTailscale);
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
          setRoutines({});
          setUsingStandby(isStandby);
          setConnState('connected');
          setConnError('');

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
              } catch {}
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
          apiRequest<HostTailscaleStatus>(resolvedURL, resolvedToken, '/api/tailscale')
            .then((ts) => seq === connSeqRef.current && setHostTailscale(ts))
            .catch(() => {});
          apiRequest<{ items: TakeoverRequest[] }>(resolvedURL, resolvedToken, '/api/desktop/takeovers')
            .then((t) => seq === connSeqRef.current && setTakeovers(t.items || []))
            .catch(() => {});
          apiRequest<{ targets?: DesktopTarget[]; activeTarget?: ActiveDesktopTarget }>(
            resolvedURL,
            resolvedToken,
            '/api/desktop/targets'
          )
            .then((d) => {
              if (seq !== connSeqRef.current) return;
              setDesktopTargets(d.targets || []);
              if (d.activeTarget) setActiveDesktopTargetState(d.activeTarget);
            })
            .catch(() => {});
          apiRequest<{ items: PendingQuestion[] }>(resolvedURL, resolvedToken, '/api/questions')
            .then((q) => seq === connSeqRef.current && setQuestions(q.items || []))
            .catch(() => {});

          startStream(resolvedURL, resolvedToken);
        } catch (err) {
          if (seq === connSeqRef.current) {
            setBaseURL(resolvedURL);
            setAuthToken(resolvedToken);
            setIsTailcat(norm.isTailcat);
            setIsTailscale(norm.isTailscale);
          }
          throw err;
        }
      };

      try {
        await tryEndpoint(profile.address, profile.token, false);
      } catch (err: any) {
        if (seq !== connSeqRef.current) return;
        if (err instanceof UnauthorizedError) {
          setUsingStandby(false);
          setConnState('unauthorized');
          setConnError(err.message);
          return;
        }
        const candidates: { address: string; token: string }[] = [];
        const sbAddr = (profile.standbyAddress || '').trim();
        if (sbAddr) {
          candidates.push({ address: sbAddr, token: profile.standbyToken || '' });
        }
        for (const fb of profile.fallbackHosts || []) {
          const addr = (fb.address || '').trim();
          if (addr && addr !== sbAddr) {
            candidates.push({ address: addr, token: fb.token || '' });
          }
        }
        let lastErr: any = err;
        for (const c of candidates) {
          try {
            await tryEndpoint(c.address, c.token, true);
            return;
          } catch (fbErr: any) {
            if (seq !== connSeqRef.current) return;
            lastErr = fbErr;
            if (fbErr instanceof UnauthorizedError) break;
          }
        }
        setUsingStandby(false);
        if (lastErr instanceof UnauthorizedError) {
          setConnState('unauthorized');
          setConnError(lastErr.message);
        } else {
          setConnState('error');
          setConnError(lastErr?.message || tStatic('connFailed'));
        }
      }
    },
    [startStream]
  );

  // Initial load from secure store
  useEffect(() => {
    let mounted = true;
    Promise.all([loadSavedServers(), loadTailscaleBindPref()]).then(([{ servers: saved, activeId }, tsPref]) => {
      if (!mounted) return;
      if (tsPref.controlUrl) setTsControlUrl(tsPref.controlUrl);
      if (tsPref.bound && isNativeTunnelAvailable()) {
        void tailscaleStart({ controlUrl: tsPref.controlUrl || '' })
          .then((st) => {
            if (!mounted) return;
            setMobileTailscale(st);
            if (st.state === 'running') {
              void tailscaleStatus(true).then((full) => mounted && setMobileTailscale(full)).catch(() => {});
            }
          })
          .catch(() => {});
      }
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
    async (input: { label?: string; address: string; token?: string; standbyAddress?: string; standbyToken?: string }) => {
      const norm = normalizeAddress(input.address);
      if (!norm.cleanAddress) throw new Error(tStatic('enterValidAddr'));
      const tok = (input.token || norm.token || '').trim();
      const sbParts = (input.standbyAddress || '')
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      const fallbacks: FallbackHost[] = [];
      for (const part of sbParts) {
        const fn = normalizeAddress(part);
        if (fn.cleanAddress) {
          const ft = (input.standbyToken || fn.token || '').trim() || undefined;
          fallbacks.push({ address: fn.cleanAddress, token: ft });
        }
      }
      const sbAddr = fallbacks[0]?.address;
      const sbTok = fallbacks[0]?.token;
      const existing = serversRef.current.find((s) => s.address === norm.cleanAddress);
      const label = (input.label || '').trim() || (norm.isTailcat ? tStatic('remoteHostDefault') : norm.cleanAddress.replace(/^https?:\/\//, ''));
      const target: ServerProfile = existing
        ? {
            ...existing,
            label: (input.label || '').trim() || existing.label,
            token: tok || existing.token,
            standbyAddress: sbAddr ?? existing.standbyAddress,
            standbyToken: sbTok ?? existing.standbyToken,
            fallbackHosts: fallbacks.length > 0 ? fallbacks : existing.fallbackHosts,
          }
        : {
            id: `${Date.now()}`,
            label,
            address: norm.cleanAddress,
            token: tok,
            standbyAddress: sbAddr,
            standbyToken: sbTok,
            fallbackHosts: fallbacks.length > 0 ? fallbacks : undefined,
          };

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
    async (botId: string, text: string, mediaIds?: string[]) => {
      const body: Record<string, unknown> = { message: text, source: 'mobile' };
      if (mediaIds && mediaIds.length > 0) {
        body.mediaIds = mediaIds;
      }
      const data = await apiRequest<{ text?: string }>(
        baseURLRef.current,
        authTokenRef.current,
        `/api/bots/${encodeURIComponent(botId)}/message`,
        { method: 'POST', body }
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

  const toggleUnread = useCallback((botId: string) => {
    setUnread((prev) => ({ ...prev, [botId]: (prev[botId] || 0) > 0 ? 0 : 1 }));
  }, []);

  const fetchMemories = useCallback(
    async (opts: { scope?: 'global' | 'project' | 'all'; botId?: string; query?: string; kind?: string; limit?: number }) => {
      const q = new URLSearchParams();
      if (opts.scope) q.set('scope', opts.scope);
      if (opts.botId) q.set('botId', opts.botId);
      if (opts.query?.trim()) q.set('query', opts.query.trim());
      if (opts.kind?.trim()) q.set('kind', opts.kind.trim());
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

  const fetchRoutines = useCallback(async (botId: string) => {
    const res = await apiRequest<{ routines: RoutineItem[] }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/bots/${encodeURIComponent(botId)}/routines`
    );
    const list = res.routines || [];
    setRoutines((prev) => ({ ...prev, [botId]: list }));
    return list;
  }, []);

  const createRoutine = useCallback(
    async (botId: string, body: RoutineUpsertInput) => {
      const created = await apiRequest<RoutineItem>(
        baseURLRef.current,
        authTokenRef.current,
        `/api/bots/${encodeURIComponent(botId)}/routines`,
        { method: 'POST', body }
      );
      await fetchRoutines(botId);
      return created;
    },
    [fetchRoutines]
  );

  const updateRoutine = useCallback(
    async (botId: string, routineId: string, body: Partial<RoutineUpsertInput>) => {
      const updated = await apiRequest<RoutineItem>(
        baseURLRef.current,
        authTokenRef.current,
        `/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(routineId)}`,
        { method: 'PATCH', body }
      );
      await fetchRoutines(botId);
      return updated;
    },
    [fetchRoutines]
  );

  const deleteRoutine = useCallback(
    async (botId: string, routineId: string) => {
      await apiRequest(
        baseURLRef.current,
        authTokenRef.current,
        `/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(routineId)}`,
        { method: 'DELETE' }
      );
      await fetchRoutines(botId);
    },
    [fetchRoutines]
  );

  const runRoutine = useCallback(
    async (botId: string, routineId: string) => {
      await apiRequest(
        baseURLRef.current,
        authTokenRef.current,
        `/api/bots/${encodeURIComponent(botId)}/routines/${encodeURIComponent(routineId)}/run?async=1`,
        { method: 'POST' }
      );
      await fetchRoutines(botId);
    },
    [fetchRoutines]
  );

  const fetchRoutineRuns = useCallback(async (botId: string, routineId?: string, limit = 10) => {
    const q = new URLSearchParams();
    if (routineId) q.set('routineId', routineId);
    q.set('limit', String(limit));
    const res = await apiRequest<{ runs: RoutineRun[] }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/bots/${encodeURIComponent(botId)}/routine-runs?${q.toString()}`
    );
    return res.runs || [];
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
      defaultHarness: string;
      defaultModel: string;
      defaultModels: Record<string, string>;
      sharedUserMd: string;
      botUpdates: { id: string; harness: string; model: string }[];
    }) => {
      if (opts.settingsChanged) {
        const s = await apiRequest<SettingsData>(baseURLRef.current, authTokenRef.current, '/api/settings', {
          method: 'PUT',
          body: {
            defaultHarness: opts.defaultHarness,
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

  const openMagpie = useCallback(async () => {
    await apiRequest(baseURLRef.current, authTokenRef.current, '/api/magpie/open', { method: 'POST' });
  }, []);

  const installMagpie = useCallback(async () => {
    let st = await apiRequest<{ state: string; error?: string }>(
      baseURLRef.current,
      authTokenRef.current,
      '/api/magpie/install',
      { method: 'POST' }
    );
    while (st.state === 'running') {
      await new Promise((r) => setTimeout(r, 1200));
      st = await apiRequest<{ state: string; error?: string }>(
        baseURLRef.current,
        authTokenRef.current,
        '/api/magpie/install'
      );
    }
    if (st.state === 'error') {
      throw new Error(st.error || 'Magpie install failed');
    }
    await loadSettings();
  }, [loadSettings]);

  const setDesktopWorker = useCallback(async (harness: string, model: string) => {
    const s = await apiRequest<SettingsData>(baseURLRef.current, authTokenRef.current, '/api/settings', {
      method: 'PUT',
      body: { desktopHarness: harness.trim(), desktopModel: model.trim() },
    });
    setSettings(s);
    return s;
  }, []);
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

  const refreshHostTailscale = useCallback(async () => {
    const ts = await apiRequest<HostTailscaleStatus>(baseURLRef.current, authTokenRef.current, '/api/tailscale');
    setHostTailscale(ts);
    return ts;
  }, []);

  const refreshMobileTailscale = useCallback(async (includePeers = true) => {
    const st = await tailscaleStatus(includePeers);
    setMobileTailscale(st);
    if (st.state === 'running') {
      await saveTailscaleBindPref({ bound: true, controlUrl: tsControlUrlRef.current });
    }
    return st;
  }, []);

  const bindTailscale = useCallback(async (opts: { authKey?: string; controlUrl?: string } = {}) => {
    setTsBinding(true);
    try {
      const ctrl = (opts.controlUrl || '').trim();
      const key = (opts.authKey || '').trim();
      let st = await tailscaleStart({ controlUrl: ctrl, authKey: key });
      setMobileTailscale(st);
      let openedAuth = '';
      if (st.state === 'needs_login' && st.authUrl) {
        openedAuth = st.authUrl;
        void Linking.openURL(st.authUrl).catch(() => {});
      }
      const deadline = Date.now() + 180000;
      while ((st.state === 'starting' || st.state === 'needs_login') && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 1500));
        st = await tailscaleStatus(true);
        setMobileTailscale(st);
        if (st.state === 'needs_login' && st.authUrl && st.authUrl !== openedAuth) {
          openedAuth = st.authUrl;
          void Linking.openURL(st.authUrl).catch(() => {});
        }
      }
      if (st.state === 'running') {
        setTsControlUrl(ctrl);
        await saveTailscaleBindPref({ bound: true, controlUrl: ctrl });
        if (!st.peers || st.peers.length === 0) {
          st = await tailscaleStatus(true);
          setMobileTailscale(st);
        }
      }
      return st;
    } finally {
      setTsBinding(false);
    }
  }, []);

  const logoutTailscale = useCallback(async () => {
    await tailscaleLogout();
    setMobileTailscale(null);
    setTsControlUrl('');
    await saveTailscaleBindPref(null);
  }, []);

  const refreshDesktopTargets = useCallback(async () => {
    const d = await apiRequest<{
      targets: DesktopTarget[];
      activeTarget?: ActiveDesktopTarget;
      takeovers: TakeoverRequest[];
    }>(
      baseURLRef.current,
      authTokenRef.current,
      '/api/desktop/targets'
    );
    const list = d.targets || [];
    setDesktopTargets(list);
    if (d.activeTarget) {
      setActiveDesktopTargetState(d.activeTarget);
    }
    setTakeovers(d.takeovers || []);
    return list;
  }, []);

  const setActiveDesktopTarget = useCallback(async (target: string) => {
    const res = await apiRequest<{ activeTarget?: ActiveDesktopTarget }>(
      baseURLRef.current,
      authTokenRef.current,
      '/api/desktop/target',
      { method: 'PUT', body: { target } }
    );
    if (res.activeTarget) {
      setActiveDesktopTargetState(res.activeTarget);
    }
  }, []);

  const fetchDesktopState = useCallback(async (target: string) => {
    const d = await apiRequest<{
      screen: ScreenState;
      takeovers: TakeoverRequest[];
      status?: { screenRecording?: string; accessibility?: string };
    }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/desktop/state?target=${encodeURIComponent(target)}`
    );
    setTakeovers(d.takeovers || []);
    return d;
  }, []);

  const fetchDesktopPermissions = useCallback(async (target = 'local') => {
    return apiRequest<DesktopPermissionsResponse>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/desktop/permissions?target=${encodeURIComponent(target || 'local')}`
    );
  }, []);

  const requestDesktopPermission = useCallback(async (target: string, permission: string, openSettings = false) => {
    return apiRequest<DesktopPermissionRequestResponse>(
      baseURLRef.current,
      authTokenRef.current,
      '/api/desktop/permissions/request',
      { method: 'POST', body: { target: target || 'local', permission, openSettings } }
    );
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

  const replyQuestion = useCallback(async (id: string, answers: Record<string, unknown>, cancelled: boolean) => {
    const res = await apiRequest<{ ok: boolean; items: PendingQuestion[] }>(
      baseURLRef.current,
      authTokenRef.current,
      `/api/questions/${encodeURIComponent(id)}/reply`,
      { method: 'POST', body: { answers, cancelled } }
    );
    setQuestions(res.items || []);
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
        isTailscale,
        online,
        usingStandby,
        bots,
        msgs,
        last,
        unread,
        tools,
        settings,
        models,
        nodes,
        tailcat,
        mobileTailscale,
        hostTailscale,
        tsBinding,
        tsControlUrl,
        takeovers,
        questions,
        desktopTargets,
        activeDesktopTarget,
        setActiveDesktopTarget,
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
        toggleUnread,
        fetchMemories,
        saveMemory,
        deleteMemory,
        routines,
        fetchRoutines,
        createRoutine,
        updateRoutine,
        deleteRoutine,
        runRoutine,
        fetchRoutineRuns,
        loadSettings,
        loadModels,
        saveSettings,
        connectMagpie,
        disconnectMagpie,
        openMagpie,
        installMagpie,
        setDesktopWorker,
        refreshNodes,
        refreshTailcat,
        refreshHostTailscale,
        refreshMobileTailscale,
        bindTailscale,
        logoutTailscale,
        refreshDesktopTargets,
        fetchDesktopState,
        fetchDesktopPermissions,
        requestDesktopPermission,
        fetchDesktopFrame,
        sendDesktopInput,
        resolveTakeover,
        replyQuestion,
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
