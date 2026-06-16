# Contributing

matrixclaw is a daemon-backed Go project. Keep changes small and aligned with
the daemon-first architecture.

## Development

```bash
go build ./...
go vet ./...
```

Build local binaries:

```bash
go build -o ./bin/matrixclaw ./cmd/matrixclaw
go build -o ./bin/matrixclawd ./cmd/matrixclawd
```

## Expectations

- Keep runtime ownership in `matrixclawd`; clients should render daemon state.
- Follow `docs/TESTING.md` when adding new tests. The current policy is to write
  acceptance/use-case tests for user-visible daemon behavior, not narrow tests
  that only freeze incidental implementation details.
- Do not commit secrets, local databases, generated binaries, or personal setup files.
- Prefer small focused pull requests over broad rewrites.
