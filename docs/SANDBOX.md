# Shell sandbox

The native agent's `bash` commands can run under an OS sandbox, so that a long
unattended run cannot damage the machine outside its project, as Codex's
sandbox modes do. The sandbox confines what commands write and, optionally,
whether they reach the network; it never limits what they read.

## Modes

| Mode | Commands may write |
|---|---|
| `off` | everything the daemon's user may |
| `workspace_write` | the session's working directory and its git directories, the temporary directories, the package caches and `writable` |
| `read_only` | the temporary directories and the package caches only |

Every confined command may also write device files such as `/dev/null`.

- The working directory is the session's, never a call's `working_dir`. A
  working directory that is the home directory or holds it is not writable:
  a sandbox that lets commands rewrite `~/.bashrc` or setup.json protects
  nothing.
- Git directories: the `.git` of the repository the working directory is in,
  and for a git worktree its `.git/worktrees/<name>` and the main repository's
  `.git`, so commits work. A worktree subagent's session works in its
  worktree, so its commands write only there (`sandbox.WorkspaceRoots`).
- Temporary directories: `/tmp`, `/var/tmp` and `$TMPDIR`, shared with the
  rest of the machine: the model often writes a file in `/tmp` with one command
  and reads it with the `read` tool, which a private `/tmp` per command would
  break.
- Package caches (`sandbox.DefaultCaches`): `~/.cache` (the XDG cache: Go's
  build cache, pip, uv, golangci-lint, ...), `~/Library/Caches` (macOS),
  `~/go/pkg/mod`, `~/.npm`, `~/.local/share/pnpm/store`, `~/.yarn/berry/cache`,
  `~/.cargo/registry`, `~/.cargo/git`, `~/.m2/repository`, `~/.gradle/caches`.
  One missing below an existing directory other than home (`~/go/pkg/mod` once
  `~/go/pkg` exists) is created so its tool can fill it.
- `read_only` keeps the caches and temporary directories writable because the
  read-only tools subagents may run, such as `go vet`, need a build directory.

## When commands are confined

`daemon.sandbox` in setup.json:

```json
"daemon": {
  "sandbox": {
    "mode": "workspace_write",
    "network": false,
    "writable": ["/srv/build-output"],
    "caches": ["~/.cache/go-build", "~/go/pkg/mod"]
  }
}
```

- `mode` absent (the default): commands nobody approves are confined, and the
  others run as before. A `full_auto` session's commands run under
  `workspace_write`; plan mode and read-only subagents run under `read_only`;
  `default` and `accept_edits` sessions, where the user approves each command,
  are not confined. When this system cannot confine commands they run
  unconfined, and `/status` says so.
- `mode` set: every native command runs under it (plan mode and read-only
  subagents under `read_only`), and when this system cannot confine commands
  `bash` refuses to run them. `off` confines nothing.
- `network: false` cuts confined commands off the network; a command is
  refused when the mechanism cannot do that. Absent, the network stays on:
  `go mod download`, `npm install` and `git fetch` keep working.
- `writable` adds directories `workspace_write` commands may write; `caches`
  replaces the default cache list (`[]` allows none). Paths are absolute or
  start with `~/`. An unknown mode or a relative path fails loading setup.json.

Changes apply to the next command. Hooks and the checks after edits run
unconfined: they are the user's own commands from setup.json or a trusted
project. External agents (Codex, Claude Code) use their own sandboxes.

## Mechanisms

`internal/sandbox` finds one at the first command and keeps it until the
daemon restarts:

1. **bubblewrap** (`bwrap` on `PATH`, when a probe runs): `--ro-bind / /`,
   `--dev /dev` (a minimal `/dev`), `--bind` for each writable directory and
   `--unshare-net` to cut the network.
2. **Landlock** (Linux 5.13+ with `landlock` in the LSM list): the daemon runs
   its own binary (`/proc/self/exe`, the running image even after an update)
   with the hidden subcommand `__sandbox <policy JSON> bash -c <command>`. The
   helper sets `no_new_privs`, restricts its own thread with a ruleset (read
   and execute everywhere, write only the allowed directories) and execs bash
   from that thread, so the command and everything it starts stay confined.
   The daemon itself is never restricted. Landlock ABI 4+ (Linux 6.7+) can cut
   the network: no TCP bind or connect. UDP (DNS lookups) and Unix sockets
   still work. Earlier ABIs cannot cut it.
3. **sandbox-exec** on macOS: a Seatbelt profile that allows everything but
   writes outside the allowed directories and, with the network cut,
   networking.

Without one (Windows, an old kernel), `/status` reports the sandbox as
unavailable.

## What the model is told

- The run context of a native run states its sandbox in one line: the mode,
  the writable project directories, and that a refused write fails with
  "Permission denied".
- A foreground command that fails with "Permission denied", "Operation not
  permitted" or "Read-only file system" (or, with the network cut, a
  resolution or unreachable error) under a sandbox gets a note after its exit
  code: where the sandbox allows writes, and to use a path inside the project
  or ask the user.
- When a set mode cannot be applied, `bash` answers with the reason and that
  the user must fix it.

`/status` (`GET /v1/server/status`, field `sandbox`) shows the mode and the
mechanism, for example `workspace_write in full_auto, read_only in plan mode
and read-only subagents; Landlock ABI 4, network can be cut`.

## Limits

- The sandbox confines writes and network, not processes: a confined command
  can still signal other processes of the same user.
- `.git` is writable, so a command can add a git hook that runs later outside
  the sandbox (Codex keeps `.git` read-only for this reason, which breaks
  commits). The caches can hold code that later runs unconfined too.
- `sudo` and other setuid programs do not gain privileges under Landlock
  (`no_new_privs`).
- Under Landlock a confined command cannot create a missing cache directory
  directly in the home directory (`~/.cache` itself).
