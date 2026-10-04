# Contributing

matrixclaw is a Go module (Go 1.26). Start with
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the package map.

## Build

```bash
go build ./...
scripts/build_release.sh          # stamped binaries in ./bin
```

or build one binary: `go build -o bin/matrixclawd ./cmd/matrixclawd`.

## Check before you push

CI runs exactly these (see [docs/TESTING.md](docs/TESTING.md)):

```bash
gofmt -l $(git ls-files '*.go')   # must print nothing
go test ./...
go build ./...
go vet ./...
golangci-lint run                 # v2, .golangci.yml
```

Run `go test -race` on the engine, core, store and shelltask packages after
concurrency changes.

## Code rules

- The daemon owns state; clients render it and send commands. Enforce access
  in the daemon, not only in a client.
- No dead code, compatibility shims, unused parameters or duplicate helpers.
  A refactor deletes what it replaces in the same change.
- Doc comments are at most 4 lines and say what the code does now: no
  history, no "used to", no plan references.
- Tests pin observable behaviour (API responses, run states, messages,
  events, files), not private formatting or incidental helpers.
- Keep changes focused; one concern per commit, each commit green.
- Never commit secrets, databases, binaries or personal setup files. Examples
  use empty strings or environment variable names such as `$OPENAI_API_KEY`.
- User-visible changes go under "Unreleased" in [CHANGELOG.md](CHANGELOG.md).
