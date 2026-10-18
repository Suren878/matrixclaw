# Repository map

`repo_map` shows the model the most important definitions of a code base,
one signature line each, grouped by file, most important files first. It is
Aider's repo map without tree-sitter: `internal/repomap` is pure Go, and the
tool (`internal/tools/tool_repo_map.go`) lists the files the way the search
tools do.

```text
internal/tools/paths.go:
13│type FilesystemPathPolicy struct {
⋮
28│func ResolveFilesystemPath(workingDir string, value string) (FilesystemPathPolicy, error)
internal/glob/gitignore.go:
12│type Ignore struct {
⋮
27│func (ig Ignore) Add(base string, content []byte) Ignore
```

## The tool

`repo_map {path?, focus_files?, focus_symbols?, max_tokens?}` is read-only and
covered by `read` rules (`Spec.CoveredBy`), like `grep`, `glob` and `ls`; it
meets the rules by its directory and leaves out what `Call.Skip` and the
secret guard leave out. Files come from the search tools' listing
(`searchFiles`: `git ls-files` or a `.gitignore`-aware walk), without hidden
entries, at most the 5000 most recently modified. `focus_files` are resolved
against the working directory; files outside `path` are ignored.
`max_tokens` defaults to 2048 and is held to 1024–4096. The call does not know
the model's window, so Aider's `window/8` is not applied.

The coding manual names it in its tool section when the run has it
(`internal/agent/prompt/coding.go`).

## Tags

A file gives definitions (name, line, the text shown) and reference counts
per identifier.

- **Go** (`tags_go.go`, `go/parser`): functions; methods, shown with their
  receiver and a multi-line signature joined into one line; types; interface
  methods; top-level constants and variables. Struct fields are not
  definitions. References are identifiers that do not resolve to a local or
  parameter of the file, are not builtins or imported package names, and are
  not selected from a standard-library package (`strings.TrimSpace` counts
  nothing; `store.Open` counts `Open`). Files with the `// Code generated ...
  DO NOT EDIT.` header give nothing.
- **Other languages** (`tags.go`, chroma lexers): Python, JavaScript,
  TypeScript, Rust, Java, Kotlin, Scala, C#, C, C++, Swift, Ruby, PHP. A name
  token is a definition when it follows a definition keyword of its language
  (`def`, `class`, `fn`, `function`, `interface`, ...; JavaScript `const`,
  `let`, `var` only on unindented lines, TypeScript `type` only before `=` or
  `<`, C `struct` only before a body); in C, C++, Java and C# when it is
  followed by `(` and preceded by a type or modifier on its line; in
  JavaScript and TypeScript when it opens an indented line ending with `{`
  and is followed by `(` (methods). Every other name token is a reference.
- Skipped: paths through `vendor`, `node_modules`, `third_party` or
  `testdata`, `*.min.js`, files over 1 MB (lexed files over 256 KB), files
  with a NUL byte in their first 8 KB, and text averaging over 300 characters
  a line.

## Ranking

As in Aider's `get_ranked_tags`:

- Files are nodes. For every identifier both defined and referenced, each
  referencing file gets an edge to each defining file, weighted
  `mul × sqrt(references)`, where `mul` is ×10 when the identifier is a focus
  symbol, ×10 for a snake, kebab or camel case name of 8 or more characters,
  ×0.1 for a `_private` name and ×0.1 when more than five files define it.
  An edge from a focus file counts ×50. A definition nobody references gets a
  0.1 self-edge. Unlike Aider, edges join only files of one language family
  (Go, Python, JavaScript/TypeScript, Rust, JVM, C/C++, ...), so `dict.get` in
  a Python script does not rank a Rust `get`.
- PageRank (networkx's: damping 0.85, 100 iterations, tolerance 1e-6 per
  node). With focus files or symbols, the teleport and dangling distribution
  goes to the focus files and to files whose path names a focus symbol, each
  `100/len(files)`.
- A file's rank flows to the definitions its edges point at, in proportion
  to the edge weights. Definitions are listed by that rank (focus files left
  out), then the files without a listed definition by file rank.

## Fitting the budget

Aider's binary search over the number of listed entries: start at
`budget/25`, render, estimate tokens (`agent/context.EstimateTextTokens`),
stop within 15% of the budget, else keep the largest map under it. Rendering
groups entries by file in rank order and shows each file's chosen lines in
line order, numbered, with the enclosing definition of an indented one (a
Python method's class, a Go interface method's type) and `⋮` for skipped
lines; lines are cut at 120 characters.

## Cache and timing

`repomap.Mapper` keeps the tags of the 8 most recently mapped directories in
memory, per file keyed by size and modification time; a map reads only new or
changed files (in parallel) and forgets files no longer listed. The ranking
and rendering run on every call. The tool executor holds one `Mapper` for the
daemon's life; nothing is written to disk.

Measured on this repository (1058 Go files, 8 shared cores): first call
about 0.25 s, later calls 60–95 ms. On a 1400-file Rust and TypeScript
repository (6.5 MB of Rust) the first call takes 2.5–6 s, since chroma's
regexp-based lexers run at about 1 MB/s per core and contend on regexp2's
runner cache; later calls 110–175 ms.
