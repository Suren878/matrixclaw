package providers

import "testing"

func TestUnknownModelsGetA128kWindow(t *testing.T) {
	if got := ResolveContextWindowTokens("custom-gateway", "", "mystery-model-x"); got != 128_000 {
		t.Fatalf("window of an unknown model = %d, want 128000", got)
	}
}
