# Contributing

matrixclaw is a daemon-backed Go project. Keep changes small, tested, and aligned
with the daemon-first architecture.

## Development

```bash
go test ./...
go vet ./...
```

Build local binaries:

```bash
go build -o ./bin/matrixclaw ./cmd/matrixclaw
go build -o ./bin/matrixclawd ./cmd/matrixclawd
```

## Expectations

- Keep runtime ownership in `matrixclawd`; clients should render daemon state.
- Add or update tests for behavior changes.
- Do not commit secrets, local databases, generated binaries, or personal setup files.
- Prefer small focused pull requests over broad rewrites.
