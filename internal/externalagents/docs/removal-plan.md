# Removal Plan

The external-agent integration must be easy to remove.

The Codex connector is experimental and depends on an experimental Codex CLI
app-server protocol. If the protocol becomes unstable or the product direction
changes, removing it should not damage normal matrixclaw assistant sessions.

## Hard Boundary

All Codex-specific code must stay under:

```text
internal/externalagents/codexapp
```

Codex-specific documentation must stay under:

```text
internal/externalagents/docs
```

Core packages must not import `codexapp` directly. They should only use generic
interfaces from `internal/externalagents`.

## Allowed Core Touch Points

Only these generic touch points are allowed:

```text
external agent registry
external agent session store
session kind routing
setup descriptor rendering
client session creation choice
normalized event rendering
```

Do not add Codex-specific fields to shared types such as:

```text
Session.CodexThreadID
Provider.Type == codex
Run.CodexStatus
```

Use generic names:

```text
external_agent_id
external_thread_id
external_session_id
metadata_json
```

## Safe Database Strategy

Use a separate table:

```sql
external_agent_sessions
```

If the feature is removed, the table can stay as harmless legacy data. The
normal `sessions` table should remain usable.

If a migration is needed, prefer additive migrations:

```text
add sessions.kind
add external_agent_sessions
```

Avoid migrations that rewrite historical assistant sessions.

## Removal Steps

To remove Codex app-server support:

```text
1. Delete internal/externalagents/codexapp.
2. Remove the codex-app registration from the external agent registry.
3. Remove Codex from setup descriptors.
4. Remove Codex from new-session choices.
5. Keep generic external-agent interfaces if other agents use them.
6. Keep external_agent_sessions table unless a later cleanup migration is worth it.
```

If Codex is the only external agent and the whole feature should be removed:

```text
1. Delete internal/externalagents.
2. Remove the runtime branch for session kind external_agent.
3. Hide or remove external-agent setup UI.
4. Leave old DB tables alone or add a no-op cleanup note for future major versions.
```

## Runtime Fallback

If a user opens a removed or unavailable external-agent session, matrixclaw
should not crash. It should show:

```text
External agent unavailable: codex-app
```

and offer a future migration path:

```text
Create normal assistant session from local transcript
```

That migration can be implemented later. The immediate requirement is graceful
failure.

## Review Checklist

Before merging any deeper integration, check:

```text
No codexapp import outside registry/wiring.
No Codex-specific fields in core session/run/provider types.
No Codex-specific UI text outside setup/session choice and docs.
All Codex event payloads converted to normalized matrixclaw events.
Normal builds pass without Codex installed.
```
