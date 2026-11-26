export type LangPref = 'system' | 'zh-CN' | 'en';
export type ResolvedLang = 'zh-CN' | 'en';
export type ThemePref = 'system' | 'light' | 'dark';
export type SettingsTab = 'general' | 'models' | 'memory' | 'desktop' | 'remote';
export type PermissionMode = 'auto' | 'edit' | 'ask' | 'plan';

export interface PermissionCapabilities {
  modes?: PermissionMode[];
  interactiveApproval?: boolean;
  supportsAllowAlways?: boolean;
}

export interface PermissionOption {
  id: string;
  kind: 'allow_once' | 'allow_always' | 'reject' | string;
  label: string;
  description?: string;
}

export interface PendingApproval {
  id: string;
  botId: string;
  botName?: string;
  taskId?: string;
  deviceId?: string;
  toolName: string;
  kind: 'shell' | 'write' | 'read' | 'mcp' | 'network' | 'other' | string;
  title?: string;
  summary?: string;
  rawInput?: Record<string, unknown>;
  options?: PermissionOption[];
  mode?: PermissionMode | string;
  createdAt: string;
}

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
  desktopTarget?: string;
  browserMode?: 'auto' | 'isolated' | 'attach' | string;
  permissionMode?: PermissionMode;
  latestStage?: StageRef;
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
  role: 'user' | 'assistant' | 'bot_out' | 'bot_in' | 'stage' | 'system' | string;
  from?: string;
  to?: string;
  text: string;
  media?: MessageMedia[];
  stage?: StageRef;
  createdAt: string;
  tools?: string[];
  durationMs?: number;
  costUsd?: number;
}

export interface HarnessInfo {
  id: string;
  label: string;
  installed?: boolean;
  binaryPath?: string;
  defaultModel?: string;
  authStatus?: 'ready' | 'needs_login' | 'unknown' | string;
  authDetail?: string;
  requiresLogin?: boolean;
  canAutoInstall?: boolean;
  category?: 'recommended' | 'advanced' | string;
  summary?: string;
  permissions?: PermissionCapabilities;
  install?: {
    command?: string;
    url?: string;
  };
}

export interface SetupTaskStatus {
  target: string;
  kind: 'install' | 'login' | string;
  state: 'idle' | 'running' | 'done' | 'error' | string;
  logs?: string[];
  error?: string;
  loginUrl?: string;
  authStatus?: string;
  authDetail?: string;
  updatedAt?: number;
}

export interface SettingsData {
  onboarding?: {
    completed?: boolean;
    needsSetup?: boolean;
  };
  defaultHarness?: string;
  defaultModel?: string;
  defaultPermissionMode?: PermissionMode;
  defaultModels?: Record<string, string>;
  sharedUserMd?: string;
  desktopHarness?: string;
  desktopModel?: string;
  canResetDesktopMode?: boolean;
  harnesses: HarnessInfo[];
}

export interface ServerInfo {
  protocolVersion: number;
  minClientProtocol?: number;
  minNodeProtocol?: number;
  version?: string;
  capabilities?: string[];
}

export type UpdateChannel = 'stable' | 'prerelease';
export type UpdateInterval = '30m' | '1h' | '6h' | '24h';

export interface UpdateConfig {
  autoCheck: boolean;
  interval: UpdateInterval | string;
  channel: UpdateChannel | string;
  proxy?: string;
}

export interface UpdateReleaseInfo {
  version: string;
  tag: string;
  name?: string;
  url: string;
  notes?: string;
  publishedAt?: string;
  prerelease?: boolean;
}

export interface UpdateStatus {
  currentVersion: string;
  state: 'idle' | 'checking' | 'up_to_date' | 'available' | 'error' | string;
  hasUpdate: boolean;
  latest?: UpdateReleaseInfo;
  checkedAt?: string;
  error?: string;
  errorCode?: string;
  config: UpdateConfig;
}

export interface NodeHarnessInfo {
  kind: string;
  label?: string;
  installed: boolean;
  authStatus?: 'ready' | 'needs_login' | 'unknown' | string;
  authDetail?: string;
  permissionCapabilities?: PermissionCapabilities;
}

export interface NodeWorkspace {
  id: string;
  name: string;
  path: string;
  isGit?: boolean;
  defaultBranch?: string;
}

export interface NodeInfo {
  id: string;
  name?: string;
  os?: string;
  arch?: string;
  hostname?: string;
  homeDir?: string;
  permissions?: string[];
  protocolVersion?: number;
  capabilities?: string[];
  version?: string;
  connected: boolean;
  reconnecting?: boolean;
  transport?: 'ws' | 'poll' | string;
  lastSeenAt?: string;
  harnesses?: NodeHarnessInfo[];
  workspaces?: NodeWorkspace[];
  maxPermissionMode?: PermissionMode | string;
  activeTaskIds?: string[];
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
  scope?: string;
  tags?: string[];
  text: string;
  updatedAt?: string;
}

export interface MemorySemanticStatus {
  enabled?: boolean;
  modelReady?: boolean;
  indexReady?: boolean;
  active?: boolean;
  model?: string;
}

export interface MemoryResponse {
  installed?: boolean;
  available?: boolean;
  error?: string;
  semantic?: MemorySemanticStatus;
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
  permissionMode?: string;
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
  taskId?: string;
  deviceId?: string;
  reason: string;
  app?: string;
  createdAt?: string;
}

export interface DesktopTarget {
  id: string;
  name: string;
  local?: boolean;
  hostId?: string;
  slot?: string;
  supported?: boolean;
  webrtcAvailable?: boolean;
  virtual?: boolean;
  virtualSupported?: boolean;
  protocolVersion?: number;
  outdated?: boolean;
  powerState?: 'running' | 'frozen' | 'stopped' | string;
  installable?: boolean;
  screen?: ScreenState;
}

export interface ActiveDesktopTarget {
  target: string;
  name?: string;
  temporary: boolean;
  expiresAt?: string;
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
  defaultPermissionMode: PermissionMode;
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
  taskId?: string;
  deviceId?: string;
  title?: string;
  fields: QuestionField[];
  createdAt: string;
}

export type BotTaskKind = 'worker' | 'routine' | 'desktop';
export type BotTaskStatus =
  | 'queued'
  | 'running'
  | 'waiting_approval'
  | 'waiting_question'
  | 'waiting_takeover'
  | 'completed'
  | 'failed'
  | 'cancelled';
export type BotTaskDelivery = 'wake' | 'receipt' | 'silent';

export interface BotTask {
  id: string;
  botId: string;
  kind: BotTaskKind | string;
  title?: string;
  prompt: string;
  status: BotTaskStatus | string;
  deviceId: string;
  harness?: string;
  model?: string;
  permissionMode?: PermissionMode | string;
  workdir?: string;
  delivery: BotTaskDelivery | string;
  dependsOn?: string[];
  parentTaskId?: string;
  routineId?: string;
  routineName?: string;
  trigger?: string;
  slotKey?: string;
  runtimeSessionId?: string;
  output?: string;
  error?: string;
  tools?: string[];
  durationMs?: number;
  costUsd?: number;
  inputTokens?: number;
  outputTokens?: number;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  injectedAt?: string;
}

export type BotTaskEventType =
  | 'status'
  | 'chunk'
  | 'tool_use'
  | 'message'
  | 'approval'
  | 'question'
  | 'takeover'
  | 'error';

export interface BotTaskEvent {
  seq: number;
  taskId: string;
  botId: string;
  eventType: BotTaskEventType | string;
  payload?: Record<string, unknown> | string;
  createdAt: string;
}

export interface SpawnTaskRequest {
  kind?: BotTaskKind | string;
  title?: string;
  prompt: string;
  deviceId?: string;
  harness?: string;
  model?: string;
  permissionMode?: PermissionMode | string;
  workdir?: string;
  delivery?: BotTaskDelivery | string;
  dependsOn?: string[];
  parentTaskId?: string;
}

export interface WorkspaceFileEntry {
  name: string;
  path: string;
  isDir: boolean;
  isSymlink?: boolean;
  isShared?: boolean;
  size: number;
  modTime: string;
  mimeType?: string;
}

export interface WorkspaceListResult {
  botId: string;
  path: string;
  parent: string;
  entries: WorkspaceFileEntry[];
  truncated?: boolean;
}

export interface WorkspaceFileContent {
  botId: string;
  path: string;
  name: string;
  size: number;
  modTime: string;
  mimeType: string;
  kind: 'text' | 'markdown' | 'image' | 'binary';
  content?: string;
  truncated?: boolean;
}

export type StageKind = 'new' | 'files' | 'file' | 'desktop' | 'browser' | 'artifact' | 'mcp-app' | string;

export interface StageRef {
  id?: string;
  kind: StageKind;
  title?: string;
  subtitle?: string;
  path?: string;
  url?: string;
  resourceUri?: string;
  target?: string;
  closable?: boolean;
  payload?: Record<string, unknown>;
  createdAt?: string;
}

export interface StageTab {
  id: string;
  kind: StageKind;
  title: string;
  subtitle?: string;
  botId?: string;
  path?: string;
  url?: string;
  resourceUri?: string;
  target?: string;
  closable?: boolean;
}

export interface StageOpenEvent {
  id?: string;
  kind: StageKind;
  title?: string;
  subtitle?: string;
  botId?: string;
  path?: string;
  url?: string;
  resourceUri?: string;
  target?: string;
  closable?: boolean;
  autoOpen?: boolean;
  createdAt?: string;
}
