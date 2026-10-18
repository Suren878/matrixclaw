# Hooks

Hooks are shell commands the daemon runs at fixed points of a native run:
before and after a tool call, when a run would stop, when a run starts and
before a summary. They speak the Claude Code hook protocol, so most Claude Code
hook scripts work unchanged. MatrixClaw also checks every file an edit
changes (see [Diagnostics after edits](#diagnostics-after-edits)), which needs
no configuration.

Code: `internal/hooks` (runner, protocol, project trust, diagnostics); core
runs tool hooks in `core.permitCall` and `core.afterToolUse`, the engine reaches
the rest through its `Hooks` port.

## Configuring hooks

In `setup.json` (`~/.config/matrixclaw/setup.json`), owned by the owner:

```json
"hooks": [
  {"event": "PreToolUse", "matcher": "bash", "command": "~/.config/matrixclaw/hooks/no-prod.sh"},
  {"event": "PostToolUse", "matcher": "edit|multiedit|write", "command": "npx prettier --write \"$MATRIXCLAW_FILE\" >/dev/null"},
  {"event": "Stop", "command": "go test ./... >/tmp/t.log 2>&1 || { tail -40 /tmp/t.log >&2; exit 2; }", "timeout": 300}
]
```

| Key | Meaning |
|---|---|
| `event` | one of `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `SessionStart`, `PreCompact`, `Notification` |
| `matcher` | what the hook runs for (below); empty or `*` is everything; not allowed for `Stop`/`SubagentStop` |
| `command` | a bash command line |
| `timeout` | seconds, default 600 |

Loading is strict: an unknown key, an unknown event, a matcher that is not a
valid regular expression or an empty command makes setup.json fail to load,
naming the key. The daemon reads setup.json at start and on every reload
(`POST /v1/admin/reload`, `/restart`).

**Matchers** select by tool name for `PreToolUse`/`PostToolUse`, by source for
`SessionStart` (`startup`, `resume`), by trigger for `PreCompact` (`auto`,
`manual`) and by notification type for `Notification` (`permission_prompt`).
Letters, digits, `_` and `|` are exact names (`edit|write`); anything else is a
regular expression that must match the whole name (`mcp\..*`).

Tool names are MatrixClaw's: `bash`, `read`, `write`, `edit`, `multiedit`,
`grep`, `glob`, `ls`, `agent`, `web_fetch`, MCP tools as `mcp.<server>.<tool>`,
and so on. A Claude Code matcher such as `Bash` or `Edit|Write` must be written
in these names; the inputs use the same field names (`command`, `file_path`,
`old_string`, `new_string`, `content`), so scripts that read them work.

### Project hooks

A project can keep hooks in `.matrixclaw/hooks.json`:

```json
{"hooks": [
  {"event": "PostToolUse", "matcher": "edit|multiedit|write", "command": "$CLAUDE_PROJECT_DIR/scripts/fmt.sh"}
]}
```

The file is found from the session's working directory up to the root of its
git repository (outside a repository only the working directory is looked at).
It is read strictly, like setup.json.

Project hooks run only after the owner trusted the file: `/hooks` lists
setup.json's hooks and the project's with their state, and `/hooks trust`
shows the commands and asks the owner to confirm. Trust is kept per file and
SHA256 in `hooks-trust.json` next to the database; any change to the file
takes it away until the owner trusts it again. Untrusted or invalid project
hooks are listed and logged once, never run. Only the owner can trust
(`POST /v1/sessions/{id}/hooks/trust` is an owner route), so a member or
guest in Telegram never does; the agent's file tools cannot write the trust
file either. Scripts a trusted file calls are not covered by its hash.

## How a hook runs

- `bash -c <command>` in the session's working directory, in its own process
  group (killed at the timeout), with the environment of the agent's shell
  commands (the login shell's, without API keys and `MATRIXCLAW_*`) plus:
  - `MATRIXCLAW_SESSION_ID`
  - `MATRIXCLAW_TOOL`: the tool name (tool events)
  - `MATRIXCLAW_FILE`: the resolved path a file tool targets (the first one for `apply_patch`)
  - `CLAUDE_PROJECT_DIR`: the project directory (the directory holding
    `.matrixclaw/` for project hooks, else the working directory)
- Hooks of an event run one after another: setup.json's first, then the
  project's.
- stdin is one JSON object:

```json
{
  "session_id": "ses_…", "run_id": "run_…", "hook_event_name": "PreToolUse",
  "cwd": "/home/me/project", "permission_mode": "default",
  "tool_name": "bash", "tool_use_id": "call_…", "tool_input": {"command": "git push"}
}
```

`PostToolUse` adds `tool_response` (`{"content", "status"}`); `Stop` and
`SubagentStop` add `stop_hook_active` and `last_assistant_message`;
`SessionStart` adds `source`, `PreCompact` `trigger`, `Notification` `message`
and `notification_type`.

### Exit codes and output

| Exit | Meaning |
|---|---|
| 0 | success; stdout is read as JSON when it is a JSON object, else as plain text |
| 2 | block: stderr goes to the model (what it does depends on the event) |
| other | a warning in the daemon log; the run goes on |

A timeout is a warning too. JSON fields read on exit 0:

| Field | Events | Effect |
|---|---|---|
| `hookSpecificOutput.permissionDecision` | PreToolUse | `allow`, `deny` or `ask` |
| `hookSpecificOutput.permissionDecisionReason` | PreToolUse | the model reads it on deny; the approval shows it on ask |
| `hookSpecificOutput.updatedInput` | PreToolUse | replaces the call's arguments (a JSON object) |
| `hookSpecificOutput.additionalContext` | PreToolUse, PostToolUse, SessionStart | text for the model, at most 10,000 characters |
| `decision: "block"` + `reason` | PreToolUse (deny), PostToolUse (reason to the model), Stop (keep going) | |
| `decision: "approve"` | PreToolUse | allow (older Claude Code form) |
| `continue: false` + `stopReason` | PreToolUse: blocks the call; Stop: lets the run end whatever other hooks said; PostToolUse: stopReason to the model | |
| `systemMessage` | all | written to the daemon log |

### Events

**PreToolUse** runs before a call, after the permission rules. A stored deny
rule always wins and the hook does not even run. Otherwise exit 2 or `deny`
blocks the call (the model reads "Blocked by hook: <reason or stderr>"), `ask`
makes it wait for approval even in `full_auto`, `allow` runs it without asking
(over ask rules and the mode). Across several hooks deny wins over ask over
allow. Rewritten input (`updatedInput`) is checked like the model's own and
meets the permission rules again, so a hook cannot rewrite a call into one a
deny rule forbids. A call that waits for approval meets its hooks again when it
runs after the grant.

**PostToolUse** runs after a call that did not fail. Plain stdout, exit 2's
stderr, `reason` of a block and `additionalContext` are appended to the result
as `<hook event="PostToolUse">…</hook>`. The result's status stays.

**Stop** runs when the model replies without tool calls and the run would end
(**SubagentStop** for a subagent's run). Exit 2 or `decision: "block"` keeps it
going: the engine adds a note with the reason and asks the model again. Stop
hooks hold a run back at most 5 times (`stop_hook_active` is true after the
first time); they run only while budget remains, after the built-in todo and
verification checks.

**SessionStart** runs when a run begins (not when a parked or restarted run
resumes): source `startup` for a session's first run, `resume` for later ones.
Its stdout or `additionalContext` reaches the model as a hidden note.

**PreCompact** runs before a summary: trigger `auto` inside a run, `manual`
for `/context compact`. It only observes.

**Notification** runs, in the background, when a call waits for approval
(`notification_type: "permission_prompt"`). It only observes.

## Examples

Block pushes to main (Claude Code compatible; needs `jq`):

```bash
#!/usr/bin/env bash
cmd=$(jq -r '.tool_input.command // empty')
if [[ $cmd == *"git push"*main* ]]; then
  echo "Do not push to main; open a branch instead." >&2
  exit 2
fi
```

```json
{"event": "PreToolUse", "matcher": "bash", "command": "~/.config/matrixclaw/hooks/no-main-push.sh"}
```

Allow read-only git commands without asking (JSON decision):

```bash
#!/usr/bin/env bash
cmd=$(jq -r '.tool_input.command')
case $cmd in
  "git status"*|"git diff"*|"git log"*)
    echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}' ;;
esac
```

Format Go files after edits:

```json
{"event": "PostToolUse", "matcher": "edit|multiedit|write", "command": "case \"$MATRIXCLAW_FILE\" in *.go) gofmt -w \"$MATRIXCLAW_FILE\";; esac"}
```

Do not stop while tests fail:

```json
{"event": "Stop", "command": "make test >/tmp/mc-test.log 2>&1 || { tail -40 /tmp/mc-test.log >&2; exit 2; }", "timeout": 600}
```

Give every new session the git state:

```json
{"event": "SessionStart", "matcher": "startup", "command": "git status --short --branch 2>/dev/null | head -30"}
```

Desktop notification when an approval waits:

```json
{"event": "Notification", "command": "notify-send MatrixClaw \"$(jq -r .message)\""}
```

## Diagnostics after edits

After a successful `edit`, `multiedit`, `write` or `apply_patch` (not one
that changed nothing), the edited files (the first five of a patch) are
checked and up to 30 lines that name them are appended to the result:

```text
Diagnostics after edit:
/home/me/project/parse.go:42:9: cannot use "one" (untyped string constant) as int value in return statement
```

Built-in checks, each skipped when its tool is not installed:

| Extension | Check | Runs in |
|---|---|---|
| `.go` | `gofmt -l -e` (syntax, formatting) and `go vet .` | the file's directory |
| `.ts`, `.tsx` | `tsc --noEmit -p .` (`node_modules/.bin/tsc` or `tsc`) | the nearest directory with `tsconfig.json`; skipped without one |
| `.py` | Python's `compile()` (what `py_compile` does, without writing `.pyc`) | the file's directory |
| `.rs` | `cargo check --message-format short`, errors only | the nearest directory with `Cargo.toml` |
| `.json` | parsed in the daemon (tsconfig, jsconfig and `.vscode` files are skipped, they allow comments) | |

The checks of one call run for at most 20 seconds together, under the call's working-directory key,
so edits in one directory do not check concurrently. Only lines naming the
edited file (by absolute path or a path relative to a directory above it) and
the indented lines after them are kept; errors in other files are not reported.
A check never blocks or undoes the edit, and a failing or slow check adds
nothing.

Override or turn checks off in setup.json:

```json
"diagnostics": {
  "commands": {
    ".py": "ruff check --output-format concise \"$MATRIXCLAW_FILE\"",
    ".rs": ""
  }
}
```

A command replaces the built-in check of its extension and runs in the
session's working directory with the hook environment; an empty command turns
that extension off. `"disabled": true` turns all checks off. Extension keys are
lowercase with their dot (`.go`).
