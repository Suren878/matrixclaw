import Foundation
import XCTest
@testable import MatrixclawClient

final class ModelDecodingTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try MatrixclawCoding.decoder.decode(type, from: Data(json.utf8))
    }

    func testUnknownRunStatusDecodesAndRoundTrips() throws {
        let run = try decode(Run.self, #"{"id":"r1","session_id":"s1","user_message_id":"m1","status":"waiting_events","started_at":"2026-09-23T10:00:00Z","updated_at":"2026-09-23T10:00:00.5Z"}"#)
        XCTAssertEqual(run.status, .unknown("waiting_events"))
        let again = try MatrixclawCoding.decoder.decode(Run.self, from: MatrixclawCoding.encoder.encode(run))
        XCTAssertEqual(again.status.rawValue, "waiting_events")
    }

    func testKnownRunStatusesKeepTheirCases() throws {
        let cases: [(String, RunStatus)] = [
            ("accepted", .accepted), ("running", .running), ("waiting_approval", .waitingApproval),
            ("completed", .completed), ("canceled", .canceled), ("failed", .failed),
        ]
        for (raw, want) in cases {
            let status = try decode(RunStatus.self, "\"\(raw)\"")
            XCTAssertEqual(status, want)
            XCTAssertEqual(status.rawValue, raw)
        }
    }

    func testMessageSeqAndNormalisedUsageDecode() throws {
        let message = try decode(Message.self, #"{"id":"m1","seq":42,"session_id":"s1","run_id":"r1","role":"user","content":"hi","created_at":"2026-09-23T10:00:00Z","updated_at":"2026-09-23T10:00:00Z"}"#)
        XCTAssertEqual(message.seq, 42)
        let usage = try decode(ProviderUsage.self, #"{"prompt_tokens":420,"output_tokens":50,"cache_read_tokens":300,"cache_write_tokens":20}"#)
        XCTAssertEqual(usage.promptTokens, 420)
        XCTAssertEqual(usage.cacheReadTokens, 300)
        XCTAssertEqual(usage.cacheWriteTokens, 20)
        XCTAssertEqual(usage.outputTokens, 50)
    }
}
