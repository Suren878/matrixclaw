package sessionllm

import (
	"context"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestManualContextWindowAppliesOnlyToTheConfiguredModel(t *testing.T) {
	reg := New("anthropic", []ProviderSpec{
		{ID: "anthropic", Type: providers.TypeAnthropic, APIKey: "k", BaseURL: "https://api.anthropic.com/v1", Model: "claude-sonnet-4-5", ContextWindow: 90_000},
		{ID: "home-box", Type: providers.TypeOpenAICompat, APIKey: "k", BaseURL: "http://127.0.0.1:1234/v1", Model: "box-model"},
	})
	for _, tc := range []struct {
		provider, model string
		want            int
	}{
		{"anthropic", "claude-sonnet-4-5", 90_000},
		{"anthropic", "", 90_000},
		{"", "", 90_000},
		{"anthropic", "claude-opus-4-7", 1_000_000},
		{"anthropic", "claude-3-haiku", 200_000},
		{"home-box", "box-model", providers.DefaultFallbackContextWindowTokens},
		{"unconfigured", "gpt-5", 400_000},
	} {
		if got := reg.ContextWindow(tc.provider, tc.model); got != tc.want {
			t.Errorf("%q %q = %d, want %d", tc.provider, tc.model, got, tc.want)
		}
	}
}

func TestContextWindowUsesTheProvidersOwnListing(t *testing.T) {
	providers.RegisterContextWindowTokens("registry-gw", "vendor/registry-model", 48_000)
	reg := New("registry-gw", []ProviderSpec{{ID: "registry-gw", Type: providers.TypeOpenAICompat, APIKey: "k", BaseURL: "https://gw.example/v1", Model: "vendor/registry-model"}})
	if got := reg.ContextWindow("registry-gw", "vendor/registry-model"); got != 48_000 {
		t.Fatalf("window = %d, want the listed 48000", got)
	}
}

func TestAliasesFindTheConfiguredProvider(t *testing.T) {
	reg := New("openrouter", []ProviderSpec{{ID: "openrouter", Type: providers.TypeOpenAICompat, APIKey: "k", BaseURL: "https://openrouter.ai/api/v1", Model: "qwen/qwen3-coder"}})
	option, model, err := reg.Normalize("or", "")
	if err != nil || option.ID != "openrouter" || model != "qwen/qwen3-coder" {
		t.Fatalf("option=%+v model=%q err=%v", option, model, err)
	}
}

func TestConcurrentResolveSharesOneRuntime(t *testing.T) {
	reg := New("gw", []ProviderSpec{{ID: "gw", Type: providers.TypeOpenAICompat, APIKey: "k", BaseURL: "https://gw.example/v1", Model: "m"}})
	runtimes := make([]providers.Runtime, 16)
	var wg sync.WaitGroup
	for i := range runtimes {
		wg.Go(func() {
			runtime, _, _, err := reg.Resolve(context.Background(), "gw", "m")
			if err != nil {
				t.Error(err)
			}
			runtimes[i] = runtime
		})
	}
	wg.Wait()
	for _, runtime := range runtimes[1:] {
		if runtime != runtimes[0] {
			t.Fatal("two runtimes were built for one provider and model")
		}
	}
}
