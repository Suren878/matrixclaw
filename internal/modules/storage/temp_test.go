package storage

import (
	"errors"
	"testing"
	"time"
)

func TestExpiredTemporaryFileIsReportedAsNotFound(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	store.tempTTL = -time.Second
	if _, err := store.SaveTemporary("expired.txt", []byte("data"), "expired.txt", nil, "text/plain"); err != nil {
		t.Fatalf("SaveTemporary: %v", err)
	}
	if _, _, err := store.ReadTemporaryBytes("expired.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReadTemporaryBytes error = %v, want ErrNotFound", err)
	}
}
