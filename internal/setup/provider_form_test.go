package setup

import (
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func TestProviderFormSpecForBuiltInDraftUsesCapabilities(t *testing.T) {
	spec := ProviderFormSpecForDraft(ProviderDraft{
		ID:        "openai",
		CatalogID: "openai",
		Name:      "OpenAI",
		Type:      providers.TypeOpenAICompat,
		Model:     "gpt-5.4-mini",
	})

	if spec.Custom {
		t.Fatal("built-in provider spec marked custom")
	}
	assertProviderFormFieldIDs(t, spec, []ProviderFormFieldID{
		ProviderFormFieldAPIKey,
		ProviderFormFieldModel,
		ProviderFormFieldReasoningEffort,
		ProviderFormFieldToolUse,
	})
	model, ok := spec.Field(ProviderFormFieldModel)
	if !ok || !model.Required || !model.Editable || !model.Picker {
		t.Fatalf("model field = %#v, want required editable picker", model)
	}
	reasoning, ok := spec.Field(ProviderFormFieldReasoningEffort)
	if !ok || reasoning.Status != providers.DefaultReasoningEffort || !reasoning.Picker {
		t.Fatalf("reasoning field = %#v, want default picker", reasoning)
	}
	if !reflect.DeepEqual(reasoning.Options, []string{"none", "minimal", "low", "medium", "high", "xhigh"}) {
		t.Fatalf("reasoning options = %#v, want OpenAI reasoning options", reasoning.Options)
	}
	toolUse, ok := spec.Field(ProviderFormFieldToolUse)
	if !ok || toolUse.Status != "Enabled" {
		t.Fatalf("tool use field = %#v, want Enabled status", toolUse)
	}
}

func TestProviderFormSpecUsesSelectedModelCapabilities(t *testing.T) {
	spec := ProviderFormSpecForDraft(ProviderDraft{
		ID:        "openai",
		CatalogID: "openai",
		Name:      "OpenAI",
		Type:      providers.TypeOpenAICompat,
		Model:     "gpt-4.1",
	})

	assertProviderFormFieldIDs(t, spec, []ProviderFormFieldID{
		ProviderFormFieldAPIKey,
		ProviderFormFieldModel,
		ProviderFormFieldToolUse,
	})
	if _, ok := spec.Field(ProviderFormFieldReasoningEffort); ok {
		t.Fatal("non-reasoning OpenAI model should not show reasoning effort")
	}
}

func TestProviderFormSpecForQwenIncludesEndpointOptions(t *testing.T) {
	spec := ProviderFormSpecForDraft(ProviderDraft{
		ID:        "qwen",
		CatalogID: "qwen",
		Name:      "Qwen / DashScope",
		Type:      providers.TypeOpenAICompat,
		BaseURL:   "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		Model:     "qwen-plus",
	})

	assertProviderFormFieldIDs(t, spec, []ProviderFormFieldID{
		ProviderFormFieldBaseURL,
		ProviderFormFieldAPIKey,
		ProviderFormFieldModel,
		ProviderFormFieldToolUse,
	})
	baseURL, ok := spec.Field(ProviderFormFieldBaseURL)
	if !ok || baseURL.Label != "Endpoint" || baseURL.Status != "Singapore / International" || !baseURL.Picker {
		t.Fatalf("qwen base URL field = %#v, want endpoint picker", baseURL)
	}
	if len(baseURL.Options) < 3 {
		t.Fatalf("qwen endpoint options = %#v, want regional endpoints", baseURL.Options)
	}
}

func TestProviderFormSpecForCustomOpenAICompatKeepsManualFields(t *testing.T) {
	spec := ProviderFormSpecForDraft(ProviderDraft{
		ID:      "custom-openai-compatible",
		Name:    "Local AI",
		Type:    providers.TypeOpenAICompat,
		BaseURL: "http://127.0.0.1:11434/v1",
	})

	if !spec.Custom {
		t.Fatal("custom provider spec marked built-in")
	}
	assertProviderFormFieldIDs(t, spec, []ProviderFormFieldID{
		ProviderFormFieldName,
		ProviderFormFieldBaseURL,
		ProviderFormFieldAPIKey,
		ProviderFormFieldModel,
		ProviderFormFieldToolUse,
	})
	if _, ok := spec.Field(ProviderFormFieldReasoningEffort); ok {
		t.Fatal("custom OpenAI-compatible provider without catalog should not show reasoning by default")
	}
	model, ok := spec.Field(ProviderFormFieldModel)
	if !ok || !model.Required || !model.Editable || model.Picker {
		t.Fatalf("custom model field = %#v, want manual editable model", model)
	}
}

func TestProviderFormSpecInputCanUseExplicitCapabilities(t *testing.T) {
	spec := ProviderFormSpecFromInput(ProviderFormSpecInput{
		ID:                "runtime-custom",
		Name:              "Runtime Custom",
		Type:              providers.TypeOpenAICompat,
		Custom:            true,
		CustomKnown:       true,
		Capabilities:      providers.Capabilities{ReasoningEffort: true},
		CapabilitiesKnown: true,
	})

	assertProviderFormFieldIDs(t, spec, []ProviderFormFieldID{
		ProviderFormFieldName,
		ProviderFormFieldBaseURL,
		ProviderFormFieldAPIKey,
		ProviderFormFieldModel,
		ProviderFormFieldReasoningEffort,
	})
	if _, ok := spec.Field(ProviderFormFieldToolUse); ok {
		t.Fatal("tool use field shown without ToolCalling capability")
	}
}

func TestProviderFormSpecMasksAPIKeyStatus(t *testing.T) {
	spec := ProviderFormSpecForDraft(ProviderDraft{
		ID:     "custom-openai-compatible",
		Type:   providers.TypeOpenAICompat,
		APIKey: "test-secret",
	})

	apiKey, ok := spec.Field(ProviderFormFieldAPIKey)
	if !ok || apiKey.Status != "****cret" || !apiKey.Sensitive || !apiKey.Required {
		t.Fatalf("api key field = %#v, want masked required sensitive field", apiKey)
	}
}

func assertProviderFormFieldIDs(t *testing.T, spec ProviderFormSpec, want []ProviderFormFieldID) {
	t.Helper()
	got := make([]ProviderFormFieldID, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		got = append(got, field.ID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("field IDs = %#v, want %#v", got, want)
	}
}
