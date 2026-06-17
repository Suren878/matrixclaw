# Telephony Refactor Audit - 2026-06-17

This audit maps the current telephony implementation before changing it. The
goal is to simplify the code while keeping the working behavior: outbound and
inbound calls through Asterisk ARI, realtime Gemini/Grok voice sessions,
recording, and post-call reporting.

## Current flow

1. The setup/config layer stores the telephony module settings in
   `internal/setup/telephony.go`.
2. The daemon API exposes those settings through
   `internal/api/module_telephony.go`; the control plane edits them through
   `internal/controlplane/modules_telephony.go`.
3. The regular assistant session gets the `telephony_call` tool from
   `internal/modules/telephony/tools.go`. The tool requires approval, checks
   module config, and POSTs `/v1/calls` to the gateway.
4. The standalone gateway in `cmd/matrixclaw-telephony-gateway` loads
   `internal/telephony/gateway.Config` from environment variables and serves
   authenticated `/v1/health` and `/v1/calls` routes.
5. Outbound calls start in `internal/telephony/gateway/outbound.go`; inbound
   calls start from ARI Stasis events in `internal/telephony/gateway/inbound.go`.
6. The gateway creates a realtime voice session through the daemon in
   `internal/telephony/gateway/realtime.go`. Telephony realtime sessions expose
   only `telephony_end_call`; that filter lives in
   `internal/modules/voice/realtime/manager.go`.
7. Audio is bridged in `internal/telephony/gateway/bridge.go`: ARI
   `externalMedia` channels feed RTP capture/playback sessions, and those RTP
   packets are translated to/from realtime audio in `audio_bridge.go`.
8. Recording starts on the phone channel in `recording.go`, is downloaded from
   ARI, optionally converted to MP3, saved locally, and optionally uploaded to
   MatrixClaw temporary storage.
9. Outbound calls can post a summary/report back to the originating session via
   `reports.go`.

## What not to delete

- The split between capture and playback RTP sessions is complex but
  purposeful. It matches ARI `externalMedia` directionality and lets the
  gateway separately receive caller audio and play assistant audio.
- The separate inbound and outbound paths are intentional. Outbound calls
  originate from MatrixClaw and can post reports back to an origin session;
  inbound calls originate from ARI events and should not assume that origin.
- `telephony_end_call` being the only tool available inside telephony realtime
  sessions is intentional. It prevents the phone call agent from seeing the full
  desktop/session tool surface.
- Recording is active code, not a stub. The highest risk is format/lifecycle
  handling, not obvious dead code.

## Main weak spots

### 1. Call state lifecycle and data races

`internal/telephony/gateway/calls.go` defines one mutable `Call` struct shared
between HTTP handlers and call goroutines. Most fields are updated directly:
status, channel IDs, realtime IDs, RTP stats, recording status, timestamps, and
errors. Only transcript slices have their own mutex.

Current risky patterns:

- `callSnapshot` reads most fields without a call-level lock.
- `handleCalls` holds `s.mu.RLock()` for the calls map and then calls
  `syncCallStats`, which mutates call fields while only a map read lock is
  held.
- `connectRealtime`, `runCallOnce`, `runInboundCallOnce`,
  `runConnectedCallWithRealtime`, `startChannelRecording`, and
  `finishCallRecording` all mutate the same call object from long-running
  goroutines.
- `startCall` and `startInboundCall` ignore the parent context and create call
  contexts from `context.Background()`. Active calls are therefore not clearly
  tied to gateway shutdown.
- Finished calls stay in `s.calls` indefinitely. A long-running gateway can
  accumulate historical call objects forever.

First implementation target:

1. Add explicit call state locking or state methods.
2. Make snapshots copy under the same lock.
3. Stop mutating call state while only holding the server calls-map lock.
4. Tie active calls to a server/root context and cancel them during shutdown.
5. Add a small retention/prune path for finished calls.

### 2. Duplicated request/client and utility code

The telephony flow works, but it has repeated helpers that make behavior drift
likely.

Examples:

- `Server.client` exists, but `postCallReport` and `saveRecordingTemporary`
  each create their own `http.Client`.
- `ariClient.do` centralizes JSON ARI requests, while
  `downloadStoredRecording` repeats raw request/auth/status handling.
- The module tool and gateway both implement phone normalization. They are not
  equivalent: the gateway normalizer handles Russian `8...` to `7...`, while
  the tool strips to digits only.
- Small helpers such as `firstNonEmpty`, bearer-token parsing, and JSON writing
  are repeated locally. Some duplication is acceptable across package
  boundaries, but the telephony path should not normalize the same phone number
  differently before and after the gateway.

Second implementation target:

1. Add a narrow MatrixClaw API client/helper inside the gateway and use it for
   realtime-session creation, temporary recording upload, and post-call
   reports.
2. Add a raw-download helper to the ARI client so stored-recording downloads
   share auth/status/error handling with the rest of ARI.
3. Move telephony phone normalization into one shared internal package used by
   both the tool and gateway.
4. Keep generic API/control-plane helper deduplication out of this pass unless
   it directly affects telephony behavior.

### 3. Recording format validation is loose

`recording.go` accepts any alphanumeric recording format after normalization.
That means an unknown extension can flow into ARI recording names, local file
paths, and MIME fallback as `application/octet-stream`.

This should be narrowed to the formats the gateway actually supports and can
serve predictably, especially because MP3 has a special capture/convert path.

### 4. Provider-visible tool behavior may be noisy

`telephony_call` is registered with the daemon tool registry at startup. Runtime
execution checks whether the module is configured, but the provider may still
see the tool before a call can actually be placed. This is not the first
refactor target because it is not corrupting active calls, but it is a product
quality issue for disabled or partially configured telephony.

### 5. Test coverage is missing around the riskiest code

There are no dedicated telephony/gateway tests in the current tree. The first
tests should cover deterministic code before trying to simulate full RTP/ARI:

- environment config parsing and defaults;
- caller allow-list normalization;
- phone normalization consistency between tool and gateway;
- recording format/capture-format/MIME decisions;
- post-call report payload decisions;
- HTTP handler auth and method/status behavior;
- call snapshot locking after the call-state refactor.

## Recommended order

1. Stabilize call state and lifecycle. This directly reduces risk around live
   calls, recordings, HTTP call listing, and shutdown.
2. Collapse duplicated telephony request/client utilities and phone
   normalization. This makes later behavior changes smaller and easier to
   verify.
3. Tighten recording format validation.
4. Revisit provider-visible telephony tool registration/status prompts.

Large deletions should wait until after these passes. The current audit did not
find a clearly dead telephony subsystem; it found working but tightly coupled
code with concurrency, lifecycle, and duplication risks.
