import Foundation

public struct StorageSaveRequest: Codable, Equatable, Sendable {
    public var path: String
    public var contentBase64: String
    public var title: String
    public var tags: [String]
    public var mimeType: String

    public init(path: String, content: Data, title: String = "", tags: [String] = [], mimeType: String = "") {
        self.path = path
        self.contentBase64 = content.base64EncodedString()
        self.title = title
        self.tags = tags
        self.mimeType = mimeType
    }
}

public struct TemporaryStoragePromoteRequest: Codable, Equatable, Sendable {
    public var destPath: String

    public init(destPath: String) {
        self.destPath = destPath
    }
}

public struct TemporaryStorageSettingsUpdate: Codable, Equatable, Sendable {
    public var autoCleanup: Bool?
    public var ttlDays: Int64
    public var maxGb: Double

    public init(autoCleanup: Bool? = nil, ttlDays: Int64 = 0, maxGb: Double = 0) {
        self.autoCleanup = autoCleanup
        self.ttlDays = ttlDays
        self.maxGb = maxGb
    }
}

public extension MatrixclawAPIClient {
    func saveStorageFile(
        path: String,
        content: Data,
        title: String = "",
        tags: [String] = [],
        mimeType: String = ""
    ) async throws -> StorageEntry {
        let request = StorageSaveRequest(path: path, content: content, title: title, tags: tags, mimeType: mimeType)
        let response: StorageFileResponse = try await self.request(.post, path: "/v1/modules/storage/files", body: request)
        return response.file
    }

    func listStorageFiles(prefix: String? = nil, query: String? = nil, limit: Int? = nil) async throws -> StorageListResult {
        var queryItems: [URLQueryItem] = []
        if let prefix {
            queryItems.append(URLQueryItem(name: "prefix", value: prefix))
        }
        if let query {
            queryItems.append(URLQueryItem(name: "query", value: query))
        }
        if let limit {
            queryItems.append(URLQueryItem(name: "limit", value: String(limit)))
        }
        return try await self.request(.get, path: "/v1/modules/storage/files", queryItems: queryItems)
    }

    func readStorageFile(path: String) async throws -> StorageReadResult {
        try await self.request(.get, path: "/v1/modules/storage/files/\(path.percentEncodedPathComponent)")
    }

    func deleteStorageFile(path: String) async throws -> StorageEntry {
        let response: StorageFileResponse = try await self.request(
            .delete,
            path: "/v1/modules/storage/files/\(path.percentEncodedPathComponent)"
        )
        return response.file
    }

    func saveTemporaryStorageFile(
        path: String,
        content: Data,
        title: String = "",
        tags: [String] = [],
        mimeType: String = ""
    ) async throws -> TemporaryStorageEntry {
        let request = StorageSaveRequest(path: path, content: content, title: title, tags: tags, mimeType: mimeType)
        let response: TemporaryStorageFileResponse = try await self.request(.post, path: "/v1/modules/storage/temp", body: request)
        return response.file
    }

    func listTemporaryStorageFiles(limit: Int? = nil) async throws -> TemporaryStorageListResult {
        let queryItems = limit.map { [URLQueryItem(name: "limit", value: String($0))] } ?? []
        return try await self.request(.get, path: "/v1/modules/storage/temp", queryItems: queryItems)
    }

    func promoteTemporaryStorageFile(path: String, destPath: String = "") async throws -> StorageEntry {
        let response: StorageFileResponse = try await self.request(
            .post,
            path: "/v1/modules/storage/temp/\(path.percentEncodedPathComponent)/promote",
            body: TemporaryStoragePromoteRequest(destPath: destPath)
        )
        return response.file
    }

    func deleteTemporaryStorageFile(path: String) async throws -> TemporaryStorageEntry {
        let response: TemporaryStorageFileResponse = try await self.request(
            .delete,
            path: "/v1/modules/storage/temp/\(path.percentEncodedPathComponent)"
        )
        return response.file
    }

    func cleanupTemporaryStorageFiles() async throws -> CleanupResult {
        let response: CleanupResponseWrapper = try await self.request(
            .post,
            path: "/v1/modules/storage/temp/cleanup",
            body: EmptyBody()
        )
        return response.cleanup
    }

    func updateTemporaryStorageSettings(_ update: TemporaryStorageSettingsUpdate) async throws -> TemporaryStorageSettings {
        let response: TemporaryStorageSettingsResponse = try await self.request(
            .patch,
            path: "/v1/modules/storage/temp/settings",
            body: update
        )
        return response.settings
    }
}

struct StorageFileResponse: Decodable {
    var file: StorageEntry
}

struct TemporaryStorageFileResponse: Decodable {
    var file: TemporaryStorageEntry
}

struct TemporaryStorageSettingsResponse: Decodable {
    var settings: TemporaryStorageSettings
}

struct CleanupResponseWrapper: Decodable {
    var cleanup: CleanupResult
}
