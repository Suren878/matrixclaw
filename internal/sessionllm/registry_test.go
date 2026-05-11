package sessionllm

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestNewBuildsRegistryFromProviderSpecs(t *testing.T) {
	t.Parallel()

	reg := New("", []ProviderSpec{
		{ID: " ", Name: "empty"},
		{
			ID:        "gemini",
			CatalogID: "gemini",
			Name:      "Google Gemini",
			Type:      providers.TypeGemini,
			Model:     "models/gemini-2.5-flash",
		},
		{
			ID:    "local",
			Name:  "Local AI",
			Type:  providers.TypeOpenAICompat,
			Model: "llama3",
		},
	})

	activeProviderID, activeModel := reg.ActiveSelection()
	if activeProviderID != "gemini" || activeModel != "models/gemini-2.5-flash" {
		t.Fatalf("ActiveSelection() = %q/%q, want gemini/models/gemini-2.5-flash", activeProviderID, activeModel)
	}

	options := reg.Providers()
	if len(options) != 2 {
		t.Fatalf("len(Providers()) = %d, want 2", len(options))
	}
	if options[0].ID != "gemini" || options[0].Label != "Google Gemini" || options[1].ID != "local" {
		t.Fatalf("Providers() = %#v, want configured specs in order", options)
	}

	option, model, err := reg.Normalize("", "")
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if option.ID != "gemini" || model != "gemini-2.5-flash" {
		t.Fatalf("Normalize() = %#v/%q, want active Gemini with normalized model", option, model)
	}
}

func TestRuntimeConfigWithModelUsesProviderSpec(t *testing.T) {
	t.Parallel()

	got := runtimeConfigWithModel(ProviderSpec{
		Type:            providers.TypeOpenAICompat,
		APIKey:          "secret",
		BaseURL:         "https://api.example.com/v1",
		Model:           "default-model",
		MaxOutputTokens: 4096,
		ReasoningEffort: "high",
		ToolUseMode:     providers.ToolUseDisabled,
	}, "override-model")

	if got.Type != providers.TypeOpenAICompat || got.Model != "override-model" {
		t.Fatalf("runtime config identity = %#v, want OpenAI-compatible override model", got)
	}
	if got.APIKey != "secret" || got.BaseURL != "https://api.example.com/v1" || got.MaxOutputTokens != 4096 {
		t.Fatalf("runtime config transport = %#v", got)
	}
	if got.ReasoningEffort != "high" || got.ToolUseMode != providers.ToolUseDisabled {
		t.Fatalf("runtime config provider controls = %#v", got)
	}
}
