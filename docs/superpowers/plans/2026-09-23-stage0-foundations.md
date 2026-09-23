# Stage 0 (Foundations) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Lay the groundwork that later stages of the long-running agent build on: a leaf `internal/transcript` package, integer message ordering (`messages.seq`) with point lookups, normalised provider usage, per-generation `run_steps` accounting, idle stream timeouts, and a tolerant iOS `RunStatus`. The agent's behaviour stays the same.

**Architecture:** The message types move out of `core` into `internal/transcript` with no aliases left behind. SQLite gains `messages.seq` (added with `ensureColumn`, backfilled and indexed) and a `run_steps` table. The old `run_usage` table is folded into `run_steps` and then dropped. The current loop in `internal/core` writes one `run_steps` row per model generation, including compaction summaries. Every provider adapter maps its wire usage into one `providers.Usage` shape and uses a shared HTTP client with an idle-read timeout. The iOS package decodes unknown run statuses instead of failing.

**Tech Stack:** Go 1.26 (module `github.com/Suren878/matrixclaw`), SQLite through `modernc.org/sqlite` (JSON1 available), `net/http`, Swift 5.9 package `clients/ios` (XCTest), Docker image `swift:6.0-jammy` to verify Swift on this Linux host.

**Spec:** `docs/superpowers/specs/2026-09-23-long-running-agent-design.md` (Stages table, row 0).
**Binding contracts:** section "Stage 0 (foundations) provides" of the cross-stage contracts.
**Baseline:** `main` at `6176876`. Line numbers in "Files:" refer to that commit. Task 1 adds one import line to many files, so later line numbers can drift by 1–2.

---

## Ground rules (read before Task 1)

- Work directly on `main` in `/root/projects/matrixclaw`. Each task ends with one commit that builds and passes `go test ./...`.
- Another agent session may edit this repo at the same time. Before every task, run `git status --short`. If a file this task touches shows up as modified by someone else, stop and ask. Stage only the files listed in the task. Never use `git add -A` or `git add .`.
- Delete replaced code and add no shims or aliases. Doc comments are at most 4 lines and never describe history. Tests cover observable behaviour only (see `docs/TESTING.md`).
- Tools: `GOIMPORTS=$(go env GOPATH)/bin/goimports` (installed at `/root/go/bin/goimports`). Run `gofmt -l internal clients cmd` before each commit; it must print nothing.
- Every commit message ends with a blank line followed by:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`
- The noise `core: after run execution for "..." failed: ... sql: database is closed` in `go test` output is pre-existing background logging and does not mean a test failed. Only `FAIL` lines count.

## File map

| File | Responsibility | Task |
|---|---|---|
| `internal/transcript/message.go` (moved from `internal/core/types_message.go`) | Message, parts, `NormalizeMessageParts`; new `Seq` | 1, 2 |
| `internal/store/schema_messages.go` (new) | `messages.seq` column, backfill, indexes | 2 |
| `internal/store/sqlite_messages.go` | seq-aware insert/scan/order; `GetMessage`, `HasToolResult`, `ListMessagesAfter` | 2, 3 |
| `internal/store/sqlite_messages_test.go` (new) | ordering, legacy backfill, point lookups | 2, 3 |
| `internal/core/ports.go` | `MessageStore` and `UsageStore` method sets | 3, 5 |
| `internal/core/{execution_turn,tool_call_prepare,tool_approvals,runs}.go` | use point lookups instead of full-session scans | 3 |
| `internal/core/messages.go`, `internal/api/messages.go` (+ `messages_test.go`) | `GET /v1/messages?after_seq=` | 4 |
| `internal/store/migrations/001_init.sql`, `internal/store/schema.go` | canonical schema: `seq`, `run_steps`, no `run_usage` | 2, 5 |
| `internal/store/schema_run_steps.go` (new) | fold legacy `run_usage` into `run_steps` | 5 |
| `internal/store/sqlite_run_steps.go` (new, replaces `sqlite_usage.go`) | `SaveRunStep`, `ListRunSteps`, `ListUsageRecords` | 5 |
| `internal/core/types_run.go`, `types_usage.go`, `usage.go` | `RunStep`, per-run usage records, `recordRunStep` | 5, 6, 7 |
| `internal/core/{execution_turn,execution_status,execution_generation,context,context_compact}.go` | write steps; delete `saveRunUsage` | 5 |
| `internal/controlplane/usage.go` (+ `usage_test.go`) | `/usage` output | 5, 7 |
| `internal/api/runs.go` (+ `runs_test.go`), `internal/core/contracts.go` | `GET /v1/runs/{id}/steps` | 6 |
| `internal/providers/contract.go` | normalised `Usage` | 7 |
| `internal/providers/ai/*/` | usage mapping; shared HTTP client | 7, 8 |
| `internal/core/{context,context_report,execution_status}.go`, `internal/controlplane/context.go` | drop `core.ProviderUsage`; `/context` label | 7 |
| `internal/providers/httpclient.go` (+ test), `generation_errors.go` | idle and response-header timeouts | 8 |
| `clients/ios/Package.swift`, `Sources/MatrixclawClient/Models.swift`, `Tests/MatrixclawClientTests/ModelDecodingTests.swift` | tolerant `RunStatus`, `seq`, usage fields | 9 |

---

### Task 1: Move message types to `internal/transcript`

This is a mechanical move with no behaviour change, so there is no new test. The existing suite is the safety net.

**Files:**
- Move: `internal/core/types_message.go:1-106` → `internal/transcript/message.go` (`package transcript`, content unchanged)
- Modify (mechanical, about 58 files): the 24 files outside `core` that reference `core.<name>`. These are in `clients/telegram` (8), `clients/terminal/chat/runtime` (2), `clients/terminal/chat/viewmodel` (1), `internal/clientruntime` (2), `internal/controlplane/dispatcher.go`, `internal/daemonclient` (2), `internal/modules/voice/realtime/manager_stream.go`, `internal/store` (3), and 4 `core_test` files. The remaining files are every `internal/core/*.go` file that uses the names unqualified. Find them with `git grep` as shown below.

- [ ] **Step 1: Check the working tree is clean**

Run: `cd /root/projects/matrixclaw && git status --short`
Expected: no output. If there is output, stop and ask the owner: the move rewrites many files, and a concurrent edit would be swept into this commit.

- [ ] **Step 2: Run the move**

```bash
cd /root/projects/matrixclaw
MOVE_DIR=/tmp/matrixclaw-stage0-move; rm -rf "$MOVE_DIR"; mkdir -p "$MOVE_DIR"
GOIMPORTS=$(go env GOPATH)/bin/goimports
NAMES='Message|MessageRole|MessageRoleUser|MessageRoleAssistant|MessageRoleSystem|MessageRoleTool|MessagePart|MessagePartKind|MessagePartKindText|MessagePartKindImage|MessagePartKindReasoning|MessagePartKindToolCall|MessagePartKindToolResult|MessagePartKindFinish|TextPart|ImagePart|ReasoningPart|ToolCallPart|ToolResultPart|FinishPart|NormalizeMessageParts'

# 1. The type file moves (git mv stages the rename).
mkdir -p internal/transcript
git mv internal/core/types_message.go internal/transcript/message.go
sed -i 's/^package core$/package transcript/' internal/transcript/message.go

# 2. Qualified uses outside core: core.X -> transcript.X, then fix imports.
git grep -lE "\bcore\.($NAMES)\b" -- '*.go' > "$MOVE_DIR/files.txt"
xargs sed -E -i "s/\bcore\.($NAMES)\b/transcript.\1/g" < "$MOVE_DIR/files.txt"
xargs "$GOIMPORTS" -w < "$MOVE_DIR/files.txt"

# 3. Unqualified uses inside package core. gofmt -r cannot tell a type from a
#    struct-literal key such as FinishPart{Message: ...}, so let the compiler
#    point at every "undefined: X" and prefix exactly those positions.
cat > "$MOVE_DIR/qualify.py" <<'EOF'
import collections, re, subprocess, sys
names, pkg, goimports = set(sys.argv[1].split("|")), sys.argv[2], sys.argv[3]
pattern = re.compile(r"^(?:\./)?(\S+\.go):(\d+):(\d+): undefined: (\w+)$")
touched = set()
while True:
    out = subprocess.run(["go", "test", "-gcflags=-e", "-run", "^$", "-count=1", pkg], capture_output=True, text=True)
    edits = collections.defaultdict(set)
    for line in (out.stdout + out.stderr).splitlines():
        m = pattern.match(line.strip())
        if m and m.group(4) in names:
            edits[m.group(1)].add((int(m.group(2)), int(m.group(3))))
    if not edits:
        break
    for path, spots in edits.items():
        with open(path, encoding="utf-8") as f:
            lines = f.read().split("\n")
        for line_no, col in sorted(spots, reverse=True):
            raw = lines[line_no - 1].encode("utf-8")
            lines[line_no - 1] = (raw[:col - 1] + b"transcript." + raw[col - 1:]).decode("utf-8")
        with open(path, "w", encoding="utf-8") as f:
            f.write("\n".join(lines))
        subprocess.run([goimports, "-w", path], check=True)
        touched.add(path)
print("\n".join(sorted(touched)))
EOF
python3 "$MOVE_DIR/qualify.py" "$NAMES" ./internal/core/ "$GOIMPORTS" >> "$MOVE_DIR/files.txt"
sort -u "$MOVE_DIR/files.txt" -o "$MOVE_DIR/files.txt"
wc -l < "$MOVE_DIR/files.txt"
```

Expected: the last line prints `58`, give or take a few if the repo moved on. Compiler columns are byte offsets, and the script edits bytes, so the emoji constants in `context.go` are handled.

- [ ] **Step 3: Verify**

```bash
cd /root/projects/matrixclaw
go build ./... && go vet ./... && gofmt -l internal clients cmd
git grep -nE "\bcore\.(Message|MessageRole\w*|MessagePart|MessagePartKind\w*|TextPart|ImagePart|ReasoningPart|ToolCallPart|ToolResultPart|FinishPart|NormalizeMessageParts)\b" -- '*.go'
grep -n "^type Message\b\|^func NormalizeMessageParts" internal/core/*.go
go test ./... 2>&1 | grep -E "^(FAIL|---)" ; echo "exit=$?"
diff <(git status --short | grep -v '^R' | awk '{print $2}' | sort) "$MOVE_DIR/files.txt" && echo "file list matches"
```

Expected: build and vet succeed; `gofmt -l` prints nothing; both greps print nothing; the test grep prints nothing (`exit=1`); the last line prints `file list matches`.

- [ ] **Step 4: Commit**

```bash
cd /root/projects/matrixclaw
git add internal/transcript/message.go $(cat /tmp/matrixclaw-stage0-move/files.txt)
git status --short | grep -v '^[RM] '
```

Expected: no output, meaning every touched file is staged. Then:

```bash
git commit -m "refactor: move message types to internal/transcript

core, store, api and the clients import transcript directly; core no
longer defines Message or its part types.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: `messages.seq` — database-wide message order

`created_at` is RFC3339Nano text, and it does not sort correctly as a string: `…10:00:00Z` sorts after `…10:00:00.5Z`. After this task every message gets an integer `seq`, database-wide and monotonic, and all ordering uses it.

`internal/store/sqlite.go:34-38` holds a single connection (`SetMaxOpenConns(1)`). Two things follow from that:
- The `MAX(seq)+1` subquery inside the `INSERT` is atomic.
- The backfill must fully drain its `SELECT` before it opens a transaction. Otherwise it deadlocks on the one connection.

**Files:**
- Create: `internal/store/sqlite_messages_test.go`
- Create: `internal/store/schema_messages.go`
- Modify: `internal/transcript/message.go` (struct `Message`, formerly `internal/core/types_message.go:17-28`)
- Modify: `internal/store/migrations/001_init.sql:52-64` (messages table), `:282-283` (drop `idx_messages_session_created_at`)
- Modify: `internal/store/schema.go:29` (call the migration before the `external_agent_sessions` ensureColumn)
- Modify: `internal/store/sqlite_messages.go:68-73` (`ListMessages` query), `:270-286` (`insertMessage`), `:288-309` (`scanMessage`)

- [ ] **Step 1: Write the failing tests**

Create `internal/store/sqlite_messages_test.go`:

```go
package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

var testEpoch = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

func openTestStore(t *testing.T, path string) *store.SQLiteStore {
	t.Helper()
	st, err := store.NewSQLite(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func newTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	st := openTestStore(t, filepath.Join(t.TempDir(), "matrixclaw.db"))
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func createTestSession(t *testing.T, st *store.SQLiteStore, id string) {
	t.Helper()
	session := core.Session{ID: id, Title: id, Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
}

func saveTestMessage(t *testing.T, st *store.SQLiteStore, message transcript.Message) {
	t.Helper()
	if message.Role == "" {
		message.Role = transcript.MessageRoleUser
	}
	if message.Content == "" {
		message.Content = message.ID
	}
	if err := st.SaveMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
}

func messageIDsAndSeqs(messages []transcript.Message) ([]string, []int64) {
	ids := make([]string, 0, len(messages))
	seqs := make([]int64, 0, len(messages))
	for _, message := range messages {
		ids = append(ids, message.ID)
		seqs = append(seqs, message.Seq)
	}
	return ids, seqs
}

func TestMessagesAreOrderedByInsertionSeq(t *testing.T) {
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	saveTestMessage(t, st, transcript.Message{ID: "first", SessionID: "s1", CreatedAt: testEpoch.Add(time.Second)})
	saveTestMessage(t, st, transcript.Message{ID: "second", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, transcript.Message{ID: "third", SessionID: "s1", CreatedAt: testEpoch.Add(500 * time.Millisecond)})

	for _, tc := range []struct {
		limit int
		want  []string
	}{
		{0, []string{"first", "second", "third"}},
		{2, []string{"second", "third"}},
	} {
		messages, err := st.ListMessages(context.Background(), "s1", tc.limit)
		if err != nil {
			t.Fatal(err)
		}
		ids, seqs := messageIDsAndSeqs(messages)
		if len(ids) != len(tc.want) {
			t.Fatalf("limit %d: ids=%v, want %v", tc.limit, ids, tc.want)
		}
		for i := range ids {
			if ids[i] != tc.want[i] || (i > 0 && seqs[i] <= seqs[i-1]) {
				t.Fatalf("limit %d: ids=%v seqs=%v, want %v with increasing seq", tc.limit, ids, seqs, tc.want)
			}
		}
	}
}

func TestLegacyMessagesGetSeqInCreatedAtOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'assistant', runtime_id TEXT NOT NULL DEFAULT 'matrixclaw', parent_session_id TEXT NOT NULL DEFAULT '', hidden INTEGER NOT NULL DEFAULT 0, working_dir TEXT NOT NULL DEFAULT '', provider_id TEXT NOT NULL DEFAULT '', model_id TEXT NOT NULL DEFAULT '', permission_mode TEXT NOT NULL DEFAULT 'default', status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, parts_json TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL DEFAULT '', FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE)`,
		`CREATE INDEX idx_messages_session_created_at ON messages(session_id, created_at)`,
		`INSERT INTO sessions(id, title, status, created_at, updated_at) VALUES('s1', 's1', 'active', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('late', 's1', '', 'user', 'late', '2026-09-23T10:00:00.5Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('early', 's1', '', 'user', 'early', '2026-09-23T10:00:00Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('tie-a', 's1', '', 'user', 'tie-a', '2026-09-23T10:00:01Z')`,
		`INSERT INTO messages(id, session_id, run_id, role, content, created_at) VALUES('tie-b', 's1', '', 'user', 'tie-b', '2026-09-23T10:00:01Z')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	wantIDs := []string{"early", "late", "tie-a", "tie-b"}
	wantSeqs := []int64{1, 2, 3, 4}
	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		messages, err := st.ListMessages(context.Background(), "s1", 0)
		_ = st.Close()
		if err != nil {
			t.Fatal(err)
		}
		ids, seqs := messageIDsAndSeqs(messages)
		for i := range wantIDs {
			if len(ids) != len(wantIDs) || ids[i] != wantIDs[i] || seqs[i] != wantSeqs[i] {
				t.Fatalf("open %d: ids=%v seqs=%v, want %v %v", reopen, ids, seqs, wantIDs, wantSeqs)
			}
		}
	}

	st := openTestStore(t, path)
	defer func() { _ = st.Close() }()
	saveTestMessage(t, st, transcript.Message{ID: "new", SessionID: "s1", CreatedAt: testEpoch})
	messages, err := st.ListMessages(context.Background(), "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "new" || messages[0].Seq != 5 {
		t.Fatalf("new message=%+v, want id new seq 5", messages)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/`
Expected: FAIL (build failed) with `message.Seq undefined (type transcript.Message has no field or method Seq)`.

- [ ] **Step 3: Implement**

In `internal/transcript/message.go`, add `Seq` right after `ID` in `type Message struct`:

```go
type Message struct {
	ID        string        `json:"id"`
	Seq       int64         `json:"seq,omitempty"`
	SessionID string        `json:"session_id"`
```

In `internal/store/migrations/001_init.sql`, make the `messages` table end like this:

```sql
    updated_at TEXT NOT NULL DEFAULT '',
    seq INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
```

In the same file, delete these two lines and the blank line after them. Old databases have this index; `migrateMessageSeq` drops it.

```sql
CREATE INDEX IF NOT EXISTS idx_messages_session_created_at
    ON messages(session_id, created_at);
```

The seq indexes live in Go, not in `001_init.sql`. The canonical script runs before `ensureColumn`, and an index on a missing column would fail on old databases.

Create `internal/store/schema_messages.go`:

```go
package store

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// migrateMessageSeq numbers messages database-wide; rows written before seq
// existed are numbered by parsed created_at, then rowid.
func migrateMessageSeq(db *sql.DB) error {
	if err := ensureColumn(db, "messages", "seq", `ALTER TABLE messages ADD COLUMN seq INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := backfillMessageSeq(db); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_messages_session_created_at`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_seq ON messages(seq)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_seq ON messages(session_id, seq)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("store: index message seq: %w", err)
		}
	}
	return nil
}

func backfillMessageSeq(db *sql.DB) error {
	type unsequenced struct {
		rowid     int64
		createdAt time.Time
	}
	rows, err := db.Query(`SELECT rowid, created_at FROM messages WHERE seq = 0 ORDER BY rowid`)
	if err != nil {
		return fmt.Errorf("store: list unsequenced messages: %w", err)
	}
	var pending []unsequenced
	for rows.Next() {
		var row unsequenced
		var createdAt string
		if err := rows.Scan(&row.rowid, &createdAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan unsequenced message: %w", err)
		}
		row.createdAt = mustParseTime(createdAt)
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate unsequenced messages: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].createdAt.Before(pending[j].createdAt) })

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin message seq backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var next int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM messages`).Scan(&next); err != nil {
		return fmt.Errorf("store: read max message seq: %w", err)
	}
	for _, row := range pending {
		next++
		if _, err := tx.Exec(`UPDATE messages SET seq = ? WHERE rowid = ?`, next, row.rowid); err != nil {
			return fmt.Errorf("store: backfill message seq: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit message seq backfill: %w", err)
	}
	return nil
}
```

The backfill is idempotent: it only touches rows where `seq = 0`, and new rows always get `seq >= 1`. A second boot finds nothing to do, and the unique index creation is a no-op.

In `internal/store/schema.go`, insert this before the line `if err := ensureColumn(db, "external_agent_sessions", "approval_policy", …` (line 29):

```go
	if err := migrateMessageSeq(db); err != nil {
		return err
	}
```

In `internal/store/sqlite_messages.go`:

1. In `ListMessages`, replace the query literal with:

```go
	query := `
SELECT ` + messageColumns + `
FROM messages
WHERE session_id = ?
ORDER BY seq DESC`
```

2. Replace the head of `insertMessage` (the SQL literal only; the argument list stays the same):

```go
const messageColumns = `id, session_id, run_id, role, content, parts_json, model, provider, created_at, updated_at, seq`

// insertMessage assigns the next database-wide seq in the same statement.
func insertMessage(ctx context.Context, execer sqlExecer, message transcript.Message) error {
	_, err := execer.ExecContext(ctx, `
INSERT INTO messages(id, session_id, run_id, role, content, parts_json, model, provider, created_at, updated_at, seq)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM messages))`,
```

3. In `scanMessage`, add `&message.Seq` as the last scan destination:

```go
	if err := scanner.Scan(&message.ID, &message.SessionID, &message.RunID, &role, &message.Content, &partsJSON, &model, &provider, &createdAt, &updatedAt, &message.Seq); err != nil {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ && go test ./... 2>&1 | grep -E "^(FAIL|---)"`
Expected: `ok  	github.com/Suren878/matrixclaw/internal/store`, and the second command prints nothing.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal clients cmd
git add internal/transcript/message.go internal/store/migrations/001_init.sql internal/store/schema.go internal/store/schema_messages.go internal/store/sqlite_messages.go internal/store/sqlite_messages_test.go
git commit -m "feat(store): order messages by a database-wide seq

messages.seq is added with ensureColumn, backfilled from parsed
created_at then rowid, and assigned MAX(seq)+1 inside the insert.
ListMessages orders by seq; the created_at index is dropped.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Point store lookups and their core callers

This task adds `GetMessage`, `HasToolResult` and `ListMessagesAfter` to the store. Core stops loading whole sessions to find one tool call or one tool result.

**Files:**
- Modify: `internal/store/sqlite_messages_test.go` (append tests)
- Modify: `internal/store/sqlite_messages.go:68-102` (replace `ListMessages` with the lookup block)
- Modify: `internal/core/ports.go:41-45` (`MessageStore`)
- Modify: `internal/core/execution_turn.go:132-152` (`resumeApprovedTools`)
- Modify: `internal/core/tool_call_prepare.go:122-133` (`isNewToolCallMessage`)
- Modify: `internal/core/tool_approvals.go:207-235` (`finishRejectedSubagentDelegateTool`), `:274-282` (`replayApprovedTool`), `:310-333` (`toolCallArgs`)
- Modify: `internal/core/runs.go:112-119` (`AcceptTriggeredRun` replay path)

- [ ] **Step 1: Write the failing test**

In the import block of `internal/store/sqlite_messages_test.go`, add `"errors"` and `"strings"`. Then append:

```go
func toolCallMessage(id string, sessionID string) transcript.Message {
	return transcript.Message{ID: id, SessionID: sessionID, Role: transcript.MessageRoleAssistant, CreatedAt: testEpoch, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: "read", Input: `{"path":"a"}`},
	}}}
}

func toolResultMessage(id string, sessionID string, toolCallID string) transcript.Message {
	return transcript.Message{ID: id, SessionID: sessionID, Role: transcript.MessageRoleTool, CreatedAt: testEpoch, Parts: []transcript.MessagePart{{
		Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: toolCallID, Name: "read", Content: "ok"},
	}}}
}

func TestMessagePointLookups(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	// Saved in this order, so seq is 1..5.
	saveTestMessage(t, st, transcript.Message{ID: "user", SessionID: "s1", CreatedAt: testEpoch})
	saveTestMessage(t, st, toolCallMessage("call-1", "s1"))
	saveTestMessage(t, st, toolResultMessage("result-1", "s1", "call-1"))
	saveTestMessage(t, st, toolResultMessage("result-2", "s2", "call-2"))
	saveTestMessage(t, st, transcript.Message{ID: "answer", SessionID: "s1", Role: transcript.MessageRoleAssistant, CreatedAt: testEpoch})

	t.Run("GetMessage", func(t *testing.T) {
		got, err := st.GetMessage(ctx, "call-1")
		if err != nil {
			t.Fatal(err)
		}
		if got.SessionID != "s1" || got.Seq != 2 || len(got.Parts) != 1 || got.Parts[0].ToolCall == nil || got.Parts[0].ToolCall.Input != `{"path":"a"}` {
			t.Fatalf("GetMessage(call-1)=%+v", got)
		}
		if _, err := st.GetMessage(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("GetMessage(missing) error=%v, want ErrNotFound", err)
		}
	})

	t.Run("HasToolResult", func(t *testing.T) {
		for _, tc := range []struct {
			sessionID, toolCallID string
			want                  bool
		}{
			{"s1", "call-1", true},
			{"s1", "call-2", false},
			{"s2", "call-2", true},
			{"s1", "missing", false},
		} {
			got, err := st.HasToolResult(ctx, tc.sessionID, tc.toolCallID)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("HasToolResult(%s, %s)=%v, want %v", tc.sessionID, tc.toolCallID, got, tc.want)
			}
		}
	})

	t.Run("ListMessagesAfter", func(t *testing.T) {
		for _, tc := range []struct {
			afterSeq int64
			limit    int
			want     []string
		}{
			{0, 0, []string{"user", "call-1", "result-1", "answer"}},
			{2, 0, []string{"result-1", "answer"}},
			{1, 2, []string{"call-1", "result-1"}},
			{5, 0, nil},
		} {
			messages, err := st.ListMessagesAfter(ctx, "s1", tc.afterSeq, tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			ids, _ := messageIDsAndSeqs(messages)
			if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
				t.Errorf("ListMessagesAfter(s1, %d, %d)=%v, want %v", tc.afterSeq, tc.limit, ids, tc.want)
			}
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL (build failed) with `st.GetMessage undefined`, `st.HasToolResult undefined` and `st.ListMessagesAfter undefined`.

- [ ] **Step 3: Implement the store methods**

In `internal/store/sqlite_messages.go`, replace the whole `ListMessages` function (from `func (s *SQLiteStore) ListMessages(` up to the line before `func (s *SQLiteStore) CreateRun(`) with:

```go
func (s *SQLiteStore) GetMessage(ctx context.Context, messageID string) (transcript.Message, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = ?`, strings.TrimSpace(messageID))
	message, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return transcript.Message{}, core.ErrNotFound
	}
	return message, err
}

// HasToolResult reports whether a tool message of the session answers toolCallID.
func (s *SQLiteStore) HasToolResult(ctx context.Context, sessionID string, toolCallID string) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM messages m, json_each(CASE WHEN json_valid(m.parts_json) THEN m.parts_json ELSE '[]' END) p
    WHERE m.session_id = ?
      AND m.role = 'tool'
      AND json_extract(p.value, '$.tool_result.tool_call_id') = ?
)`, strings.TrimSpace(sessionID), strings.TrimSpace(toolCallID)).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("store: has tool result: %w", err)
	}
	return found, nil
}

// ListMessages returns the latest limit messages (all when limit is 0) in seq order.
func (s *SQLiteStore) ListMessages(ctx context.Context, sessionID string, limit int) ([]transcript.Message, error) {
	query := `SELECT ` + messageColumns + `
FROM messages
WHERE session_id = ?
ORDER BY seq DESC`
	args := []any{sessionID}
	if limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, limit)
	}
	messages, err := s.queryMessages(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	reverseMessages(messages)
	return messages, nil
}

// ListMessagesAfter returns up to limit messages (all when limit is 0) with seq > afterSeq, ascending.
func (s *SQLiteStore) ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error) {
	query := `SELECT ` + messageColumns + `
FROM messages
WHERE session_id = ? AND seq > ?
ORDER BY seq ASC`
	args := []any{strings.TrimSpace(sessionID), afterSeq}
	if limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, limit)
	}
	return s.queryMessages(ctx, query, args...)
}

func (s *SQLiteStore) queryMessages(ctx context.Context, query string, args ...any) ([]transcript.Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var messages []transcript.Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate messages: %w", err)
	}
	return messages, nil
}
```

`scanMessage` wraps the scan error with `%w`, so `errors.Is(err, sql.ErrNoRows)` still matches.

In `internal/core/ports.go`, replace `MessageStore` with:

```go
type MessageStore interface {
	SaveMessage(ctx context.Context, message transcript.Message) error
	UpdateMessage(ctx context.Context, message transcript.Message) error
	GetMessage(ctx context.Context, messageID string) (transcript.Message, error)
	HasToolResult(ctx context.Context, sessionID string, toolCallID string) (bool, error)
	ListMessages(ctx context.Context, sessionID string, limit int) ([]transcript.Message, error)
	ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error)
}
```

- [ ] **Step 4: Run the store test to verify it passes**

Run: `go test ./internal/store/`
Expected: `ok`

- [ ] **Step 5: Switch the core callers to point lookups**

Existing core tests cover these paths: approval replay, delegate rejection, triggered-run replay, and repeated tool-call IDs (`TestRepeatedToolCallIDExecutesOnlyOnce`, `TestApprovedToolFailureIsReturnedToModelWithoutReplay`, `run_recovery_test.go`). No new test is needed, because behaviour does not change.

`internal/core/execution_turn.go`, `resumeApprovedTools`: replace the block starting at `messages, err := c.store.ListMessages(ctx, turn.SessionID, 0)` and ending at `completedToolCalls[toolCallID] = struct{}{}` with:

```go
	replayed := map[string]struct{}{}
	waitingApproval := false
	for _, approval := range approvalsForRun(approvedApprovals, turn.RunID) {
		toolCallID := strings.TrimSpace(approval.ToolCallRef)
		if toolCallID == "" {
			continue
		}
		if _, exists := replayed[toolCallID]; exists {
			continue
		}
		done, err := c.store.HasToolResult(ctx, turn.SessionID, toolCallID)
		if err != nil {
			return false, err
		}
		if done {
			continue
		}

		result, err := c.replayApprovedTool(ctx, approval)
		if err != nil && result.ToolResultMessage == nil {
			return false, err
		}
		replayed[toolCallID] = struct{}{}
```

The rest of the loop (steer injection, `result.Approval`) stays as it is.

`internal/core/tool_call_prepare.go`: replace `isNewToolCallMessage` with the version below. Message IDs are primary keys, so a tool-call ID that already belongs to another session used to fail later with a PK error. Now it fails here, with a clear error.

```go
func (c *Core) isNewToolCallMessage(ctx context.Context, sessionID string, toolCallID string) (bool, error) {
	message, err := c.store.GetMessage(ctx, toolCallID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if message.SessionID != sessionID {
		return false, fmt.Errorf("%w: tool call id %q already belongs to another session", ErrInvalidInput, toolCallID)
	}
	return false, nil
}
```

`internal/core/tool_approvals.go`:

1. In `finishRejectedSubagentDelegateTool`, replace everything from `messages, err := c.store.ListMessages(ctx, approval.SessionID, 0)` through the `if strings.TrimSpace(toolCall.ID) == "" { … }` block with:

```go
	toolCallID := strings.TrimSpace(approval.ToolCallRef)
	done, err := c.store.HasToolResult(ctx, approval.SessionID, toolCallID)
	if err != nil || done {
		return err
	}
	toolCall, err := c.sessionToolCallMessage(ctx, approval.SessionID, toolCallID)
	if err != nil {
		return fmt.Errorf("parent delegate tool call: %w", err)
	}
	args, _ := toolCallArgs(toolCall)
```

The `prepared := preparedToolCall{…}` block and `finishToolCall` call that follow stay unchanged. Before, a message without the matching part gave `args == nil`, and `args, _ :=` keeps that behaviour.

2. In `replayApprovedTool`, replace:

```go
	messages, err := c.store.ListMessages(ctx, approval.SessionID, 0)
	if err != nil {
		return ExecuteToolResult{}, err
	}

	args, err := toolCallArgs(messages, approval.ToolCallRef)
	if err != nil {
		return ExecuteToolResult{}, err
	}
```

with:

```go
	toolCall, err := c.sessionToolCallMessage(ctx, approval.SessionID, approval.ToolCallRef)
	if err != nil {
		return ExecuteToolResult{}, err
	}
	args, found := toolCallArgs(toolCall)
	if !found {
		return ExecuteToolResult{}, fmt.Errorf("%w: tool call %s", ErrNotFound, toolCall.ID)
	}
```

3. Replace the whole `toolCallArgs` function with:

```go
// sessionToolCallMessage loads the assistant message that carries toolCallID.
func (c *Core) sessionToolCallMessage(ctx context.Context, sessionID string, toolCallID string) (transcript.Message, error) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return transcript.Message{}, fmt.Errorf("%w: tool call id is required", ErrInvalidInput)
	}
	message, err := c.store.GetMessage(ctx, toolCallID)
	if err == nil && message.SessionID != sessionID {
		err = ErrNotFound
	}
	if errors.Is(err, ErrNotFound) {
		return transcript.Message{}, fmt.Errorf("%w: tool call %s", ErrNotFound, toolCallID)
	}
	return message, err
}

// toolCallArgs returns the input of the tool call part whose id is the message id.
func toolCallArgs(message transcript.Message) (json.RawMessage, bool) {
	for _, part := range message.Parts {
		if part.ToolCall == nil || strings.TrimSpace(part.ToolCall.ID) != message.ID {
			continue
		}
		if strings.TrimSpace(part.ToolCall.Input) == "" {
			return nil, true
		}
		return json.RawMessage(part.ToolCall.Input), true
	}
	return nil, false
}
```

`internal/core/runs.go`, `AcceptTriggeredRun`: replace

```go
		if messages, listErr := c.store.ListMessages(ctx, existing.SessionID, 0); listErr == nil {
			for _, candidate := range messages {
				if candidate.ID == existing.UserMessageID {
					message = candidate
					break
				}
			}
		}
```

with:

```go
		if stored, getErr := c.store.GetMessage(ctx, existing.UserMessageID); getErr == nil {
			message = stored
		}
```

Then fix the imports. This adds `"errors"` to `tool_call_prepare.go`:

```bash
$(go env GOPATH)/bin/goimports -w internal/core/tool_call_prepare.go internal/core/tool_approvals.go internal/core/execution_turn.go internal/core/runs.go
```

`toolResultCallIDs` (`internal/core/tool_helpers.go:46`) stays, because `execution_tools.go` and `run_recovery.go` still use it.

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | grep -E "^(FAIL|---)"`
Expected: build and vet succeed; the grep prints nothing.

- [ ] **Step 7: Commit**

```bash
gofmt -l internal clients cmd
git add internal/store/sqlite_messages.go internal/store/sqlite_messages_test.go internal/core/ports.go internal/core/execution_turn.go internal/core/tool_call_prepare.go internal/core/tool_approvals.go internal/core/runs.go
git commit -m "feat(store): add point message lookups and use them in core

GetMessage, HasToolResult (JSON1 over tool messages) and
ListMessagesAfter(seq) replace full-session scans in approval replay,
delegate rejection, tool-call dedupe and triggered-run replay.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: `GET /v1/messages?after_seq=`

This exposes `ListMessagesAfter` to clients. Telegram will switch to it in its own stage.

**Files:**
- Create: `internal/api/messages_test.go`
- Modify: `internal/core/messages.go:1-13`
- Modify: `internal/api/messages.go:10-30` (GET branch)

- [ ] **Step 1: Write the failing test**

Create `internal/api/messages_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

var apiTestEpoch = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)

func newAPITestServer(t *testing.T) (*Server, *store.SQLiteStore) {
	t.Helper()
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	session := core.Session{ID: "s1", Title: "s1", Kind: core.SessionKindAssistant, Status: core.SessionStatusActive, CreatedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}
	if err := st.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return New(core.New(st)), st
}

func TestListMessagesAfterSeq(t *testing.T) {
	server, st := newAPITestServer(t)
	for _, id := range []string{"m1", "m2", "m3"} {
		message := transcript.Message{ID: id, SessionID: "s1", Role: transcript.MessageRoleUser, Content: id, CreatedAt: apiTestEpoch}
		if err := st.SaveMessage(context.Background(), message); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query      string
		wantStatus int
		wantIDs    string
	}{
		{"session_id=s1&after_seq=1", http.StatusOK, "m2,m3"},
		{"session_id=s1&after_seq=1&limit=1", http.StatusOK, "m2"},
		{"session_id=s1&after_seq=3", http.StatusOK, ""},
		{"session_id=s1&limit=2", http.StatusOK, "m2,m3"},
		{"session_id=s1&after_seq=x", http.StatusBadRequest, ""},
	} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/messages?"+tc.query, nil))
		if recorder.Code != tc.wantStatus {
			t.Fatalf("%s: status=%d body=%s", tc.query, recorder.Code, recorder.Body.String())
		}
		if tc.wantStatus != http.StatusOK {
			continue
		}
		var response core.MessagesResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(response.Messages))
		for _, message := range response.Messages {
			ids = append(ids, message.ID)
		}
		if got := strings.Join(ids, ","); got != tc.wantIDs {
			t.Errorf("%s: ids=%q, want %q", tc.query, got, tc.wantIDs)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/`
Expected: FAIL. `session_id=s1&after_seq=1: ids="m1,m2,m3", want "m2,m3"` (the parameter is ignored), and `after_seq=x` returns status 200.

- [ ] **Step 3: Implement**

Append to `internal/core/messages.go`:

```go
func (c *Core) ListMessagesAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]transcript.Message, error) {
	if normalizeText(sessionID) == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	return c.store.ListMessagesAfter(ctx, normalizeText(sessionID), afterSeq, limit)
}
```

In `internal/api/messages.go`, GET branch, replace `messages, err := s.core.ListMessages(r.Context(), sessionID, limit)` with:

```go
		var messages []transcript.Message
		var err error
		if rawAfter := r.URL.Query().Get("after_seq"); rawAfter != "" {
			afterSeq, parseErr := strconv.ParseInt(rawAfter, 10, 64)
			if parseErr != nil {
				writeErrorMessage(w, http.StatusBadRequest, "invalid after_seq")
				return
			}
			messages, err = s.core.ListMessagesAfter(r.Context(), sessionID, afterSeq, limit)
		} else {
			messages, err = s.core.ListMessages(r.Context(), sessionID, limit)
		}
```

Keep the following `if err != nil { writeError(w, err); return }` unchanged. Then run `$(go env GOPATH)/bin/goimports -w internal/api/messages.go` to add the `transcript` import.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/api/ ./internal/core/`
Expected: both `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal clients cmd
git add internal/core/messages.go internal/api/messages.go internal/api/messages_test.go
git commit -m "feat(api): list messages after a seq cursor

GET /v1/messages?session_id=&after_seq=N returns up to limit messages
with seq > N in ascending order.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: `run_steps` and per-run usage built from it

Today `saveRunUsage` (`internal/core/usage.go:45-98`) rebuilds a single `run_usage` row on every tool turn and at completion. It does this by scanning the whole session's finish parts, and it only counts tool turns plus the final answer. After this task:

- `generateAssistantTurn` (`execution_turn.go:246`) writes one `run_steps` row for every successful `Generate`. That covers tool turns, the text-only final turn, and retried empty replies, which also cost tokens.
- `generateCompactSummary` (`context_compact.go:22`) writes a row with `stop_reason = "compact"` when it runs inside a run. It calls the same session runtime; a manual `/compact` has no run and writes nothing.
- Per-run usage becomes a `GROUP BY` over `run_steps`. Legacy `run_usage` rows become one `legacy` step each, and the table is dropped.

The `run_steps` token columns are filled from the current `providers.Usage` fields (`InputTokens`, `CachedTokens`). Task 7 switches those two lines to the normalised fields.

**Files:**
- Create: `internal/store/sqlite_run_steps_test.go`, `internal/store/schema_run_steps.go`, `internal/store/sqlite_run_steps.go`, `internal/controlplane/usage_test.go`
- Delete: `internal/store/sqlite_usage.go:1-145` (move `boolInt` at `:134-139` to `sqlite_sessions.go`)
- Modify: `internal/store/migrations/001_init.sql:133-149` (`run_usage` → `run_steps`), `:300-301` (drop the run_usage index)
- Modify: `internal/store/schema.go` (call `migrateRunUsage` right after `migrateMessageSeq`)
- Modify: `internal/store/sqlite_sessions.go` (append `boolInt`)
- Modify: `internal/core/types_run.go:41` (add `RunStep` before `BusyInputMode`)
- Modify: `internal/core/types_usage.go:1-41` (rewrite)
- Modify: `internal/core/ports.go:66-69` (`UsageStore`)
- Modify: `internal/core/usage.go:1-98` (rewrite)
- Modify: `internal/core/execution_status.go:43,72`, `internal/core/execution_generation.go:62` (delete `saveRunUsage` calls)
- Modify: `internal/core/execution_turn.go:246`, `internal/core/context.go:95-141,143-161,200-210`, `internal/core/context_compact.go:12-38`
- Modify: `internal/core/message_progress_test.go:33` (the fake store embeds `Store` and now needs `SaveRunStep`)
- Modify: `internal/core/execution_generation_test.go:72-78` (assertions), append one test
- Modify: `internal/controlplane/usage.go:33-59`
- Modify: `README.md:251,432`

- [ ] **Step 1: Write the failing store tests**

Create `internal/store/sqlite_run_steps_test.go`:

```go
package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

func createTestRun(t *testing.T, st *store.SQLiteStore, sessionID string, runID string) {
	t.Helper()
	run := core.Run{ID: runID, SessionID: sessionID, UserMessageID: "msg_" + runID, Status: core.RunStatusCompleted, StartedAt: testEpoch, UpdatedAt: testEpoch}
	if err := st.CreateRun(context.Background(), run); err != nil {
		t.Fatal(err)
	}
}

func TestRunStepsAreNumberedAndAggregatedPerRun(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r1")
	createTestRun(t, st, "s1", "r2")
	for _, step := range []core.RunStep{
		{RunID: "r1", Model: "m-a", Provider: "p", PromptTokens: 100, CacheReadTokens: 80, CacheWriteTokens: 10, OutputTokens: 5, StopReason: "tool_use", ToolCalls: 2, LatencyMillis: 900, CreatedAt: testEpoch},
		{RunID: "r1", Model: "m-b", Provider: "p", PromptTokens: 120, CacheReadTokens: 100, OutputTokens: 7, ReasoningTokens: 3, StopReason: "end_turn", CreatedAt: testEpoch.Add(time.Second)},
		{RunID: "r2", Model: "m-c", Provider: "q", PromptTokens: 50, OutputTokens: 1, StopReason: "end_turn", CreatedAt: testEpoch.Add(2 * time.Second)},
	} {
		if err := st.SaveRunStep(ctx, step); err != nil {
			t.Fatal(err)
		}
	}

	steps, err := st.ListRunSteps(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Step != 1 || steps[0].ToolCalls != 2 || steps[0].LatencyMillis != 900 || steps[0].CacheWriteTokens != 10 ||
		steps[1].Step != 2 || steps[1].StopReason != "end_turn" || !steps[1].CreatedAt.Equal(testEpoch.Add(time.Second)) {
		t.Fatalf("steps=%+v", steps)
	}

	for _, tc := range []struct {
		name   string
		filter core.UsageFilter
		want   []core.UsageRecord
	}{
		{"session", core.UsageFilter{SessionID: "s1"}, []core.UsageRecord{
			{SessionID: "s1", RunID: "r1", Provider: "p", Model: "m-b", Steps: 2, PromptTokens: 220, CacheReadTokens: 180, CacheWriteTokens: 10, OutputTokens: 12, ReasoningTokens: 3, UpdatedAt: testEpoch.Add(time.Second)},
			{SessionID: "s1", RunID: "r2", Provider: "q", Model: "m-c", Steps: 1, PromptTokens: 50, OutputTokens: 1, UpdatedAt: testEpoch.Add(2 * time.Second)},
		}},
		{"run", core.UsageFilter{RunID: "r2"}, []core.UsageRecord{
			{SessionID: "s1", RunID: "r2", Provider: "q", Model: "m-c", Steps: 1, PromptTokens: 50, OutputTokens: 1, UpdatedAt: testEpoch.Add(2 * time.Second)},
		}},
		{"latest run only", core.UsageFilter{SessionID: "s1", Limit: 1}, []core.UsageRecord{
			{SessionID: "s1", RunID: "r2", Provider: "q", Model: "m-c", Steps: 1, PromptTokens: 50, OutputTokens: 1, UpdatedAt: testEpoch.Add(2 * time.Second)},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records, err := st.ListUsageRecords(ctx, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != len(tc.want) {
				t.Fatalf("records=%+v, want %+v", records, tc.want)
			}
			for i := range records {
				if !records[i].UpdatedAt.Equal(tc.want[i].UpdatedAt) {
					t.Fatalf("record %d updated_at=%s, want %s", i, records[i].UpdatedAt, tc.want[i].UpdatedAt)
				}
				records[i].UpdatedAt = tc.want[i].UpdatedAt
				if records[i] != tc.want[i] {
					t.Fatalf("record %d=%+v, want %+v", i, records[i], tc.want[i])
				}
			}
		})
	}
}

func TestLegacyRunUsageBecomesOneRunStep(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	createTestRun(t, st, "s1", "r-anthropic")
	createTestRun(t, st, "s1", "r-openai")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE run_usage (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT NOT NULL UNIQUE, message_id TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, total_tokens INTEGER NOT NULL DEFAULT 0, cached_tokens INTEGER NOT NULL DEFAULT 0, reasoning_tokens INTEGER NOT NULL DEFAULT 0, estimated INTEGER NOT NULL DEFAULT 0, provider_raw TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
		`CREATE INDEX idx_run_usage_session_created_at ON run_usage(session_id, created_at)`,
		`INSERT INTO run_usage(id, session_id, run_id, provider, model, input_tokens, output_tokens, total_tokens, cached_tokens, created_at) VALUES('u1', 's1', 'r-anthropic', 'anthropic-compatible', 'claude', 100, 7, 107, 40, '2026-09-23T10:00:00Z')`,
		`INSERT INTO run_usage(id, session_id, run_id, provider, model, input_tokens, output_tokens, total_tokens, cached_tokens, reasoning_tokens, created_at) VALUES('u2', 's1', 'r-openai', 'openai-compatible', 'gpt', 50, 5, 55, 10, 2, '2026-09-23T10:00:01Z')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	want := []core.UsageRecord{
		{SessionID: "s1", RunID: "r-anthropic", Provider: "anthropic-compatible", Model: "claude", Steps: 1, PromptTokens: 140, OutputTokens: 7},
		{SessionID: "s1", RunID: "r-openai", Provider: "openai-compatible", Model: "gpt", Steps: 1, PromptTokens: 50, CacheReadTokens: 10, OutputTokens: 5, ReasoningTokens: 2},
	}
	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		records, err := st.ListUsageRecords(ctx, core.UsageFilter{SessionID: "s1"})
		_ = st.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != len(want) {
			t.Fatalf("open %d: records=%+v", reopen, records)
		}
		for i := range records {
			records[i].UpdatedAt = time.Time{}
			if records[i] != want[i] {
				t.Fatalf("open %d: record %d=%+v, want %+v", reopen, i, records[i], want[i])
			}
		}
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var tables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'run_usage'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("run_usage table still exists after migration")
	}
}
```

- [ ] **Step 2: Write the failing core and controlplane tests**

In `internal/core/execution_generation_test.go`, `TestToolTurnPersistsFinalCommentaryAndUsage`, replace the usage assertion (lines 76-78):

```go
			if len(usage.Records) != 1 || usage.Summary.InputTokens != 30 || usage.Summary.OutputTokens != 5 || usage.Summary.TotalTokens != 35 {
				t.Fatalf("usage did not include tool turn: %#v", usage)
			}
```

with:

```go
			if len(usage.Records) != 1 || usage.Summary.Runs != 1 || usage.Summary.Steps != 2 || usage.Summary.PromptTokens != 30 || usage.Summary.OutputTokens != 5 {
				t.Fatalf("usage did not include tool turn: %#v", usage)
			}
			steps, err := db.ListRunSteps(context.Background(), run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(steps) != 2 ||
				steps[0].StopReason != "tool_use" || steps[0].ToolCalls != 1 || steps[0].PromptTokens != 10 || steps[0].Model != "test-model" ||
				steps[1].StopReason != "end_turn" || steps[1].ToolCalls != 0 || steps[1].OutputTokens != 3 || steps[1].Provider != "recovery-test" {
				t.Fatalf("steps=%+v", steps)
			}
```

Append to the same file. The history message is 720k runes, about 180k estimated tokens. That is above the 128k auto-compaction threshold for the 256k fallback window (`context_report.go:196-206`) and below the window itself.

```go
func TestRunStepsCountCompactionGeneration(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			return providers.Response{Text: "Earlier history summarized.", Model: "test-model", Provider: "recovery-test", Usage: providers.Usage{InputTokens: 100, OutputTokens: 7}}, nil
		}
		return providers.Response{Text: "Done.", Model: "test-model", Provider: "recovery-test", Usage: providers.Usage{InputTokens: 20, OutputTokens: 3}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "compaction-step", core.RunStatusAccepted, false)
	history := transcript.Message{
		ID: "msg_large_history", SessionID: session.ID, Role: transcript.MessageRoleAssistant,
		Content:   strings.Repeat("old context ", 60_000),
		CreatedAt: run.StartedAt, UpdatedAt: run.StartedAt,
	}
	if err := db.SaveMessage(context.Background(), history); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	steps, err := db.ListRunSteps(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].StopReason != "compact" || steps[0].PromptTokens != 100 || steps[1].StopReason != "end_turn" {
		t.Fatalf("steps=%+v, want compaction then final turn", steps)
	}
}
```

Create `internal/controlplane/usage_test.go`. It exercises the command boundary (`Dispatcher.Handle`) with a stub runtime:

```go
package controlplane

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

type tokenReportRuntime struct {
	usage   core.UsageReport
	context core.ContextReport
}

func (r tokenReportRuntime) CurrentBinding(context.Context, string) (core.ClientBinding, error) {
	return core.ClientBinding{SessionID: "s1"}, nil
}

func (r tokenReportRuntime) ListSessions(context.Context) ([]core.Session, error) {
	return []core.Session{{ID: "s1", Title: "s1"}}, nil
}

func (r tokenReportRuntime) CreateSession(context.Context, string, string, string) (core.Session, error) {
	return core.Session{}, core.ErrInvalidInput
}

func (r tokenReportRuntime) UseSession(context.Context, string, string) (core.ClientBinding, error) {
	return core.ClientBinding{}, core.ErrInvalidInput
}

func (r tokenReportRuntime) RenameSession(context.Context, string, string) (core.Session, error) {
	return core.Session{}, core.ErrInvalidInput
}

func (r tokenReportRuntime) DeleteSession(context.Context, string) error {
	return core.ErrInvalidInput
}

func (r tokenReportRuntime) SessionUsage(context.Context, string) (core.UsageReport, error) {
	return r.usage, nil
}

func (r tokenReportRuntime) SessionContext(context.Context, string) (core.ContextReport, error) {
	return r.context, nil
}

func (r tokenReportRuntime) CompactSession(context.Context, string) (core.CompactSessionResult, error) {
	return core.CompactSessionResult{}, core.ErrInvalidInput
}

func TestUsageCommandShowsStepsAndCacheTokens(t *testing.T) {
	runtime := tokenReportRuntime{usage: core.UsageReport{Summary: core.UsageSummary{
		Runs: 2, Steps: 5, PromptTokens: 12_000, CacheReadTokens: 9_000, CacheWriteTokens: 1_500, OutputTokens: 800, ReasoningTokens: 200,
	}}}
	result, err := New(runtime, "").Handle(context.Background(), "key", "/usage")
	if err != nil {
		t.Fatal(err)
	}
	want := "Runs: 2\nSteps: 5\nPrompt: 12k tokens\nCache read: 9.0k tokens\nCache write: 1.5k tokens\nOutput: 800 tokens\nReasoning: 200 tokens"
	if result.Info == nil || result.Info.Text != want || len(result.Info.Rows) != 7 {
		t.Fatalf("usage result=%+v, want text:\n%s", result.Info, want)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go vet ./internal/store/ ./internal/core/ ./internal/controlplane/`
Expected: FAIL with `undefined: core.RunStep`, `db.ListRunSteps undefined`, and `unknown field Steps in struct literal of type core.UsageSummary`.

- [ ] **Step 4: Implement core types and ports**

`internal/core/types_run.go`: insert before `type BusyInputMode string`:

```go
// RunStep is one model generation of a run; SaveRunStep assigns Step.
type RunStep struct {
	RunID            string    `json:"run_id"`
	Step             int       `json:"step"`
	Model            string    `json:"model,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	PromptTokens     int64     `json:"prompt_tokens,omitempty"`
	CacheReadTokens  int64     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64     `json:"cache_write_tokens,omitempty"`
	OutputTokens     int64     `json:"output_tokens,omitempty"`
	ReasoningTokens  int64     `json:"reasoning_tokens,omitempty"`
	StopReason       string    `json:"stop_reason,omitempty"`
	LatencyMillis    int64     `json:"latency_ms,omitempty"`
	ToolCalls        int       `json:"tool_calls,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
```

Replace the whole of `internal/core/types_usage.go` with:

```go
package core

import "time"

// UsageRecord sums the run_steps of one run; Provider and Model are the last step's.
type UsageRecord struct {
	SessionID        string    `json:"session_id"`
	RunID            string    `json:"run_id"`
	Provider         string    `json:"provider,omitempty"`
	Model            string    `json:"model,omitempty"`
	Steps            int       `json:"steps"`
	PromptTokens     int64     `json:"prompt_tokens,omitempty"`
	CacheReadTokens  int64     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64     `json:"cache_write_tokens,omitempty"`
	OutputTokens     int64     `json:"output_tokens,omitempty"`
	ReasoningTokens  int64     `json:"reasoning_tokens,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type UsageFilter struct {
	SessionID string
	RunID     string
	Limit     int
}

type UsageSummary struct {
	SessionID        string `json:"session_id,omitempty"`
	Runs             int    `json:"runs"`
	Steps            int    `json:"steps"`
	PromptTokens     int64  `json:"prompt_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	OutputTokens     int64  `json:"output_tokens,omitempty"`
	ReasoningTokens  int64  `json:"reasoning_tokens,omitempty"`
}

type UsageReport struct {
	Summary UsageSummary  `json:"summary"`
	Records []UsageRecord `json:"records,omitempty"`
}
```

`internal/core/ports.go`: replace `UsageStore` with:

```go
type UsageStore interface {
	SaveRunStep(ctx context.Context, step RunStep) error
	ListRunSteps(ctx context.Context, runID string) ([]RunStep, error)
	ListUsageRecords(ctx context.Context, filter UsageFilter) ([]UsageRecord, error)
}
```

Replace the whole of `internal/core/usage.go` with the file below. This deletes `saveRunUsage` and its full-session scan.

```go
package core

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (c *Core) Usage(ctx context.Context, filter UsageFilter) (UsageReport, error) {
	filter.SessionID = normalizeText(filter.SessionID)
	filter.RunID = normalizeText(filter.RunID)
	records, err := c.store.ListUsageRecords(ctx, filter)
	if err != nil {
		return UsageReport{}, err
	}
	return UsageReport{
		Summary: summarizeUsage(records, filter.SessionID),
		Records: records,
	}, nil
}

func summarizeUsage(records []UsageRecord, sessionID string) UsageSummary {
	summary := UsageSummary{SessionID: strings.TrimSpace(sessionID), Runs: len(records)}
	for _, record := range records {
		summary.Steps += record.Steps
		summary.PromptTokens += record.PromptTokens
		summary.CacheReadTokens += record.CacheReadTokens
		summary.CacheWriteTokens += record.CacheWriteTokens
		summary.OutputTokens += record.OutputTokens
		summary.ReasoningTokens += record.ReasoningTokens
	}
	return summary
}

// recordRunStep stores one generation of a run. A failed write is logged and
// never fails the run.
func (c *Core) recordRunStep(ctx context.Context, runID string, response providers.Response, stopReason string, latency time.Duration) {
	runID = normalizeText(runID)
	if runID == "" || c.store == nil {
		return
	}
	step := RunStep{
		RunID:           runID,
		Model:           response.Model,
		Provider:        response.Provider,
		PromptTokens:    response.Usage.InputTokens,
		CacheReadTokens: response.Usage.CachedTokens,
		OutputTokens:    response.Usage.OutputTokens,
		ReasoningTokens: response.Usage.ReasoningTokens,
		StopReason:      stopReason,
		LatencyMillis:   latency.Milliseconds(),
		ToolCalls:       len(response.ToolCalls),
		CreatedAt:       c.now().UTC(),
	}
	if err := c.store.SaveRunStep(context.WithoutCancel(ctx), step); err != nil {
		log.Printf("core: record step of run %q: %v", runID, err)
	}
}

func generationStopReason(response providers.Response) string {
	if len(response.ToolCalls) > 0 {
		return "tool_use"
	}
	return "end_turn"
}
```

- [ ] **Step 5: Write steps from the loop and from compaction; delete the old calls**

- `internal/core/execution_status.go`: delete both lines `c.saveRunUsage(ctx, *run, *assistant, response.Usage)` (lines 43 and 72).
- `internal/core/execution_generation.go`: delete `c.saveRunUsage(ctx, Run{ID: turn.RunID, SessionID: turn.SessionID}, *assistant, response.Usage)` (line 62).
- `internal/core/execution_turn.go`, `generateAssistantTurn`: replace `response, err := turn.Runtime.Generate(streamCtx, request)` with:

```go
	started := time.Now()
	response, err := turn.Runtime.Generate(streamCtx, request)
	if err == nil {
		c.recordRunStep(ctx, turn.RunID, response, generationStopReason(response), time.Since(started))
	}
```

- `internal/core/context.go`: change the start of `CompactSession` to delegate to a run-aware variant:

```go
func (c *Core) CompactSession(ctx context.Context, sessionID string) (CompactSessionResult, error) {
	return c.compactSession(ctx, sessionID, "")
}

// compactSession records the summary generation as a step of runID when set.
func (c *Core) compactSession(ctx context.Context, sessionID string, runID string) (CompactSessionResult, error) {
	sessionID = normalizeText(sessionID)
```

The rest of the old body stays under `compactSession`, with one change: `c.generateCompactSummary(ctx, session, effectiveMessages)` becomes `c.generateCompactSummary(ctx, session, effectiveMessages, runID)`. In `autoCompactSessionIfNeeded` and `forceCompactSessionForRetry`, pass the run: `return c.compactSessionWithLoadedMessages(ctx, session, messages, turn.RunID)`. Replace `compactSessionWithLoadedMessages` with:

```go
func (c *Core) compactSessionWithLoadedMessages(ctx context.Context, session Session, messages []transcript.Message, runID string) (bool, error) {
	_, effectiveMessages := latestCompactSummary(messages)
	if len(effectiveMessages) == 0 {
		return false, nil
	}
	_, err := c.compactSession(ctx, session.ID, runID)
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}
```

- `internal/core/context_compact.go`, `generateCompactSummary`: change the signature to `func (c *Core) generateCompactSummary(ctx context.Context, session Session, messages []transcript.Message, runID string) (string, error)`. Put `started := time.Now()` directly before `response, err := runtime.Generate(ctx, providers.Request{`. After the `if err != nil { return "", err }` that follows the call, add:

```go
	c.recordRunStep(ctx, runID, response, "compact", time.Since(started))
```

- `internal/core/message_progress_test.go`: the fake store embeds a nil `Store`, so add this method to it, next to `GetRun`:

```go
func (s *progressCountingStore) SaveRunStep(context.Context, RunStep) error {
	return nil
}
```

Run: `$(go env GOPATH)/bin/goimports -w internal/core/context_compact.go internal/core/execution_turn.go internal/core/execution_status.go internal/core/execution_generation.go`

- [ ] **Step 6: Implement the store side**

`internal/store/migrations/001_init.sql`: replace the whole `CREATE TABLE IF NOT EXISTS run_usage (…);` statement with:

```sql
CREATE TABLE IF NOT EXISTS run_steps (
    run_id TEXT NOT NULL,
    step INTEGER NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    stop_reason TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    tool_calls INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (run_id, step),
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);
```

Delete these two lines and the blank line after them:

```sql
CREATE INDEX IF NOT EXISTS idx_run_usage_session_created_at
    ON run_usage(session_id, created_at);
```

`internal/store/schema.go`: directly after the `migrateMessageSeq` call added in Task 2, add:

```go
	if err := migrateRunUsage(db); err != nil {
		return err
	}
```

Create `internal/store/schema_run_steps.go`:

```go
package store

import (
	"database/sql"
	"fmt"
)

// migrateRunUsage turns each legacy run_usage aggregate into one run step and
// drops the table. Legacy Anthropic input excluded its cached tokens.
func migrateRunUsage(db *sql.DB) error {
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'run_usage'`).Scan(&tables); err != nil {
		return fmt.Errorf("store: inspect run_usage: %w", err)
	}
	if tables == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin run_usage migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT OR IGNORE INTO run_steps(run_id, step, model, provider, prompt_tokens, cache_read_tokens,
    output_tokens, reasoning_tokens, stop_reason, created_at)
SELECT u.run_id, 1, u.model, u.provider,
       CASE WHEN u.provider = 'anthropic-compatible' THEN u.input_tokens + u.cached_tokens ELSE u.input_tokens END,
       CASE WHEN u.provider = 'anthropic-compatible' THEN 0 ELSE u.cached_tokens END,
       u.output_tokens, u.reasoning_tokens, 'legacy', u.created_at
FROM run_usage u
JOIN runs r ON r.id = u.run_id`); err != nil {
		return fmt.Errorf("store: copy run_usage into run_steps: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE run_usage`); err != nil {
		return fmt.Errorf("store: drop run_usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit run_usage migration: %w", err)
	}
	return nil
}
```

`'anthropic-compatible'` is `providers.TypeAnthropic` (`internal/providers/catalog.go:8`). The legacy Anthropic `CachedTokens` was creation plus read, with no split, so the cache-read column is left at 0.

Delete the old usage store and keep its one shared helper:

```bash
git rm internal/store/sqlite_usage.go
```

Append to `internal/store/sqlite_sessions.go`:

```go
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
```

Create `internal/store/sqlite_run_steps.go`:

```go
package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// SaveRunStep appends step as the next step number of its run.
func (s *SQLiteStore) SaveRunStep(ctx context.Context, step core.RunStep) error {
	runID := strings.TrimSpace(step.RunID)
	if runID == "" {
		return core.ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO run_steps(run_id, step, model, provider, prompt_tokens, cache_read_tokens, cache_write_tokens,
    output_tokens, reasoning_tokens, stop_reason, latency_ms, tool_calls, created_at)
VALUES(?, (SELECT COALESCE(MAX(step), 0) + 1 FROM run_steps WHERE run_id = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, runID, step.Model, step.Provider,
		step.PromptTokens, step.CacheReadTokens, step.CacheWriteTokens,
		step.OutputTokens, step.ReasoningTokens, step.StopReason,
		step.LatencyMillis, step.ToolCalls, formatTime(step.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save run step: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListRunSteps(ctx context.Context, runID string) ([]core.RunStep, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, step, model, provider, prompt_tokens, cache_read_tokens, cache_write_tokens,
       output_tokens, reasoning_tokens, stop_reason, latency_ms, tool_calls, created_at
FROM run_steps
WHERE run_id = ?
ORDER BY step`, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("store: list run steps: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var steps []core.RunStep
	for rows.Next() {
		var step core.RunStep
		var createdAt string
		if err := rows.Scan(&step.RunID, &step.Step, &step.Model, &step.Provider,
			&step.PromptTokens, &step.CacheReadTokens, &step.CacheWriteTokens,
			&step.OutputTokens, &step.ReasoningTokens, &step.StopReason,
			&step.LatencyMillis, &step.ToolCalls, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan run step: %w", err)
		}
		step.CreatedAt = mustParseTime(createdAt)
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate run steps: %w", err)
	}
	return steps, nil
}

// ListUsageRecords sums run_steps per run, oldest run first; Limit keeps the latest runs.
func (s *SQLiteStore) ListUsageRecords(ctx context.Context, filter core.UsageFilter) ([]core.UsageRecord, error) {
	// MAX(s.step) is the only min/max aggregate, so SQLite takes the bare
	// provider, model and created_at columns from each run's last step.
	query := `
SELECT r.session_id, s.run_id, MAX(s.step), s.provider, s.model,
       SUM(s.prompt_tokens), SUM(s.cache_read_tokens), SUM(s.cache_write_tokens),
       SUM(s.output_tokens), SUM(s.reasoning_tokens), s.created_at
FROM run_steps s
JOIN runs r ON r.id = s.run_id`
	args := make([]any, 0, 3)
	clauses := make([]string, 0, 2)
	if sessionID := strings.TrimSpace(filter.SessionID); sessionID != "" {
		clauses = append(clauses, "r.session_id = ?")
		args = append(args, sessionID)
	}
	if runID := strings.TrimSpace(filter.RunID); runID != "" {
		clauses = append(clauses, "s.run_id = ?")
		args = append(args, runID)
	}
	if len(clauses) > 0 {
		query += "\nWHERE " + strings.Join(clauses, " AND ")
	}
	query += "\nGROUP BY s.run_id\nORDER BY r.rowid DESC"
	if filter.Limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list usage records: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []core.UsageRecord
	for rows.Next() {
		var record core.UsageRecord
		var updatedAt string
		if err := rows.Scan(&record.SessionID, &record.RunID, &record.Steps, &record.Provider, &record.Model,
			&record.PromptTokens, &record.CacheReadTokens, &record.CacheWriteTokens,
			&record.OutputTokens, &record.ReasoningTokens, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: scan usage record: %w", err)
		}
		record.UpdatedAt = mustParseTime(updatedAt)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate usage records: %w", err)
	}
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
	return records, nil
}
```

- [ ] **Step 7: Update `/usage` and the README**

In `internal/controlplane/usage.go`, replace `usageInfoData` and `usageInfoText` (lines 33-59) with the functions below. `handleUsage` and `usageTokenLabel` stay.

```go
func usageInfoData(report core.UsageReport) InfoData {
	return InfoData{
		Title: "Token Usage",
		Text:  usageInfoText(report),
		Rows:  usageInfoRows(report.Summary),
	}
}

func usageInfoText(report core.UsageReport) string {
	rows := usageInfoRows(report.Summary)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, row.Label+": "+row.Value)
	}
	return strings.Join(lines, "\n")
}

func usageInfoRows(summary core.UsageSummary) []InfoRow {
	return []InfoRow{
		{Label: "Runs", Value: fmt.Sprintf("%d", summary.Runs)},
		{Label: "Steps", Value: fmt.Sprintf("%d", summary.Steps)},
		{Label: "Prompt", Value: usageTokenLabel(summary.PromptTokens)},
		{Label: "Cache read", Value: usageTokenLabel(summary.CacheReadTokens)},
		{Label: "Cache write", Value: usageTokenLabel(summary.CacheWriteTokens)},
		{Label: "Output", Value: usageTokenLabel(summary.OutputTokens)},
		{Label: "Reasoning", Value: usageTokenLabel(summary.ReasoningTokens)},
	}
}
```

`README.md`: change line 251 to
`- Token usage recorded per model generation (prompt, cache read/write, output, reasoning), surfaced in \`/usage\` and \`/context\`.`
and line 432 to
`/usage                       show runs, steps, prompt/cache/output/reasoning tokens`

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/store/ ./internal/core/ ./internal/controlplane/ && go test ./... 2>&1 | grep -E "^(FAIL|---)"`
Expected: three `ok` lines; the final grep prints nothing. Also check `grep -rn "saveRunUsage\|SaveUsageRecord\|run_usage" internal --include=*.go`. It should print only the migration in `internal/store/schema_run_steps.go` and the legacy DDL in `sqlite_run_steps_test.go`.

- [ ] **Step 9: Commit**

```bash
gofmt -l internal clients cmd
git add internal/store/migrations/001_init.sql internal/store/schema.go internal/store/schema_run_steps.go internal/store/sqlite_run_steps.go internal/store/sqlite_run_steps_test.go internal/store/sqlite_sessions.go \
  internal/core/types_run.go internal/core/types_usage.go internal/core/ports.go internal/core/usage.go internal/core/execution_status.go internal/core/execution_generation.go internal/core/execution_turn.go internal/core/context.go internal/core/context_compact.go internal/core/message_progress_test.go internal/core/execution_generation_test.go \
  internal/controlplane/usage.go internal/controlplane/usage_test.go README.md
git commit -m "feat: record every model generation in run_steps

The loop writes one run_steps row per Generate (tool turns, final turn,
retries) and per compaction summary inside a run. Per-run usage is a
GROUP BY over run_steps; the full-scan saveRunUsage rebuild and the
run_usage table are removed (legacy rows become one 'legacy' step).

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

Do not list `internal/store/sqlite_usage.go` in `git add`: Step 6's `git rm` has already staged the deletion, and `git add` of a path missing from both index and worktree fails with `pathspec … did not match`, aborting the whole add.

---

### Task 6: `GET /v1/runs/{id}/steps`

This makes `ListRunSteps` reachable by clients: per-step cache read/write for status views, and later budget displays.

**Files:**
- Create: `internal/api/runs_test.go`
- Modify: `internal/core/contracts.go:121-123` (add `RunStepsResponse` after `RunResponse`)
- Modify: `internal/core/usage.go` (add `RunSteps`)
- Modify: `internal/api/runs.go:10-19`

- [ ] **Step 1: Write the failing test**

Create `internal/api/runs_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestRunStepsEndpointListsStepsInOrder(t *testing.T) {
	server, st := newAPITestServer(t)
	ctx := context.Background()
	run := core.Run{ID: "r1", SessionID: "s1", UserMessageID: "u1", Status: core.RunStatusCompleted, StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch}
	if err := st.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"tool_use", "end_turn"} {
		if err := st.SaveRunStep(ctx, core.RunStep{RunID: "r1", StopReason: reason, PromptTokens: 10, CreatedAt: apiTestEpoch}); err != nil {
			t.Fatal(err)
		}
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/runs/r1/steps", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response core.RunStepsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Steps) != 2 || response.Steps[0].Step != 1 || response.Steps[0].StopReason != "tool_use" || response.Steps[1].Step != 2 {
		t.Fatalf("steps=%+v", response.Steps)
	}

	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/runs/r1", nil))
	var runResponse core.RunResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &runResponse); err != nil || runResponse.Run.ID != "r1" {
		t.Fatalf("run lookup broke: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/`
Expected: FAIL (build failed) with `undefined: core.RunStepsResponse`.

- [ ] **Step 3: Implement**

`internal/core/contracts.go`, after `RunResponse`:

```go
type RunStepsResponse struct {
	Steps []RunStep `json:"steps"`
}
```

`internal/core/usage.go`, before `recordRunStep`:

```go
func (c *Core) RunSteps(ctx context.Context, runID string) ([]RunStep, error) {
	runID = normalizeText(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	return c.store.ListRunSteps(ctx, runID)
}
```

Then run `$(go env GOPATH)/bin/goimports -w internal/core/usage.go` to add `fmt`.

`internal/api/runs.go`: make the first `case` of the `switch` in `handleRunByID` the steps route. It must come before the plain GET:

```go
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/steps"):
		steps, err := s.core.RunSteps(r.Context(), strings.TrimSuffix(path, "/steps"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.RunStepsResponse{Steps: steps})
	case r.Method == http.MethodGet:
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/api/ ./internal/core/`
Expected: both `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal clients cmd
git add internal/core/contracts.go internal/core/usage.go internal/api/runs.go internal/api/runs_test.go
git commit -m "feat(api): expose run steps

GET /v1/runs/{id}/steps returns the run's generations with token,
cache, latency and stop-reason data.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Normalised `providers.Usage`

**New shape:** `PromptTokens` is the whole input, cache reads and writes included. `OutputTokens` includes reasoning tokens (they are billed as output). `CacheReadTokens`, `CacheWriteTokens` and `ReasoningTokens` are breakdowns of those totals. `InputTokens`, `TotalTokens` and `CachedTokens` are removed.

**Wire mapping, one row per adapter:**

| Adapter | PromptTokens | CacheReadTokens | CacheWriteTokens | OutputTokens | ReasoningTokens |
|---|---|---|---|---|---|
| anthropiccompat (JSON `usage`; stream: `message_start.message.usage`, then cumulative `message_delta.usage`) | `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` | `cache_read_input_tokens` | `cache_creation_input_tokens` | `output_tokens` | 0 (not reported) |
| openaicompat (chat completions JSON and final stream chunk) | `prompt_tokens` | `prompt_tokens_details.cached_tokens`, else DeepSeek `prompt_cache_hit_tokens` | `prompt_tokens_details.cache_write_tokens` (OpenRouter) | `completion_tokens` | `completion_tokens_details.reasoning_tokens` |
| openaicodex (Responses `usage`) | `input_tokens` | `input_tokens_details.cached_tokens` | 0 | `output_tokens` | `output_tokens_details.reasoning_tokens` |
| gemini (`usageMetadata`) | `promptTokenCount` (includes cached) | `cachedContentTokenCount` | 0 | `candidatesTokenCount + thoughtsTokenCount` | `thoughtsTokenCount` |

Anthropic streaming previously returned no usage at all. Now it does.

**Consumers:**
- `core.ProviderUsage` (`internal/core/context.go:57-65`) is deleted. `ContextReport.LastProviderUsage` becomes `*providers.Usage`, so JSON keys change to `prompt_tokens`/`cache_read_tokens`/…
- The finish-part usage payload uses the same type. Messages stored before this change carry the old keys and are no longer read, so `/context` shows no "Last provider usage" until the next generation.
- `providerUsageIsZero` and `providerUsageEmpty` are replaced by `Usage.IsZero`.
- `recordRunStep` maps the new fields.
- `/context` label is updated (`internal/controlplane/context.go:179-188`).
- `internal/clientruntime/state.go:163-169` and `clients/terminal/chat/viewmodel/surface_state.go:108-114` only copy the struct and keep compiling.
- iOS model: Task 9.

**Files:**
- Create: `internal/providers/ai/anthropiccompat/usage_test.go`, `internal/providers/ai/openaicompat/usage_test.go`, `internal/providers/ai/gemini/usage_test.go`, `internal/providers/ai/openaicodex/usage_test.go`
- Modify: `internal/providers/contract.go:68-75`
- Modify: `internal/providers/ai/anthropiccompat/adapter.go:274-309,368-417`
- Modify: `internal/providers/ai/openaicompat/types.go:87-97`, `internal/providers/ai/openaicompat/chat.go:366-379`
- Modify: `internal/providers/ai/openaicodex/runtime.go:355-368`, `internal/providers/ai/openaicodex/response_test.go:37`
- Modify: `internal/providers/ai/gemini/adapter.go:446-460`
- Modify: `internal/core/context.go:53,57-65`, `internal/core/context_report.go:244-270`, `internal/core/execution_status.go:185-219`, `internal/core/usage.go` (`recordRunStep`)
- Modify: `internal/controlplane/context.go:179-188`, `internal/controlplane/usage_test.go` (append)
- Modify: `internal/core/execution_generation_test.go` (field names and one assertion)

- [ ] **Step 1: Write the failing adapter tests**

`internal/providers/ai/anthropiccompat/usage_test.go`:

```go
package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const anthropicStreamWithUsage = "event: message_start\n" +
	`data: {"type":"message_start","message":{"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":300,"output_tokens":1}}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"

func TestUsageCountsCachedInputAsPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(anthropicStreamWithUsage))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Hi"}],"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":300,"output_tokens":50}}`))
	}))
	defer server.Close()
	runtime, err := New(context.Background(), Config{APIKey: "test", BaseURL: server.URL, Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	want := providers.Usage{PromptTokens: 420, CacheReadTokens: 300, CacheWriteTokens: 20, OutputTokens: 50}
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"json", context.Background()},
		{"stream", providers.WithTextStream(context.Background(), func(string) error { return nil })},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := runtime.Generate(tc.ctx, providers.Request{Messages: []providers.Message{{Role: "user", Content: "hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			got := response.Usage
			if len(got.ProviderRaw) == 0 {
				t.Fatal("provider raw usage missing")
			}
			got.ProviderRaw = nil
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("usage=%+v, want %+v", got, want)
			}
		})
	}
}
```

`internal/providers/ai/openaicompat/usage_test.go`:

```go
package openaicompat

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageIsNormalised(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage string
		want  providers.Usage
	}{
		{"openai", `{"prompt_tokens":1000,"completion_tokens":100,"total_tokens":1100,"prompt_tokens_details":{"cached_tokens":800},"completion_tokens_details":{"reasoning_tokens":40}}`,
			providers.Usage{PromptTokens: 1000, CacheReadTokens: 800, OutputTokens: 100, ReasoningTokens: 40}},
		{"openrouter cache write", `{"prompt_tokens":1000,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":900}}`,
			providers.Usage{PromptTokens: 1000, CacheWriteTokens: 900, OutputTokens: 10}},
		{"deepseek cache hit", `{"prompt_tokens":1000,"completion_tokens":10,"prompt_cache_hit_tokens":600,"prompt_cache_miss_tokens":400}`,
			providers.Usage{PromptTokens: 1000, CacheReadTokens: 600, OutputTokens: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jsonBody := `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":` + tc.usage + `}`
			fromJSON, err := (&Runtime{}).decodeChatResponse([]byte(jsonBody))
			if err != nil {
				t.Fatal(err)
			}
			frames := `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\n" +
				`data: {"choices":[],"usage":` + tc.usage + `}` + "\n\ndata: [DONE]\n\n"
			fromStream, err := (&Runtime{}).decodeStream(context.Background(), strings.NewReader(frames))
			if err != nil {
				t.Fatal(err)
			}
			for source, got := range map[string]providers.Usage{"json": fromJSON.Usage, "stream": fromStream.Usage} {
				if len(got.ProviderRaw) == 0 {
					t.Fatalf("%s: provider raw usage missing", source)
				}
				got.ProviderRaw = nil
				if !reflect.DeepEqual(got, tc.want) {
					t.Errorf("%s usage=%+v, want %+v", source, got, tc.want)
				}
			}
		})
	}
}
```

`internal/providers/ai/gemini/usage_test.go`:

```go
package gemini

import (
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageCountsThoughtsAsOutput(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":600,"candidatesTokenCount":100,"thoughtsTokenCount":40,"totalTokenCount":1140}}`)
	response, err := (&Runtime{}).decodeGenerateResponse(providers.Request{}, body)
	if err != nil {
		t.Fatal(err)
	}
	got := response.Usage
	if len(got.ProviderRaw) == 0 {
		t.Fatal("provider raw usage missing")
	}
	got.ProviderRaw = nil
	want := providers.Usage{PromptTokens: 1000, CacheReadTokens: 600, OutputTokens: 140, ReasoningTokens: 40}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage=%+v, want %+v", got, want)
	}
}
```

`internal/providers/ai/openaicodex/usage_test.go`:

```go
package openaicodex

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestUsageKeepsCachedInputInPrompt(t *testing.T) {
	stream := `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":700},"output_tokens":200,"output_tokens_details":{"reasoning_tokens":150},"total_tokens":1200}}}

`
	response, err := (&Runtime{model: "test-model"}).decodeStream(context.Background(), strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	got := response.Usage
	if len(got.ProviderRaw) == 0 {
		t.Fatal("provider raw usage missing")
	}
	got.ProviderRaw = nil
	want := providers.Usage{PromptTokens: 1000, CacheReadTokens: 700, OutputTokens: 200, ReasoningTokens: 150}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage=%+v, want %+v", got, want)
	}
}
```

In `internal/providers/ai/openaicodex/response_test.go:37`, replace `response.Usage.TotalTokens != 15` with `response.Usage.PromptTokens != 10 || response.Usage.OutputTokens != 5`.

- [ ] **Step 2: Write the failing core and controlplane tests**

In `internal/core/execution_generation_test.go`, rename every `InputTokens:` to `PromptTokens:`:

```bash
sed -i 's/InputTokens:/PromptTokens:/g' internal/core/execution_generation_test.go
```

In `TestToolTurnPersistsFinalCommentaryAndUsage`, directly before `steps, err := db.ListRunSteps(context.Background(), run.ID)`, add:

```go
			report, err := app.SessionContext(context.Background(), session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if report.LastProviderUsage == nil || report.LastProviderUsage.PromptTokens != 20 || report.LastProviderUsage.OutputTokens != 3 {
				t.Fatalf("last provider usage=%+v", report.LastProviderUsage)
			}
```

In `internal/controlplane/usage_test.go`, change the import block to:

```go
import (
	"context"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
)
```

and append:

```go
func TestContextInfoShowsCachedPromptTokens(t *testing.T) {
	for _, tc := range []struct {
		usage providers.Usage
		want  string
	}{
		{providers.Usage{PromptTokens: 12_000, OutputTokens: 800}, "Last provider usage: 12k in / 800 out"},
		{providers.Usage{PromptTokens: 12_000, CacheReadTokens: 9_000, CacheWriteTokens: 1_500, OutputTokens: 800}, "Last provider usage: 12k in (9.0k cached, 1.5k written) / 800 out"},
		{providers.Usage{ProviderRaw: []byte(`{}`)}, "Last provider usage: reported"},
	} {
		usage := tc.usage
		runtime := tokenReportRuntime{context: core.ContextReport{SessionID: "s1", LastProviderUsage: &usage}}
		result, err := New(runtime, "").Handle(context.Background(), "key", "/context info")
		if err != nil {
			t.Fatal(err)
		}
		if result.Info == nil || !strings.Contains(result.Info.Text, tc.want) {
			t.Errorf("context info=%+v, want line %q", result.Info, tc.want)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go vet ./internal/... 2>&1 | grep -v '^#'`
Expected: `unknown field PromptTokens in struct literal of type providers.Usage` in the four adapter tests, the core test and the controlplane test; also `response.Usage.PromptTokens undefined` in the codex test.

- [ ] **Step 4: Implement the contract and adapters**

`internal/providers/contract.go`: replace `type Usage struct {…}` with:

```go
// Usage is normalised by every adapter: PromptTokens is the whole input,
// cache reads and writes included; OutputTokens includes ReasoningTokens.
type Usage struct {
	PromptTokens     int64           `json:"prompt_tokens,omitempty"`
	OutputTokens     int64           `json:"output_tokens,omitempty"`
	CacheReadTokens  int64           `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64           `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int64           `json:"reasoning_tokens,omitempty"`
	ProviderRaw      json.RawMessage `json:"provider_raw,omitempty"`
}

func (u Usage) IsZero() bool {
	return u.PromptTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 &&
		u.CacheWriteTokens == 0 && u.ReasoningTokens == 0 && len(u.ProviderRaw) == 0
}
```

`internal/providers/ai/anthropiccompat/adapter.go`:

1. In `anthropicUsage`, replace the returned literal and add the merge helper right after the function:

```go
	raw, _ := json.Marshal(usage)
	return providers.Usage{
		PromptTokens:     usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadInputTokens,
		CacheWriteTokens: usage.CacheCreationInputTokens,
		ProviderRaw:      raw,
	}
}

// mergeAnthropicUsage applies message_delta usage, whose counts are cumulative.
func mergeAnthropicUsage(current anthropicUsagePayload, delta anthropicUsagePayload) anthropicUsagePayload {
	if delta.InputTokens > 0 {
		current.InputTokens = delta.InputTokens
	}
	if delta.CacheCreationInputTokens > 0 {
		current.CacheCreationInputTokens = delta.CacheCreationInputTokens
	}
	if delta.CacheReadInputTokens > 0 {
		current.CacheReadInputTokens = delta.CacheReadInputTokens
	}
	if delta.OutputTokens > 0 {
		current.OutputTokens = delta.OutputTokens
	}
	return current
}
```

2. In `type anthropicStreamDelta struct`, add these two fields after `ContentBlock`:

```go
	Message struct {
		Usage anthropicUsagePayload `json:"usage"`
	} `json:"message"`
	Usage anthropicUsagePayload `json:"usage"`
```

3. In `decodeStream`: declare `var usage anthropicUsagePayload` after `var text strings.Builder`. Directly after the `if event.Type == "message_stop" { … }` block, insert:

```go
		switch event.Type {
		case "message_start":
			usage = chunk.Message.Usage
		case "message_delta":
			usage = mergeAnthropicUsage(usage, chunk.Usage)
		}
```

Then add `Usage: anthropicUsage(usage),` to the final `providers.Response{…}` literal.

`internal/providers/ai/openaicompat/types.go`: replace the top of `chatCompletionUsage`, keeping `CompletionTokensDetails` as it is:

```go
type chatCompletionUsage struct {
	PromptTokens         int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens     int64 `json:"completion_tokens,omitempty"`
	TotalTokens          int64 `json:"total_tokens,omitempty"`
	PromptCacheHitTokens int64 `json:"prompt_cache_hit_tokens,omitempty"`
	PromptTokensDetails  struct {
		CachedTokens     int64 `json:"cached_tokens,omitempty"`
		CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
```

`internal/providers/ai/openaicompat/chat.go`, `openAIUsage`: replace everything after the zero check with:

```go
	raw, _ := json.Marshal(usage)
	cacheRead := usage.PromptTokensDetails.CachedTokens
	if cacheRead == 0 {
		cacheRead = usage.PromptCacheHitTokens
	}
	return providers.Usage{
		PromptTokens:     usage.PromptTokens,
		OutputTokens:     usage.CompletionTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: usage.PromptTokensDetails.CacheWriteTokens,
		ReasoningTokens:  usage.CompletionTokensDetails.ReasoningTokens,
		ProviderRaw:      raw,
	}
}
```

`internal/providers/ai/openaicodex/runtime.go`, `toProviderUsage`: the returned literal becomes:

```go
	return providers.Usage{
		PromptTokens:    usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		CacheReadTokens: usage.InputTokensDetails.CachedTokens,
		ReasoningTokens: usage.OutputTokensDetails.ReasoningTokens,
		ProviderRaw:     raw,
	}
```

`internal/providers/ai/gemini/adapter.go`, `geminiUsage`: the returned literal becomes:

```go
	return providers.Usage{
		PromptTokens:    usage.PromptTokenCount,
		OutputTokens:    usage.CandidatesTokenCount + usage.ThoughtsTokenCount,
		CacheReadTokens: usage.CachedContentTokenCount,
		ReasoningTokens: usage.ThoughtsTokenCount,
		ProviderRaw:     raw,
	}
```

- [ ] **Step 5: Update the core and controlplane consumers**

`internal/core/context.go`: change the `LastProviderUsage` field to `LastProviderUsage *providers.Usage \`json:"last_provider_usage,omitempty"\``, and delete `type ProviderUsage struct {…}` (lines 57-65) completely.

`internal/core/context_report.go`: `latestProviderUsage` now returns `*providers.Usage`, decodes into `providers.Usage`, and checks `IsZero`:

```go
func latestProviderUsage(messages []transcript.Message) *providers.Usage {
	for i := len(messages) - 1; i >= 0; i-- {
		for j := len(messages[i].Parts) - 1; j >= 0; j-- {
			part := messages[i].Parts[j]
			if part.Kind != transcript.MessagePartKindFinish || part.Finish == nil || len(part.Finish.Details) == 0 {
				continue
			}
			var payload struct {
				Usage providers.Usage `json:"usage"`
			}
			if err := json.Unmarshal(part.Finish.Details, &payload); err == nil && !payload.Usage.IsZero() {
				usage := payload.Usage
				return &usage
			}
		}
	}
	return nil
}
```

Delete `providerUsageEmpty` (the function right after it).

`internal/core/execution_status.go`: replace the head of `providerUsageFinishPart` down to the `json.Marshal` call with the code below, and delete `providerUsageIsZero` entirely.

```go
func providerUsageFinishPart(usage providers.Usage) *transcript.MessagePart {
	if usage.IsZero() {
		return nil
	}
	payload, err := json.Marshal(struct {
		Usage providers.Usage `json:"usage"`
	}{Usage: usage})
```

`internal/core/usage.go`, `recordRunStep`: the step literal becomes:

```go
	step := RunStep{
		RunID:            runID,
		Model:            response.Model,
		Provider:         response.Provider,
		PromptTokens:     response.Usage.PromptTokens,
		CacheReadTokens:  response.Usage.CacheReadTokens,
		CacheWriteTokens: response.Usage.CacheWriteTokens,
		OutputTokens:     response.Usage.OutputTokens,
		ReasoningTokens:  response.Usage.ReasoningTokens,
		StopReason:       stopReason,
		LatencyMillis:    latency.Milliseconds(),
		ToolCalls:        len(response.ToolCalls),
		CreatedAt:        c.now().UTC(),
	}
```

`internal/controlplane/context.go`: replace `providerUsageLabel` with:

```go
func providerUsageLabel(usage providers.Usage) string {
	if usage.PromptTokens == 0 && usage.OutputTokens == 0 {
		return "reported"
	}
	prompt := formatShortNumber(int(usage.PromptTokens)) + " in"
	if usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0 {
		prompt += fmt.Sprintf(" (%s cached, %s written)", formatShortNumber(int(usage.CacheReadTokens)), formatShortNumber(int(usage.CacheWriteTokens)))
	}
	return prompt + " / " + formatShortNumber(int(usage.OutputTokens)) + " out"
}
```

Run: `$(go env GOPATH)/bin/goimports -w internal/core/context.go internal/core/context_report.go internal/core/execution_status.go internal/controlplane/context.go`. This drops `encoding/json` from `context.go` and adds `providers` to `controlplane/context.go`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | grep -E "^(FAIL|---)"`
Expected: build and vet succeed; the grep prints nothing. Then:
`grep -rnE "\.(InputTokens|TotalTokens|CachedTokens)\b|ProviderUsage\b|providerUsage(IsZero|Empty)" internal clients --include=*.go`
It may print only adapter-internal wire structs: `anthropicUsagePayload.InputTokens`, `responsesUsage.InputTokens`/`TotalTokens`, `chatCompletionUsage.TotalTokens`, `PromptTokensDetails.CachedTokens`, `InputTokensDetails.CachedTokens`, and `LastProviderUsage` field accesses.

- [ ] **Step 7: Commit**

```bash
gofmt -l internal clients cmd
git add internal/providers/contract.go \
  internal/providers/ai/anthropiccompat/adapter.go internal/providers/ai/anthropiccompat/usage_test.go \
  internal/providers/ai/openaicompat/types.go internal/providers/ai/openaicompat/chat.go internal/providers/ai/openaicompat/usage_test.go \
  internal/providers/ai/openaicodex/runtime.go internal/providers/ai/openaicodex/response_test.go internal/providers/ai/openaicodex/usage_test.go \
  internal/providers/ai/gemini/adapter.go internal/providers/ai/gemini/usage_test.go \
  internal/core/context.go internal/core/context_report.go internal/core/execution_status.go internal/core/usage.go internal/core/execution_generation_test.go \
  internal/controlplane/context.go internal/controlplane/usage_test.go
git commit -m "feat(providers): normalise usage across adapters

Usage is PromptTokens (whole input incl. cache), OutputTokens (incl.
reasoning), CacheRead/CacheWrite/Reasoning breakdowns. Anthropic stream
usage is now captured. core.ProviderUsage is removed; /context shows
cache reads and writes.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Idle stream timeouts

Today the adapters use whole-request `http.Client{Timeout: …}`: 90 s for anthropic and gemini, 120 s for codex, 5 min for openaicompat. That cuts off long but healthy streams. The replacement is one shared client with:
- `ResponseHeaderTimeout = 10 min`. Non-streaming calls get their headers only after the whole generation: Gemini `generateContent` always, compaction and non-TUI calls on openaicompat/anthropic.
- An idle-between-reads timeout of 120 s on the body.

An idle timeout returns `providers.ErrStreamIdle`, which is retryable. The core retry still only fires when no output was published (`execution_generation.go:13-31`). The codex auth clients (`openaicodex/auth.go:76,106,159`) keep their 20 s whole-request timeout, because they are short OAuth calls, not model streams.

**Files:**
- Create: `internal/providers/httpclient.go`, `internal/providers/httpclient_test.go`
- Modify: `internal/providers/generation_errors.go:21`
- Modify: `internal/providers/ai/anthropiccompat/adapter.go:17-21,351-354`
- Modify: `internal/providers/ai/gemini/adapter.go:18-21,384-387`
- Modify: `internal/providers/ai/openaicompat/config.go:13,108-111`, `internal/providers/ai/openaicompat/models.go:16-19`
- Modify: `internal/providers/ai/openaicodex/runtime.go:17,41-44`, `internal/providers/ai/openaicodex/models.go:14-17`
- Delete: `internal/providers/ai/openaicompat/config_test.go` (it asserts the removed 5-minute whole-request timeout)

- [ ] **Step 1: Write the failing test**

Create `internal/providers/httpclient_test.go`:

```go
package providers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientTimesOutOnlyOnSilence(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wait := func() {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}
		if r.URL.Path == "/no-headers" {
			wait()
			return
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := 0; i < 5; i++ {
			_, _ = fmt.Fprintf(w, "data: %d\n\n", i)
			flusher.Flush()
			if r.URL.Path == "/stall" {
				wait()
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()
	defer close(release)
	client := newHTTPClient(200*time.Millisecond, 200*time.Millisecond)

	t.Run("slow but live stream completes", func(t *testing.T) {
		res, err := client.Get(server.URL + "/live")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil || len(body) != 5*len("data: 0\n\n") {
			t.Fatalf("body=%q err=%v", body, err)
		}
	})

	t.Run("stalled stream fails with retryable idle error", func(t *testing.T) {
		res, err := client.Get(server.URL + "/stall")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		_, err = io.ReadAll(res.Body)
		if !errors.Is(err, ErrStreamIdle) || !IsRetryableGenerationError(err) {
			t.Fatalf("err=%v, want retryable ErrStreamIdle", err)
		}
	})

	t.Run("missing headers fail with retryable timeout", func(t *testing.T) {
		_, err := client.Get(server.URL + "/no-headers")
		if err == nil || !IsRetryableGenerationError(err) {
			t.Fatalf("err=%v, want retryable header timeout", err)
		}
	})
}
```

The live stream lasts 250 ms in total with 50 ms gaps. The idle limit is 200 ms, so the whole stream takes longer than the limit, but no single gap exceeds it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/providers/`
Expected: FAIL (build failed) with `undefined: newHTTPClient` and `undefined: ErrStreamIdle`.

- [ ] **Step 3: Implement the helper**

Create `internal/providers/httpclient.go`:

```go
package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

const (
	defaultStreamIdleTimeout = 120 * time.Second
	// Non-streaming replies send their headers only after the whole generation.
	defaultResponseHeaderTimeout = 10 * time.Minute
)

var ErrStreamIdle = errors.New("provider stream idle timeout")

// NewHTTPClient returns the client for model requests: no whole-request
// deadline, but a response body silent for defaultStreamIdleTimeout fails.
func NewHTTPClient() *http.Client {
	return newHTTPClient(defaultResponseHeaderTimeout, defaultStreamIdleTimeout)
}

func newHTTPClient(headerTimeout time.Duration, idleTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Transport: idleTimeoutTransport{base: transport, idle: idleTimeout}}
}

type idleTimeoutTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t idleTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	res, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel(nil)
		return nil, err
	}
	res.Body = &idleTimeoutBody{
		body:   res.Body,
		ctx:    ctx,
		cancel: cancel,
		idle:   t.idle,
		timer:  time.AfterFunc(t.idle, func() { cancel(ErrStreamIdle) }),
	}
	return res, nil
}

// idleTimeoutBody cancels the request when no Read returns for idle.
type idleTimeoutBody struct {
	body   io.ReadCloser
	ctx    context.Context
	cancel context.CancelCauseFunc
	idle   time.Duration
	timer  *time.Timer
}

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err != nil && errors.Is(context.Cause(b.ctx), ErrStreamIdle) {
		return n, ErrStreamIdle
	}
	b.timer.Reset(b.idle)
	return n, err
}

func (b *idleTimeoutBody) Close() error {
	b.timer.Stop()
	b.cancel(nil)
	return b.body.Close()
}
```

`ErrStreamIdle` deliberately does not wrap `context.Canceled`. `IsRetryableGenerationError` treats cancellation as final, and a user cancel still surfaces as `context.Canceled`, because its cause is not `ErrStreamIdle`.

`internal/providers/generation_errors.go`: replace the `if errors.Is(err, ErrEmptyResponse) …` condition with:

```go
	if errors.Is(err, ErrEmptyResponse) || errors.Is(err, ErrIncompleteResponse) || errors.Is(err, ErrStreamIdle) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
```

- [ ] **Step 4: Run the test to verify it passes (also under the race detector)**

Run: `go test ./internal/providers/ && go test -race -count=5 -run TestHTTPClientTimesOutOnlyOnSilence ./internal/providers/`
Expected: `ok` twice.

- [ ] **Step 5: Switch the four adapters to the shared client**

- `internal/providers/ai/anthropiccompat/adapter.go`: delete `defaultTimeout = 90 * time.Second` from the const block. In `normalizeConfig`, `client = &http.Client{Timeout: defaultTimeout}` becomes `client = providers.NewHTTPClient()`.
- `internal/providers/ai/gemini/adapter.go`: the const block becomes `const defaultMaxOutputTokens = 4096`. In `normalizeConfig`, use `client = providers.NewHTTPClient()`.
- `internal/providers/ai/openaicompat/config.go`: delete `const defaultTimeout = 5 * time.Minute`. In `normalizeConfig`, use `client = providers.NewHTTPClient()`. Make the same change in `ListModels` in `models.go`.
- `internal/providers/ai/openaicodex/runtime.go`: delete `const defaultTimeout = 120 * time.Second`. In `New`, use `client = providers.NewHTTPClient()`. Make the same change in `ListModels` in `models.go`.
- Delete the obsolete test: `git rm internal/providers/ai/openaicompat/config_test.go`

Run: `$(go env GOPATH)/bin/goimports -w internal/providers/ai/anthropiccompat/adapter.go internal/providers/ai/gemini/adapter.go internal/providers/ai/openaicompat/config.go internal/providers/ai/openaicompat/models.go internal/providers/ai/openaicodex/runtime.go internal/providers/ai/openaicodex/models.go`. This removes the now-unused `time` imports.

- [ ] **Step 6: Verify**

Run: `go build ./... && go vet ./... && go test ./... 2>&1 | grep -E "^(FAIL|---)"; grep -rn "Timeout:" internal/providers/ai`
Expected: the test grep prints nothing. The last grep prints only the three `openaicodex/auth.go` lines (`defaultRequestTimout`).

- [ ] **Step 7: Commit**

```bash
gofmt -l internal clients cmd
git add internal/providers/httpclient.go internal/providers/httpclient_test.go internal/providers/generation_errors.go \
  internal/providers/ai/anthropiccompat/adapter.go internal/providers/ai/gemini/adapter.go \
  internal/providers/ai/openaicompat/config.go internal/providers/ai/openaicompat/models.go \
  internal/providers/ai/openaicodex/runtime.go internal/providers/ai/openaicodex/models.go
git commit -m "feat(providers): idle stream timeouts instead of whole-request caps

All adapters share providers.NewHTTPClient: 10 min to response
headers, 120 s allowed between body reads. A stalled stream fails
with the retryable ErrStreamIdle; long healthy streams are no longer
cut at 90 s/120 s/5 min.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

`config_test.go` is not in the list: `git rm` in Step 5 already staged its deletion.

---

### Task 9: iOS package — tolerant `RunStatus`, `seq`, normalised usage

`clients/ios` has no test target yet, and `which swift` finds nothing on this host. The full package does not build on Linux either, because `EventStreamClient.swift:138` uses `URLSession.bytes(for:)`, which Linux Foundation lacks.

To verify here, copy `Package.swift`, `Tests/` and the four Foundation-only model sources (`Models`, `JSONValue`, `MatrixclawCoding`, `MatrixclawClientError`) into a throwaway package inside the `swift:6.0-jammy` Docker image (already pulled on this host), then run `swift test` there. The repo is mounted read-only, so no build artefacts land in it. On macOS/Xcode, run `swift test` in `clients/ios` directly.

**Files:**
- Modify: `clients/ios/Package.swift:16-20`
- Modify: `clients/ios/Sources/MatrixclawClient/Models.swift:62-73` (`Message`), `:211-218` (`RunStatus`), `:332-340` (`ProviderUsage`)
- Create: `clients/ios/Tests/MatrixclawClientTests/ModelDecodingTests.swift`

- [ ] **Step 1: Add the test target and the failing tests**

`clients/ios/Package.swift`: the `targets` array becomes:

```swift
    targets: [
        .target(
            name: "MatrixclawClient"
        ),
        .testTarget(
            name: "MatrixclawClientTests",
            dependencies: ["MatrixclawClient"]
        )
    ]
```

Create `clients/ios/Tests/MatrixclawClientTests/ModelDecodingTests.swift`:

```swift
import Foundation
import XCTest
@testable import MatrixclawClient

final class ModelDecodingTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try MatrixclawCoding.decoder.decode(type, from: Data(json.utf8))
    }

    func testUnknownRunStatusDecodesAndRoundTrips() throws {
        let run = try decode(Run.self, #"{"id":"r1","session_id":"s1","user_message_id":"m1","status":"waiting_events","started_at":"2026-09-23T10:00:00Z","updated_at":"2026-09-23T10:00:00.5Z"}"#)
        XCTAssertEqual(run.status, .unknown("waiting_events"))
        let again = try MatrixclawCoding.decoder.decode(Run.self, from: MatrixclawCoding.encoder.encode(run))
        XCTAssertEqual(again.status.rawValue, "waiting_events")
    }

    func testKnownRunStatusesKeepTheirCases() throws {
        let cases: [(String, RunStatus)] = [
            ("accepted", .accepted), ("running", .running), ("waiting_approval", .waitingApproval),
            ("completed", .completed), ("canceled", .canceled), ("failed", .failed),
        ]
        for (raw, want) in cases {
            let status = try decode(RunStatus.self, "\"\(raw)\"")
            XCTAssertEqual(status, want)
            XCTAssertEqual(status.rawValue, raw)
        }
    }

    func testMessageSeqAndNormalisedUsageDecode() throws {
        let message = try decode(Message.self, #"{"id":"m1","seq":42,"session_id":"s1","run_id":"r1","role":"user","content":"hi","created_at":"2026-09-23T10:00:00Z","updated_at":"2026-09-23T10:00:00Z"}"#)
        XCTAssertEqual(message.seq, 42)
        let usage = try decode(ProviderUsage.self, #"{"prompt_tokens":420,"output_tokens":50,"cache_read_tokens":300,"cache_write_tokens":20}"#)
        XCTAssertEqual(usage.promptTokens, 420)
        XCTAssertEqual(usage.cacheReadTokens, 300)
        XCTAssertEqual(usage.cacheWriteTokens, 20)
        XCTAssertEqual(usage.outputTokens, 50)
    }
}
```

Write the Linux runner. It lives outside the repo and is not committed.

```bash
cat > /tmp/matrixclaw-ios-test.sh <<'EOF'
docker run --rm -v "$1":/pkg:ro swift:6.0-jammy bash -c '
set -e
mkdir -p /work/Sources/MatrixclawClient
cp /pkg/Package.swift /work/
cp -r /pkg/Tests /work/
for f in Models JSONValue MatrixclawCoding MatrixclawClientError; do cp /pkg/Sources/MatrixclawClient/$f.swift /work/Sources/MatrixclawClient/; done
cd /work && swift test 2>&1 | tail -15'
EOF
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 900 bash /tmp/matrixclaw-ios-test.sh /root/projects/matrixclaw/clients/ios`
Expected: compile errors `type 'RunStatus' has no member 'unknown'`, `value of type 'Message' has no member 'seq'`, `value of type 'ProviderUsage' has no member 'promptTokens'` / `'cacheWriteTokens'`.

- [ ] **Step 3: Implement**

In `Models.swift`, add `seq` after `id` in `struct Message`:

```swift
public struct Message: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var seq: Int64?
```

Replace `public enum RunStatus: String, Codable, Sendable { … }` with:

```swift
/// Statuses added by newer daemons decode as `.unknown` instead of failing.
public enum RunStatus: RawRepresentable, Codable, Equatable, Hashable, Sendable {
    case accepted
    case running
    case waitingApproval
    case completed
    case canceled
    case failed
    case unknown(String)

    public init(rawValue: String) {
        switch rawValue {
        case "accepted": self = .accepted
        case "running": self = .running
        case "waiting_approval": self = .waitingApproval
        case "completed": self = .completed
        case "canceled": self = .canceled
        case "failed": self = .failed
        default: self = .unknown(rawValue)
        }
    }

    public var rawValue: String {
        switch self {
        case .accepted: return "accepted"
        case .running: return "running"
        case .waitingApproval: return "waiting_approval"
        case .completed: return "completed"
        case .canceled: return "canceled"
        case .failed: return "failed"
        case .unknown(let value): return value
        }
    }

    public init(from decoder: Decoder) throws {
        self.init(rawValue: try decoder.singleValueContainer().decode(String.self))
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }
}
```

The non-failable `init(rawValue:)` satisfies `RawRepresentable`. Existing app code that calls `RunStatus(rawValue:)` or `.rawValue` still compiles. A `switch` over `RunStatus` in an app now needs a `.unknown` case (or `default`), and it should have one.

Replace `ProviderUsage` with the normalised shape the daemon now sends:

```swift
public struct ProviderUsage: Codable, Equatable, Sendable {
    public var promptTokens: Int64?
    public var outputTokens: Int64?
    public var cacheReadTokens: Int64?
    public var cacheWriteTokens: Int64?
    public var reasoningTokens: Int64?
    public var providerRaw: JSONValue?
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 900 bash /tmp/matrixclaw-ios-test.sh /root/projects/matrixclaw/clients/ios`
Expected: `Executed 3 tests, with 0 failures (0 unexpected)`.

- [ ] **Step 5: Commit**

```bash
git add clients/ios/Package.swift clients/ios/Sources/MatrixclawClient/Models.swift clients/ios/Tests/MatrixclawClientTests/ModelDecodingTests.swift
git commit -m "feat(ios): decode unknown run statuses and normalised usage

RunStatus gains .unknown(String) so statuses added by newer daemons do
not break shipped apps; Message gains seq; ProviderUsage follows the
daemon's prompt/cache/output/reasoning fields. Adds the first test
target.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Stage verification (no commit)

- [ ] **Step 1: Full Go gate**

Run:

```bash
cd /root/projects/matrixclaw
go build ./... && go vet ./... && gofmt -l internal clients cmd
go test -count=1 ./... 2>&1 | grep -E "^(FAIL|---|panic)"; echo "grep-exit=$?"
```

Expected: `gofmt` prints nothing, and the last line reads `grep-exit=1`, meaning no failures.

- [ ] **Step 2: Contract spot checks**

```bash
grep -rn "type Message struct" internal/core internal/transcript          # only internal/transcript/message.go
grep -rn "saveRunUsage\|SaveUsageRecord\|ProviderUsage struct\|http.Client{Timeout: defaultTimeout}" internal --include=*.go   # nothing
grep -n "GetMessage\|HasToolResult\|ListMessagesAfter\|SaveRunStep\|ListRunSteps" internal/core/ports.go   # all five present
```

- [ ] **Step 3: Migration smoke test on a copy of the live database**

This checks the backfill and the `run_usage` fold on real data without touching the running daemon. `sqlite3 .backup` is safe while `matrixclawd` holds the database. The temporary test file must be deleted afterwards and never committed.

```bash
cd /root/projects/matrixclaw
SMOKE=/tmp/matrixclaw-stage0-smoke; rm -rf "$SMOKE"; mkdir -p "$SMOKE"
sqlite3 ~/.local/state/matrixclaw/matrixclaw.db ".backup '$SMOKE/matrixclaw.db'"
sqlite3 "$SMOKE/matrixclaw.db" "SELECT COUNT(*) FROM messages; SELECT COUNT(*) FROM run_usage;"
cat > internal/store/zz_smoke_test.go <<'EOF'
package store_test

import (
	"os"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/store"
)

func TestSmokeMigrateCopy(t *testing.T) {
	path := os.Getenv("SMOKE_DB")
	if path == "" {
		t.Skip("SMOKE_DB not set")
	}
	for i := 0; i < 2; i++ {
		started := time.Now()
		st, err := store.NewSQLite(path)
		if err != nil {
			t.Fatal(err)
		}
		_ = st.Close()
		t.Logf("open %d took %s", i, time.Since(started))
	}
}
EOF
SMOKE_DB="$SMOKE/matrixclaw.db" go test -count=1 -run TestSmokeMigrateCopy -v ./internal/store/
rm internal/store/zz_smoke_test.go
sqlite3 "$SMOKE/matrixclaw.db" "SELECT COUNT(*), COUNT(DISTINCT seq), MIN(seq), MAX(seq), SUM(seq = 0) FROM messages;"
sqlite3 "$SMOKE/matrixclaw.db" "SELECT name FROM sqlite_master WHERE name IN ('run_usage','run_steps','idx_messages_seq','idx_messages_session_seq','idx_messages_session_created_at') ORDER BY name;"
sqlite3 "$SMOKE/matrixclaw.db" "SELECT COUNT(*) FROM run_steps WHERE stop_reason = 'legacy';"
git status --short internal/store
rm -rf "$SMOKE"
```

Expected:
- The test passes. The second open should take well under the first, because there is nothing left to backfill.
- The messages query shows `count = distinct = max`, `min = 1`, and `0` rows with `seq = 0`.
- Only `idx_messages_seq`, `idx_messages_session_seq` and `run_steps` are listed.
- The legacy step count equals the pre-migration `run_usage` count, minus runs that no longer exist.
- `git status` shows no `zz_smoke_test.go`.

- [ ] **Step 4: Manual check in the TUI (owner or on the test stand)**

Build with `go build -o ./bin/matrixclaw ./cmd/matrixclaw && go build -o ./bin/matrixclawd ./cmd/matrixclawd`, then run a daemon on a scratch state dir (`XDG_STATE_HOME=$(mktemp -d)`) with a real provider. Ask for a task that needs 3–4 tool calls. Then:
- `/usage` shows `Steps` equal to the number of model calls, and non-zero cache reads on the second and later steps for providers with implicit caching.
- `/context` → Usage shows `Last provider usage: … in (… cached, … written) / … out`.
- `curl -s localhost:<port>/v1/runs/<run_id>/steps` lists the steps in order.

Do not point a second daemon at the live `~/.local/state/matrixclaw`: the running `matrixclawd` (pid 1562) owns it, and its Telegram worker would be duplicated.

---

## Self-review

**1. Spec coverage (Stages row 0 and contracts "Stage 0 provides"):**

| Requirement | Task |
|---|---|
| `internal/transcript` with all moved names, importers updated, core no longer defines them | 1 |
| `Message.Seq` `json:"seq,omitempty"` | 2 |
| `messages.seq` via `ensureColumn`, backfilled in (created_at, rowid) order, index (session_id, seq), `MAX(seq)+1` in the insert, all ordering by seq | 2 (plus a unique `idx_messages_seq` so the `MAX` is O(log n)) |
| `GetMessage`, `HasToolResult`, `ListMessagesAfter` (ascending, limit 0 = all) | 3 (store and core callers), 4 (API) |
| `providers.Usage` shape, old fields removed, every adapter maps into it | 7 |
| Whole-request timeouts removed in all 4 adapters, shared helper with header and idle timeouts, 120 s idle | 8 (header timeout is 10 min, see below) |
| iOS `RunStatus` → `.unknown(String)` | 9 |
| `run_steps` table and columns, PK (run_id, step), `core.RunStep` in `types_run.go`, `SaveRunStep`/`ListRunSteps`, one row per generation, per-run usage from run_steps, full-scan rebuild deleted | 5, 6 |
| Spec §3 "cache read/write per step … shown in /status" | shown in `/usage` and `/context` (Tasks 5, 7) and at `GET /v1/runs/{id}/steps`, because `/status` is the server-status command |
| Spec §6 testing: "iOS package: decoding of an unknown run status"; "per provider … normalised usage, idle timeout" | 9; 7 (four adapters) and 8 (shared transport) |

Not in Stage 0, by design: the `run_step` event and structured log line (spec §6 Observability) belong to `Journal.RecordStep` in stage 2a. `Origin` in transcript belongs to stage 2b.

**2. Placeholder scan:** every code step shows complete code or an exact before/after block. Commands have expected output. There are no "TBD", "similar to", or "add error handling" steps.

**3. Type consistency:**
- `transcript.Message.Seq int64` is used by the store scan (Task 2) and the tests (2, 3, 4).
- `core.RunStep` fields (`PromptTokens`, `CacheReadTokens`, `CacheWriteTokens`, `OutputTokens`, `ReasoningTokens`, `StopReason`, `LatencyMillis`, `ToolCalls`, `CreatedAt`) match the store SQL and tests in Tasks 5 and 6.
- `core.UsageRecord`/`UsageSummary` fields (`Steps`, `PromptTokens`, …, `UpdatedAt`) match `ListUsageRecords`, `summarizeUsage`, `/usage` and the tests.
- `providers.Usage` fields defined in Task 7 match the adapter tests, `recordRunStep`, `latestProviderUsage`, `providerUsageLabel` and the Swift `ProviderUsage` keys (`prompt_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`, `provider_raw`).
- Task 5 intentionally maps from the pre-Task-7 fields (`InputTokens`, `CachedTokens`), and Task 7 Step 5 rewrites exactly that literal.
- `newAPITestServer`/`apiTestEpoch` are defined in Task 4 and reused in Task 6. The `tokenReportRuntime` stub is defined in Task 5 and extended in Task 7.

**Deviations from the contract text (for other stage plans):**
- `SaveRunStep` assigns `Step` itself (`MAX(step)+1` per run) and ignores the caller's `Step`.
- The response-header timeout is 10 min, not 120 s, because non-streaming generations only send headers when they finish.
- `run_steps.stop_reason` holds `tool_use`/`end_turn` (derived from tool calls) and `compact` for summaries until stage 1 supplies `providers.StopReason`.
- `SaveMessage` still returns only `error`; the assigned `seq` is visible after a read.
