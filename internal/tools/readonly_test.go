package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyRegistryTools(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	docsDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	filePath := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "readme.md"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	registry := newReadOnlyRegistry()

	tests := []struct {
		name   string
		toolID string
		args   any
		want   string
	}{
		{
			name:   "read",
			toolID: "read",
			args:   ReadParams{FilePath: "src/main.go"},
			want:   "println(\"hello\")",
		},
		{
			name:   "glob",
			toolID: "glob",
			args:   GlobParams{Pattern: "**/*.go"},
			want:   "main.go",
		},
		{
			name:   "grep",
			toolID: "grep",
			args:   GrepParams{Pattern: "println", Path: "src"},
			want:   "Found 1 matches",
		},
		{
			name:   "ls",
			toolID: "ls",
			args:   LSParams{},
			want:   "docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			result, err := registry.Execute(context.Background(), tt.toolID, Call{
				WorkingDir: root,
				Args:       args,
			})
			if err != nil {
				t.Fatalf("%s Execute() error = %v", tt.toolID, err)
			}
			if !strings.Contains(result.Content, tt.want) {
				t.Fatalf("%s content missing %q: %q", tt.toolID, tt.want, result.Content)
			}
		})
	}
}

func TestRegistryReturnsToolErrorForInvalidArguments(t *testing.T) {
	t.Parallel()

	registry := newCoreCodingRegistry()
	for _, toolID := range []string{
		"read",
		"glob",
		"grep",
		"ls",
		"write",
		"edit",
		"multiedit",
		"bash",
		"job_output",
		"job_kill",
	} {
		t.Run(toolID, func(t *testing.T) {
			t.Parallel()

			result, err := registry.Execute(context.Background(), toolID, Call{
				WorkingDir: t.TempDir(),
				Args:       []byte(`{"file_path":"a"}{"file_path":"b"}`),
			})
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !result.IsError {
				t.Fatal("Execute().IsError = false, want true")
			}
			if !strings.Contains(result.Content, "Invalid "+toolID+" arguments") {
				t.Fatalf("content = %q, want invalid arguments message", result.Content)
			}
		})
	}
}

func TestBashExecutorCancelsRunningCommand(t *testing.T) {
	t.Parallel()

	args, _ := json.Marshal(BashParams{Command: "sleep 10"})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	result, err := NewBashExecutor().Execute(ctx, Call{
		WorkingDir: t.TempDir(),
		Approved:   true,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.IsError {
		t.Fatal("Execute().IsError = false, want true after cancellation")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled command took %s, want under 1s", elapsed)
	}
}

func TestBashProcessProbeNoMatchIsNotError(t *testing.T) {
	t.Parallel()

	args, _ := json.Marshal(BashParams{Command: "ps aux | grep -E 'matrixclaw-test-process-that-does-not-exist' | grep -v grep"})
	result, err := NewBashExecutor().Execute(context.Background(), Call{
		WorkingDir: t.TempDir(),
		Approved:   true,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute().IsError = true, want false for empty process probe")
	}
	meta, ok := result.Metadata.(BashResponseMetadata)
	if !ok {
		t.Fatalf("metadata type = %T, want BashResponseMetadata", result.Metadata)
	}
	if meta.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", meta.ExitCode)
	}
}

func TestSearchHelpersReturnContextCancellation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("alpha\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, _, err := globFiles(ctx, "*.txt", root, defaultSearchLimit); !errors.Is(err, context.Canceled) {
		t.Fatalf("globFiles() error = %v, want context.Canceled", err)
	}
	if _, _, err := grepFiles(ctx, "alpha", root, "", defaultSearchLimit); !errors.Is(err, context.Canceled) {
		t.Fatalf("grepFiles() error = %v, want context.Canceled", err)
	}
}

func TestJobOutputWaitReturnsContextCancellation(t *testing.T) {
	args, _ := json.Marshal(BashParams{
		Command:         "sleep 10",
		RunInBackground: true,
	})
	bgResult, err := NewBashExecutor().Execute(context.Background(), Call{
		WorkingDir: t.TempDir(),
		Approved:   true,
		Args:       args,
	})
	if err != nil {
		t.Fatalf("background bash Execute() error = %v", err)
	}
	if bgResult.Background == nil {
		t.Fatal("background bash did not return job metadata")
	}
	t.Cleanup(func() {
		_ = defaultJobManager.kill(bgResult.Background.ID)
	})

	waitArgs, _ := json.Marshal(JobOutputParams{ShellID: bgResult.Background.ID, Wait: true})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err = NewJobOutputExecutor().Execute(ctx, Call{Args: waitArgs})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("job_output Execute() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("job_output wait cancellation took %s, want under 1s", elapsed)
	}
}

func TestMutationAndShellTools(t *testing.T) {
	root := t.TempDir()
	registry := newCoreCodingRegistry()

	writeArgs, _ := json.Marshal(WriteParams{FilePath: "notes.txt", Content: "alpha\nbeta\n"})
	needsApproval, err := registry.Execute(context.Background(), "write", Call{
		WorkingDir: root,
		Args:       writeArgs,
	})
	if err != nil {
		t.Fatalf("write pre-approval Execute() error = %v", err)
	}
	if needsApproval.Approval == nil {
		t.Fatalf("write should require approval")
	}
	if _, ok := needsApproval.Approval.Params.(WritePermissionsParams); !ok {
		t.Fatalf("write approval params type = %T, want WritePermissionsParams", needsApproval.Approval.Params)
	}

	writeResult, err := registry.Execute(context.Background(), "write", Call{
		WorkingDir: root,
		Approved:   true,
		Args:       writeArgs,
	})
	if err != nil {
		t.Fatalf("write Execute() error = %v", err)
	}
	if writeResult.FileVersion == nil || !strings.Contains(writeResult.FileVersion.NewContent, "alpha") {
		t.Fatalf("write file version missing content: %#v", writeResult.FileVersion)
	}

	editArgs, _ := json.Marshal(EditParams{FilePath: "notes.txt", OldString: "beta", NewString: "gamma"})
	editResult, err := registry.Execute(context.Background(), "edit", Call{
		WorkingDir: root,
		Approved:   true,
		Args:       editArgs,
	})
	if err != nil {
		t.Fatalf("edit Execute() error = %v", err)
	}
	if editResult.FileVersion == nil || !strings.Contains(editResult.FileVersion.NewContent, "gamma") {
		t.Fatalf("edit file version missing replacement: %#v", editResult.FileVersion)
	}

	multiArgs, _ := json.Marshal(MultiEditParams{
		FilePath: "notes.txt",
		Edits: []EditOperation{
			{OldString: "alpha", NewString: "delta"},
			{OldString: "gamma", NewString: "epsilon"},
		},
	})
	multiResult, err := registry.Execute(context.Background(), "multiedit", Call{
		WorkingDir: root,
		Approved:   true,
		Args:       multiArgs,
	})
	if err != nil {
		t.Fatalf("multiedit Execute() error = %v", err)
	}
	if multiResult.FileVersion == nil || !strings.Contains(multiResult.FileVersion.NewContent, "epsilon") {
		t.Fatalf("multiedit file version missing final content: %#v", multiResult.FileVersion)
	}

	bgArgs, _ := json.Marshal(BashParams{
		Command:         "sleep 1; printf 'done'",
		RunInBackground: true,
	})
	bgResult, err := registry.Execute(context.Background(), "bash", Call{
		WorkingDir: root,
		Approved:   true,
		Args:       bgArgs,
	})
	if err != nil {
		t.Fatalf("background bash Execute() error = %v", err)
	}
	if bgResult.Background == nil {
		t.Fatalf("background bash should return background job")
	}
	bgMeta, ok := bgResult.Metadata.(BashResponseMetadata)
	if !ok {
		t.Fatalf("background bash metadata type = %T, want BashResponseMetadata", bgResult.Metadata)
	}
	if bgMeta.ShellID == "" || !bgMeta.Background {
		t.Fatalf("background bash metadata missing shell_id/background: %#v", bgMeta)
	}

	jobArgs, _ := json.Marshal(JobOutputParams{ShellID: bgResult.Background.ID})
	var outputResult Result
	for i := 0; i < 20; i++ {
		outputResult, err = registry.Execute(context.Background(), "job_output", Call{
			WorkingDir: root,
			Args:       jobArgs,
		})
		if err != nil {
			t.Fatalf("job_output Execute() error = %v", err)
		}
		if strings.Contains(outputResult.Content, "done") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(outputResult.Content, "done") {
		t.Fatalf("job_output content missing command output: %q", outputResult.Content)
	}
}

func TestBuildUnifiedDiffCounts(t *testing.T) {
	tests := []struct {
		name         string
		oldContent   string
		newContent   string
		wantDiff     string
		wantAdds     int
		wantRemovals int
	}{
		{
			name:       "inserted line",
			oldContent: "alpha\n",
			newContent: "alpha\nbeta\n",
			wantDiff:   "+beta",
			wantAdds:   1,
		},
		{
			name:       "noop",
			oldContent: "alpha\n",
			newContent: "alpha\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, additions, removals := buildUnifiedDiff("notes.txt", tt.oldContent, tt.newContent)
			if additions != tt.wantAdds || removals != tt.wantRemovals {
				t.Fatalf("diff counts = +%d -%d, want +%d -%d:\n%s", additions, removals, tt.wantAdds, tt.wantRemovals, diff)
			}
			if tt.wantDiff != "" && !strings.Contains(diff, tt.wantDiff) {
				t.Fatalf("diff missing %q:\n%s", tt.wantDiff, diff)
			}
			if tt.wantDiff == "" && diff != "" {
				t.Fatalf("diff = %q, want empty", diff)
			}
			if strings.Contains(diff, "-\n") {
				t.Fatalf("diff contains phantom empty deletion:\n%s", diff)
			}
		})
	}
}
