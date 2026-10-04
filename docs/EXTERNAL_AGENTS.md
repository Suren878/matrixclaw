# External Agents

An external agent is a coding-agent CLI that MatrixClaw runs inside one of its
sessions. MatrixClaw keeps the session, transcript, client bindings and event
display; the agent runs its own model, tools and sandbox. External agents are
not LLM providers and do not use MatrixClaw's tools or permission rules.

Built-in adapters:

| Agent | ID (aliases) | Runs |
| --- | --- | --- |
| Codex | `codex-app` (`codex`) | `codex app-server --listen stdio://`, one long-lived process |
| Claude Code | `claude-code` (`claude`) | `claude -p --output-format stream-json ...`, one process per turn |

## Setup

1. Install the agent CLI and log in to it the way its vendor documents. The
   daemon finds the binary on `PATH` or in the usual npm, nvm, asdf and Homebrew
   locations. macOS `.app` bundle paths are not accepted; point at the CLI
   binary.
2. Enable it (owner only):

   ```text
   /modules agents                        list, enable, disable, set path
   /modules agents enable codex
   /modules agents codex path /opt/codex/bin/codex
   ```

   The settings are stored in setup config under
   `modules.external_agents.<id>` as `enabled` and `path`. The API equivalent is
   `PATCH /v1/external-agents/{id}` with `{"enabled": true, "path": "..."}`.
3. Check status:

   ```bash
   matrixclaw agents        # installed / enabled / not installed
   ```

An agent shows as enabled only when it is both enabled and installed.

## Starting a Session

- TUI or Telegram owner chat: `/new` and pick the agent. Only installed and
  enabled agents are listed, and only for the owner.
- CLI: `matrixclaw agents start codex [DIR]` creates a session in `DIR`
  (default: current directory).
- API: `POST /v1/sessions` with `"external_agent_id": "codex"` (or `"claude"`).

Each message you send becomes one turn of the agent. Its replies, reasoning,
tool calls and file changes appear in the normal transcript. Interrupting a
run interrupts the agent's turn. When the daemon restarts, the next message
resumes the same Codex thread or Claude Code session.

## Permissions

A session created without a permission mode starts in `full_auto`. The session
mode maps to the agent's own settings:

| Session mode | Codex `approvalPolicy` / `sandbox` | Claude Code `--permission-mode` |
| --- | --- | --- |
| `full_auto` | `never` / `danger-full-access` | `auto` |
| `accept_edits` | `on-request` / `workspace-write` | `acceptEdits` |
| `default` | `on-request` / `read-only` | `default` |
| read-only subagent | `never` / `read-only` | `dontAsk` |

A mode change applies from the next turn. For Codex only the approval policy
changes there; the sandbox changes when the daemon next resumes the thread.

MatrixClaw does not relay an agent's approval requests to you. Codex approval
requests are declined automatically and the turn continues, so in `default` and
`accept_edits` anything Codex would ask about is refused. Claude Code runs
non-interactively and cannot ask either.

Security limits:

- In `full_auto` Codex runs without a sandbox and Claude Code in its
  autonomous `auto` mode; both act as the daemon's OS user.
- Only the owner can start an external-agent session, change its permission
  mode, or bind to or send into one. Members, guests and other Telegram chats
  are refused; the same applies to any session in `full_auto`.
- MatrixClaw permission rules do not apply to an external agent's tools.

## Subagents

Assistant sessions have an `agent` tool that hands a bounded task to a child
agent. The child runs in a hidden session and the parent receives only its
result. The system prompt tells the model which child runtimes are enabled.

| Parameter | Meaning |
| --- | --- |
| `description` (required) | Short label for the task, 3-5 words. |
| `prompt` (required) | Everything the child needs: goal, context, what to report. |
| `background` | Start the child as a background task; its result reaches the parent as a message when it ends. At most `daemon.background_agents` (default 4) run per session. |
| `isolation` | `shared` (default): the parent's directory, one writing child at a time. `worktree`: a git worktree of its own, so several run at once. |
| `readonly` | Read-only tools only; read-only children run in parallel. |
| `runtime` | `matrixclaw` (default), `codex`, `claude`, or `auto` (same as `matrixclaw`). |
| `model` | Optional model for the child runtime. |

A native child starts from the call's prompt alone: it does not inherit the
parent's history, todo list, skills or memory prompt. It keeps its own todo
list (`todo_write`) and may run background commands, which stop when it
finishes. It cannot use `agent`, `await`, `memory`, `session_search`,
`text_to_speech`, or any other automation, storage or skill tools (reminders,
scheduled tasks, calls, storage and file delivery, `skill_*`). Child runs have
their own budget (`daemon.budgets.subagent`, default 100 steps and 1 h active
time); a blocking child's time does not count against the parent's.

When a child asks for an approval, the request appears in the parent's session
and chat; the child continues with the decision.

External children (`codex`, `claude`) need the agent enabled and run in
`full_auto`. With `readonly: true` they use the read-only mapping above: Codex
refuses writes without asking; Claude Code's `dontAsk` denies every call that
would prompt (edits and shell commands outside its read-only set) while reads
still work, and tools pre-approved by the user's Claude Code
`permissions.allow` rules still run. MatrixClaw refuses any approval a
read-only child asks for ("read-only subagent cannot run <tool>").

Children and external-agent sessions cannot start subagents.

Contributor notes on the adapter boundary live in
[`internal/externalagents/docs`](../internal/externalagents/docs/README.md).
