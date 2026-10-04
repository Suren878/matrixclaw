# Testing

Tests pin behaviour a user or a client can observe. A test that only freezes
private formatting, a helper's incidental output or a branch that cannot fail
in a visible way is not worth keeping.

## CI

`.github/workflows/test.yml` runs on every push to `main` and every pull
request; `release.yml` runs the same checks (without lint) before building a
tag:

```bash
gofmt -l $(git ls-files '*.go')   # must print nothing
go test ./...
go build ./...
go vet ./...
golangci-lint run                 # v2 (CI pins v2.12) with .golangci.yml; CI fails on new issues only
```

`.golangci.yml` uses the `standard` linters plus `errorlint`, `nilnesserr`,
`nolintlint`, `rowserrcheck`, `sqlclosecheck` and `wastedassign`, with the
`gofmt` formatter.

The iOS package (`clients/ios`) has its own `swift test` suite, which CI does
not run.

## Running locally

- `go test ./...` takes well under a minute on a warm build cache;
  `internal/core` is the slowest package.
- After concurrency changes to the engine, scheduler, tasks or store, also run
  the race detector:

  ```bash
  go test -race ./internal/agent/... ./internal/core ./internal/store \
    ./internal/shelltask ./internal/permission
  ```

- `internal/shelltask` and the core task tests start real `bash` process
  groups, so they need a Unix host.
- Terminal rendering goldens live in `clients/terminal/chat/runtime/testdata`;
  after an intended visual change rewrite them with
  `go test ./clients/terminal/chat/runtime -update` and review the diff.

## Where to test what

Drive matrixclaw through its stable boundaries:

| Boundary | How |
|---|---|
| Agent engine | `internal/agent/agenttest`: `NewScriptedModel(turns...)` and `NewFixture()` fakes for every port |
| Core workflows | a real `store.NewSQLite` in `t.TempDir()` and `core.New(store)`, with a scripted model; runs, approvals, tasks, recovery |
| HTTP API | `api.New(deps).Handler()` under `httptest`; `routes_test.go` lists every request the clients send |
| Commands and screens | a `controlplane.Dispatcher` over a real API server per role (`internal/controlplane/daemon_test.go`) |
| Store | temporary databases; upgrade tests open an old-shape database and check the result (`schema_upgrade_test.go`) |
| Providers | mock HTTP servers: request shape, streaming, tool calls, stop reasons, error mapping |
| Web tools | `internal/webtools/testdata` page fixtures |
| Terminal | read-model tests and rendering goldens |

Rules:

- Name tests after the scenario (`TestAGrantRunsTheCallOnceAndReadsOnlyItsRun`),
  arranged as given / when / then.
- Core tests build their own store and core, so they call `t.Parallel()`.
  Keep new ones parallel and free of package-level state; a test that swaps
  the global logger stays sequential.
- Restart behaviour is tested by building a new core over the same store and
  calling `Core.Recover`.
