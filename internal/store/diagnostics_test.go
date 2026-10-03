package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/store"
)

func TestCheckSQLiteAcceptsADatabaseTheStoreOpened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	_ = openTestStore(t, path).Close()

	diag, err := store.CheckSQLite(path)
	if err != nil || !diag.SchemaReady {
		t.Fatalf("diagnostics = %+v, %v", diag, err)
	}
}
