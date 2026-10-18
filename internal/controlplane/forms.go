package controlplane

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const (
	formLifetime = 30 * time.Minute
	formLimit    = 64
)

// formStore keeps what a screen needs between commands (an open provider
// form with its API key, a skill draft, a sign-in in progress) under a
// random id, so command lines carry the id and never the values. Entries
// live in memory only and expire unused after formLifetime.
type formStore struct {
	mu      sync.Mutex
	entries map[string]formEntry
	now     func() time.Time
}

type formEntry struct {
	value   any
	expires time.Time
}

func newFormStore(now func() time.Time) *formStore {
	return &formStore{entries: map[string]formEntry{}, now: now}
}

// open keeps value under a new id, dropping the entry closest to expiry when
// the store is full.
func (s *formStore) open(value any) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	for len(s.entries) >= formLimit {
		oldest := ""
		for id, entry := range s.entries {
			if oldest == "" || entry.expires.Before(s.entries[oldest].expires) {
				oldest = id
			}
		}
		delete(s.entries, oldest)
	}
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	id := base64.RawURLEncoding.EncodeToString(raw[:])
	s.entries[id] = formEntry{value: value, expires: s.now().Add(formLifetime)}
	return id
}

// put replaces an open entry's value; false when it expired.
func (s *formStore) put(id string, value any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	if _, ok := s.entries[id]; !ok {
		return false
	}
	s.entries[id] = formEntry{value: value, expires: s.now().Add(formLifetime)}
	return true
}

func (s *formStore) close(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
}

func (s *formStore) pruneLocked() {
	now := s.now()
	for id, entry := range s.entries {
		if !now.Before(entry.expires) {
			delete(s.entries, id)
		}
	}
}

// formValue is the open entry id when it holds a T; using it keeps it open.
func formValue[T any](s *formStore, id string) (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	entry, ok := s.entries[id]
	value, isT := entry.value.(T)
	if !ok || !isT {
		var zero T
		return zero, false
	}
	entry.expires = s.now().Add(formLifetime)
	s.entries[id] = entry
	return value, true
}
