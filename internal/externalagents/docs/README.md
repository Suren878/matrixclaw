# External Agents Integration

This folder documents the experimental external-agent integration shape.
The goal is to let matrixclaw talk to tools like Codex app-server without
turning those tools into normal LLM providers or spreading their protocol
details through the core.

The integration must stay removable. If the Codex direction stops being useful,
the project should be able to delete the Codex adapter and keep normal
assistant sessions, providers, TUI, Telegram, and storage working.

## Principle

matrixclaw owns the product session.

External agents own their own runtime thread.

```text
matrixclaw session
  id: session_x
  kind: external_agent
  client surfaces: TUI / Telegram / future mobile
  local journal: messages, files, run events, approvals

external agent attachment
  agent_id: codex-app
  external_thread_id: Codex thread id
  metadata_json: adapter-specific state
```

The external agent is an attachment, not the source of truth. matrixclaw should
store enough local history and metadata to show the session, recover from
adapter failures, and detach the external agent later if needed.

## Desired Package Layout

```text
internal/externalagents/
  agent.go
  registry.go
  store.go
  docs/
    README.md
    codex-app.md
    removal-plan.md
  codexapp/
    client.go
    process.go
    types.go
    *_test.go
```

The only Codex-specific package should be `codexapp`.

Core packages should depend on the generic `externalagents` interfaces, never
on `codexapp` directly.

## Generic Interface Shape

```go
type Agent interface {
    ID() string
    DisplayName() string
    Available(ctx context.Context) Availability
    StartSession(ctx context.Context, req StartSessionRequest) (ExternalSession, error)
    ResumeSession(ctx context.Context, session ExternalSession) error
    Send(ctx context.Context, session ExternalSession, input Input) (<-chan Event, error)
    Interrupt(ctx context.Context, session ExternalSession) error
    Close() error
}
```

The registry should expose adapter discovery:

```go
type Registry interface {
    List(ctx context.Context) []Descriptor
    Get(id string) (Agent, bool)
}
```

`Descriptor` is what setup and UI should read:

```go
type Descriptor struct {
    ID          string
    DisplayName string
    Installed   bool
    Enabled     bool
    AuthState   string
    Mode        string
}
```

## Storage Shape

Use a separate table. Do not add `codex_thread_id` columns to the main
`sessions` table.

```sql
CREATE TABLE external_agent_sessions (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    external_thread_id TEXT NOT NULL,
    external_session_id TEXT,
    cwd TEXT,
    model TEXT,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

Main session metadata only needs a generic kind:

```text
sessions.kind = assistant | external_agent
```

If keeping the main sessions table untouched is easier during early
experiments, store kind in metadata JSON first and migrate later.

## Runtime Routing

The runtime should have one generic branch:

```go
if session.Kind == "external_agent" {
    return externalAgentRuntime.Send(...)
}
return assistantRuntime.Send(...)
```

No client should know whether the external agent is Codex, Claude Code,
OpenCode, or something else. TUI and Telegram should render normalized
matrixclaw events.

## Event Normalization

Adapters translate their native event stream into a small matrixclaw event set:

```text
message.delta
reasoning.delta
tool.started
tool.output.delta
tool.completed
diff.updated
approval.requested
turn.started
turn.completed
turn.failed
```

The Codex adapter can keep raw JSON for diagnostics, but UI/runtime should use
normalized events.

## Setup Flow

Setup should show a separate section:

```text
External Agents
  Codex        Installed / Not installed    Enabled / Disabled
  Claude Code  later
  OpenCode     later
```

Codex setup should detect:

```text
codex binary exists
app-server starts
initialize succeeds
auth is usable, if exposed by protocol
```

Setup should not import `codexapp`. It should ask the registry for descriptors.

## Session Creation Flow

New session should offer:

```text
Assistant
Codex
```

Assistant sessions use normal matrixclaw providers and tools.

Codex sessions use external-agent runtime:

```text
matrixclaw session -> codex app-server thread -> turn/start
```

## Current Experimental Status

Implemented:

```text
internal/externalagents/codexapp
generic externalagents registry
generic externalagents runtime interface
SQLite external_agent_sessions store
core branch for sessions with external-agent attachments
Codex app-server runtime bridge
normalized message.delta / turn.completed / turn.failed events
```

It can:

```text
start codex app-server over stdio
initialize
thread/start
thread/resume
turn/start
turn/steer
receive notifications
map Codex notifications to generic external-agent events
execute an attached external-agent run through core without UI wiring
run fake protocol tests
run live initialize smoke test
run live thread+turn smoke test
```

Not implemented yet:

```text
setup UI
TUI/Telegram session creation
approval request handling
Codex runtime wiring in daemon bootstrap
API endpoint to list external agents
API/session creation flow for external-agent sessions
tool/diff/approval event rendering beyond message/turn events
```
