package openaicompat

import (
	"context"
	"testing"
	"time"
)

func TestNewUsesLongRunningDefaultTimeout(t *testing.T) {
	runtime, err := New(context.Background(), Config{
		APIKey:  "test-key",
		BaseURL: "https://example.com/v1",
		Model:   "test-model",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	compat, ok := runtime.(*Runtime)
	if !ok {
		t.Fatalf("runtime type = %T, want *Runtime", runtime)
	}
	if got := compat.client.Timeout; got != 5*time.Minute {
		t.Fatalf("default HTTP timeout = %s, want %s", got, 5*time.Minute)
	}
}
