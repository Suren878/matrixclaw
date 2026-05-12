import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public struct MatrixclawSSEMessage: Equatable, Sendable {
    public var id: UInt64?
    public var eventName: String?
    public var data: String

    public init(id: UInt64? = nil, eventName: String? = nil, data: String) {
        self.id = id
        self.eventName = eventName
        self.data = data
    }
}

public enum MatrixclawStreamEvent: Equatable, Sendable {
    case ready(EventReady)
    case event(MatrixclawEvent)
    case raw(MatrixclawSSEMessage)
}

public struct MatrixclawReconnectPolicy: Sendable {
    public var isEnabled: Bool
    public var initialDelay: TimeInterval
    public var maximumDelay: TimeInterval

    public static let disabled = MatrixclawReconnectPolicy(isEnabled: false)
    public static let enabled = MatrixclawReconnectPolicy(isEnabled: true)

    public init(
        isEnabled: Bool,
        initialDelay: TimeInterval = 1,
        maximumDelay: TimeInterval = 15
    ) {
        self.isEnabled = isEnabled
        self.initialDelay = initialDelay
        self.maximumDelay = maximumDelay
    }
}

public struct MatrixclawEventStreamHooks: Sendable {
    public var onOpen: (@Sendable (_ lastEventId: UInt64?) -> Void)?
    public var onReconnect: (@Sendable (_ error: Error?, _ lastEventId: UInt64?, _ delay: TimeInterval) -> Void)?
    public var onMessage: (@Sendable (_ message: MatrixclawSSEMessage) -> Void)?

    public init(
        onOpen: (@Sendable (_ lastEventId: UInt64?) -> Void)? = nil,
        onReconnect: (@Sendable (_ error: Error?, _ lastEventId: UInt64?, _ delay: TimeInterval) -> Void)? = nil,
        onMessage: (@Sendable (_ message: MatrixclawSSEMessage) -> Void)? = nil
    ) {
        self.onOpen = onOpen
        self.onReconnect = onReconnect
        self.onMessage = onMessage
    }
}

public final class MatrixclawEventStreamClient: @unchecked Sendable {
    public var configuration: MatrixclawConfiguration

    private let session: URLSession
    private let decoder: JSONDecoder

    public init(
        configuration: MatrixclawConfiguration,
        session: URLSession = .shared,
        decoder: JSONDecoder = MatrixclawCoding.decoder
    ) {
        self.configuration = configuration
        self.session = session
        self.decoder = decoder
    }

    public func events(
        sessionId: String,
        afterId: UInt64? = nil,
        lastEventId: UInt64? = nil,
        reconnectPolicy: MatrixclawReconnectPolicy = .disabled,
        hooks: MatrixclawEventStreamHooks = MatrixclawEventStreamHooks()
    ) -> AsyncThrowingStream<MatrixclawStreamEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                var currentLastEventId = lastEventId ?? afterId
                var retryDelay = reconnectPolicy.initialDelay

                while !Task.isCancelled {
                    do {
                        hooks.onOpen?(currentLastEventId)
                        for try await message in try await openRawMessages(
                            sessionId: sessionId,
                            afterId: currentLastEventId,
                            lastEventId: currentLastEventId
                        ) {
                            hooks.onMessage?(message)
                            if let id = message.id {
                                currentLastEventId = id
                            }
                            continuation.yield(decodeStreamEvent(message))
                        }
                        throw MatrixclawClientError.streamClosed
                    } catch {
                        guard reconnectPolicy.isEnabled, !Task.isCancelled else {
                            continuation.finish(throwing: error)
                            return
                        }
                        hooks.onReconnect?(error, currentLastEventId, retryDelay)
                        try? await Task.sleep(nanoseconds: retryDelay.nanoseconds)
                        retryDelay = min(retryDelay * 2, reconnectPolicy.maximumDelay)
                    }
                }
                continuation.finish()
            }

            continuation.onTermination = { _ in
                task.cancel()
            }
        }
    }

    public func rawMessages(
        sessionId: String,
        afterId: UInt64? = nil,
        lastEventId: UInt64? = nil
    ) async throws -> AsyncThrowingStream<MatrixclawSSEMessage, Error> {
        try await openRawMessages(sessionId: sessionId, afterId: afterId, lastEventId: lastEventId)
    }

    private func openRawMessages(
        sessionId: String,
        afterId: UInt64?,
        lastEventId: UInt64?
    ) async throws -> AsyncThrowingStream<MatrixclawSSEMessage, Error> {
        var request = try makeRequest(sessionId: sessionId, afterId: afterId)
        if let lastEventId {
            request.setValue(String(lastEventId), forHTTPHeaderField: "Last-Event-ID")
        }
        let (bytes, response) = try await session.bytes(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw MatrixclawClientError.invalidResponse
        }
        guard httpResponse.statusCode < 400 else {
            throw MatrixclawClientError.api(
                statusCode: httpResponse.statusCode,
                message: HTTPURLResponse.localizedString(forStatusCode: httpResponse.statusCode),
                body: nil
            )
        }

        return AsyncThrowingStream { continuation in
            let task = Task {
                var parser = SSEParser()
                do {
                    for try await line in bytes.lines {
                        if let message = parser.receive(line: line) {
                            continuation.yield(message)
                        }
                    }
                    if let message = parser.finish() {
                        continuation.yield(message)
                    }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in
                task.cancel()
            }
        }
    }

    private func makeRequest(sessionId: String, afterId: UInt64?) throws -> URLRequest {
        guard var components = URLComponents(url: configuration.baseURL.appendingPath("/v1/events"), resolvingAgainstBaseURL: false) else {
            throw MatrixclawClientError.invalidURL
        }
        var queryItems = [URLQueryItem(name: "session_id", value: sessionId)]
        if let afterId {
            queryItems.append(URLQueryItem(name: "after", value: String(afterId)))
        }
        components.queryItems = queryItems
        guard let url = components.url else {
            throw MatrixclawClientError.invalidURL
        }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        request.setValue("no-cache", forHTTPHeaderField: "Cache-Control")
        if let token = configuration.bearerToken?.nilIfBlank {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return request
    }

    private func decodeStreamEvent(_ message: MatrixclawSSEMessage) -> MatrixclawStreamEvent {
        guard let data = message.data.data(using: .utf8) else {
            return .raw(message)
        }
        if message.eventName == "ready", let ready = try? decoder.decode(EventReady.self, from: data) {
            return .ready(ready)
        }
        if let event = try? decoder.decode(MatrixclawEvent.self, from: data) {
            return .event(event)
        }
        return .raw(message)
    }
}

private struct SSEParser {
    private var id: UInt64?
    private var eventName: String?
    private var dataLines: [String] = []

    mutating func receive(line: String) -> MatrixclawSSEMessage? {
        if line.isEmpty {
            return flush()
        }
        if line.hasPrefix(":") {
            return nil
        }
        let parts = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let field = String(parts.first ?? "")
        let value = parts.count > 1 ? String(parts[1]).trimSingleLeadingSpace() : ""
        switch field {
        case "id":
            id = UInt64(value)
        case "event":
            eventName = value
        case "data":
            dataLines.append(value)
        default:
            break
        }
        return nil
    }

    mutating func finish() -> MatrixclawSSEMessage? {
        flush()
    }

    private mutating func flush() -> MatrixclawSSEMessage? {
        defer {
            id = nil
            eventName = nil
            dataLines.removeAll(keepingCapacity: true)
        }
        guard !dataLines.isEmpty else {
            return nil
        }
        return MatrixclawSSEMessage(id: id, eventName: eventName, data: dataLines.joined(separator: "\n"))
    }
}

private extension String {
    func trimSingleLeadingSpace() -> String {
        hasPrefix(" ") ? String(dropFirst()) : self
    }
}

private extension TimeInterval {
    var nanoseconds: UInt64 {
        UInt64((self * 1_000_000_000).rounded())
    }
}
