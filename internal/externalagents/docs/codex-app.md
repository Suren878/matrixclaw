# Codex App-Server Adapter

`internal/externalagents/codexapp` runs Codex through its experimental
app-server protocol. Agent ID `codex-app`, alias `codex`. The adapter tolerates
extra fields and loose shapes because the protocol may change (for example
`thread.status` is decoded as `any`; the generated schema and the live server
disagree on its type).

## Process

```bash
codex app-server --listen stdio://
```

- One app-server process per runtime, shared by all Codex sessions. It is
  started lazily on first use, detached from any single run's context, and
  restarted on the next call if it has exited.
- Transport: newline-delimited JSON-RPC over stdin/stdout. Stderr goes to the
  daemon's stderr.
- Handshake (30 s timeout): `initialize` with
  `clientInfo: {name: "matrixclaw", version: "0"}` and
  `capabilities: {experimentalApi: true}`, then the `initialized` notification.

## Methods Used

| Method | When | Params sent |
| --- | --- | --- |
| `thread/start` | session created | `model`, `cwd`, `approvalPolicy`, `sandbox`, `config` (MatrixClaw session metadata) |
| `thread/resume` | turn start reports `no rollout found for thread id` or `thread not found`; the turn is retried once | `threadId`, `model`, `cwd`, `approvalPolicy`, `sandbox` |
| `turn/start` | each user message | `threadId`, `input: [{type: "text", text, text_elements: []}]`, `approvalPolicy`, `model` |
| `turn/interrupt` | run canceled | `threadId`, `turnId` (the active turn) |

`thread.id` is stored as `external_thread_id`, `thread.sessionId` as
`external_session_id`. When the session has no policy, the adapter uses
`approvalPolicy: never`, `sandbox: danger-full-access`. The user-facing
permission mapping is in [docs/EXTERNAL_AGENTS.md](../../../docs/EXTERNAL_AGENTS.md#permissions).

## Notifications

Only notifications for the current thread and turn are forwarded:

| Codex notification | MatrixClaw event |
| --- | --- |
| `turn/started` | `turn.heartbeat` |
| `item/agentMessage/delta` | `message.delta` |
| `item/reasoning/textDelta`, `item/reasoning/summaryTextDelta` | `reasoning.delta` |
| `item/started` / `item/completed` (tool items, see below) | `tool.started` / `tool.completed` |
| `item/commandExecution/outputDelta`, `item/fileChange/outputDelta` | `tool.output.delta` |
| `item/fileChange/patchUpdated` | `diff.updated` |
| `thread/compacted` | `turn.heartbeat` ("context compacted") |
| `error` with `willRetry` | `turn.heartbeat` |
| `error` without `willRetry` | `turn.failed` |
| `turn/completed` | `turn.completed` if status is `completed`; otherwise `turn.failed` (interrupted, failed, or unexpected status) |

The adapter itself emits `turn.started` after `turn/start` succeeds, and
`turn.failed` if the process or stream dies mid-turn. Other notifications are
ignored.

Tool items are mapped by `item.type`:

| Codex item | Tool name |
| --- | --- |
| `commandExecution` | `bash` |
| `fileChange` | `edit` |
| `mcpToolCall` | `<server>.<tool>` |
| `dynamicToolCall` | its tool name |
| `collabAgentToolCall` | its tool name (`agent_task` if none) |
| `webSearch` | `web_search` |
| `imageView` | `view_image` |
| `imageGeneration` | `image_generation` |

## Server Requests

MatrixClaw cannot ask the user mid-turn, so it answers every request at once:

| Request | Answer |
| --- | --- |
| `item/commandExecution/requestApproval`, `item/fileChange/requestApproval` | `decision: decline` |
| `execCommandApproval`, `applyPatchApproval` | `decision: denied` |
| `mcpServer/elicitation/request` | `action: decline` |
| anything else | JSON-RPC error `-32601` |

## Models

`Models()` suggests `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.3-codex`,
`gpt-5.3-codex-spark`. Any model name is passed through to Codex.

## Tests

`client_test.go` and `runtime_test.go` run against a fake JSON-RPC peer and need
no Codex install.
