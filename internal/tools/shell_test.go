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
