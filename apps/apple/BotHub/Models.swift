import Foundation

// 与 apps/mobile/src/types.ts 对齐；服务端字段可能缺省，统一用可选值宽松解码。

struct SessionRecord: Codable, Hashable {
    var sessionId: String?
    var turnCount: Int?
    var totalCostUsd: Double?
}

struct BotStatus: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
    var description: String?
    var workspace: String?
    var harness: String?
    var model: String?
    var modelInherited: Bool?
    var privateSessions: Bool?
    var archivedSessions: Int?
    var busy: Bool?
    var queued: Int?
    var session: SessionRecord?
    var instructions: String?

    var displayName: String { (name ?? "").isEmpty ? id : name! }
    var isBusy: Bool { busy ?? false }
}

struct ChatMessage: Codable, Identifiable, Hashable {
    var id: String
    var botId: String?
    var role: String
    var from: String?
    var to: String?
    var text: String?
    var createdAt: String?
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

struct MemoryList: Codable {
    var installed: Bool?
    var available: Bool?
    var error: String?
    var items: [MemoryItem]?
}

struct HarnessMagpie: Codable, Hashable {
    var installed: Bool?
    var connected: Bool?
    var model: String?
    var nativeModel: String?
}

struct HarnessInfo: Codable, Identifiable, Hashable {
    var id: String
    var label: String?
    var defaultModel: String?
    var magpie: HarnessMagpie?

    var title: String { (label ?? "").isEmpty ? id : label! }
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

struct SettingsData: Codable {
    var magpie: MagpieInfo?
    var desktopHarness: String?
    var desktopModel: String?
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
}

struct NodeInfo: Codable, Identifiable, Hashable {
    var id: String
    var name: String?
    var os: String?
    var arch: String?
    var hostname: String?
    var homeDir: String?
    var permissions: [String]?
    var connected: Bool?
    var lastSeenAt: String?
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
    var os: String?
    var arch: String?
    var supported: Bool?
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

struct ServerProfile: Codable, Identifiable, Hashable {
    var id: String
    var label: String
    var address: String
    var token: String
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
    var enabled: Bool?
    var running: Bool?
    var nextRunAt: String?
    var lastRun: RoutineRun?
}

// 接口外层包装
struct BotsResp: Decodable { var bots: [BotStatus]? }
struct MessagesResp: Decodable { var messages: [ChatMessage]? }
struct NodesResp: Decodable { var nodes: [NodeInfo]? }
struct TakeoversResp: Decodable { var ok: Bool?; var items: [TakeoverRequest]? }
struct RoutinesResp: Decodable { var routines: [RoutineItem]? }
struct RunsResp: Decodable { var runs: [RoutineRun]? }
struct ModelsResp: Decodable { var models: [String]? }
struct TextResp: Decodable { var text: String? }
struct InterruptResp: Decodable { var interrupted: Bool? }
struct OkResp: Decodable { var ok: Bool?; var output: String?; var error: String? }
struct TargetsResp: Decodable { var targets: [DesktopTarget]?; var takeovers: [TakeoverRequest]? }
struct Empty: Decodable {}

func decodeAny<T: Decodable>(_ obj: Any, as: T.Type = T.self) -> T? {
    guard JSONSerialization.isValidJSONObject(obj),
          let data = try? JSONSerialization.data(withJSONObject: obj) else { return nil }
    return try? JSONDecoder().decode(T.self, from: data)
}
