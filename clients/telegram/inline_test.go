package telegram

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestInlineRequestCacheKeepsOnlyRecentRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inline.json")
	worker := newWorker(Config{InlineCachePath: path}, nil)
	var first, last string
	for i := range recentInlineLimit + 10 {
		last = strings.TrimPrefix(worker.rememberInlineRequest(fmt.Sprintf("query-%d", i), fmt.Sprintf("text %d", i)), inlineCallbackPrefix)
		if i == 0 {
			first = last
		}
	}
	cached := readInlineRequestCache(path)
	if len(cached) != recentInlineLimit {
		t.Fatalf("cache holds %d requests, want %d", len(cached), recentInlineLimit)
	}
	if worker.inlineRequestText(first) != "" {
		t.Fatal("oldest request still remembered")
	}
	if got := worker.inlineRequestText(last); got != fmt.Sprintf("text %d", recentInlineLimit+9) {
		t.Fatalf("newest request = %q", got)
	}
}
