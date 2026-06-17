# Worktree Triage — 2026-06-17

This is the current cleanup map for local worktrees under `.worktrees/`.
Do not merge any dirty worktree directly into `main`; extract small, reviewed
changes only.

## Summary

| Worktree | State | Recommendation |
|---|---|---|
| `browser-module` | One unique commit (`60c2dee`) plus a large dirty browser/MCP/web diff and many untracked files. Dirty tree `go test ./...` passes, but `golangci-lint run ./...` reports 97 issues from the stale draft. Safe pieces were manually ported to `main` as `5e780cd` (managed Chromium executable/revision repair), `0eac510` (orphaned running run failure), and `cf65148` (shell guard for managed browser setup). | Keep as reference only. Do not merge the dirty tree wholesale; extract future browser fixes as small TDD patches from current `main`. |
| `docs-cleanup-20260611` | Removed locally after manual triage. Useful docs were adapted into `main` as `d419d9e` (`docs/BROWSER.md`) and `b275686` (architecture, Telegram, and external-agent guides). Remaining dirty rewrite deleted current docs and was discarded. | Closed. Start any further docs refresh from current `main`. |
| `refactor-terminal-ui-stack` | Removed locally after manual triage. One isolated setup prompt editor bug was ported as `79efa1e`. Remaining dirty route-stack/controlplane rewrite touched 70+ files, had 85 lint issues, and was discarded. | Closed. Start future terminal/controlplane work from current `main`; do not recreate the stale route-stack rewrite. |
| `stabilize-runtime-stability` | Removed locally after manual triage. Useful parts were ported to `main` as `1869693` (Codex turn event subscriptions), `6ab7950` (external runtime panic reporting), and `660cc8d` (subagent aftermath store errors). | Closed. Do not recreate from the old branch; start future runtime work from current `main`. |
| `subagents-v2` | Removed locally after manual port to `main` as `a4c11a4` (Telegram typing refresh during active runs). | Closed. Do not recreate from the old branch; start future Telegram/subagent work from current `main`. |

## Stale Branches

- `feature-usage-plan-search` was merged into `main` and deleted locally.
- `docs-cleanup-20260611` and `refactor-terminal-ui-stack` were manually
  triaged, then their dirty worktrees and local branches were deleted.
- `stabilize-runtime-stability` and `subagents-v2` were manually ported, then
  their clean worktrees and local branches were deleted.
- Remaining `browser-module` MCP create/delete API changes are obsolete on
  current `main`; provider-visible browser content changes need a separate
  message-model design instead of direct salvage from the stale diff.

## Order

1. Keep `browser-module` as a reference for a future browser feature rebuild.
2. Start any new refactor from current `main`, not from stale worktrees.
