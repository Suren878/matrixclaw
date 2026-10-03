package daemoncmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveWebResearchFilesKeepsWhatLinksPointTo(t *testing.T) {
	data, outside := t.TempDir(), t.TempDir()
	kept := filepath.Join(outside, "keep.txt")
	research := filepath.Join(data, "web-research")
	for path, content := range map[string]string{
		kept: "mine",
		filepath.Join(research, "res_1", "page.txt"): "page",
		filepath.Join(data, "storage", "file.txt"):   "stored",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(research, "res_1", "link")); err != nil {
		t.Fatal(err)
	}

	removeWebResearchFiles(filepath.Join(data, "matrixclaw.db"))
	if _, err := os.Lstat(research); !os.IsNotExist(err) {
		t.Fatalf("web-research is still there: %v", err)
	}
	for _, path := range []string{kept, filepath.Join(data, "storage", "file.txt")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	if err := os.Symlink(outside, research); err != nil {
		t.Fatal(err)
	}
	removeWebResearchFiles(filepath.Join(data, "matrixclaw.db"))
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("a linked web-research lost its target: %v", err)
	}
	if _, err := os.Lstat(research); err != nil {
		t.Fatalf("the link itself was removed: %v", err)
	}
}
