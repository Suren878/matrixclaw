# Worktree Triage — 2026-06-17

This is the current cleanup map for local worktrees under `.worktrees/`.
Do not merge any dirty worktree directly into `main`; extract small, reviewed
changes only.

## Summary

| Worktree | State | Recommendation |
|---|---|---|
| `browser-module` | One unique commit (`60c2dee`) plus a large dirty browser/MCP/web diff and many untracked files. | Keep as reference only. Rebuild browser module from `main` in a fresh branch if needed. Do not merge the dirty tree wholesale. |
| `docs-cleanup-20260611` | Branch tip is merged into `main`; dirty docs rewrite deletes several current docs and adds replacement docs. | Treat as abandoned docs experiment. Start any docs refresh from `main`; remove this worktree after confirming the dirty draft is not needed. |
| `refactor-terminal-ui-stack` | Branch tip is merged into `main`; dirty TUI/controlplane refactor touches 70+ files. | Treat as abandoned. Mine only specific ideas such as dialog stack/navigation if they solve a current bug; otherwise remove after review. |
| `stabilize-runtime-stability` | Clean worktree with two unique commits: external agent worker stabilization and Codex turn subscription fixes. | High-value candidate. Cherry-pick or manually port after the current cleanup commit, then run focused external-agent tests and full `go test ./...`. |
| `subagents-v2` | Clean worktree with one unique commit: Telegram typing indicator refresh during runs. | Small candidate. Port separately after runtime stabilization or cherry-pick if it still applies cleanly. |

## Stale Branches

- `feature-usage-plan-search` is merged into `main` and has no active worktree.
  It can be deleted locally.

## Order

1. Finish and commit current `main` cleanup.
2. Delete the merged `feature-usage-plan-search` branch.
3. Port `subagents-v2` typing indicator refresh as a small standalone change.
4. Port `stabilize-runtime-stability` in a separate branch/commit group.
5. Review `browser-module` as a future feature, not as a merge source.
6. Remove `docs-cleanup-20260611` and `refactor-terminal-ui-stack` only after
   confirming their dirty drafts are no longer needed.
