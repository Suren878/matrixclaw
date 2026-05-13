import Foundation

public struct VersionInfo: Codable, Equatable, Sendable {
    public var version: String
    public var commit: String?
    public var date: String?
}

public struct HealthResponse: Codable, Equatable, Sendable {
    public var ok: Bool
    public var version: VersionInfo
}

public struct ErrorResponse: Codable, Equatable, Sendable {
    public var error: String
}

public enum SessionStatus: String, Codable, Sendable {
    case active
    case archived
}

public enum SessionRuntime: String, Codable, Sendable {
    case matrixclaw
    case codex
}

public enum PermissionMode: String, Codable, Sendable {
    case `default`
    case acceptEdits = "accept_edits"
    case fullAuto = "full_auto"
}

public struct Session: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var title: String
    public var kind: String?
    public var runtimeId: SessionRuntime?
    public var workingDir: String?
    public var providerId: String?
    public var modelId: String?
    public var permissionMode: PermissionMode?
    public var status: SessionStatus
    public var createdAt: Date
    public var updatedAt: Date
}

public struct ClientBinding: Codable, Equatable, Sendable {
    public var client: String
    public var externalKey: String
    public var sessionId: String
    public var updatedAt: Date
}

public enum MessageRole: String, Codable, Sendable {
    case user
    case assistant
    case system
    case tool
}

public struct Message: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var sessionId: String
    public var runId: String
    public var role: MessageRole
    public var content: String
    public var parts: [MessagePart]?
    public var model: String?
    public var provider: String?
    public var createdAt: Date
    public var updatedAt: Date
}

public enum MessagePartKind: String, Codable, Sendable {
    case text
    case image
    case reasoning
    case toolCall = "tool_call"
    case toolResult = "tool_result"
    case finish
}

public struct MessagePart: Codable, Equatable, Sendable {
    public var kind: MessagePartKind
    public var text: TextPart?
    public var image: ImagePart?
    public var reasoning: ReasoningPart?
    public var toolCall: ToolCallPart?
    public var toolResult: ToolResultPart?
    public var finish: FinishPart?

    public static func text(_ value: String) -> MessagePart {
        MessagePart(kind: .text, text: TextPart(text: value))
    }

    public static func image(
        mimeType: String,
        dataBase64: String,
        name: String? = nil,
        size: Int64? = nil
    ) -> MessagePart {
        MessagePart(
            kind: .image,
            image: ImagePart(mimeType: mimeType, dataBase64: dataBase64, name: name, size: size)
        )
    }

    public static func storedImage(
        storagePath: String,
        mimeType: String? = nil,
        name: String? = nil,
        temporary: Bool = true,
        size: Int64? = nil
    ) -> MessagePart {
        MessagePart(
            kind: .image,
            image: ImagePart(
                mimeType: mimeType,
                name: name,
                storagePath: storagePath,
                temporary: temporary,
                size: size
            )
        )
    }

    public init(
        kind: MessagePartKind,
        text: TextPart? = nil,
        image: ImagePart? = nil,
        reasoning: ReasoningPart? = nil,
        toolCall: ToolCallPart? = nil,
        toolResult: ToolResultPart? = nil,
        finish: FinishPart? = nil
    ) {
        self.kind = kind
        self.text = text
        self.image = image
        self.reasoning = reasoning
        self.toolCall = toolCall
        self.toolResult = toolResult
        self.finish = finish
    }
}

public struct TextPart: Codable, Equatable, Sendable {
    public var text: String

    public init(text: String) {
        self.text = text
    }
}

public struct ImagePart: Codable, Equatable, Sendable {
    public var mimeType: String?
    public var dataBase64: String?
    public var name: String?
    public var storagePath: String?
    public var temporary: Bool?
    public var size: Int64?

    public init(
        mimeType: String? = nil,
        dataBase64: String? = nil,
        name: String? = nil,
        storagePath: String? = nil,
        temporary: Bool? = nil,
        size: Int64? = nil
    ) {
        self.mimeType = mimeType
        self.dataBase64 = dataBase64
        self.name = name
        self.storagePath = storagePath
        self.temporary = temporary
        self.size = size
    }
}

public struct ReasoningPart: Codable, Equatable, Sendable {
    public var text: String
    public var signature: String?
    public var thoughtSignature: String?
    public var toolId: String?
    public var responsesData: JSONValue?
}

public struct ToolCallPart: Codable, Equatable, Sendable {
    public var id: String
    public var name: String
    public var input: String
    public var finished: Bool?
}

public struct ToolResultPart: Codable, Equatable, Sendable {
    public var toolCallId: String
    public var name: String
    public var content: String
    public var mimeType: String?
    public var metadata: JSONValue?
    public var status: String?
    public var isError: Bool?
}

public struct FinishPart: Codable, Equatable, Sendable {
    public var reason: String?
    public var message: String?
    public var details: JSONValue?
}

public enum RunStatus: String, Codable, Sendable {
    case accepted
    case running
    case waitingApproval = "waiting_approval"
    case completed
    case canceled
    case failed
}

public struct Run: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var sessionId: String
    public var userMessageId: String
    public var client: String?
    public var externalKey: String?
    public var status: RunStatus
    public var error: String?
    public var startedAt: Date
    public var finishedAt: Date?
    public var updatedAt: Date
}

public struct RunTiming: Codable, Equatable, Sendable {
    public var totalMs: Int64?
    public var modelMs: Int64?
    public var toolMs: Int64?
    public var approvalMs: Int64?
    public var lastEventAt: Date?
}

public enum ApprovalState: String, Codable, Sendable {
    case pending
    case approved
    case rejected
}

public struct Approval: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var sessionId: String
    public var runId: String?
    public var toolCallId: String?
    public var toolName: String?
    public var description: String?
    public var action: String?
    public var params: JSONValue?
    public var path: String?
    public var state: ApprovalState
    public var requestedAt: Date
    public var decidedAt: Date?
}

public struct PermissionNotification: Codable, Equatable, Sendable {
    public var approvalId: String?
    public var toolCallId: String
    public var granted: Bool?
    public var denied: Bool?
}

public enum ToolLifecycleState: String, Codable, Sendable {
    case requested
    case waitingApproval = "waiting_approval"
    case completed
    case failed
}

public struct ToolUpdate: Codable, Equatable, Sendable {
    public var toolCallId: String
    public var toolName: String
    public var state: ToolLifecycleState
    public var resultStatus: String?
    public var runId: String?
    public var sessionId: String?
    public var approvalId: String?
    public var resultMessageId: String?
    public var error: String?
}

public struct FileSnapshot: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var sessionId: String
    public var path: String
    public var content: String
    public var version: Int
    public var createdAt: Date
    public var updatedAt: Date
}

public struct ClientSnapshot: Codable, Equatable, Sendable {
    public var sessionId: String
    public var session: Session?
    public var context: ContextReport?
    public var messages: [Message]?
    public var run: Run?
    public var timing: RunTiming?
    public var toolUpdates: [ToolUpdate]?
    public var approvals: [Approval]?
    public var approvalNotifications: [PermissionNotification]?
    public var files: [FileSnapshot]?
}

public struct ContextReport: Codable, Equatable, Sendable {
    public var sessionId: String
    public var estimated: Bool
    public var tokenEstimate: Int
    public var windowTokens: Int?
    public var messageCount: Int
    public var blocks: [ContextBlock]
    public var lastProviderUsage: ProviderUsage?
    public var compact: ContextCompact
}

public struct ContextBlock: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var kind: String
    public var source: String
    public var tokenEstimate: Int
    public var included: Bool
    public var truncated: Bool?
    public var cacheStability: String?
}

public struct ProviderUsage: Codable, Equatable, Sendable {
    public var inputTokens: Int64?
    public var outputTokens: Int64?
    public var totalTokens: Int64?
    public var cachedTokens: Int64?
    public var reasoningTokens: Int64?
    public var estimated: Bool?
    public var providerRaw: JSONValue?
}

public struct ContextCompact: Codable, Equatable, Sendable {
    public var recommended: Bool
    public var reason: String?
}

public struct AcceptRunResult: Codable, Equatable, Sendable {
    public var sessionId: String
    public var userMessage: Message
    public var run: Run
}

public struct AcceptRunErrorResponse: Codable, Equatable, Sendable {
    public var error: String
    public var sessionId: String?
    public var userMessage: Message?
    public var run: Run?
}

public struct SessionProviderOption: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var label: String
    public var type: String?
    public var defaultModel: String?
    public var configured: Bool
}

public struct ProviderCapabilities: Codable, Equatable, Sendable {
    public var modelDiscovery: Bool?
    public var reasoningEffort: Bool?
    public var toolCalling: Bool?
    public var normalizeModel: Bool?
}

public struct ProviderSetupItem: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var catalogId: String?
    public var name: String
    public var type: String
    public var status: String
    public var configured: Bool
    public var active: Bool
    public var implemented: Bool
    public var requiresBaseUrl: Bool?
    public var capabilities: ProviderCapabilities?
    public var baseUrl: String?
    public var model: String?
    public var toolUseMode: String?
    public var defaultModel: String?
    public var apiKeyPreview: String?
    public var notes: String?
}

public struct ProviderSetupUpdate: Codable, Equatable, Sendable {
    public var name: String?
    public var type: String?
    public var apiKey: String?
    public var baseUrl: String?
    public var model: String?
    public var toolUseMode: String?
    public var active: Bool?

    public init(
        name: String? = nil,
        type: String? = nil,
        apiKey: String? = nil,
        baseUrl: String? = nil,
        model: String? = nil,
        toolUseMode: String? = nil,
        active: Bool? = nil
    ) {
        self.name = name
        self.type = type
        self.apiKey = apiKey
        self.baseUrl = baseUrl
        self.model = model
        self.toolUseMode = toolUseMode
        self.active = active
    }
}

public enum EventType: String, Codable, Sendable {
    case runUpdated = "run.updated"
    case messageCreated = "message.created"
    case messageUpdated = "message.updated"
    case toolUpdated = "tool.updated"
    case approvalRequested = "approval.requested"
    case approvalResolved = "approval.resolved"
    case fileVersioned = "file.versioned"
    case unknown

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        let value = try container.decode(String.self)
        self = EventType(rawValue: value) ?? .unknown
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }
}

public struct MatrixclawEvent: Codable, Identifiable, Equatable, Sendable {
    public var id: UInt64?
    public var type: EventType
    public var sessionId: String
    public var runId: String?
    public var payload: JSONValue?
    public var at: Date?

    public func decodePayload<T: Decodable>(_ type: T.Type, using decoder: JSONDecoder = MatrixclawCoding.decoder) throws -> T {
        guard let payload else {
            throw MatrixclawClientError.missingPayload
        }
        return try payload.decoded(type, using: decoder)
    }
}

public struct EventReady: Codable, Equatable, Sendable {
    public var sessionId: String
    public var afterId: UInt64
}

public struct StorageEntry: Codable, Equatable, Sendable {
    public var path: String
    public var title: String?
    public var tags: [String]?
    public var mimeType: String?
    public var size: Int64
    public var createdAt: Date
    public var updatedAt: Date
}

public struct TemporaryStorageEntry: Codable, Equatable, Sendable {
    public var path: String
    public var title: String?
    public var tags: [String]?
    public var mimeType: String?
    public var size: Int64
    public var createdAt: Date
    public var expiresAt: Date
}

public struct StorageListResult: Codable, Equatable, Sendable {
    public var root: String
    public var files: [StorageEntry]
}

public struct StorageReadResult: Codable, Equatable, Sendable {
    public var file: StorageEntry
    public var content: String
}

public struct TemporaryStorageSettings: Codable, Equatable, Sendable {
    public var autoCleanup: Bool
    public var ttlSeconds: Int64
    public var maxBytes: Int64
    public var totalBytes: Int64
    public var totalFiles: Int
}

public struct TemporaryStorageListResult: Codable, Equatable, Sendable {
    public var root: String
    public var files: [TemporaryStorageEntry]
    public var settings: TemporaryStorageSettings
}

public struct CleanupResult: Codable, Equatable, Sendable {
    public var deletedFiles: Int
    public var freedBytes: Int64
}
