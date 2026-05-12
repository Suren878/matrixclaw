import Foundation

public enum MatrixclawClientError: Error, Equatable, Sendable {
    case invalidURL
    case invalidResponse
    case emptyResponseBody
    case api(statusCode: Int, message: String, body: Data?)
    case missingPayload
    case streamClosed
}

extension MatrixclawClientError: LocalizedError {
    public var errorDescription: String? {
        switch self {
        case .invalidURL:
            return "Invalid Matrixclaw URL."
        case .invalidResponse:
            return "The server response was not an HTTP response."
        case .emptyResponseBody:
            return "The server returned an empty response body."
        case .api(let statusCode, let message, _):
            return "Matrixclaw API error \(statusCode): \(message)"
        case .missingPayload:
            return "The event does not contain a payload."
        case .streamClosed:
            return "The event stream closed."
        }
    }
}
