package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
)

type recordingStorageStore struct {
	readMaxBytes int64
	saveCalls    int
}

func (s *recordingStorageStore) Root() string {
	return "/tmp/storage"
}

func (s *recordingStorageStore) List(string, string, int) ([]localstorage.Entry, error) {
	return nil, nil
}

func (s *recordingStorageStore) SaveBytes(string, []byte, string, []string, string) (localstorage.Entry, error) {
	s.saveCalls++
	return localstorage.Entry{}, nil
}

func (s *recordingStorageStore) Read(rawPath string, maxBytes int64) (localstorage.Entry, string, error) {
	s.readMaxBytes = maxBytes
	return localstorage.Entry{
		Path:      rawPath,
		Size:      4,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, "data", nil
}

func (s *recordingStorageStore) Delete(string) (localstorage.Entry, error) {
	return localstorage.Entry{}, nil
}

func (s *recordingStorageStore) ListTemporary(int) (localstorage.TempListResult, error) {
	return localstorage.TempListResult{}, nil
}

func (s *recordingStorageStore) SaveTemporary(string, []byte, string, []string, string) (localstorage.TempEntry, error) {
	return localstorage.TempEntry{}, nil
}

func (s *recordingStorageStore) CleanupTemporary() (localstorage.CleanupResult, error) {
	return localstorage.CleanupResult{}, nil
}

func (s *recordingStorageStore) UpdateTemporarySettings(*bool, int64, float64) (localstorage.TempSettings, error) {
	return localstorage.TempSettings{}, nil
}

func (s *recordingStorageStore) PromoteTemporary(string, string) (localstorage.Entry, error) {
	return localstorage.Entry{}, nil
}

func (s *recordingStorageStore) DeleteTemporary(string) (localstorage.TempEntry, error) {
	return localstorage.TempEntry{}, nil
}

func TestStorageReadsUseExplicitMaxBytes(t *testing.T) {
	store := &recordingStorageStore{}
	server := New(core.New(nil))
	server.SetStorageStore(store)

	req := httptest.NewRequest(http.MethodGet, "/v1/modules/storage/files/docs/a.txt", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.readMaxBytes != storageReadLimitBytes {
		t.Fatalf("read maxBytes = %d, want %d", store.readMaxBytes, storageReadLimitBytes)
	}
}

func TestStorageSaveRejectsDeclaredOversizeBody(t *testing.T) {
	store := &recordingStorageStore{}
	server := New(core.New(nil))
	server.SetStorageStore(store)

	req := httptest.NewRequest(http.MethodPost, "/v1/modules/storage/files", strings.NewReader(`{}`))
	req.ContentLength = storageSaveJSONBodyLimitBytes + 1
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("body = %q, want request body too large", rec.Body.String())
	}
}

func TestStorageSaveRejectsInvalidBase64(t *testing.T) {
	store := &recordingStorageStore{}
	server := New(core.New(nil))
	server.SetStorageStore(store)

	req := httptest.NewRequest(http.MethodPost, "/v1/modules/storage/files", strings.NewReader(`{"path":"docs/a.txt","content_base64":"%"}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "content_base64 is invalid") {
		t.Fatalf("body = %q, want content_base64 is invalid", rec.Body.String())
	}
}

func TestStorageSaveRejectsPlaintextContent(t *testing.T) {
	store := &recordingStorageStore{}
	server := New(core.New(nil))
	server.SetStorageStore(store)

	req := httptest.NewRequest(http.MethodPost, "/v1/modules/storage/files", strings.NewReader(`{"path":"docs/a.txt","content":"plain"}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if store.saveCalls != 0 {
		t.Fatalf("SaveBytes called %d times, want 0", store.saveCalls)
	}
}
