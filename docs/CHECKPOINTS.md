# File checkpoints

Before a native run changes files, the daemon snapshots the session's working
directory, so a long unattended run can be reviewed and undone: `/undo`,
`/rewind` and the model's own `checkpoint_diff`.

## When a checkpoint is taken

Once per tool batch (the calls of one model reply), before the first call of
the batch that may change files starts: a mutating file or shell tool (`write`,
`edit`, `multiedit`, `bash`, ...) or an `agent` call whose child writes the
shared directory. Read-only batches take none, and a batch whose tree equals
the run's previous checkpoint records nothing new. The seam is
`coreTools.Authorize` (`internal/core/file_checkpoints.go`); the engine's
checkpoint before each model call starts the next batch.

A snapshot captures what bash commands change too (`sed -i`, code generators,
`go mod tidy`), not only the file tools. It covers the session's working
directory; a command that writes elsewhere (`working_dir`, absolute paths) is
not captured. A snapshot that fails or takes over a minute is logged once per
directory and the run goes on without it.

Each checkpoint is a row of `file_checkpoints` (session, run, newest message
`seq`, the tool that triggered it, working directory, git tree). Each working
directory keeps its newest 50.

## Shadow repositories

`internal/checkpoint` keeps one git repository per work tree under
`<data dir>/checkpoints/<hash>/`, used with `GIT_DIR=<shadow>` and
`GIT_WORK_TREE=<work tree>`. A snapshot is `git add --all` and `git
write-tree` on the shadow index, so the user's index, branches and history are
never touched, and the user's global and system git configuration (filters,
hooks) is not read.

- When the working directory is inside a git repository, the work tree is that
  repository's top level and everything is limited to the working directory,
  so the repository's `.gitignore` files and `info/exclude` apply as they do
  for git. Ignored files are never snapshotted nor touched by a restore.
- Nested repositories (a directory with its own `.git`) are left out: their
  files are theirs to keep.
- Settings: `core.autocrlf=false`, `core.fsmonitor=false`,
  `feature.manyFiles`, `index.version=4`, `core.untrackedCache`, no automatic
  gc. The project's `.gitattributes` filters and line-end conversions are
  overridden, so files come back byte for byte.
- Refused: the file system root, the home directory and every directory above
  it, `~/Desktop`, `~/Documents`, `~/Downloads`, and directories with more than
  50,000 files. Sessions there run without checkpoints.
- Each kept snapshot has a ref; once checkpoints are trimmed, the refs no row
  keeps are dropped and, at most hourly, `git gc --prune=now` removes the rest.

## Restoring

A restore brings the working directory back to a checkpoint's tree: files
changed or deleted since are written back, files added since are deleted
(with the directories they leave empty). It is refused while the session has
an active run. Before it changes anything it checkpoints the current state
(shown as "before a restore"), so a restore can itself be undone.

The model is told: the restore appends an engine note to the session naming
the restored files and saying that contents read before may be stale. The
file tools' read ledger needs no extra step: it holds a hash of the content
the session saw, so `write`, `edit` and `multiedit` refuse a restored file
until it is read again.

The transcript is not rewound; only files are.

## Commands and API

- `/undo` restores the state before the last run's first change, after a
  confirmation listing what changes.
- `/rewind` lists the session's checkpoints with what restoring each would
  change now; picking one asks for confirmation, then restores.
- `GET /v1/sessions/{id}/checkpoints` lists checkpoints, newest first, with
  those changes; `unavailable` says why there are none (checkpoints off, an
  unsupported directory).
- `POST /v1/sessions/{id}/checkpoints/{checkpoint}/restore` restores
  (owner only).
- `checkpoint_diff {since?, path?}` (read-only) shows the model a diffstat and
  unified diff since the run's first checkpoint (`run_start`, the default) or
  a checkpoint id of the session or its parent, optionally limited to a path.

## Turning it off

```json
{ "daemon": { "checkpoints": { "enabled": false } } }
```

Checkpoints are on when the key is absent. The daemon reads it at start; with
checkpoints off nothing is snapshotted and `checkpoint_diff` is not offered.
