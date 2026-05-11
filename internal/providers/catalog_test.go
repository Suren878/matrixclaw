package providers

import (
	"reflect"
	"testing"
)

func TestReasoningEffortDefaults(t *testing.T) {
	t.Parallel()

	wantEfforts := []string{"low", "medium", "high"}
	if got := ReasoningEfforts(); !reflect.DeepEqual(got, wantEfforts) {
		t.Fatalf("ReasoningEfforts() = %#v, want %#v", got, wantEfforts)
	}
	if got := NormalizeReasoningEffort(" Medium "); got != DefaultReasoningEffort {
		t.Fatalf("NormalizeReasoningEffort() = %q, want %q", got, DefaultReasoningEffort)
	}

	tests := []struct {
		name        string
		providerID  string
		providerTyp string
		input       string
		wantDefault string
		wantInput   string
	}{
		{
			name:        "openai supports reasoning effort",
			providerID:  "openai",
			providerTyp: TypeOpenAICompat,
			input:       "high",
			wantDefault: DefaultReasoningEffort,
			wantInput:   "high",
		},
		{
			name:        "openai falls back to default for invalid effort",
			providerID:  "openai",
			providerTyp: TypeOpenAICompat,
			input:       "unknown",
			wantDefault: DefaultReasoningEffort,
			wantInput:   DefaultReasoningEffort,
		},
		{
			name:        "deepseek does not expose reasoning effort",
			providerID:  "deepseek",
			providerTyp: TypeOpenAICompat,
			input:       "high",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DefaultReasoningEffortForProvider(tt.providerID, tt.providerTyp); got != tt.wantDefault {
				t.Fatalf("DefaultReasoningEffortForProvider() = %q, want %q", got, tt.wantDefault)
			}
			if got := NormalizeReasoningEffortForProvider(tt.providerID, tt.providerTyp, tt.input); got != tt.wantInput {
				t.Fatalf("NormalizeReasoningEffortForProvider() = %q, want %q", got, tt.wantInput)
			}
		})
	}
}

func TestNormalizeModelIDAppliesProviderCatalogRules(t *testing.T) {
	t.Parallel()

	if got := NormalizeModelID("gemini", TypeGemini, "models/gemini-2.5-flash"); got != "gemini-2.5-flash" {
		t.Fatalf("NormalizeModelID(gemini) = %q, want gemini-2.5-flash", got)
	}
	if got := NormalizeModelID("openai", TypeOpenAICompat, "models/gpt-test"); got != "models/gpt-test" {
		t.Fatalf("NormalizeModelID(openai) = %q, want original model id", got)
	}
}

func TestCatalogEntryByID(t *testing.T) {
	t.Parallel()

	entry, ok := CatalogEntryByID(" GEMINI ")
	if !ok {
		t.Fatal("CatalogEntryByID(gemini) not found")
	}
	if entry.DefaultModel != DefaultGeminiModel {
		t.Fatalf("gemini default model = %q, want %q", entry.DefaultModel, DefaultGeminiModel)
	}
}

func TestOpenAICompatibleCatalogEntriesStayGeneric(t *testing.T) {
	t.Parallel()

	for _, providerID := range []string{"openai", "deepseek", "xai", "zai", "kimi", "aihubmix"} {
		providerID := providerID
		t.Run(providerID, func(t *testing.T) {
			t.Parallel()

			entry, ok := CatalogEntryByID(providerID)
			if !ok {
				t.Fatalf("CatalogEntryByID(%q) not found", providerID)
			}
			if entry.Type != TypeOpenAICompat {
				t.Fatalf("%s type = %q, want %q", providerID, entry.Type, TypeOpenAICompat)
			}
			if entry.Capabilities.NormalizeModel {
				t.Fatalf("%s should not use Gemini model normalization", providerID)
			}

			profile := ProfileForProvider(entry.Type)
			if profile.ProviderType != TypeOpenAICompat {
				t.Fatalf("%s ProviderType = %q, want %q", providerID, profile.ProviderType, TypeOpenAICompat)
			}
			if profile.RuntimeProfile.ToolSchemaDialect != ToolSchemaJSONSchema {
				t.Fatalf("%s ToolSchemaDialect = %q, want %q", providerID, profile.RuntimeProfile.ToolSchemaDialect, ToolSchemaJSONSchema)
			}
		})
	}
}
