package providers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelMetadataCacheIsSavedOncePerListingAndReloaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "context-windows.json")
	UseModelMetadataCache(path)
	t.Cleanup(func() { UseModelMetadataCache("") })

	RegisterModelMetadata("cache-gw", TypeOpenAICompat, "cache-model-a", ModelMetadataRegistration{ContextWindow: 64_000})
	RegisterModelMetadata("cache-gw", TypeOpenAICompat, "cache-model-b", ModelMetadataRegistration{MaxOutputTokens: 4_000})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("registering a model wrote the cache (stat err=%v)", err)
	}
	SaveModelMetadata()

	modelMetadataOverrides.Lock()
	modelMetadataOverrides.values = map[string]ModelMetadataRegistration{}
	modelMetadataOverrides.Unlock()
	UseModelMetadataCache(path)
	if got := ResolveContextWindowTokens("cache-gw", TypeOpenAICompat, "cache-model-a"); got != 64_000 {
		t.Fatalf("reloaded window=%d, want 64000", got)
	}
	if got := ResolveModelMetadata("cache-gw", TypeOpenAICompat, "cache-model-b").MaxOutputTokens; got != 4_000 {
		t.Fatalf("reloaded max output=%d, want 4000", got)
	}
}
