export interface SessionRecord {
  sessionId?: string;
  turnCount?: number;
  totalCostUsd?: number;
}

export interface BotStatus {
  id: string;
  name: string;
  description?: string;
  workspace: string;
  harness: string;
  model?: string;
  modelInherited?: boolean;
  privateSessions?: boolean;
  archivedSessions?: number;
  busy?: boolean;
  queued?: number;
  session?: SessionRecord;
}

export interface MagpieMemoryItem {
  id: string;
  kind?: string;
  scope?: string;
  tags?: string[];
  updatedAt?: string;
  text: string;
}

export interface MagpieMemoryList {
  installed: boolean;
  available: boolean;
  error?: string;
  items: MagpieMemoryItem[];
}

export interface BotDetail extends BotStatus {
  instructions: string;
}

export interface ChatMessage {
  id: string;
  botId: string;
  role: 'user' | 'assistant' | 'bot_out' | 'bot_in' | 'system';
  from?: string;
  to?: string;
  text: string;
  createdAt: string;
  durationMs?: number;
  costUsd?: number;
}

export interface HarnessInfo {
  id: string;
  label: string;
  defaultModel: string;
  magpie: {
    installed?: boolean;
    connected: boolean;
    model?: string;
    nativeModel?: string;
  };
}

export interface MagpieProvider {
  id: string;
  name: string;
  endpoint?: string;
  status?: string;
  modelsInfo?: string;
  usedBy?: string;
  enabled: boolean;
}

export interface MagpiePreset {
  id: string;
  name: string;
}

export interface SettingsData {
  magpie?: {
    installed: boolean;
    healthy?: boolean;
    providers?: MagpieProvider[];
    presets?: MagpiePreset[];
    install?: {
      script?: string;
      brew?: string;
      download?: string;
      docs?: string;
    };
  };
  desktopHarness?: string;
  desktopModel?: string;
  defaultModel: string;
  defaultModels?: Record<string, string>;
  sharedUserMd?: string;
  harnesses: HarnessInfo[];
}

export interface TailcatStatus {
  state: 'disabled' | 'connecting' | 'ready' | 'error';
  link?: string;
  error?: string;
  hasToken?: boolean;
  tokenEnvRef?: string;
}

export interface NodeInfo {
  id: string;
  name: string;
  os: string;
  arch: string;
  hostname?: string;
  homeDir?: string;
  permissions: string[];
  connected: boolean;
  lastSeenAt?: string;
}

export interface ScreenState {
  target: string;
  owner: 'idle' | 'human';
  botId?: string;
  botName?: string;
}

export interface TakeoverRequest {
  id: string;
  target: string;
  botId: string;
  botName?: string;
  reason: string;
  app?: string;
  createdAt: string;
}

export interface DesktopTarget {
  id: string;
  name: string;
  local: boolean;
  os?: string;
  arch?: string;
  supported: boolean;
  webrtcAvailable?: boolean;
  backend?: string;
  screen: ScreenState;
}

export interface DesktopFrame {
  jpegBase64: string;
  imageWidth: number;
  imageHeight: number;
  logicalX: number;
  logicalY: number;
  logicalWidth: number;
  logicalHeight: number;
  backend: string;
  timestamp: number;
}

export interface ServerProfile {
  id: string;
  label: string;
  address: string;
  token: string;
}

export type RoutineDelivery = 'receipt' | 'notify' | 'none';

export interface RoutineRun {
  id: string;
  botId: string;
  routineId: string;
  routineName?: string;
  trigger: 'schedule' | 'manual' | string;
  status: 'ok' | 'silent' | 'error' | string;
  delivery: RoutineDelivery | string;
  model?: string;
  output?: string;
  error?: string;
  durationMs?: number;
  costUsd?: number;
  inputTokens?: number;
  outputTokens?: number;
  startedAt: string;
  finishedAt: string;
  injectedAt?: string;
}

export interface RoutineItem {
  id: string;
  name: string;
  everyMinutes?: number;
  dailyAt?: string;
  cron?: string;
  webhookTrigger?: boolean;
  webhookPath?: string;
  webhookSecretSet?: boolean;
  webhookUrl?: string;
  prompt: string;
  model?: string;
  mainDelivery: RoutineDelivery;
  enabled: boolean;
  running: boolean;
  nextRunAt?: string;
  lastRun?: RoutineRun;
}

export interface RoutineUpsertInput {
  id?: string;
  name: string;
  everyMinutes?: number;
  dailyAt?: string;
  cron?: string;
  webhookTrigger?: boolean;
  webhookSecret?: string;
  clearWebhookSecret?: boolean;
  webhookUrl?: string;
  prompt: string;
  model?: string;
  mainDelivery?: RoutineDelivery;
  enabled?: boolean;
}
