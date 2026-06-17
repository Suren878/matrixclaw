# Worktree Triage — 2026-06-17

This is the current cleanup map for local worktrees under `.worktrees/`.
Do not merge any dirty worktree directly into `main`; extract small, reviewed
changes only.

## Summary

| Worktree | State | Recommendation |
|---|---|---|
| `browser-module` | One unique commit (`60c2dee`) plus a large dirty browser/MCP/web diff and many untracked files. Dirty tree `go test ./...` passes, but `golangci-lint run ./...` reports 97 issues from the stale draft. The managed Chromium executable/revision repair fix was manually ported to `main` as `5e780cd`. | Keep as reference only. Do not merge the dirty tree wholesale; extract future browser fixes as small TDD patches from current `main`. |
| `docs-cleanup-20260611` | Branch tip is merged into `main`; dirty docs rewrite deletes several current docs and adds replacement docs (`ARCHITECTURE`, `BROWSER`, `EXTERNAL_AGENTS`, `TELEGRAM`). | Treat as abandoned docs experiment. Start any docs refresh from `main`; remove this worktree after confirming the dirty draft is not needed. |
| `refactor-terminal-ui-stack` | Branch tip is merged into `main`; dirty TUI/controlplane refactor touches 70+ files. Dirty tree `go test ./...` passes, but `golangci-lint run ./...` reports 85 issues, including many unused helpers. | Treat as abandoned UI experiment. Mine only specific ideas such as dialog stack/navigation if they solve a current bug; otherwise remove after review. |
| `stabilize-runtime-stability` | Removed locally after manual triage. Useful parts were ported to `main` as `1869693` (Codex turn event subscriptions), `6ab7950` (external runtime panic reporting), and `660cc8d` (subagent aftermath store errors). | Closed. Do not recreate from the old branch; start future runtime work from current `main`. |
| `subagents-v2` | Removed locally after manual port to `main` as `a4c11a4` (Telegram typing refresh during active runs). | Closed. Do not recreate from the old branch; start future Telegram/subagent work from current `main`. |

## Stale Branches

- `feature-usage-plan-search` was merged into `main` and deleted locally.
- `docs-cleanup-20260611` and `refactor-terminal-ui-stack` are merged by branch
  tip, but both worktrees have dirty local drafts. Delete only together with an
  explicit decision to discard those drafts.
- `stabilize-runtime-stability` and `subagents-v2` were manually ported, then
  their clean worktrees and local branches were deleted.

## Order

1. Keep `browser-module` as a reference for a future browser feature rebuild.
2. Decide whether to discard the dirty drafts in `docs-cleanup-20260611` and
   `refactor-terminal-ui-stack`; if yes, remove those worktrees and local
   branches.
3. Start any new refactor from current `main`, not from the stale worktrees.
