export interface SessionRecord {
  sessionId?: string;
  turnCount?: number;
  totalCostUsd?: number;
}

export interface BotStatus {
  id: string;
  name: string;
  description?: string;
  avatar?: string;
  color?: string;
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

export interface MagpieMemorySemanticStatus {
  enabled?: boolean;
  modelReady?: boolean;
  indexReady?: boolean;
  active?: boolean;
  model?: string;
}

export interface MagpieMemoryList {
  installed: boolean;
  available: boolean;
  error?: string;
  semantic?: MagpieMemorySemanticStatus;
  items: MagpieMemoryItem[];
}

export interface BotDetail extends BotStatus {
  instructions: string;
}

export interface MessageMedia {
  id: string;
  kind?: 'image' | 'file' | string;
  url: string;
  mimeType?: string;
  name?: string;
  alt?: string;
  size?: number;
}

export interface ChatMessage {
  id: string;
  botId: string;
  role: 'user' | 'assistant' | 'bot_out' | 'bot_in' | 'system';
  from?: string;
  to?: string;
  text: string;
  media?: MessageMedia[];
  createdAt: string;
  tools?: string[];
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
    webUrl?: string;
    install?: {
      script?: string;
      brew?: string;
      download?: string;
      docs?: string;
    };
  };
  desktopHarness?: string;
  desktopModel?: string;
  defaultHarness?: string;
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

export interface ActiveDesktopTarget {
  target: string;
  name?: string;
  temporary: boolean;
  expiresAt?: string;
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

export interface HostTailscaleStatus {
  enabled?: boolean;
  state: 'disabled' | 'starting' | 'needs_login' | 'running' | 'error' | string;
  authUrl?: string;
  hostname?: string;
  dnsName?: string;
  ip?: string;
  tailnet?: string;
  link?: string;
  controlUrl?: string;
  error?: string;
  hasToken?: boolean;
  tokenEnvRef?: string;
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

export interface QuestionOption {
  value: string;
  label?: string;
  description?: string;
}

export interface QuestionField {
  key: string;
  title?: string;
  description?: string;
  type: 'string' | 'multiselect' | 'boolean' | 'number' | 'integer' | string;
  required?: boolean;
  placeholder?: string;
  default?: unknown;
  options?: QuestionOption[];
  custom?: boolean;
}

export interface PendingQuestion {
  id: string;
  botId: string;
  botName?: string;
  title?: string;
  fields: QuestionField[];
  createdAt: string;
}

export interface SlashCommandItem {
  name: string;
  description?: string;
  argumentHint?: string;
  source: 'harness' | 'skill' | 'workflow' | 'hub' | string;
}

export interface MentionCandidate {
  kind: 'file' | 'bot' | 'agent' | string;
  value: string;
  label: string;
  description?: string;
}

export interface PendingAttachment {
  id: string;
  name: string;
  kind: 'image' | 'file';
  uri: string;
  media?: MessageMedia;
  uploading?: boolean;
  error?: string;
}

export interface DesktopPermissionItem {
  id: string;
  name: string;
  description: string;
  status: 'granted' | 'denied' | 'not_determined' | 'unsupported' | string;
  canPrompt?: boolean;
  canOpenSettings?: boolean;
}

export interface DesktopPermissionsResponse {
  target?: string;
  platform: string;
  items: DesktopPermissionItem[];
}

export interface DesktopPermissionRequestResponse {
  ok: boolean;
  prompted?: boolean;
  openedSettings?: boolean;
  item: DesktopPermissionItem;
  items: DesktopPermissionItem[];
}
