package localruntime

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStartManagedProcessReturnsStartError(t *testing.T) {
	runtime := New(filepath.Join(t.TempDir(), "root"))
	cmd := exec.Command(filepath.Join(t.TempDir(), "missing-binary"))
	if _, err := runtime.startManagedProcess(managedProcessOptions{cmd: cmd}); err == nil {
		t.Fatal("expected start error")
	}
}
