# Stage 6a — Background Tasks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Shell commands that outlive their call become durable background tasks: `bash(run_in_background)` starts a process group whose output goes to a private, size-capped file; a foreground command gets a timeout (10 minutes, up to 60) and moves to the background after 2 minutes; `task_output` / `task_kill` replace `job_output` / `job_kill`; one `tasks` table holds shell tasks and subagents (replacing `subagent_tasks`); a finished task is an event the session's run reads as an engine note; leftover process groups are killed at daemon start and their tasks marked `lost`; `/tasks` lists and stops a session's background tasks in the TUI and Telegram.

**Architecture:** A new leaf package `internal/shelltask` runs `bash -lc` in its own process group with stdout/stderr piped into an `Output` file (0600, at most 20 MB: first 1 MB, a fixed-width marker naming the dropped bytes, the newest rest) and reads it by output offset, so a cursor survives the rewrite; it kills groups and recognises leftover leaders by their `ps -o lstart=` start time (no `/proc`, so it works on macOS). Core owns the task service (`internal/core/shell_tasks.go`): it implements the new `tools.ShellTasks` port the `bash`, `task_output` and `task_kill` executors call, records tasks in the `tasks` table, watches live processes and finishes them once (`FinishTask`), and stops leftovers at start (`RecoverTasks`). A finished background task with `delivered_at` NULL is the event: the engine's new `InputEvent` inbox kind journals it as an `origin: engine` note at the start of the next step and consumes it; running tasks are a section of the context note. Subagent rows move into the same table (kind `subagent`), their completion bookkeeping becomes `delivered_at` / `delivered_run_id`.

**Tech Stack:** Go 1.26, SQLite (modernc), `os/exec` with `Setpgid`, `ps` for process start times, bubbletea v2 TUI, Telegram Bot API.

**Prerequisite / base:** Stages 0–5 and the stage 4c review fixes are on `main`; this plan was written and every task built and tested in a scratch copy on top of **`d290a40`** ("fix(todo): empty lists as [], quieter context note, scrolling TUI panel"). Line numbers are not used; locate code by the declaration names given.

---

## Ground rules for executors

- Repo `/root/projects/matrixclaw`, module `github.com/Suren878/matrixclaw`. Work directly on `main`. Another session may commit in parallel: run `git status --short` before each commit and stage only the paths the task lists with explicit `git add <paths>` / `git rm <paths>` (never `-A`, `-u` or directories).
- Code blocks give complete new files, or for existing files the **whole new version of every declaration that changes** ("replace `func X` with"), the declarations to add, the declarations to delete by name, and the new import block when it changes. `func (T) M` names a method on `T` or `*T`. Non-Go files are given as diffs.
- Run `gofmt -w` on every Go file you touch. Every commit must pass `go build ./... && go vet ./... && go test ./...`.
- Commit messages end with a blank line and exactly `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- Owner rules: delete replaced code (no shims, aliases or dead code); doc comments at most 4 lines and without history; tests only on observable behaviour; match the surrounding style; core tests call `t.Parallel()`.
- Do not start, stop or restart any `matrixclawd` daemon and do not touch `~/.matrixclaw`: a production daemon runs on this host. Tests start real `bash`/`sleep` processes in temporary directories and kill them.
- If the plan does not fit the code, fix small mismatches and report them; stop with BLOCKED / NEEDS_CONTEXT for anything bigger.
- Verified: every task below was committed in a scratch worktree on top of `d290a40`; after each commit `go build ./... && go vet ./... && go test ./...` passed; on the final state also `go test -race` for `internal/core`, `internal/shelltask`, `internal/tools`, `internal/agent/...`, `internal/store`, `internal/controlplane`, `internal/api`, and `deadcode -test ./...` reported nothing new.

## Decisions taken in this plan (owner should know)

1. **One table, two views.** `tasks(id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal, description, working_dir, background, readonly, agent_name, runtime, model, isolation, pid, pgid, output_path, exit_code, output_cursor, child_session_id, child_run_id, summary, error, result_message_id, delivered_at, delivered_run_id, started_at, updated_at, finished_at)` — the spec's columns plus what subagents already keep (`description` = display name, `agent_name`, `summary`, `error`, `result_message_id`), `background` (async subagents and every shell task), `readonly` and `model` for stage 6c. Go keeps `SubagentTask` as the subagent view (same API) and adds `Task` as the generic view (shell tasks, events, `/tasks`). `SubagentTaskStatus` becomes `TaskStatus` (one status type; `lost` added for shell tasks).
2. **Migration**: on open, if `subagent_tasks` exists its rows are copied with `INSERT OR IGNORE … SELECT` (missing old columns take their defaults, rows of deleted sessions are skipped) and the table is dropped, in one transaction; the second open finds no table. `completion_queued_at/delivered_at/auto_resume_run_id` become `delivered_at` / `delivered_run_id`: a finished row never queued is copied as delivered. Only `MarkTasksDelivered` writes the delivery columns.
3. **Events are rows.** A finished background task with `delivered_at` NULL is an event of its session. The engine gets `InputEvent`; each step starts by journaling the session's events (finish order) as `origin: engine` notes and consuming them (`delivered_run_id` = the run). A task the model already knows the end of is delivered at once: blocking subagents, `task_kill`, subagents canceled with their parent, and a `task_output` read that saw the task finished. Subagent completions also reach a *running* parent this way now; an idle parent still gets the existing follow-up run (stage 6b generalises it).
4. **Note texts.** Shell: `Background task <id> finished with exit code N.` (or `was stopped: <why>.` / `was lost: <why>.`), `Command: …`, the last 2000 bytes of output, and the file path. Subagent: `Subagent <name> (<id>) finished: <status>.`, goal, result. The context note gains `Background tasks still running (task_output reads them, task_kill stops them):` with one line per running background task.
5. **Output files** live at `<session files>/<session>/tasks/<task>.log` (0600), so `removeSessionFiles` deletes them with the session; deleting a session first kills its running shell tasks. Past 20 MB the file is rewritten as head (1 MB) + marker `\n[... <19 digits> bytes of output dropped ...]\n` + the newest ~9.5 MB; reads take output offsets and report skipped bytes. Foreground commands write the same file and remove it when they finish, unless their output exceeded what the call returns (30 000 bytes); the result then names the kept file.
6. **Bash arguments:** `timeout` and `auto_background_after` are seconds (defaults 600 and 120; timeout at most 3600, else an error result before approval). A command moved to the background keeps its timeout (killed at the deadline, status `failed`, error names the timeout); `run_in_background` commands have none. A foreground command waits 2 s for commands it left running to close the output (as before); a background one waits for them, so `server &` keeps its task alive.
7. **`task_output{id, wait_seconds ≤ 600, filter}`** returns at most 30 000 bytes from the session's cursor for that task (`output_cursor`), then the status line `(task running)` / `(task failed, exit code 1)`; a regexp `filter` keeps matching lines of what was read (the cursor still advances); `More` tells the model to call again or read the file. For a subagent task it returns the status and result. **`task_kill{id}`** keeps `job_kill`'s approval (mutating, `task_kill` permission params) and also cancels a subagent task.
8. **Restart:** `RecoverTasks` runs synchronously before the workflow worker starts (so no run can start a task meanwhile). For each `running` shell task it kills the group unless the leader PID is alive with another start time (PID reused); it then marks the task `lost` ("the daemon restarted while it ran and stopped it"). The next run of the session reads that as a note; lost tasks never start a run.
9. **`/tasks` is shared** with the scheduled-task picker that already owns the command: the bound session's background tasks (newest 10) come first, then the scheduled tasks as today. `/tasks bg <id>` opens Output / Stop (Stop asks to confirm); API `GET /v1/sessions/{id}/tasks`, `GET /v1/tasks/{id}` (task + last 3000 bytes) and `POST /v1/tasks/{id}/cancel`. A task the user stops stays an event, so the session's next run learns about it. The spec did not say which command owned `/tasks`; renaming the scheduled tasks was the alternative.
10. **Tool wiring:** `bash`, `task_output`, `task_kill` keep their specs in `tools.coreDefinitions` without a `NewExecutor`; `tools.NewShellExecutors(tasks ShellTasks)` builds them and the daemon registers them with the core as `ShellTasks`. `tools.Result.Background` and `tools.BackgroundJob` are deleted (metadata carries `task_id`). The TUI renders `task_output` / `task_kill` like the old job tools.
11. **Prompt:** one tool-use line asks to run long commands in the background and read them with `task_output`; stage 6b adds `await` to it.

Out of scope: `await` and waking runs (6b), the unified `agent` tool (6c), killing the run's tasks when it is canceled (6c), re-attaching processes after a restart (spec: deferred).

## File structure

Created: `internal/shelltask/{output,process}.go` (+ `shelltask_test.go`), `internal/store/{schema_tasks,sqlite_tasks}.go` (+ `sqlite_tasks_test.go`), `internal/core/{types_task,shell_tasks,task_events,tasks}.go` (+ `shell_tasks_test.go`), `internal/tools/tasks.go`, `internal/tools/shell_test.go`, `internal/agent/events.go` (+ `events_test.go`), `internal/api/tasks.go` (+ test), `internal/daemonclient/tasks.go`, `internal/controlplane/background_tasks.go` (+ test).

Modified: store schema/migration and subagent queries; core subagent persistence, ports, events, sessions, inbox, prompts; `internal/tools/{shell,definitions,schema,types}.go`; engine `ports.go`, `engine.go`, `agenttest`; API/daemonclient/clientruntime/controlplane routing; TUI `surface/chat/{bash,tools}.go`; `internal/mcp/server.go`; `internal/daemoncmd/run.go`; prompt guidance; the spec.

---



### Task 1: `internal/shelltask` — process groups and capped output files

**Files:**
- Create: `internal/shelltask/output.go`
- Create: `internal/shelltask/process.go`
- Test (create): `internal/shelltask/shelltask_test.go`

The process layer knows nothing of sessions or storage. `Output` is an `io.Writer` the command's pipe copies into; `Read` maps output offsets to the file through the marker, `Tail` returns the newest bytes, `KillLeftover` checks the leader's start time with `ps -o lstart= -p <pid>` (with `LC_ALL=C TZ=UTC`) and allows two seconds of slack for ps's second precision.

- [ ] **Step 1: Write the failing tests**

Create `internal/shelltask/shelltask_test.go`:

```go
package shelltask_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/shelltask"
)

func newOutput(t *testing.T) *shelltask.Output {
	t.Helper()
	out, err := shelltask.CreateOutput(filepath.Join(t.TempDir(), "tasks", "task_1.log"))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOutputFileIsPrivateAndReadFromACursor(t *testing.T) {
	out := newOutput(t)
	if _, err := out.Write([]byte("hello\nwörld\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}

	first, err := shelltask.Read(out.Path(), 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	// "wö" would split the two-byte ö; the read stops before it.
	if first.Text != "hello\nw" || first.Next != 7 || !first.More {
		t.Fatalf("first = %+v", first)
	}
	rest, err := shelltask.Read(out.Path(), first.Next, 100)
	if err != nil || rest.Text != "örld\n" || rest.More {
		t.Fatalf("rest = %+v, %v", rest, err)
	}
}

func TestOutputKeepsItsHeadAndNewestTailPastTheCap(t *testing.T) {
	out := newOutput(t)
	head := bytes.Repeat([]byte("h"), shelltask.HeadBytes)
	middle := bytes.Repeat([]byte("m"), shelltask.MaxBytes)
	for _, part := range [][]byte{head, middle, []byte("THE END")} {
		if _, err := out.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(out.Path())
	if err != nil || info.Size() > shelltask.MaxBytes {
		t.Fatalf("size = %d, %v", info.Size(), err)
	}
	tail, err := shelltask.Tail(out.Path(), 7)
	if err != nil || tail != "THE END" {
		t.Fatalf("tail = %q, %v", tail, err)
	}

	start, err := shelltask.Read(out.Path(), 0, 10)
	if err != nil || start.Text != "hhhhhhhhhh" {
		t.Fatalf("start = %+v, %v", start, err)
	}
	// Reading past the head skips what was dropped and lands in the kept tail.
	afterHead, err := shelltask.Read(out.Path(), shelltask.HeadBytes, 10)
	if err != nil || afterHead.Skipped == 0 || afterHead.Text != "mmmmmmmmmm" {
		t.Fatalf("after head = skipped %d text %q, %v", afterHead.Skipped, afterHead.Text, err)
	}
	total := int64(shelltask.HeadBytes) + shelltask.MaxBytes + 7
	end, err := shelltask.Read(out.Path(), total-7, 100)
	if err != nil || end.Text != "THE END" || end.More || end.Next != total {
		t.Fatalf("end = %+v, %v", end, err)
	}
}

func TestProcessRunsInItsOwnGroupAndReportsItsExitCode(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("echo out; echo err >&2; exit 3", t.TempDir(), out, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if code := p.ExitCode(); code != 3 {
		t.Fatalf("exit code = %d", code)
	}
	chunk, err := shelltask.Read(out.Path(), 0, 100)
	if err != nil || chunk.Text != "out\nerr\n" {
		t.Fatalf("output = %+v, %v", chunk, err)
	}
}

func TestKillEndsTheWholeGroup(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("sleep 60 & sleep 60; wait", t.TempDir(), out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the group outlived Kill")
	}
	if code := p.ExitCode(); code != -1 {
		t.Fatalf("exit code = %d", code)
	}
}

func TestKillLeftoverChecksTheLeaderStartTime(t *testing.T) {
	out := newOutput(t)
	p, err := shelltask.Start("sleep 60", t.TempDir(), out, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Kill() })

	if err := shelltask.KillLeftover(p.PID(), p.PID(), p.StartedAt().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
		t.Fatal("a process started at another time was killed")
	case <-time.After(200 * time.Millisecond):
	}

	if err := shelltask.KillLeftover(p.PID(), p.PID(), p.StartedAt()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the leftover group was not killed")
	}
	if err := shelltask.KillLeftover(p.PID(), p.PID(), p.StartedAt()); err != nil {
		t.Fatalf("a gone group: %v", err)
	}
}

func TestReadOfAMissingFileFails(t *testing.T) {
	if _, err := shelltask.Read(filepath.Join(t.TempDir(), "gone.log"), 0, 10); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/shelltask -run '^(TestOutputFileIsPrivateAndReadFromACursor|TestOutputKeepsItsHeadAndNewestTailPastTheCap|TestProcessRunsInItsOwnGroupAndReportsItsExitCode|TestKillEndsTheWholeGroup|TestKillLeftoverChecksTheLeaderStartTime|TestReadOfAMissingFileFails)$'
```

Expected: build fails: package shelltask has no Go files.

- [ ] **Step 3: Implement**

Create `internal/shelltask/output.go`:

```go
// Package shelltask runs shell commands in process groups of their own and keeps
// their output in a size-capped file.
package shelltask

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// An output file holds at most MaxBytes: once full it keeps the first HeadBytes,
// a marker naming how much was dropped, and the newest output after it.
const (
	MaxBytes  = 20 << 20
	HeadBytes = 1 << 20
)

const (
	markerPrefix = "\n[... "
	markerSuffix = " bytes of output dropped ...]\n"
	markerDigits = 19
	markerLen    = int64(len(markerPrefix) + markerDigits + len(markerSuffix))
)

// Output is the file a command writes its stdout and stderr to.
type Output struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	size    int64
	dropped int64
}

// CreateOutput creates the file at path, readable by the owner only.
func CreateOutput(path string) (*Output, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &Output{path: path, file: file}, nil
}

// Path is where the output is kept.
func (o *Output) Path() string {
	return o.path
}

// Write appends p; output past MaxBytes drops the oldest bytes after the head.
func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.file == nil {
		return 0, os.ErrClosed
	}
	if o.size+int64(len(p)) <= MaxBytes {
		n, err := o.file.Write(p)
		o.size += int64(n)
		return n, err
	}
	if err := o.compact(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes the file; the output stays readable.
func (o *Output) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.file == nil {
		return nil
	}
	err := o.file.Close()
	o.file = nil
	return err
}

// compact rewrites the file as its head, the marker and the newest half of the
// room left, ending with p.
func (o *Output) compact(p []byte) error {
	tailStart := int64(HeadBytes)
	if o.dropped > 0 {
		tailStart += markerLen
	}
	tail := o.size - tailStart
	keep := (MaxBytes - HeadBytes - markerLen) / 2
	fromFile := min(max(keep-int64(len(p)), 0), tail)
	o.dropped += tail - fromFile
	if int64(len(p)) > keep {
		o.dropped += int64(len(p)) - keep
		p = p[int64(len(p))-keep:]
	}

	source, err := os.Open(o.path)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	temp, err := os.CreateTemp(filepath.Dir(o.path), ".output-*")
	if err != nil {
		return err
	}
	written, err := o.writeCompacted(temp, source, tailStart+tail-fromFile, fromFile, p)
	if err = errors.Join(err, temp.Close()); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := os.Rename(temp.Name(), o.path); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	_ = o.file.Close()
	o.file, err = os.OpenFile(o.path, os.O_WRONLY|os.O_APPEND, 0o600)
	o.size = written
	return err
}

func (o *Output) writeCompacted(dst io.Writer, source *os.File, keptFrom, kept int64, p []byte) (int64, error) {
	var written int64
	for _, part := range []io.Reader{
		io.NewSectionReader(source, 0, HeadBytes),
		strings.NewReader(marker(o.dropped)),
		io.NewSectionReader(source, keptFrom, kept),
		bytes.NewReader(p),
	} {
		n, err := io.Copy(dst, part)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func marker(dropped int64) string {
	return fmt.Sprintf("%s%0*d%s", markerPrefix, markerDigits, dropped, markerSuffix)
}

// layout says where the dropped part of an output file is: dropped bytes of
// the output are missing between HeadBytes and the rest of the file.
type layout struct {
	size    int64
	dropped int64
}

func readLayout(file *os.File) (layout, error) {
	info, err := file.Stat()
	if err != nil {
		return layout{}, err
	}
	out := layout{size: info.Size()}
	if out.size < HeadBytes+markerLen {
		return out, nil
	}
	buf := make([]byte, markerLen)
	if _, err := file.ReadAt(buf, HeadBytes); err != nil {
		return layout{}, err
	}
	text := string(buf)
	if !strings.HasPrefix(text, markerPrefix) || !strings.HasSuffix(text, markerSuffix) {
		return out, nil
	}
	dropped, err := strconv.ParseInt(text[len(markerPrefix):len(markerPrefix)+markerDigits], 10, 64)
	if err != nil {
		return out, nil
	}
	out.dropped = dropped
	return out, nil
}

// total is how many bytes the command has written.
func (l layout) total() int64 {
	if l.dropped == 0 {
		return l.size
	}
	return l.size - markerLen + l.dropped
}

// fileOffset maps an output offset to the file; skipped counts output bytes
// that were dropped before it.
func (l layout) fileOffset(cursor int64) (offset int64, skipped int64) {
	if l.dropped == 0 || cursor < HeadBytes {
		return cursor, 0
	}
	if cursor < HeadBytes+l.dropped {
		skipped = HeadBytes + l.dropped - cursor
		cursor = HeadBytes + l.dropped
	}
	return cursor - l.dropped + markerLen, skipped
}

// Chunk is output read from a cursor on.
type Chunk struct {
	Text string
	// Next is the cursor after Text; Skipped counts bytes dropped before it.
	Next    int64
	Skipped int64
	// More is set when output past Next was already written.
	More bool
}

// Read returns up to limit bytes of the output at path from cursor on, an
// output offset that survives dropping; it never splits a UTF-8 character.
func Read(path string, cursor int64, limit int) (Chunk, error) {
	file, err := os.Open(path)
	if err != nil {
		return Chunk{}, err
	}
	defer func() { _ = file.Close() }()
	l, err := readLayout(file)
	if err != nil {
		return Chunk{}, err
	}
	cursor = min(max(cursor, 0), l.total())
	offset, skipped := l.fileOffset(cursor)
	cursor += skipped
	end := l.size
	if l.dropped > 0 && offset < HeadBytes {
		end = HeadBytes
	}
	n := min(int64(limit), end-offset)
	buf := make([]byte, n)
	if _, err := file.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return Chunk{}, err
	}
	buf = completeRunes(buf, offset+n < l.size)
	next := cursor + int64(len(buf))
	return Chunk{Text: string(buf), Next: next, Skipped: skipped, More: next < l.total()}, nil
}

// Tail returns the last limit bytes of the output at path.
func Tail(path string, limit int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	n := min(int64(limit), info.Size())
	buf := make([]byte, n)
	if _, err := file.ReadAt(buf, info.Size()-n); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	for len(buf) > 0 && !utf8.RuneStart(buf[0]) {
		buf = buf[1:]
	}
	return string(buf), nil
}

// completeRunes drops a character cut at the end of buf when more follows.
func completeRunes(buf []byte, more bool) []byte {
	if !more {
		return buf
	}
	for i := len(buf) - 1; i >= 0 && i >= len(buf)-utf8.UTFMax; i-- {
		if utf8.RuneStart(buf[i]) {
			if !utf8.FullRune(buf[i:]) {
				return buf[:i]
			}
			break
		}
	}
	return buf
}
```

Create `internal/shelltask/process.go`:

```go
package shelltask

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Process is a running shell command; its process group has the ID of its PID.
type Process struct {
	cmd       *exec.Cmd
	out       *Output
	done      chan struct{}
	exitCode  int
	startedAt time.Time
}

// Start runs command with bash -lc in dir, in a process group of its own, its
// stdout and stderr going to out. Once the shell exits, Done waits at most
// waitDelay for commands it left running to close the output; 0 waits for them.
func Start(command, dir string, out *Output, waitDelay time.Duration) (*Process, error) {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	p := &Process{cmd: cmd, out: out, done: make(chan struct{}), exitCode: -1, startedAt: time.Now()}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go p.wait()
	return p, nil
}

func (p *Process) wait() {
	_ = p.cmd.Wait()
	if state := p.cmd.ProcessState; state != nil {
		p.exitCode = state.ExitCode()
	}
	_ = p.out.Close()
	close(p.done)
}

// PID is the shell's process ID, which is also its process group ID.
func (p *Process) PID() int {
	return p.cmd.Process.Pid
}

// StartedAt is when the process was started.
func (p *Process) StartedAt() time.Time {
	return p.startedAt
}

// Output is where the process writes.
func (p *Process) Output() *Output {
	return p.out
}

// Done is closed once the process exited and its output is closed.
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// ExitCode is the shell's exit code once Done is closed; -1 when a signal ended it.
func (p *Process) ExitCode() int {
	<-p.done
	return p.exitCode
}

// Kill ends every process of the group.
func (p *Process) Kill() error {
	return KillGroup(p.PID())
}

// KillGroup sends SIGKILL to the process group; a gone group is no error.
func KillGroup(pgid int) error {
	if pgid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// KillLeftover kills the process group a daemon that is gone started at
// startedAt with leader pid. A leader that is alive but started at another time
// is some other process that got the ID, and is left alone.
func KillLeftover(pid, pgid int, startedAt time.Time) error {
	started, alive, err := processStart(pid)
	if err != nil {
		return err
	}
	if alive && !sameSecond(started, startedAt) {
		return nil
	}
	return KillGroup(pgid)
}

// processStart reads when process pid started, from ps so that it works
// without /proc; alive is false when there is no such process.
func processStart(pid int) (started time.Time, alive bool, err error) {
	if pid <= 0 {
		return time.Time{}, false, nil
	}
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return time.Time{}, false, nil
	}
	cmd := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	text := strings.Join(strings.Fields(string(out)), " ")
	if text == "" {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	started, err = time.Parse("Mon Jan 2 15:04:05 2006", text)
	return started, true, err
}

// sameSecond reports whether ps's second-precision start time matches t.
func sameSecond(ps time.Time, t time.Time) bool {
	diff := ps.Sub(t.UTC().Truncate(time.Second))
	return diff >= -2*time.Second && diff <= 2*time.Second
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/shelltask -run '^(TestOutputFileIsPrivateAndReadFromACursor|TestOutputKeepsItsHeadAndNewestTailPastTheCap|TestProcessRunsInItsOwnGroupAndReportsItsExitCode|TestKillEndsTheWholeGroup|TestKillLeftoverChecksTheLeaderStartTime|TestReadOfAMissingFileFails)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/shelltask/output.go \
  internal/shelltask/process.go \
  internal/shelltask/shelltask_test.go
git commit -m "feat(shelltask): process groups and size-capped output files

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 2: One task status type

**Files:** every Go file naming `SubagentTaskStatus` or `subagentTaskTerminalStatus`; Create: `internal/core/types_task.go`

One status type serves shell tasks and subagents. The rename is mechanical (it also renames `activeSubagentTaskStatuses` to `activeTaskStatuses`); the type then moves to a new file.

- [ ] **Step 1: Rename**

```bash
grep -rl 'SubagentTaskStatus\|subagentTaskTerminalStatus' --include=*.go . \
  | xargs sed -i 's/SubagentTaskStatus/TaskStatus/g; s/subagentTaskTerminalStatus/taskStatusTerminal/g'
```

- [ ] **Step 2: Move the type**

Delete the `type TaskStatus string` declaration and its `const (...)` block from `internal/core/types_subagent.go`, and `func taskStatusTerminal` from `internal/core/subagents_presentation.go`. Create `internal/core/types_task.go`:

```go
package core

// TaskStatus is where a background task stands.
type TaskStatus string

const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusWaitingApproval TaskStatus = "waiting_approval"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusFailed          TaskStatus = "failed"
	TaskStatusCanceled        TaskStatus = "canceled"
)

func taskStatusTerminal(status TaskStatus) bool {
	return status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCanceled
}
```

- [ ] **Step 3: Format, check and commit**

```bash
gofmt -w $(git diff --name-only -- '*.go') internal/core/types_task.go
go build ./... && go vet ./... && go test ./...
git status --short
git add internal/core/types_task.go $(git diff --name-only -- internal clients)
git commit -m "refactor(core): one task status type for background tasks

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

The files the rename touches are 19 in `internal/core`, `internal/store/sqlite_subagents.go` and `clients/terminal/chat/runtime/{app_layout_views,conversation}.go`; make sure `git diff --name-only` shows nothing else before adding.


### Task 3: The `tasks` table replaces `subagent_tasks`

**Files:**
- Modify: `internal/core/ports.go`
- Modify: `internal/core/subagents_lifecycle.go`
- Modify: `internal/core/subagents_persistence.go`
- Modify: `internal/core/subagents_work.go`
- Modify: `internal/core/types_subagent.go`
- Modify: `internal/store/migrations/001_init.sql`
- Modify: `internal/store/schema.go`
- Create: `internal/store/schema_tasks.go`
- Modify: `internal/store/sqlite_subagents.go`
- Create: `internal/store/sqlite_tasks.go`
- Test (modify): `internal/core/native_run_characterization_test.go`
- Test (modify): `internal/core/subagents_cancel_test.go`
- Test (create): `internal/store/sqlite_tasks_test.go`

`tasks` replaces `subagent_tasks` in `001_init.sql` (new databases never create the old table) and `migrateSubagentTasks` moves existing rows. The subagent store methods keep their signatures but read and write `tasks` rows of kind `subagent` (`goal` ↔ `command_or_goal`, `display_name` ↔ `description`, `mode` ↔ `background`, `created_at` ↔ `started_at`). `SubagentTask`'s completion fields become `DeliveredAt` / `DeliveredRunID`: a finished task with `DeliveredAt == nil` is a queued completion; `finishSubagentTaskRecord(..., queueCompletion=false)` marks the task delivered before it finishes it, so it is never an event. `MarkTasksDelivered` is the only writer of the delivery columns (it keeps the first delivery). The core `TaskStore` port starts here with that one method.

- [ ] **Step 1: Write the failing tests**

In `internal/core/native_run_characterization_test.go`, replace `func TestAsyncSubagentCompletionStartsParentFollowUpRun` with:

```go
func TestAsyncSubagentCompletionStartsParentFollowUpRun(t *testing.T) {
	t.Parallel()
	scenario := runAsyncSubagentScenario(t)

	task, err := scenario.db.GetSubagentTaskByParentToolCall(context.Background(), scenario.session.ID, scenario.run.ID, "call-spawn")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.TaskStatusCompleted || task.DeliveredAt == nil || task.DeliveredRunID == "" {
		t.Fatalf("task = %s delivered=%v by %q", task.Status, task.DeliveredAt, task.DeliveredRunID)
	}
}
```

In `internal/core/subagents_cancel_test.go`, replace `func TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp` with:

```go
func TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	childStarted := newStartSignal()
	parentWaiting := newStartSignal()
	var mu sync.Mutex
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return blockUntilCanceled(ctx, childStarted)
		}
		mu.Lock()
		parentCalls++
		first := parentCalls == 1
		mu.Unlock()
		if first {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "spawn_subagent", Arguments: []byte(`{"name":"Scanner","goal":"scan the tree","runtime":"matrixclaw"}`)}}}, nil
		}
		return blockUntilCanceled(ctx, parentWaiting)
	})})
	session, run := saveCrashRecoveryRun(t, db, "cancel-spawn", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, childStarted.ch, "child generation")
	waitRecoverySignal(t, parentWaiting.ch, "parent generation")

	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled parent"); err != nil {
		t.Fatalf("ExecuteRun after cancel: %v", err)
	}
	starter.wait(t)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-spawn")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCanceled)
	assertTaskStatus(t, task, core.TaskStatusCanceled)
	if task.DeliveredAt == nil || task.DeliveredRunID != "" {
		t.Fatal("a canceled async subagent left a completion for its canceled parent")
	}
	if got := starter.count(task.ChildRunID); got != 1 {
		t.Fatalf("child starts = %d, want 1", got)
	}
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleUser && message.RunID != run.ID {
			t.Fatalf("parent session got a follow-up run %s after cancel", message.RunID)
		}
	}
}
```

Create `internal/store/sqlite_tasks_test.go`:

```go
package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSubagentTasksMoveIntoTheTasksTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE subagent_tasks (id TEXT PRIMARY KEY, mode TEXT NOT NULL DEFAULT 'blocking', parent_session_id TEXT NOT NULL,
			parent_run_id TEXT NOT NULL DEFAULT '', parent_tool_call_id TEXT NOT NULL DEFAULT '', child_session_id TEXT NOT NULL DEFAULT '',
			child_run_id TEXT NOT NULL DEFAULT '', runtime TEXT NOT NULL, goal TEXT NOT NULL, status TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', completion_queued_at TEXT, completion_delivered_at TEXT,
			completion_auto_resume_run_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, finished_at TEXT)`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, parent_run_id, parent_tool_call_id, child_run_id, runtime, goal, status, summary,
			completion_queued_at, created_at, updated_at, finished_at)
			VALUES('queued', 'async', 's1', 'r1', 'call_1', 'child_1', 'matrixclaw', 'Read the logs', 'completed', 'All good',
			'2026-09-23T10:00:05Z', '2026-09-23T10:00:00Z', '2026-09-23T10:00:05Z', '2026-09-23T10:00:05Z')`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, runtime, goal, status, completion_queued_at, completion_delivered_at,
			completion_auto_resume_run_id, created_at, updated_at, finished_at)
			VALUES('delivered', 'async', 's1', 'matrixclaw', 'Fix it', 'completed', '2026-09-23T10:00:05Z', '2026-09-23T10:00:06Z',
			'r2', '2026-09-23T10:00:00Z', '2026-09-23T10:00:06Z', '2026-09-23T10:00:05Z')`,
		`INSERT INTO subagent_tasks(id, parent_session_id, runtime, goal, status, created_at, updated_at, finished_at)
			VALUES('blocking', 's1', 'matrixclaw', 'Check', 'failed', '2026-09-23T10:00:00Z', '2026-09-23T10:00:01Z', '2026-09-23T10:00:01Z')`,
		`INSERT INTO subagent_tasks(id, mode, parent_session_id, runtime, goal, status, created_at, updated_at)
			VALUES('running', 'async', 's1', 'codex', 'Build', 'running', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
		`INSERT INTO subagent_tasks(id, parent_session_id, runtime, goal, status, created_at, updated_at)
			VALUES('orphan', 'gone', 'matrixclaw', 'Lost', 'running', '2026-09-23T10:00:00Z', '2026-09-23T10:00:00Z')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	for reopen := 0; reopen < 2; reopen++ {
		st := openTestStore(t, path)
		pending, err := st.ListPendingSubagentCompletionTasks(ctx, 0)
		if err != nil || len(pending) != 1 || pending[0].ID != "queued" {
			t.Fatalf("open %d: pending = %+v, %v", reopen, pending, err)
		}
		queued := pending[0]
		if queued.Mode != core.SubagentTaskModeAsync || queued.ParentRunID != "r1" || queued.ParentToolCallID != "call_1" ||
			queued.ChildRunID != "child_1" || queued.Goal != "Read the logs" || queued.Summary != "All good" || queued.Isolation != core.SubagentIsolationShared {
			t.Fatalf("open %d: queued = %+v", reopen, queued)
		}
		delivered, err := st.GetSubagentTask(ctx, "delivered")
		if err != nil || delivered.DeliveredAt == nil || delivered.DeliveredRunID != "r2" {
			t.Fatalf("open %d: delivered = %+v, %v", reopen, delivered, err)
		}
		blocking, err := st.GetSubagentTask(ctx, "blocking")
		if err != nil || blocking.Mode != core.SubagentTaskModeBlocking || blocking.DeliveredAt == nil {
			t.Fatalf("open %d: blocking = %+v, %v", reopen, blocking, err)
		}
		active, err := st.ListActiveSubagentTasksByParent(ctx, "s1")
		if err != nil || len(active) != 1 || active[0].ID != "running" || active[0].DeliveredAt != nil {
			t.Fatalf("open %d: active = %+v, %v", reopen, active, err)
		}
		if _, err := st.GetSubagentTask(ctx, "orphan"); err != core.ErrNotFound {
			t.Fatalf("open %d: orphan err = %v", reopen, err)
		}
		_ = st.Close()
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var tables int
	if err := check.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'subagent_tasks'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("subagent_tasks still exists after the migration")
	}
}

func TestMarkTasksDeliveredKeepsTheFirstDelivery(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	task := core.SubagentTask{ID: "task_1", Mode: core.SubagentTaskModeAsync, ParentSessionID: "s1", Runtime: "matrixclaw", Goal: "Look", Status: core.TaskStatusCompleted, CreatedAt: testEpoch, UpdatedAt: testEpoch, FinishedAt: &testEpoch}
	if err := st.CreateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_1"}, "run_a", testEpoch); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_1"}, "run_b", testEpoch.Add(1)); err != nil {
		t.Fatal(err)
	}
	// Saving the task again leaves its delivery alone.
	if err := st.UpdateSubagentTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSubagentTask(ctx, "task_1")
	if err != nil || got.DeliveredRunID != "run_a" || got.DeliveredAt == nil || !got.DeliveredAt.Equal(testEpoch) {
		t.Fatalf("task = %+v, %v", got, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/core -run '^(TestAsyncSubagentCompletionStartsParentFollowUpRun|TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp)$'
go test ./internal/store -run '^(TestSubagentTasksMoveIntoTheTasksTable|TestMarkTasksDeliveredKeepsTheFirstDelivery)$'
```

Expected: build fails (`st.MarkTasksDelivered` and `DeliveredAt` are undefined).

- [ ] **Step 3: Implement**

In `internal/core/ports.go`, replace `type Store` with:

```go
type Store interface {
	SessionStore
	SubagentTaskStore
	TaskStore
	BindingStore
	DeliveryStore
	MessageStore
	RunStore
	SessionInputStore
	UsageStore
	SessionBudgetStore
	EngineStateStore
	TodoStore
	SearchStore
	MemoryStore
	ApprovalStore
	PermissionRuleStore
	FileSnapshotStore
}
```

In `internal/core/ports.go`, add:

```go
// TaskStore keeps the background tasks of sessions.
type TaskStore interface {
	MarkTasksDelivered(ctx context.Context, taskIDs []string, runID string, at time.Time) error
}
```

In `internal/core/subagents_lifecycle.go`, replace `func (Core) deliverPendingSubagentCompletionsForParent` with:

```go
func (c *Core) deliverPendingSubagentCompletionsForParent(ctx context.Context, parentSessionID string) error {
	parentSessionID = normalizeText(parentSessionID)
	if parentSessionID == "" {
		return nil
	}
	if ready, err := c.parentReadyForSubagentAutoResume(ctx, parentSessionID); err != nil || !ready {
		return err
	}
	tasks, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{
		ParentSessionID: parentSessionID,
		Mode:            SubagentTaskModeAsync,
		Statuses: []TaskStatus{
			TaskStatusCompleted,
			TaskStatusFailed,
			TaskStatusCanceled,
		},
		Limit: 50,
	})
	if err != nil {
		return err
	}
	pending := make([]SubagentTask, 0, len(tasks))
	for _, task := range tasks {
		if task.DeliveredAt == nil {
			pending = append(pending, task)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	triggerID := subagentCompletionTriggerID(parentSessionID, pending)
	result, err := c.AcceptTriggeredRun(ctx, HandleTriggeredRunInput{
		TriggerID: triggerID,
		SessionID: parentSessionID,
		Text:      subagentCompletionPrompt(pending),
	})
	if err != nil {
		return err
	}
	now := c.now().UTC()
	for _, task := range pending {
		if _, err := c.markSubagentCompletionDelivered(ctx, task, now, result.Run.ID); err != nil {
			return err
		}
	}
	return nil
}
```

In `internal/core/subagents_persistence.go`, replace `func (Core) finishSubagentTaskRecord` with:

```go
// finishSubagentTaskRecord ends the task; without queueCompletion its parent
// is not told it finished, as it already knows.
func (c *Core) finishSubagentTaskRecord(ctx context.Context, task SubagentTask, status TaskStatus, summary string, errText string, queueCompletion bool) (SubagentTask, error) {
	now := c.now().UTC()
	if !queueCompletion {
		if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, "", now); err != nil {
			return SubagentTask{}, err
		}
		task.DeliveredAt = &now
	}
	return c.updateSubagentTaskRecordWith(ctx, task, func(task *SubagentTask) {
		task.Status = status
		task.Summary = summary
		task.Error = errText
		task.UpdatedAt = now
		finishedAt := now
		task.FinishedAt = &finishedAt
	})
}
```

In `internal/core/subagents_persistence.go`, replace `func (Core) markSubagentCompletionDelivered` with:

```go
func (c *Core) markSubagentCompletionDelivered(ctx context.Context, task SubagentTask, at time.Time, runID string) (SubagentTask, error) {
	if at.IsZero() {
		at = c.now().UTC()
	}
	at = at.UTC()
	if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, runID, at); err != nil {
		return SubagentTask{}, err
	}
	task.DeliveredAt = &at
	task.DeliveredRunID = runID
	task.UpdatedAt = at
	c.saveSubagentWorkJob(ctx, task)
	c.publishSubagentTaskUpdated(task)
	return task, nil
}
```

In `internal/core/subagents_work.go`, replace `type subagentWorkResult` with:

```go
type subagentWorkResult struct {
	TaskID           string `json:"task_id"`
	AgentName        string `json:"agent_name,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	Mode             string `json:"mode,omitempty"`
	Isolation        string `json:"isolation,omitempty"`
	ParentSessionID  string `json:"parent_session_id,omitempty"`
	ParentRunID      string `json:"parent_run_id,omitempty"`
	ParentToolCallID string `json:"parent_tool_call_id,omitempty"`
	ChildSessionID   string `json:"child_session_id,omitempty"`
	ChildRunID       string `json:"child_run_id,omitempty"`
	Runtime          string `json:"runtime,omitempty"`
	Status           string `json:"status,omitempty"`
	Error            string `json:"error,omitempty"`
	ResultMessageID  string `json:"result_message_id,omitempty"`
	DeliveredAt      string `json:"delivered_at,omitempty"`
	DeliveredRunID   string `json:"delivered_run_id,omitempty"`
}
```

In `internal/core/subagents_work.go`, replace `func subagentWorkJob` with:

```go
func subagentWorkJob(task SubagentTask) work.Job {
	resultRaw, _ := json.Marshal(subagentWorkResult{
		TaskID:           task.ID,
		AgentName:        task.AgentName,
		DisplayName:      task.DisplayName,
		Mode:             string(task.Mode),
		Isolation:        string(task.Isolation),
		ParentSessionID:  task.ParentSessionID,
		ParentRunID:      task.ParentRunID,
		ParentToolCallID: task.ParentToolCallID,
		ChildSessionID:   task.ChildSessionID,
		ChildRunID:       task.ChildRunID,
		Runtime:          task.Runtime,
		Status:           string(task.Status),
		Error:            task.Error,
		ResultMessageID:  task.ResultMessageID,
		DeliveredAt:      optionalTime(task.DeliveredAt),
		DeliveredRunID:   task.DeliveredRunID,
	})
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := task.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	job := work.Job{
		ID:         task.ID,
		Kind:       work.KindSubagent,
		Status:     workStatusForSubagent(task.Status),
		Task:       task.Goal,
		Summary:    task.Summary,
		ResultJSON: string(resultRaw),
		WorkerRef:  task.ChildRunID,
		Error:      task.Error,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		FinishedAt: task.FinishedAt,
	}
	if task.Status == TaskStatusRunning || task.Status == TaskStatusWaitingApproval {
		heartbeatAt := updatedAt
		job.HeartbeatAt = &heartbeatAt
		startedAt := createdAt
		job.StartedAt = &startedAt
		job.Attempts = 1
	}
	return job
}
```

In `internal/core/types_subagent.go`, replace `type SubagentTask` with:

```go
type SubagentTask struct {
	ID               string            `json:"id"`
	AgentName        string            `json:"agent_name,omitempty"`
	DisplayName      string            `json:"display_name,omitempty"`
	Mode             SubagentTaskMode  `json:"mode"`
	Isolation        SubagentIsolation `json:"isolation"`
	ParentSessionID  string            `json:"parent_session_id"`
	ParentRunID      string            `json:"parent_run_id,omitempty"`
	ParentToolCallID string            `json:"parent_tool_call_id,omitempty"`
	ChildSessionID   string            `json:"child_session_id,omitempty"`
	ChildRunID       string            `json:"child_run_id,omitempty"`
	Runtime          string            `json:"runtime"`
	Goal             string            `json:"goal"`
	Status           TaskStatus        `json:"status"`
	Summary          string            `json:"summary,omitempty"`
	Error            string            `json:"error,omitempty"`
	ResultMessageID  string            `json:"result_message_id,omitempty"`
	// DeliveredAt is when the parent was told the task finished, and
	// DeliveredRunID the run that told it; nil while that is due.
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	DeliveredRunID string     `json:"delivered_run_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}
```

In `internal/store/migrations/001_init.sql`:

```diff
diff --git a/internal/store/migrations/001_init.sql b/internal/store/migrations/001_init.sql
index 98a1382..ae31d3a 100644
--- a/internal/store/migrations/001_init.sql
+++ b/internal/store/migrations/001_init.sql
@@ -14,30 +14,38 @@ CREATE TABLE IF NOT EXISTS sessions (
     updated_at TEXT NOT NULL
 );
 
-CREATE TABLE IF NOT EXISTS subagent_tasks (
+CREATE TABLE IF NOT EXISTS tasks (
     id TEXT PRIMARY KEY,
-    agent_name TEXT NOT NULL DEFAULT '',
-    display_name TEXT NOT NULL DEFAULT '',
-    mode TEXT NOT NULL DEFAULT 'blocking',
-    isolation TEXT NOT NULL DEFAULT 'shared',
-    parent_session_id TEXT NOT NULL,
-    parent_run_id TEXT NOT NULL DEFAULT '',
+    session_id TEXT NOT NULL,
+    run_id TEXT NOT NULL DEFAULT '',
     parent_tool_call_id TEXT NOT NULL DEFAULT '',
+    kind TEXT NOT NULL,
+    status TEXT NOT NULL,
+    command_or_goal TEXT NOT NULL DEFAULT '',
+    description TEXT NOT NULL DEFAULT '',
+    working_dir TEXT NOT NULL DEFAULT '',
+    background INTEGER NOT NULL DEFAULT 1,
+    readonly INTEGER NOT NULL DEFAULT 0,
+    agent_name TEXT NOT NULL DEFAULT '',
+    runtime TEXT NOT NULL DEFAULT '',
+    model TEXT NOT NULL DEFAULT '',
+    isolation TEXT NOT NULL DEFAULT '',
+    pid INTEGER NOT NULL DEFAULT 0,
+    pgid INTEGER NOT NULL DEFAULT 0,
+    output_path TEXT NOT NULL DEFAULT '',
+    exit_code INTEGER,
+    output_cursor INTEGER NOT NULL DEFAULT 0,
     child_session_id TEXT NOT NULL DEFAULT '',
     child_run_id TEXT NOT NULL DEFAULT '',
-    runtime TEXT NOT NULL,
-    goal TEXT NOT NULL,
-    status TEXT NOT NULL,
     summary TEXT NOT NULL DEFAULT '',
     error TEXT NOT NULL DEFAULT '',
     result_message_id TEXT NOT NULL DEFAULT '',
-    completion_queued_at TEXT,
-    completion_delivered_at TEXT,
-    completion_auto_resume_run_id TEXT NOT NULL DEFAULT '',
-    created_at TEXT NOT NULL,
+    delivered_at TEXT,
+    delivered_run_id TEXT NOT NULL DEFAULT '',
+    started_at TEXT NOT NULL,
     updated_at TEXT NOT NULL,
     finished_at TEXT,
-    FOREIGN KEY (parent_session_id) REFERENCES sessions(id) ON DELETE CASCADE
+    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
 );
 
 CREATE TABLE IF NOT EXISTS client_bindings (
@@ -237,11 +245,14 @@ CREATE TABLE IF NOT EXISTS external_agent_sessions (
     FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
 );
 
-CREATE INDEX IF NOT EXISTS idx_subagent_tasks_parent
-    ON subagent_tasks(parent_session_id, parent_run_id, parent_tool_call_id);
+CREATE INDEX IF NOT EXISTS idx_tasks_session_status
+    ON tasks(session_id, status);
+
+CREATE INDEX IF NOT EXISTS idx_tasks_parent_call
+    ON tasks(session_id, run_id, parent_tool_call_id);
 
-CREATE INDEX IF NOT EXISTS idx_subagent_tasks_child_run
-    ON subagent_tasks(child_run_id);
+CREATE INDEX IF NOT EXISTS idx_tasks_child_run
+    ON tasks(child_run_id);
 
 CREATE INDEX IF NOT EXISTS idx_runs_session_started_at
     ON runs(session_id, started_at);
```

In `internal/store/schema.go`, replace `func applyCanonicalSchema` with:

```go
func applyCanonicalSchema(db *sql.DB) error {
	if _, err := db.Exec(canonicalSchemaSQL); err != nil {
		return fmt.Errorf("store: apply canonical schema: %w", err)
	}
	if err := ensureColumn(db, "sessions", "kind", `ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'assistant'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "sessions", "runtime_id", `ALTER TABLE sessions ADD COLUMN runtime_id TEXT NOT NULL DEFAULT 'matrixclaw'`); err != nil {
		return err
	}
	if err := ensureColumn(db, "sessions", "parent_session_id", `ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "sessions", "hidden", `ALTER TABLE sessions ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := migrateMessageSeq(db); err != nil {
		return err
	}
	if err := ensureColumn(db, "messages", "origin", `ALTER TABLE messages ADD COLUMN origin TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := migrateCompactionMarkers(db); err != nil {
		return err
	}
	if err := migrateRunUsage(db); err != nil {
		return err
	}
	if err := ensureColumn(db, "external_agent_sessions", "approval_policy", `ALTER TABLE external_agent_sessions ADD COLUMN approval_policy TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "external_agent_sessions", "sandbox", `ALTER TABLE external_agent_sessions ADD COLUMN sandbox TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    key TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    working_dir TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("store: create memories table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_scope_workdir_updated ON memories(scope, working_dir, updated_at DESC)`); err != nil {
		return fmt.Errorf("store: create memories index: %w", err)
	}
	if err := dropPlanningTables(db); err != nil {
		return err
	}
	if err := migrateSubagentTasks(db); err != nil {
		return err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_inputs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    target_run_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    parts_json TEXT NOT NULL DEFAULT '',
    client TEXT NOT NULL DEFAULT '',
    external_key TEXT NOT NULL DEFAULT '',
    delivery_address_json TEXT NOT NULL DEFAULT '',
    working_dir TEXT NOT NULL DEFAULT '',
    consumed_run_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    consumed_at TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session inputs table: %w", err)
	}
	if err := ensureColumn(db, "session_inputs", "delivery_address_json", `ALTER TABLE session_inputs ADD COLUMN delivery_address_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "client_capabilities_json", `ALTER TABLE runs ADD COLUMN client_capabilities_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "stop_reason", `ALTER TABLE runs ADD COLUMN stop_reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "continues_run_id", `ALTER TABLE runs ADD COLUMN continues_run_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "runs", "trigger_kind", `ALTER TABLE runs ADD COLUMN trigger_kind TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "run_checkpoints", "engine_state", `ALTER TABLE run_checkpoints ADD COLUMN engine_state TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "run_checkpoints", "tool_batch", `ALTER TABLE run_checkpoints ADD COLUMN tool_batch TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "session_inputs", "client_capabilities_json", `ALTER TABLE session_inputs ADD COLUMN client_capabilities_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "client_deliveries", "payload_json", `ALTER TABLE client_deliveries ADD COLUMN payload_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "approvals", "reason", `ALTER TABLE approvals ADD COLUMN reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := ensureColumn(db, "approvals", "suggestion_json", `ALTER TABLE approvals ADD COLUMN suggestion_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id, hidden)`); err != nil {
		return fmt.Errorf("store: create sessions parent index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_inputs_session_status_created ON session_inputs(session_id, status, created_at)`); err != nil {
		return fmt.Errorf("store: create session inputs status index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_session_inputs_target_run ON session_inputs(target_run_id, mode, status)`); err != nil {
		return fmt.Errorf("store: create session inputs target index: %w", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET runtime_id = 'external_agent' WHERE kind = 'external_agent' AND runtime_id IN ('matrixclaw', 'codex', 'codex-app')`); err != nil {
		return fmt.Errorf("store: backfill external session runtime: %w", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET permission_mode = 'full_auto' WHERE kind = 'external_agent' AND permission_mode = 'default'`); err != nil {
		return fmt.Errorf("store: backfill external session permission mode: %w", err)
	}
	if _, err := db.Exec(`UPDATE external_agent_sessions SET approval_policy = 'never' WHERE approval_policy = ''`); err != nil {
		return fmt.Errorf("store: backfill external session approval policy: %w", err)
	}
	if _, err := db.Exec(`UPDATE external_agent_sessions SET sandbox = 'danger-full-access' WHERE sandbox = ''`); err != nil {
		return fmt.Errorf("store: backfill external session sandbox: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_budgets (
    session_id TEXT PRIMARY KEY,
    steps INTEGER,
    active_seconds INTEGER,
    tokens INTEGER,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session budgets table: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_engine_state (
    session_id TEXT PRIMARY KEY,
    engine_state TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session engine state table: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS session_todos (
    session_id TEXT PRIMARY KEY,
    items_json TEXT NOT NULL,
    chain_run_id TEXT NOT NULL DEFAULT '',
    updated_run_id TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`); err != nil {
		return fmt.Errorf("store: create session todos table: %w", err)
	}
	if err := migratePermissionRules(db); err != nil {
		return err
	}
	return migrateMessageSearch(db)
}
```

Create `internal/store/schema_tasks.go`:

```go
package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// migrateSubagentTasks copies the rows of subagent_tasks into tasks and drops
// it. A finished task never queued for its parent counts as delivered; columns
// that older databases lack take their defaults.
func migrateSubagentTasks(db *sql.DB) error {
	columns, err := tableColumns(db, "subagent_tasks")
	if err != nil || len(columns) == 0 {
		return err
	}
	column := func(name, fallback string) string {
		if columns[name] {
			return name
		}
		return fallback
	}
	queued := column("completion_queued_at", "NULL")
	copyRows := `
INSERT OR IGNORE INTO tasks(id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal,
    description, background, agent_name, runtime, isolation, child_session_id, child_run_id, summary, error,
    result_message_id, delivered_at, delivered_run_id, started_at, updated_at, finished_at)
SELECT id, parent_session_id, parent_run_id, parent_tool_call_id, 'subagent', status, goal,
    ` + column("display_name", "''") + `,
    CASE WHEN ` + column("mode", "'blocking'") + ` = 'async' THEN 1 ELSE 0 END,
    ` + column("agent_name", "''") + `, runtime, ` + column("isolation", "'shared'") + `,
    child_session_id, child_run_id, summary, error, ` + column("result_message_id", "''") + `,
    COALESCE(` + column("completion_delivered_at", "NULL") + `, CASE WHEN ` + queued + ` IS NULL THEN finished_at END),
    ` + column("completion_auto_resume_run_id", "''") + `, created_at, updated_at, finished_at
FROM subagent_tasks
WHERE parent_session_id IN (SELECT id FROM sessions)`
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin subagent_tasks migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(copyRows); err != nil {
		return fmt.Errorf("store: copy subagent_tasks into tasks: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE subagent_tasks`); err != nil {
		return fmt.Errorf("store: drop subagent_tasks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit subagent_tasks migration: %w", err)
	}
	return nil
}

// tableColumns names the columns of table; none when it does not exist.
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("store: inspect %s schema: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: scan %s schema: %w", table, err)
		}
		columns[strings.ToLower(name)] = true
	}
	return columns, rows.Err()
}
```

Replace the whole of `internal/store/sqlite_subagents.go` with:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

// Subagent tasks are the rows of tasks of kind subagent.

func (s *SQLiteStore) CreateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	task = normalizeSubagentTaskForStore(task)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks(
    id, kind, agent_name, description, background, isolation, session_id, run_id, parent_tool_call_id,
    child_session_id, child_run_id, runtime, command_or_goal, status, summary, error, result_message_id,
    started_at, updated_at, finished_at
)
VALUES(?, 'subagent', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.AgentName,
		task.DisplayName,
		task.Mode == core.SubagentTaskModeAsync,
		string(task.Isolation),
		task.ParentSessionID,
		task.ParentRunID,
		task.ParentToolCallID,
		task.ChildSessionID,
		task.ChildRunID,
		task.Runtime,
		task.Goal,
		string(task.Status),
		task.Summary,
		task.Error,
		task.ResultMessageID,
		formatTime(task.CreatedAt),
		formatTime(task.UpdatedAt),
		nullableTime(task.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create subagent task: %w", err)
	}
	return nil
}

// UpdateSubagentTask saves the task; whether and when it was delivered is kept
// by MarkTasksDelivered alone.
func (s *SQLiteStore) UpdateSubagentTask(ctx context.Context, task core.SubagentTask) error {
	task = normalizeSubagentTaskForStore(task)
	result, err := s.db.ExecContext(ctx, `
UPDATE tasks
SET agent_name = ?, description = ?, background = ?, isolation = ?, session_id = ?, run_id = ?, parent_tool_call_id = ?,
    child_session_id = ?, child_run_id = ?, runtime = ?, command_or_goal = ?, status = ?, summary = ?, error = ?,
    result_message_id = ?, updated_at = ?, finished_at = ?
WHERE id = ? AND kind = 'subagent'`,
		task.AgentName,
		task.DisplayName,
		task.Mode == core.SubagentTaskModeAsync,
		string(task.Isolation),
		task.ParentSessionID,
		task.ParentRunID,
		task.ParentToolCallID,
		task.ChildSessionID,
		task.ChildRunID,
		task.Runtime,
		task.Goal,
		string(task.Status),
		task.Summary,
		task.Error,
		task.ResultMessageID,
		formatTime(task.UpdatedAt),
		nullableTime(task.FinishedAt),
		task.ID,
	)
	if err != nil {
		return fmt.Errorf("store: update subagent task: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: update subagent task rows: %w", err)
	} else if rows == 0 {
		return core.ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) GetSubagentTask(ctx context.Context, taskID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM tasks
WHERE id = ? AND kind = 'subagent'`, taskID)
	return scanOneSubagentTask(row)
}

func (s *SQLiteStore) GetSubagentTaskByParentToolCall(ctx context.Context, parentSessionID string, parentRunID string, parentToolCallID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM tasks
WHERE kind = 'subagent' AND session_id = ? AND run_id = ? AND parent_tool_call_id = ?
ORDER BY started_at ASC
LIMIT 1`, parentSessionID, parentRunID, parentToolCallID)
	return scanOneSubagentTask(row)
}

func (s *SQLiteStore) GetSubagentTaskByChildRun(ctx context.Context, childRunID string) (core.SubagentTask, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+subagentTaskColumns+`
FROM tasks
WHERE kind = 'subagent' AND child_run_id = ?
ORDER BY started_at ASC
LIMIT 1`, childRunID)
	return scanOneSubagentTask(row)
}

func (s *SQLiteStore) ListSubagentTasks(ctx context.Context, filter core.SubagentTaskFilter) ([]core.SubagentTask, error) {
	where := []string{"kind = 'subagent'"}
	var args []any
	if strings.TrimSpace(filter.ParentSessionID) != "" {
		where = append(where, "session_id = ?")
		args = append(args, strings.TrimSpace(filter.ParentSessionID))
	}
	if filter.Mode != "" {
		where = append(where, "background = ?")
		args = append(args, filter.Mode == core.SubagentTaskModeAsync)
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, string(status))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	query := "SELECT " + subagentTaskColumns + " FROM tasks WHERE " + strings.Join(where, " AND ") + " ORDER BY started_at DESC, id DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	return s.listSubagentTasks(ctx, query, args...)
}

func (s *SQLiteStore) ListActiveSubagentTasksByParent(ctx context.Context, parentSessionID string) ([]core.SubagentTask, error) {
	return s.ListSubagentTasks(ctx, core.SubagentTaskFilter{
		ParentSessionID: strings.TrimSpace(parentSessionID),
		Mode:            core.SubagentTaskModeAsync,
		Statuses: []core.TaskStatus{
			core.TaskStatusPending,
			core.TaskStatusRunning,
			core.TaskStatusWaitingApproval,
		},
	})
}

// ListPendingSubagentCompletionTasks lists finished async subagents whose parent
// was not told yet, oldest first.
func (s *SQLiteStore) ListPendingSubagentCompletionTasks(ctx context.Context, limit int) ([]core.SubagentTask, error) {
	query := "SELECT " + subagentTaskColumns + ` FROM tasks
WHERE kind = 'subagent' AND background = 1 AND finished_at IS NOT NULL AND delivered_at IS NULL
ORDER BY finished_at ASC, id ASC`
	var args []any
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	return s.listSubagentTasks(ctx, query, args...)
}

func (s *SQLiteStore) listSubagentTasks(ctx context.Context, query string, args ...any) ([]core.SubagentTask, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list subagent tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tasks []core.SubagentTask
	for rows.Next() {
		task, err := scanSubagentTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate subagent tasks: %w", err)
	}
	return tasks, nil
}

type subagentTaskScanner interface {
	Scan(dest ...any) error
}

const subagentTaskColumns = `id, agent_name, description, background, isolation, session_id, run_id, parent_tool_call_id,
child_session_id, child_run_id, runtime, command_or_goal, status, summary, error, result_message_id,
delivered_at, delivered_run_id, started_at, updated_at, finished_at`

func scanOneSubagentTask(row *sql.Row) (core.SubagentTask, error) {
	task, err := scanSubagentTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return core.SubagentTask{}, core.ErrNotFound
	}
	return task, err
}

func scanSubagentTask(scanner subagentTaskScanner) (core.SubagentTask, error) {
	var task core.SubagentTask
	var status string
	var background bool
	var isolation string
	var createdAt string
	var updatedAt string
	var finishedAt sql.NullString
	var deliveredAt sql.NullString
	if err := scanner.Scan(
		&task.ID,
		&task.AgentName,
		&task.DisplayName,
		&background,
		&isolation,
		&task.ParentSessionID,
		&task.ParentRunID,
		&task.ParentToolCallID,
		&task.ChildSessionID,
		&task.ChildRunID,
		&task.Runtime,
		&task.Goal,
		&status,
		&task.Summary,
		&task.Error,
		&task.ResultMessageID,
		&deliveredAt,
		&task.DeliveredRunID,
		&createdAt,
		&updatedAt,
		&finishedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.SubagentTask{}, err
		}
		return core.SubagentTask{}, fmt.Errorf("store: scan subagent task: %w", err)
	}
	task.Mode = core.SubagentTaskModeBlocking
	if background {
		task.Mode = core.SubagentTaskModeAsync
	}
	task.Isolation = core.SubagentIsolation(isolation)
	if task.Isolation == "" {
		task.Isolation = core.SubagentIsolationShared
	}
	task.Status = core.TaskStatus(status)
	task.CreatedAt = mustParseTime(createdAt)
	task.UpdatedAt = mustParseTime(updatedAt)
	task.DeliveredAt = parseNullableTime(deliveredAt)
	task.FinishedAt = parseNullableTime(finishedAt)
	return task, nil
}

func parseNullableTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := mustParseTime(value.String)
	return &parsed
}

func normalizeSubagentTaskForStore(task core.SubagentTask) core.SubagentTask {
	task.AgentName = strings.Join(strings.Fields(task.AgentName), " ")
	if task.Mode == "" {
		task.Mode = core.SubagentTaskModeBlocking
	}
	if task.Isolation == "" {
		task.Isolation = core.SubagentIsolationShared
	}
	return task
}
```

Create `internal/store/sqlite_tasks.go`:

```go
package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MarkTasksDelivered records that runID told the tasks' sessions they finished;
// an empty runID means nothing is to be told.
func (s *SQLiteStore) MarkTasksDelivered(ctx context.Context, taskIDs []string, runID string, at time.Time) error {
	if len(taskIDs) == 0 {
		return nil
	}
	args := []any{formatTime(at), strings.TrimSpace(runID), formatTime(at)}
	placeholders := make([]string, 0, len(taskIDs))
	for _, id := range taskIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE tasks SET delivered_at = ?, delivered_run_id = ?, updated_at = ?
WHERE delivered_at IS NULL AND id IN (`+strings.Join(placeholders, ", ")+`)`, args...); err != nil {
		return fmt.Errorf("store: mark tasks delivered: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/core -run '^(TestAsyncSubagentCompletionStartsParentFollowUpRun|TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp)$'
go test ./internal/store -run '^(TestSubagentTasksMoveIntoTheTasksTable|TestMarkTasksDeliveredKeepsTheFirstDelivery)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/native_run_characterization_test.go \
  internal/core/ports.go \
  internal/core/subagents_cancel_test.go \
  internal/core/subagents_lifecycle.go \
  internal/core/subagents_persistence.go \
  internal/core/subagents_work.go \
  internal/core/types_subagent.go \
  internal/store/migrations/001_init.sql \
  internal/store/schema.go \
  internal/store/schema_tasks.go \
  internal/store/sqlite_subagents.go \
  internal/store/sqlite_tasks.go \
  internal/store/sqlite_tasks_test.go
git commit -m "feat(store): background tasks live in one tasks table

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 4: Generic tasks in the store

**Files:**
- Modify: `internal/core/ports.go`
- Modify: `internal/core/types_task.go`
- Modify: `internal/store/sqlite_tasks.go`
- Test (modify): `internal/store/sqlite_tasks_test.go`

The generic `Task` view and the store methods the shell task service and the event inbox need. `FinishTask` ends a task once (`WHERE finished_at IS NULL`) and reports whether this call ended it, so a kill and the process's own exit cannot both finish it. `ListTasks` returns newest first; `Undelivered` selects finished background tasks without `delivered_at`.

- [ ] **Step 1: Write the failing tests**

In `internal/store/sqlite_tasks_test.go`, the imports become:

```go
import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)
```

In `internal/store/sqlite_tasks_test.go`, add:

```go
func TestShellTasksFinishOnceAndBecomeEventsUntilDelivered(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t, filepath.Join(t.TempDir(), "tasks.db"))
	createTestSession(t, st, "s1")
	createTestSession(t, st, "s2")
	for _, task := range []core.Task{
		{ID: "task_a", SessionID: "s1", RunID: "r1", ParentToolCallID: "call_1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm test", WorkingDir: "/work", Background: true, PID: 42, PGID: 42, OutputPath: "/data/s1/tasks/task_a.log", StartedAt: testEpoch, UpdatedAt: testEpoch},
		{ID: "task_b", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "sleep 9", Background: true, StartedAt: testEpoch.Add(time.Second), UpdatedAt: testEpoch},
		{ID: "task_c", SessionID: "s2", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true, StartedAt: testEpoch, UpdatedAt: testEpoch},
	} {
		if err := st.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetTask(ctx, "task_a")
	if err != nil || got.RunID != "r1" || got.ParentToolCallID != "call_1" || got.PID != 42 || got.OutputPath != "/data/s1/tasks/task_a.log" || got.ExitCode != nil || !got.Background {
		t.Fatalf("task = %+v, %v", got, err)
	}

	code := 1
	finished, err := st.FinishTask(ctx, "task_a", core.TaskStatusFailed, &code, "", testEpoch.Add(time.Minute))
	if err != nil || !finished {
		t.Fatalf("finish = %v, %v", finished, err)
	}
	again, err := st.FinishTask(ctx, "task_a", core.TaskStatusCanceled, nil, "killed", testEpoch.Add(2*time.Minute))
	if err != nil || again {
		t.Fatalf("second finish = %v, %v", again, err)
	}
	if err := st.SetTaskCursor(ctx, "task_a", 128); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetTask(ctx, "task_a")
	if err != nil || got.Status != core.TaskStatusFailed || got.ExitCode == nil || *got.ExitCode != 1 || got.OutputCursor != 128 || got.FinishedAt == nil {
		t.Fatalf("finished task = %+v, %v", got, err)
	}

	running, err := st.ListTasks(ctx, core.TaskFilter{Kind: core.TaskKindShell, Statuses: []core.TaskStatus{core.TaskStatusRunning}})
	if err != nil || len(running) != 2 || running[0].ID != "task_b" || running[1].ID != "task_c" {
		t.Fatalf("running = %+v, %v", running, err)
	}
	events, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Undelivered: true})
	if err != nil || len(events) != 1 || events[0].ID != "task_a" {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if err := st.MarkTasksDelivered(ctx, []string{"task_a"}, "r2", testEpoch.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if events, err := st.ListTasks(ctx, core.TaskFilter{SessionID: "s1", Undelivered: true}); err != nil || len(events) != 0 {
		t.Fatalf("events after delivery = %+v, %v", events, err)
	}
	if _, err := st.GetTask(ctx, "task_gone"); err != core.ErrNotFound {
		t.Fatalf("missing task err = %v", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/store -run '^(TestShellTasksFinishOnceAndBecomeEventsUntilDelivered)$'
```

Expected: build fails (`CreateTask`, `core.Task` are undefined).

- [ ] **Step 3: Implement**

In `internal/core/ports.go`, replace `type TaskStore` with:

```go
// TaskStore keeps the background tasks of sessions. FinishTask ends a task
// once and reports whether this call ended it.
type TaskStore interface {
	CreateTask(ctx context.Context, task Task) error
	GetTask(ctx context.Context, taskID string) (Task, error)
	ListTasks(ctx context.Context, filter TaskFilter) ([]Task, error)
	FinishTask(ctx context.Context, taskID string, status TaskStatus, exitCode *int, errText string, at time.Time) (bool, error)
	SetTaskCursor(ctx context.Context, taskID string, cursor int64) error
	MarkTasksDelivered(ctx context.Context, taskIDs []string, runID string, at time.Time) error
}
```

In `internal/core/types_task.go`, the imports become:

```go
import "time"
```

In `internal/core/types_task.go`, replace `func taskStatusTerminal` with:

```go
func taskStatusTerminal(status TaskStatus) bool {
	return status == TaskStatusCompleted || status == TaskStatusFailed || status == TaskStatusCanceled || status == TaskStatusLost
}
```

In `internal/core/types_task.go`, replace `const block starting with TaskStatusPending` with:

```go
const (
	TaskStatusPending         TaskStatus = "pending"
	TaskStatusRunning         TaskStatus = "running"
	TaskStatusWaitingApproval TaskStatus = "waiting_approval"
	TaskStatusCompleted       TaskStatus = "completed"
	TaskStatusFailed          TaskStatus = "failed"
	TaskStatusCanceled        TaskStatus = "canceled"
	// TaskStatusLost is a shell task the daemon stopped when it restarted.
	TaskStatusLost TaskStatus = "lost"
)
```

In `internal/core/types_task.go`, add:

```go
// TaskKind says what a background task runs.
type TaskKind string
```

In `internal/core/types_task.go`, add:

```go
const (
	TaskKindShell    TaskKind = "shell"
	TaskKindSubagent TaskKind = "subagent"
)
```

In `internal/core/types_task.go`, add:

```go
// Task is work a session runs besides its runs: a shell command or a subagent.
// Command is the shell command or the subagent's goal. A finished background
// task whose DeliveredAt is nil is an event its session has not seen yet.
type Task struct {
	ID               string     `json:"id"`
	SessionID        string     `json:"session_id"`
	RunID            string     `json:"run_id,omitempty"`
	ParentToolCallID string     `json:"parent_tool_call_id,omitempty"`
	Kind             TaskKind   `json:"kind"`
	Status           TaskStatus `json:"status"`
	Command          string     `json:"command"`
	Description      string     `json:"description,omitempty"`
	WorkingDir       string     `json:"working_dir,omitempty"`
	Background       bool       `json:"background"`
	AgentName        string     `json:"agent_name,omitempty"`
	PID              int        `json:"pid,omitempty"`
	PGID             int        `json:"pgid,omitempty"`
	OutputPath       string     `json:"output_path,omitempty"`
	ExitCode         *int       `json:"exit_code,omitempty"`
	OutputCursor     int64      `json:"output_cursor,omitempty"`
	ChildSessionID   string     `json:"child_session_id,omitempty"`
	ChildRunID       string     `json:"child_run_id,omitempty"`
	Summary          string     `json:"summary,omitempty"`
	Error            string     `json:"error,omitempty"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	DeliveredRunID   string     `json:"delivered_run_id,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}
```

In `internal/core/types_task.go`, add:

```go
// TaskFilter selects tasks; Undelivered keeps finished background tasks their
// session has not seen.
type TaskFilter struct {
	SessionID   string
	Kind        TaskKind
	Statuses    []TaskStatus
	Undelivered bool
	Limit       int
}
```

In `internal/store/sqlite_tasks.go`, the imports become:

```go
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)
```

In `internal/store/sqlite_tasks.go`, add:

```go
const taskColumns = `id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal, description, working_dir,
background, agent_name, pid, pgid, output_path, exit_code, output_cursor, child_session_id, child_run_id, summary, error,
delivered_at, delivered_run_id, started_at, updated_at, finished_at`
```

In `internal/store/sqlite_tasks.go`, add:

```go
func (s *SQLiteStore) CreateTask(ctx context.Context, task core.Task) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks(id, session_id, run_id, parent_tool_call_id, kind, status, command_or_goal, description, working_dir,
    background, agent_name, pid, pgid, output_path, exit_code, output_cursor, child_session_id, child_run_id,
    summary, error, started_at, updated_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.SessionID, task.RunID, task.ParentToolCallID, string(task.Kind), string(task.Status), task.Command,
		task.Description, task.WorkingDir, task.Background, task.AgentName, task.PID, task.PGID, task.OutputPath,
		task.ExitCode, task.OutputCursor, task.ChildSessionID, task.ChildRunID, task.Summary, task.Error,
		formatTime(task.StartedAt), formatTime(task.UpdatedAt), nullableTime(task.FinishedAt),
	)
	if err != nil {
		return fmt.Errorf("store: create task: %w", err)
	}
	return nil
}
```

In `internal/store/sqlite_tasks.go`, add:

```go
func (s *SQLiteStore) GetTask(ctx context.Context, taskID string) (core.Task, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, strings.TrimSpace(taskID)))
	if errors.Is(err, sql.ErrNoRows) {
		return core.Task{}, core.ErrNotFound
	}
	return task, err
}
```

In `internal/store/sqlite_tasks.go`, add:

```go
// ListTasks lists the tasks the filter selects, newest first.
func (s *SQLiteStore) ListTasks(ctx context.Context, filter core.TaskFilter) ([]core.Task, error) {
	where := []string{"1 = 1"}
	var args []any
	if id := strings.TrimSpace(filter.SessionID); id != "" {
		where = append(where, "session_id = ?")
		args = append(args, id)
	}
	if filter.Kind != "" {
		where = append(where, "kind = ?")
		args = append(args, string(filter.Kind))
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			placeholders = append(placeholders, "?")
			args = append(args, string(status))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	}
	if filter.Undelivered {
		where = append(where, "background = 1 AND finished_at IS NOT NULL AND delivered_at IS NULL")
	}
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE ` + strings.Join(where, " AND ") + ` ORDER BY started_at DESC, id DESC`
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var tasks []core.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate tasks: %w", err)
	}
	return tasks, nil
}
```

In `internal/store/sqlite_tasks.go`, add:

```go
func (s *SQLiteStore) FinishTask(ctx context.Context, taskID string, status core.TaskStatus, exitCode *int, errText string, at time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
UPDATE tasks SET status = ?, exit_code = ?, error = ?, finished_at = ?, updated_at = ?
WHERE id = ? AND finished_at IS NULL`, string(status), exitCode, errText, formatTime(at), formatTime(at), strings.TrimSpace(taskID))
	if err != nil {
		return false, fmt.Errorf("store: finish task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: finish task rows: %w", err)
	}
	return rows > 0, nil
}
```

In `internal/store/sqlite_tasks.go`, add:

```go
func (s *SQLiteStore) SetTaskCursor(ctx context.Context, taskID string, cursor int64) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET output_cursor = ? WHERE id = ?`, cursor, strings.TrimSpace(taskID)); err != nil {
		return fmt.Errorf("store: set task cursor: %w", err)
	}
	return nil
}
```

In `internal/store/sqlite_tasks.go`, add:

```go
type taskScanner interface {
	Scan(dest ...any) error
}
```

In `internal/store/sqlite_tasks.go`, add:

```go
func scanTask(scanner taskScanner) (core.Task, error) {
	var task core.Task
	var kind, status, startedAt, updatedAt string
	var exitCode sql.NullInt64
	var deliveredAt, finishedAt sql.NullString
	if err := scanner.Scan(&task.ID, &task.SessionID, &task.RunID, &task.ParentToolCallID, &kind, &status, &task.Command,
		&task.Description, &task.WorkingDir, &task.Background, &task.AgentName, &task.PID, &task.PGID, &task.OutputPath,
		&exitCode, &task.OutputCursor, &task.ChildSessionID, &task.ChildRunID, &task.Summary, &task.Error,
		&deliveredAt, &task.DeliveredRunID, &startedAt, &updatedAt, &finishedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Task{}, err
		}
		return core.Task{}, fmt.Errorf("store: scan task: %w", err)
	}
	task.Kind = core.TaskKind(kind)
	task.Status = core.TaskStatus(status)
	if exitCode.Valid {
		code := int(exitCode.Int64)
		task.ExitCode = &code
	}
	task.StartedAt = mustParseTime(startedAt)
	task.UpdatedAt = mustParseTime(updatedAt)
	task.DeliveredAt = parseNullableTime(deliveredAt)
	task.FinishedAt = parseNullableTime(finishedAt)
	return task, nil
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/store -run '^(TestShellTasksFinishOnceAndBecomeEventsUntilDelivered)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/ports.go \
  internal/core/types_task.go \
  internal/store/sqlite_tasks.go \
  internal/store/sqlite_tasks_test.go
git commit -m "feat(store): shell tasks, their events and output cursors

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 5: Core shell task service

**Files:**
- Modify: `internal/core/core.go`
- Modify: `internal/core/events.go`
- Modify: `internal/core/sessions.go`
- Create: `internal/core/shell_tasks.go`
- Create: `internal/tools/tasks.go`
- Test (create): `internal/core/shell_tasks_test.go`

The core half of the shell tools: `RunCommand`, `ReadTaskOutput` and `StopTask` implement the `tools.ShellTasks` port (declared here in `internal/tools/tasks.go`; the executors follow in Task 6). A command is registered in `liveTasks` **before** its row is created, so `RecoverTasks` never mistakes a new task for a leftover. `watchShellTask` waits for the process (and kills it at an adopted command's deadline), then `finishTask` records the end once and publishes `task.updated`. `StopTask` (the model's `task_kill`) marks the task delivered before it cancels it; `cancelTask` finishes a shell task `canceled` before killing it, so the watcher's own finish is a no-op. `DeleteSession` stops the session's tasks first.

- [ ] **Step 1: Write the failing tests**

Create `internal/core/shell_tasks_test.go`:

```go
package core_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/shelltask"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func newTaskCore(t *testing.T) (*core.Core, *store.SQLiteStore, core.Session, string) {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	files := t.TempDir()
	app.WithSessionFiles(files)
	session := permissionSession(t, db, "session_tasks", t.TempDir(), core.PermissionModeDefault, "")
	return app, db, session, files
}

func taskCall(session core.Session) tools.Call {
	return tools.Call{SessionID: session.ID, RunID: "run_tasks", ToolCallID: "call_bash", WorkingDir: session.WorkingDir}
}

func foreground(command string) tools.Command {
	return tools.Command{Command: command, Timeout: time.Minute, AutoBackground: 30 * time.Second, OutputLimit: 1000}
}

func waitTaskStatus(t *testing.T, db *store.SQLiteStore, id string, status core.TaskStatus) core.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		task, err := db.GetTask(context.Background(), id)
		if err == nil && task.Status == status {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s = %+v, %v; want %s", id, task, err, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestForegroundCommandReturnsItsOutputAndLeavesNoFile(t *testing.T) {
	t.Parallel()
	app, _, session, files := newTaskCore(t)

	result, err := app.RunCommand(context.Background(), taskCall(session), foreground("echo hello; exit 2"))

	if err != nil || result.TaskID != "" || result.Output != "hello\n" || result.ExitCode != 2 || result.OutputPath != "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	entries, _ := os.ReadDir(filepath.Join(files, session.ID, "tasks"))
	if len(entries) != 0 {
		t.Fatalf("left files %v", entries)
	}
}

func TestForegroundOutputPastTheLimitKeepsItsFile(t *testing.T) {
	t.Parallel()
	app, _, session, _ := newTaskCore(t)
	command := foreground("seq 1 1000")
	command.OutputLimit = 10

	result, err := app.RunCommand(context.Background(), taskCall(session), command)

	if err != nil || result.Output != "1\n2\n3\n4\n5\n" || result.OutputPath == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if tail, err := shelltask.Tail(result.OutputPath, 5); err != nil || tail != "1000\n" {
		t.Fatalf("kept file tail = %q, %v", tail, err)
	}
}

func TestForegroundCommandTimesOut(t *testing.T) {
	t.Parallel()
	app, _, session, _ := newTaskCore(t)
	command := foreground("echo started; sleep 30")
	command.Timeout, command.AutoBackground = 300*time.Millisecond, 0

	result, err := app.RunCommand(context.Background(), taskCall(session), command)

	if err != nil || !result.TimedOut || result.Output != "started\n" || result.ExitCode != -1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

func TestSlowForegroundCommandMovesToTheBackground(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("echo first; sleep 1; echo second")
	command.AutoBackground = 200 * time.Millisecond

	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil || result.TaskID == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	task := waitTaskStatus(t, db, result.TaskID, core.TaskStatusCompleted)
	if task.Kind != core.TaskKindShell || task.RunID != "run_tasks" || task.ParentToolCallID != "call_bash" || task.ExitCode == nil || *task.ExitCode != 0 || task.DeliveredAt != nil {
		t.Fatalf("task = %+v", task)
	}

	out, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: task.ID, Limit: 100})
	if err != nil || out.Text != "first\nsecond\n" || out.Status != "completed" {
		t.Fatalf("output = %+v, %v", out, err)
	}
	again, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: task.ID, Limit: 100})
	if err != nil || again.Text != "" {
		t.Fatalf("second read = %+v, %v", again, err)
	}
}

func TestBackgroundTaskOutputWaitsAndFilters(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("echo ok 1; echo FAIL 2; sleep 0.3; echo FAIL 3; exit 1")
	command.Background = true

	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil || result.TaskID == "" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	out, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: result.TaskID, Wait: 10 * time.Second, Filter: "^FAIL", Limit: 100})
	if err != nil || out.Text != "FAIL 2\nFAIL 3" || out.Status != "failed" || out.ExitCode == nil || *out.ExitCode != 1 {
		t.Fatalf("output = %+v, %v", out, err)
	}
	if task, _ := db.GetTask(context.Background(), result.TaskID); task.OutputCursor == 0 {
		t.Fatalf("cursor not saved: %+v", task)
	}
	if _, err := app.ReadTaskOutput(context.Background(), taskCall(session), tools.TaskRead{TaskID: result.TaskID, Filter: "("}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("bad filter err = %v", err)
	}
	other := taskCall(session)
	other.SessionID = "session_other"
	if _, err := app.ReadTaskOutput(context.Background(), other, tools.TaskRead{TaskID: result.TaskID}); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("another session read it: %v", err)
	}
}

func TestStoppedTaskIsCanceledWithoutAnEvent(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("sleep 30")
	command.Background = true
	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}

	info, err := app.StopTask(context.Background(), taskCall(session), result.TaskID)
	if err != nil || info.Status != "canceled" {
		t.Fatalf("stop = %+v, %v", info, err)
	}
	task := waitTaskStatus(t, db, result.TaskID, core.TaskStatusCanceled)
	if task.DeliveredAt == nil || task.Error != "stopped with task_kill" {
		t.Fatalf("task = %+v", task)
	}
	waitProcessGone(t, task.PID)
}

func waitProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("process %d still runs", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRecoverTasksKillsLeftoversAndMarksThemLost(t *testing.T) {
	t.Parallel()
	app, db, session, files := newTaskCore(t)
	out, err := shelltask.CreateOutput(filepath.Join(files, session.ID, "tasks", "task_left.log"))
	if err != nil {
		t.Fatal(err)
	}
	process, err := shelltask.Start("sleep 60", session.WorkingDir, out, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Kill() })
	left := core.Task{ID: "task_left", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "sleep 60", Background: true,
		PID: process.PID(), PGID: process.PID(), OutputPath: out.Path(), StartedAt: process.StartedAt(), UpdatedAt: process.StartedAt()}
	if err := db.CreateTask(context.Background(), left); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverTasks(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case <-process.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the leftover process group still runs")
	}
	task := waitTaskStatus(t, db, "task_left", core.TaskStatusLost)
	if task.DeliveredAt != nil || !strings.Contains(task.Error, "daemon restarted") {
		t.Fatalf("task = %+v", task)
	}
}

func TestDeletingASessionStopsItsTasks(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("sleep 30")
	command.Background = true
	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.GetTask(context.Background(), result.TaskID)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.DeleteSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
	waitProcessGone(t, task.PID)
	if _, err := os.Stat(task.OutputPath); !os.IsNotExist(err) {
		t.Fatalf("output file after delete: %v", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/core -run '^(TestForegroundCommandReturnsItsOutputAndLeavesNoFile|TestForegroundOutputPastTheLimitKeepsItsFile|TestForegroundCommandTimesOut|TestSlowForegroundCommandMovesToTheBackground|TestBackgroundTaskOutputWaitsAndFilters|TestStoppedTaskIsCanceledWithoutAnEvent|TestRecoverTasksKillsLeftoversAndMarksThemLost|TestDeletingASessionStopsItsTasks)$'
```

Expected: build fails (`RunCommand`, `tools.Command` are undefined).

- [ ] **Step 3: Implement**

In `internal/core/core.go`, replace `type Core` with:

```go
type Core struct {
	mu              sync.RWMutex
	store           Store
	workStore       work.Store
	runStarter      RunStarter
	llms            SessionLLMRegistry
	assistant       AssistantProfile
	attachments     agentcontext.AttachmentReader
	externalAgents  *externalagents.Registry
	externalStore   externalagents.AttachmentStore
	activeRuns      map[string]*activeRun
	scheduledRuns   map[string]time.Time
	sessionGates    map[string]*sync.Mutex
	tools           ToolExecutor
	skillsContext   SkillsPromptContextProvider
	runtimeStatus   RuntimeStatusContextProvider
	events          *eventBus
	now             func() time.Time
	newID           func(prefix string) string
	historyLimit    int
	lifetime        context.Context
	budgets         RunBudgets
	sessionFiles    string
	compactProvider string
	compactModel    string
	windowCap       int
	// modelSlots bounds the model requests of all native runs at once.
	modelSlots *toolsched.Semaphore
	// toolLocks serialises tool calls sharing a concurrency key across runs.
	toolLocks *toolsched.Locks
	// compactUnavailable is set while the compact model cannot be resolved,
	// so the failure is logged once.
	compactUnavailable atomic.Bool

	// badBoundaries holds the IDs of unreadable boundaries already logged.
	badBoundaries sync.Map
	// liveTasks are the shell tasks this daemon started that still run.
	tasksMu   sync.Mutex
	liveTasks map[string]*liveTask
}
```

In `internal/core/core.go`, replace `func New` with:

```go
func New(store Store) *Core {
	return &Core{
		store:         store,
		activeRuns:    map[string]*activeRun{},
		scheduledRuns: map[string]time.Time{},
		sessionGates:  map[string]*sync.Mutex{},
		liveTasks:     map[string]*liveTask{},
		events:        newEventBus(),
		now:           time.Now,
		newID:         defaultID,
		historyLimit:  50,
		lifetime:      context.Background(),
		budgets:       DefaultRunBudgets(),
		modelSlots:    toolsched.NewSemaphore(DefaultModelConcurrency),
		toolLocks:     toolsched.NewLocks(),
	}
}
```

In `internal/core/events.go`, replace `const block starting with EventRunUpdated` with:

```go
const (
	EventRunUpdated      EventType = "run.updated"
	EventMessageCreated  EventType = "message.created"
	EventMessageUpdated  EventType = "message.updated"
	EventTodoUpdated     EventType = "todo.updated"
	EventToolUpdated     EventType = "tool.updated"
	EventApprovalRequest EventType = "approval.requested"
	EventApprovalResult  EventType = "approval.resolved"
	EventFileVersioned   EventType = "file.versioned"
	EventSubagentUpdated EventType = "subagent.updated"
	EventInputUpdated    EventType = "input.updated"
	EventTaskUpdated     EventType = "task.updated"
)
```

In `internal/core/sessions.go`, replace `func (Core) DeleteSession` with:

```go
func (c *Core) DeleteSession(ctx context.Context, sessionID string) error {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if err := c.stopSessionTasks(ctx, sessionID); err != nil {
		return err
	}
	if err := c.store.DeleteSession(ctx, sessionID); err != nil {
		return err
	}
	if err := c.removeSessionFiles(sessionID); err != nil {
		log.Printf("core: remove files of deleted session %q: %v", sessionID, err)
	}
	return nil
}
```

Create `internal/core/shell_tasks.go`:

```go
package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/shelltask"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// taskOutputDir holds a session's background task output files.
const taskOutputDir = "tasks"

// foregroundWaitDelay is how long a finished foreground command waits for
// processes it left behind to close their output.
const foregroundWaitDelay = 2 * time.Second

// RunCommand runs a bash call's command. A background command, or a foreground
// one still running after its AutoBackground delay, goes on as a task of the
// call's session; a foreground one is killed at its timeout or when ctx stops.
func (c *Core) RunCommand(ctx context.Context, call tools.Call, command tools.Command) (tools.CommandResult, error) {
	id := c.newID("task")
	path, err := c.taskOutputPath(call.SessionID, id)
	if err != nil {
		return tools.CommandResult{}, err
	}
	out, err := shelltask.CreateOutput(path)
	if err != nil {
		return tools.CommandResult{}, err
	}
	waitDelay := foregroundWaitDelay
	if command.Background {
		waitDelay = 0
	}
	process, err := shelltask.Start(command.Command, command.WorkingDir, out, waitDelay)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return tools.CommandResult{}, err
	}
	if command.Background {
		return c.adoptCommand(ctx, call, command, id, process, time.Time{})
	}
	timeout := time.NewTimer(command.Timeout)
	defer timeout.Stop()
	var background <-chan time.Time
	if command.AutoBackground > 0 && command.AutoBackground < command.Timeout {
		timer := time.NewTimer(command.AutoBackground)
		defer timer.Stop()
		background = timer.C
	}
	select {
	case <-process.Done():
		return c.finishedCommand(process, command, false)
	case <-timeout.C:
		_ = process.Kill()
		<-process.Done()
		return c.finishedCommand(process, command, true)
	case <-background:
		return c.adoptCommand(ctx, call, command, id, process, process.StartedAt().Add(command.Timeout))
	case <-ctx.Done():
		_ = process.Kill()
		<-process.Done()
		_ = os.Remove(path)
		return tools.CommandResult{}, ctx.Err()
	}
}

// finishedCommand reads a foreground command's output; the file is kept only
// when the output is longer than the call may return.
func (c *Core) finishedCommand(process *shelltask.Process, command tools.Command, timedOut bool) (tools.CommandResult, error) {
	path := process.Output().Path()
	result := tools.CommandResult{ExitCode: process.ExitCode(), TimedOut: timedOut, StartedAt: process.StartedAt(), EndedAt: c.now()}
	chunk, err := shelltask.Read(path, 0, command.OutputLimit)
	if err != nil {
		return tools.CommandResult{}, err
	}
	result.Output = chunk.Text
	if chunk.More {
		result.OutputPath = path
		return result, nil
	}
	_ = os.Remove(path)
	return result, nil
}

// adoptCommand records a running command as a background task and watches it
// until it ends or, with a deadline, is killed then.
func (c *Core) adoptCommand(ctx context.Context, call tools.Call, command tools.Command, id string, process *shelltask.Process, deadline time.Time) (tools.CommandResult, error) {
	now := c.now().UTC()
	task := Task{
		ID:               id,
		SessionID:        call.SessionID,
		RunID:            call.RunID,
		ParentToolCallID: call.ToolCallID,
		Kind:             TaskKindShell,
		Status:           TaskStatusRunning,
		Command:          command.Command,
		Description:      command.Description,
		WorkingDir:       command.WorkingDir,
		Background:       true,
		PID:              process.PID(),
		PGID:             process.PID(),
		OutputPath:       process.Output().Path(),
		StartedAt:        process.StartedAt().UTC(),
		UpdatedAt:        now,
	}
	live := &liveTask{process: process, recorded: make(chan struct{})}
	c.tasksMu.Lock()
	c.liveTasks[id] = live
	c.tasksMu.Unlock()
	if err := c.store.CreateTask(context.WithoutCancel(ctx), task); err != nil {
		c.forgetLiveTask(id)
		_ = process.Kill()
		return tools.CommandResult{}, err
	}
	c.publishTaskUpdated(task)
	safego.Go("core.watchShellTask", func() { c.watchShellTask(task, live, deadline) })
	return tools.CommandResult{TaskID: id, StartedAt: process.StartedAt(), EndedAt: now}, nil
}

// liveTask is a shell task of this daemon; recorded is closed once its end is stored.
type liveTask struct {
	process  *shelltask.Process
	recorded chan struct{}
}

func (c *Core) watchShellTask(task Task, live *liveTask, deadline time.Time) {
	defer c.forgetLiveTask(task.ID)
	defer close(live.recorded)
	process := live.process
	errText := ""
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-process.Done():
		case <-timer.C:
			_ = process.Kill()
			errText = fmt.Sprintf("killed when its timeout of %s ran out", deadline.Sub(task.StartedAt).Round(time.Second))
		}
	}
	code := process.ExitCode()
	status := TaskStatusCompleted
	if code != 0 {
		status = TaskStatusFailed
	}
	if err := c.finishTask(context.Background(), task.ID, status, &code, errText); err != nil {
		log.Printf("core: finish task %q: %v", task.ID, err)
	}
}

// finishTask ends a task unless it already ended and tells its session's
// clients; the finished task becomes an event for the session.
func (c *Core) finishTask(ctx context.Context, taskID string, status TaskStatus, exitCode *int, errText string) error {
	finished, err := c.store.FinishTask(ctx, taskID, status, exitCode, errText, c.now().UTC())
	if err != nil || !finished {
		return err
	}
	task, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	c.publishTaskUpdated(task)
	return nil
}

func (c *Core) forgetLiveTask(id string) {
	c.tasksMu.Lock()
	delete(c.liveTasks, id)
	c.tasksMu.Unlock()
}

func (c *Core) liveTask(id string) (*liveTask, bool) {
	c.tasksMu.Lock()
	defer c.tasksMu.Unlock()
	live, ok := c.liveTasks[id]
	return live, ok
}

// ReadTaskOutput returns a task's output since the call's session last read it;
// it waits up to read.Wait for a running task to finish.
func (c *Core) ReadTaskOutput(ctx context.Context, call tools.Call, read tools.TaskRead) (tools.TaskOutput, error) {
	task, err := c.sessionTask(ctx, call.SessionID, read.TaskID)
	if err != nil {
		return tools.TaskOutput{}, err
	}
	var filter *regexp.Regexp
	if strings.TrimSpace(read.Filter) != "" {
		if filter, err = regexp.Compile(read.Filter); err != nil {
			return tools.TaskOutput{}, fmt.Errorf("%w: filter: %v", ErrInvalidInput, err)
		}
	}
	if live, ok := c.liveTask(task.ID); ok && read.Wait > 0 {
		timer := time.NewTimer(read.Wait)
		select {
		case <-live.recorded:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
		if err := ctx.Err(); err != nil {
			return tools.TaskOutput{}, err
		}
		if task, err = c.store.GetTask(ctx, task.ID); err != nil {
			return tools.TaskOutput{}, err
		}
	}
	out := tools.TaskOutput{TaskInfo: taskInfo(task)}
	if task.Kind != TaskKindShell {
		out.Text = firstNonEmpty(task.Summary, task.Error)
		return out, nil
	}
	chunk, err := shelltask.Read(task.OutputPath, task.OutputCursor, read.Limit)
	if err != nil {
		return tools.TaskOutput{}, err
	}
	if err := c.store.SetTaskCursor(ctx, task.ID, chunk.Next); err != nil {
		return tools.TaskOutput{}, err
	}
	out.Text, out.Skipped, out.More = chunk.Text, chunk.Skipped, chunk.More
	if filter != nil {
		out.Text = matchingLines(out.Text, filter)
	}
	return out, nil
}

func matchingLines(text string, filter *regexp.Regexp) string {
	var kept []string
	for line := range strings.SplitSeq(text, "\n") {
		if filter.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// StopTask stops a task of the call's session for the model, which then needs
// no event about it.
func (c *Core) StopTask(ctx context.Context, call tools.Call, taskID string) (tools.TaskInfo, error) {
	task, err := c.sessionTask(ctx, call.SessionID, taskID)
	if err != nil {
		return tools.TaskInfo{}, err
	}
	if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, call.RunID, c.now().UTC()); err != nil {
		return tools.TaskInfo{}, err
	}
	task, err = c.cancelTask(ctx, task, "stopped with task_kill")
	if err != nil {
		return tools.TaskInfo{}, err
	}
	return taskInfo(task), nil
}

// cancelTask ends a running task: a shell task's process group is killed, a
// subagent's run is canceled. A finished task is returned as it is.
func (c *Core) cancelTask(ctx context.Context, task Task, reason string) (Task, error) {
	if taskStatusTerminal(task.Status) {
		return task, nil
	}
	if task.Kind == TaskKindSubagent {
		if _, err := c.CancelRun(ctx, task.ChildRunID); err != nil {
			return Task{}, err
		}
		return c.store.GetTask(ctx, task.ID)
	}
	if err := c.finishTask(ctx, task.ID, TaskStatusCanceled, nil, reason); err != nil {
		return Task{}, err
	}
	if live, ok := c.liveTask(task.ID); ok {
		if err := live.process.Kill(); err != nil {
			return Task{}, err
		}
	}
	return c.store.GetTask(ctx, task.ID)
}

// sessionTask is the task with the ID if it belongs to the session.
func (c *Core) sessionTask(ctx context.Context, sessionID string, taskID string) (Task, error) {
	task, err := c.store.GetTask(ctx, normalizeText(taskID))
	if errors.Is(err, ErrNotFound) || err == nil && task.SessionID != sessionID {
		return Task{}, fmt.Errorf("%w: no task %q in this session", ErrInvalidInput, taskID)
	}
	return task, err
}

// RecoverTasks runs at daemon start: shell tasks left running by the previous
// daemon have their process group killed and are marked lost.
func (c *Core) RecoverTasks(ctx context.Context) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if _, live := c.liveTask(task.ID); live {
			continue
		}
		if err := shelltask.KillLeftover(task.PID, task.PGID, task.StartedAt); err != nil {
			log.Printf("core: kill leftover task %q: %v", task.ID, err)
		}
		if err := c.finishTask(ctx, task.ID, TaskStatusLost, nil, "the daemon restarted while it ran and stopped it"); err != nil {
			return err
		}
	}
	return nil
}

// stopSessionTasks kills the shell tasks a session runs, before it is deleted.
func (c *Core) stopSessionTasks(ctx context.Context, sessionID string) error {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Kind: TaskKindShell, Statuses: []TaskStatus{TaskStatusRunning}})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if _, err := c.cancelTask(ctx, task, "its session was deleted"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Core) taskOutputPath(sessionID string, taskID string) (string, error) {
	dir, err := c.sessionDir(sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, taskOutputDir, taskID+".log"), nil
}

func (c *Core) publishTaskUpdated(task Task) {
	c.publishEvent(Event{Type: EventTaskUpdated, SessionID: task.SessionID, RunID: task.RunID, Payload: task})
}

func taskInfo(task Task) tools.TaskInfo {
	return tools.TaskInfo{
		TaskID:      task.ID,
		Kind:        string(task.Kind),
		Command:     task.Command,
		Description: task.Description,
		Status:      string(task.Status),
		ExitCode:    task.ExitCode,
		OutputPath:  task.OutputPath,
	}
}
```

Create `internal/tools/tasks.go`:

```go
package tools

import (
	"context"
	"time"
)

// ShellTasks runs the commands of bash calls; a command that outlives its call
// becomes a background task of the call's session.
type ShellTasks interface {
	RunCommand(ctx context.Context, call Call, command Command) (CommandResult, error)
	ReadTaskOutput(ctx context.Context, call Call, read TaskRead) (TaskOutput, error)
	StopTask(ctx context.Context, call Call, taskID string) (TaskInfo, error)
}

// Command is a shell command to run for a call. Background starts it as a
// background task at once; otherwise Timeout kills it and, when shorter than
// Timeout, AutoBackground moves it to the background while it still runs.
// OutputLimit caps the output a finished command returns.
type Command struct {
	Command        string
	Description    string
	WorkingDir     string
	Background     bool
	Timeout        time.Duration
	AutoBackground time.Duration
	OutputLimit    int
}

// CommandResult is how a command ended, or the task it went on as (TaskID).
// OutputPath names the file holding the whole output when Output was cut.
type CommandResult struct {
	TaskID     string
	Output     string
	OutputPath string
	ExitCode   int
	TimedOut   bool
	StartedAt  time.Time
	EndedAt    time.Time
}

// TaskRead asks for a task's output since the last read, waiting up to Wait
// for the task to finish; Filter keeps the lines matching a regular expression.
type TaskRead struct {
	TaskID string
	Wait   time.Duration
	Filter string
	Limit  int
}

// TaskInfo describes a background task to the model and clients.
type TaskInfo struct {
	TaskID      string `json:"task_id"`
	Kind        string `json:"kind"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	OutputPath  string `json:"output_path,omitempty"`
}

// TaskOutput is a task's output since the last read. Skipped counts bytes
// dropped from the file before Text; More is set when more output is waiting.
type TaskOutput struct {
	TaskInfo
	Text    string
	Skipped int64
	More    bool
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/core -run '^(TestForegroundCommandReturnsItsOutputAndLeavesNoFile|TestForegroundOutputPastTheLimitKeepsItsFile|TestForegroundCommandTimesOut|TestSlowForegroundCommandMovesToTheBackground|TestBackgroundTaskOutputWaitsAndFilters|TestStoppedTaskIsCanceledWithoutAnEvent|TestRecoverTasksKillsLeftoversAndMarksThemLost|TestDeletingASessionStopsItsTasks)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/core/core.go \
  internal/core/events.go \
  internal/core/sessions.go \
  internal/core/shell_tasks.go \
  internal/core/shell_tasks_test.go \
  internal/tools/tasks.go
git commit -m "feat(core): shell commands that outlive their call become background tasks

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 6: Bash timeouts and auto-background; `task_output` and `task_kill`

**Files:**
- Modify: `clients/terminal/ui/surface/chat/bash.go`
- Modify: `clients/terminal/ui/surface/chat/tools.go`
- Modify: `internal/daemoncmd/run.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/tools/definitions.go`
- Modify: `internal/tools/schema.go`
- Modify: `internal/tools/shell.go`
- Modify: `internal/tools/types.go`
- Test (modify): `internal/providers/ai/gemini/schema_test.go`
- Test (modify): `internal/tools/concurrency_key_test.go`
- Test (modify): `internal/tools/permission_subject_test.go`
- Test (modify): `internal/tools/shell_browser_guard_test.go`
- Test (create): `internal/tools/shell_test.go`

The executors become thin: they parse and validate arguments, ask for approval exactly as before, call the port and format the result. Their specs stay in `coreDefinitions` without `NewExecutor`, so `NewCoreCodingRegistry` no longer builds them; `NewShellExecutors(tasks)` does, and the daemon registers them with the core (tests pass `nil` when they only need specs or subjects). `RecoverTasks` is wired into the daemon here, before the workflow worker starts. The old job manager, `BackgroundJob`, `Result.Background` and the MCP server's `background` field go.

- [ ] **Step 1: Write the failing tests**

In `internal/providers/ai/gemini/schema_test.go`, replace `func registeredToolDefinitions` with:

```go
// registeredToolDefinitions mirrors the daemon's registry, minus modules that import this package.
func registeredToolDefinitions(t *testing.T) []providers.ToolDefinition {
	t.Helper()
	app := core.New(nil)
	web := webtools.NewWebService(nil, nil)
	registry := tools.NewCoreCodingRegistry(
		automation.NewReminderTool(nil),
		automation.NewScheduledAITaskTool(nil),
		deliverymodule.NewSendFileTool(nil, nil),
		webtools.NewWebFetchExecutorWithService(web),
		webtools.NewWebSearchExecutorWithService(web),
	)
	storage, err := storagemodule.New(storagemodule.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, executors := range [][]tools.Executor{
		webtools.NewWebResearchExecutorsWithService(web),
		tools.NewShellExecutors(app),
		core.TodoToolExecutors(app),
		core.MemoryToolExecutors(app),
		core.SubagentToolExecutors(app),
		tools.NewOSMGeoExecutors(tools.NewOSMService(tools.OSMConfig{})),
		skills.ToolExecutors(nil),
	} {
		if err := registry.Register(executors...); err != nil {
			t.Fatal(err)
		}
	}
	if err := storage.RegisterTools(registry); err != nil {
		t.Fatal(err)
	}
	var definitions []providers.ToolDefinition
	for _, spec := range registry.List() {
		definitions = append(definitions, providers.ToolDefinition{Name: spec.ID, Description: spec.Description, InputSchema: spec.InputJSONSchema})
	}
	return definitions
}
```

In `internal/tools/concurrency_key_test.go`, replace `func TestRegistryConcurrencyKeyPrefersTheExecutorsOwnKey` with:

```go
func TestRegistryConcurrencyKeyPrefersTheExecutorsOwnKey(t *testing.T) {
	own := keyedExecutor{spec: Spec{ID: "own", Name: "Own", Description: "own key", Risk: RiskSafe, Namespace: "test", Effect: EffectMutation, Category: CategoryAutomation, Profiles: []Profile{ProfileCoding}, OutputKind: OutputText, InputJSONSchema: []byte(`{}`)}}
	registry := NewRegistry(append(NewShellExecutors(nil), own)...)
	if err := registry.Err(); err != nil {
		t.Fatal(err)
	}
	call := Call{WorkingDir: "/work"}

	if got := registry.ConcurrencyKey("bash", call); got != "dir:/work" {
		t.Fatalf("bash key = %q", got)
	}
	if got := registry.ConcurrencyKey("own", call); got != "own:/work" {
		t.Fatalf("own key = %q", got)
	}
	if got := registry.ConcurrencyKey("missing", call); got != "" {
		t.Fatalf("unknown tool key = %q", got)
	}
}
```

In `internal/tools/permission_subject_test.go`, replace `func TestPermissionSubjectsNameResolvedPathsAndCommands` with:

```go
func TestPermissionSubjectsNameResolvedPathsAndCommands(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	registry := NewCoreCodingRegistry(NewShellExecutors(nil)...)
	for _, tc := range []struct {
		tool string
		args string
		want permission.Subject
	}{
		{"bash", `{"command":"go test ./..."}`, permission.Subject{Kind: permission.KindCommand, Value: "go test ./..."}},
		{"read", `{"file_path":"link/a.go"}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(real, "a.go")}},
		{"write", `{"file_path":"` + filepath.Join(root, "new.txt") + `","content":"x"}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(root, "new.txt")}},
		{"edit", `{"file_path":"link/b.go","old_string":"a","new_string":"b"}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(real, "b.go")}},
		{"multiedit", `{"file_path":"c.go","edits":[]}`, permission.Subject{Kind: permission.KindFile, Value: filepath.Join(root, "c.go")}},
		{"glob", `{"pattern":"*.go"}`, permission.Subject{Kind: permission.KindDirectory, Value: root}},
		{"grep", `{"pattern":"x","path":"link"}`, permission.Subject{Kind: permission.KindDirectory, Value: real}},
		{"ls", `{}`, permission.Subject{Kind: permission.KindDirectory, Value: root}},
		{"read", `{}`, permission.Subject{}},
		{"bash", `not json`, permission.Subject{}},
		{"task_output", `{"id":"task_1"}`, permission.Subject{}},
	} {
		got := registry.Subject(tc.tool, Call{WorkingDir: root, Args: json.RawMessage(tc.args)})
		if got != tc.want {
			t.Errorf("%s %s: subject = %+v, want %+v", tc.tool, tc.args, got, tc.want)
		}
	}
}
```

In `internal/tools/shell_browser_guard_test.go`, replace `func TestBashExecutorBlocksManagedBrowserInstallEvenWhenApproved` with:

```go
func TestBashExecutorBlocksManagedBrowserInstallEvenWhenApproved(t *testing.T) {
	args, _ := json.Marshal(BashParams{
		Command: "playwright-mcp install-browser chrome-for-testing",
	})
	result, err := NewShellExecutors(nil)[0].Execute(context.Background(), Call{
		Args:     args,
		Approved: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("IsError = false, result = %#v", result)
	}
	if !strings.Contains(result.Content, "Managed Browser setup is only available through Modules") {
		t.Fatalf("content = %q, want managed Browser setup guidance", result.Content)
	}
}
```

In `internal/tools/shell_browser_guard_test.go`, replace `func TestBashExecutorBlocksManagedBrowserInstallBeforeApproval` with:

```go
func TestBashExecutorBlocksManagedBrowserInstallBeforeApproval(t *testing.T) {
	args, _ := json.Marshal(BashParams{
		Command: "npm install --prefix /tmp/matrixclaw/runtime/browser/playwright-mcp @playwright/mcp@latest",
	})
	result, err := NewShellExecutors(nil)[0].Execute(context.Background(), Call{Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Approval != nil {
		t.Fatalf("result = %#v, want guard error without approval request", result)
	}
	if !strings.Contains(result.Content, "Modules -> Browser -> Install/Repair") {
		t.Fatalf("content = %q, want Browser module setup guidance", result.Content)
	}
}
```

Create `internal/tools/shell_test.go`:

```go
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakeShellTasks struct {
	commands []Command
	result   CommandResult
	reads    []TaskRead
	output   TaskOutput
	stopped  []string
}

func (f *fakeShellTasks) RunCommand(_ context.Context, _ Call, command Command) (CommandResult, error) {
	f.commands = append(f.commands, command)
	return f.result, nil
}

func (f *fakeShellTasks) ReadTaskOutput(_ context.Context, _ Call, read TaskRead) (TaskOutput, error) {
	f.reads = append(f.reads, read)
	return f.output, nil
}

func (f *fakeShellTasks) StopTask(_ context.Context, _ Call, taskID string) (TaskInfo, error) {
	f.stopped = append(f.stopped, taskID)
	return TaskInfo{TaskID: taskID, Status: "canceled"}, nil
}

func runShellTool(t *testing.T, tasks ShellTasks, name string, args string) Result {
	t.Helper()
	registry := NewRegistry(NewShellExecutors(tasks)...)
	result, err := registry.Execute(context.Background(), name, Call{WorkingDir: "/work", Approved: true, Args: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBashAppliesTimeoutAndAutoBackgroundDefaults(t *testing.T) {
	tasks := &fakeShellTasks{result: CommandResult{Output: "ok\n"}}

	result := runShellTool(t, tasks, "bash", `{"command":"go test ./..."}`)
	runShellTool(t, tasks, "bash", `{"command":"make","timeout":3600,"auto_background_after":30,"run_in_background":true}`)

	if result.Content != "ok" || result.IsError {
		t.Fatalf("result = %+v", result)
	}
	want := []Command{
		{Command: "go test ./...", WorkingDir: "/work", Timeout: 10 * time.Minute, AutoBackground: 2 * time.Minute, OutputLimit: maxToolOutput},
		{Command: "make", WorkingDir: "/work", Background: true, Timeout: time.Hour, AutoBackground: 30 * time.Second, OutputLimit: maxToolOutput},
	}
	for i := range want {
		if tasks.commands[i] != want[i] {
			t.Errorf("command %d = %+v, want %+v", i, tasks.commands[i], want[i])
		}
	}
	if tooLong := runShellTool(t, tasks, "bash", `{"command":"make","timeout":3601}`); !tooLong.IsError || len(tasks.commands) != 2 {
		t.Fatalf("a timeout over an hour ran: %+v", tooLong)
	}
}

func TestBashReportsTheTaskACommandWentOnAs(t *testing.T) {
	tasks := &fakeShellTasks{result: CommandResult{TaskID: "task_1"}}

	moved := runShellTool(t, tasks, "bash", `{"command":"npm test"}`)
	started := runShellTool(t, tasks, "bash", `{"command":"npm run dev","run_in_background":true}`)

	if !strings.Contains(moved.Content, "still running after 2m0s, so it went on as background task task_1") || moved.Metadata.(BashResponseMetadata).TaskID != "task_1" {
		t.Fatalf("moved = %+v", moved)
	}
	if !strings.HasPrefix(started.Content, "Background task task_1 started.") {
		t.Fatalf("started = %+v", started)
	}
}

func TestBashReportsTimeoutsAndCutOutput(t *testing.T) {
	timedOut := runShellTool(t, &fakeShellTasks{result: CommandResult{Output: "partial", TimedOut: true, ExitCode: -1}}, "bash", `{"command":"sleep 99","timeout":5}`)
	if !timedOut.IsError || !strings.Contains(timedOut.Content, "killed after its timeout of 5s") {
		t.Fatalf("timed out = %+v", timedOut)
	}
	cut := runShellTool(t, &fakeShellTasks{result: CommandResult{Output: "head", OutputPath: "/data/task_1.log"}}, "bash", `{"command":"seq 1 1000000"}`)
	if cut.Content != "head\n\n(output truncated; the full output is in /data/task_1.log)" {
		t.Fatalf("cut = %q", cut.Content)
	}
}

func TestTaskOutputShowsNewOutputAndStatus(t *testing.T) {
	code := 1
	tasks := &fakeShellTasks{output: TaskOutput{TaskInfo: TaskInfo{TaskID: "task_1", Status: "failed", ExitCode: &code, OutputPath: "/data/task_1.log"}, Text: "FAIL x\n", Skipped: 10, More: true}}

	result := runShellTool(t, tasks, "task_output", `{"id":"task_1","wait_seconds":30,"filter":"FAIL"}`)

	want := "[... 10 bytes of output dropped ...]\n\nFAIL x\n\n(task failed, exit code 1)\n\n(more output is waiting: call task_output again, or read /data/task_1.log)"
	if result.Content != want || result.IsError {
		t.Fatalf("content = %q", result.Content)
	}
	if read := tasks.reads[0]; read != (TaskRead{TaskID: "task_1", Wait: 30 * time.Second, Filter: "FAIL", Limit: maxToolOutput}) {
		t.Fatalf("read = %+v", read)
	}
}

func TestTaskKillAsksThenStops(t *testing.T) {
	tasks := &fakeShellTasks{}
	registry := NewRegistry(NewShellExecutors(tasks)...)
	asked, err := registry.Execute(context.Background(), "task_kill", Call{Args: json.RawMessage(`{"id":"task_1"}`)})
	if err != nil || asked.Approval == nil || len(tasks.stopped) != 0 {
		t.Fatalf("unapproved = %+v, %v", asked, err)
	}
	stopped := runShellTool(t, tasks, "task_kill", `{"id":"task_1"}`)
	if stopped.Content != "Task task_1 is canceled." || len(tasks.stopped) != 1 {
		t.Fatalf("stopped = %+v", stopped)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/tools -run '^(TestRegistryConcurrencyKeyPrefersTheExecutorsOwnKey|TestPermissionSubjectsNameResolvedPathsAndCommands|TestBashExecutorBlocksManagedBrowserInstallEvenWhenApproved|TestBashExecutorBlocksManagedBrowserInstallBeforeApproval|TestBashAppliesTimeoutAndAutoBackgroundDefaults|TestBashReportsTheTaskACommandWentOnAs|TestBashReportsTimeoutsAndCutOutput|TestTaskOutputShowsNewOutputAndStatus|TestTaskKillAsksThenStops)$'
```

Expected: build fails (`NewShellExecutors`, `ShellTasks` users are undefined).

- [ ] **Step 3: Implement**

In `clients/terminal/ui/surface/chat/bash.go`, replace `func (BashToolRenderContext) RenderTool` with:

```go
func (b *BashToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Run", opts.Anim, opts.Compact)
	}

	var params tools.BashParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		params.Command = "failed to parse command"
	}

	var meta tools.BashResponseMetadata
	if opts.HasResult() {
		_ = json.Unmarshal([]byte(opts.Result.Metadata), &meta)
	}

	if meta.Background {
		description := cmp.Or(meta.Description, params.Command)
		content := "Command: " + params.Command + "\n" + opts.Result.Content
		return renderTaskTool(sty, opts, cappedWidth, "Start", meta.TaskID, description, content)
	}

	cmd := strings.ReplaceAll(params.Command, "\n", " ")
	cmd = strings.ReplaceAll(cmd, "\t", "    ")
	toolParams := []string{cmd}
	if params.RunInBackground {
		toolParams = append(toolParams, "background", "true")
	}

	header := runHeader(sty, opts.Status, cappedWidth, opts.Compact, toolParams...)
	if opts.Compact {
		return header
	}
	if earlyState, ok := runEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}
	if !opts.HasResult() {
		return header
	}

	output := meta.Output
	if output == "" && opts.Result.Content != "no output" {
		output = opts.Result.Content
	}
	if output == "" {
		return header
	}

	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, output, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
type TaskOutputToolMessageItem struct{ *baseToolMessageItem }
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
type TaskKillToolMessageItem struct{ *baseToolMessageItem }
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
func NewTaskOutputToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &TaskOutputToolRenderContext{}, canceled)
}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
func NewTaskKillToolMessageItem(sty *surfacestyles.Styles, toolCall surfacemessage.ToolCall, result *surfacemessage.ToolResult, canceled bool) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &TaskKillToolRenderContext{}, canceled)
}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
type TaskOutputToolRenderContext struct{}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
type TaskKillToolRenderContext struct{}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
func (j *TaskOutputToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	return renderTaskToolCall(sty, width, opts, "Output")
}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
func (j *TaskKillToolRenderContext) RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	return renderTaskToolCall(sty, width, opts, "Kill")
}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
func renderTaskToolCall(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts, action string) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Task", opts.Anim, opts.Compact)
	}

	var params tools.TaskOutputParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &surfacemessage.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	var description string
	if opts.HasResult() && opts.Result.Metadata != "" {
		var meta tools.TaskInfo
		if err := json.Unmarshal([]byte(opts.Result.Metadata), &meta); err == nil {
			description = cmp.Or(meta.Description, meta.Command)
		}
	}

	content := ""
	if opts.HasResult() {
		content = opts.Result.Content
	}
	return renderTaskTool(sty, opts, cappedWidth, action, params.ID, description, content)
}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
func renderTaskTool(sty *surfacestyles.Styles, opts *ToolRenderOpts, width int, action, taskID, description, content string) string {
	header := taskHeader(sty, opts.Status, action, taskID, description, width)
	if opts.Compact {
		return header
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, width); ok {
		return joinToolParts(header, earlyState)
	}
	if content == "" {
		return header
	}

	bodyWidth := width - toolBodyLeftPaddingTotal
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, content, bodyWidth, opts.ExpandedContent))
	return joinToolParts(header, body)
}
```

In `clients/terminal/ui/surface/chat/bash.go`, add:

```go
func taskHeader(sty *surfacestyles.Styles, status ToolStatus, action, taskID, description string, width int) string {
	icon := toolIcon(sty, status)
	taskPart := sty.Tool.JobToolName.Render("Task")
	actionPart := sty.Tool.JobAction.Render("(" + action + ")")
	idPart := sty.Tool.JobPID.Render(taskID)
	prefix := fmt.Sprintf("%s %s %s %s", icon, taskPart, actionPart, idPart)

	if description == "" {
		return prefix
	}

	prefixWidth := lipgloss.Width(prefix)
	availableWidth := width - prefixWidth - 1
	if availableWidth < 10 {
		return prefix
	}

	truncatedDesc := ansi.Truncate(description, availableWidth, "…")
	return prefix + " " + sty.Tool.JobDescription.Render(truncatedDesc)
}
```

In `clients/terminal/ui/surface/chat/bash.go`, delete `func (JobKillToolRenderContext) RenderTool`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `func (JobOutputToolRenderContext) RenderTool`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `func NewJobKillToolMessageItem`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `func NewJobOutputToolMessageItem`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `func jobHeader`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `func renderJobTool`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `type JobKillToolMessageItem`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `type JobKillToolRenderContext`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `type JobOutputToolMessageItem`.

In `clients/terminal/ui/surface/chat/bash.go`, delete `type JobOutputToolRenderContext`.

In `clients/terminal/ui/surface/chat/tools.go`, replace `func NewToolMessageItem` with:

```go
func NewToolMessageItem(
	sty *surfacestyles.Styles,
	messageID string,
	toolCall surfacemessage.ToolCall,
	result *surfacemessage.ToolResult,
	canceled bool,
) ToolMessageItem {
	var item ToolMessageItem
	switch normalizedToolName(toolCall.Name) {
	case "bash":
		item = NewBashToolMessageItem(sty, toolCall, result, canceled)
	case "task_output":
		item = NewTaskOutputToolMessageItem(sty, toolCall, result, canceled)
	case "task_kill":
		item = NewTaskKillToolMessageItem(sty, toolCall, result, canceled)
	case "read":
		item = NewReadToolMessageItem(sty, toolCall, result, canceled)
	case "write":
		item = NewWriteToolMessageItem(sty, toolCall, result, canceled)
	case "edit":
		item = NewEditToolMessageItem(sty, toolCall, result, canceled)
	case "multiedit":
		item = NewMultiEditToolMessageItem(sty, toolCall, result, canceled)
	case "glob":
		item = NewGlobToolMessageItem(sty, toolCall, result, canceled)
	case "grep":
		item = NewGrepToolMessageItem(sty, toolCall, result, canceled)
	case "ls":
		item = NewLSToolMessageItem(sty, toolCall, result, canceled)
	case "delegate_task", "spawn_subagent":
		item = NewDelegateTaskToolMessageItem(sty, toolCall, result, canceled)
	default:
		item = NewGenericToolMessageItem(sty, toolCall, result, canceled)
	}
	item.SetMessageID(messageID)
	return item
}
```

In `internal/daemoncmd/run.go`, replace `func Run` with:

```go
func Run(ctx context.Context) error {
	bootstrap, err := loadBootstrap()
	if err != nil {
		return err
	}

	sqliteStore, err := store.NewSQLite(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = sqliteStore.Close() }()
	automationStore, err := automation.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = automationStore.Close() }()
	workStore, err := work.NewSQLiteStore(bootstrap.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = workStore.Close() }()
	webResearchStore := webresearch.NewStore(workStore)

	storageModule, err := localstorage.New(localstorage.Config{
		Root: defaultStorageRoot(bootstrap.DBPath),
	})
	if err != nil {
		return err
	}
	mcpModule, err := mcpmodule.New(ctx, mcpConfigWithBrowser(bootstrap.ExternalAgents))
	if err != nil {
		log.Printf("matrixclawd mcp module disabled: %v", err)
		mcpModule, _ = mcpmodule.New(ctx, setup.MCPConfig{})
	}
	defer func() { _ = mcpModule.Close() }()
	skillsModule, err := skillsmodule.New(skillsConfigFromBootstrap(bootstrap))
	if err != nil {
		return err
	}
	defer func() { _ = skillsModule.Close() }()
	moduleRegistry := modules.NewRegistry(storageModule, mcpModule, skillsModule)

	app := core.New(sqliteStore).
		WithSessionLLMs(bootstrap.SessionLLMs).
		WithRunBudgets(bootstrap.Budgets).
		WithSessionFiles(sessionFilesRoot(bootstrap.DBPath)).
		WithCompactModel(bootstrap.CompactModel.Provider, bootstrap.CompactModel.Model).
		WithContextWindowCap(bootstrap.WindowCap).
		WithModelConcurrency(bootstrap.ModelConcurrency).
		WithWorkStore(workStore).
		WithAttachmentReader(storageAttachmentReader{store: storageModule.Store()}).
		WithSkillsContext(skillsModule).
		WithRuntimeStatusContext(&setupRuntimeStatusContext{setup: bootstrap.SetupService, runtime: localruntime.New("")})
	// The workflow worker (below) may start executing persisted runs before
	// supervisor.ApplyBootstrap runs, so the profile is set here too, via the
	// same helper, ensuring module context is never missing.
	applyAssistantProfile(app, bootstrap.Assistant, moduleRegistry.Context)
	externalRegistry, externalRuntimes, err := builtins.BuildRegistry(bootstrap.ExternalAgents)
	if err != nil {
		return err
	}
	app.WithExternalAgents(externalRegistry, sqliteStore)
	automationService := automation.NewService(automationStore, app, bootstrap.Timezone).
		WithDeliveryTargets(automationDeliveryTargets(bootstrap))
	webSearchConfig := webSearchProviderConfig(bootstrap.SetupService)
	webResearchEngine := newWebResearchEngine(bootstrap.DBPath, bootstrap.ExternalAgents.MCP, mcpModule, webResearchStore, webSearchConfig)
	webTools := webtools.NewWebService(webSearchConfig, webResearchEngine)
	osmGeo := tools.NewOSMServiceFromEnv()
	extraTools := []tools.Executor{
		automation.NewReminderTool(automationService),
		automation.NewScheduledAITaskTool(automationService),
		deliverymodule.NewSendFileTool(storageModule.Store(), app),
		telephonymodule.NewCallTool(bootstrap.SetupService),
		telephonymodule.NewEndCallTool(bootstrap.SetupService),
		voicemodule.NewTextToSpeechTool(bootstrap.SetupService),
		webtools.NewWebFetchExecutorWithService(webTools),
		webtools.NewWebSearchExecutorWithService(webTools),
	}
	extraTools = append(extraTools, webtools.NewWebResearchExecutorsWithService(webTools)...)
	toolRegistry := tools.NewCoreCodingRegistry(extraTools...)
	if err := toolRegistry.Register(tools.NewShellExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.TodoToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.MemoryToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Register(core.SubagentToolExecutors(app)...); err != nil {
		return err
	}
	if err := toolRegistry.Err(); err != nil {
		return err
	}
	if err := toolRegistry.Register(tools.NewOSMGeoExecutors(osmGeo)...); err != nil {
		return err
	}
	if err := moduleRegistry.RegisterTools(toolRegistry); err != nil {
		return err
	}
	app.WithTools(newSetupAwareToolExecutor(toolRegistry, bootstrap.SetupService))
	// Shell tasks do not survive a restart; this runs before any run can start new ones.
	if err := app.RecoverTasks(ctx); err != nil {
		log.Printf("matrixclawd background task recovery failed: %v", err)
	}
	// The workflow worker executes persisted runs as soon as it starts, so it
	// starts only once the core is fully wired and before anything accepts runs.
	lifetime, stopLifetime := context.WithCancel(ctx)
	defer stopLifetime()
	app.WithLifetime(lifetime)
	runStarter, err := goworkflows.NewForStore(bootstrap.DBPath, app)
	if err != nil {
		return err
	}
	defer func() {
		// End the lifetime first so runs interrupted by closing the worker are not rescheduled.
		stopLifetime()
		_ = runStarter.Close()
	}()
	app.WithRunStarter(runStarter)
	server := api.New(app)
	server.SetAPIToken(bootstrap.APIToken)
	server.SetAutomationService(automationService)
	server.SetStorageStore(storageModule.Store())
	server.SetSkillsService(skillsModule.Service())
	server.SetSetupService(bootstrap.SetupService)
	server.SetRealtimeVoiceService(newRealtimeVoiceManager(bootstrap.SetupService, app))
	supervisor := newSupervisor(ctx, server, app, osmGeo)
	supervisor.SetModuleContext(moduleRegistry.Context)
	supervisor.SetExternalAgents(sqliteStore, externalRuntimes)
	defer supervisor.CloseExternalAgents()
	httpServer := &http.Server{
		Addr:              bootstrap.Addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 2)
	safego.Go("daemon.httpServer", func() {
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		errCh <- err
	})

	if err := supervisor.ApplyBootstrap(bootstrap); err != nil {
		return err
	}
	startConfiguredVoiceRuntimes(ctx, bootstrap.SetupService)
	safego.Go("automation.Run", func() { automationService.Run(ctx) })
	safego.Go("webresearch.Run", func() { webResearchEngine.Start(ctx) })
	safego.Go("supervisor.deliverStartupNotifications", func() {
		supervisor.DeliverPendingStartupNotifications(bootstrap)
	})
	safego.Go("core.recoverState", func() {
		if err := app.RecoverActiveRuns(context.Background()); err != nil {
			log.Printf("matrixclawd active run recovery failed: %v", err)
		}
		if err := app.RecoverSessionInputs(context.Background()); err != nil {
			log.Printf("matrixclawd session input recovery failed: %v", err)
		}
		if err := app.RecoverSubagentTasks(context.Background()); err != nil {
			log.Printf("matrixclawd subagent recovery failed: %v", err)
		}
	})

	log.Printf("matrixclawd bootstrap: setup=%s", bootstrap.SetupPath)
	log.Printf("matrixclawd listening on %s using %s", bootstrap.Addr, bootstrap.DBPath)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
```

In `internal/mcp/server.go`, replace `func toolResultToMCP` with:

```go
func toolResultToMCP(result tools.Result) *sdk.CallToolResult {
	content := strings.TrimSpace(result.Content)
	if content == "" {
		content = string(result.Status)
	}
	out := &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: content}},
		IsError: result.IsError || result.Status == tools.ResultStatusError,
	}
	structured := map[string]any{
		"status": result.Status,
	}
	if result.Metadata != nil {
		structured["metadata"] = result.Metadata
	}
	if result.FileVersion != nil {
		structured["file_version"] = result.FileVersion
	}
	if result.Approval != nil {
		structured["approval"] = result.Approval
		out.IsError = true
	}
	out.StructuredContent = structured
	return out
}
```

In `internal/tools/definitions.go`, replace `type Definition` with:

```go
// Definition is a core tool; one whose executor needs the daemon's services
// (the shell tools) has no NewExecutor and is registered on its own.
type Definition struct {
	Spec        Spec
	NewExecutor func() Executor
}
```

In `internal/tools/definitions.go`, replace `var coreDefinitions` with:

```go
var coreDefinitions = []Definition{
	{
		Spec: Spec{
			ID:              readToolName,
			Name:            "Read",
			Description:     "Read a file with line numbers",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputFileContent,
			InputJSONSchema: readInputSchema,
		},
		NewExecutor: NewReadExecutor,
	},
	{
		Spec: Spec{
			ID:              globToolName,
			Name:            "Glob",
			Description:     "Find files by path pattern",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputSearchResults,
			InputJSONSchema: globInputSchema,
		},
		NewExecutor: NewGlobExecutor,
	},
	{
		Spec: Spec{
			ID:              grepToolName,
			Name:            "Grep",
			Description:     "Search file contents by pattern",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputSearchResults,
			InputJSONSchema: grepInputSchema,
		},
		NewExecutor: NewGrepExecutor,
	},
	{
		Spec: Spec{
			ID:              lsToolName,
			Name:            "LS",
			Description:     "List files in a tree",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreFilesystem,
			Category:        CategoryFilesystem,
			Profiles:        []Profile{ProfileReadOnly, ProfileCoding},
			OutputKind:      OutputFileTree,
			InputJSONSchema: lsInputSchema,
		},
		NewExecutor: NewLSExecutor,
	},
	{
		Spec: Spec{
			ID:               writeToolName,
			Name:             "Write",
			Description:      "Create or replace a file",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "write_permissions",
			Namespace:        namespaceCoreFilesystem,
			Category:         CategoryFilesystem,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputDiff,
			InputJSONSchema:  writeInputSchema,
		},
		NewExecutor: NewWriteExecutor,
	},
	{
		Spec: Spec{
			ID:               editToolName,
			Name:             "Edit",
			Description:      "Replace content inside an existing file",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "edit_permissions",
			Namespace:        namespaceCoreFilesystem,
			Category:         CategoryFilesystem,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputDiff,
			InputJSONSchema:  editInputSchema,
		},
		NewExecutor: NewEditExecutor,
	},
	{
		Spec: Spec{
			ID:               multiEditToolName,
			Name:             "MultiEdit",
			Description:      "Apply several edits to one file",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "multi_edit_permissions",
			Namespace:        namespaceCoreFilesystem,
			Category:         CategoryFilesystem,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputDiff,
			InputJSONSchema:  multiEditInputSchema,
		},
		NewExecutor: NewMultiEditExecutor,
	},
	{
		Spec: Spec{
			ID:               bashToolName,
			Name:             "Bash",
			Description:      "Run a shell command",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "bash_permissions",
			Namespace:        namespaceCoreShell,
			Category:         CategoryShell,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputText,
			InputJSONSchema:  bashInputSchema,
		},
	},
	{
		Spec: Spec{
			ID:              taskOutputToolName,
			Name:            "TaskOutput",
			Description:     "Read a background task's new output",
			Risk:            RiskSafe,
			Effect:          EffectReadOnly,
			ApprovalMode:    ApprovalNever,
			Namespace:       namespaceCoreShell,
			Category:        CategoryShell,
			Profiles:        []Profile{ProfileCoding},
			OutputKind:      OutputJob,
			InputJSONSchema: taskOutputInputSchema,
		},
	},
	{
		Spec: Spec{
			ID:               taskKillToolName,
			Name:             "TaskKill",
			Description:      "Stop a background task",
			Risk:             RiskApproval,
			Effect:           EffectMutation,
			ApprovalMode:     ApprovalOnRequest,
			PermissionParams: "task_kill",
			Namespace:        namespaceCoreShell,
			Category:         CategoryShell,
			Profiles:         []Profile{ProfileCoding},
			OutputKind:       OutputJob,
			InputJSONSchema:  taskKillInputSchema,
		},
	},
}
```

In `internal/tools/schema.go`, replace `var block starting with readInputSchema` with:

```go
var (
	readInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "offset": {"type": "integer", "minimum": 0},
    "limit": {"type": "integer", "minimum": 1}
  },
  "required": ["file_path"],
  "additionalProperties": false
}`)
	globInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string"},
    "path": {"type": "string"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
	grepInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string"},
    "path": {"type": "string"},
    "include": {"type": "string"},
    "literal_text": {"type": "boolean"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
	lsInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "ignore": {"type": "array", "items": {"type": "string"}},
    "depth": {"type": "integer", "minimum": 0}
  },
  "additionalProperties": false
}`)
	writeInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "content": {"type": "string"}
  },
  "required": ["file_path", "content"],
  "additionalProperties": false
}`)
	editOperationSchema = `{
    "type": "object",
    "properties": {
      "old_string": {"type": "string"},
      "new_string": {"type": "string"},
      "replace_all": {"type": "boolean"}
    },
    "required": ["old_string", "new_string"],
    "additionalProperties": false
  }`
	editInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "old_string": {"type": "string"},
    "new_string": {"type": "string"},
    "replace_all": {"type": "boolean"}
  },
  "required": ["file_path", "old_string", "new_string"],
  "additionalProperties": false
}`)
	multiEditInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "file_path": {"type": "string"},
    "edits": {
      "type": "array",
      "items": ` + editOperationSchema + `,
      "minItems": 1
    }
  },
  "required": ["file_path", "edits"],
  "additionalProperties": false
}`)
	bashInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "description": {"type": "string"},
    "command": {"type": "string"},
    "working_dir": {"type": "string"},
    "run_in_background": {"type": "boolean", "description": "Start the command as a background task and return its task id at once."},
    "timeout": {"type": "integer", "minimum": 0, "maximum": 3600, "description": "Seconds before a foreground command is killed; default 600."},
    "auto_background_after": {"type": "integer", "minimum": 0, "description": "Seconds after which a foreground command still running becomes a background task; default 120."}
  },
  "required": ["command"],
  "additionalProperties": false
}`)
	taskOutputInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "The background task id."},
    "wait_seconds": {"type": "integer", "minimum": 0, "maximum": 600, "description": "Wait up to this long for the task to finish first."},
    "filter": {"type": "string", "description": "Regular expression; only matching lines of the new output are returned."}
  },
  "required": ["id"],
  "additionalProperties": false
}`)
	taskKillInputSchema = rawSchema(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "The background task id."}
  },
  "required": ["id"],
  "additionalProperties": false
}`)
)
```

Replace the whole of `internal/tools/shell.go` with:

```go
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	bashToolName       = "bash"
	taskOutputToolName = "task_output"
	taskKillToolName   = "task_kill"
	maxToolOutput      = 30000
)

// A foreground command is killed after its timeout, 10 minutes unless the call
// names up to 60, and moves to the background after 2 minutes.
const (
	DefaultCommandTimeout = 10 * time.Minute
	MaxCommandTimeout     = time.Hour
	DefaultAutoBackground = 2 * time.Minute
	maxTaskWait           = 10 * time.Minute
)

var errShellUnavailable = errors.New("shell commands are not available in this daemon")

type BashParams struct {
	Description         string `json:"description,omitempty"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir,omitempty"`
	RunInBackground     bool   `json:"run_in_background,omitempty"`
	Timeout             int    `json:"timeout,omitempty"`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty"`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
	Timeout             int    `json:"timeout"`
	AutoBackgroundAfter int    `json:"auto_background_after"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	ExitCode         int    `json:"exit_code,omitempty"`
	Description      string `json:"description,omitempty"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	TimedOut         bool   `json:"timed_out,omitempty"`
	OutputPath       string `json:"output_path,omitempty"`
}

type TaskOutputParams struct {
	ID          string `json:"id"`
	WaitSeconds int    `json:"wait_seconds,omitempty"`
	Filter      string `json:"filter,omitempty"`
}

type TaskKillParams struct {
	ID string `json:"id"`
}

type bashExecutor struct{ tasks ShellTasks }
type taskOutputExecutor struct{ tasks ShellTasks }
type taskKillExecutor struct{ tasks ShellTasks }

// NewShellExecutors returns bash, task_output and task_kill over tasks.
func NewShellExecutors(tasks ShellTasks) []Executor {
	return []Executor{&bashExecutor{tasks: tasks}, &taskOutputExecutor{tasks: tasks}, &taskKillExecutor{tasks: tasks}}
}

func (e *bashExecutor) Spec() Spec {
	return coreDefinitionSpec(bashToolName)
}

func (e *taskOutputExecutor) Spec() Spec {
	return coreDefinitionSpec(taskOutputToolName)
}

func (e *taskKillExecutor) Spec() Spec {
	return coreDefinitionSpec(taskKillToolName)
}

func (e *bashExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params BashParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(bashToolName, err)
	}
	if strings.TrimSpace(params.Command) == "" {
		return Result{Content: "command is required", IsError: true}, nil
	}
	if blockedManagedBrowserInstallCommand(params.Command) {
		return Result{Content: managedBrowserSetupMessage, Status: ResultStatusError, IsError: true}, nil
	}
	timeout, autoBackground, err := commandLimits(params)
	if err != nil {
		return Result{Content: err.Error(), Status: ResultStatusError, IsError: true}, nil
	}
	workingDir := resolvePath(call.WorkingDir, params.WorkingDir)
	if !call.Approved {
		return approvalResult(bashToolName, "execute", workingDir, "Execute command: "+params.Command, BashPermissionsParams{
			Description:         params.Description,
			Command:             params.Command,
			WorkingDir:          workingDir,
			RunInBackground:     params.RunInBackground,
			Timeout:             params.Timeout,
			AutoBackgroundAfter: params.AutoBackgroundAfter,
		}), nil
	}
	if e.tasks == nil {
		return Result{}, errShellUnavailable
	}
	result, err := e.tasks.RunCommand(ctx, call, Command{
		Command:        params.Command,
		Description:    params.Description,
		WorkingDir:     workingDir,
		Background:     params.RunInBackground,
		Timeout:        timeout,
		AutoBackground: autoBackground,
		OutputLimit:    maxToolOutput,
	})
	if err != nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	meta := BashResponseMetadata{
		StartTime:        result.StartedAt.UnixMilli(),
		EndTime:          result.EndedAt.UnixMilli(),
		Description:      params.Description,
		WorkingDirectory: workingDir,
	}
	if result.TaskID != "" {
		meta.Background, meta.TaskID = true, result.TaskID
		return Result{Content: backgroundStartText(result.TaskID, params.RunInBackground, autoBackground), Metadata: meta}, nil
	}
	meta.Output = strings.TrimSpace(result.Output)
	meta.ExitCode, meta.TimedOut, meta.OutputPath = result.ExitCode, result.TimedOut, result.OutputPath
	content := meta.Output
	if result.OutputPath != "" {
		content += "\n\n(output truncated; the full output is in " + result.OutputPath + ")"
	}
	if result.TimedOut {
		content += fmt.Sprintf("\n\n(killed after its timeout of %s; run long commands with run_in_background)", timeout)
		return Result{Content: strings.TrimSpace(content), Metadata: meta, Status: ResultStatusError, IsError: true}, nil
	}
	if result.ExitCode == 0 {
		return Result{Content: content, Metadata: meta}, nil
	}
	status := ResultStatusError
	if isExpectedEmptyProcessProbe(params.Command, meta.Output, result.ExitCode) {
		status = ResultStatusNeutral
	}
	return Result{Content: content, Metadata: meta, Status: status, IsError: status == ResultStatusError}, nil
}

// commandLimits reads a call's timeout and auto-background delay in seconds;
// zero takes the defaults.
func commandLimits(params BashParams) (time.Duration, time.Duration, error) {
	timeout := time.Duration(params.Timeout) * time.Second
	switch {
	case params.Timeout < 0 || timeout > MaxCommandTimeout:
		return 0, 0, fmt.Errorf("timeout is in seconds, at most %d", int(MaxCommandTimeout/time.Second))
	case params.AutoBackgroundAfter < 0:
		return 0, 0, errors.New("auto_background_after is in seconds and must not be negative")
	case timeout == 0:
		timeout = DefaultCommandTimeout
	}
	autoBackground := time.Duration(params.AutoBackgroundAfter) * time.Second
	if autoBackground == 0 {
		autoBackground = DefaultAutoBackground
	}
	return timeout, autoBackground, nil
}

func backgroundStartText(taskID string, requested bool, after time.Duration) string {
	how := "Read its output with task_output (wait_seconds waits for it to finish); stop it with task_kill."
	if requested {
		return "Background task " + taskID + " started. " + how
	}
	return fmt.Sprintf("The command is still running after %s, so it went on as background task %s. %s", after, taskID, how)
}

func (e *taskOutputExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params TaskOutputParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(taskOutputToolName, err)
	}
	if strings.TrimSpace(params.ID) == "" {
		return Result{Content: "id is required", IsError: true}, nil
	}
	wait := time.Duration(params.WaitSeconds) * time.Second
	if params.WaitSeconds < 0 || wait > maxTaskWait {
		return Result{Content: fmt.Sprintf("wait_seconds is at most %d", int(maxTaskWait/time.Second)), IsError: true}, nil
	}
	if e.tasks == nil {
		return Result{}, errShellUnavailable
	}
	out, err := e.tasks.ReadTaskOutput(ctx, call, TaskRead{TaskID: strings.TrimSpace(params.ID), Wait: wait, Filter: params.Filter, Limit: maxToolOutput})
	if err != nil {
		return Result{}, err
	}
	return Result{Content: taskOutputText(out), Metadata: out.TaskInfo, Status: ResultStatusNeutral}, nil
}

func taskOutputText(out TaskOutput) string {
	var parts []string
	if out.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("[... %d bytes of output dropped ...]", out.Skipped))
	}
	if text := strings.TrimRight(out.Text, "\n"); text != "" {
		parts = append(parts, text)
	} else {
		parts = append(parts, "(no new output)")
	}
	status := "(task " + out.Status
	if out.ExitCode != nil {
		status += fmt.Sprintf(", exit code %d", *out.ExitCode)
	}
	parts = append(parts, status+")")
	if out.More {
		parts = append(parts, "(more output is waiting: call task_output again, or read "+out.OutputPath+")")
	}
	return strings.Join(parts, "\n\n")
}

func (e *taskKillExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params TaskKillParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(taskKillToolName, err)
	}
	id := strings.TrimSpace(params.ID)
	if id == "" {
		return Result{Content: "id is required", IsError: true}, nil
	}
	if !call.Approved {
		return approvalResult(taskKillToolName, "kill", id, "Stop background task "+id, params), nil
	}
	if e.tasks == nil {
		return Result{}, errShellUnavailable
	}
	info, err := e.tasks.StopTask(ctx, call, id)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: "Task " + id + " is " + info.Status + ".", Metadata: info}, nil
}

func isExpectedEmptyProcessProbe(command string, output string, exitCode int) bool {
	if exitCode != 1 || strings.TrimSpace(output) != "" {
		return false
	}
	return IsProcessProbeCommand(command)
}

func IsProcessProbeCommand(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	switch {
	case strings.Contains(command, "grep -v grep"):
		return true
	case strings.Contains(command, "pgrep"):
		return true
	case strings.Contains(command, "pidof"):
		return true
	case strings.Contains(command, "pkill"):
		return true
	case strings.Contains(command, "ps ") && strings.Contains(command, "grep"):
		return true
	default:
		return false
	}
}
```

In `internal/tools/types.go`, the imports become:

```go
import (
	"context"
	"encoding/json"

	"github.com/Suren878/matrixclaw/internal/permission"
)
```

In `internal/tools/types.go`, replace `type Result` with:

```go
type Result struct {
	Content     string           `json:"content"`
	Metadata    any              `json:"metadata,omitempty"`
	MIMEType    string           `json:"mime_type,omitempty"`
	Status      ResultStatus     `json:"status,omitempty"`
	IsError     bool             `json:"is_error,omitempty"`
	Approval    *ApprovalRequest `json:"approval,omitempty"`
	FileVersion *FileVersion     `json:"file_version,omitempty"`
	// OutputPath is the file holding the full output when Content was cut.
	OutputPath string `json:"output_path,omitempty"`
}
```

In `internal/tools/types.go`, delete `type BackgroundJob`.

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/tools -run '^(TestRegistryConcurrencyKeyPrefersTheExecutorsOwnKey|TestPermissionSubjectsNameResolvedPathsAndCommands|TestBashExecutorBlocksManagedBrowserInstallEvenWhenApproved|TestBashExecutorBlocksManagedBrowserInstallBeforeApproval|TestBashAppliesTimeoutAndAutoBackgroundDefaults|TestBashReportsTheTaskACommandWentOnAs|TestBashReportsTimeoutsAndCutOutput|TestTaskOutputShowsNewOutputAndStatus|TestTaskKillAsksThenStops)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add clients/terminal/ui/surface/chat/bash.go \
  clients/terminal/ui/surface/chat/tools.go \
  internal/daemoncmd/run.go \
  internal/mcp/server.go \
  internal/providers/ai/gemini/schema_test.go \
  internal/tools/concurrency_key_test.go \
  internal/tools/definitions.go \
  internal/tools/permission_subject_test.go \
  internal/tools/schema.go \
  internal/tools/shell.go \
  internal/tools/shell_browser_guard_test.go \
  internal/tools/shell_test.go \
  internal/tools/types.go
git commit -m "feat(tools): bash timeouts and auto-background; task_output and task_kill replace job tools

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 7: Finished tasks reach the run as engine notes

**Files:**
- Modify: `internal/agent/agenttest/agenttest.go`
- Modify: `internal/agent/engine.go`
- Create: `internal/agent/events.go`
- Modify: `internal/agent/ports.go`
- Modify: `internal/core/agent_inbox.go`
- Modify: `internal/core/agent_prompts.go`
- Modify: `internal/core/shell_tasks.go`
- Create: `internal/core/task_events.go`
- Test (create): `internal/agent/events_test.go`
- Test (modify): `internal/core/shell_tasks_test.go`

The engine drains events at the start of every step (after decided approvals, before the context note), so a task that finished while the model worked is in the very next request. Core's inbox lists the session's undelivered tasks in finish order and `Consume` marks the given IDs delivered to the run (steer IDs simply match no task). `ReadTaskOutput` of a finished task marks it delivered, so waiting on it with `task_output` does not produce a second note.

- [ ] **Step 1: Write the failing tests**

Create `internal/agent/events_test.go`:

```go
package agent_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/agenttest"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestFinishedTasksReachTheModelAsNotesBeforeItsNextStep(t *testing.T) {
	f := agenttest.NewFixture()
	f.Inbox.Events = []agent.Input{{Kind: agent.InputEvent, ID: "task_1", Text: "Background task task_1 finished with exit code 0: go test ./..."}}
	f.Tools.Funcs["read"] = func(tools.Call) tools.Result {
		f.Inbox.Events = append(f.Inbox.Events, agent.Input{Kind: agent.InputEvent, ID: "task_2", Text: "Background task task_2 finished with exit code 1: make"})
		return tools.Result{Content: "file body"}
	}
	model := agenttest.NewScriptedModel(calls(call("c1", "read")), text("Both done."))

	outcome := run(t, f, model)

	if outcome.Status != agent.StatusCompleted || len(f.Inbox.Events) != 0 {
		t.Fatalf("outcome = %+v, events left = %+v", outcome, f.Inbox.Events)
	}
	var notes []string
	for _, message := range f.Journal.Messages {
		if message.Origin == transcript.OriginEngine {
			notes = append(notes, message.Content)
		}
	}
	if len(notes) != 2 || notes[0] != "Background task task_1 finished with exit code 0: go test ./..." || notes[1] != "Background task task_2 finished with exit code 1: make" {
		t.Fatalf("notes = %q", notes)
	}
	requests := model.Requests()
	if last := requests[0].Messages[len(requests[0].Messages)-1]; last.Role != "user" || last.Content != notes[0] {
		t.Fatalf("first request ends with %+v", last)
	}
	if last := requests[1].Messages[len(requests[1].Messages)-1]; last.Content != notes[1] {
		t.Fatalf("second request ends with %+v", last)
	}
}
```

In `internal/core/shell_tasks_test.go`, the imports become:

```go
import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/shelltask"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)
```

In `internal/core/shell_tasks_test.go`, add:

```go
func TestNativeRunSeesFinishedAndRunningTasks(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx := context.Background()
	files := t.TempDir()
	app.WithSessionFiles(files)
	session, run := saveCrashRecoveryRun(t, db, "notes", core.RunStatusAccepted, false)
	out, err := shelltask.CreateOutput(filepath.Join(files, session.ID, "tasks", "task_done.log"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("--- FAIL: TestParse\n")); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
	now := runRecoveryTestTime()
	for _, task := range []core.Task{
		{ID: "task_done", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "go test ./...", Background: true, OutputPath: out.Path(), StartedAt: now, UpdatedAt: now},
		{ID: "task_live", SessionID: session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true, StartedAt: now, UpdatedAt: now},
	} {
		if err := db.CreateTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	code := 1
	if _, err := db.FinishTask(ctx, "task_done", core.TaskStatusFailed, &code, "", now); err != nil {
		t.Fatal(err)
	}

	var requests []providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		return providers.Response{Text: "Noted."}, nil
	})})
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}

	var sent []string
	for _, message := range requests[0].Messages {
		sent = append(sent, message.Content)
	}
	transcript := strings.Join(sent, "\n")
	for _, want := range []string{"Background task task_done finished with exit code 1.", "--- FAIL: TestParse", "- task_live: npm run dev"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("first request lacks %q:\n%s", want, transcript)
		}
	}
	done, err := db.GetTask(ctx, "task_done")
	if err != nil || done.DeliveredAt == nil || done.DeliveredRunID != run.ID {
		t.Fatalf("task_done = %+v, %v", done, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/agent -run '^(TestFinishedTasksReachTheModelAsNotesBeforeItsNextStep)$'
go test ./internal/core -run '^(TestNativeRunSeesFinishedAndRunningTasks)$'
```

Expected: the agent test fails to build (`agent.InputEvent`, `Inbox.Events` undefined); the core test finds no note.

- [ ] **Step 3: Implement**

In `internal/agent/agenttest/agenttest.go`, replace `type Inbox` with:

```go
// Inbox hands out pending steers, whose IDs are their text, and events until
// they are consumed, and decided approvals on every peek. Like the store, it
// fails on a stopped context.
type Inbox struct {
	Steers  []string
	Decided []agent.Input
	Events  []agent.Input
	Cancel  bool
}
```

In `internal/agent/agenttest/agenttest.go`, replace `func (Inbox) Peek` with:

```go
func (in *Inbox) Peek(ctx context.Context, _ string, kind agent.InputKind) ([]agent.Input, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch kind {
	case agent.InputSteer:
		out := make([]agent.Input, 0, len(in.Steers))
		for _, text := range in.Steers {
			out = append(out, agent.Input{Kind: agent.InputSteer, ID: text, Text: text})
		}
		return out, nil
	case agent.InputDecided:
		return in.Decided, nil
	case agent.InputEvent:
		return slices.Clone(in.Events), nil
	default:
		return nil, fmt.Errorf("agenttest: unknown input kind %q", kind)
	}
}
```

In `internal/agent/agenttest/agenttest.go`, replace `func (Inbox) Consume` with:

```go
func (in *Inbox) Consume(ctx context.Context, _ string, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in.Steers = slices.DeleteFunc(in.Steers, func(text string) bool { return slices.Contains(ids, text) })
	in.Events = slices.DeleteFunc(in.Events, func(event agent.Input) bool { return slices.Contains(ids, event.ID) })
	return nil
}
```

In `internal/agent/engine.go`, replace `func (run) step` with:

```go
func (r *run) step(ctx context.Context) stepResult {
	waiting, err := r.resumeDecided(ctx)
	if err != nil {
		return failedStep(err)
	}
	if waiting {
		return stepResult{kind: stepWaitingApproval}
	}
	if err := r.drainEvents(ctx); err != nil {
		return failedStep(err)
	}
	if err := r.syncContext(ctx); err != nil {
		return failedStep(err)
	}
	final, err := r.prepareStep(ctx)
	if err != nil {
		return failedStep(err)
	}
	request, final, err := r.fitRequest(ctx, final)
	if err != nil {
		return failedStep(err)
	}
	if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
		return failedStep(err)
	}
	r.counters.Steps++
	gen, err := r.generateWithRetry(ctx, request)
	if err != nil && agentcontext.IsContextLengthExceeded(err) {
		tokens := r.promptTokens(request)
		r.learnLimit(tokens)
		compacted, compactErr := r.compactHistory(ctx, nil, tokens, agentcontext.TailPercent/2)
		if compactErr != nil {
			return failedStep(compactErr)
		}
		if !compacted {
			err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
		} else {
			retry, buildErr := r.afterSummary(ctx, final)
			if buildErr != nil {
				return failedStep(buildErr)
			}
			if gen, err = r.generateWithRetry(ctx, retry); err != nil && agentcontext.IsContextLengthExceeded(err) {
				err = fmt.Errorf("%w: %w", ErrContextExhausted, err)
			}
		}
	}
	if err == nil {
		r.anchorUsage(gen.response)
	}
	if final != "" && errors.Is(err, providers.ErrEmptyResponse) {
		return finalTurn(gen, final)
	}
	if err != nil {
		result := stepResult{kind: stepDone, assistant: &gen.assistant, saved: gen.saved, response: gen.response, err: err, markErrored: true}
		if errors.Is(err, ErrContextExhausted) {
			result.stop = StopContextExhausted
		}
		return result
	}
	if r.canceled(ctx) {
		return stepResult{kind: stepDone, canceled: true, assistant: &gen.assistant, saved: gen.saved}
	}
	if final != "" {
		return finalTurn(gen, final)
	}
	return r.handleResponse(ctx, gen)
}
```

Create `internal/agent/events.go`:

```go
package agent

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// drainEvents journals what happened outside the run since its last step, such
// as background tasks that finished, as engine notes and then consumes it.
func (r *run) drainEvents(ctx context.Context) error {
	events, err := r.Inbox.Peek(ctx, r.task.RunID, InputEvent)
	if err != nil || len(events) == 0 {
		return err
	}
	ids := make([]string, 0, len(events))
	for _, event := range events {
		if err := r.appendEngineMessage(ctx, transcript.OriginEngine, event.Text); err != nil {
			return err
		}
		ids = append(ids, event.ID)
	}
	return r.Inbox.Consume(ctx, r.task.RunID, ids)
}
```

In `internal/agent/ports.go`, replace `type Input` with:

```go
// Input arrives from outside the engine: steer Text, a decided approval, or an
// event such as a finished background task, whose Text the model reads.
// Denied approvals carry the user's Reason.
type Input struct {
	Kind       InputKind
	ID         string
	Text       string
	ToolCallID string
	ToolName   string
	WorkingDir string
	Args       json.RawMessage
	Denied     bool
	Reason     string
}
```

In `internal/agent/ports.go`, replace `type Inbox` with:

```go
// Inbox delivers outside input. Peek never consumes: steers and events stay
// pending until the engine consumes their IDs, and InputDecided returns decided
// approvals whose call has no result yet.
type Inbox interface {
	Peek(ctx context.Context, runID string, kind InputKind) ([]Input, error)
	Consume(ctx context.Context, runID string, ids []string) error
	Canceled(ctx context.Context, runID string) (bool, error)
}
```

In `internal/agent/ports.go`, replace `const block starting with InputSteer` with:

```go
const (
	InputSteer   InputKind = "steer"
	InputDecided InputKind = "decided"
	InputEvent   InputKind = "event"
)
```

In `internal/core/agent_inbox.go`, replace `func (coreInbox) Peek` with:

```go
func (in coreInbox) Peek(ctx context.Context, runID string, kind agent.InputKind) ([]agent.Input, error) {
	switch kind {
	case agent.InputSteer:
		return in.steers(ctx, runID)
	case agent.InputDecided:
		return in.decided(ctx, runID)
	case agent.InputEvent:
		return in.events(ctx)
	default:
		return nil, fmt.Errorf("core: unknown inbox input %q", kind)
	}
}
```

In `internal/core/agent_inbox.go`, replace `func (coreInbox) Consume` with:

```go
// Consume marks the run's pending steers and the events with the given IDs
// consumed by it.
func (in coreInbox) Consume(ctx context.Context, runID string, ids []string) error {
	if err := in.c.store.MarkTasksDelivered(ctx, ids, runID, in.c.now().UTC()); err != nil {
		return err
	}
	inputs, err := in.c.store.ListPendingSteerInputs(ctx, in.session.ID, runID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if !slices.Contains(ids, input.ID) {
			continue
		}
		consumedAt := in.c.now().UTC()
		input.Status = SessionInputStatusConsumed
		input.ConsumedRunID = runID
		input.ConsumedAt = &consumedAt
		input.UpdatedAt = consumedAt
		if err := in.c.store.UpdateSessionInput(ctx, input); err != nil {
			return err
		}
		in.c.publishSessionInputUpdated(input)
	}
	return nil
}
```

In `internal/core/agent_inbox.go`, add:

```go
// events lists the session's background tasks that finished unseen, in the
// order they finished.
func (in coreInbox) events(ctx context.Context) ([]agent.Input, error) {
	tasks, err := in.c.store.ListTasks(ctx, TaskFilter{SessionID: in.session.ID, Undelivered: true})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(tasks, func(a, b Task) int { return a.FinishedAt.Compare(*b.FinishedAt) })
	out := make([]agent.Input, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, agent.Input{Kind: agent.InputEvent, ID: task.ID, Text: taskEventText(task)})
	}
	return out, nil
}
```

In `internal/core/agent_prompts.go`, replace `func (corePrompts) Context` with:

```go
// Context is what changes during a run: the recovery notice, the todo list,
// the background tasks still running, runtime status and memory written since
// the run started.
func (p *corePrompts) Context(ctx context.Context) string {
	var sections []string
	if checkpoint, ok, err := p.c.runCheckpoint(ctx, p.turn.RunID); err == nil && ok {
		sections = append(sections, runCheckpointRecoveryPrompt(checkpoint))
	}
	sections = append(sections, p.c.sessionTodoPrompt(ctx, p.turn.SessionID, append([]string{p.turn.RunID}, p.turn.Continues...)), p.c.runningTasksPrompt(ctx, p.turn.SessionID))
	if p.turn.Subagent {
		return prompt.JoinSections(sections...)
	}
	sections = append(sections, p.c.nativeStatusPrompt(ctx, p.turn, p.toolIDs))
	if memory := p.c.MemoryPromptContext(ctx, p.turn.WorkingDir); memory != p.memory {
		sections = append(sections, memoryChangedPrompt(memory))
	}
	return prompt.JoinSections(sections...)
}
```

In `internal/core/shell_tasks.go`, replace `func (Core) ReadTaskOutput` with:

```go
// ReadTaskOutput returns a task's output since the call's session last read it;
// it waits up to read.Wait for a running task to finish.
func (c *Core) ReadTaskOutput(ctx context.Context, call tools.Call, read tools.TaskRead) (tools.TaskOutput, error) {
	task, err := c.sessionTask(ctx, call.SessionID, read.TaskID)
	if err != nil {
		return tools.TaskOutput{}, err
	}
	var filter *regexp.Regexp
	if strings.TrimSpace(read.Filter) != "" {
		if filter, err = regexp.Compile(read.Filter); err != nil {
			return tools.TaskOutput{}, fmt.Errorf("%w: filter: %v", ErrInvalidInput, err)
		}
	}
	if live, ok := c.liveTask(task.ID); ok && read.Wait > 0 {
		timer := time.NewTimer(read.Wait)
		select {
		case <-live.recorded:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
		if err := ctx.Err(); err != nil {
			return tools.TaskOutput{}, err
		}
		if task, err = c.store.GetTask(ctx, task.ID); err != nil {
			return tools.TaskOutput{}, err
		}
	}
	if task.FinishedAt != nil {
		// The model reads how the task ended here and needs no event about it.
		if err := c.store.MarkTasksDelivered(ctx, []string{task.ID}, call.RunID, c.now().UTC()); err != nil {
			return tools.TaskOutput{}, err
		}
	}
	out := tools.TaskOutput{TaskInfo: taskInfo(task)}
	if task.Kind != TaskKindShell {
		out.Text = firstNonEmpty(task.Summary, task.Error)
		return out, nil
	}
	chunk, err := shelltask.Read(task.OutputPath, task.OutputCursor, read.Limit)
	if err != nil {
		return tools.TaskOutput{}, err
	}
	if err := c.store.SetTaskCursor(ctx, task.ID, chunk.Next); err != nil {
		return tools.TaskOutput{}, err
	}
	out.Text, out.Skipped, out.More = chunk.Text, chunk.Skipped, chunk.More
	if filter != nil {
		out.Text = matchingLines(out.Text, filter)
	}
	return out, nil
}
```

Create `internal/core/task_events.go`:

```go
package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/shelltask"
)

// taskEventTail is how much of a finished command's output its event shows.
const taskEventTail = 2000

// taskEventText tells the model how a background task ended.
func taskEventText(task Task) string {
	if task.Kind == TaskKindSubagent {
		return fmt.Sprintf("Subagent %s (%s) finished: %s.\nGoal: %s\nResult: %s", firstNonEmpty(task.AgentName, task.ID), task.ID, task.Status, task.Command, firstNonEmpty(firstNonEmpty(task.Summary, task.Error), "none"))
	}
	var head string
	switch task.Status {
	case TaskStatusLost:
		head = fmt.Sprintf("Background task %s was lost: %s.", task.ID, task.Error)
	case TaskStatusCanceled:
		head = fmt.Sprintf("Background task %s was stopped: %s.", task.ID, task.Error)
	default:
		head = fmt.Sprintf("Background task %s finished with exit code %d", task.ID, exitCodeOf(task))
		if task.Error != "" {
			head += " (" + task.Error + ")"
		}
		head += "."
	}
	lines := []string{head, "Command: " + task.Command}
	if tail, err := shelltask.Tail(task.OutputPath, taskEventTail); err == nil && strings.TrimSpace(tail) != "" {
		lines = append(lines, "Last output:", strings.TrimRight(tail, "\n"))
	}
	lines = append(lines, "Its whole output is in "+task.OutputPath+"; task_output reads what you have not seen.")
	return strings.Join(lines, "\n")
}

func exitCodeOf(task Task) int {
	if task.ExitCode == nil {
		return -1
	}
	return *task.ExitCode
}

// runningTasksPrompt lists the session's background tasks that still run, for
// the context note; "" when none does.
func (c *Core) runningTasksPrompt(ctx context.Context, sessionID string) string {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: sessionID, Statuses: []TaskStatus{TaskStatusPending, TaskStatusRunning, TaskStatusWaitingApproval}})
	if err != nil {
		return ""
	}
	lines := []string{"Background tasks still running (task_output reads them, task_kill stops them):"}
	for i := len(tasks) - 1; i >= 0; i-- {
		task := tasks[i]
		if !task.Background {
			continue
		}
		label := task.ID
		if task.Kind == TaskKindSubagent {
			label += " (subagent " + firstNonEmpty(task.AgentName, task.Description) + ")"
		}
		lines = append(lines, "- "+label+": "+truncateForTitle(task.Command, 200))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/agent -run '^(TestFinishedTasksReachTheModelAsNotesBeforeItsNextStep)$'
go test ./internal/core -run '^(TestNativeRunSeesFinishedAndRunningTasks)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/agent/agenttest/agenttest.go \
  internal/agent/engine.go \
  internal/agent/events.go \
  internal/agent/events_test.go \
  internal/agent/ports.go \
  internal/core/agent_inbox.go \
  internal/core/agent_prompts.go \
  internal/core/shell_tasks.go \
  internal/core/shell_tasks_test.go \
  internal/core/task_events.go
git commit -m "feat(agent): finished background tasks reach the run as engine notes

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 8: `/tasks` for background tasks

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/sessions.go`
- Create: `internal/api/tasks.go`
- Modify: `internal/clientruntime/controlplane_runtime.go`
- Modify: `internal/controlplane/automation_commands.go`
- Create: `internal/controlplane/background_tasks.go`
- Modify: `internal/controlplane/dispatcher.go`
- Modify: `internal/controlplane/tasks_picker.go`
- Create: `internal/core/tasks.go`
- Modify: `internal/core/types_task.go`
- Create: `internal/daemonclient/tasks.go`
- Test (create): `internal/api/tasks_test.go`
- Test (create): `internal/controlplane/background_tasks_test.go`
- Test (modify): `internal/core/shell_tasks_test.go`

`/tasks` keeps the scheduled tasks and puts the bound session's background tasks first. The controlplane only sees tasks through the new `BackgroundTaskRuntime` (implemented by `clientruntime.ControlplaneRuntime` over the daemon client) and refuses ids of another session with "Task not found.".

- [ ] **Step 1: Write the failing tests**

Create `internal/api/tasks_test.go`:

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

func TestTaskEndpointsListShowAndCancel(t *testing.T) {
	server, st := newAPITestServer(t)
	for _, task := range []core.Task{
		{ID: "task_bg", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true, StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch},
		{ID: "task_inline", SessionID: "s1", Kind: core.TaskKindSubagent, Status: core.TaskStatusRunning, Command: "Look", StartedAt: apiTestEpoch, UpdatedAt: apiTestEpoch},
	} {
		if err := st.CreateTask(context.Background(), task); err != nil {
			t.Fatal(err)
		}
	}
	serve := func(method, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder
	}

	var listed core.SessionTasksResponse
	if got := serve(http.MethodGet, "/v1/sessions/s1/tasks"); got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &listed) != nil || len(listed.Tasks) != 1 || listed.Tasks[0].ID != "task_bg" {
		t.Fatalf("list status=%d body=%s", got.Code, got.Body.String())
	}
	var detail core.TaskDetailResponse
	if got := serve(http.MethodGet, "/v1/tasks/task_bg"); got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &detail) != nil || detail.Task.Command != "npm run dev" {
		t.Fatalf("detail status=%d body=%s", got.Code, got.Body.String())
	}
	var canceled core.TaskResponse
	if got := serve(http.MethodPost, "/v1/tasks/task_bg/cancel"); got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &canceled) != nil || canceled.Task.Status != core.TaskStatusCanceled {
		t.Fatalf("cancel status=%d body=%s", got.Code, got.Body.String())
	}
	for path, want := range map[string]int{"/v1/tasks/task_gone": http.StatusNotFound, "/v1/sessions/gone/tasks": http.StatusNotFound} {
		if got := serve(http.MethodGet, path); got.Code != want {
			t.Errorf("GET %s status=%d, want %d", path, got.Code, want)
		}
	}
	if got := serve(http.MethodGet, "/v1/tasks/task_bg/cancel"); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET cancel status=%d", got.Code)
	}
}
```

Create `internal/controlplane/background_tasks_test.go`:

```go
package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

type backgroundTaskRuntime struct {
	tokenReportRuntime
	tasks    []core.Task
	canceled []string
}

func (r *backgroundTaskRuntime) SessionTasks(_ context.Context, sessionID string) ([]core.Task, error) {
	var out []core.Task
	for _, task := range r.tasks {
		if task.SessionID == sessionID {
			out = append(out, task)
		}
	}
	return out, nil
}

func (r *backgroundTaskRuntime) TaskDetail(_ context.Context, taskID string) (core.TaskDetailResponse, error) {
	for _, task := range r.tasks {
		if task.ID == taskID {
			return core.TaskDetailResponse{Task: task, OutputTail: "listening on :3000\n"}, nil
		}
	}
	return core.TaskDetailResponse{}, core.ErrNotFound
}

func (r *backgroundTaskRuntime) CancelTask(_ context.Context, taskID string) (core.Task, error) {
	r.canceled = append(r.canceled, taskID)
	return core.Task{ID: taskID, Kind: core.TaskKindShell, Command: "npm run dev", Status: core.TaskStatusCanceled}, nil
}

func newBackgroundTaskRuntime() *backgroundTaskRuntime {
	code := 1
	finished := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	return &backgroundTaskRuntime{tasks: []core.Task{
		{ID: "task_dev", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "npm run dev", Background: true},
		{ID: "task_test", SessionID: "s1", Kind: core.TaskKindShell, Status: core.TaskStatusFailed, Command: "go test ./...", ExitCode: &code, Background: true, FinishedAt: &finished},
		{ID: "task_other", SessionID: "s2", Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make", Background: true},
	}}
}

func TestTasksListsTheSessionsBackgroundTasks(t *testing.T) {
	result, err := New(newBackgroundTaskRuntime(), "").Handle(context.Background(), "key", "/tasks")

	if err != nil || result.Picker == nil || len(result.Picker.Items) != 2 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	first, second := result.Picker.Items[0], result.Picker.Items[1]
	if first.Title != "npm run dev" || first.Info != "running" || first.Command != "/tasks bg task_dev" || second.Info != "failed, exit 1" {
		t.Fatalf("items = %+v", result.Picker.Items)
	}
}

func TestBackgroundTaskShowsOutputAndStopsAfterConfirmation(t *testing.T) {
	runtime := newBackgroundTaskRuntime()
	dispatcher := New(runtime, "")

	actions, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_dev")
	if err != nil || actions.Picker == nil || len(actions.Picker.Items) != 2 || actions.Picker.Items[1].Command != "/tasks bg task_dev stop" {
		t.Fatalf("actions = %+v, %v", actions, err)
	}
	output, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_dev output")
	if err != nil || output.Info == nil || output.Info.Text != "listening on :3000" {
		t.Fatalf("output = %+v, %v", output, err)
	}
	asked, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_dev stop")
	if err != nil || asked.Confirm == nil || len(runtime.canceled) != 0 {
		t.Fatalf("asked = %+v, %v", asked, err)
	}
	stopped, err := dispatcher.Handle(context.Background(), "key", asked.Confirm.ConfirmCommand)
	if err != nil || !strings.Contains(stopped.Text, "canceled") || len(runtime.canceled) != 1 || runtime.canceled[0] != "task_dev" {
		t.Fatalf("stopped = %+v, %v, canceled %v", stopped, err, runtime.canceled)
	}

	finished, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_test")
	if err != nil || finished.Picker == nil || len(finished.Picker.Items) != 1 {
		t.Fatalf("a finished task offers %+v, %v", finished, err)
	}
	other, err := dispatcher.Handle(context.Background(), "key", "/tasks bg task_other stop confirm")
	if err != nil || other.Text != "Task not found." || len(runtime.canceled) != 1 {
		t.Fatalf("another session's task = %+v, %v", other, err)
	}
}
```

In `internal/core/shell_tasks_test.go`, add:

```go
func TestUserStopsATaskAndTheSessionIsTold(t *testing.T) {
	t.Parallel()
	app, db, session, _ := newTaskCore(t)
	command := foreground("echo up; sleep 30")
	command.Background = true
	result, err := app.RunCommand(context.Background(), taskCall(session), command)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListSessionTasks(context.Background(), session.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != result.TaskID {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if detail, err := app.TaskDetail(context.Background(), result.TaskID); err == nil && detail.OutputTail == "up\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task wrote no output")
		}
	}

	task, err := app.CancelTask(context.Background(), result.TaskID)
	if err != nil || task.Status != core.TaskStatusCanceled {
		t.Fatalf("cancel = %+v, %v", task, err)
	}
	waitProcessGone(t, task.PID)
	events, err := db.ListTasks(context.Background(), core.TaskFilter{SessionID: session.ID, Undelivered: true})
	if err != nil || len(events) != 1 || events[0].Error != "stopped by the user" {
		t.Fatalf("events = %+v, %v", events, err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/api -run '^(TestTaskEndpointsListShowAndCancel)$'
go test ./internal/controlplane -run '^(TestTasksListsTheSessionsBackgroundTasks|TestBackgroundTaskShowsOutputAndStopsAfterConfirmation)$'
go test ./internal/core -run '^(TestUserStopsATaskAndTheSessionIsTold)$'
```

Expected: build fails (`ListSessionTasks`, `TaskDetail`, `CancelTask` undefined).

- [ ] **Step 3: Implement**

In `internal/api/server.go`, replace `func (Server) routes` with:

```go
func (s *Server) routes() {
	s.mux.HandleFunc("/v1/health", s.handleHealth)
	s.mux.HandleFunc("/v1/server/status", s.handleServerStatus)
	s.mux.HandleFunc("/v1/setup/providers", s.handleSetupProviders)
	s.mux.HandleFunc("/v1/setup/providers/", s.handleSetupProviderByID)
	s.mux.HandleFunc("/v1/session-providers", s.handleSessionProviders)
	s.mux.HandleFunc("/v1/external-agents", s.handleExternalAgents)
	s.mux.HandleFunc("/v1/external-agents/", s.handleExternalAgentByID)
	s.mux.HandleFunc("/v1/admin/reload", s.handleAdminReload)
	s.mux.HandleFunc("/v1/admin/restart", s.handleAdminRestart)
	s.mux.HandleFunc("/v1/admin/stop", s.handleAdminStop)
	s.mux.HandleFunc("/v1/automation/jobs", s.handleAutomationJobs)
	s.mux.HandleFunc("/v1/automation/jobs/", s.handleAutomationJobByID)
	s.mux.HandleFunc("/v1/client-deliveries", s.handleClientDeliveries)
	s.mux.HandleFunc("/v1/client-deliveries/", s.handleClientDeliveryByID)
	s.mux.HandleFunc("/v1/sessions", s.handleSessions)
	s.mux.HandleFunc("/v1/sessions/", s.handleSessionByID)
	s.mux.HandleFunc("/v1/bindings/current", s.handleCurrentBinding)
	s.mux.HandleFunc("/v1/bindings/use", s.handleUseBinding)
	s.mux.HandleFunc("/v1/tools", s.handleTools)
	s.mux.HandleFunc("/v1/tools/execute", s.handleToolExecute)
	s.mux.HandleFunc("/v1/modules/storage/files", s.handleStorageFiles)
	s.mux.HandleFunc("/v1/modules/storage/files/", s.handleStorageFileByPath)
	s.mux.HandleFunc("/v1/modules/storage/temp", s.handleStorageTemp)
	s.mux.HandleFunc("/v1/modules/storage/temp/", s.handleStorageTempByPath)
	s.mux.HandleFunc("/v1/modules/voice", s.handleVoiceModules)
	s.mux.HandleFunc("/v1/modules/voice/realtime_voice", s.handleRealtimeVoiceModule)
	s.mux.HandleFunc("/v1/modules/voice/", s.handleVoiceModuleByID)
	s.mux.HandleFunc("/v1/realtime-voice/sessions", s.handleRealtimeVoiceSessions)
	s.mux.HandleFunc("/v1/realtime-voice/sessions/", s.handleRealtimeVoiceSessionByID)
	s.mux.HandleFunc("/v1/modules/telephony", s.handleTelephonyModule)
	s.mux.HandleFunc("/v1/modules/web-search", s.handleWebSearch)
	s.mux.HandleFunc("/v1/modules/browser", s.handleBrowserModule)
	s.mux.HandleFunc("/v1/modules/browser/providers/", s.handleBrowserProvider)
	s.mux.HandleFunc("/v1/modules/mcp", s.handleMCP)
	s.mux.HandleFunc("/v1/modules/mcp/", s.handleMCPByID)
	s.mux.HandleFunc("/v1/modules/skills", s.handleSkills)
	s.mux.HandleFunc("/v1/modules/skills/", s.handleSkillByID)
	s.mux.HandleFunc("/v1/approvals", s.handleApprovals)
	s.mux.HandleFunc("/v1/approvals/", s.handleApprovalByID)
	s.mux.HandleFunc("/v1/permission-rules/", s.handlePermissionRuleByID)
	s.mux.HandleFunc("/v1/events", s.handleEvents)
	s.mux.HandleFunc("/v1/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("/v1/search", s.handleSearch)
	s.mux.HandleFunc("/v1/memory", s.handleMemory)
	s.mux.HandleFunc("/v1/messages", s.handleMessages)
	s.mux.HandleFunc("/v1/runs/", s.handleRunByID)
	s.mux.HandleFunc("/v1/tasks/", s.handleTaskByID)
}
```

In `internal/api/sessions.go`, replace `func (Server) handleSessionByID` with:

```go
func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"))
	if path == "" {
		writeNotFound(w)
		return
	}
	childRoutes := []struct {
		suffix string
		handle func(http.ResponseWriter, *http.Request, string)
	}{
		{suffix: "/llm", handle: s.handleSessionLLMUpdate},
		{suffix: "/permissions", handle: s.handleSessionPermissionsUpdate},
		{suffix: "/permission-rules", handle: s.handleSessionPermissionRules},
		{suffix: "/models", handle: s.handleSessionLLMModels},
		{suffix: "/context", handle: s.handleSessionContext},
		{suffix: "/usage", handle: s.handleSessionUsage},
		{suffix: "/budget", handle: s.handleSessionBudget},
		{suffix: "/todo", handle: s.handleSessionTodo},
		{suffix: "/tasks", handle: s.handleSessionTasks},
		{suffix: "/compact", handle: s.handleSessionCompact},
		{suffix: "/clear", handle: s.handleSessionClear},
		{suffix: "/system-message", handle: s.handleSessionSystemMessage},
	}
	for _, route := range childRoutes {
		sessionID, matched, ok := sessionChildID(path, route.suffix)
		if !matched {
			continue
		}
		if !ok {
			writeNotFound(w)
			return
		}
		route.handle(w, r, sessionID)
		return
	}

	sessionID := path
	if strings.Contains(sessionID, "/") {
		writeNotFound(w)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var req core.RenameSessionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}

		session, err := s.core.RenameSession(r.Context(), core.RenameSessionInput{
			SessionID: sessionID,
			Title:     req.Title,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.SessionResponse{Session: session})
	case http.MethodDelete:
		if err := s.core.DeleteSession(r.Context(), sessionID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeMethodNotAllowed(w, http.MethodPatch, http.MethodDelete)
	}
}
```

Create `internal/api/tasks.go`:

```go
package api

import (
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (s *Server) handleSessionTasks(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	tasks, err := s.core.ListSessionTasks(r.Context(), sessionID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, core.SessionTasksResponse{Tasks: tasks})
}

// handleTaskByID serves GET /v1/tasks/{id} and POST /v1/tasks/{id}/cancel.
func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	taskID, action, _ := strings.Cut(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/"), "/")
	switch {
	case taskID == "":
		writeNotFound(w)
	case action == "":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		detail, err := s.core.TaskDetail(r.Context(), taskID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	case action == "cancel":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		task, err := s.core.CancelTask(r.Context(), taskID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, core.TaskResponse{Task: task})
	default:
		writeNotFound(w)
	}
}
```

In `internal/clientruntime/controlplane_runtime.go`, add:

```go
func (r ControlplaneRuntime) SessionTasks(ctx context.Context, sessionID string) ([]core.Task, error) {
	client, err := r.client("")
	if err != nil {
		return nil, err
	}
	return client.SessionTasks(ctx, sessionID)
}
```

In `internal/clientruntime/controlplane_runtime.go`, add:

```go
func (r ControlplaneRuntime) TaskDetail(ctx context.Context, taskID string) (core.TaskDetailResponse, error) {
	client, err := r.client("")
	if err != nil {
		return core.TaskDetailResponse{}, err
	}
	return client.TaskDetail(ctx, taskID)
}
```

In `internal/clientruntime/controlplane_runtime.go`, add:

```go
func (r ControlplaneRuntime) CancelTask(ctx context.Context, taskID string) (core.Task, error) {
	client, err := r.client("")
	if err != nil {
		return core.Task{}, err
	}
	return client.CancelTask(ctx, taskID)
}
```

In `internal/controlplane/automation_commands.go`, replace `func (Dispatcher) handleTasks` with:

```go
func (d *Dispatcher) handleTasks(ctx context.Context, externalKey string, args string) (Result, error) {
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.tasksPicker(ctx, externalKey)
	case "bg":
		return d.handleBackgroundTask(ctx, externalKey, rest)
	}
	if d.automation == nil {
		return unsupportedRuntime("tasks"), nil
	}
	switch step {
	case "add":
		return d.handleTaskAdd(ctx, externalKey, rest)
	case "archive":
		return d.tasksArchivePicker(ctx)
	case "menu":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return d.tasksPicker(ctx, externalKey)
		}
		return d.taskActionsPicker(ctx, jobID)
	case "pause":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks pause <id>"}, nil
		}
		job, err := d.automation.PauseAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Paused: " + formatAutomationJob(job)}, nil
	case "resume":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks resume <id>"}, nil
		}
		job, err := d.automation.ResumeAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Resumed: " + formatAutomationJob(job)}, nil
	case "complete":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks complete <id>"}, nil
		}
		job, err := d.automation.CompleteAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Archived: " + formatAutomationJob(job)}, nil
	case "delete":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks delete <id>"}, nil
		}
		return Result{
			Handled: true,
			Confirm: deleteConfirmData("Delete task?", taskDeleteConfirmCommand(jobID), taskMenuCommand(jobID)),
		}, nil
	case "delete-confirm":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks delete-confirm <id>"}, nil
		}
		job, err := d.automation.DeleteAutomationJob(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Deleted: " + formatAutomationJob(job)}, nil
	case "delete-closed":
		return Result{
			Handled: true,
			Confirm: deleteConfirmData("Delete completed tasks?", tasksDeleteClosedConfirmCommand(), tasksArchiveCommand()),
		}, nil
	case "delete-closed-confirm":
		return d.deleteClosedTasks(ctx)
	case "run":
		jobID, _ := firstCommandToken(rest)
		if jobID == "" {
			return Result{Handled: true, Text: "Usage: /tasks run <id>"}, nil
		}
		fire, err := d.automation.RunAutomationJobNow(ctx, jobID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Task started: " + fire.RunID}, nil
	default:
		return Result{Handled: true, Text: "Usage:\n/tasks\n/tasks bg <id> [output|stop]\n/tasks add once 2026-04-26 16:00 -- prompt\n/tasks add cron \"0 10 1 * *\" -- prompt\n/tasks complete <id>\n/tasks delete <id>\n/tasks run <id>"}, nil
	}
}
```

Create `internal/controlplane/background_tasks.go`:

```go
package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

// maxPickedTasks bounds the background tasks /tasks lists.
const maxPickedTasks = 10

// backgroundTaskItems lists the bound session's background tasks for /tasks.
func (d *Dispatcher) backgroundTaskItems(ctx context.Context, externalKey string) ([]PickerItem, error) {
	if d.tasks == nil {
		return nil, nil
	}
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil || sessionID == "" {
		return nil, err
	}
	tasks, err := d.tasks.SessionTasks(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, 0, min(len(tasks), maxPickedTasks))
	for _, task := range tasks[:min(len(tasks), maxPickedTasks)] {
		items = append(items, PickerItem{
			ID:      "bg:" + task.ID,
			Title:   backgroundTaskTitle(task),
			Info:    backgroundTaskStatus(task),
			Command: tasksCommand("bg", task.ID),
		})
	}
	return items, nil
}

// handleBackgroundTask serves /tasks bg <id> [output | stop [confirm]].
func (d *Dispatcher) handleBackgroundTask(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.tasks == nil {
		return unsupportedRuntime("background task"), nil
	}
	taskID, action := firstCommandToken(args)
	if taskID == "" {
		return Result{Handled: true, Text: "Usage: /tasks bg <id> [output|stop]"}, nil
	}
	detail, err := d.tasks.TaskDetail(ctx, taskID)
	if errors.Is(err, core.ErrNotFound) {
		return Result{Handled: true, Text: "Task not found."}, nil
	}
	if err != nil {
		return Result{}, err
	}
	sessionID, err := d.currentSessionID(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	task := detail.Task
	if task.SessionID != sessionID {
		return Result{Handled: true, Text: "Task not found."}, nil
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "":
		items := []PickerItem{{ID: "output", Title: "Output", Info: backgroundTaskStatus(task), Command: tasksCommand("bg", task.ID, "output")}}
		if !backgroundTaskFinished(task) {
			items = append(items, PickerItem{ID: "stop", Title: "Stop", Command: tasksCommand("bg", task.ID, "stop"), Role: PickerItemRoleDanger})
		}
		return Result{Handled: true, Picker: NewPickerData(PickerTaskActions, backgroundTaskTitle(task)).Context(task.ID).Back(tasksCommand()).Items(items...).Ptr()}, nil
	case "output":
		info := backgroundTaskInfo(detail)
		return Result{Handled: true, Info: &info}, nil
	case "stop":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Stop " + backgroundTaskTitle(task) + "?",
			ConfirmLabel:   "Stop",
			CancelLabel:    "Close",
			ConfirmCommand: tasksCommand("bg", task.ID, "stop", "confirm"),
			CancelCommand:  tasksCommand("bg", task.ID),
			ConfirmDanger:  true,
		}}, nil
	case "stop confirm":
		stopped, err := d.tasks.CancelTask(ctx, task.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: fmt.Sprintf("%s: %s.", backgroundTaskTitle(stopped), backgroundTaskStatus(stopped))}, nil
	default:
		return Result{Handled: true, Text: "Usage: /tasks bg <id> [output|stop]"}, nil
	}
}

func backgroundTaskTitle(task core.Task) string {
	if task.Kind == core.TaskKindSubagent {
		return "Subagent " + firstNonEmptyTrimmed(task.AgentName, task.ID)
	}
	return truncateTaskText(firstNonEmptyTrimmed(task.Description, task.Command), 48)
}

func backgroundTaskStatus(task core.Task) string {
	status := string(task.Status)
	if task.ExitCode != nil && *task.ExitCode != 0 {
		status += fmt.Sprintf(", exit %d", *task.ExitCode)
	}
	return status
}

func backgroundTaskFinished(task core.Task) bool {
	return task.FinishedAt != nil
}

func backgroundTaskInfo(detail core.TaskDetailResponse) InfoData {
	task := detail.Task
	rows := []InfoRow{
		{Label: "Task", Value: task.ID},
		{Label: "Status", Value: backgroundTaskStatus(task)},
		{Label: "Command", Value: task.Command},
	}
	if task.OutputPath != "" {
		rows = append(rows, InfoRow{Label: "Output file", Value: task.OutputPath})
	}
	text := strings.TrimSpace(detail.OutputTail)
	if task.Kind == core.TaskKindSubagent {
		text = firstNonEmptyTrimmed(task.Summary, task.Error)
	}
	if text == "" {
		text = "No output yet."
	}
	return InfoData{Title: backgroundTaskTitle(task), Text: text, Rows: rows, CloseCommand: tasksCommand("bg", task.ID)}
}
```

In `internal/controlplane/dispatcher.go`, replace `type Dispatcher` with:

```go
type Dispatcher struct {
	configured     bool
	sessions       SessionRuntime
	sessionModels  SessionModelRuntime
	externalAgents ExternalAgentRuntime
	voiceModules   VoiceModuleRuntime
	realtimeVoice  RealtimeVoiceRuntime
	telephony      TelephonyRuntime
	browserModules BrowserModuleRuntime
	providers      ProviderRuntime
	permissions    PermissionRuntime
	rules          PermissionRuleRuntime
	approvals      ApprovalRuntime
	messages       SessionMessageRuntime
	sender         SessionSendRuntime
	continuer      ContinueRuntime
	contextRuntime ContextRuntime
	usage          UsageRuntime
	budget         BudgetRuntime
	todo           TodoRuntime
	tasks          BackgroundTaskRuntime
	memory         MemoryRuntime
	search         SearchRuntime
	storage        StorageRuntime
	automation     AutomationRuntime
	server         ServerRuntime
	webSearch      WebSearchRuntime
	skills         SkillsRuntime
	mcp            MCPRuntime
	workingDir     string
	now            func() time.Time
}
```

In `internal/controlplane/dispatcher.go`, replace `func New` with:

```go
func New(runtime any, workingDir string) *Dispatcher {
	d := &Dispatcher{
		configured: runtime != nil,
		workingDir: strings.TrimSpace(workingDir),
		now:        time.Now,
	}
	if runtime != nil {
		d.sessions, _ = runtime.(SessionRuntime)
		d.sessionModels, _ = runtime.(SessionModelRuntime)
		d.externalAgents, _ = runtime.(ExternalAgentRuntime)
		d.voiceModules, _ = runtime.(VoiceModuleRuntime)
		d.realtimeVoice, _ = runtime.(RealtimeVoiceRuntime)
		d.telephony, _ = runtime.(TelephonyRuntime)
		d.browserModules, _ = runtime.(BrowserModuleRuntime)
		d.providers, _ = runtime.(ProviderRuntime)
		d.permissions, _ = runtime.(PermissionRuntime)
		d.rules, _ = runtime.(PermissionRuleRuntime)
		d.approvals, _ = runtime.(ApprovalRuntime)
		d.messages, _ = runtime.(SessionMessageRuntime)
		d.sender, _ = runtime.(SessionSendRuntime)
		d.continuer, _ = runtime.(ContinueRuntime)
		d.contextRuntime, _ = runtime.(ContextRuntime)
		d.usage, _ = runtime.(UsageRuntime)
		d.budget, _ = runtime.(BudgetRuntime)
		d.todo, _ = runtime.(TodoRuntime)
		d.tasks, _ = runtime.(BackgroundTaskRuntime)
		d.memory, _ = runtime.(MemoryRuntime)
		d.search, _ = runtime.(SearchRuntime)
		d.storage, _ = runtime.(StorageRuntime)
		d.automation, _ = runtime.(AutomationRuntime)
		d.server, _ = runtime.(ServerRuntime)
		d.webSearch, _ = runtime.(WebSearchRuntime)
		d.skills, _ = runtime.(SkillsRuntime)
		d.mcp, _ = runtime.(MCPRuntime)
	}
	return d
}
```

In `internal/controlplane/dispatcher.go`, add:

```go
// BackgroundTaskRuntime reads and stops the background tasks of sessions.
type BackgroundTaskRuntime interface {
	SessionTasks(ctx context.Context, sessionID string) ([]core.Task, error)
	TaskDetail(ctx context.Context, taskID string) (core.TaskDetailResponse, error)
	CancelTask(ctx context.Context, taskID string) (core.Task, error)
}
```

In `internal/controlplane/tasks_picker.go`, replace `func (Dispatcher) tasksPicker` with:

```go
// tasksPicker lists the bound session's background tasks, then the scheduled tasks.
func (d *Dispatcher) tasksPicker(ctx context.Context, externalKey string) (Result, error) {
	if d.automation == nil && d.tasks == nil {
		return unsupportedRuntime("tasks"), nil
	}
	items, err := d.backgroundTaskItems(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if d.automation == nil {
		if len(items) == 0 {
			return Result{Handled: true, Text: "No background tasks."}, nil
		}
		return Result{Handled: true, Picker: NewPickerData(PickerTasks, "Tasks").Items(items...).Ptr()}, nil
	}
	jobs, err := d.automation.ListAutomationJobs(ctx)
	if err != nil {
		return Result{}, err
	}
	active, closed := splitAutomationJobs(jobs)
	for _, job := range active {
		items = append(items, PickerItem{
			ID:      "open:" + job.ID,
			Title:   taskListTitle(job),
			Info:    taskListInfo(job),
			Command: taskMenuCommand(job.ID),
		})
	}
	archiveTitle := "Archive"
	archiveInfo := "Completed tasks"
	if len(closed) > 0 {
		archiveTitle = "Archive"
		archiveInfo = fmt.Sprintf("%d completed", len(closed))
	}
	items = append(items, PickerItem{ID: "archive", Title: archiveTitle, Info: archiveInfo, Command: tasksArchiveCommand()})
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerTasks, "Tasks").Items(items...).Ptr(),
	}, nil
}
```

Create `internal/core/tasks.go`:

```go
package core

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/shelltask"
)

// maxListedTasks bounds how many of a session's tasks ListSessionTasks returns.
const maxListedTasks = 30

// taskOutputTail is how much output TaskDetail shows.
const taskOutputTail = 3000

// ListSessionTasks lists the session's background tasks, newest first.
func (c *Core) ListSessionTasks(ctx context.Context, sessionID string) ([]Task, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return nil, err
	}
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: session.ID, Limit: maxListedTasks})
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		if task.Background {
			out = append(out, task)
		}
	}
	return out, nil
}

// TaskDetail returns a task and the end of its output.
func (c *Core) TaskDetail(ctx context.Context, taskID string) (TaskDetailResponse, error) {
	task, err := c.store.GetTask(ctx, normalizeText(taskID))
	if err != nil {
		return TaskDetailResponse{}, err
	}
	detail := TaskDetailResponse{Task: task}
	if task.OutputPath != "" {
		if tail, err := shelltask.Tail(task.OutputPath, taskOutputTail); err == nil {
			detail.OutputTail = tail
		}
	}
	return detail, nil
}

// CancelTask stops a task for the user; its session reads that at its next step.
func (c *Core) CancelTask(ctx context.Context, taskID string) (Task, error) {
	task, err := c.store.GetTask(ctx, normalizeText(taskID))
	if err != nil {
		return Task{}, err
	}
	if !task.Background {
		return Task{}, fmt.Errorf("%w: task %s is not a background task", ErrInvalidInput, task.ID)
	}
	return c.cancelTask(ctx, task, "stopped by the user")
}
```

In `internal/core/types_task.go`, add:

```go
// SessionTasksResponse lists a session's background tasks.
type SessionTasksResponse struct {
	Tasks []Task `json:"tasks"`
}
```

In `internal/core/types_task.go`, add:

```go
// TaskResponse carries one task.
type TaskResponse struct {
	Task Task `json:"task"`
}
```

In `internal/core/types_task.go`, add:

```go
// TaskDetailResponse is a task and the end of its output.
type TaskDetailResponse struct {
	Task       Task   `json:"task"`
	OutputTail string `json:"output_tail,omitempty"`
}
```

Create `internal/daemonclient/tasks.go`:

```go
package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) SessionTasks(ctx context.Context, sessionID string) ([]core.Task, error) {
	var response core.SessionTasksResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions/"+escapedPath(sessionID)+"/tasks", nil, &response); err != nil {
		return nil, err
	}
	return response.Tasks, nil
}

func (c *Client) TaskDetail(ctx context.Context, taskID string) (core.TaskDetailResponse, error) {
	var response core.TaskDetailResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tasks/"+escapedPath(taskID), nil, &response); err != nil {
		return core.TaskDetailResponse{}, err
	}
	return response, nil
}

func (c *Client) CancelTask(ctx context.Context, taskID string) (core.Task, error) {
	var response core.TaskResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/tasks/"+escapedPath(taskID)+"/cancel", nil, &response); err != nil {
		return core.Task{}, err
	}
	return response.Task, nil
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/api -run '^(TestTaskEndpointsListShowAndCancel)$'
go test ./internal/controlplane -run '^(TestTasksListsTheSessionsBackgroundTasks|TestBackgroundTaskShowsOutputAndStopsAfterConfirmation)$'
go test ./internal/core -run '^(TestUserStopsATaskAndTheSessionIsTold)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add internal/api/server.go \
  internal/api/sessions.go \
  internal/api/tasks.go \
  internal/api/tasks_test.go \
  internal/clientruntime/controlplane_runtime.go \
  internal/controlplane/automation_commands.go \
  internal/controlplane/background_tasks.go \
  internal/controlplane/background_tasks_test.go \
  internal/controlplane/dispatcher.go \
  internal/controlplane/tasks_picker.go \
  internal/core/shell_tasks_test.go \
  internal/core/tasks.go \
  internal/core/types_task.go \
  internal/daemonclient/tasks.go
git commit -m "feat(controlplane): /tasks lists and stops the session's background tasks

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 9: Prompt guidance and spec notes

**Files:**
- Modify: `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`
- Modify: `internal/agent/prompt/guidance.go`
- Test (modify): `internal/agent/prompt/guidance_test.go`

The prompt learns about background commands, and the spec records what was built.

- [ ] **Step 1: Write the failing tests**

In `internal/agent/prompt/guidance_test.go`, add:

```go
func TestToolUseDisciplineRunsLongCommandsInTheBackground(t *testing.T) {
	text := ToolUseDiscipline()
	for _, want := range []string{"run_in_background", "task_output", "task_kill", "after 2 minutes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tool-use discipline lacks %q:\n%s", want, text)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run:

```bash
go test ./internal/agent/prompt -run '^(TestToolUseDisciplineRunsLongCommandsInTheBackground)$'
```

Expected: the guidance test fails: the discipline does not mention `run_in_background`.

- [ ] **Step 3: Implement**

In `docs/superpowers/specs/2026-09-23-long-running-agent-design.md`:

```diff
diff --git a/docs/superpowers/specs/2026-09-23-long-running-agent-design.md b/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
index d401015..5dd7e08 100644
--- a/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
+++ b/docs/superpowers/specs/2026-09-23-long-running-agent-design.md
@@ -537,6 +537,53 @@ runtime, model}`; `runtime`/`model` keep delegation to Codex and Claude Code.
   requests of all native runs, subagents and summaries included. A run holds a
   slot only inside `Generate`, so a parent blocked in `delegate_task` holds none.
 
+### Implementation notes (as built, stage 6a)
+
+- **Table**: `tasks` also keeps what subagents need (`description`,
+  `agent_name`, `background`, `summary`, `error`, `result_message_id`) and
+  `readonly` for stage 6c. `subagent_tasks` rows are copied on open (columns an
+  old database lacks take their defaults; rows of deleted sessions are
+  skipped) and the table is dropped. Blocking/async subagents are
+  `background` 0/1.
+- **Events**: a finished background task with `delivered_at` NULL is the
+  event; `delivered_run_id` names the run that read it. Only
+  `MarkTasksDelivered` writes these columns. A task the parent already knows
+  about (blocking subagents, `task_kill`, a subagent canceled with its parent,
+  `task_output` after the task ended) is delivered when it ends. Queued
+  subagent completions replace `completion_queued_at/delivered_at`.
+- **Engine**: `Inbox.Peek(InputEvent)` returns the session's undelivered
+  tasks in finish order; each step starts by journaling them as `origin:
+  engine` notes (exit code, command, the last 2000 bytes of output and the
+  file path; for subagents the result) and consumes them. Running background
+  tasks are a section of the context note. Subagent completions reach a
+  running parent the same way; an idle parent still gets a triggered run.
+- **Processes** (`internal/shelltask`): `bash -lc` in its own process group,
+  stdout and stderr through a pipe into `<session files>/<session>/tasks/
+  <task>.log` (0600). Past 20 MB the file is rewritten as the first 1 MB, a
+  fixed-width marker with the dropped byte count, and the newest half of the
+  room; reads use output offsets, so a cursor survives the rewrite.
+  Foreground commands write the same file, removed when they finish unless
+  their output was longer than the call returns (30 000 bytes; the result then
+  names the file). A foreground command waits 2 s for commands it left behind
+  to close the output; a background one waits for them.
+- **Bash**: `timeout` and `auto_background_after` are seconds (default 600 and
+  120; timeout at most 3600). A command moved to the background keeps its
+  timeout; one started with `run_in_background` has none. `task_output{id,
+  wait_seconds ≤ 600, filter}` returns at most 30 000 bytes from the task's
+  cursor (a filter keeps matching lines; the cursor still advances);
+  `task_kill{id}` asks for approval like `job_kill` did and also cancels a
+  subagent task. The shell tools are registered with the core as their
+  `tools.ShellTasks`.
+- **Restart**: `RecoverTasks` runs before the workflow worker starts; a
+  leftover leader whose start time (`ps -o lstart=`, no `/proc`) differs from
+  the task's is another process and is not killed. Lost tasks do not start a
+  run; the next run reads them. Deleting a session kills its tasks.
+- **`/tasks`** lists the bound session's background tasks above the scheduled
+  tasks (`/tasks bg <id>` shows the output tail, `stop` asks first); API
+  `GET /v1/sessions/{id}/tasks`, `GET /v1/tasks/{id}`,
+  `POST /v1/tasks/{id}/cancel`. A task the user stops is an event for the
+  session.
+
 ## 5. Providers
 
 - `providers.Request` gains `MaxOutputTokens` (priority: provider config →
```

In `internal/agent/prompt/guidance.go`, replace `func ToolUseDiscipline` with:

```go
// ToolUseDiscipline is the tool-use guidance for models that can call tools.
func ToolUseDiscipline() string {
	return strings.TrimSpace(`Tool use discipline:
- Treat requests to do work as instructions to carry the task through to a verified result. A promise, plan, or successful intermediate tool call is not completion.
- Tool calls you make in one reply run at the same time and may finish in any order; their results come back in the order you made them. Put independent reads, searches and inspections into one reply as parallel calls. A call that needs another call's result or effect goes in a later reply.
- Run commands that take minutes (builds, test suites, servers) with bash run_in_background and go on with other work; read their output with task_output and stop them with task_kill. A foreground command becomes a background task by itself after 2 minutes and is killed after its timeout (10 minutes unless you set one). You are told when a background task finishes.
- Inspect each tool result before deciding the next step. If a tool fails, use its error to correct the request or choose another approach; do not claim success or repeat the same failed call without a reason.
- Continue while useful authorized work remains. Ask a concise question only when missing information or permission actually blocks the next necessary step.
- Track work of three or more steps with todo_write and update it as you go: keep one item in_progress while you work on it and mark it completed as soon as it is done. Skip the list for simple requests.
- In the final reply, report what was accomplished, how it was checked (tests, build, real output), and any failure or remaining blocker honestly.
- Before calling another tool, check whether existing tool results already contain the requested answer; if they do, stop tool use and reply.
- Do not run extra searches, browser snapshots, or verification calls just to improve confidence when the answer is already clear and source-backed.
- If a result is partly useful but has minor uncertainty, answer with that uncertainty instead of repeatedly searching, unless the user asked for exhaustive verification or the sources conflict.
- For simple lookups, prefer one direct path to the answer over parallel or repeated searches.`)
}
```

- [ ] **Step 4: Run the tests**

Run:

```bash
go test ./internal/agent/prompt -run '^(TestToolUseDisciplineRunsLongCommandsInTheBackground)$'
```

Expected: PASS.

- [ ] **Step 5: Full check and commit**

```bash
gofmt -l ./internal ./clients   # prints nothing
go build ./... && go vet ./... && go test ./...
git status --short   # stage only the paths below
git add docs/superpowers/specs/2026-09-23-long-running-agent-design.md \
  internal/agent/prompt/guidance.go \
  internal/agent/prompt/guidance_test.go
git commit -m "docs: stage 6a as built; prompt asks for background commands

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```


### Task 10: Verification

- [ ] **Step 1: Race and dead code**

```bash
go test -race ./internal/core ./internal/shelltask ./internal/tools ./internal/agent/... ./internal/store ./internal/controlplane ./internal/api
deadcode -test ./... > /tmp/dc-6a.txt; git stash -q; deadcode -test ./... > /tmp/dc-base.txt; git stash pop -q
diff <(sed 's/:[0-9]*:[0-9]*//' /tmp/dc-base.txt | sort) <(sed 's/:[0-9]*:[0-9]*//' /tmp/dc-6a.txt | sort)
```

(Run the `deadcode` comparison against the commit before Task 1 instead of a stash if the tree is clean: `git worktree add /tmp/base <sha-before-task-1>`.) Expected: race tests pass; no new dead code.

- [ ] **Step 2: Manual check on the test stand** (not the production daemon): in a session ask the agent to run `sleep 150; echo done` in the foreground — after 2 minutes the call returns a task id; ask it to run `npm`-like long work with `run_in_background`, then `/tasks` shows it with Output/Stop; stop the stand's daemon while a task runs and start it again — the task is `lost` and the next message's run mentions it.
