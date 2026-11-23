export type LangPref = 'system' | 'zh-CN' | 'en';
export type ResolvedLang = 'zh-CN' | 'en';
export type ThemePref = 'system' | 'light' | 'dark';
export type SettingsTab = 'general' | 'models' | 'memory' | 'remote';

export interface BotSessionInfo {
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
  harness?: string;
  model?: string;
  modelInherited?: boolean;
  workspace?: string;
  busy?: boolean;
  privateSessions?: boolean;
}

export interface BotDetail extends BotStatus {
  session?: BotSessionInfo;
  archivedSessions?: number;
  instructions?: string;
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
  role: 'user' | 'assistant' | 'bot_out' | 'bot_in' | 'system' | string;
  from?: string;
  to?: string;
  text: string;
  media?: MessageMedia[];
  createdAt: string;
  tools?: string[];
  durationMs?: number;
  costUsd?: number;
}

export interface MagpieProvider {
  id: string;
  name: string;
  enabled: boolean;
  endpoint?: string;
  status?: string;
  modelsInfo?: string;
  usedBy?: string;
}

export interface MagpiePreset {
  id: string;
  name: string;
}

export interface HarnessInfo {
  id: string;
  label: string;
  installed?: boolean;
  binaryPath?: string;
  defaultModel?: string;
  install?: {
    command?: string;
    url?: string;
  };
  magpie?: {
    connected?: boolean;
    model?: string;
    nativeModel?: string;
  };
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
      docs?: string;
    };
  };
  onboarding?: {
    completed?: boolean;
    needsSetup?: boolean;
  };
  defaultHarness?: string;
  defaultModel?: string;
  defaultModels?: Record<string, string>;
  sharedUserMd?: string;
  desktopHarness?: string;
  desktopModel?: string;
  harnesses: HarnessInfo[];
}

export interface NodeInfo {
  id: string;
  name?: string;
  os?: string;
  arch?: string;
  permissions?: string[];
  connected: boolean;
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
  media?: MessageMedia;
  name: string;
  kind: 'image' | 'file';
  previewUrl?: string;
  uploading?: boolean;
  error?: string;
}

export interface LocalNodeState {
  available?: boolean;
  enabled?: boolean;
  connected?: boolean;
  status: string;
  name?: string;
  permissions?: string[];
}

export interface TailcatStatus {
  state: 'disabled' | 'connecting' | 'ready' | 'error' | string;
  link?: string;
  error?: string;
  hasToken?: boolean;
  tokenEnvRef?: string;
  apiToken?: string;
}

export interface TailscaleStatus {
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
  authKeyEnvRef?: string;
  apiToken?: string;
}

export interface MemoryItem {
  id: string;
  kind?: string;
  tags?: string[];
  text: string;
  updatedAt?: string;
}

export interface MemoryResponse {
  installed?: boolean;
  available?: boolean;
  error?: string;
  items?: MemoryItem[];
}

export interface RoutineRun {
  id?: string;
  botId?: string;
  status: 'ok' | 'silent' | 'error' | string;
  trigger?: string;
  durationMs: number;
  costUsd?: number;
  output?: string;
  error?: string;
  finishedAt: string;
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
  mainDelivery: 'receipt' | 'notify' | 'none' | string;
  enabled: boolean;
  running?: boolean;
  nextRunAt?: string;
  lastRun?: RoutineRun;
}

export interface ScreenState {
  target: string;
  owner: 'idle' | 'human' | string;
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
  createdAt?: string;
}

export interface DesktopTarget {
  id: string;
  name: string;
  local?: boolean;
  supported?: boolean;
  webrtcAvailable?: boolean;
  virtual?: boolean;
  powerState?: 'running' | 'frozen' | 'stopped' | string;
  installable?: boolean;
  screen?: ScreenState;
}

export interface DesktopWindow {
  id: string;
  app?: string;
  title?: string;
}

export interface DesktopHostStatus {
  supported?: boolean;
  webrtcAvailable?: boolean;
  screenRecording?: string;
  accessibility?: string;
  logicalX?: number;
  logicalY?: number;
  logicalWidth?: number;
  logicalHeight?: number;
  virtual?: boolean;
  powerState?: 'running' | 'frozen' | 'stopped' | string;
  keepAwake?: boolean;
  installable?: boolean;
  missingDeps?: string[];
  installCommand?: string;
  displayServer?: string;
}

export interface DesktopFrame {
  jpegBase64: string;
  imageWidth: number;
  imageHeight: number;
  logicalX?: number;
  logicalY?: number;
  logicalWidth?: number;
  logicalHeight?: number;
}

export interface ConfirmOptions {
  title: string;
  text: string;
  okText?: string;
  okClass?: 'bad' | 'go' | '';
  onOk: () => Promise<void> | void;
  altText?: string;
  altClass?: 'bad' | 'go' | '';
  onAlt?: () => Promise<void> | void;
}

export interface SettingsDraft {
  defaultHarness: string;
  defaultModel: string;
  defaultModels: Record<string, string>;
  sharedUserMd: string;
  bots: Record<string, { harness: string; model: string }>;
}

export interface QuestionOption {
  value: string;
  label: string;
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
