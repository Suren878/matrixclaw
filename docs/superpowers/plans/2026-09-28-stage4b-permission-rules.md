# Stage 4b — Permission Rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every tool call — engine runs, the API, voice, the MCP server, approval and recovery replays — is decided in one place by allow/ask/deny rules matched against the call's subject (bash command, absolute path, domain, MCP `server__tool`), then by the session's mode preset, then by the tool's own default; approvals offer "Allow", "Always allow" (a suggested rule for this session or, for the owner, every session) and "Deny with reason"; rules are listed, added and deleted with `/permissions`; the in-memory session auto-approvals of the TUI and Telegram are gone.

**Architecture:** A new leaf package `internal/permission` holds rules, subject matching, bash parsing (`mvdan.cc/sh/v3/syntax`), the mode presets and rule suggestions. Executors name their calls' subjects through the optional `tools.SubjectProvider`; `core.ToolExecutor.Subject` exposes them. `core.checkPermission` loads the global rules and those of the session and its parents from `permission_rules`, and is asked by `coreTools.Authorize` (deny before the call runs) and by `executeToolWithGrant` (every pipeline). A call that asks carries a suggested rule to its approval; `ResolveApproval` with `Always` saves it. Clients offer the new buttons; the shared controlplane manages rules, with global ones reserved for the owner.

**Tech Stack:** Go 1.26, `mvdan.cc/sh/v3` v3.14.1 (new, pure Go), SQLite (modernc), bubbletea v2 TUI, Telegram Bot API.

**Prerequisite:** stage 4a (`docs/superpowers/plans/2026-09-28-stage4a-denial-and-barrier.md`) is committed; this plan builds on its `ApprovalResolveRequest`, `recordApprovalDecision`, `passDecisionToSubagent`, `/approval deny`, `denyingApproval` and the barrier engine.

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session may commit in parallel: **locate code by function name, not line number**, run `git status --short` before each commit and stage only the paths listed in the task with explicit `git add <paths>` (never `-A`, `-u` or directories). `git rm` for deleted files.
- Run `gofmt -w` on every Go file you touch (code blocks below are not guaranteed to be column-aligned). Every commit must pass `go build ./... && go vet ./... && go test ./...` — run the full suite before committing.
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger. When a pre-existing test fails only because an expectation this plan changes on purpose, update that expectation and name it in your report.
- Every code block below was built and run in a scratch copy on top of stage 4a: the full suite passed after each task, also with `-race` for `internal/core`, `internal/agent`, `internal/permission` and `clients/telegram`.

## Decisions taken in this plan (owner should know)

1. **`internal/permission` is a leaf**: it imports only the standard library and `mvdan.cc/sh/v3/syntax`. It knows rules (`Rule{ID, Tool, Pattern, Effect allow|ask|deny, Scope session|global, SessionID, CreatedAt}`), subjects, `Evaluate`, `Preset`, `Suggest` and `ParseLine`; it knows no tool, store or session.
2. **Subjects.** Instead of the spec's `PermissionSubject(args) (string, error)`, executors implement the optional `tools.SubjectProvider` — `PermissionSubject(call tools.Call) permission.Subject` with a kind (`file`, `directory`, `command`, `domain`, `name`) so rules know how to match, and no error (invalid arguments give the zero subject; the tool reports the error itself). Implemented by bash (the command), read/write/edit/multiedit (the file, absolute, symlinks resolved), glob/grep/ls (the directory searched; no path = the working directory), web_fetch (the lower-case host) and every MCP tool (`server__tool`). Tools without a subject (memory, storage, telephony, …) match only rules for the whole tool.
3. **Rule tool names** are tool IDs, except that every MCP tool is `mcp` (its subject names server and tool, so `mcp: browser__*` covers a server) and `multiedit` follows `edit` rules. `*` is every tool; an empty pattern or `*` is every call of the tool.
4. **Patterns.** Paths: `*` and `?` within one path element, `**` across elements, `/dir/**` also matches `/dir`; saved absolute (relative to the session's working directory, `~/` expanded, symlinks resolved before the first glob). Bash: `go test:*` matches commands starting with those words, `git status` only that command. Domains: glob, case-insensitive (`*.example.com` = its subdomains). MCP: glob over `server__tool`.
5. **Order:** deny → ask → allow → mode preset → tool default. A bash deny/ask rule matches when any simple command of the line matches (commands inside `$(…)` included). A bash allow needs an allow rule for **every** simple command and no risky construct; a rule for the whole tool (`bash` / `*`, and `full_auto`) allows risky lines too. **Risky** (never allowed by a pattern rule, so the user is asked): output redirection to a file (`/dev/null` and `>&2`-style descriptors excepted), any `VAR=…` assignment, `export`/`declare`/`local`, command and process substitution, function declarations, `$VAR`/`$((…))` words, a non-literal command name, wrapper commands (`sudo`, `env`, `xargs`, `sh`/`bash`/`zsh`, `eval`, `exec`, `source`, `nohup`, `timeout`, `nice`, `time`, `watch`, `strace`, …), `-exec`/`-execdir`/`-ok`/`-okdir` and `-toolexec`/`--exec`/`--to-command`/`--checkpoint-action`/`--upload-pack`/`--receive-pack`/`--rsh` flags, and lines that do not parse (deny rules still see their whitespace-separated words). Wrappers and `$VAR` words go beyond the spec's list: each of them can run or smuggle another command.
6. **Presets:** `default` adds nothing; `accept_edits` allows `write` and `edit` (so multiedit) under the working directory with symlinks resolved — the old accept-edits behaviour; `full_auto` allows every call. Deny and ask rules beat presets: **a deny rule now blocks even in `full_auto`**, which used to approve everything.
7. **One evaluation place:** `core.checkPermission`. `coreTools.Authorize` asks it for deny rules before a call is journaled (the model reads `Blocked by rule <tool: pattern>`); `executeToolWithGrant` — the engine's `Execute` and `ExecuteTool` for the API, voice, the MCP server, approval replays and recovery replays — asks it again with the call: deny → blocked result, even for a granted replay; a grant, an allow rule or the preset → the tool runs approved; an ask rule → approval (the tool's own preview when it asks by itself, otherwise a plain request with action `ask_rule`); nothing → the tool's dry run decides, as before. An ask rule on a tool that asks by itself relies on that dry run: if the dry run finishes without asking (e.g. `memory` list), its result stands.
8. **An engine call that `Authorize` rejects is always an error result now**, also for a granted call (stage 4a failed the run there): a deny rule added after a grant must not kill the run.
9. **Suggested rule and "Always allow".** A call that asks gets `permission.Suggest`'s rule: first word plus a subcommand-looking second word (`go test:*`, `git status:*`, `ls:*`), the file's directory (`/abs/internal/**`), the searched directory, the domain, the MCP tool, or the whole tool for subject-less tools. None for a risky or compound bash line, for ask-rule approvals (an allow rule could never beat the ask rule) and for recovery "retry" approvals. The suggestion is stored with the approval (`approvals.suggestion_json`) and sent to clients (`Approval.Suggestion`, `PermissionRequest.Suggestion`). `ApprovalResolveRequest.Always` (`session` | `global`) grants and saves the rule as an allow rule; a bridged approval carries the child's suggestion and the rule goes to the approval's own session — the **parent** — while the child gets a plain grant. Other open approvals the new rule would cover are not auto-resolved.
10. **Storage:** `permission_rules(id, tool, pattern, effect, scope, session_id, created_at)`; global rules have `session_id NULL`, session rules are deleted with their session (FK cascade). The rules of a session are the global ones plus its own plus its parents' (subagents inherit).
11. **Rules API:** `GET`/`POST /v1/sessions/{id}/permission-rules`, `DELETE /v1/permission-rules/{id}`.
12. **`/permissions`** (shared by TUI and Telegram): the picker lists the three modes and every rule of the session with its scope (global / this session / parent session); choosing a rule asks to delete it. Also `/permissions add allow|ask|deny <tool> [pattern] [global]` — the only way to create deny and ask rules — and `/permissions delete <rule id>`. Only the owner creates or deletes global rules: `clientruntime.ControlplaneRuntime.Owner` is true in the TUI and in the Telegram owner's private chat (`AllowedUserID` set and equal to the chat); never for guests, never when `AllowedUserID` is unset. The daemon API trusts its token holders and does not check ownership itself.
13. **Approval UI.** TUI: Allow / Always (session) / Always (global) / Deny / Deny with reason (keys `a`/`s`/`g`/`d`/`r`), with an "Always: bash: go test:*" line; the "Always" buttons appear only with a suggestion. Telegram: ✅ Allow · ♾ Always: session · 🌐 Always: global (owner chat only) · ❌ Deny · ✍️ Deny with reason, and an "Always: …" text line. Deleted: TUI `autoEditSessions` and "Allow Session", `surfacepermission.CanAllowSessionApproval`, `clients/telegram/auto_approval.go` with `Worker.autoEdits`, `core.PermissionModeForSessionApproval`, and in core `autoApprovesTool`, `acceptEditsAllows`, `mutationPathWithinRoot`, `executePreparedTool`.
14. **Dependency:** `mvdan.cc/sh/v3 v3.14.1`, the newest release (`go list -m -versions` on 2026-09-28). `go get` also raises `golang.org/x/sys` v0.46.0 → v0.47.0 (required by sh); `go.sum` gains checksum lines for `github.com/go-quicktest/qt` and `github.com/rogpeppe/go-internal` v1.15.0.
15. The iOS package is unchanged: the new approval fields are additive JSON.

Out of scope: parallel scheduling of read-only calls (stage 4c), auto-resolving open approvals covered by a new rule, a rule editor beyond `/permissions add`.

## File structure

Created:
- `internal/permission/{permission,match,bash}.go`, `internal/permission/{permission,bash}_test.go`
- `internal/store/sqlite_permission_rules.go`, `internal/store/sqlite_permission_rules_test.go`
- `internal/tools/permission_subject.go`, `internal/tools/permission_subject_test.go`
- `internal/webtools/permission_subject.go`, `internal/webtools/permission_subject_test.go`
- `internal/mcp/permission_subject.go`, `internal/mcp/permission_subject_test.go`
- `internal/core/permission_rules.go`, `internal/core/tool_permissions_test.go`
- `internal/api/permission_rules.go`, `internal/api/permission_rules_test.go`
- `internal/daemonclient/permission_rules.go`
- `internal/controlplane/permissions_test.go`

Deleted: `clients/telegram/auto_approval.go`.

Modified:
- `go.mod`, `go.sum`
- `internal/tools/types.go`; `internal/store/{schema,sqlite_approvals_files}.go` and `sqlite_approvals_test.go`
- `internal/core/{ports,types_session,types_approval,contracts,permissions,tool_permissions,tool_call_run,tool_call_approval,tool_approvals,agent_tools,subagents}.go`, `internal/core/subagents_approval_test.go`
- `internal/agent/tools.go`; `internal/daemoncmd/tool_visibility.go`
- `internal/api/{server,sessions}.go`; `internal/clientruntime/{controlplane_runtime,state}.go`; `internal/controlplane/{dispatcher,permissions}.go`
- TUI: `clients/terminal/ui/surface/permission/{types,helpers}.go`, `clients/terminal/ui/surface/dialog/{permissions,permissions_keys,permissions_events,permissions_render,permissions_test}.go`, `clients/terminal/chat/viewmodel/surface_adapter.go`, `clients/terminal/chat/runtime/{runtime,app,app_update,app_server_commands,app_approvals,app_dialog_actions}.go`
- Telegram: `clients/telegram/{constants,keyboards,callbacks,run_render,render_helpers,helpers,command_runtime,commands,prompts,messages,worker_types,worker_constructor}.go`, `clients/telegram/approvals_test.go`
- Docs: `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`, `docs/TELEGRAM.md`

---
### Task 1: `internal/permission` — rules, subjects, bash parsing, presets, suggestions

**Files:**
- Create: `internal/permission/permission.go`, `internal/permission/match.go`, `internal/permission/bash.go`
- Create: `internal/permission/permission_test.go`, `internal/permission/bash_test.go`
- Modify: `go.mod`, `go.sum` (new dependency `mvdan.cc/sh/v3 v3.14.1`)

- [ ] **Step 1: Write the failing tests**

`internal/permission/bash_test.go`:

```go
package permission

import (
	"reflect"
	"testing"
)

func TestParseLineFindsEverySimpleCommand(t *testing.T) {
	for _, tc := range []struct {
		line  string
		want  [][]string
		risky bool
	}{
		{"go test ./...", [][]string{{"go", "test", "./..."}}, false},
		{"go vet ./... && go test ./... | tee /dev/null", [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}, {"tee", "/dev/null"}}, false},
		{"if true; then ls -la; fi", [][]string{{"true"}, {"ls", "-la"}}, false},
		{`'r'\m -rf "/tmp/x"`, [][]string{{"rm", "-rf", "/tmp/x"}}, false},
		{"go test ./... 2>&1 >/dev/null", [][]string{{"go", "test", "./..."}}, false},
		{"echo hi > out.txt", [][]string{{"echo", "hi"}}, true},
		{"echo hi >> out.txt", [][]string{{"echo", "hi"}}, true},
		{"cat <<EOF\nhi\nEOF", [][]string{{"cat"}}, false},
		{"GOFLAGS=-x go build", [][]string{{"go", "build"}}, true},
		{"echo $(rm -rf /)", [][]string{{"echo", ""}, {"rm", "-rf", "/"}}, true},
		{"diff <(ls a) b", [][]string{{"diff", "", "b"}, {"ls", "a"}}, true},
		{`find . -name x -exec rm {} \;`, [][]string{{"find", ".", "-name", "x", "-exec", "rm", "{}", ";"}}, true},
		{"go build -toolexec=/tmp/x ./...", [][]string{{"go", "build", "-toolexec=/tmp/x", "./..."}}, true},
		{"sudo ls", [][]string{{"sudo", "ls"}}, true},
		{"xargs rm < files", [][]string{{"xargs", "rm"}}, true},
		{"ls $HOME", [][]string{{"ls", ""}}, true},
		{"export X=1", nil, true},
		{"f() { rm -rf /; }", [][]string{{"rm", "-rf", "/"}}, true},
		{"echo 'unterminated", [][]string{{"echo", "'unterminated"}}, true},
		{"", nil, false},
	} {
		got := ParseLine(tc.line)
		if !reflect.DeepEqual(got.Commands, tc.want) || got.Risky != tc.risky {
			t.Errorf("ParseLine(%q) = %q risky=%v, want %q risky=%v", tc.line, got.Commands, got.Risky, tc.want, tc.risky)
		}
	}
}
```

`internal/permission/permission_test.go`:

```go
package permission

import "testing"

func rule(effect Effect, tool, pattern string) Rule {
	return Rule{Tool: tool, Pattern: pattern, Effect: effect, Scope: ScopeSession}
}

func command(line string) Request {
	return Request{Tool: "bash", Subject: Subject{Kind: KindCommand, Value: line}}
}

func file(tool, path string) Request {
	return Request{Tool: tool, Subject: Subject{Kind: KindFile, Value: path}}
}

func TestEvaluateOrdersDenyAskAllowThenPreset(t *testing.T) {
	for _, tc := range []struct {
		name   string
		req    Request
		rules  []Rule
		preset []Rule
		want   Effect
		rule   string
	}{
		{"no rule leaves the tool default", command("go test ./..."), nil, nil, "", ""},
		{"allow by prefix", command("go test ./..."), []Rule{rule(Allow, "bash", "go test:*")}, nil, Allow, "bash: go test:*"},
		{"exact pattern needs the exact command", command("git status -s"), []Rule{rule(Allow, "bash", "git status")}, nil, "", ""},
		{"deny beats allow", command("rm -rf /tmp/x"), []Rule{rule(Allow, "bash", "*"), rule(Deny, "bash", "rm:*")}, nil, Deny, "bash: rm:*"},
		{"ask beats allow", file("read", "/home/u/.env"), []Rule{rule(Allow, "read", "/home/u/**"), rule(Ask, "read", "/home/u/.env")}, nil, Ask, "read: /home/u/.env"},
		{"deny beats ask", file("read", "/home/u/.ssh/id"), []Rule{rule(Ask, "read", "/home/u/**"), rule(Deny, "read", "/home/u/.ssh/**")}, nil, Deny, "read: /home/u/.ssh/**"},
		{"deny inside a substitution", command("echo $(rm -rf /)"), []Rule{rule(Deny, "bash", "rm:*")}, nil, Deny, "bash: rm:*"},
		{"every simple command must be allowed", command("go test ./... && rm x"), []Rule{rule(Allow, "bash", "go test:*")}, nil, "", ""},
		{"all simple commands allowed", command("go vet ./... && go test ./..."), []Rule{rule(Allow, "bash", "go vet:*"), rule(Allow, "bash", "go test:*")}, nil, Allow, "bash: go vet:*"},
		{"redirect to a file falls back", command("go test ./... > out.txt"), []Rule{rule(Allow, "bash", "go test:*")}, nil, "", ""},
		{"leading assignment falls back", command("CGO_ENABLED=0 go test ./..."), []Rule{rule(Allow, "bash", "go test:*")}, nil, "", ""},
		{"exec flag falls back", command("find . -exec rm {} ;"), []Rule{rule(Allow, "bash", "find:*")}, nil, "", ""},
		{"a rule for the whole tool allows risky lines", command("echo x > out.txt"), []Rule{rule(Allow, "bash", "")}, nil, Allow, "bash"},
		{"preset after rules", file("edit", "/work/a.go"), nil, Preset(ModeAcceptEdits, "/work"), Allow, "edit: /work/**"},
		{"preset stops at the root", file("edit", "/etc/passwd"), nil, Preset(ModeAcceptEdits, "/work"), "", ""},
		{"rules before the preset", command("rm -rf /"), []Rule{rule(Deny, "bash", "rm:*")}, Preset(ModeFullAuto, "/work"), Deny, "bash: rm:*"},
		{"full auto allows the rest", command("echo x > out.txt"), nil, Preset(ModeFullAuto, "/work"), Allow, "*"},
		{"rules name their tool", file("read", "/work/a.go"), []Rule{rule(Deny, "edit", "/work/**")}, nil, "", ""},
		{"tool star covers every tool", file("read", "/work/a.go"), []Rule{rule(Deny, "*", "")}, nil, Deny, "*"},
		{"subject-less call matches only whole-tool rules", Request{Tool: "memory"}, []Rule{rule(Deny, "memory", "x*"), rule(Allow, "memory", "")}, nil, Allow, "memory"},
		{"domain without case", Request{Tool: "web_fetch", Subject: Subject{Kind: KindDomain, Value: "Docs.Example.com"}}, []Rule{rule(Deny, "web_fetch", "*.example.com")}, nil, Deny, "web_fetch: *.example.com"},
		{"mcp name glob", Request{Tool: "mcp", Subject: Subject{Kind: KindName, Value: "browser__browser_click"}}, []Rule{rule(Allow, "mcp", "browser__*")}, nil, Allow, "mcp: browser__*"},
	} {
		got := Evaluate(tc.req, tc.rules, tc.preset)
		if got.Effect != tc.want || (tc.want != "" && got.Rule.String() != tc.rule) {
			t.Errorf("%s: verdict = %s by %q, want %s by %q", tc.name, got.Effect, got.Rule.String(), tc.want, tc.rule)
		}
	}
}

func TestPathGlobs(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"/work/**", "/work", true},
		{"/work/**", "/work/a/b.go", true},
		{"/work/**", "/workshop/a.go", false},
		{"/work/*.go", "/work/a.go", true},
		{"/work/*.go", "/work/sub/a.go", false},
		{"/work/**/*.go", "/work/a.go", true},
		{"/work/**/*.go", "/work/sub/deep/a.go", true},
		{"/work/?.go", "/work/a.go", true},
		{"/work/?.go", "/work/ab.go", false},
		{"/home/ü/**", "/home/ü/x", true},
	} {
		if got := matchSubject(tc.pattern, Subject{Kind: KindFile, Value: tc.path}); got != tc.want {
			t.Errorf("%q vs %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestSuggestNamesTheNarrowRule(t *testing.T) {
	for _, tc := range []struct {
		req  Request
		want string
		ok   bool
	}{
		{command("go test ./internal/..."), "bash: go test:*", true},
		{command("git status"), "bash: git status:*", true},
		{command("ls -la"), "bash: ls:*", true},
		{command("cat notes.txt"), "bash: cat:*", true},
		{command("go test ./... && rm x"), "", false},
		{command("echo x > out.txt"), "", false},
		{file("edit", "/work/internal/a.go"), "edit: /work/internal/**", true},
		{Request{Tool: "grep", Subject: Subject{Kind: KindDirectory, Value: "/work"}}, "grep: /work/**", true},
		{Request{Tool: "web_fetch", Subject: Subject{Kind: KindDomain, Value: "go.dev"}}, "web_fetch: go.dev", true},
		{Request{Tool: "mcp", Subject: Subject{Kind: KindName, Value: "github__create_issue"}}, "mcp: github__create_issue", true},
		{Request{Tool: "memory"}, "memory", true},
	} {
		got, ok := Suggest(tc.req)
		if ok != tc.ok || (ok && got.String() != tc.want) {
			t.Errorf("Suggest(%+v) = %q, %v; want %q, %v", tc.req, got.String(), ok, tc.want, tc.ok)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/permission/`
Expected: FAIL — `undefined: ParseLine`, `undefined: Evaluate`, … (no package sources yet).

- [ ] **Step 3: Implement the package**

`internal/permission/permission.go`:

```go
// Package permission decides whether a tool call runs, asks first or is blocked:
// rules matched against the call's subject, bash command-line parsing and the
// presets behind the permission modes.
package permission

import (
	"path/filepath"
	"strings"
	"time"
)

// Effect is what a rule does with the calls it matches.
type Effect string

const (
	Allow Effect = "allow"
	Ask   Effect = "ask"
	Deny  Effect = "deny"
)

// Valid reports whether e is a known effect.
func (e Effect) Valid() bool {
	return e == Allow || e == Ask || e == Deny
}

// Scope is where a rule applies: its session and that session's subagents, or
// every session.
type Scope string

const (
	ScopeSession Scope = "session"
	ScopeGlobal  Scope = "global"
)

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool {
	return s == ScopeSession || s == ScopeGlobal
}

// Rule matches the calls of Tool whose subject matches Pattern. Tool "*" is every
// tool; an empty Pattern or "*" is every call of the tool.
type Rule struct {
	ID        string    `json:"id"`
	Tool      string    `json:"tool"`
	Pattern   string    `json:"pattern,omitempty"`
	Effect    Effect    `json:"effect"`
	Scope     Scope     `json:"scope"`
	SessionID string    `json:"session_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// String is the rule as users read it, such as "bash: go test:*".
func (r Rule) String() string {
	return Suggestion{Tool: r.Tool, Pattern: r.Pattern}.String()
}

func (r Rule) wildcard() bool {
	pattern := strings.TrimSpace(r.Pattern)
	return pattern == "" || pattern == "*"
}

func (r Rule) covers(tool string) bool {
	return r.Tool == "*" || strings.EqualFold(r.Tool, tool)
}

// Suggestion is the rule an "Always allow" answer saves.
type Suggestion struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern,omitempty"`
}

func (s Suggestion) String() string {
	if pattern := strings.TrimSpace(s.Pattern); pattern != "" && pattern != "*" {
		return s.Tool + ": " + pattern
	}
	return s.Tool
}

// Kind says how rules match a subject.
type Kind string

const (
	KindFile      Kind = "file"
	KindDirectory Kind = "directory"
	KindCommand   Kind = "command"
	KindDomain    Kind = "domain"
	KindName      Kind = "name"
)

// Subject is what rules match of one call: an absolute file or directory path, a
// bash command line, a host name or an MCP "server__tool" name. The zero Subject
// is matched only by rules for the whole tool.
type Subject struct {
	Kind  Kind
	Value string
}

// Request is one tool call as rules see it; Tool is the name rules are written for.
type Request struct {
	Tool    string
	Subject Subject
}

// Verdict is how rules treat one call and the rule that decided it; the zero
// Verdict means no rule matched and the tool's own default applies.
type Verdict struct {
	Effect Effect
	Rule   Rule
}

// Evaluate applies rules to req: deny rules first, then ask rules, then allow
// rules; preset rules are consulted only when none of those decided.
func Evaluate(req Request, rules []Rule, preset []Rule) Verdict {
	var line Line
	if req.Subject.Kind == KindCommand {
		line = ParseLine(req.Subject.Value)
	}
	for _, effect := range []Effect{Deny, Ask} {
		for _, rule := range rules {
			if rule.Effect == effect && rule.covers(req.Tool) && rule.touches(req.Subject, line) {
				return Verdict{Effect: effect, Rule: rule}
			}
		}
	}
	for _, set := range [][]Rule{rules, preset} {
		if rule, ok := allowedBy(set, req, line); ok {
			return Verdict{Effect: Allow, Rule: rule}
		}
	}
	return Verdict{}
}

// touches reports whether the rule matches the subject or, for a command line,
// any one of its simple commands.
func (r Rule) touches(subject Subject, line Line) bool {
	if r.wildcard() {
		return true
	}
	if subject.Kind != KindCommand {
		return matchSubject(r.Pattern, subject)
	}
	for _, words := range line.Commands {
		if matchCommand(r.Pattern, words) {
			return true
		}
	}
	return false
}

// allowedBy finds the allow rule that lets req run. A command line needs a rule
// for the whole tool, or no risky construct and an allow rule for every one of
// its simple commands.
func allowedBy(rules []Rule, req Request, line Line) (Rule, bool) {
	for _, rule := range rules {
		if rule.Effect != Allow || !rule.covers(req.Tool) {
			continue
		}
		if rule.wildcard() || req.Subject.Kind != KindCommand && matchSubject(rule.Pattern, req.Subject) {
			return rule, true
		}
	}
	if req.Subject.Kind != KindCommand || line.Risky || len(line.Commands) == 0 {
		return Rule{}, false
	}
	var first Rule
	for i, words := range line.Commands {
		rule, ok := commandAllowedBy(rules, req.Tool, words)
		if !ok {
			return Rule{}, false
		}
		if i == 0 {
			first = rule
		}
	}
	return first, true
}

func commandAllowedBy(rules []Rule, tool string, words []string) (Rule, bool) {
	for _, rule := range rules {
		if rule.Effect == Allow && rule.covers(tool) && matchCommand(rule.Pattern, words) {
			return rule, true
		}
	}
	return Rule{}, false
}

// Suggest proposes the rule an "Always allow" answer saves for req: the command's
// first word (and subcommand), the file's directory, the searched directory, the
// domain or the MCP tool. ok is false for a risky or compound command line.
func Suggest(req Request) (Suggestion, bool) {
	suggestion := Suggestion{Tool: req.Tool}
	value := req.Subject.Value
	switch req.Subject.Kind {
	case KindCommand:
		line := ParseLine(value)
		if line.Risky || len(line.Commands) != 1 {
			return Suggestion{}, false
		}
		words := line.Commands[0]
		prefix := words[0]
		if len(words) > 1 && subcommand(words[1]) {
			prefix += " " + words[1]
		}
		suggestion.Pattern = prefix + ":*"
	case KindFile:
		suggestion.Pattern = strings.TrimSuffix(filepath.Dir(value), "/") + "/**"
	case KindDirectory:
		suggestion.Pattern = strings.TrimSuffix(value, "/") + "/**"
	case KindDomain, KindName:
		suggestion.Pattern = value
	}
	return suggestion, true
}

// subcommand reports whether a command's second word names a subcommand, as in
// "go test" or "git status", rather than a flag, a path or a file.
func subcommand(word string) bool {
	if word == "" || word[0] == '-' {
		return false
	}
	for _, r := range word {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// Mode names are the permission presets: default leaves every tool to its own
// default, accept_edits also allows file edits inside the working directory and
// full_auto allows every call no rule denies or asks for.
const (
	ModeDefault     = "default"
	ModeAcceptEdits = "accept_edits"
	ModeFullAuto    = "full_auto"
)

// Preset is the rules mode adds after a session's own rules; root is the working
// directory with symlinks resolved.
func Preset(mode string, root string) []Rule {
	switch mode {
	case ModeFullAuto:
		return []Rule{{Tool: "*", Effect: Allow}}
	case ModeAcceptEdits:
		if root == "" {
			return nil
		}
		pattern := strings.TrimSuffix(root, "/") + "/**"
		return []Rule{
			{Tool: "write", Pattern: pattern, Effect: Allow},
			{Tool: "edit", Pattern: pattern, Effect: Allow},
		}
	default:
		return nil
	}
}
```

`internal/permission/match.go`:

```go
package permission

import (
	"regexp"
	"strings"
)

// matchSubject matches a pattern against a path, domain or name subject; domains
// compare without case.
func matchSubject(pattern string, subject Subject) bool {
	if subject.Value == "" {
		return false
	}
	switch subject.Kind {
	case KindFile, KindDirectory:
		if base, ok := strings.CutSuffix(pattern, "/**"); ok && subject.Value == base {
			return true
		}
		return globMatch(pattern, subject.Value)
	case KindDomain:
		return globMatch(strings.ToLower(pattern), strings.ToLower(subject.Value))
	default:
		return globMatch(pattern, subject.Value)
	}
}

// matchCommand matches one simple command: "go test:*" matches the commands that
// start with those words, "git status" only that exact command.
func matchCommand(pattern string, words []string) bool {
	body, prefix := strings.CutSuffix(strings.TrimSpace(pattern), ":*")
	want := strings.Fields(body)
	if len(want) == 0 || len(words) < len(want) || !prefix && len(words) != len(want) {
		return false
	}
	for i, word := range want {
		if words[i] != word {
			return false
		}
	}
	return true
}

// globMatch matches a glob where "*" and "?" stay within one path element and
// "**" crosses elements ("**/" also matches no element at all).
func globMatch(pattern string, value string) bool {
	var expr strings.Builder
	expr.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == '*' && i+1 < len(runes) && runes[i+1] == '*':
			i++
			if i+1 < len(runes) && runes[i+1] == '/' {
				i++
				expr.WriteString("(?:.*/)?")
			} else {
				expr.WriteString(".*")
			}
		case runes[i] == '*':
			expr.WriteString("[^/]*")
		case runes[i] == '?':
			expr.WriteString("[^/]")
		default:
			expr.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	expr.WriteString("$")
	re, err := regexp.Compile(expr.String())
	return err == nil && re.MatchString(value)
}
```

`internal/permission/bash.go`:

```go
package permission

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Line is a parsed bash command line: the words of every simple command (those in
// substitutions too; a word that is not plain text is ""), and Risky when no
// pattern rule may allow it: it writes a file, assigns or declares variables,
// substitutes, runs a wrapper or an -exec style flag, or does not parse.
type Line struct {
	Commands [][]string
	Risky    bool
}

// wrappers run another command given as their arguments.
var wrappers = map[string]bool{
	".": true, "bash": true, "builtin": true, "busybox": true, "chroot": true, "command": true,
	"dash": true, "doas": true, "env": true, "eval": true, "exec": true, "fish": true,
	"ionice": true, "ksh": true, "ltrace": true, "nice": true, "nohup": true, "parallel": true,
	"script": true, "setsid": true, "sh": true, "source": true, "stdbuf": true, "strace": true,
	"su": true, "sudo": true, "time": true, "timeout": true, "watch": true, "xargs": true, "zsh": true,
}

var (
	execFlags        = map[string]bool{"-exec": true, "-execdir": true, "-ok": true, "-okdir": true}
	execFlagPrefixes = []string{"-toolexec", "--toolexec", "--exec", "--to-command", "--checkpoint-action", "--upload-pack", "--receive-pack", "--rsh"}
)

// ParseLine parses a bash command line; one that does not parse keeps its
// whitespace-separated words as a single command, so deny rules still see them.
func ParseLine(text string) Line {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil {
		return Line{Commands: [][]string{strings.Fields(text)}, Risky: true}
	}
	var line Line
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.CallExpr:
			line.addCall(node)
		case *syntax.Redirect:
			if writesFile(node) {
				line.Risky = true
			}
		case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.DeclClause, *syntax.FuncDecl, *syntax.CoprocClause:
			line.Risky = true
		}
		return true
	})
	return line
}

func (l *Line) addCall(call *syntax.CallExpr) {
	if len(call.Assigns) > 0 {
		l.Risky = true
	}
	if len(call.Args) == 0 {
		return
	}
	words := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		word, ok := literal(arg)
		if !ok || execFlag(word) {
			l.Risky = true
		}
		words = append(words, word)
	}
	if words[0] == "" || wrappers[filepath.Base(words[0])] {
		l.Risky = true
	}
	l.Commands = append(l.Commands, words)
}

func execFlag(word string) bool {
	if execFlags[word] {
		return true
	}
	for _, prefix := range execFlagPrefixes {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}
	return false
}

// writesFile reports whether a redirection writes a file; /dev/null and
// duplicated descriptors do not count.
func writesFile(redirect *syntax.Redirect) bool {
	switch redirect.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.AppClob, syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob, syntax.RdrInOut:
		target, ok := literal(redirect.Word)
		return !ok || target != "/dev/null"
	case syntax.DplOut:
		target, ok := literal(redirect.Word)
		return !ok || !descriptor(target)
	default:
		return false
	}
}

func descriptor(target string) bool {
	if target == "-" {
		return true
	}
	for _, r := range target {
		if r < '0' || r > '9' {
			return false
		}
	}
	return target != ""
}

// literal is the text of a word made only of plain, quoted and escaped text.
func literal(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var text strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			text.WriteString(unescape(part.Value, false))
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", false
			}
			text.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return "", false
				}
				text.WriteString(unescape(lit.Value, true))
			}
		default:
			return "", false
		}
	}
	return text.String(), true
}

// unescape drops the backslashes bash removes: before any character outside
// quotes, and before $, `, ", \ and a newline inside double quotes.
func unescape(value string, quoted bool) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var out strings.Builder
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' || i+1 == len(runes) {
			out.WriteRune(runes[i])
			continue
		}
		next := runes[i+1]
		if quoted && !strings.ContainsRune("$`\"\\\n", next) {
			out.WriteRune(runes[i])
			continue
		}
		i++
		if next != '\n' {
			out.WriteRune(next)
		}
	}
	return out.String()
}
```

- [ ] **Step 4: Add the dependency and run the tests**

```bash
go get mvdan.cc/sh/v3@v3.14.1
go mod tidy
gofmt -w internal/permission
go vet ./internal/permission/ && go test ./internal/permission/
```

Expected: `go get` prints `upgraded golang.org/x/sys v0.46.0 => v0.47.0` and `added mvdan.cc/sh/v3 v3.14.1`; after `go mod tidy`, `mvdan.cc/sh/v3 v3.14.1` sits in the first `require` block of `go.mod` (not `// indirect`) and `git diff go.mod` shows only that line and the `x/sys` bump. Tests: `ok`.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add go.mod go.sum internal/permission/permission.go internal/permission/match.go internal/permission/bash.go internal/permission/permission_test.go internal/permission/bash_test.go
git commit -m "feat(permission): rules, subjects, bash command parsing and mode presets

Adds mvdan.cc/sh/v3 v3.14.1 (pure Go) for bash parsing.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Store permission rules

**Files:**
- Create: `internal/store/sqlite_permission_rules.go`, `internal/store/sqlite_permission_rules_test.go`
- Modify: `internal/store/schema.go` (`applyCanonicalSchema`)
- Modify: `internal/core/ports.go` (`PermissionRuleStore`, `Store`)

- [ ] **Step 1: Write the failing test**

`internal/store/sqlite_permission_rules_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func TestPermissionRulesOfSessionsAndGlobal(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	for _, id := range []string{"parent", "child", "other"} {
		createTestSession(t, st, id)
	}
	for i, rule := range []permission.Rule{
		{ID: "r-global", Tool: "read", Pattern: "/home/u/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal},
		{ID: "r-parent", Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "parent"},
		{ID: "r-child", Tool: "edit", Pattern: "/work/**", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "child"},
		{ID: "r-other", Tool: "bash", Pattern: "rm:*", Effect: permission.Deny, Scope: permission.ScopeSession, SessionID: "other"},
	} {
		rule.CreatedAt = testEpoch.Add(time.Duration(i) * time.Second)
		if err := st.CreatePermissionRule(ctx, rule); err != nil {
			t.Fatal(err)
		}
	}

	rules, err := st.ListPermissionRules(ctx, []string{"child", "parent"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, rule := range rules {
		ids = append(ids, rule.ID)
	}
	if got := len(ids); got != 3 || ids[0] != "r-global" || ids[1] != "r-parent" || ids[2] != "r-child" {
		t.Fatalf("rules = %v", ids)
	}
	if rules[0].SessionID != "" || rules[1].SessionID != "parent" || rules[1].Pattern != "go test:*" || rules[1].Effect != permission.Allow {
		t.Fatalf("stored rules = %+v", rules)
	}

	if err := st.DeleteSession(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePermissionRule(ctx, "r-global"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeletePermissionRule(ctx, "r-global"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
	rules, err = st.ListPermissionRules(ctx, []string{"child", "parent"})
	if err != nil || len(rules) != 1 || rules[0].ID != "r-child" {
		t.Fatalf("rules after deletes = %+v err = %v", rules, err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/ -run TestPermissionRulesOfSessionsAndGlobal`
Expected: FAIL to compile — `st.CreatePermissionRule undefined`.

- [ ] **Step 3: Implement**

`internal/store/sqlite_permission_rules.go`:

```go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func migratePermissionRules(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS permission_rules (
    id TEXT PRIMARY KEY,
    tool TEXT NOT NULL,
    pattern TEXT NOT NULL DEFAULT '',
    effect TEXT NOT NULL,
    scope TEXT NOT NULL,
    session_id TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create permission rules table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_permission_rules_session ON permission_rules(session_id)`); err != nil {
		return fmt.Errorf("store: create permission rules session index: %w", err)
	}
	return nil
}

// CreatePermissionRule stores a rule; a global rule belongs to no session.
func (s *SQLiteStore) CreatePermissionRule(ctx context.Context, rule permission.Rule) error {
	var sessionID any
	if rule.Scope == permission.ScopeSession {
		sessionID = strings.TrimSpace(rule.SessionID)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO permission_rules(id, tool, pattern, effect, scope, session_id, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.Tool, rule.Pattern, string(rule.Effect), string(rule.Scope), sessionID, formatTime(rule.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create permission rule: %w", err)
	}
	return nil
}

// DeletePermissionRule removes a rule; ErrNotFound when no rule has the ID.
func (s *SQLiteStore) DeletePermissionRule(ctx context.Context, ruleID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM permission_rules WHERE id = ?`, strings.TrimSpace(ruleID))
	if err != nil {
		return fmt.Errorf("store: delete permission rule: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete permission rule rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

// ListPermissionRules returns the global rules and the rules of the given
// sessions, oldest first.
func (s *SQLiteStore) ListPermissionRules(ctx context.Context, sessionIDs []string) ([]permission.Rule, error) {
	query := `
SELECT id, tool, pattern, effect, scope, COALESCE(session_id, ''), created_at
FROM permission_rules
WHERE session_id IS NULL`
	args := make([]any, 0, len(sessionIDs))
	if len(sessionIDs) > 0 {
		query += ` OR session_id IN (?` + strings.Repeat(`, ?`, len(sessionIDs)-1) + `)`
		for _, id := range sessionIDs {
			args = append(args, strings.TrimSpace(id))
		}
	}
	query += ` ORDER BY created_at, id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list permission rules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var rules []permission.Rule
	for rows.Next() {
		var rule permission.Rule
		var effect, scope, createdAt string
		if err := rows.Scan(&rule.ID, &rule.Tool, &rule.Pattern, &effect, &scope, &rule.SessionID, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan permission rule: %w", err)
		}
		rule.Effect = permission.Effect(effect)
		rule.Scope = permission.Scope(scope)
		rule.CreatedAt = mustParseTime(createdAt)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate permission rules: %w", err)
	}
	return rules, nil
}
```

`internal/store/schema.go`, at the end of `applyCanonicalSchema`:

```go
	if err := migratePermissionRules(db); err != nil {
		return err
	}
	return migrateMessageSearch(db)
}
```

`internal/core/ports.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`, add before `FileSnapshotStore`

```go
// PermissionRuleStore keeps permission rules; global rules belong to no session.
type PermissionRuleStore interface {
	CreatePermissionRule(ctx context.Context, rule permission.Rule) error
	DeletePermissionRule(ctx context.Context, ruleID string) error
	ListPermissionRules(ctx context.Context, sessionIDs []string) ([]permission.Rule, error)
}
```

and embed `PermissionRuleStore` in `Store` after `ApprovalStore`. (`SQLiteStore` is the only full `Store`; the test fakes embed the interface.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `gofmt -w internal/store internal/core/ports.go && go test ./internal/store/ -run TestPermissionRulesOfSessionsAndGlobal -v`
Expected: PASS.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/store/sqlite_permission_rules.go internal/store/sqlite_permission_rules_test.go internal/store/schema.go internal/core/ports.go
git commit -m "feat(store): keep permission rules per session and globally

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Executors name the subjects of their calls

**Files:**
- Create: `internal/tools/permission_subject.go`, `internal/tools/permission_subject_test.go`
- Create: `internal/webtools/permission_subject.go`, `internal/webtools/permission_subject_test.go`
- Create: `internal/mcp/permission_subject.go`, `internal/mcp/permission_subject_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/tools/permission_subject_test.go`:

```go
package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
)

func TestPermissionSubjectsNameResolvedPathsAndCommands(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	registry := NewCoreCodingRegistry()
	for _, tc := range []struct {
		tool string
		args string
		want permission.Subject
	}{
		{"bash", `{"command":"go test ./..."}`, permission.Subject{Kind: permission.KindCommand, Value: "go test ./..."}},
		{"read", `{"file_path":"link/a.go"}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(real, "a.go")}},
		{"write", `{"file_path":"` + filepath.Join(root, "new.txt") + `","content":"x"}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(root, "new.txt")}},
		{"edit", `{"file_path":"link/b.go","old_string":"a","new_string":"b"}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(real, "b.go")}},
		{"multiedit", `{"file_path":"c.go","edits":[]}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(root, "c.go")}},
		{"glob", `{"pattern":"*.go"}`, permission.Subject{Kind: permission.KindDirectory, Value: root}},
		{"grep", `{"pattern":"x","path":"link"}`, permission.Subject{Kind: permission.KindDirectory, Value: real}},
		{"ls", `{}`, permission.Subject{Kind: permission.KindDirectory, Value: root}},
		{"read", `{}`, permission.Subject{}},
		{"bash", `not json`, permission.Subject{}},
		{"job_output", `{"shell_id":"job-1"}`, permission.Subject{}},
	} {
		got := registry.Subject(tc.tool, Call{WorkingDir: root, Args: json.RawMessage(tc.args)})
		if got != tc.want {
			t.Errorf("%s %s: subject = %+v, want %+v", tc.tool, tc.args, got, tc.want)
		}
	}
}
```

`internal/webtools/permission_subject_test.go`:

```go
package webtools

import (
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestWebFetchSubjectIsTheHost(t *testing.T) {
	registry := tools.NewRegistry(NewWebFetchExecutor())
	for args, want := range map[string]permission.Subject{
		`{"url":"https://Docs.Example.com:8443/a?b=1"}`: {Kind: permission.KindDomain, Value: "docs.example.com"},
		`{"url":"not a url"}`:                           {},
		`{}`:                                            {},
	} {
		if got := registry.Subject("web_fetch", tools.Call{Args: json.RawMessage(args)}); got != want {
			t.Errorf("%s: subject = %+v, want %+v", args, got, want)
		}
	}
}
```

`internal/mcp/permission_subject_test.go`:

```go
package mcp

import (
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestRemoteToolSubjectNamesServerAndTool(t *testing.T) {
	executor := newRemoteToolExecutor(ServerConfig{ID: "github", ToolPrefix: "gh"}, nil, &sdk.Tool{Name: "create_issue"})
	registry := tools.NewRegistry(executor)

	got := registry.Subject(executor.Spec().ID, tools.Call{})

	if want := (permission.Subject{Kind: permission.KindName, Value: "github__create_issue"}); got != want {
		t.Fatalf("subject = %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tools/ ./internal/webtools/ ./internal/mcp/`
Expected: FAIL to compile — `registry.Subject undefined (type *Registry has no field or method Subject)`.

- [ ] **Step 3: Implement**

`internal/tools/permission_subject.go` (the executors are the core filesystem and shell executors of this package):

```go
package tools

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
)

// SubjectProvider is implemented by executors whose calls permission rules can
// name more narrowly than the whole tool.
type SubjectProvider interface {
	PermissionSubject(call Call) permission.Subject
}

// Subject is what permission rules match of a call; the zero Subject when the
// tool names none.
func (r *Registry) Subject(toolID string, call Call) permission.Subject {
	if r == nil {
		return permission.Subject{}
	}
	r.mu.RLock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	r.mu.RUnlock()
	provider, provides := registered.executor.(SubjectProvider)
	if !ok || !provides {
		return permission.Subject{}
	}
	return provider.PermissionSubject(call)
}

func (e *bashExecutor) PermissionSubject(call Call) permission.Subject {
	var params BashParams
	if json.Unmarshal(call.Args, &params) != nil || strings.TrimSpace(params.Command) == "" {
		return permission.Subject{}
	}
	return permission.Subject{Kind: permission.KindCommand, Value: params.Command}
}

func (e *readExecutor) PermissionSubject(call Call) permission.Subject {
	var params ReadParams
	return fileSubject(call, &params, func() string { return params.FilePath })
}

func (e *writeExecutor) PermissionSubject(call Call) permission.Subject {
	var params WriteParams
	return fileSubject(call, &params, func() string { return params.FilePath })
}

func (e *editExecutor) PermissionSubject(call Call) permission.Subject {
	var params EditParams
	return fileSubject(call, &params, func() string { return params.FilePath })
}

func (e *multiEditExecutor) PermissionSubject(call Call) permission.Subject {
	var params MultiEditParams
	return fileSubject(call, &params, func() string { return params.FilePath })
}

func (e *globExecutor) PermissionSubject(call Call) permission.Subject {
	var params GlobParams
	return directorySubject(call, &params, func() string { return params.Path })
}

func (e *grepExecutor) PermissionSubject(call Call) permission.Subject {
	var params GrepParams
	return directorySubject(call, &params, func() string { return params.Path })
}

func (e *lsExecutor) PermissionSubject(call Call) permission.Subject {
	var params LSParams
	return directorySubject(call, &params, func() string { return params.Path })
}

// fileSubject is the file a call names, as an absolute path with symlinks resolved.
func fileSubject(call Call, params any, path func() string) permission.Subject {
	if json.Unmarshal(call.Args, params) != nil || strings.TrimSpace(path()) == "" {
		return permission.Subject{}
	}
	return pathSubject(permission.KindFile, call.WorkingDir, path())
}

// directorySubject is the directory a search starts from; no path is the working directory.
func directorySubject(call Call, params any, path func() string) permission.Subject {
	if len(call.Args) > 0 && json.Unmarshal(call.Args, params) != nil {
		return permission.Subject{}
	}
	return pathSubject(permission.KindDirectory, call.WorkingDir, path())
}

func pathSubject(kind permission.Kind, workingDir string, value string) permission.Subject {
	policy, err := ResolveFilesystemPath(workingDir, value)
	if err != nil {
		return permission.Subject{}
	}
	return permission.Subject{Kind: kind, Value: policy.RealPath}
}
```

`internal/webtools/permission_subject.go`:

```go
package webtools

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// PermissionSubject is the host the call fetches from.
func (e *webFetchExecutor) PermissionSubject(call tools.Call) permission.Subject {
	var params WebFetchParams
	if json.Unmarshal(call.Args, &params) != nil {
		return permission.Subject{}
	}
	parsed, err := url.Parse(strings.TrimSpace(params.URL))
	if err != nil || parsed.Hostname() == "" {
		return permission.Subject{}
	}
	return permission.Subject{Kind: permission.KindDomain, Value: strings.ToLower(parsed.Hostname())}
}
```

`internal/mcp/permission_subject.go`:

```go
package mcp

import (
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// PermissionSubject names the server and its tool as "server__tool".
func (e *remoteToolExecutor) PermissionSubject(tools.Call) permission.Subject {
	return permission.Subject{Kind: permission.KindName, Value: e.server.ID + "__" + e.remoteName}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/tools internal/webtools internal/mcp && go test ./internal/tools/ ./internal/webtools/ ./internal/mcp/`
Expected: `ok` for the three packages.

- [ ] **Step 5: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/tools/permission_subject.go internal/tools/permission_subject_test.go internal/webtools/permission_subject.go internal/webtools/permission_subject_test.go internal/mcp/permission_subject.go internal/mcp/permission_subject_test.go
git commit -m "feat(tools): name the command, path, domain or MCP tool a call acts on

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Evaluate permissions in one place for every pipeline

`core.checkPermission` evaluates the rules of the call's session (global, own, parents') and then the mode preset. `coreTools.Authorize` blocks denied calls before they run; `executeToolWithGrant` — shared by the engine's `Execute` and by `ExecuteTool` (API, voice, MCP server, approval and recovery replays) — applies every verdict. The old mode switch (`autoApprovesTool`) is replaced by the presets.

**Files:**
- Modify: `internal/core/ports.go` (`ToolExecutor.Subject`)
- Modify: `internal/daemoncmd/tool_visibility.go` (add `setupAwareToolExecutor.Subject`)
- Modify: `internal/core/types_session.go` (mode constants from `permission`)
- Modify (rewrite): `internal/core/tool_permissions.go`, `internal/core/tool_call_run.go`
- Modify: `internal/core/agent_tools.go` (`coreTools.Authorize`)
- Modify: `internal/agent/tools.go` (`runCall`)
- Create: `internal/core/tool_permissions_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/core/tool_permissions_test.go` (the real read and write tools over a temp directory, and a fake `bash` that names its command to the rules):

```go
package core_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// commandTool asks before every command, like bash, and names the command to rules.
type commandTool struct {
	ran []string
}

func (t *commandTool) Spec() tools.Spec {
	spec := recoveryToolSpec("bash", tools.EffectMutation)
	spec.Category = tools.CategoryShell
	return spec
}

func (t *commandTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	command := commandOf(call)
	if !call.Approved {
		return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "bash", ToolCallID: call.ToolCallID, Action: "execute", Description: command}}, nil
	}
	t.ran = append(t.ran, command)
	return tools.Result{Content: "ran " + command}, nil
}

func (t *commandTool) PermissionSubject(call tools.Call) permission.Subject {
	return permission.Subject{Kind: permission.KindCommand, Value: commandOf(call)}
}

func commandOf(call tools.Call) string {
	var params struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(call.Args, &params)
	return params.Command
}

// permissionCore has the real file tools and a fake bash over a temp working directory.
func permissionCore(t *testing.T) (*core.Core, *store.SQLiteStore, *commandTool, string) {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"notes.txt": "public notes", "secret/key.txt": "hunter2"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bash := &commandTool{}
	registry := tools.NewCoreReadOnlyRegistry(tools.NewWriteExecutor(), bash)
	app.WithTools(registry)
	return app, db, bash, dir
}

func permissionSession(t *testing.T, db *store.SQLiteStore, id string, dir string, mode core.PermissionMode, parentID string) core.Session {
	t.Helper()
	now := runRecoveryTestTime()
	session := core.Session{
		ID: id, Title: id, Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw,
		WorkingDir: dir, PermissionMode: mode, ParentSessionID: parentID, Hidden: parentID != "",
		ProviderID: "recovery-test", ModelID: "test-model", Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func saveRule(t *testing.T, db *store.SQLiteStore, id string, rule permission.Rule) {
	t.Helper()
	rule.ID, rule.CreatedAt = id, runRecoveryTestTime()
	if rule.Scope == "" {
		rule.Scope = permission.ScopeSession
	}
	if err := db.CreatePermissionRule(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
}

func executeTool(t *testing.T, app *core.Core, sessionID string, tool string, args string, approved bool) core.ExecuteToolResult {
	t.Helper()
	result, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: sessionID, ToolName: tool, Args: json.RawMessage(args), Approved: approved})
	if err != nil {
		t.Fatalf("ExecuteTool(%s %s): %v", tool, args, err)
	}
	return result
}

func resultText(result core.ExecuteToolResult) string {
	if result.ToolResultMessage == nil {
		return ""
	}
	return result.ToolResultMessage.Content
}

func TestDenyRuleBlocksAReadOnlyToolForRunsAndDirectCalls(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_deny", dir, core.PermissionModeFullAuto, "")
	saveRule(t, db, "rule_secret", permission.Rule{Tool: "read", Pattern: filepath.Join(dir, "secret") + "/**", Effect: permission.Deny, SessionID: session.ID})
	blocked := "Blocked by rule read: " + filepath.Join(dir, "secret") + "/**"

	if got := resultText(executeTool(t, app, session.ID, "read", `{"file_path":"secret/key.txt"}`, false)); got != blocked {
		t.Fatalf("direct call result = %q", got)
	}
	if got := resultText(executeTool(t, app, session.ID, "read", `{"file_path":"secret/key.txt"}`, true)); got != blocked {
		t.Fatalf("approved replay result = %q", got)
	}
	if got := resultText(executeTool(t, app, session.ID, "read", `{"file_path":"notes.txt"}`, false)); got == blocked || got == "" {
		t.Fatalf("unrelated read result = %q", got)
	}

	var saw string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if saw = toolResultContent(request, "call-read"); saw == "" {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-read", Name: "read", Arguments: []byte(`{"file_path":"secret/key.txt"}`)}}}, nil
		}
		return providers.Response{Text: "The key is off limits."}, nil
	})})
	run := core.Run{ID: "run_deny", SessionID: session.ID, UserMessageID: "msg_deny", Status: core.RunStatusAccepted, StartedAt: runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime()}
	user := transcript.Message{ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser, Content: "read the key", CreatedAt: run.StartedAt, UpdatedAt: run.StartedAt}
	if err := db.AcceptMessage(context.Background(), user, run); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if saw != blocked {
		t.Fatalf("model read %q", saw)
	}
}

func TestAskRuleAsksBeforeAReadOnlyTool(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_ask", dir, core.PermissionModeDefault, "")
	saveRule(t, db, "rule_ask", permission.Rule{Tool: "read", Pattern: dir + "/**", Effect: permission.Ask, SessionID: session.ID})

	pending := executeTool(t, app, session.ID, "read", `{"file_path":"notes.txt"}`, false)
	if pending.Approval == nil || pending.Approval.Action != "ask_rule" || pending.ToolResultMessage != nil {
		t.Fatalf("result = %+v", pending)
	}
	if _, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	assertToolResultCount(t, db, session.ID, pending.ToolCallMessage.ID, 1)
}

func TestAllowRuleRunsMatchingCommandsWithoutAsking(t *testing.T) {
	app, db, bash, dir := permissionCore(t)
	session := permissionSession(t, db, "session_allow", dir, core.PermissionModeDefault, "")
	saveRule(t, db, "rule_echo", permission.Rule{Tool: "bash", Pattern: "echo:*", Effect: permission.Allow, SessionID: session.ID})

	if got := resultText(executeTool(t, app, session.ID, "bash", `{"command":"echo ok && echo done"}`, false)); got != "ran echo ok && echo done" {
		t.Fatalf("allowed command result = %q", got)
	}
	for _, command := range []string{"echo ok > out.txt", "rm -rf build", "echo ok; rm -rf build"} {
		args, _ := json.Marshal(map[string]string{"command": command})
		if result := executeTool(t, app, session.ID, "bash", string(args), false); result.Approval == nil {
			t.Errorf("%q ran without approval: %+v", command, result)
		}
	}
	if len(bash.ran) != 1 {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestSubagentsAndOtherSessionsFollowInheritedAndGlobalRules(t *testing.T) {
	app, db, bash, dir := permissionCore(t)
	parent := permissionSession(t, db, "session_parent", dir, core.PermissionModeFullAuto, "")
	child := permissionSession(t, db, "session_child", dir, core.PermissionModeFullAuto, parent.ID)
	other := permissionSession(t, db, "session_other", dir, core.PermissionModeFullAuto, "")
	saveRule(t, db, "rule_parent", permission.Rule{Tool: "read", Pattern: filepath.Join(dir, "secret") + "/**", Effect: permission.Deny, SessionID: parent.ID})
	saveRule(t, db, "rule_global", permission.Rule{Tool: "bash", Pattern: "rm:*", Effect: permission.Deny, Scope: permission.ScopeGlobal})

	if got := resultText(executeTool(t, app, child.ID, "read", `{"file_path":"secret/key.txt"}`, false)); got != "Blocked by rule read: "+filepath.Join(dir, "secret")+"/**" {
		t.Fatalf("child read = %q", got)
	}
	if got := resultText(executeTool(t, app, other.ID, "read", `{"file_path":"secret/key.txt"}`, false)); got == "" || strings.HasPrefix(got, "Blocked") {
		t.Fatalf("other session read = %q", got)
	}
	if got := resultText(executeTool(t, app, other.ID, "bash", `{"command":"rm -rf build"}`, false)); got != "Blocked by rule bash: rm:*" {
		t.Fatalf("global rule result = %q", got)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestModePresetsAllowEditsInsideTheWorkingDirectoryOrEverything(t *testing.T) {
	app, db, bash, dir := permissionCore(t)
	edits := permissionSession(t, db, "session_edits", dir, core.PermissionModeAcceptEdits, "")
	auto := permissionSession(t, db, "session_auto", dir, core.PermissionModeFullAuto, "")
	outside := filepath.Join(t.TempDir(), "outside.txt")

	if result := executeTool(t, app, edits.ID, "write", `{"file_path":"inside.txt","content":"x"}`, false); result.Approval != nil {
		t.Fatalf("write inside the working directory asked: %+v", result.Approval)
	}
	if _, err := os.Stat(filepath.Join(dir, "inside.txt")); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"file_path": outside, "content": "x"})
	if result := executeTool(t, app, edits.ID, "write", string(args), false); result.Approval == nil {
		t.Fatal("write outside the working directory ran without approval")
	}
	if result := executeTool(t, app, edits.ID, "bash", `{"command":"go test ./..."}`, false); result.Approval == nil {
		t.Fatal("accept_edits ran a command without approval")
	}
	if result := executeTool(t, app, auto.ID, "bash", `{"command":"go test ./..."}`, false); result.Approval != nil || len(bash.ran) != 1 {
		t.Fatalf("full_auto asked: %+v ran = %v", result.Approval, bash.ran)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/core/ -run 'TestDenyRuleBlocksAReadOnlyToolForRunsAndDirectCalls|TestAskRuleAsksBeforeAReadOnlyTool|TestAllowRuleRunsMatchingCommandsWithoutAsking|TestSubagentsAndOtherSessionsFollowInheritedAndGlobalRules|TestModePresetsAllowEditsInsideTheWorkingDirectoryOrEverything'`
Expected: FAIL — `direct call result = "<file>\n     1\thunter2\n</file>"` (no rule is evaluated yet), `allowed command result = ""`, `child read = …`, and the ask test gets a result instead of an approval. `TestModePresetsAllowEditsInsideTheWorkingDirectoryOrEverything` already passes: it guards the old `accept_edits`/`full_auto` behaviour through the rewrite.

- [ ] **Step 3: Expose subjects through the core port**

`internal/core/ports.go`, `ToolExecutor`:

```go
type ToolExecutor interface {
	List() []tools.Spec
	Spec(toolID string) (tools.Spec, bool)
	Execute(ctx context.Context, toolID string, call tools.Call) (tools.Result, error)
	// Subject is what permission rules match of a call.
	Subject(toolID string, call tools.Call) permission.Subject
}
```

`*tools.Registry` already satisfies it (Task 3). `internal/daemoncmd/tool_visibility.go`: import `"github.com/Suren878/matrixclaw/internal/permission"` and add before `visible`:

```go
func (e *setupAwareToolExecutor) Subject(toolID string, call tools.Call) permission.Subject {
	if e == nil || e.inner == nil {
		return permission.Subject{}
	}
	return e.inner.Subject(toolID, call)
}
```

`internal/core/types_session.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`; the modes are the preset names:

```go
const (
	PermissionModeDefault     PermissionMode = permission.ModeDefault
	PermissionModeAcceptEdits PermissionMode = permission.ModeAcceptEdits
	PermissionModeFullAuto    PermissionMode = permission.ModeFullAuto
)
```

- [ ] **Step 4: Replace the permission check**

Replace the whole of `internal/core/tool_permissions.go` (this deletes `autoApprovesTool`, `acceptEditsAllows` and `mutationPathWithinRoot`) with:

```go
package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// callPermission is how the permission rules see one call and treat it.
type callPermission struct {
	request permission.Request
	verdict permission.Verdict
}

// checkPermission evaluates a call against the global rules, the rules of its
// session and the session's parents, then the preset of the session's mode.
// Engine runs and ExecuteTool (API, voice, MCP server, replays) both ask here.
func (c *Core) checkPermission(ctx context.Context, sessionID string, spec tools.Spec, call tools.Call) (callPermission, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return callPermission{}, err
	}
	rules, err := c.permissionRules(ctx, session)
	if err != nil {
		return callPermission{}, err
	}
	request := permission.Request{Tool: permissionTool(spec), Subject: c.tools.Subject(spec.ID, call)}
	root := firstNonEmpty(normalizeWorkingDir(session.WorkingDir), normalizeWorkingDir(call.WorkingDir))
	preset := permission.Preset(string(NormalizePermissionMode(string(session.PermissionMode))), realPath(root))
	return callPermission{request: request, verdict: permission.Evaluate(request, rules, preset)}, nil
}

// permissionRules are the global rules and the rules of the session and its parents.
func (c *Core) permissionRules(ctx context.Context, session Session) ([]permission.Rule, error) {
	sessionIDs := []string{session.ID}
	for parentID := normalizeText(session.ParentSessionID); parentID != "" && !slices.Contains(sessionIDs, parentID); {
		sessionIDs = append(sessionIDs, parentID)
		parent, err := c.store.GetSession(ctx, parentID)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		parentID = normalizeText(parent.ParentSessionID)
	}
	return c.store.ListPermissionRules(ctx, sessionIDs)
}

// permissionTool is the tool name rules are written for: every MCP tool is "mcp"
// (its subject names server and tool) and multiedit shares the rules of edit.
func permissionTool(spec tools.Spec) string {
	switch {
	case strings.HasPrefix(spec.Namespace, "mcp."):
		return "mcp"
	case spec.ID == "multiedit":
		return "edit"
	default:
		return spec.ID
	}
}

// realPath is path with symlinks resolved, as rules compare paths.
func realPath(path string) string {
	if path == "" {
		return ""
	}
	policy, err := tools.ResolveFilesystemPath(path, "")
	if err != nil {
		return path
	}
	return policy.RealPath
}

// blockedResult is what the model reads for a call a deny rule blocks.
func blockedResult(rule permission.Rule) tools.Result {
	return tools.Result{Content: "Blocked by rule " + rule.String(), Status: tools.ResultStatusError, IsError: true}
}

// askedByRule requests approval for a call an ask rule catches in a tool that
// does not ask on its own.
func askedByRule(prepared preparedToolCall, input ExecuteToolInput, rule permission.Rule) tools.Result {
	return tools.Result{
		Content: "Approval required",
		Approval: &tools.ApprovalRequest{
			ToolID:      prepared.ToolName,
			ToolCallID:  prepared.ToolCallID,
			Action:      "ask_rule",
			Description: fmt.Sprintf("Rule %q asks before this call", rule.String()),
			Params:      input.Args,
		},
	}
}
```

Replace the whole of `internal/core/tool_call_run.go` (this deletes `executePreparedTool`) with:

```go
package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// executeToolWithGrant runs a call as its permission says: a deny rule blocks
// it; a grant, an allow rule or the mode preset runs it approved; an ask rule
// requests approval; otherwise the tool's own dry run decides.
func (c *Core) executeToolWithGrant(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput) (tools.Result, error) {
	check, err := c.checkPermission(ctx, prepared.SessionID, prepared.Spec, prepared.call(input, input.Approved))
	if err != nil {
		return tools.Result{}, err
	}
	verdict := check.verdict
	var result tools.Result
	var execErr error
	switch {
	case verdict.Effect == permission.Deny:
		return blockedResult(verdict.Rule), nil
	case input.Approved || verdict.Effect == permission.Allow:
		result, execErr = c.tools.Execute(ctx, prepared.ToolName, prepared.call(input, true))
	case verdict.Effect == permission.Ask && !prepared.Spec.RequiresApproval():
		result = askedByRule(prepared, input, verdict.Rule)
	default:
		result, execErr = c.tools.Execute(ctx, prepared.ToolName, prepared.call(input, false))
	}
	if execErr != nil || result.Approval != nil {
		return result, execErr
	}
	return c.keepLargeOutput(prepared.SessionID, result), nil
}

func (prepared preparedToolCall) call(input ExecuteToolInput, approved bool) tools.Call {
	return tools.Call{
		SessionID:   prepared.SessionID,
		RunID:       prepared.RunID,
		ToolCallID:  prepared.ToolCallID,
		Client:      input.Client,
		ExternalKey: input.ExternalKey,
		WorkingDir:  prepared.WorkingDir,
		Approved:    approved,
		Args:        input.Args,
	}
}
```

`internal/core/agent_tools.go`: import `"github.com/Suren878/matrixclaw/internal/permission"` and replace `Authorize`:

```go
// Authorize rejects invalid calls and those a deny rule blocks; the rest of the
// permission check runs with the call in Execute.
func (t coreTools) Authorize(ctx context.Context, name string, call tools.Call) (agent.Decision, error) {
	// A call ID owned by another session fails the run with a clear error instead of
	// a primary-key conflict on the first journal write.
	if _, err := t.c.isNewToolCallMessage(ctx, call.SessionID, call.ToolCallID); err != nil {
		return agent.Decision{}, err
	}
	session, spec, err := t.c.checkToolCall(ctx, call.SessionID, name)
	if errors.Is(err, ErrInvalidInput) {
		return agent.Decision{Reason: err.Error()}, nil
	}
	if err != nil {
		return agent.Decision{}, err
	}
	if call.WorkingDir = normalizeWorkingDir(call.WorkingDir); call.WorkingDir == "" {
		call.WorkingDir = session.WorkingDir
	}
	check, err := t.c.checkPermission(ctx, call.SessionID, spec, call)
	if err != nil {
		return agent.Decision{}, err
	}
	if check.verdict.Effect == permission.Deny {
		return agent.Decision{Reason: blockedResult(check.verdict.Rule).Content}, nil
	}
	return agent.Decision{Allowed: true, Barrier: spec.Mutates()}, nil
}
```

`internal/agent/tools.go` (`runCall`): a rejected call is always an error result the model reads, also when it was granted — replace the rejection block with

```go
	if !decision.Allowed {
		return callDone, r.rejectCall(ctx, req, decision.Reason)
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w internal/core internal/agent/tools.go internal/daemoncmd/tool_visibility.go && go test ./internal/core/ ./internal/agent/ ./internal/daemoncmd/`
Expected: `ok` for the three packages (the five new tests included).

- [ ] **Step 6: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./internal/core/ ./internal/agent/
git status --short
git add internal/core/ports.go internal/daemoncmd/tool_visibility.go internal/core/types_session.go internal/core/tool_permissions.go \
  internal/core/tool_call_run.go internal/core/agent_tools.go internal/agent/tools.go internal/core/tool_permissions_test.go
git commit -m "feat(core): decide every tool call by permission rules and mode presets

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Suggested rules and "Always allow"

A call that asks carries a suggested rule to its approval (stored, sent to clients, copied to bridged approvals). `ApprovalResolveRequest.Always` grants the call and saves the rule for the approval's session — the parent's for a bridged approval — or globally.

**Files:**
- Modify: `internal/tools/types.go` (`ApprovalRequest.Suggestion`)
- Modify: `internal/core/types_approval.go` (`Approval.Suggestion`, `PermissionRequest.Suggestion`)
- Modify: `internal/core/contracts.go` (`ApprovalResolveRequest.Always`)
- Modify: `internal/core/tool_call_run.go` (`executeToolWithGrant`)
- Modify: `internal/core/tool_call_approval.go` (`createPendingApproval`)
- Modify: `internal/core/subagents.go` (`subagentApprovalRequest`)
- Modify: `internal/core/tool_approvals.go` (`ResolveApproval`; add `keepSuggestedRule`)
- Modify: `internal/clientruntime/state.go` (`PermissionRequestFromApproval`)
- Modify: `internal/store/schema.go`, `internal/store/sqlite_approvals_files.go`
- Test: `internal/store/sqlite_approvals_test.go`, `internal/core/tool_permissions_test.go`, `internal/core/subagents_approval_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/sqlite_approvals_test.go` (and import `"github.com/Suren878/matrixclaw/internal/permission"`):

```go
func TestApprovalKeepsItsSuggestedRule(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	suggestion := &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}
	if err := st.CreateApproval(ctx, core.Approval{ID: "a1", SessionID: "s1", ToolName: "bash", State: core.ApprovalStatePending, Suggestion: suggestion, RequestedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateApproval(ctx, core.Approval{ID: "a2", SessionID: "s1", ToolName: "bash", State: core.ApprovalStatePending, RequestedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}

	stored, err := st.GetApproval(ctx, "a1")
	if err != nil || stored.Suggestion == nil || *stored.Suggestion != *suggestion {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	listed, err := st.ListApprovals(ctx, "s1", core.ApprovalStatePending)
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed = %+v err = %v", listed, err)
	}
	for _, approval := range listed {
		if (approval.ID == "a1") != (approval.Suggestion != nil) {
			t.Fatalf("listed %s suggestion = %+v", approval.ID, approval.Suggestion)
		}
	}
}
```

Append to `internal/core/tool_permissions_test.go` (and add `"errors"` to its imports):

```go
func TestAlwaysAllowKeepsTheSuggestedRule(t *testing.T) {
	for _, scope := range []permission.Scope{permission.ScopeSession, permission.ScopeGlobal} {
		app, db, bash, dir := permissionCore(t)
		session := permissionSession(t, db, "session_always", dir, core.PermissionModeDefault, "")
		other := permissionSession(t, db, "session_other", dir, core.PermissionModeDefault, "")

		pending := executeTool(t, app, session.ID, "bash", `{"command":"go test ./..."}`, false)
		if pending.Approval == nil || pending.Approval.Suggestion == nil || pending.Approval.Suggestion.String() != "bash: go test:*" {
			t.Fatalf("%s: approval = %+v", scope, pending.Approval)
		}
		if _, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Approved: true, Always: scope}); err != nil {
			t.Fatal(err)
		}

		if result := executeTool(t, app, session.ID, "bash", `{"command":"go test ./internal/..."}`, false); result.Approval != nil {
			t.Fatalf("%s: the kept rule did not allow the next test run", scope)
		}
		if result := executeTool(t, app, other.ID, "bash", `{"command":"go test ./..."}`, false); (result.Approval == nil) != (scope == permission.ScopeGlobal) {
			t.Fatalf("%s: other session approval = %+v", scope, result.Approval)
		}
		if len(bash.ran) < 2 {
			t.Fatalf("%s: ran = %v", scope, bash.ran)
		}
	}
}

func TestAlwaysAllowNeedsASuggestedRule(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_risky", dir, core.PermissionModeDefault, "")
	pending := executeTool(t, app, session.ID, "bash", `{"command":"go test ./... > out.txt"}`, false)
	if pending.Approval == nil || pending.Approval.Suggestion != nil {
		t.Fatalf("approval = %+v", pending.Approval)
	}

	_, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeSession})

	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if stored, _ := db.GetApproval(context.Background(), pending.Approval.ID); stored.State != core.ApprovalStatePending {
		t.Fatalf("approval state = %s, want still pending", stored.State)
	}
}
```

Append to `internal/core/subagents_approval_test.go` (and import `"github.com/Suren878/matrixclaw/internal/permission"`):

```go
func TestAlwaysAllowOnABridgedApprovalKeepsTheRuleForTheParent(t *testing.T) {
	var starter *executingRunStarter
	b := newBridgedChild(t, func(app *core.Core) core.RunStarter {
		starter = &executingRunStarter{app: app}
		return starter
	})
	parent, bridge, childRunID := b.park(t)
	if bridge.Suggestion == nil || bridge.Suggestion.String() != "mutate_state" {
		t.Fatalf("bridged suggestion = %+v", bridge.Suggestion)
	}

	if _, err := b.app.ResolveApproval(context.Background(), bridge.ID, core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeSession}); err != nil {
		t.Fatal(err)
	}
	waitForRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	starter.wait(t)

	child, err := b.db.GetRun(context.Background(), childRunID)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := b.db.ListPermissionRules(context.Background(), []string{child.SessionID})
	if err != nil || len(rules) != 0 {
		t.Fatalf("child rules = %+v err = %v", rules, err)
	}
	rules, err = b.db.ListPermissionRules(context.Background(), []string{parent.SessionID})
	if err != nil || len(rules) != 1 || rules[0].String() != "mutate_state" || rules[0].SessionID != parent.SessionID {
		t.Fatalf("parent rules = %+v err = %v", rules, err)
	}
	if b.mutations != 1 {
		t.Fatalf("mutations = %d", b.mutations)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go vet ./internal/store/ ./internal/core/`
Expected: FAIL to compile — `unknown field Suggestion in struct literal of type core.Approval`, `unknown field Always in struct literal of type core.ApprovalResolveRequest`.

- [ ] **Step 3: Carry the suggestion**

`internal/tools/types.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`; `ApprovalRequest` ends with

```go
	Params      any    `json:"params,omitempty"`
	// Suggestion is the rule an "Always allow" answer keeps.
	Suggestion *permission.Suggestion `json:"suggestion,omitempty"`
}
```

(`tools` may import `permission`: the permission package imports nothing of the project.)

`internal/core/types_approval.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`;

```go
type Approval struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"session_id"`
	RunID       string          `json:"run_id,omitempty"`
	ToolCallRef string          `json:"tool_call_id,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	Description string          `json:"description,omitempty"`
	Action      string          `json:"action,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	Path        string          `json:"path,omitempty"`
	State       ApprovalState   `json:"state"`
	// Reason is why the user denied the call; the model reads it.
	Reason string `json:"reason,omitempty"`
	// Suggestion is the rule an "Always allow" answer keeps.
	Suggestion  *permission.Suggestion `json:"suggestion,omitempty"`
	RequestedAt time.Time              `json:"requested_at"`
	DecidedAt   *time.Time             `json:"decided_at,omitempty"`
}

type PermissionRequest struct {
	ID          string                 `json:"id"`
	SessionID   string                 `json:"session_id"`
	ToolCallID  string                 `json:"tool_call_id"`
	ToolName    string                 `json:"tool_name"`
	Description string                 `json:"description"`
	Action      string                 `json:"action"`
	Params      json.RawMessage        `json:"params,omitempty"`
	Path        string                 `json:"path"`
	Suggestion  *permission.Suggestion `json:"suggestion,omitempty"`
}
```

`internal/core/tool_call_approval.go` (`createPendingApproval`): add `Suggestion:  result.Approval.Suggestion,` to the `Approval` literal (after `Path`) and `Suggestion:  approval.Suggestion,` to the published `PermissionRequest` (after `Path`).

`internal/clientruntime/state.go` (`PermissionRequestFromApproval`): add `Suggestion:  approval.Suggestion,` after `Path`.

`internal/core/subagents.go` (`subagentApprovalRequest`): the bridged request carries the child's suggestion — add `Suggestion:  childApproval.Suggestion,` after `Params`.

`internal/core/tool_call_run.go`, the final `executeToolWithGrant` (suggests a rule when the call asks by itself, never for an ask rule):

```go
// executeToolWithGrant runs a call as its permission says: a deny rule blocks
// it; a grant, an allow rule or the mode preset runs it approved; an ask rule
// requests approval; otherwise the tool's own dry run decides.
func (c *Core) executeToolWithGrant(ctx context.Context, prepared preparedToolCall, input ExecuteToolInput) (tools.Result, error) {
	check, err := c.checkPermission(ctx, prepared.SessionID, prepared.Spec, prepared.call(input, input.Approved))
	if err != nil {
		return tools.Result{}, err
	}
	verdict := check.verdict
	var result tools.Result
	var execErr error
	switch {
	case verdict.Effect == permission.Deny:
		return blockedResult(verdict.Rule), nil
	case input.Approved || verdict.Effect == permission.Allow:
		result, execErr = c.tools.Execute(ctx, prepared.ToolName, prepared.call(input, true))
	case verdict.Effect == permission.Ask && !prepared.Spec.RequiresApproval():
		result = askedByRule(prepared, input, verdict.Rule)
	default:
		result, execErr = c.tools.Execute(ctx, prepared.ToolName, prepared.call(input, false))
	}
	if result.Approval != nil && result.Approval.Suggestion == nil && verdict.Effect != permission.Ask {
		if suggestion, ok := permission.Suggest(check.request); ok {
			result.Approval.Suggestion = &suggestion
		}
	}
	if execErr != nil || result.Approval != nil {
		return result, execErr
	}
	return c.keepLargeOutput(prepared.SessionID, result), nil
}
```

- [ ] **Step 4: Store the suggestion**

`internal/store/schema.go`, right after the `approvals.reason` `ensureColumn` of stage 4a:

```go
	if err := ensureColumn(db, "approvals", "suggestion_json", `ALTER TABLE approvals ADD COLUMN suggestion_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
```

`internal/store/sqlite_approvals_files.go`: import `"github.com/Suren878/matrixclaw/internal/permission"` and replace the four approval functions with the following (`UpdateApproval` is unchanged and repeated for completeness), adding `decodeSuggestion`:

```go
func (s *SQLiteStore) CreateApproval(ctx context.Context, approval core.Approval) error {
	suggestion := ""
	if approval.Suggestion != nil {
		body, err := json.Marshal(approval.Suggestion)
		if err != nil {
			return fmt.Errorf("store: encode approval suggestion: %w", err)
		}
		suggestion = string(body)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO approvals(id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, suggestion_json, requested_at, decided_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		approval.ID,
		approval.SessionID,
		approval.RunID,
		approval.ToolCallRef,
		approval.ToolName,
		approval.Description,
		approval.Action,
		string(approval.Params),
		approval.Path,
		string(approval.State),
		approval.Reason,
		suggestion,
		formatTime(approval.RequestedAt),
		nullableTime(approval.DecidedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create approval: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetApproval(ctx context.Context, approvalID string) (core.Approval, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, suggestion_json, requested_at, decided_at
FROM approvals
WHERE id = ?`, approvalID)

	var approval core.Approval
	var state string
	var paramsJSON string
	var suggestionJSON string
	var requestedAt string
	var decidedAt sql.NullString
	if err := row.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &state, &approval.Reason, &suggestionJSON, &requestedAt, &decidedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Approval{}, core.ErrNotFound
		}
		return core.Approval{}, fmt.Errorf("store: get approval: %w", err)
	}
	approval.State = core.ApprovalState(state)
	approval.Params = json.RawMessage(paramsJSON)
	approval.Suggestion = decodeSuggestion(suggestionJSON)
	approval.RequestedAt = mustParseTime(requestedAt)
	if decidedAt.Valid {
		parsed := mustParseTime(decidedAt.String)
		approval.DecidedAt = &parsed
	}
	return approval, nil
}

func (s *SQLiteStore) UpdateApproval(ctx context.Context, approval core.Approval) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE approvals
SET state = ?, reason = ?, decided_at = ?
WHERE id = ?`,
		string(approval.State),
		approval.Reason,
		nullableTime(approval.DecidedAt),
		approval.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update approval: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update approval rows: %w", err)
	}
	if count == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListApprovals(ctx context.Context, sessionID string, state core.ApprovalState) ([]core.Approval, error) {
	query := `
SELECT id, session_id, run_id, tool_call_ref, tool_name, description, action, params_json, path, state, reason, suggestion_json, requested_at, decided_at
FROM approvals
WHERE session_id = ?`
	args := []any{sessionID}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, string(state))
	}
	query += ` ORDER BY requested_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list approvals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var approvals []core.Approval
	for rows.Next() {
		var approval core.Approval
		var rawState string
		var paramsJSON string
		var suggestionJSON string
		var requestedAt string
		var decidedAt sql.NullString
		if err := rows.Scan(&approval.ID, &approval.SessionID, &approval.RunID, &approval.ToolCallRef, &approval.ToolName, &approval.Description, &approval.Action, &paramsJSON, &approval.Path, &rawState, &approval.Reason, &suggestionJSON, &requestedAt, &decidedAt); err != nil {
			return nil, fmt.Errorf("store: scan approval: %w", err)
		}
		approval.State = core.ApprovalState(rawState)
		approval.Params = json.RawMessage(paramsJSON)
		approval.Suggestion = decodeSuggestion(suggestionJSON)
		approval.RequestedAt = mustParseTime(requestedAt)
		if decidedAt.Valid {
			parsed := mustParseTime(decidedAt.String)
			approval.DecidedAt = &parsed
		}
		approvals = append(approvals, approval)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate approvals: %w", err)
	}
	return approvals, nil
}

// decodeSuggestion reads a stored suggestion; an unreadable one offers none.
func decodeSuggestion(raw string) *permission.Suggestion {
	if raw == "" {
		return nil
	}
	var suggestion permission.Suggestion
	if json.Unmarshal([]byte(raw), &suggestion) != nil {
		return nil
	}
	return &suggestion
}

```

- [ ] **Step 5: "Always allow" in `ResolveApproval`**

`internal/core/contracts.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`;

```go
// ApprovalResolveRequest is a user's decision on an approval; Reason explains a
// denial to the model, and Always keeps the approval's suggested rule in that scope.
type ApprovalResolveRequest struct {
	Approved bool             `json:"approved"`
	Reason   string           `json:"reason,omitempty"`
	Always   permission.Scope `json:"always,omitempty"`
}
```

`internal/core/tool_approvals.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`; `ResolveApproval` saves the rule before recording the grant, and a bridged approval passes a plain grant to the child:

```go
func (c *Core) ResolveApproval(ctx context.Context, approvalID string, decision ApprovalResolveRequest) (Approval, error) {
	approval, err := c.store.GetApproval(ctx, normalizeText(approvalID))
	if err != nil {
		return Approval{}, err
	}
	if approval.State != ApprovalStatePending {
		if approval.State == approvalState(decision.Approved) {
			return approval, nil
		}
		return Approval{}, fmt.Errorf("%w: approval already resolved", ErrInvalidInput)
	}
	if decision.Approved && decision.Always != "" {
		if err := c.keepSuggestedRule(ctx, approval, decision.Always); err != nil {
			return Approval{}, err
		}
	}
	bridge, bridged := decodeSubagentApprovalBridge(approval)
	approval, err = c.recordApprovalDecision(ctx, approval, decision, bridged)
	if err != nil {
		return Approval{}, err
	}
	switch {
	case bridged:
		decision.Always = ""
		return approval, c.passDecisionToSubagent(ctx, bridge, decision)
	case strings.TrimSpace(approval.RunID) == "":
		return approval, c.finishRunlessApproval(ctx, approval)
	default:
		return approval, c.resumeDecidedRun(ctx, approval.SessionID, approval.RunID)
	}
}

// keepSuggestedRule saves the approval's suggested rule as an allow rule; a
// session rule belongs to the approval's session, the parent's for a bridged one.
func (c *Core) keepSuggestedRule(ctx context.Context, approval Approval, scope permission.Scope) error {
	if !scope.Valid() {
		return fmt.Errorf("%w: unknown rule scope %q", ErrInvalidInput, scope)
	}
	if approval.Suggestion == nil {
		return fmt.Errorf("%w: approval %s suggests no rule", ErrInvalidInput, approval.ID)
	}
	rule := permission.Rule{
		ID:        c.newID("rule"),
		Tool:      approval.Suggestion.Tool,
		Pattern:   approval.Suggestion.Pattern,
		Effect:    permission.Allow,
		Scope:     scope,
		CreatedAt: c.now().UTC(),
	}
	if scope == permission.ScopeSession {
		rule.SessionID = approval.SessionID
	}
	return c.store.CreatePermissionRule(ctx, rule)
}
```

(The doc comment of `ResolveApproval` from stage 4a stays.)

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w internal/tools/types.go internal/core internal/clientruntime/state.go internal/store && go test ./internal/store/ ./internal/core/ ./internal/clientruntime/`
Expected: `ok`; the new tests `TestApprovalKeepsItsSuggestedRule`, `TestAlwaysAllowKeepsTheSuggestedRule`, `TestAlwaysAllowNeedsASuggestedRule`, `TestAlwaysAllowOnABridgedApprovalKeepsTheRuleForTheParent` pass.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/tools/types.go internal/core/types_approval.go internal/core/contracts.go internal/core/tool_call_run.go internal/core/tool_call_approval.go \
  internal/core/subagents.go internal/core/tool_approvals.go internal/clientruntime/state.go internal/store/schema.go internal/store/sqlite_approvals_files.go \
  internal/store/sqlite_approvals_test.go internal/core/tool_permissions_test.go internal/core/subagents_approval_test.go
git commit -m "feat(core): suggest a rule with each approval and keep it on Always allow

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Manage rules — core API, HTTP endpoints, `/permissions` with owner-only global rules

**Files:**
- Create: `internal/core/permission_rules.go` (`SessionPermissionRules`, `AddPermissionRule`, `DeletePermissionRule`, `toolSpec`, `absolutePattern`)
- Modify: `internal/core/contracts.go` (`PermissionRuleRequest`, `PermissionRulesResponse`, `PermissionRuleResponse`)
- Create: `internal/api/permission_rules.go`, `internal/api/permission_rules_test.go`; Modify: `internal/api/server.go` (`routes`), `internal/api/sessions.go` (`handleSessionByID`)
- Create: `internal/daemonclient/permission_rules.go`
- Modify: `internal/clientruntime/controlplane_runtime.go` (`Owner`, rule methods, `ManagesGlobalRules`)
- Modify: `internal/controlplane/dispatcher.go` (`PermissionRuleRuntime`, `Dispatcher.rules`, `New`)
- Modify (rewrite): `internal/controlplane/permissions.go`; Create: `internal/controlplane/permissions_test.go`
- Modify: `clients/terminal/chat/runtime/runtime.go` (`New`: the TUI is the owner)
- Test: `internal/core/tool_permissions_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/core/tool_permissions_test.go`:

```go
func TestAddedRulesNameAbsolutePathsAndKnownTools(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_rules", dir, core.PermissionModeDefault, "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if home, err = filepath.EvalSymlinks(home); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		request core.PermissionRuleRequest
		want    string
	}{
		{core.PermissionRuleRequest{Tool: "read", Pattern: "secret/**", Effect: permission.Deny, Scope: permission.ScopeSession}, "read: " + dir + "/secret/**"},
		{core.PermissionRuleRequest{Tool: "write", Pattern: "~/notes/*.md", Effect: permission.Ask, Scope: permission.ScopeGlobal}, "write: " + home + "/notes/*.md"},
		{core.PermissionRuleRequest{Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession}, "bash: go test:*"},
		{core.PermissionRuleRequest{Tool: "*", Effect: permission.Deny, Scope: permission.ScopeSession}, "*"},
	} {
		rule, err := app.AddPermissionRule(context.Background(), session.ID, tc.request)
		if err != nil || rule.String() != tc.want || (rule.Scope == permission.ScopeSession) != (rule.SessionID == session.ID) {
			t.Errorf("%+v: rule = %+v (%s) err = %v, want %s", tc.request, rule, rule.String(), err, tc.want)
		}
	}
	for _, request := range []core.PermissionRuleRequest{
		{Tool: "rm_everything", Effect: permission.Deny, Scope: permission.ScopeSession},
		{Tool: "bash", Effect: "sometimes", Scope: permission.ScopeSession},
		{Tool: "bash", Effect: permission.Allow, Scope: "forever"},
	} {
		if _, err := app.AddPermissionRule(context.Background(), session.ID, request); !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("%+v: error = %v, want ErrInvalidInput", request, err)
		}
	}
	rules, err := app.SessionPermissionRules(context.Background(), session.ID)
	if err != nil || len(rules) != 4 {
		t.Fatalf("rules = %+v err = %v", rules, err)
	}
}
```

`internal/api/permission_rules_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestPermissionRuleEndpoints(t *testing.T) {
	server, _ := newAPITestServer(t)
	post := httptest.NewRecorder()
	server.Handler().ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/v1/sessions/s1/permission-rules", strings.NewReader(`{"tool":"mcp","pattern":"github__*","effect":"allow","scope":"session"}`)))
	var added core.PermissionRuleResponse
	if err := json.Unmarshal(post.Body.Bytes(), &added); err != nil || post.Code != http.StatusOK || added.Rule.ID == "" || added.Rule.SessionID != "s1" {
		t.Fatalf("POST status=%d body=%s", post.Code, post.Body.String())
	}

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/permission-rules", nil))
	var listed core.PermissionRulesResponse
	if err := json.Unmarshal(get.Body.Bytes(), &listed); err != nil || len(listed.Rules) != 1 || listed.Rules[0].String() != "mcp: github__*" {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}

	for _, want := range []int{http.StatusNoContent, http.StatusNotFound} {
		deleted := httptest.NewRecorder()
		server.Handler().ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/v1/permission-rules/"+added.Rule.ID, nil))
		if deleted.Code != want {
			t.Fatalf("DELETE status=%d, want %d", deleted.Code, want)
		}
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/v1/sessions/s1/permission-rules", strings.NewReader(`{"tool":"mcp","effect":"sometimes","scope":"session"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid POST status=%d", invalid.Code)
	}
}
```

`internal/controlplane/permissions_test.go`:

```go
package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

type rulesRuntime struct {
	tokenReportRuntime
	owner   bool
	rules   []permission.Rule
	added   []core.PermissionRuleRequest
	deleted []string
}

func (r *rulesRuntime) UpdateSessionPermissionMode(_ context.Context, sessionID string, mode core.PermissionMode) (core.Session, error) {
	return core.Session{ID: sessionID, PermissionMode: mode}, nil
}

func (r *rulesRuntime) SessionPermissionRules(context.Context, string) ([]permission.Rule, error) {
	return r.rules, nil
}

func (r *rulesRuntime) AddPermissionRule(_ context.Context, sessionID string, request core.PermissionRuleRequest) (permission.Rule, error) {
	r.added = append(r.added, request)
	rule := permission.Rule{ID: "rule_new", Tool: request.Tool, Pattern: request.Pattern, Effect: request.Effect, Scope: request.Scope}
	if request.Scope == permission.ScopeSession {
		rule.SessionID = sessionID
	}
	return rule, nil
}

func (r *rulesRuntime) DeletePermissionRule(_ context.Context, ruleID string) error {
	r.deleted = append(r.deleted, ruleID)
	return nil
}

func (r *rulesRuntime) ManagesGlobalRules() bool { return r.owner }

func testRules() []permission.Rule {
	return []permission.Rule{
		{ID: "rule_global", Tool: "read", Pattern: "/home/u/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal},
		{ID: "rule_parent", Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "parent"},
		{ID: "rule_own", Tool: "edit", Pattern: "/work/**", Effect: permission.Allow, Scope: permission.ScopeSession, SessionID: "s1"},
	}
}

func TestPermissionsListsModesAndRules(t *testing.T) {
	runtime := &rulesRuntime{rules: testRules()}

	result, err := New(runtime, "").Handle(context.Background(), "key", "/permissions")

	if err != nil || result.Picker == nil {
		t.Fatalf("result = %+v err = %v", result, err)
	}
	var rows []string
	for _, item := range result.Picker.Items {
		rows = append(rows, item.Title+" | "+item.Info+" | "+item.Command)
	}
	want := []string{
		"Ask First |  | /permissions default",
		"Edits Only |  | /permissions accept_edits",
		"Full Auto |  | /permissions full_auto",
		"deny read: /home/u/.ssh/** | global | /permissions delete rule_global",
		"allow bash: go test:* | parent session | /permissions delete rule_parent",
		"allow edit: /work/** | this session | /permissions delete rule_own",
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

func TestPermissionsDeleteAsksThenDeletes(t *testing.T) {
	runtime := &rulesRuntime{rules: testRules()}
	dispatcher := New(runtime, "")

	result, err := dispatcher.Handle(context.Background(), "key", "/permissions delete rule_own")
	if err != nil || result.Confirm == nil || result.Confirm.ConfirmCommand != "/permissions delete rule_own confirm" || len(runtime.deleted) != 0 {
		t.Fatalf("result = %+v deleted = %v err = %v", result, runtime.deleted, err)
	}
	result, err = dispatcher.Handle(context.Background(), "key", result.Confirm.ConfirmCommand)
	if err != nil || len(runtime.deleted) != 1 || runtime.deleted[0] != "rule_own" || result.Text != "🗑️ Rule deleted: allow edit: /work/**" {
		t.Fatalf("result = %+v deleted = %v err = %v", result, runtime.deleted, err)
	}
}

func TestOnlyTheOwnerChangesGlobalRules(t *testing.T) {
	for _, owner := range []bool{false, true} {
		runtime := &rulesRuntime{owner: owner, rules: testRules()}
		dispatcher := New(runtime, "")

		deleted, err := dispatcher.Handle(context.Background(), "key", "/permissions delete rule_global confirm")
		if err != nil {
			t.Fatal(err)
		}
		added, err := dispatcher.Handle(context.Background(), "key", "/permissions add deny read ~/.ssh/** global")
		if err != nil {
			t.Fatal(err)
		}

		if owner != (len(runtime.deleted) == 1 && len(runtime.added) == 1) {
			t.Fatalf("owner=%v: deleted = %v added = %+v (%q, %q)", owner, runtime.deleted, runtime.added, deleted.Text, added.Text)
		}
		if !owner && (deleted.Text != "Only the owner can change global rules." || added.Text != deleted.Text) {
			t.Fatalf("non-owner replies = %q, %q", deleted.Text, added.Text)
		}
		if owner && runtime.added[0] != (core.PermissionRuleRequest{Tool: "read", Pattern: "~/.ssh/**", Effect: permission.Deny, Scope: permission.ScopeGlobal}) {
			t.Fatalf("added = %+v", runtime.added[0])
		}
	}
}

func TestPermissionsAddReadsEffectToolAndPattern(t *testing.T) {
	for command, want := range map[string]core.PermissionRuleRequest{
		"/permissions add allow bash go test:*": {Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession},
		"/permissions add ask web_fetch":        {Tool: "web_fetch", Effect: permission.Ask, Scope: permission.ScopeSession},
	} {
		runtime := &rulesRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || len(runtime.added) != 1 || runtime.added[0] != want {
			t.Errorf("%s: result = %+v added = %+v err = %v", command, result, runtime.added, err)
		}
	}
	for _, command := range []string{"/permissions add", "/permissions add allow", "/permissions add maybe bash ls:*", "/permissions sometimes"} {
		runtime := &rulesRuntime{}

		result, err := New(runtime, "").Handle(context.Background(), "key", command)

		if err != nil || result.Text != permissionsUsage || len(runtime.added) != 0 {
			t.Errorf("%s: result = %+v err = %v", command, result, err)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go vet ./internal/core/ ./internal/api/ ./internal/controlplane/`
Expected: FAIL to compile — `app.AddPermissionRule undefined`, `undefined: core.PermissionRuleRequest`, `undefined: permissionsUsage`.

- [ ] **Step 3: Core and HTTP**

`internal/core/contracts.go`, before `AdminRestartRequest`:

```go
// PermissionRuleRequest adds a rule. Pattern is a path glob, a command prefix
// such as "go test:*", a domain or an MCP "server__tool" name; empty is the whole tool.
type PermissionRuleRequest struct {
	Tool    string            `json:"tool"`
	Pattern string            `json:"pattern,omitempty"`
	Effect  permission.Effect `json:"effect"`
	Scope   permission.Scope  `json:"scope"`
}

type PermissionRulesResponse struct {
	Rules []permission.Rule `json:"rules"`
}

type PermissionRuleResponse struct {
	Rule permission.Rule `json:"rule"`
}
```

`internal/core/permission_rules.go`:

```go
package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// SessionPermissionRules lists the rules a session's calls follow: the global
// rules, its own and its parents'.
func (c *Core) SessionPermissionRules(ctx context.Context, sessionID string) ([]permission.Rule, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return nil, err
	}
	return c.permissionRules(ctx, session)
}

// AddPermissionRule saves a rule for the session, or for every session when its
// scope is global. File tool patterns become absolute against the session's
// working directory.
func (c *Core) AddPermissionRule(ctx context.Context, sessionID string, request PermissionRuleRequest) (permission.Rule, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return permission.Rule{}, err
	}
	rule := permission.Rule{
		ID:        c.newID("rule"),
		Tool:      normalizeText(request.Tool),
		Pattern:   normalizeText(request.Pattern),
		Effect:    request.Effect,
		Scope:     request.Scope,
		CreatedAt: c.now().UTC(),
	}
	if !rule.Effect.Valid() || !rule.Scope.Valid() || rule.Tool == "" {
		return permission.Rule{}, fmt.Errorf("%w: a rule needs a tool, an effect (allow, ask, deny) and a scope (session, global)", ErrInvalidInput)
	}
	if rule.Tool != "*" && rule.Tool != "mcp" {
		spec, ok := c.toolSpec(rule.Tool)
		if !ok {
			return permission.Rule{}, fmt.Errorf("%w: unknown tool %q", ErrInvalidInput, rule.Tool)
		}
		rule.Tool = permissionTool(spec)
		if spec.Category == tools.CategoryFilesystem {
			rule.Pattern = absolutePattern(rule.Pattern, session.WorkingDir)
		}
	}
	if rule.Scope == permission.ScopeSession {
		rule.SessionID = session.ID
	}
	if err := c.store.CreatePermissionRule(ctx, rule); err != nil {
		return permission.Rule{}, err
	}
	return rule, nil
}

// DeletePermissionRule removes a rule.
func (c *Core) DeletePermissionRule(ctx context.Context, ruleID string) error {
	return c.store.DeletePermissionRule(ctx, normalizeText(ruleID))
}

func (c *Core) toolSpec(toolID string) (tools.Spec, bool) {
	if c.tools == nil {
		return tools.Spec{}, false
	}
	return c.tools.Spec(toolID)
}

// absolutePattern resolves a path pattern against the working directory: "~/"
// is the home directory, and symlinks leave the part before the first glob.
func absolutePattern(pattern string, workingDir string) string {
	if pattern == "" || pattern == "*" {
		return pattern
	}
	if rest, ok := strings.CutPrefix(pattern, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			pattern = home + "/" + rest
		}
	}
	base, glob := pattern, ""
	if i := strings.IndexAny(pattern, "*?"); i >= 0 {
		slash := strings.LastIndex(pattern[:i], "/")
		switch {
		case slash < 0:
			base, glob = "", pattern
		case slash == 0:
			base, glob = "/", pattern[1:]
		default:
			base, glob = pattern[:slash], pattern[slash+1:]
		}
	}
	policy, err := tools.ResolveFilesystemPath(workingDir, base)
	if err != nil {
		return pattern
	}
	if glob == "" {
		return policy.RealPath
	}
	return strings.TrimSuffix(policy.RealPath, "/") + "/" + filepath.ToSlash(glob)
}
```

`internal/api/permission_rules.go`:

```go
package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionPermissionRules(w http.ResponseWriter, r *http.Request, sessionID string) {
	switch r.Method {
	case http.MethodGet:
		rules, err := s.core.SessionPermissionRules(r.Context(), sessionID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.PermissionRulesResponse{Rules: rules})
	case http.MethodPost:
		var request core.PermissionRuleRequest
		if !decodeJSONBody(w, r, &request) {
			return
		}
		rule, err := s.core.AddPermissionRule(r.Context(), sessionID, request)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.PermissionRuleResponse{Rule: rule})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handlePermissionRuleByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeMethodNotAllowed(w, http.MethodDelete)
		return
	}
	ruleID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/permission-rules/"), "/")
	if ruleID == "" || strings.Contains(ruleID, "/") {
		writeNotFound(w)
		return
	}
	if err := s.core.DeletePermissionRule(r.Context(), ruleID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

`internal/api/sessions.go` (`handleSessionByID`), after the `/permissions` child route:

```go
		{suffix: "/permission-rules", handle: s.handleSessionPermissionRules},
```

`internal/api/server.go` (`routes`), after the `/v1/approvals/` handler:

```go
	s.mux.HandleFunc("/v1/permission-rules/", s.handlePermissionRuleByID)
```

`internal/daemonclient/permission_rules.go`:

```go
package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func (c *Client) SessionPermissionRules(ctx context.Context, sessionID string) ([]permission.Rule, error) {
	var response core.PermissionRulesResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/permission-rules"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Rules, nil
}

func (c *Client) AddPermissionRule(ctx context.Context, sessionID string, request core.PermissionRuleRequest) (permission.Rule, error) {
	var response core.PermissionRuleResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/permission-rules"
	if err := c.doJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return permission.Rule{}, err
	}
	return response.Rule, nil
}

func (c *Client) DeletePermissionRule(ctx context.Context, ruleID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/permission-rules/"+escapedPath(ruleID), nil, nil)
}
```

- [ ] **Step 4: Controlplane and clients**

`internal/clientruntime/controlplane_runtime.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`; the struct gains

```go
	Daemon      DaemonClientFunc
	// Owner lets the commands create and delete global permission rules.
	Owner bool
}
```

and after `ResolveApproval`:

```go
func (r ControlplaneRuntime) SessionPermissionRules(ctx context.Context, sessionID string) ([]permission.Rule, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.SessionPermissionRules(ctx, sessionID)
}

func (r ControlplaneRuntime) AddPermissionRule(ctx context.Context, sessionID string, request core.PermissionRuleRequest) (permission.Rule, error) {
	client, err := r.client("")
	if err != nil {
		return permission.Rule{}, err
	}
	return client.AddPermissionRule(ctx, sessionID, request)
}

func (r ControlplaneRuntime) DeletePermissionRule(ctx context.Context, ruleID string) error {
	client, err := r.client("")
	if err != nil {
		return err
	}
	return client.DeletePermissionRule(ctx, ruleID)
}

// ManagesGlobalRules reports whether this client may change global rules.
func (r ControlplaneRuntime) ManagesGlobalRules() bool {
	return r.Owner
}
```

`internal/controlplane/dispatcher.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`; after `PermissionRuntime`:

```go
type PermissionRuleRuntime interface {
	SessionPermissionRules(ctx context.Context, sessionID string) ([]permission.Rule, error)
	AddPermissionRule(ctx context.Context, sessionID string, request core.PermissionRuleRequest) (permission.Rule, error)
	DeletePermissionRule(ctx context.Context, ruleID string) error
	ManagesGlobalRules() bool
}
```

a field `rules          PermissionRuleRuntime` after `permissions` in `Dispatcher`, and `d.rules, _ = runtime.(PermissionRuleRuntime)` after the `PermissionRuntime` assignment in `New`.

Replace the whole of `internal/controlplane/permissions.go` with:

```go
package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

const permissionsUsage = "Usage: /permissions default|accept_edits|full_auto, /permissions add allow|ask|deny <tool> [pattern] [global], /permissions delete <rule id>"

func (d *Dispatcher) handlePermissions(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.permissions == nil {
		return unsupportedRuntime("permission"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if !core.CapabilitiesForSession(*session).PermissionMode {
		return Result{Handled: true, Text: "Permission Mode is available for Matrixclaw sessions only."}, nil
	}

	action, rest := cutWord(args)
	switch strings.ToLower(action) {
	case "":
		return d.permissionsPicker(ctx, *session)
	case "add":
		return d.addPermissionRule(ctx, *session, rest)
	case "delete":
		return d.deletePermissionRule(ctx, *session, rest)
	}
	mode, ok := parsePermissionMode(args)
	if !ok {
		return Result{Handled: true, Text: permissionsUsage}, nil
	}
	updated, err := d.permissions.UpdateSessionPermissionMode(ctx, session.ID, mode)
	if err != nil {
		return Result{}, err
	}
	text := "✅ Permission mode: " + permissionModeStatus(updated.PermissionMode)
	if d.messages != nil {
		if _, err := d.messages.CreateSystemMessage(ctx, updated.ID, text); err != nil {
			return Result{}, err
		}
	}
	return Result{
		Handled:        true,
		Text:           text,
		ReloadSnapshot: true,
	}, nil
}

// permissionsPicker offers the permission modes and lists the session's rules;
// choosing a rule deletes it.
func (d *Dispatcher) permissionsPicker(ctx context.Context, session core.Session) (Result, error) {
	picker := NewPickerData(PickerPermissions, "Permissions").Context(session.ID).Select("").Items(permissionModeItems(session.PermissionMode)...)
	if d.rules != nil {
		rules, err := d.rules.SessionPermissionRules(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		for _, rule := range rules {
			picker.Danger(rule.ID, string(rule.Effect)+" "+rule.String(), ruleScopeLabel(rule, session.ID), permissionsCommand("delete", rule.ID))
		}
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func ruleScopeLabel(rule permission.Rule, sessionID string) string {
	switch {
	case rule.Scope == permission.ScopeGlobal:
		return "global"
	case rule.SessionID == sessionID:
		return "this session"
	default:
		return "parent session"
	}
}

// addPermissionRule reads "allow|ask|deny <tool> [pattern] [global]".
func (d *Dispatcher) addPermissionRule(ctx context.Context, session core.Session, args string) (Result, error) {
	if d.rules == nil {
		return unsupportedRuntime("permission rule"), nil
	}
	fields := strings.Fields(args)
	if len(fields) < 2 || !permission.Effect(strings.ToLower(fields[0])).Valid() {
		return Result{Handled: true, Text: permissionsUsage}, nil
	}
	request := core.PermissionRuleRequest{Effect: permission.Effect(strings.ToLower(fields[0])), Tool: fields[1], Scope: permission.ScopeSession}
	pattern := fields[2:]
	if last := len(pattern) - 1; last >= 0 && strings.EqualFold(pattern[last], "global") {
		request.Scope, pattern = permission.ScopeGlobal, pattern[:last]
	}
	request.Pattern = strings.Join(pattern, " ")
	if request.Scope == permission.ScopeGlobal && !d.rules.ManagesGlobalRules() {
		return Result{Handled: true, Text: "Only the owner can change global rules."}, nil
	}
	rule, err := d.rules.AddPermissionRule(ctx, session.ID, request)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "✅ Rule added: " + string(rule.Effect) + " " + rule.String() + " (" + ruleScopeLabel(rule, session.ID) + ")"}, nil
}

// deletePermissionRule asks first, then deletes one of the session's rules.
func (d *Dispatcher) deletePermissionRule(ctx context.Context, session core.Session, args string) (Result, error) {
	if d.rules == nil {
		return unsupportedRuntime("permission rule"), nil
	}
	ruleID, confirm := cutWord(args)
	rules, err := d.rules.SessionPermissionRules(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	var rule permission.Rule
	for _, candidate := range rules {
		if candidate.ID == ruleID {
			rule = candidate
		}
	}
	if rule.ID == "" {
		return Result{Handled: true, Text: "No such rule for this session."}, nil
	}
	if rule.Scope == permission.ScopeGlobal && !d.rules.ManagesGlobalRules() {
		return Result{Handled: true, Text: "Only the owner can change global rules."}, nil
	}
	label := string(rule.Effect) + " " + rule.String()
	if confirm != "confirm" {
		return Result{Handled: true, Confirm: &ConfirmData{
			Title:          "Delete rule",
			Message:        "Delete " + label + " (" + ruleScopeLabel(rule, session.ID) + ")?",
			ConfirmLabel:   "Delete",
			ConfirmCommand: permissionsCommand("delete", rule.ID, "confirm"),
			CancelCommand:  permissionsCommand(),
			ConfirmDanger:  true,
		}}, nil
	}
	if err := d.rules.DeletePermissionRule(ctx, rule.ID); err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "🗑️ Rule deleted: " + label}, nil
}

func permissionModeItems(current core.PermissionMode) []PickerItem {
	current = core.NormalizePermissionMode(string(current))
	modes := []struct {
		mode  core.PermissionMode
		title string
	}{
		{mode: core.PermissionModeDefault, title: "Ask First"},
		{mode: core.PermissionModeAcceptEdits, title: "Edits Only"},
		{mode: core.PermissionModeFullAuto, title: "Full Auto"},
	}
	items := make([]PickerItem, 0, len(modes))
	for _, mode := range modes {
		items = append(items, PickerItem{
			ID:       string(mode.mode),
			Title:    mode.title,
			Command:  permissionsCommand(string(mode.mode)),
			Selected: current == mode.mode,
		})
	}
	return items
}

func parsePermissionMode(value string) (core.PermissionMode, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
	switch normalized {
	case "default", "safe":
		return core.PermissionModeDefault, true
	case "accept_edits", "accept", "edits", "auto_edit", "auto_edits":
		return core.PermissionModeAcceptEdits, true
	case "full_auto", "full_access", "full", "auto", "access":
		return core.PermissionModeFullAuto, true
	default:
		return core.PermissionModeDefault, false
	}
}
```

`clients/terminal/chat/runtime/runtime.go` (`New`): the TUI runs as the owner —

```go
	rt.ControlplaneRuntime = clientruntime.ControlplaneRuntime{
		Client:      clientName,
		ExternalKey: externalKey,
		WorkingDir:  rt.config.WorkingDir,
		Daemon: func(string) (*daemonclient.Client, error) {
			return rt.daemon()
		},
		Owner: true,
	}
```

(Telegram keeps `Owner` false until Task 8 names its owner chat.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w internal/core internal/api internal/daemonclient internal/clientruntime internal/controlplane clients/terminal/chat/runtime && go test ./internal/core/ ./internal/api/ ./internal/controlplane/ ./internal/clientruntime/ ./clients/terminal/...`
Expected: `ok` for every package.

- [ ] **Step 6: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/permission_rules.go internal/core/contracts.go internal/core/tool_permissions_test.go internal/api/permission_rules.go internal/api/permission_rules_test.go \
  internal/api/server.go internal/api/sessions.go internal/daemonclient/permission_rules.go internal/clientruntime/controlplane_runtime.go \
  internal/controlplane/dispatcher.go internal/controlplane/permissions.go internal/controlplane/permissions_test.go clients/terminal/chat/runtime/runtime.go
git commit -m "feat(controlplane): list, add and delete permission rules with /permissions

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: TUI — "Always allow" replaces "Allow Session"

**Files:**
- Modify: `clients/terminal/ui/surface/permission/types.go` (`PermissionRequest.Suggestion`), `clients/terminal/ui/surface/permission/helpers.go` (delete `CanAllowSessionApproval`)
- Modify: `clients/terminal/chat/viewmodel/surface_adapter.go` (`ToSurfacePermissionRequest`)
- Modify: `clients/terminal/ui/surface/dialog/{permissions,permissions_keys,permissions_events,permissions_render}.go`, `permissions_test.go`
- Modify: `clients/terminal/chat/runtime/{app,app_update,app_server_commands,app_approvals,app_dialog_actions}.go`

- [ ] **Step 1: Write the failing test**

Append to `clients/terminal/ui/surface/dialog/permissions_test.go` (and import `"github.com/Suren878/matrixclaw/internal/permission"`):

```go
func TestPermissionsDialogOffersAlwaysAllowOnlyWithASuggestedRule(t *testing.T) {
	suggested := surfacepermission.PermissionRequest{ID: "approval_1", ToolName: "bash", Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}}
	for key, want := range map[rune]PermissionAction{'s': PermissionAlwaysSession, 'g': PermissionAlwaysGlobal} {
		action := NewPermissions(surfacecommon.DefaultCommon(), suggested).HandleMsg(tea.KeyPressMsg{Code: key, Text: string(key)})
		if response, ok := action.(ActionPermissionResponse); !ok || response.Action != want {
			t.Fatalf("%c: action = %#v, want %s", key, action, want)
		}
	}
	plain := surfacepermission.PermissionRequest{ID: "approval_2", ToolName: "bash"}
	if action := NewPermissions(surfacecommon.DefaultCommon(), plain).HandleMsg(tea.KeyPressMsg{Code: 's', Text: "s"}); action != nil {
		t.Fatalf("always allow without a suggested rule: %#v", action)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./clients/terminal/ui/surface/dialog/`
Expected: FAIL to compile — `unknown field Suggestion in struct literal of type permission.PermissionRequest`.

- [ ] **Step 3: Carry the suggestion to the dialog**

`clients/terminal/ui/surface/permission/types.go` (the package is itself named `permission`, so the rules package gets an alias):

```go
package permission

import permissionrules "github.com/Suren878/matrixclaw/internal/permission"

type PermissionRequest struct {
	ID          string                      `json:"id"`
	SessionID   string                      `json:"session_id"`
	ToolCallID  string                      `json:"tool_call_id"`
	ToolName    string                      `json:"tool_name"`
	Description string                      `json:"description"`
	Action      string                      `json:"action"`
	Params      any                         `json:"params,omitempty"`
	Path        string                      `json:"path"`
	Suggestion  *permissionrules.Suggestion `json:"suggestion,omitempty"`
}
```

(`PermissionNotification` below it is unchanged.) In `clients/terminal/ui/surface/permission/helpers.go`, delete `CanAllowSessionApproval` and the now unused `internal/tools` import.

`clients/terminal/chat/viewmodel/surface_adapter.go` (`ToSurfacePermissionRequest`): add `Suggestion:  request.Suggestion,` after `Path`.

- [ ] **Step 4: Dialog buttons and keys**

`permissions.go`:

```go
const (
	PermissionAllow          PermissionAction = "allow"
	PermissionAlwaysSession  PermissionAction = "always_session"
	PermissionAlwaysGlobal   PermissionAction = "always_global"
	PermissionDeny           PermissionAction = "deny"
	PermissionDenyWithReason PermissionAction = "deny_with_reason"
)
```

`permissions_keys.go`: in `permissionsKeyMap` replace `AllowSession     key.Binding` with

```go
	AlwaysSession    key.Binding
	AlwaysGlobal     key.Binding
```

and in `defaultPermissionsKeyMap` replace the `AllowSession` binding with

```go
		AlwaysSession: key.NewBinding(
			key.WithKeys("s", "S", "ctrl+s"),
			key.WithHelp("s", "always in session"),
		),
		AlwaysGlobal: key.NewBinding(
			key.WithKeys("g", "G"),
			key.WithHelp("g", "always everywhere"),
		),
```

`permissions_events.go`: drop the `surfacepermission` import; in `HandleMsg` replace the `AllowSession` case with

```go
		case key.Matches(msg, p.keyMap.AlwaysSession):
			if p.permission.Suggestion != nil {
				return p.respond(PermissionAlwaysSession)
			}
		case key.Matches(msg, p.keyMap.AlwaysGlobal):
			if p.permission.Suggestion != nil {
				return p.respond(PermissionAlwaysGlobal)
			}
```

replace `permissionOptions` with

```go
func (p *Permissions) permissionOptions() []permissionOption {
	options := []permissionOption{
		{label: "Allow", action: PermissionAllow},
	}
	if p.permission.Suggestion != nil {
		options = append(options,
			permissionOption{label: "Always (session)", action: PermissionAlwaysSession},
			permissionOption{label: "Always (global)", action: PermissionAlwaysGlobal},
		)
	}
	options = append(options,
		permissionOption{label: "Deny", action: PermissionDeny},
		permissionOption{label: "Deny with reason", action: PermissionDenyWithReason},
	)
	return options
}
```

and delete `canAllowSession`.

`permissions_render.go` (`renderHeader`): show the rule "Always" would keep, right after `lines := []string{title, "", sourceLine, toolLine, pathLine}`:

```go
	if suggestion := p.permission.Suggestion; suggestion != nil {
		lines = append(lines, p.renderKeyValue("Always", suggestion.String(), contentWidth))
	}
```

- [ ] **Step 5: The app sends the decision; the session auto-approval goes**

`app_dialog_actions.go`: imports become `strings`, bubbletea, `surfacedialog`, `internal/controlplane`, `internal/core`, `internal/permission` (the `surfacepermission` import goes); `handlePermissionResponse`:

```go
func (m *appModel) handlePermissionResponse(msg surfacedialog.ActionPermissionResponse) tea.Cmd {
	m.dialog.CloseDialog(surfacedialog.PermissionsID)
	m.suppressedApprovals[msg.Permission.ID] = struct{}{}
	request := core.ApprovalResolveRequest{}
	switch msg.Action {
	case surfacedialog.PermissionDenyWithReason:
		m.denyingApproval = msg.Permission.ID
		m.dialog.OpenDialog(surfacedialog.NewPromptCommand(m.com, controlplane.DenyWithReasonPrompt(msg.Permission.ID)))
		return nil
	case surfacedialog.PermissionAllow:
		request.Approved = true
	case surfacedialog.PermissionAlwaysSession:
		request.Approved, request.Always = true, permission.ScopeSession
	case surfacedialog.PermissionAlwaysGlobal:
		request.Approved, request.Always = true, permission.ScopeGlobal
	}
	return tea.Batch(m.resolveApprovalCmd(msg.Permission, request), m.syncPermissionDialogCmd())
}
```

`app_approvals.go`: in `syncPermissionDialogCmd` delete the loop that auto-resolved `autoApprovesEditApproval` approvals; delete `autoApprovesEditApproval`; `resolveApprovalCmd` takes the request:

```go
func (m *appModel) resolveApprovalCmd(permission surfacepermission.PermissionRequest, request core.ApprovalResolveRequest) tea.Cmd {
	if m.rt == nil {
		return nil
	}
	return func() tea.Msg {
		approval, err := m.rt.ResolveApproval(m.ctx, permission.ID, request)
		return resolveApprovalMsg{
			approval:   approval,
			approved:   request.Approved,
			approvalID: permission.ID,
			err:        err,
		}
	}
}
```

`app.go`: delete the `autoEditSessions` field of `appModel` and its initialisation in `newApp`. `app_update.go` (`reconnectMsg` case) and `app_server_commands.go` (`handleServerRestartAck`): delete the `m.autoEditSessions = map[string]struct{}{}` lines.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `gofmt -w clients/terminal && go vet ./clients/... && go test ./clients/terminal/...`
Expected: `ok`; `grep -rn "autoEditSessions\|CanAllowSessionApproval\|PermissionAllowSession" clients/terminal` prints nothing.

- [ ] **Step 7: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./...
git status --short
git add clients/terminal/ui/surface/permission/types.go clients/terminal/ui/surface/permission/helpers.go clients/terminal/chat/viewmodel/surface_adapter.go \
  clients/terminal/ui/surface/dialog/permissions.go clients/terminal/ui/surface/dialog/permissions_keys.go clients/terminal/ui/surface/dialog/permissions_events.go \
  clients/terminal/ui/surface/dialog/permissions_render.go clients/terminal/ui/surface/dialog/permissions_test.go \
  clients/terminal/chat/runtime/app.go clients/terminal/chat/runtime/app_update.go clients/terminal/chat/runtime/app_server_commands.go \
  clients/terminal/chat/runtime/app_approvals.go clients/terminal/chat/runtime/app_dialog_actions.go
git commit -m "feat(tui): always allow a suggested rule; drop the session auto-approval

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Telegram — "Always allow" buttons, the owner chat, no in-memory auto-approval

**Files:**
- Delete: `clients/telegram/auto_approval.go`
- Modify: `internal/core/permissions.go` (delete `PermissionModeForSessionApproval`)
- Modify: `clients/telegram/constants.go` (`cbApprovalGlobal`)
- Modify: `clients/telegram/keyboards.go` (`approvalKeyboard`)
- Modify: `clients/telegram/callbacks.go` (`handleCallbackQuery`, `resolveApprovalCallback`; add `approvalStatus`)
- Modify: `clients/telegram/run_render.go` (`renderApprovalUpdates`), `clients/telegram/render_helpers.go` (`renderApprovalText`)
- Modify: `clients/telegram/helpers.go` (add `ownerChat`)
- Modify: `clients/telegram/command_runtime.go` (`dispatcher`) and its callers in `commands.go`, `prompts.go`, `messages.go`
- Modify: `clients/telegram/worker_types.go`, `clients/telegram/worker_constructor.go` (drop `autoEdits`)
- Test: `clients/telegram/approvals_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `clients/telegram/approvals_test.go` (and import `"github.com/Suren878/matrixclaw/internal/permission"`):

```go
func approvalButtons(t *testing.T, request SendMessageRequest) []string {
	t.Helper()
	markup, ok := request.ReplyMarkup.(*InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("reply markup = %#v", request.ReplyMarkup)
	}
	var buttons []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			buttons = append(buttons, button.CallbackData)
		}
	}
	return buttons
}

func TestApprovalButtonsOfferGlobalRulesOnlyInTheOwnerChat(t *testing.T) {
	suggested := core.Approval{ID: "a1", RunID: "run", State: core.ApprovalStatePending, ToolName: "bash", Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}}
	plain := core.Approval{ID: "a2", RunID: "run", State: core.ApprovalStatePending, ToolName: "memory"}
	owner := chatTarget{kind: telegramTargetChat, chatID: 42, externalKey: "42"}
	guest := chatTarget{kind: telegramTargetGuest, chatID: 42, guestQueryID: "q", externalKey: "guest:q"}
	for _, tc := range []struct {
		target   chatTarget
		approval core.Approval
		want     string
	}{
		{owner, suggested, "ao:a1 as:a1 ag:a1 ad:a1 ar:a1"},
		{guest, suggested, "ao:a1 as:a1 ad:a1 ar:a1"},
		{owner, plain, "ao:a2 ad:a2 ar:a2"},
	} {
		api := &approvalBotAPI{}
		worker := &Worker{api: api, config: Config{AllowedUserID: 42}}

		if err := worker.renderApprovalUpdates(context.Background(), tc.target, []core.Approval{tc.approval}, "run", newRunDeliveryState()); err != nil {
			t.Fatal(err)
		}

		if got := strings.Join(approvalButtons(t, api.sent[0]), " "); got != tc.want {
			t.Errorf("%s %s: buttons = %s, want %s", tc.target.kind, tc.approval.ID, got, tc.want)
		}
	}
}

func TestAlwaysGlobalNeedsTheOwnerChat(t *testing.T) {
	for _, allowed := range []int64{0, 42} {
		var resolved []core.ApprovalResolveRequest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request core.ApprovalResolveRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			resolved = append(resolved, request)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(core.ApprovalResponse{Approval: core.Approval{ID: "a1", State: core.ApprovalStateApproved, Suggestion: &permission.Suggestion{Tool: "bash", Pattern: "go test:*"}}})
		}))
		api := &approvalBotAPI{}
		worker := &Worker{api: api, config: Config{AllowedUserID: allowed, BaseURL: server.URL, ClientName: "telegram-test", DaemonHTTPClient: server.Client()}, prompts: map[string]controlplane.PromptData{}}

		err := worker.handleCallbackQuery(context.Background(), &CallbackQuery{ID: "cq", From: &User{ID: 42}, Message: &Message{MessageID: 7, Chat: Chat{ID: 42, Type: "private"}}, Data: cbApprovalGlobal + "a1"})
		server.Close()

		if err != nil {
			t.Fatal(err)
		}
		if allowed == 0 && (len(resolved) != 0 || api.sent[0].Text != "Only the owner can keep a rule for every session.") {
			t.Fatalf("open bot: resolved = %+v sent = %+v", resolved, api.sent)
		}
		if allowed == 42 && (len(resolved) != 1 || resolved[0] != (core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeGlobal})) {
			t.Fatalf("owner chat: resolved = %+v", resolved)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./clients/telegram/ -run 'TestApprovalButtonsOfferGlobalRulesOnlyInTheOwnerChat|TestAlwaysGlobalNeedsTheOwnerChat'`
Expected: FAIL to compile — `undefined: cbApprovalGlobal`.

- [ ] **Step 3: Delete the in-memory auto-approval**

```bash
git rm clients/telegram/auto_approval.go
```

`clients/telegram/worker_types.go`: delete the `autoEdits        map[string]struct{}` field of `Worker`; `clients/telegram/worker_constructor.go`: delete `autoEdits:        map[string]struct{}{},`. `internal/core/permissions.go` keeps only `NormalizePermissionMode`:

```go
package core

import "strings"

func NormalizePermissionMode(mode string) PermissionMode {
	switch PermissionMode(strings.ToLower(strings.TrimSpace(mode))) {
	case PermissionModeAcceptEdits:
		return PermissionModeAcceptEdits
	case PermissionModeFullAuto:
		return PermissionModeFullAuto
	default:
		return PermissionModeDefault
	}
}
```

- [ ] **Step 4: Owner chat, buttons and answers**

`clients/telegram/helpers.go`, before `targetFromMessage`:

```go
// ownerChat reports whether target is the owner's private chat, the only
// Telegram chat that may change global permission rules.
func (w *Worker) ownerChat(target chatTarget) bool {
	return w.config.AllowedUserID != 0 && target.isChat() && target.chatID == w.config.AllowedUserID
}
```

`clients/telegram/constants.go`, the approval callbacks:

```go
	cbApprovalOnce    = "ao:"
	cbApprovalSession = "as:"
	cbApprovalGlobal  = "ag:"
	cbApprovalDeny    = "ad:"
	cbApprovalReason  = "ar:"
```

`clients/telegram/keyboards.go`:

```go
// approvalKeyboard answers one approval; "Always" keeps the suggested rule, and
// only the owner's chat may keep it for every session.
func approvalKeyboard(approval core.Approval, owner bool) *InlineKeyboardMarkup {
	approvalID := approval.ID
	rows := [][]InlineKeyboardButton{{{Text: "✅ Allow", CallbackData: cbApprovalOnce + approvalID}}}
	if approval.Suggestion != nil {
		always := []InlineKeyboardButton{{Text: "♾ Always: session", CallbackData: cbApprovalSession + approvalID}}
		if owner {
			always = append(always, InlineKeyboardButton{Text: "🌐 Always: global", CallbackData: cbApprovalGlobal + approvalID})
		}
		rows = append(rows, always)
	}
	rows = append(rows, []InlineKeyboardButton{
		{Text: "❌ Deny", CallbackData: cbApprovalDeny + approvalID},
		{Text: "✍️ Deny with reason", CallbackData: cbApprovalReason + approvalID},
	})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}
```

`clients/telegram/run_render.go` (`renderApprovalUpdates`): delete the `autoApprovesEditApproval` branch; the message is sent with the owner-aware keyboard:

```go
		reply, err := w.sendTelegramMessage(ctx, SendMessageRequest{
			ChatID:      target.chatID,
			Text:        clipTelegramText("Approval required\n\n" + renderApprovalText(approval)),
			ReplyMarkup: approvalKeyboard(approval, w.ownerChat(target)),
		})
```

`clients/telegram/render_helpers.go` (`renderApprovalText`): before the description lines,

```go
	if approval.Suggestion != nil {
		lines = append(lines, "Always: "+approval.Suggestion.String())
	}
```

`clients/telegram/callbacks.go`: import `"github.com/Suren878/matrixclaw/internal/permission"`; the approval cases of `handleCallbackQuery` become

```go
	case strings.HasPrefix(cq.Data, cbApprovalOnce):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalOnce), core.ApprovalResolveRequest{Approved: true})
	case strings.HasPrefix(cq.Data, cbApprovalSession):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalSession), core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeSession})
	case strings.HasPrefix(cq.Data, cbApprovalGlobal):
		if !w.ownerChat(target) {
			return w.sendText(telegramCtx, target, "Only the owner can keep a rule for every session.")
		}
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalGlobal), core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeGlobal})
	case strings.HasPrefix(cq.Data, cbApprovalDeny):
		return w.resolveApprovalCallback(telegramCtx, target, cq, strings.TrimPrefix(cq.Data, cbApprovalDeny), core.ApprovalResolveRequest{})
	case strings.HasPrefix(cq.Data, cbApprovalReason):
		return w.askDenialReason(telegramCtx, target, strings.TrimPrefix(cq.Data, cbApprovalReason))
```

and `resolveApprovalCallback` (with the new `approvalStatus`) is:

```go
func (w *Worker) resolveApprovalCallback(ctx context.Context, target chatTarget, cq *CallbackQuery, approvalID string, request core.ApprovalResolveRequest) error {
	if prompt, ok := w.prompt(target.externalKey); ok && prompt.SubmitCommandPrefix == controlplane.DenyWithReasonPrompt(approvalID).SubmitCommandPrefix {
		w.clearPrompt(target.externalKey)
	}
	approval, err := w.daemon(target.externalKey).ResolveApproval(ctx, approvalID, request)
	if err != nil {
		return w.editOrSend(ctx, target, cq.Message.MessageID, fmt.Sprintf("Resolve approval failed: %v", err), nil)
	}
	return w.editOrSend(ctx, target, cq.Message.MessageID, approvalStatus(approval, request)+"\n\n"+renderApprovalText(approval), nil)
}

func approvalStatus(approval core.Approval, request core.ApprovalResolveRequest) string {
	switch {
	case !request.Approved:
		return "Denied"
	case request.Always == permission.ScopeSession && approval.Suggestion != nil:
		return "Always allowed in this session: " + approval.Suggestion.String()
	case request.Always == permission.ScopeGlobal && approval.Suggestion != nil:
		return "Always allowed everywhere: " + approval.Suggestion.String()
	default:
		return "Approved"
	}
}
```

`clients/telegram/command_runtime.go`: the dispatcher knows whether the chat is the owner's:

```go
func (w *Worker) dispatcher(target chatTarget) *controlplane.Dispatcher {
	runtime := clientruntime.ControlplaneRuntime{
		Client:     w.config.ClientName,
		WorkingDir: w.config.WorkingDir,
		Daemon: func(externalKey string) (*daemonclient.Client, error) {
			return w.daemon(externalKey), nil
		},
		Owner: w.ownerChat(target),
	}
	return controlplane.New(runtime, w.config.WorkingDir)
}
```

Every caller already has `target`; replace `w.dispatcher().Handle(` with `w.dispatcher(target).Handle(` in `commands.go` (1 call), `prompts.go` (2) and `messages.go` (3):

```bash
sed -i 's/w\.dispatcher()\.Handle(/w.dispatcher(target).Handle(/' clients/telegram/commands.go clients/telegram/prompts.go clients/telegram/messages.go
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -w clients/telegram internal/core/permissions.go && go vet ./clients/telegram/ ./internal/core/ && go test ./clients/telegram/`
Expected: `ok`; `grep -rn "autoEdit\|canAllowSessionApproval\|PermissionModeForSessionApproval" clients internal` prints nothing.

- [ ] **Step 6: Full suite and commit**

```bash
go build ./... && go vet ./... && go test ./... && go test -race ./clients/telegram/
git status --short
git add internal/core/permissions.go clients/telegram/constants.go clients/telegram/keyboards.go clients/telegram/callbacks.go clients/telegram/run_render.go \
  clients/telegram/render_helpers.go clients/telegram/helpers.go clients/telegram/command_runtime.go clients/telegram/commands.go clients/telegram/prompts.go \
  clients/telegram/messages.go clients/telegram/worker_types.go clients/telegram/worker_constructor.go clients/telegram/approvals_test.go
git commit -m "feat(telegram): always allow a suggested rule; global rules only in the owner chat

Removes the in-memory session auto-approval.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

(`git rm` in Step 3 already staged the deletion of `auto_approval.go`.)

---

### Task 9: Record how stage 4b was built

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`
- Modify: `docs/TELEGRAM.md`

- [ ] **Step 1: Spec notes**

In the spec, right after the "Implementation notes (as built, stage 4a)" block at the end of section 4, add:

```markdown
### Implementation notes (as built, stage 4b)

- **Subjects** come from the optional `tools.SubjectProvider`
  (`PermissionSubject(call) permission.Subject` with a kind: file, directory,
  command, domain, name); tools without one match only whole-tool rules. Rules
  name MCP tools as `mcp` and multiedit as `edit`.
- **Bash** lines that write files, assign or declare variables, substitute,
  expand `$VAR`, run a wrapper (`sudo`, `env`, `xargs`, `sh -c`, …) or an
  `-exec`-style flag are never allowed by a pattern rule; a whole-tool allow
  (`bash`, `full_auto`) still allows them. Deny rules beat presets, also
  `full_auto`.
- **One check**: `core.checkPermission`, asked by `Tools.Authorize` (deny) and
  by `executeToolWithGrant` (every pipeline, granted replays included).
- **Suggestions** are stored with the approval (`approvals.suggestion_json`);
  "Always allow" is `{"approved": true, "always": "session"|"global"}`.
- **Rules** live in `permission_rules` (global: `session_id NULL`); API
  `GET/POST /v1/sessions/{id}/permission-rules`, `DELETE
  /v1/permission-rules/{id}`; `/permissions add|delete`. Global rules are
  changed only by the TUI and the Telegram owner chat (a client-side check).
```

- [ ] **Step 2: Telegram doc**

In `docs/TELEGRAM.md`, the `/permissions` line of the command list becomes:

```text
/permissions     approval mode and permission rules
```

- [ ] **Step 3: Commit**

```bash
git status --short
git add docs/superpowers/specs/2026-09-23-long-running-agent-design.md docs/TELEGRAM.md
git commit -m "docs: record how stage 4b permission rules were built

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Spec coverage (stage 4b)

| Spec requirement | Task |
|---|---|
| `internal/permission` leaf package: rules, subjects, bash parsing, mode presets | 1 |
| Rules `{tool, pattern, effect, scope}` in `permission_rules` | 2 |
| `PermissionSubject` per executor: bash → command, read/write/edit/glob/grep → absolute path, web_fetch → domain, MCP → `server__tool` | 3 |
| Read-only tools subject to deny rules; a deny rule yields `Blocked by rule <…>` | 1, 4 |
| Order deny → ask → allow → mode preset → tool default; modes as named presets | 1, 4 |
| Bash parsed with `mvdan.cc/sh/v3`: every simple command allowed; redirects to files, `VAR=`, substitutions, `-exec`/`-toolexec` → ask | 1 |
| Evaluated in one place for `Tools.Authorize` and `core.ExecuteTool` (API, voice, MCP server, replays) | 4 |
| Approval prompt: Allow / Always allow (suggested rule, session or global) / Deny with reason — TUI and Telegram | 5, 7, 8 (+ stage 4a) |
| Global rules only by the owner (TUI, Telegram owner chat), never by Telegram guests | 6, 8 |
| Children inherit the parent session's rules; "Always allow" on a bridged child approval writes to the parent session | 4, 5 |
| Globs resolved to absolute paths when a rule is saved | 6 |
| `/permissions` list/delete (and add) | 6 |
| Delete `clients/telegram/auto_approval.go` | 8 |
| Manual check on the test stand: `/permissions add allow bash go test:*` then a run calling `go test ./...` needs no approval; a deny rule on `read` blocks the model with `Blocked by rule …`; Telegram shows the global button only in the owner chat | after Task 9 |
