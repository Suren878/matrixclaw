import Foundation

public struct MatrixclawConfiguration: Sendable {
    public var baseURL: URL
    public var bearerToken: String?
    public var clientName: String
    public var externalKey: String
    public var defaultWorkingDirectory: String?

    public init(
        baseURL: URL,
        bearerToken: String? = nil,
        clientName: String = "ios",
        externalKey: String,
        defaultWorkingDirectory: String? = nil
    ) {
        self.baseURL = baseURL.matrixclawBaseURL
        self.bearerToken = bearerToken?.nilIfBlank
        self.clientName = clientName.trimmingCharacters(in: .whitespacesAndNewlines)
        self.externalKey = externalKey.trimmingCharacters(in: .whitespacesAndNewlines)
        self.defaultWorkingDirectory = defaultWorkingDirectory?.nilIfBlank
    }
}

extension URL {
    var matrixclawBaseURL: URL {
        var value = absoluteString
        while value.hasSuffix("/") {
            value.removeLast()
        }
        return URL(string: value) ?? self
    }
}

extension String {
    var nilIfBlank: String? {
        let trimmed = trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }
}
