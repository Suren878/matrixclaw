# Testing Strategy

MatrixClaw reset its legacy Go test suite on 2026-06-16. The old tests mostly
froze implementation details and left the important user workflows thinly
covered. New tests should be written as acceptance/use-case tests first, then
smaller contract tests only where they protect a risky boundary.

## Current CI Gate

CI currently runs:

```bash
go build ./...
go vet ./...
```

Do not add `go test ./...` back to CI until the new suite covers real product
behavior and is stable enough to be trusted.

## Test Shape

Prefer scenario names and Given/When/Then structure:

```text
Given a daemon with a fake LLM provider and an empty SQLite store
When a user creates a session and sends "hello"
Then the session has the user message, the assistant response, and a completed run
```

Tests should drive MatrixClaw through stable boundaries:

- HTTP API and daemon client behavior
- core session/run/message workflows
- fake LLM providers and fake external agents
- fake Telegram/client delivery adapters
- SQLite persistence with temporary databases
- tool approval and tool-result loops

Avoid tests that only assert private formatting, incidental helper output, or a
branch that was added once and is unlikely to fail in a way users can observe.

## First Scenarios To Rebuild

1. Session lifecycle: create, rename, list, delete or archive.
2. Model turn: user message, fake provider response, completed run, saved
   assistant message.
3. Tool approval: model requests a tool, approval is created, approve/reject
   produces the expected run state and messages.
4. Client delivery: Telegram or another client receives a daemon response for a
   bound session.
5. Subagent flow: parent run creates a subagent task, child completes, parent
   receives the result.
6. Persistence restart: sessions, messages, runs, and deliveries survive a store
   reopen.
7. Provider error handling: retryable and non-retryable fake provider errors map
   to the correct run status and user-visible error.

## Contract Tests

Add focused contract tests after the acceptance scenarios exist. Good candidates:

- provider adapters: request shape, streaming chunks, tool calls, auth headers,
  and error mapping through mock HTTP servers
- automation scheduler: due jobs, missed jobs, and visible failure logging
- SQLite stores: migrations and persistence invariants that user workflows rely
  on
