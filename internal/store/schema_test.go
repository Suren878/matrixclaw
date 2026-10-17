package store_test

import (
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/store"
)

func TestNewerDatabaseIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	if err := openTestStore(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB(t, path).Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	if st, err := store.NewSQLite(path); err == nil {
		_ = st.Close()
		t.Fatal("a database from a newer matrixclaw opened")
	}
}
