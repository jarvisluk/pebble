import Foundation

// 与 apps/mobile/src/types.ts 对齐；服务端字段可能缺省，统一用可选值宽松解码。

enum ClientProtocol {
    static let current = 2
}

struct ServerInfo: Codable, Equatable {
    var protocolVersion: Int
    var minClientProtocol: Int?
    var minNodeProtocol: Int?
    var version: String?
    var capabilities: [String]?
}

struct SessionRecord: Codable, Hashable {
    var sessionId: String?
    var turnCount: Int?
    var totalCostUsd: Double?
}

struct BotStatus: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
    var description: String?
    var avatar: String?
    var color: String?
    var workspace: String?
    var harness: String?
    var model: String?
    var modelInherited: Bool?
    var privateSessions: Bool?
    var desktopTarget: String?
    var browserMode: String?
    var permissionMode: String?
    var archivedSessions: Int?
    var busy: Bool?
    var queued: Int?
    var session: SessionRecord?
    var instructions: String?
    var latestStage: StageRef?

    var displayName: String { (name ?? "").isEmpty ? id : name! }
    var isBusy: Bool { busy ?? false }
}

struct MessageMedia: Codable, Identifiable, Hashable {
    var id: String
    var kind: String?
    var url: String
    var mimeType: String?
    var name: String?
    var alt: String?
    var size: Int64?

    var isImage: Bool {
        if kind == "image" { return true }
        if kind == "file" { return false }
        return mimeType?.hasPrefix("image/") ?? false
    }
}

struct ChatMessage: Codable, Identifiable, Hashable {
    var id: String
    var botId: String?
    var role: String
    var from: String?
    var to: String?
    var text: String?
    var media: [MessageMedia]?
    var stage: StageRef?
    var createdAt: String?
    var tools: [String]?
    var durationMs: Double?
    var costUsd: Double?

    var body: String { text ?? "" }
}

struct MemoryItem: Codable, Identifiable, Hashable {
    var id: String
    var kind: String?
    var scope: String?
    var tags: [String]?
    var updatedAt: String?
    var text: String?
}

struct MemorySemanticStatus: Codable, Hashable {
    var enabled: Bool?
    var modelReady: Bool?
    var indexReady: Bool?
    var active: Bool?
    var model: String?
}

struct MemoryList: Codable {
    var installed: Bool?
    var available: Bool?
    var error: String?
    var semantic: MemorySemanticStatus?
    var items: [MemoryItem]?
}

struct HarnessMagpie: Codable, Hashable {
    var installed: Bool?
    var connected: Bool?
    var model: String?
    var nativeModel: String?
}

struct PermissionCapabilities: Codable, Hashable {
    var modes: [String]?
    var interactiveApproval: Bool?
    var supportsAllowAlways: Bool?
}

struct HarnessInstallInfo: Codable, Hashable {
    var command: String?
    var url: String?
}

struct HarnessInfo: Codable, Identifiable, Hashable {
    var id: String
    var label: String?
    var defaultModel: String?
    var installed: Bool?
    var binaryPath: String?
    var authStatus: String?
    var authDetail: String?
    var requiresLogin: Bool?
    var canAutoInstall: Bool?
    var category: String?
    var summary: String?
    var install: HarnessInstallInfo?
    var magpie: HarnessMagpie?
    var permissions: PermissionCapabilities?

    var title: String { (label ?? "").isEmpty ? id : label! }
    var supportedModes: [String] {
        let list = (permissions?.modes ?? []).filter { !$0.isEmpty }
        return list.isEmpty ? ["auto"] : list
    }
}

struct MagpieProvider: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
    var endpoint: String?
    var status: String?
    var modelsInfo: String?
    var usedBy: String?
    var enabled: Bool?
}

struct MagpiePreset: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
}

struct MagpieInfo: Codable {
    var installed: Bool?
    var healthy: Bool?
    var providers: [MagpieProvider]?
    var presets: [MagpiePreset]?
    var webUrl: String?
}

struct SetupTaskStatus: Codable {
    var target: String
    var kind: String
    var state: String
    var logs: [String]?
    var error: String?
    var loginUrl: String?
    var authStatus: String?
    var authDetail: String?
}

struct OnboardingInfo: Codable {
    var completed: Bool?
    var needsSetup: Bool?
    var installedHarnesses: [String]?
}

struct SettingsData: Codable {
    var magpie: MagpieInfo?
    var onboarding: OnboardingInfo?
    var desktopHarness: String?
    var desktopModel: String?
    var defaultHarness: String?
    var defaultPermissionMode: String?
    var defaultModel: String?
    var defaultModels: [String: String]?
    var sharedUserMd: String?
    var harnesses: [HarnessInfo]?
}

struct TailcatStatus: Codable {
    var state: String?
    var link: String?
    var error: String?
    var hasToken: Bool?
    var tokenEnvRef: String?
    var apiToken: String?
}

struct TailscalePeer: Codable, Identifiable, Hashable {
    var id: String
    var hostname: String
    var dnsName: String?
    var ip: String?
    var os: String?
    var address: String
    var online: Bool
    var isBotHub: Bool
}

struct MobileTailscaleStatus: Codable {
    var state: String
    var authUrl: String?
    var hostname: String?
    var dnsName: String?
    var ip: String?
    var tailnet: String?
    var link: String?
    var error: String?
    var peers: [TailscalePeer]?
}

struct HostTailscaleStatus: Codable {
    var enabled: Bool?
    var state: String?
    var authUrl: String?
    var hostname: String?
    var dnsName: String?
    var ip: String?
    var tailnet: String?
    var link: String?
    var controlUrl: String?
    var error: String?
    var hasToken: Bool?
    var tokenEnvRef: String?
    var apiToken: String?
    var authKeyEnvRef: String?
}

struct NodeInfo: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
    var os: String?
    var arch: String?
    var hostname: String?
    var homeDir: String?
    var permissions: [String]?
    var protocolVersion: Int?
    var capabilities: [String]?
    var version: String?
    var connected: Bool?
    var lastSeenAt: String?

    var isOutdated: Bool { (protocolVersion ?? 1) < ClientProtocol.current }
}

struct ScreenState: Codable, Hashable {
    var target: String?
    var owner: String?
    var botId: String?
    var botName: String?
}

struct TakeoverRequest: Codable, Identifiable, Hashable {
    var id: String
    var target: String?
    var botId: String?
    var botName: String?
    var reason: String?
    var app: String?
    var createdAt: String?
}

struct DesktopTarget: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
    var local: Bool?
    var hostId: String?
    var slot: String?
    var os: String?
    var arch: String?
    var supported: Bool?
    var virtual: Bool?
    var virtualSupported: Bool?
    var protocolVersion: Int?
    var outdated: Bool?
    var powerState: String?
    var backend: String?
    var screen: ScreenState?
}

struct DesktopFrame: Codable {
    var jpegBase64: String
    var imageWidth: Double?
    var imageHeight: Double?
    var logicalX: Double?
    var logicalY: Double?
    var logicalWidth: Double?
    var logicalHeight: Double?
}

struct DesktopStatus: Codable {
    var screenRecording: String?
    var accessibility: String?
}

struct DesktopState: Codable {
    var screen: ScreenState?
    var takeovers: [TakeoverRequest]?
    var status: DesktopStatus?
}

struct FallbackHost: Codable, Hashable {
    var label: String?
    var address: String
    var token: String?
}

struct ServerProfile: Codable, Identifiable, Hashable {
    var id: String
    var label: String
    var address: String
    var token: String
    var standbyAddress: String?
    var standbyToken: String?
    var fallbackHosts: [FallbackHost]?
}

struct RoutineRun: Codable, Identifiable, Hashable {
    var id: String
    var routineId: String?
    var routineName: String?
    var trigger: String?
    var status: String?
    var delivery: String?
    var model: String?
    var output: String?
    var error: String?
    var durationMs: Double?
    var costUsd: Double?
    var startedAt: String?
    var finishedAt: String?
}

struct RoutineItem: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
    var everyMinutes: Int?
    var dailyAt: String?
    var cron: String?
    var webhookTrigger: Bool?
    var webhookPath: String?
    var webhookSecretSet: Bool?
    var webhookUrl: String?
    var prompt: String?
    var model: String?
    var mainDelivery: String?
    var permissionMode: String?
    var enabled: Bool?
    var running: Bool?
    var nextRunAt: String?
    var lastRun: RoutineRun?
}

// 接口外层包装
struct BotsResp: Decodable {
    var bots: [BotStatus]?
    var server: ServerInfo?
}
struct MessagesResp: Decodable { var messages: [ChatMessage]? }
struct NodesResp: Decodable { var nodes: [NodeInfo]? }
struct TakeoversResp: Decodable { var ok: Bool?; var items: [TakeoverRequest]? }

enum JSONValue: Codable, Hashable {
    case string(String)
    case number(Double)
    case bool(Bool)
    case array([JSONValue])
    case object([String: JSONValue])
    case null

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let b = try? c.decode(Bool.self) { self = .bool(b) }
        else if let n = try? c.decode(Double.self) { self = .number(n) }
        else if let s = try? c.decode(String.self) { self = .string(s) }
        else if let a = try? c.decode([JSONValue].self) { self = .array(a) }
        else if let o = try? c.decode([String: JSONValue].self) { self = .object(o) }
        else { self = .null }
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .string(let s): try c.encode(s)
        case .number(let n): try c.encode(n)
        case .bool(let b): try c.encode(b)
        case .array(let a): try c.encode(a)
        case .object(let o): try c.encode(o)
        case .null: try c.encodeNil()
        }
    }

    var stringValue: String? {
        switch self {
        case .string(let s): return s
        case .number(let n): return n.truncatingRemainder(dividingBy: 1) == 0 ? String(Int(n)) : String(n)
        case .bool(let b): return b ? "true" : "false"
        default: return nil
        }
    }

    var boolValue: Bool? {
        if case .bool(let b) = self { return b }
        return nil
    }

    var stringArrayValue: [String]? {
        if case .array(let a) = self { return a.compactMap(\.stringValue) }
        return nil
    }
}

struct QuestionOption: Codable, Hashable {
    var value: String
    var label: String?
    var description: String?
}

struct QuestionField: Codable, Hashable {
    var key: String
    var title: String?
    var description: String?
    var type: String
    var required: Bool?
    var placeholder: String?
    var `default`: JSONValue?
    var options: [QuestionOption]?
    var custom: Bool?
}

struct PendingQuestion: Codable, Identifiable, Hashable {
    var id: String
    var botId: String
    var botName: String?
    var title: String?
    var fields: [QuestionField]
    var createdAt: String?
}

struct QuestionsResp: Decodable { var ok: Bool?; var items: [PendingQuestion]? }

struct PermissionOption: Codable, Identifiable, Hashable {
    var id: String
    var kind: String
    var label: String
    var description: String?
}

struct PendingApproval: Codable, Identifiable, Hashable {
    var id: String
    var botId: String
    var botName: String?
    var toolName: String
    var kind: String?
    var title: String?
    var summary: String?
    var rawInput: [String: JSONValue]?
    var options: [PermissionOption]?
    var mode: String?
    var createdAt: String?

    var rawInputPretty: String? {
        guard let rawInput, !rawInput.isEmpty else { return nil }
        let enc = JSONEncoder()
        enc.outputFormatting = [.prettyPrinted, .sortedKeys]
        guard let data = try? enc.encode(rawInput) else { return nil }
        return String(data: data, encoding: .utf8)
    }
}

struct ApprovalsResp: Decodable { var ok: Bool?; var items: [PendingApproval]? }
struct RoutinesResp: Decodable { var routines: [RoutineItem]? }
struct RunsResp: Decodable { var runs: [RoutineRun]? }
struct ModelsResp: Decodable { var models: [String]? }
struct TextResp: Decodable { var text: String? }
struct InterruptResp: Decodable { var interrupted: Bool? }
struct OkResp: Decodable { var ok: Bool?; var output: String?; var error: String? }
struct ActiveDesktopTarget: Codable, Equatable {
    var target: String
    var name: String?
    var temporary: Bool
    var expiresAt: String?
}

struct TargetsResp: Decodable {
    var targets: [DesktopTarget]?
    var activeTarget: ActiveDesktopTarget?
    var takeovers: [TakeoverRequest]?
}

struct ActiveTargetResp: Decodable {
    var activeTarget: ActiveDesktopTarget?
}
struct Empty: Decodable {}

struct SlashCommandItem: Codable, Hashable, Identifiable {
    var name: String
    var description: String?
    var argumentHint: String?
    var source: String?
    var id: String { "\(source ?? "h"):\(name)" }
}

struct MentionCandidate: Codable, Hashable, Identifiable {
    var kind: String
    var value: String
    var label: String
    var description: String?
    var id: String { "\(kind):\(value)" }
}

struct InboundAttachmentResponse: Codable {
    var media: MessageMedia
    var workspacePath: String?
}

struct PendingAttachment: Identifiable {
    var id: String
    var name: String
    var isImage: Bool
    var media: MessageMedia?
    var imageData: Data?
    var uploading = true
    var error: String?
}

struct CommandsResp: Decodable { var commands: [SlashCommandItem]? }
struct MentionsResp: Decodable { var items: [MentionCandidate]? }

struct DesktopPermissionItem: Decodable, Identifiable, Hashable {
    var id: String
    var name: String
    var description: String
    var status: String
    var canPrompt: Bool?
    var canOpenSettings: Bool?
}

struct DesktopPermissionsResp: Decodable {
    var target: String?
    var platform: String?
    var items: [DesktopPermissionItem]?
}

struct DesktopPermissionRequestResp: Decodable {
    var ok: Bool?
    var prompted: Bool?
    var openedSettings: Bool?
    var item: DesktopPermissionItem?
    var items: [DesktopPermissionItem]?
}

func decodeAny<T: Decodable>(_ obj: Any, as: T.Type = T.self) -> T? {
    guard JSONSerialization.isValidJSONObject(obj),
          let data = try? JSONSerialization.data(withJSONObject: obj) else { return nil }
    return try? JSONDecoder().decode(T.self, from: data)
}

struct WorkspaceFileEntry: Decodable, Identifiable, Hashable {
    var name: String
    var path: String
    var isDir: Bool
    var isSymlink: Bool?
    var isShared: Bool?
    var size: Int64
    var modTime: String
    var mimeType: String?
    var id: String { path }
}

struct WorkspaceListResult: Decodable {
    var botId: String
    var path: String
    var parent: String
    var entries: [WorkspaceFileEntry]
    var truncated: Bool?
}

struct WorkspaceFileContent: Decodable {
    var botId: String
    var path: String
    var name: String
    var size: Int64
    var modTime: String
    var mimeType: String
    var kind: String // text | markdown | image | binary
    var content: String?
    var truncated: Bool?
}

struct WorkspaceFileTicketResp: Decodable {
    var ticket: String
    var path: String
    var expiresIn: Int
}

enum StageKind: String, Codable, Hashable {
    case new
    case files
    case file
    case desktop
    case browser
    case artifact
    case mcpApp = "mcp-app"

    init(rawOrUnknown raw: String?) {
        let clean = (raw ?? "").trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        self = StageKind(rawValue: clean) ?? .artifact
    }
}

struct StageRef: Codable, Hashable, Identifiable {
    var id: String?
    var kind: String
    var title: String?
    var subtitle: String?
    var path: String?
    var url: String?
    var resourceUri: String?
    var target: String?
    var closable: Bool?
    var createdAt: String?

    var resolvedId: String {
        if let id, !id.isEmpty { return id }
        if let path, !path.isEmpty { return "\(kind):\(path)" }
        if let target, !target.isEmpty { return "\(kind):\(target)" }
        return kind
    }

    var resolvedKind: StageKind {
        if kind == "files", let path, !path.isEmpty { return .file }
        return StageKind(rawOrUnknown: kind)
    }
}

struct StageTab: Identifiable, Hashable {
    var id: String
    var kind: StageKind
    var rawKind: String
    var title: String
    var subtitle: String?
    var botId: String?
    var path: String?
    var url: String?
    var resourceUri: String?
    var target: String?
    var closable: Bool = true
}

struct StageOpenEvent: Decodable, Equatable {
    var id: String?
    var kind: String
    var title: String?
    var subtitle: String?
    var botId: String?
    var path: String?
    var url: String?
    var resourceUri: String?
    var target: String?
    var closable: Bool?
    var autoOpen: Bool?
    var createdAt: String?
}

struct PendingChatInsert: Equatable {
    var seq: Int
    var text: String
}

struct UpdateConfig: Codable, Equatable {
    var autoCheck: Bool
    var interval: String
    var channel: String
    var proxy: String?
}

struct UpdateReleaseInfo: Codable, Equatable {
    var version: String
    var tag: String
    var name: String?
    var url: String
    var notes: String?
    var publishedAt: String?
    var prerelease: Bool?
}

struct UpdateStatus: Codable, Equatable {
    var currentVersion: String
    var state: String
    var hasUpdate: Bool
    var latest: UpdateReleaseInfo?
    var checkedAt: String?
    var error: String?
    var errorCode: String?
    var config: UpdateConfig
}
