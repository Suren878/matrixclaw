# Subagents

The `agent` tool starts a child agent on one self-contained task. The child
sees none of the parent's conversation, works with a restricted tool set and
returns only its final report. Code: `internal/core/subagents*.go`,
`internal/core/agent_profiles.go`; prompts in
`internal/agent/prompt/subagents.go`.

## The agent tool

| Field | Meaning |
|---|---|
| `description`, `prompt` | label shown to the user; the complete brief |
| `agent_type` | the child's profile (below); default `general` |
| `readonly` | read-only tools for any type |
| `isolation` | `shared` (the parent's directory) or `worktree` (its own git worktree) |
| `background` | return a task id at once; the report arrives as a message |
| `runtime`, `model` | child runtime (native `matrixclaw` or an external agent) and model |
| `output_schema` | a JSON schema the final report must match |

The `agent_type` and `runtime` enums list only what the run may pick now.
`agent_type` and `output_schema` work with the native runtime only.

Tools that come with it (children never get them):

- `agent_steer {task_id, message}` sends a running background child a steer
  input; its run reads it before its next step.
- `worktree_merge {task_id, paths?}` and `worktree_discard {task_id}` (below).

## Agent types

A type is one `core.AgentProfile`, whether builtin or read from a file: a
prompt section for the child, a tool allowlist (empty: every tool a child
gets), read-only, default model, step budget, and `LeanContext` (leave
project instructions over 2 KB out). The profile is stored with the task
(`tasks.agent_json`), so a child resumed after a restart keeps it.

| Type | Tools | Notes |
|---|---|---|
| `general` | every child tool | today's child |
| `explore` | read, grep, glob, ls, repo_map, read-only bash | read-only; reports `path:line`; lean context |
| `plan` | read, grep, glob, ls, repo_map, read-only bash, checkpoint_diff | read-only; numbered plan with files and risks |
| `review` | as plan | read-only; reviews the diff (git, checkpoint_diff), findings by severity |
| `verify` | as plan, plus task_output, task_kill, todo_write and full bash | runs builds, tests and linters, never edits; failures with minimal repros |

`checkpoint_diff` is listed only when the daemon has file checkpoints. The
allowlist is applied to the child's tool list and again to every call.

### Agent files

Markdown files with YAML frontmatter, the body being the prompt:

```markdown
---
name: db-expert          # default: the file name; lowercase, digits, - and _
description: knows the schema and migrations
tools: [read, grep, glob, bash]   # or "Read, Grep"; omit for every child tool
readonly: true
model: gpt-5.1-mini      # a model of the parent's provider; "inherit" = parent's
budget_steps: 40         # capped at the daemon's subagent budget
---
You are the database expert of this project...
```

Keys it does not know (such as Claude Code's `color`) are ignored, so Claude
Code agent files work. A file that cannot be read is logged once and left
out.

- **User agents**: `agents/*.md` next to setup.json
  (`~/.config/matrixclaw/agents`). They may redefine a builtin type, which is
  how to give `explore` a cheaper model.
- **Project agents**: the nearest `.matrixclaw/agents/*.md` from the working
  directory up to the git root. They may not redefine a builtin type.

**Trust.** User and project agents are offered only to coding runs: a run
with tools and a working directory that does not answer a reply-once address
(a Telegram guest query or inline message). That is the gate the project's
`AGENTS.md` files already pass, and a project agent can do no more than they
can: its prompt is text from the same repository, its tools only narrow what
a general child has, its budget is capped, its model is one the parent could
pick itself, and every call still goes through the permission rules and
approvals. Guests therefore never get user or project types.

## Output schema

`output_schema` is checked twice. When the child would stop, a reply that is
not a JSON value matching the schema (the whole reply, or its one fenced code
block) is sent back once with the validation error, through the engine's Stop
seam (`coreHooks.Stop`, after the user's Stop hooks). When the task ends, a
matching answer becomes the compacted JSON; a still wrong one fails the task
and keeps the text with the error.

## Worktrees

`isolation: worktree` gives a writing child its own git worktree, so several
run at once.

- **Base.** The parent's files as they are now, uncommitted and untracked
  (not ignored) files included: a temporary index loads HEAD, adds the
  working tree, `write-tree` and `commit-tree`. The parent's index and stash
  are never touched.
- **Place.** `$DATA/worktrees/<repo>-<hash>/<task id>` on branch
  `mc/<task id>`; the child works in the same subdirectory the parent is in.
- **Confinement.** The child's working directory, the files a changing call
  names and the path arguments of its bash lines must stay inside the
  worktree (`tools.CheckConfinedCall`); reads elsewhere are allowed. It is a
  guard against mistakes, not a sandbox.
- **End.** Whatever the run's outcome, the worktree's changes are committed to
  the branch (`--no-verify`, the repository's identity or `matrixclaw`), and
  the result gets `Worktree`, `Branch` and `Changed` (diffstat and files)
  lines. A branch without changes is removed with its worktree.
- **Merge.** `worktree_merge` (asks; checkpointed like an edit) applies
  `git diff base..mc/<task>` to the parent's tree with `git apply --3way`
  through a temporary index. `paths`, relative to the parent's directory,
  limit it. Conflicts leave markers and are listed; the worktree stays. A
  full merge without conflicts removes worktree and branch.
- **Discard.** `worktree_discard` removes worktree and branch.
- **Prune.** `Core.Recover` removes worktrees whose task is unknown, dropped
  them, or ended more than 7 days ago, deletes their branches and runs
  `git worktree prune`.

## Orchestration

The parent's guidance teaches the fan-out: split a large change into slices
touching different files, start one background worktree child per slice,
`await` them, `worktree_merge` each, then run the build and tests (or a
`verify` child) before reporting.
