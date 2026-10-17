package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	result, err := registry.Execute(context.Background(), name, Call{WorkingDir: "/work", Args: json.RawMessage(args)})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBashAppliesTimeoutAndAutoBackgroundDefaults(t *testing.T) {
	tasks := &fakeShellTasks{result: CommandResult{Output: "ok\n"}}

	result := runShellTool(t, tasks, "bash", `{"command":"go test ./..."}`)
	runShellTool(t, tasks, "bash", `{"command":"make","timeout":3600,"auto_background_after":30,"run_in_background":true}`)
	runShellTool(t, tasks, "bash", `{"command":"make","timeout":120000,"auto_background_after":30000}`)

	if result.Content != "ok\n\nExit code: 0" || result.IsError() {
		t.Fatalf("result = %+v", result)
	}
	want := []Command{
		{Command: "go test ./...", WorkingDir: "/work", Timeout: 10 * time.Minute, AutoBackground: 2 * time.Minute, OutputLimit: maxToolOutput},
		{Command: "make", WorkingDir: "/work", Background: true, Timeout: time.Hour, AutoBackground: 30 * time.Second, OutputLimit: maxToolOutput},
		{Command: "make", WorkingDir: "/work", Timeout: 2 * time.Minute, AutoBackground: 30 * time.Second, OutputLimit: maxToolOutput},
	}
	for i := range want {
		if tasks.commands[i] != want[i] {
			t.Errorf("command %d = %+v, want %+v", i, tasks.commands[i], want[i])
		}
	}
	if tooLong := runShellTool(t, tasks, "bash", `{"command":"make","timeout":3600001}`); !tooLong.IsError() || len(tasks.commands) != 3 {
		t.Fatalf("a timeout over an hour ran: %+v", tooLong)
	}
}

func TestBashStatesTheExitCodeOfEveryCommand(t *testing.T) {
	for _, tc := range []struct {
		command string
		result  CommandResult
		content string
		status  ResultStatus
	}{
		{"go test ./...", CommandResult{Output: "ok\n"}, "ok\n\nExit code: 0", ""},
		{"test -f missing", CommandResult{ExitCode: 1}, "(no output)\n\nExit code: 1", ResultStatusNeutral},
		{"ps aux | grep '[s]erver'", CommandResult{ExitCode: 1}, "(no output)\n\nExit code: 1", ResultStatusNeutral},
		{"git grep -n TODO", CommandResult{ExitCode: 1}, "(no output)\n\nExit code: 1", ResultStatusNeutral},
		{"go vet ./...", CommandResult{ExitCode: 1}, "(no output)\n\nExit code: 1", ResultStatusError},
		{"echo grep; false", CommandResult{Output: "grep\n", ExitCode: 1}, "grep\n\nExit code: 1", ResultStatusError},
		{"kill -9 $$", CommandResult{ExitCode: -1}, "(no output)\n\nExit code: -1 (ended by a signal)", ResultStatusError},
		{"npm run dev &", CommandResult{Output: "ready\n", KilledLeftovers: true}, "ready\n\nExit code: 0\n\nProcesses the command left running were killed; start servers and watchers with run_in_background: true.", ""},
	} {
		args, _ := json.Marshal(BashParams{Command: tc.command})
		result := runShellTool(t, &fakeShellTasks{result: tc.result}, "bash", string(args))
		if result.Content != tc.content || result.Status != tc.status {
			t.Errorf("%q: content %q status %q, want %q %q", tc.command, result.Content, result.Status, tc.content, tc.status)
		}
	}
}

func TestBashKeysOnTheDirectoryItRunsIn(t *testing.T) {
	registry := NewRegistry(NewShellExecutors(&fakeShellTasks{})...)
	key := registry.ConcurrencyKey("bash", Call{WorkingDir: "/work", Args: json.RawMessage(`{"command":"make","working_dir":"sub"}`)})
	if key != WorkingDirKey("/work/sub") {
		t.Fatalf("key = %q", key)
	}
}

func TestBashReportsTheTaskACommandWentOnAs(t *testing.T) {
	tasks := &fakeShellTasks{result: CommandResult{TaskID: "task_1"}}

	moved := runShellTool(t, tasks, "bash", `{"command":"npm test"}`)
	started := runShellTool(t, tasks, "bash", `{"command":"npm run dev","run_in_background":true}`)

	if !strings.Contains(moved.Content, "still running after 120s, so it moved to the background as task task_1") || moved.Metadata.(BashResponseMetadata).TaskID != "task_1" {
		t.Fatalf("moved = %+v", moved)
	}
	if !strings.HasPrefix(started.Content, "Background task task_1 started.") {
		t.Fatalf("started = %+v", started)
	}
}

func TestBashReportsTimeoutsAndCutOutput(t *testing.T) {
	timedOut := runShellTool(t, &fakeShellTasks{result: CommandResult{Output: "partial", TimedOut: true, ExitCode: -1}}, "bash", `{"command":"sleep 99","timeout":5}`)
	if !timedOut.IsError() || timedOut.Content != "partial\n\nTimed out after 5s and killed. Run long commands with run_in_background: true." {
		t.Fatalf("timed out = %+v", timedOut)
	}
	cut := runShellTool(t, &fakeShellTasks{result: CommandResult{Output: "head\n[... 10 bytes of output left out ...]\ntail", OutputPath: "/data/task_1.log"}}, "bash", `{"command":"seq 1 1000000"}`)
	if cut.OutputPath != "/data/task_1.log" || !strings.Contains(cut.Content, "full output is in /data/task_1.log") || !strings.HasSuffix(cut.Content, "Exit code: 0") {
		t.Fatalf("cut = %+v", cut)
	}
}

func TestTaskOutputShowsNewOutputAndStatus(t *testing.T) {
	code := 1
	tasks := &fakeShellTasks{output: TaskOutput{TaskInfo: TaskInfo{TaskID: "task_1", Status: "failed", ExitCode: &code, OutputPath: "/data/task_1.log"}, Text: "FAIL x\n", Skipped: 10, More: true}}

	result := runShellTool(t, tasks, "task_output", `{"id":"task_1","wait_seconds":30,"filter":"FAIL"}`)

	want := "[... 10 bytes of output dropped ...]\n\nFAIL x\n\n(task failed, exit code 1)\n\n(more output is waiting: call task_output again, or read /data/task_1.log)"
	if result.Content != want || result.IsError() {
		t.Fatalf("content = %q", result.Content)
	}
	if read := tasks.reads[0]; read != (TaskRead{TaskID: "task_1", Wait: 30 * time.Second, Filter: "FAIL", Limit: maxToolOutput}) {
		t.Fatalf("read = %+v", read)
	}
}

func TestOnlyAQuietWaitOnARunningTaskIsMarkedWaiting(t *testing.T) {
	running := TaskInfo{TaskID: "task_1", Status: "running"}
	quiet := &fakeShellTasks{output: TaskOutput{TaskInfo: running, Running: true}}
	if waited := runShellTool(t, quiet, "task_output", `{"id":"task_1","wait_seconds":600}`); !waited.Waiting {
		t.Fatalf("a quiet wait = %+v", waited)
	}
	if polled := runShellTool(t, quiet, "task_output", `{"id":"task_1"}`); polled.Waiting {
		t.Fatalf("a poll without waiting = %+v", polled)
	}
	talking := &fakeShellTasks{output: TaskOutput{TaskInfo: running, Running: true, Text: "ok 12/40\n"}}
	if read := runShellTool(t, talking, "task_output", `{"id":"task_1","wait_seconds":600}`); read.Waiting {
		t.Fatalf("new output = %+v", read)
	}
	done := &fakeShellTasks{output: TaskOutput{TaskInfo: TaskInfo{TaskID: "task_1", Status: "completed"}}}
	if ended := runShellTool(t, done, "task_output", `{"id":"task_1","wait_seconds":600}`); ended.Waiting {
		t.Fatalf("a finished task = %+v", ended)
	}
}

func TestTaskKillPreviewsThenStops(t *testing.T) {
	tasks := &fakeShellTasks{}
	registry := NewRegistry(NewShellExecutors(tasks)...)
	if spec, _ := registry.Spec("task_kill"); !spec.Asks {
		t.Fatal("task_kill does not ask")
	}
	preview, refused := registry.Preview(context.Background(), "task_kill", Call{Args: json.RawMessage(`{"id":"task_1"}`)})
	if refused != nil || preview.Description != "Stop background task task_1" || len(tasks.stopped) != 0 {
		t.Fatalf("preview = %+v, %+v", preview, refused)
	}
	stopped := runShellTool(t, tasks, "task_kill", `{"id":"task_1"}`)
	if stopped.Content != "Task task_1 is canceled." || len(tasks.stopped) != 1 {
		t.Fatalf("stopped = %+v", stopped)
	}
}

func TestReadOnlyBashRunsOnlyReadsInsideTheWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(work, "keys")); err != nil {
		t.Fatal(err)
	}
	spec, ok := ReadOnlySpec(coreDefinitionSpec("bash"))
	if !ok || spec.Mutates() || spec.Asks {
		t.Fatalf("read-only bash spec = %+v, %v", spec, ok)
	}
	if _, ok := ReadOnlySpec(coreDefinitionSpec("write")); ok {
		t.Fatal("write has a read-only variant")
	}
	for command, allowed := range map[string]bool{
		"git log --oneline -5 && git diff":   true,
		"rg -n TODO ./internal | head":       true,
		"cat " + filepath.Join(work, "a.go"): true,
		"go test ./...":                      false,
		"cat /etc/passwd":                    false,
		"cat ../other/a.go":                  false,
		"cat ~/.ssh/id_rsa":                  false,
		"cat keys/id_rsa":                    false,
		"git --git-dir=/srv/repo.git log":    false,
	} {
		args, _ := json.Marshal(BashParams{Command: command})
		if err := CheckReadOnlyCall(Call{WorkingDir: work, Args: args}); (err == nil) != allowed {
			t.Errorf("%q: %v, want allowed %v", command, err, allowed)
		}
	}
	args, _ := json.Marshal(BashParams{Command: "ls"})
	if err := CheckReadOnlyCall(Call{WorkingDir: home, Args: args}); err == nil {
		t.Error("read-only bash ran in the home directory")
	}
}
