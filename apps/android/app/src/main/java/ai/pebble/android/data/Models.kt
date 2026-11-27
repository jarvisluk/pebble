package ai.pebble.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.intOrNull

const val CLIENT_PROTOCOL = 2

val HubJson = Json {
    ignoreUnknownKeys = true
    isLenient = true
    coerceInputValues = true
    explicitNulls = false
}

@Serializable
data class ServerInfo(
    val protocolVersion: Int = 1,
    val minClientProtocol: Int? = null,
    val minNodeProtocol: Int? = null,
    val version: String? = null,
    val capabilities: List<String>? = null,
)

@Serializable
data class SessionRecord(
    val sessionId: String? = null,
    val turnCount: Int? = null,
    val totalCostUsd: Double? = null,
)

@Serializable
data class BotStatus(
    val id: String,
    val name: String? = null,
    val description: String? = null,
    val avatar: String? = null,
    val color: String? = null,
    val workspace: String? = null,
    val harness: String? = null,
    val model: String? = null,
    val modelInherited: Boolean? = null,
    val privateSessions: Boolean? = null,
    val desktopTarget: String? = null,
    val browserMode: String? = null,
    val permissionMode: String? = null,
    val archivedSessions: Int? = null,
    val busy: Boolean? = null,
    val queued: Int? = null,
    val session: SessionRecord? = null,
    val instructions: String? = null,
    val latestStage: StageRef? = null,
) {
    val displayName: String get() = if (name.isNullOrEmpty()) id else name
    val isBusy: Boolean get() = busy == true
}

@Serializable
data class MessageMedia(
    val id: String,
    val kind: String? = null,
    val url: String,
    val mimeType: String? = null,
    val name: String? = null,
    val alt: String? = null,
    val size: Long? = null,
) {
    val isImage: Boolean
        get() = when {
            kind == "image" -> true
            kind == "file" -> false
            else -> mimeType?.startsWith("image/") == true
        }
}

@Serializable
data class ChatMessage(
    val id: String,
    val botId: String? = null,
    val role: String,
    val from: String? = null,
    val to: String? = null,
    val text: String? = null,
    val media: List<MessageMedia>? = null,
    val stage: StageRef? = null,
    val createdAt: String? = null,
    val tools: List<String>? = null,
    val durationMs: Double? = null,
    val costUsd: Double? = null,
) {
    val body: String get() = text ?: ""
}

@Serializable
data class MemoryItem(
    val id: String,
    val path: String? = null,
    val scope: String? = null,
    val botId: String? = null,
    val kind: String? = null,
    val title: String? = null,
    val text: String? = null,
    val snippet: String? = null,
    val why: String? = null,
    val tags: List<String>? = null,
    val status: String? = null,
    val pin: Boolean? = null,
    val confidence: Double? = null,
    val validFrom: String? = null,
    val validUntil: String? = null,
    val supersededBy: String? = null,
    val sourceSession: String? = null,
    val sourceAuthor: String? = null,
    val origin: String? = null,
    val score: Double? = null,
    val createdAt: String? = null,
    val updatedAt: String? = null,
)

@Serializable
data class MemoryMagpieStatus(
    val installed: Boolean? = null,
    val hasLegacyItems: Boolean? = null,
    val legacyCount: Int? = null,
    val injectedHarnesses: List<String>? = null,
)

@Serializable
data class MemoryCounts(
    val active: Int = 0,
    val candidate: Int = 0,
    val superseded: Int = 0,
    val archived: Int = 0,
)

@Serializable
data class MemorySemanticStatus(
    val enabled: Boolean? = null,
    val modelReady: Boolean? = null,
    val indexReady: Boolean? = null,
    val active: Boolean? = null,
    val model: String? = null,
)

@Serializable
data class MemoryList(
    val backend: String? = null,
    val installed: Boolean? = null,
    val available: Boolean? = null,
    val error: String? = null,
    val magpie: MemoryMagpieStatus? = null,
    val semantic: MemorySemanticStatus? = null,
    val counts: MemoryCounts? = null,
    val items: List<MemoryItem>? = null,
)

@Serializable
data class MemoryIndexDoc(
    val ok: Boolean? = null,
    val path: String? = null,
    val content: String = "",
    val lines: Int = 0,
    val maxLines: Int = 0,
)

@Serializable
data class MemoryDreamConfig(
    val enabled: Boolean? = null,
    val dailyAt: String? = null,
    val model: String? = null,
    val minNewMessages: Int? = null,
)

@Serializable
data class MemoryEmbeddingConfig(
    val enabled: Boolean? = null,
    val baseUrl: String? = null,
    val apiKey: String? = null,
    val hasApiKey: Boolean? = null,
    val model: String? = null,
)

@Serializable
data class MemoryConfig(
    val enabled: Boolean? = null,
    val coexistence: String? = null,
    val snapshotMaxChars: Int? = null,
    val disableClaudeAutoMemory: Boolean? = null,
    val dream: MemoryDreamConfig? = null,
    val embedding: MemoryEmbeddingConfig? = null,
)

@Serializable
data class MemoryDreamEntry(
    val botId: String? = null,
    val date: String? = null,
    val path: String? = null,
    val summary: String? = null,
    val updatedAt: String? = null,
)

@Serializable
data class MemoryStatusResp(
    val backend: String? = null,
    val config: MemoryConfig? = null,
    val docCount: Int = 0,
    val chunkCount: Int = 0,
    val counts: MemoryCounts? = null,
    val semantic: MemorySemanticStatus? = null,
    val magpie: MemoryMagpieStatus? = null,
    val recentDreams: List<MemoryDreamEntry>? = null,
)

@Serializable
data class MemorySaveResp(
    val ok: Boolean? = null,
    val id: String? = null,
    val item: MemoryItem? = null,
    val similarExisting: JsonElement? = null,
)

@Serializable
data class MemoryPatchResp(
    val ok: Boolean? = null,
    val item: MemoryItem? = null,
)

@Serializable
data class MemoryReindexResp(
    val ok: Boolean? = null,
    val docsIndexed: Int = 0,
    val chunksIndexed: Int = 0,
)

@Serializable
data class MemoryImportResp(
    val ok: Boolean? = null,
    val imported: Int = 0,
    val skipped: Int = 0,
)

@Serializable
data class MemoryDreamRunResp(
    val ok: Boolean? = null,
    val results: JsonElement? = null,
)

@Serializable
data class PermissionCapabilities(
    val modes: List<String>? = null,
    val interactiveApproval: Boolean? = null,
    val supportsAllowAlways: Boolean? = null,
)

@Serializable
data class HarnessInstallInfo(
    val command: String? = null,
    val url: String? = null,
)

@Serializable
data class HarnessInfo(
    val id: String,
    val label: String? = null,
    val defaultModel: String? = null,
    val installed: Boolean? = null,
    val binaryPath: String? = null,
    val authStatus: String? = null,
    val authDetail: String? = null,
    val requiresLogin: Boolean? = null,
    val canAutoInstall: Boolean? = null,
    val category: String? = null,
    val summary: String? = null,
    val install: HarnessInstallInfo? = null,
    val permissions: PermissionCapabilities? = null,
) {
    val title: String get() = if (label.isNullOrEmpty()) id else label
    val supportedModes: List<String>
        get() = permissions?.modes?.filter { it.isNotEmpty() }?.ifEmpty { listOf("auto") } ?: listOf("auto")
}

@Serializable
data class SetupTaskStatus(
    val target: String,
    val kind: String,
    val state: String,
    val logs: List<String>? = null,
    val error: String? = null,
    val loginUrl: String? = null,
    val authStatus: String? = null,
    val authDetail: String? = null,
)

@Serializable
data class SettingsData(
    val desktopHarness: String? = null,
    val desktopModel: String? = null,
    val defaultHarness: String? = null,
    val defaultPermissionMode: String? = null,
    val defaultModel: String? = null,
    val defaultModels: Map<String, String>? = null,
    val sharedUserMd: String? = null,
    val harnesses: List<HarnessInfo>? = null,
    val memory: MemoryConfig? = null,
)

@Serializable
data class TailcatStatus(
    val state: String? = null,
    val link: String? = null,
    val error: String? = null,
    val hasToken: Boolean? = null,
    val tokenEnvRef: String? = null,
    val apiToken: String? = null,
)

@Serializable
data class TailscalePeer(
    val id: String,
    val hostname: String,
    val dnsName: String? = null,
    val ip: String? = null,
    val os: String? = null,
    val address: String,
    val online: Boolean = false,
    val isBotHub: Boolean = false,
)

@Serializable
data class MobileTailscaleStatus(
    val state: String,
    val authUrl: String? = null,
    val hostname: String? = null,
    val dnsName: String? = null,
    val ip: String? = null,
    val tailnet: String? = null,
    val link: String? = null,
    val error: String? = null,
    val peers: List<TailscalePeer>? = null,
)

@Serializable
data class HostTailscaleStatus(
    val enabled: Boolean? = null,
    val state: String? = null,
    val authUrl: String? = null,
    val hostname: String? = null,
    val dnsName: String? = null,
    val ip: String? = null,
    val tailnet: String? = null,
    val link: String? = null,
    val controlUrl: String? = null,
    val error: String? = null,
    val hasToken: Boolean? = null,
    val tokenEnvRef: String? = null,
    val apiToken: String? = null,
    val authKeyEnvRef: String? = null,
)

@Serializable
data class NodeHarnessInfo(
    val kind: String,
    val label: String? = null,
    val installed: Boolean? = null,
    val authStatus: String? = null,
    val authDetail: String? = null,
    val permissionCapabilities: PermissionCapabilities? = null,
) {
    val title: String get() = if (label.isNullOrEmpty()) kind else label
}

@Serializable
data class NodeWorkspace(
    val id: String,
    val name: String,
    val path: String,
    val isGit: Boolean? = null,
    val defaultBranch: String? = null,
)

@Serializable
data class NodeInfo(
    val id: String,
    val name: String? = null,
    val os: String? = null,
    val arch: String? = null,
    val hostname: String? = null,
    val homeDir: String? = null,
    val permissions: List<String>? = null,
    val protocolVersion: Int? = null,
    val capabilities: List<String>? = null,
    val version: String? = null,
    val connected: Boolean? = null,
    val reconnecting: Boolean? = null,
    val transport: String? = null,
    val lastSeenAt: String? = null,
    val harnesses: List<NodeHarnessInfo>? = null,
    val workspaces: List<NodeWorkspace>? = null,
    val maxPermissionMode: String? = null,
    val activeTaskIds: List<String>? = null,
) {
    val isOutdated: Boolean get() = (protocolVersion ?: 1) < CLIENT_PROTOCOL
}

@Serializable
data class ScreenState(
    val target: String? = null,
    val owner: String? = null,
    val botId: String? = null,
    val botName: String? = null,
)

@Serializable
data class TakeoverRequest(
    val id: String,
    val target: String? = null,
    val botId: String? = null,
    val botName: String? = null,
    val taskId: String? = null,
    val deviceId: String? = null,
    val reason: String? = null,
    val app: String? = null,
    val createdAt: String? = null,
)

@Serializable
data class DesktopTarget(
    val id: String,
    val name: String? = null,
    val local: Boolean? = null,
    val hostId: String? = null,
    val slot: String? = null,
    val os: String? = null,
    val arch: String? = null,
    val supported: Boolean? = null,
    val virtual: Boolean? = null,
    val virtualSupported: Boolean? = null,
    val protocolVersion: Int? = null,
    val outdated: Boolean? = null,
    val powerState: String? = null,
    val backend: String? = null,
    val screen: ScreenState? = null,
)

@Serializable
data class DesktopFrame(
    val jpegBase64: String = "",
    val imageWidth: Double? = null,
    val imageHeight: Double? = null,
    val logicalX: Double? = null,
    val logicalY: Double? = null,
    val logicalWidth: Double? = null,
    val logicalHeight: Double? = null,
)

@Serializable
data class DesktopStatus(
    val screenRecording: String? = null,
    val accessibility: String? = null,
)

@Serializable
data class DesktopState(
    val screen: ScreenState? = null,
    val takeovers: List<TakeoverRequest>? = null,
    val status: DesktopStatus? = null,
)

@Serializable
data class FallbackHost(
    val label: String? = null,
    val address: String,
    val token: String? = null,
)

@Serializable
data class ServerProfile(
    val id: String,
    val label: String,
    val address: String,
    val token: String = "",
    val standbyAddress: String? = null,
    val standbyToken: String? = null,
    val fallbackHosts: List<FallbackHost>? = null,
)

@Serializable
data class RoutineRun(
    val id: String,
    val routineId: String? = null,
    val routineName: String? = null,
    val trigger: String? = null,
    val status: String? = null,
    val delivery: String? = null,
    val model: String? = null,
    val output: String? = null,
    val error: String? = null,
    val durationMs: Double? = null,
    val costUsd: Double? = null,
    val startedAt: String? = null,
    val finishedAt: String? = null,
)

@Serializable
data class RoutineItem(
    val id: String,
    val name: String? = null,
    val everyMinutes: Int? = null,
    val dailyAt: String? = null,
    val cron: String? = null,
    val webhookTrigger: Boolean? = null,
    val webhookPath: String? = null,
    val webhookSecretSet: Boolean? = null,
    val webhookUrl: String? = null,
    val prompt: String? = null,
    val model: String? = null,
    val mainDelivery: String? = null,
    val permissionMode: String? = null,
    val enabled: Boolean? = null,
    val running: Boolean? = null,
    val nextRunAt: String? = null,
    val lastRun: RoutineRun? = null,
)

@Serializable
data class QuestionOption(
    val value: String,
    val label: String? = null,
    val description: String? = null,
)

@Serializable
data class QuestionField(
    val key: String,
    val title: String? = null,
    val description: String? = null,
    val type: String,
    val required: Boolean? = null,
    val placeholder: String? = null,
    @SerialName("default")
    val defaultValue: JsonElement? = null,
    val options: List<QuestionOption>? = null,
    val custom: Boolean? = null,
)

@Serializable
data class PendingQuestion(
    val id: String,
    val botId: String,
    val botName: String? = null,
    val taskId: String? = null,
    val deviceId: String? = null,
    val title: String? = null,
    val fields: List<QuestionField> = emptyList(),
    val createdAt: String? = null,
)

@Serializable
data class ActiveDesktopTarget(
    val target: String,
    val name: String? = null,
    val temporary: Boolean = false,
    val expiresAt: String? = null,
)

@Serializable
data class SlashCommandItem(
    val name: String,
    val description: String? = null,
    val argumentHint: String? = null,
    val source: String? = null,
) {
    val id: String get() = "${source ?: "h"}:$name"
}

@Serializable
data class MentionCandidate(
    val kind: String,
    val value: String,
    val label: String,
    val description: String? = null,
) {
    val id: String get() = "$kind:$value"
}

@Serializable
data class InboundAttachmentResponse(
    val media: MessageMedia,
    val workspacePath: String? = null,
)

data class PendingAttachment(
    val id: String,
    val name: String,
    val isImage: Boolean,
    val media: MessageMedia? = null,
    val previewBytes: ByteArray? = null,
    val uploading: Boolean = true,
    val error: String? = null,
)

@Serializable
data class DesktopPermissionItem(
    val id: String,
    val name: String,
    val description: String,
    val status: String,
    val canPrompt: Boolean? = null,
    val canOpenSettings: Boolean? = null,
)

@Serializable
data class DesktopPermissionsResp(
    val target: String? = null,
    val platform: String? = null,
    val items: List<DesktopPermissionItem>? = null,
)

@Serializable
data class DesktopPermissionRequestResp(
    val ok: Boolean? = null,
    val prompted: Boolean? = null,
    val openedSettings: Boolean? = null,
    val item: DesktopPermissionItem? = null,
    val items: List<DesktopPermissionItem>? = null,
)

@Serializable
data class PermissionOption(
    val id: String,
    val kind: String,
    val label: String,
    val description: String? = null,
)

@Serializable
data class PendingApproval(
    val id: String,
    val botId: String,
    val botName: String? = null,
    val taskId: String? = null,
    val deviceId: String? = null,
    val toolName: String,
    val kind: String? = null,
    val title: String? = null,
    val summary: String? = null,
    val rawInput: JsonObject? = null,
    val options: List<PermissionOption>? = null,
    val mode: String? = null,
    val createdAt: String? = null,
)

@Serializable
data class BotTask(
    val id: String,
    val botId: String,
    val kind: String,
    val title: String? = null,
    val prompt: String = "",
    val status: String = "queued",
    val deviceId: String? = null,
    val harness: String? = null,
    val model: String? = null,
    val permissionMode: String? = null,
    val workdir: String? = null,
    val workspaceId: String? = null,
    val isolation: String? = null,
    val branch: String? = null,
    val worktreePath: String? = null,
    val checkpointRef: String? = null,
    val delivery: String? = null,
    val dependsOn: List<String>? = null,
    val parentTaskId: String? = null,
    val routineId: String? = null,
    val routineName: String? = null,
    val trigger: String? = null,
    val slotKey: String? = null,
    val runtimeSessionId: String? = null,
    val output: String? = null,
    val error: String? = null,
    val tools: List<String>? = null,
    val durationMs: Double? = null,
    val costUsd: Double? = null,
    val inputTokens: Int? = null,
    val outputTokens: Int? = null,
    val createdAt: String? = null,
    val startedAt: String? = null,
    val finishedAt: String? = null,
    val injectedAt: String? = null,
) {
    val displayTitle: String
        get() {
            val t = title.orEmpty().trim()
            if (t.isNotEmpty()) return t
            val rn = routineName.orEmpty().trim()
            if (rn.isNotEmpty()) return rn
            val line = prompt.lineSequence().firstOrNull().orEmpty().trim()
            return if (line.length > 64) line.take(64) + "…" else line
        }

    val isTerminal: Boolean
        get() = status == "completed" || status == "failed" || status == "cancelled"

    val isWaiting: Boolean
        get() = status == "waiting_approval" || status == "waiting_question" || status == "waiting_takeover"

    val isActive: Boolean
        get() = !isTerminal
}

@Serializable
data class BotTaskEvent(
    val seq: Long,
    val taskId: String,
    val botId: String,
    val eventType: String,
    val payload: JsonElement? = null,
    val createdAt: String? = null,
)

// Response wrappers
@Serializable data class BotsResp(val bots: List<BotStatus>? = null, val server: ServerInfo? = null)
@Serializable data class MessagesResp(val messages: List<ChatMessage>? = null)
@Serializable data class NodesResp(val nodes: List<NodeInfo>? = null)
@Serializable data class TakeoversResp(val ok: Boolean? = null, val items: List<TakeoverRequest>? = null)
@Serializable data class QuestionsResp(val ok: Boolean? = null, val items: List<PendingQuestion>? = null)
@Serializable data class ApprovalsResp(val ok: Boolean? = null, val items: List<PendingApproval>? = null)
@Serializable data class RoutinesResp(val routines: List<RoutineItem>? = null)
@Serializable data class RunsResp(val runs: List<RoutineRun>? = null)
@Serializable data class TasksResp(val tasks: List<BotTask>? = null)
@Serializable data class TaskEventsResp(val events: List<BotTaskEvent>? = null)

@Serializable
data class TaskDiffFile(
    val path: String,
    val status: String? = null,
    val additions: Int? = null,
    val deletions: Int? = null,
)

@Serializable
data class TaskDiffResult(
    val taskId: String,
    val branch: String? = null,
    val worktreePath: String? = null,
    val checkpointRef: String? = null,
    val files: List<TaskDiffFile> = emptyList(),
    val patch: String = "",
)

@Serializable
data class TaskRollbackResult(
    val taskId: String,
    val rolledBack: Boolean = false,
    val removedWorktree: Boolean? = null,
    val branch: String? = null,
    val worktreePath: String? = null,
)
@Serializable data class ModelsResp(val models: List<String>? = null)
@Serializable data class TextResp(val text: String? = null)
@Serializable data class InterruptResp(val interrupted: Boolean? = null)
@Serializable data class OkResp(val ok: Boolean? = null, val output: String? = null, val error: String? = null)
@Serializable data class TicketResp(val ticket: String? = null)
@Serializable data class TargetsResp(
    val targets: List<DesktopTarget>? = null,
    val activeTarget: ActiveDesktopTarget? = null,
    val takeovers: List<TakeoverRequest>? = null,
)
@Serializable data class ActiveTargetResp(val activeTarget: ActiveDesktopTarget? = null)
@Serializable data class CommandsResp(val commands: List<SlashCommandItem>? = null)
@Serializable data class MentionsResp(val items: List<MentionCandidate>? = null)

@Serializable
data class TunnelStartResp(
    val url: String? = null,
    val token: String? = null,
    val error: String? = null,
)

enum class StageKind(val raw: String) {
    NEW("new"),
    FILES("files"),
    FILE("file"),
    DESKTOP("desktop"),
    TASK("task"),
    BROWSER("browser"),
    ARTIFACT("artifact"),
    MCP_APP("mcp_app");

    companion object {
        fun fromRaw(raw: String?): StageKind {
            val clean = raw?.trim()?.lowercase().orEmpty()
            return when (clean) {
                "mcp-app", "mcp_app" -> MCP_APP
                else -> entries.firstOrNull { it.raw == clean } ?: FILES
            }
        }
    }
}

@Serializable
data class StageRef(
    val id: String? = null,
    val kind: String = "files",
    val title: String? = null,
    val subtitle: String? = null,
    val path: String? = null,
    val url: String? = null,
    val resourceUri: String? = null,
    val target: String? = null,
    val taskId: String? = null,
    val closable: Boolean? = null,
    val createdAt: String? = null,
) {
    val resolvedKind: StageKind get() = StageKind.fromRaw(kind)

    val resolvedId: String
        get() {
            val explicit = id?.trim().orEmpty()
            if (explicit.isNotEmpty()) return explicit
            return when (resolvedKind) {
                StageKind.FILES -> "files"
                StageKind.FILE -> "file:${path.orEmpty()}"
                StageKind.DESKTOP -> {
                    val dest = target?.trim().orEmpty().ifEmpty { "local" }
                    if (dest == "local") "desktop" else "desktop:$dest"
                }
                StageKind.TASK -> "task:${taskId.orEmpty().ifEmpty { "default" }}"
                StageKind.NEW -> "new"
                StageKind.BROWSER -> "browser:${url.orEmpty().ifEmpty { "default" }}"
                StageKind.ARTIFACT -> "artifact:${path.orEmpty().ifEmpty { title.orEmpty().ifEmpty { "default" } }}"
                StageKind.MCP_APP -> "mcp_app:${resourceUri.orEmpty().ifEmpty { title.orEmpty().ifEmpty { "default" } }}"
            }
        }
}

data class StageTab(
    val id: String,
    val kind: StageKind,
    val rawKind: String,
    val title: String,
    val subtitle: String? = null,
    val botId: String? = null,
    val path: String? = null,
    val url: String? = null,
    val resourceUri: String? = null,
    val target: String? = null,
    val taskId: String? = null,
    val closable: Boolean = true,
) {
    val isDesktop: Boolean get() = kind == StageKind.DESKTOP
}

@Serializable
data class WorkspaceFileEntry(
    val name: String,
    val path: String,
    val isDir: Boolean = false,
    val isShared: Boolean? = null,
    val size: Long = 0L,
    val modTime: String? = null,
    val mimeType: String? = null,
) {
    val id: String get() = path
}

@Serializable
data class WorkspaceListResult(
    val botId: String = "",
    val path: String = "",
    val parent: String? = null,
    val root: String? = null,
    val truncated: Boolean? = null,
    val entries: List<WorkspaceFileEntry> = emptyList(),
)

@Serializable
data class WorkspaceFileContent(
    val botId: String = "",
    val path: String = "",
    val name: String = "",
    val size: Long = 0L,
    val modTime: String? = null,
    val mimeType: String = "application/octet-stream",
    val kind: String = "binary", // "text" | "markdown" | "image" | "binary"
    val content: String? = null,
    val truncated: Boolean? = null,
)

@Serializable
data class WorkspaceFileTicketResp(
    val ticket: String = "",
    val expiresIn: Int? = null,
)

@Serializable
data class StageOpenEvent(
    val id: String? = null,
    val kind: String = "files",
    val title: String? = null,
    val subtitle: String? = null,
    val botId: String? = null,
    val path: String? = null,
    val url: String? = null,
    val resourceUri: String? = null,
    val target: String? = null,
    val taskId: String? = null,
    val closable: Boolean? = null,
    val autoOpen: Boolean? = null,
    val createdAt: String? = null,
)

data class PendingChatInsert(
    val seq: Int,
    val text: String,
)

@Serializable
data class UpdateConfig(
    val autoCheck: Boolean = true,
    val interval: String = "6h",
    val channel: String = "stable",
    val proxy: String? = null,
)

@Serializable
data class UpdateReleaseInfo(
    val version: String = "",
    val tag: String = "",
    val name: String? = null,
    val url: String = "",
    val notes: String? = null,
    val publishedAt: String? = null,
    val prerelease: Boolean? = null,
)

@Serializable
data class UpdateStatus(
    val currentVersion: String = "",
    val state: String = "idle",
    val hasUpdate: Boolean = false,
    val latest: UpdateReleaseInfo? = null,
    val checkedAt: String? = null,
    val error: String? = null,
    val errorCode: String? = null,
    val config: UpdateConfig = UpdateConfig(),
)

fun JsonElement?.asDisplayString(): String? {
    val p = this as? JsonPrimitive ?: return null
    if (p is JsonNull) return null
    p.booleanOrNull?.let { return if (it) "true" else "false" }
    p.intOrNull?.let { return it.toString() }
    p.doubleOrNull?.let { d ->
        return if (d % 1.0 == 0.0) d.toLong().toString() else d.toString()
    }
    return p.contentOrNull
}

fun JsonElement?.asBoolValue(): Boolean? = (this as? JsonPrimitive)?.booleanOrNull

fun JsonElement?.asStringList(): List<String>? {
    val arr = this as? JsonArray ?: return null
    return arr.mapNotNull { it.asDisplayString() }
}

fun anyToJsonElement(value: Any?): JsonElement = when (value) {
    null -> JsonNull
    is JsonElement -> value
    is Boolean -> JsonPrimitive(value)
    is Number -> JsonPrimitive(value)
    is String -> JsonPrimitive(value)
    is Map<*, *> -> JsonObject(value.entries.associate { (k, v) -> k.toString() to anyToJsonElement(v) })
    is Iterable<*> -> JsonArray(value.map { anyToJsonElement(it) })
    is Array<*> -> JsonArray(value.map { anyToJsonElement(it) })
    else -> JsonPrimitive(value.toString())
}

fun mapToJsonObject(map: Map<String, Any?>): JsonObject =
    JsonObject(map.mapValues { (_, v) -> anyToJsonElement(v) })
