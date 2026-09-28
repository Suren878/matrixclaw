package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
)

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
	registry := NewCoreCodingRegistry()
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
		{"job_output", `{"shell_id":"job-1"}`, permission.Subject{}},
	} {
		got := registry.Subject(tc.tool, Call{WorkingDir: root, Args: json.RawMessage(tc.args)})
		if got != tc.want {
			t.Errorf("%s %s: subject = %+v, want %+v", tc.tool, tc.args, got, tc.want)
		}
	}
}
