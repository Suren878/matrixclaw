# Language servers

MatrixClaw runs language servers for the native agent. They give it two
things:

- **Diagnostics after edits**: the errors an `edit`, `multiedit`, `write` or
  `apply_patch` left, in the edited files and in the files that use them
  (the callers a changed signature broke), appended to the tool result.
- **The `lsp` tool**: definition, references, implementation, hover and
  symbols, answered by the language's own server instead of a text search.

Code: `internal/lsp` (JSON-RPC client, server manager, diagnostics, tool);
`internal/hooks` asks it after edits ([HOOKS.md](HOOKS.md#diagnostics-after-edits)).

## Servers

| Language | Files | Project root (nearest directory up with) | Server |
|---|---|---|---|
| `go` | `.go` | `go.work`, `go.mod` | `gopls` |
| `typescript` | `.ts` `.tsx` `.mts` `.cts` `.js` `.jsx` `.mjs` `.cjs` | `tsconfig.json`, `jsconfig.json`, `package.json` | `typescript-language-server --stdio` |
| `python` | `.py` `.pyi` | `pyproject.toml`, `pyrightconfig.json`, `setup.py`, `setup.cfg`, `requirements.txt`, `Pipfile` | `pyright-langserver --stdio`, else `pylsp` |
| `rust` | `.rs` | `Cargo.toml` | `rust-analyzer` |

MatrixClaw installs none of them. A server is used only when its program is
on the `PATH` of the agent's shell commands (the login shell's environment),
and only for files below a project root; a file outside any project gets
the command check instead.

- One server runs per language and project root, started on first use
  (an edit or an `lsp` call) under the daemon's process supervisor, so it
  dies with the daemon. It gets the environment of the agent's shell
  commands, without API keys or `MATRIXCLAW_*` variables. Its stderr goes to
  `lsp/lsp-<language>.log` in the data directory.
- A server nobody used for 10 minutes is shut down (`shutdown`, `exit`, then
  killed); the next use starts it again. Daemon shutdown stops them all.
- A server that fails to start or initialize (45 s) is logged and not tried
  again for 5 minutes; meanwhile its files get the command checks and the
  `lsp` tool is not offered for its projects. A rustup proxy without the
  `rust-analyzer` component is such a server.
- gopls runs with `diagnosticsDelay: 100ms` (default 1 s), so the errors in
  importing packages come sooner.

The server keeps at most 64 documents open per root, closing the one used
longest ago. Before every check or `lsp` call, open documents whose files
changed on disk (a shell command, `git checkout`) are sent again.

## Diagnostics after an edit

For each edited file (at most five a call) whose extension has no command of
its own in `diagnostics.commands`:

1. The file is opened on its server (`didOpen`), or its new text is sent
   (`didChange`, full text, the next version), then `didSave` when the server
   asks for it.
2. The check waits for a `publishDiagnostics` of that file newer than the
   version sent (servers that send no version: one that came after the
   change), at most 5 s including the server's start.
3. Then until the server settled: gopls is asked `gopls.diagnose_files`,
   which returns once it diagnosed everything the edit affects, importers
   included; other servers until they sent nothing for 150 ms. At most 10 s
   in all.

Only errors (severity 1) are reported, at most 20 per file and then
"... and N more errors in <file>", followed by the errors of up to five other
files, newest first:

```text
Diagnostics after edit:
/home/me/proj/a.go:3:28: cannot use "x" (untyped string constant) as int value in return statement
Errors in other files:
/home/me/proj/c/c.go:5:27: not enough arguments in call to m.A
	have ()
	want (int)
/home/me/proj/b.go:3:25: not enough arguments in call to A
	have ()
	want (int)
```

Columns count characters (the servers count UTF-16 units). "Errors in other
files" from gopls are every error it knows after the edit, so they are
current; from the other servers they are only those published while the
edited files were checked, so an error another file had before the edit is
not repeated. typescript-language-server and pyright diagnose open files
only, and rust-analyzer's `cargo check` results usually arrive after the
wait, so other-file errors come mostly from gopls.

A file whose server is missing, still starting after 5 s, or silent about the
file gets the command check of [HOOKS.md](HOOKS.md#diagnostics-after-edits)
instead, in the same "Diagnostics after edit:" section. Nothing blocks or
undoes the edit.

**Latency.** With gopls warm on this repository (about 1,000 Go files), the
check added 20-200 ms per edit, median about 90 ms, including the importers'
check (`gopls.diagnose_files`); a signature change of a function used in 11
files of other packages reported all of them in about 100 ms. The first edit
of a project starts gopls: about 0.6 s. The command check it replaces
(`gofmt -l` and `go vet .`) took 1.0-1.1 s for `internal/core` and reported
no other files.

## The `lsp` tool

```json
{"operation": "references", "file_path": "internal/lsp/manager.go", "line": 120, "character": 12}
```

| Operation | Needs | Answer |
|---|---|---|
| `definition`, `implementation` | `line`, `character` | where the symbol is defined or implemented |
| `references` | `line`, `character` | every use, declaration included |
| `hover` | `line`, `character` | type and documentation |
| `document_symbols` | | the outline of `file_path` |
| `workspace_symbols` | `query` | matching symbols in the whole project (`file_path` names the language and project) |

`line` and `character` are 1-based, as in `read` output. Locations come back
as `path:line:column: source line`, at most 100 (300 outline entries).

- It is read-only and covered by `read` rules (`Spec.CoveredBy`): a rule on
  `read` for the file applies to the call, and locations in files a deny or
  ask rule would catch, credential files and places that hold keys are left
  out with a count.
- It is offered to a run (subagents and plan mode included) only when a
  server would serve the session's working directory: a project root of a
  language above it whose server is installed and not failing. Otherwise the
  model never sees it.

## Configuration

In setup.json, under `diagnostics` (owned by the owner):

```json
"diagnostics": {
  "lsp": {
    "servers": {
      "python": ["pylsp"],
      "rust": []
    }
  }
}
```

| Key | Meaning |
|---|---|
| `lsp.disabled` | `true` turns every language server off: edits get the command checks and the `lsp` tool is gone |
| `lsp.servers.<language>` | the server's command line as a list (`["pylsp"]`, `["/opt/bin/gopls", "-remote=auto"]`); an empty list turns the language off. Languages: `go`, `typescript`, `python`, `rust` |
| `commands.<.ext>` | a command of its own for an extension wins over its language server ([HOOKS.md](HOOKS.md#diagnostics-after-edits)) |
| `disabled` | `true` turns every check after edits off, servers included; the `lsp` tool stays |

Loading is strict: an unknown key, a language not in the list or a blank
program name makes setup.json fail to load, naming the key. Changes apply on
the next use; a server whose command changed is restarted.
