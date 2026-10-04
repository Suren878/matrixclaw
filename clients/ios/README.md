# MatrixclawClient (Swift)

`clients/ios` is a Swift package with a client for the `matrixclawd` HTTP/SSE
API. It has no UI and no dependencies; an app builds its own screens and state
on top of it. Platforms: iOS 15+, macOS 12+ (Swift tools 5.9).

## Build and Test

```bash
cd clients/ios
swift build
swift test
```

Run these on macOS: the event stream uses `URLSession.bytes`, which is not in
swift-corelibs Foundation on Linux. To use the package in an app, add the local
folder in Xcode with **File > Add Package Dependencies... > Add Local** and link
the `MatrixclawClient` library.

## Connecting to the Daemon

The daemon listens on `127.0.0.1:8080` by default (`daemon.http_addr` in setup,
or `MATRIXCLAW_HTTP_ADDR`). A phone cannot reach loopback, so either:

- keep the daemon on loopback and reach it through a tunnel or HTTPS reverse
  proxy on the host, or
- bind it to another address. The daemon refuses that unless
  `MATRIXCLAW_ALLOW_REMOTE_HTTP=1` is set and an API token is configured. The
  API itself is plain HTTP, so put TLS (proxy or VPN) in front of it.

## Auth

Set the daemon's API token with `MATRIXCLAW_API_TOKEN` or `daemon.api_token` in
setup, and pass the same value as `bearerToken`. The client sends it as
`Authorization: Bearer <token>` on every request, including the event stream.
Only `GET /v1/health` works without it. Store the token in the Keychain; the
package does not persist anything.

The client sends no `X-Matrixclaw-Role` header, so the daemon treats it as the
owner.

## Usage

`clientName` and `externalKey` identify this device's binding, i.e. which
session it is currently using. Keep `externalKey` stable per device or account.

```swift
import MatrixclawClient

let config = MatrixclawConfiguration(
    baseURL: URL(string: "https://matrixclaw.example.net")!,
    bearerToken: token,
    clientName: "ios",
    externalKey: deviceID
)
let api = MatrixclawAPIClient(configuration: config)

let session = try await api.createSession(title: "iPhone")
_ = try await api.useSession(sessionId: session.id)
let snapshot = try await api.snapshot()
let accepted = try await api.sendMessageText(sessionId: snapshot.sessionId, text: "Hello")
```

## Event Stream

```swift
let stream = MatrixclawEventStreamClient(configuration: config)

for try await item in stream.events(sessionId: session.id, reconnectPolicy: .enabled) {
    switch item {
    case .ready(let ready): print("ready after", ready.afterId)
    case .event(let event): print(event.type, event.id as Any)
    case .raw(let raw): print("unparsed", raw.eventName as Any)
    }
}
```

With `.enabled` the stream reconnects with backoff (1 s up to 15 s) and resumes
after the last event ID, sent as both the `after` query parameter and the
`Last-Event-ID` header. `MatrixclawEventStreamHooks` reports open, reconnect
and raw-message callbacks. Event payloads are kept as `JSONValue`; decode them
with `event.decodePayload(_:)`.

## Coverage

| Area | Endpoints |
| --- | --- |
| Health, bindings, snapshot | `GET /v1/health`, `GET /v1/bindings/current`, `POST /v1/bindings/use`, `GET /v1/snapshot` |
| Sessions | `GET/POST /v1/sessions`, `PATCH/DELETE /v1/sessions/{id}` |
| Messages, events | `GET/POST /v1/messages`, `GET /v1/events` (SSE) |
| Runs, approvals | `GET /v1/runs/{id}`, `POST /v1/runs/{id}/cancel`, `GET /v1/approvals`, `POST /v1/approvals/{id}/resolve` |
| Providers | `GET /v1/session-providers`, `GET /v1/setup/providers`, `PATCH/DELETE /v1/setup/providers/{id}` |
| Storage module | `/v1/modules/storage/files` and `/v1/modules/storage/temp` (save, list, read, delete, promote, cleanup, settings) |

Files and images are uploaded as JSON with `content_base64` through the storage
endpoints. Voice, external agents, tasks, automation and the other daemon routes
are not wrapped; see `internal/api/server.go` for the full route list.
