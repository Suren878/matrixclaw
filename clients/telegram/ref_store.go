package telegram

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
)

// refStore keeps the newest values behind the short references sent to
// Telegram (long button data, inline queries), in memory and in a file, so
// references sent before a restart still resolve. Values are command lines
// and query text, never secrets: forms keep those in the dispatcher.
type refStore struct {
	mu     sync.Mutex
	path   string
	values *recentMap[string]
}

type savedRef struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// newRefStore loads the references saved at path, oldest first.
func newRefStore(path string, limit int) *refStore {
	store := &refStore{path: strings.TrimSpace(path), values: newRecentMap[string](limit)}
	if store.path == "" {
		return store
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		return store
	}
	var saved []savedRef
	if err := json.Unmarshal(data, &saved); err != nil {
		// References an older version saved cannot resolve; the next put
		// would replace the file anyway.
		log.Printf("telegram: reference file %s removed, it cannot be read: %v", store.path, err)
		_ = os.Remove(store.path)
		return store
	}
	for _, ref := range saved {
		store.values.put(ref.Key, ref.Value)
	}
	return store
}

func (s *refStore) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values.get(key)
}

// put keeps value under key and saves the file when key is new.
func (s *refStore) put(key string, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.values.get(key); ok && existing == value {
		return
	}
	s.values.put(key, value)
	if s.path == "" {
		return
	}
	saved := make([]savedRef, 0, len(s.values.order))
	for _, key := range s.values.order {
		saved = append(saved, savedRef{Key: key, Value: s.values.values[key]})
	}
	data, err := json.Marshal(saved)
	if err == nil {
		err = writeFileAtomic(s.path, data)
	}
	if err != nil {
		log.Printf("telegram: save references failed: %v", err)
	}
}
