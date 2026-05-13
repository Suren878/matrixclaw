import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

enum HTTPMethod: String {
    case get = "GET"
    case post = "POST"
    case patch = "PATCH"
    case delete = "DELETE"
}

public final class MatrixclawAPIClient: @unchecked Sendable {
    public var configuration: MatrixclawConfiguration

    private let session: URLSession
    private let decoder: JSONDecoder
    private let encoder: JSONEncoder

    public init(
        configuration: MatrixclawConfiguration,
        session: URLSession = .shared,
        decoder: JSONDecoder = MatrixclawCoding.decoder,
        encoder: JSONEncoder = MatrixclawCoding.encoder
    ) {
        self.configuration = configuration
        self.session = session
        self.decoder = decoder
        self.encoder = encoder
    }

    public func health() async throws -> HealthResponse {
        try await request(.get, path: "/v1/health")
    }

    public func currentBinding() async throws -> ClientBinding {
        let response: ClientBindingResponse = try await request(
            .get,
            path: "/v1/bindings/current",
            queryItems: bindingQueryItems()
        )
        return response.binding
    }

    public func snapshot() async throws -> ClientSnapshot {
        let response: ClientSnapshotResponse = try await request(
            .get,
            path: "/v1/snapshot",
            queryItems: bindingQueryItems()
        )
        return response.snapshot
    }

    public func listSessions() async throws -> [Session] {
        let response: SessionsResponse = try await request(.get, path: "/v1/sessions")
        return response.sessions ?? []
    }

    public func createSession(
        title: String,
        workingDir: String? = nil,
        runtimeId: SessionRuntime? = nil,
        permissionMode: PermissionMode? = nil
    ) async throws -> Session {
        let body = CreateSessionRequest(
            title: title,
            runtimeId: runtimeId,
            workingDir: workingDir ?? "",
            permissionMode: permissionMode
        )
        let response: SessionResponse = try await request(.post, path: "/v1/sessions", body: body)
        return response.session
    }

    public func renameSession(sessionId: String, title: String) async throws -> Session {
        let response: SessionResponse = try await request(
            .patch,
            path: "/v1/sessions/\(sessionId.percentEncodedPathComponent)",
            body: RenameSessionRequest(title: title)
        )
        return response.session
    }

    public func deleteSession(sessionId: String) async throws {
        let _: EmptyResponse = try await request(.delete, path: "/v1/sessions/\(sessionId.percentEncodedPathComponent)")
    }

    public func useSession(sessionId: String) async throws -> ClientBinding {
        let body = UseBindingInput(
            client: configuration.clientName,
            externalKey: configuration.externalKey,
            sessionId: sessionId
        )
        let response: ClientBindingResponse = try await request(.post, path: "/v1/bindings/use", body: body)
        return response.binding
    }

    public func listMessages(sessionId: String, limit: Int = 50) async throws -> [Message] {
        let response: MessagesResponse = try await request(
            .get,
            path: "/v1/messages",
            queryItems: [
                URLQueryItem(name: "session_id", value: sessionId),
                URLQueryItem(name: "limit", value: String(limit))
            ]
        )
        return response.messages ?? []
    }

    public func sendMessageText(
        sessionId: String,
        text: String,
        workingDir: String? = nil,
        allowAutoBindOne: Bool = true
    ) async throws -> AcceptRunResult {
        try await sendMessage(
            sessionId: sessionId,
            text: text,
            parts: nil,
            workingDir: workingDir,
            allowAutoBindOne: allowAutoBindOne
        )
    }

    public func sendMessage(
        sessionId: String,
        text: String,
        parts: [MessagePart]?,
        workingDir: String? = nil,
        allowAutoBindOne: Bool = true
    ) async throws -> AcceptRunResult {
        let body = HandleMessageInput(
            client: configuration.clientName,
            externalKey: configuration.externalKey,
            sessionId: sessionId,
            text: text,
            parts: parts,
            workingDir: workingDir ?? configuration.defaultWorkingDirectory ?? "",
            allowAutoBindOne: allowAutoBindOne
        )
        return try await request(.post, path: "/v1/messages", body: body)
    }

    public func listApprovals(sessionId: String? = nil, state: ApprovalState? = nil) async throws -> [Approval] {
        var queryItems: [URLQueryItem] = []
        if let sessionId {
            queryItems.append(URLQueryItem(name: "session_id", value: sessionId))
        }
        if let state {
            queryItems.append(URLQueryItem(name: "state", value: state.rawValue))
        }
        let response: ApprovalsResponse = try await request(.get, path: "/v1/approvals", queryItems: queryItems)
        return response.approvals ?? []
    }

    public func resolveApproval(approvalId: String, approved: Bool) async throws -> Approval {
        let response: ApprovalResponse = try await request(
            .post,
            path: "/v1/approvals/\(approvalId.percentEncodedPathComponent)/resolve",
            body: ApprovalResolveRequest(approved: approved)
        )
        return response.approval
    }

    public func getRun(runId: String) async throws -> Run {
        let response: RunResponse = try await request(.get, path: "/v1/runs/\(runId.percentEncodedPathComponent)")
        return response.run
    }

    public func cancelRun(runId: String) async throws -> Run {
        let response: RunResponse = try await request(.post, path: "/v1/runs/\(runId.percentEncodedPathComponent)/cancel")
        return response.run
    }

    public func listSessionProviders() async throws -> [SessionProviderOption] {
        let response: SessionProvidersResponse = try await request(.get, path: "/v1/session-providers")
        return response.providers ?? []
    }

    public func listSetupProviders() async throws -> [ProviderSetupItem] {
        let response: ProviderSetupListResponse = try await request(
            .get,
            path: "/v1/setup/providers",
            queryItems: clientQueryItems()
        )
        return response.providers ?? []
    }

    public func configureSetupProvider(providerId: String, update: ProviderSetupUpdate) async throws -> ProviderSetupItem {
        let response: ProviderSetupResponse = try await request(
            .patch,
            path: "/v1/setup/providers/\(providerId.percentEncodedPathComponent)",
            queryItems: clientQueryItems(),
            body: update
        )
        return response.provider
    }

    public func deleteSetupProvider(providerId: String) async throws {
        let _: OKResponse = try await request(
            .delete,
            path: "/v1/setup/providers/\(providerId.percentEncodedPathComponent)",
            queryItems: clientQueryItems()
        )
    }

    func request<Response: Decodable>(
        _ method: HTTPMethod,
        path: String,
        queryItems: [URLQueryItem] = []
    ) async throws -> Response {
        try await request(method, path: path, queryItems: queryItems, encodedBody: nil)
    }

    func request<Response: Decodable, Body: Encodable>(
        _ method: HTTPMethod,
        path: String,
        queryItems: [URLQueryItem] = [],
        body: Body
    ) async throws -> Response {
        try await request(method, path: path, queryItems: queryItems, encodedBody: AnyEncodable(body))
    }

    private func request<Response: Decodable>(
        _ method: HTTPMethod,
        path: String,
        queryItems: [URLQueryItem],
        encodedBody: AnyEncodable?
    ) async throws -> Response {
        let request = try makeURLRequest(method, path: path, queryItems: queryItems, body: encodedBody)
        let (data, response) = try await session.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw MatrixclawClientError.invalidResponse
        }
        guard httpResponse.statusCode < 400 else {
            throw decodeAPIError(statusCode: httpResponse.statusCode, data: data)
        }
        if Response.self == EmptyResponse.self {
            return EmptyResponse() as! Response
        }
        guard !data.isEmpty else {
            throw MatrixclawClientError.emptyResponseBody
        }
        return try decoder.decode(Response.self, from: data)
    }

    private func makeURLRequest(
        _ method: HTTPMethod,
        path: String,
        queryItems: [URLQueryItem] = [],
        body: AnyEncodable?
    ) throws -> URLRequest {
        guard var components = URLComponents(url: configuration.baseURL.appendingPath(path), resolvingAgainstBaseURL: false) else {
            throw MatrixclawClientError.invalidURL
        }
        if !queryItems.isEmpty {
            components.queryItems = queryItems
        }
        guard let url = components.url else {
            throw MatrixclawClientError.invalidURL
        }

        var request = URLRequest(url: url)
        request.httpMethod = method.rawValue
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token = configuration.bearerToken?.nilIfBlank {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        if let body {
            request.httpBody = try encoder.encode(body)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        return request
    }

    func decodeAPIError(statusCode: Int, data: Data) -> MatrixclawClientError {
        let decodedMessage = try? decoder.decode(ErrorResponse.self, from: data).error
        let message = decodedMessage?.nilIfBlank
            ?? HTTPURLResponse.localizedString(forStatusCode: statusCode)
        return .api(statusCode: statusCode, message: message, body: data.isEmpty ? nil : data)
    }

    func clientQueryItems() -> [URLQueryItem] {
        [URLQueryItem(name: "client", value: configuration.clientName)]
    }

    func bindingQueryItems() -> [URLQueryItem] {
        [
            URLQueryItem(name: "client", value: configuration.clientName),
            URLQueryItem(name: "external_key", value: configuration.externalKey)
        ]
    }
}

private struct AnyEncodable: Encodable {
    private let encodeValue: (Encoder) throws -> Void

    init<T: Encodable>(_ value: T) {
        self.encodeValue = value.encode(to:)
    }

    func encode(to encoder: Encoder) throws {
        try encodeValue(encoder)
    }
}

struct EmptyBody: Encodable {}

struct EmptyResponse: Decodable {
    init() {}
}

struct OKResponse: Codable {
    var ok: Bool
}

struct CreateSessionRequest: Encodable {
    var title: String
    var runtimeId: SessionRuntime?
    var workingDir: String
    var permissionMode: PermissionMode?
}

struct RenameSessionRequest: Encodable {
    var title: String
}

struct UseBindingInput: Encodable {
    var client: String
    var externalKey: String
    var sessionId: String
}

struct HandleMessageInput: Encodable {
    var client: String
    var externalKey: String
    var sessionId: String
    var text: String
    var parts: [MessagePart]?
    var workingDir: String
    var allowAutoBindOne: Bool
}

struct ApprovalResolveRequest: Encodable {
    var approved: Bool
}

struct SessionsResponse: Decodable {
    var sessions: [Session]?
}

struct SessionResponse: Decodable {
    var session: Session
}

struct MessagesResponse: Decodable {
    var messages: [Message]?
}

struct ApprovalsResponse: Decodable {
    var approvals: [Approval]?
}

struct ApprovalResponse: Decodable {
    var approval: Approval
}

struct RunResponse: Decodable {
    var run: Run
}

struct ClientBindingResponse: Decodable {
    var binding: ClientBinding
}

struct ClientSnapshotResponse: Decodable {
    var snapshot: ClientSnapshot
}

struct SessionProvidersResponse: Decodable {
    var providers: [SessionProviderOption]?
}

struct ProviderSetupListResponse: Decodable {
    var providers: [ProviderSetupItem]?
}

struct ProviderSetupResponse: Decodable {
    var provider: ProviderSetupItem
}

extension URL {
    func appendingPath(_ path: String) -> URL {
        let normalizedBase = absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        let normalizedPath = path.hasPrefix("/") ? String(path.dropFirst()) : path
        return URL(string: normalizedBase + "/" + normalizedPath) ?? self.appendingPathComponent(normalizedPath)
    }
}

extension String {
    var percentEncodedPathComponent: String {
        addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(CharacterSet(charactersIn: "/?#[]@!$&'()*+,;="))) ?? self
    }
}
