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

`sessions.runtime_id` is a broad runtime family, not a concrete app name:

```text
matrixclaw       normal provider-backed assistant session
external_agent   Codex / Claude Code / Kimi Code / OpenCode / future adapters
```

The concrete adapter is selected by `external_agent_sessions.agent_id`. Legacy
values such as `runtime_id=codex` are accepted as aliases at API boundaries, but
new code should create external-agent sessions with:

```json
{
  "kind": "external_agent",
  "runtime_id": "external_agent",
  "external_agent_id": "codex"
}
```

Do not add a new `SessionRuntime*` constant for every tool. New tools belong in
the external-agent registry as adapters.

## Desired Package Layout

```text
internal/externalagents/
  agent.go
  registry.go
  store.go
  builtins/
    registry.go
  docs/
    README.md
    codex-app.md
    removal-plan.md
  codexapp/
    client.go
    process.go
    types.go
```

The only Codex-specific package should be `codexapp`.

The only daemon composition code that should know which concrete adapters exist
is `internal/externalagents/builtins`. Add future adapters there as factories
instead of teaching core, setup, TUI, or Telegram about each runtime.

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
    Aliases     []string
    DisplayName string
    Installed   bool
    Enabled     bool
    AuthState   string
    Mode        string
    Path        string
    Version     string
    Detail      string
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
sessions.runtime_id = matrixclaw | external_agent
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
TUI management currently lives under:

```text
/modules -> External Agents
```

Enabled agents appear in the New Session picker. Disabled or missing agents are
visible in Modules but cannot be used to create a session.

## Session Creation Flow

New session should offer:

```text
Assistant
Codex
Claude Code
Kimi Code
OpenCode
```

Assistant sessions use normal matrixclaw providers and tools.

External-agent sessions use the common runtime path:

```text
matrixclaw session -> external agent attachment -> adapter thread/session -> turn/send
```

The UI may show friendly choices, but the core API should receive an
`external_agent_id`. That keeps session creation stable as more adapters are
added.

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
daemon bootstrap wiring for Codex app-server
API endpoint to list external agents
API endpoint to enable/disable external agents
API/session creation flow for external-agent sessions
controlplane session creation choices from enabled external-agent descriptors
TUI Modules screen for external-agent status and enable/disable
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
execute an attached external-agent run through core
run fake protocol tests
run live initialize smoke test
run live thread+turn smoke test
```

Not implemented yet:

```text
setup UI
approval request handling
tool/diff/approval event rendering beyond message/turn events
```
