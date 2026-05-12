# MatrixclawClient for iOS

`clients/ios` is a Swift Package with a reusable Matrixclaw daemon client layer. It is intentionally UI-free: apps can import `MatrixclawClient` and build their own SwiftUI/UIKit state and screens around it.

## Open

In Xcode, use **File > Add Package Dependencies...** and select this local folder:

```text
clients/ios
```

The package has no external dependencies and targets iOS 15+.

## Basic Usage

```swift
import Foundation
import MatrixclawClient

let config = MatrixclawConfiguration(
    baseURL: URL(string: "http://127.0.0.1:8080")!,
    bearerToken: "<daemon api token>",
    clientName: "ios",
    externalKey: "device-or-account-stable-id"
)

let api = MatrixclawAPIClient(configuration: config)

let health = try await api.health()
let sessions = try await api.listSessions()

let session = try await api.createSession(title: "iPhone", workingDir: "")
_ = try await api.useSession(sessionId: session.id)

let snapshot = try await api.snapshot()
let result = try await api.sendMessageText(sessionId: snapshot.sessionId, text: "Hello")
```

## Event Stream

```swift
let stream = MatrixclawEventStreamClient(configuration: config)

for try await item in stream.events(
    sessionId: session.id,
    reconnectPolicy: .enabled,
    hooks: MatrixclawEventStreamHooks(
        onReconnect: { error, lastEventId, delay in
            print("reconnect", lastEventId as Any, delay, error as Any)
        }
    )
) {
    switch item {
    case .ready(let ready):
        print("ready after", ready.afterId)
    case .event(let event):
        print("event", event.type, event.id as Any)
    case .raw(let raw):
        print("raw sse", raw.eventName as Any)
    }
}
```

The stream sends both `after` query and `Last-Event-ID` header when resuming. Unknown event payloads are preserved as `JSONValue`.

## Covered Endpoints

- `GET /v1/health`
- `GET /v1/bindings/current?client=&external_key=`
- `POST /v1/bindings/use`
- `GET /v1/snapshot?client=&external_key=`
- `GET /v1/sessions`
- `POST /v1/sessions`
- `PATCH /v1/sessions/{id}`
- `DELETE /v1/sessions/{id}`
- `GET /v1/messages?session_id=&limit=`
- `POST /v1/messages`
- `GET /v1/approvals?session_id=&state=`
- `POST /v1/approvals/{id}/resolve`
- `GET /v1/runs/{id}`
- `POST /v1/runs/{id}/cancel`
- `GET /v1/session-providers`
- `GET /v1/setup/providers?client=`
- `PATCH /v1/setup/providers/{id}?client=`
- `DELETE /v1/setup/providers/{id}?client=`
- `GET /v1/events?session_id=&after=` with SSE parsing and reconnect shape
- Optional storage module endpoints under `/v1/modules/storage/files` and `/v1/modules/storage/temp`

## Backend Notes

There is no realtime voice support here. File/image upload is modeled through the existing storage module JSON contract using `content_base64`; there is no dedicated iOS multipart upload endpoint in the current API.

Token persistence is app-owned. Store the bearer token in Keychain from the host app and pass it into `MatrixclawConfiguration`.
