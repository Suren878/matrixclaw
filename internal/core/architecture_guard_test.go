package core_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreDoesNotImportOrchestrationPackage(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.) error = %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Clean(entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("ParseFile(%s) error = %v", entry.Name(), err)
		}
		for _, imported := range file.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			if importPath == "github.com/Suren878/matrixclaw/internal/orchestration" ||
				strings.HasPrefix(importPath, "github.com/Suren878/matrixclaw/internal/orchestration/") {
				t.Fatalf("%s imports orchestration; core should depend on the RunStarter port only", entry.Name())
			}
		}
	}
}
