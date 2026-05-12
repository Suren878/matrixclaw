package sessionllm

import (
	"errors"
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

func TestRegistryAllowsEmptyConfigurationUntilResolve(t *testing.T) {
	t.Parallel()

	reg := New("", nil)
	if providerID, modelID := reg.ActiveSelection(); providerID != "" || modelID != "" {
		t.Fatalf("ActiveSelection() = %q/%q, want empty", providerID, modelID)
	}
	if providers := reg.Providers(); len(providers) != 0 {
		t.Fatalf("Providers() = %#v, want none", providers)
	}
	if option, model, err := reg.Normalize("", ""); err != nil || option.ID != "" || model != "" {
		t.Fatalf("Normalize(empty) = %#v/%q/%v, want empty nil result", option, model, err)
	}
	if _, _, _, err := reg.Resolve(nil, "", ""); !errors.Is(err, ErrNoActiveProvider) {
		t.Fatalf("Resolve() error = %v, want ErrNoActiveProvider", err)
	}
}

func TestRuntimeConfigWithModelUsesProviderSpec(t *testing.T) {
	t.Parallel()

	got := runtimeConfigWithModel(ProviderSpec{
		ID:              "provider-1",
		CatalogID:       "openai",
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
	if got.ProviderID != "provider-1" || got.CatalogID != "openai" {
		t.Fatalf("runtime config provider ids = %#v", got)
	}
	if got.APIKey != "secret" || got.BaseURL != "https://api.example.com/v1" || got.MaxOutputTokens != 4096 {
		t.Fatalf("runtime config transport = %#v", got)
	}
	if got.ReasoningEffort != "high" || got.ToolUseMode != providers.ToolUseDisabled {
		t.Fatalf("runtime config provider controls = %#v", got)
	}
}
