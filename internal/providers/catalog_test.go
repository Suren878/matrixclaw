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
	if got := ReasoningEffortsForProvider("openai", TypeOpenAICompat); !reflect.DeepEqual(got, []string{"none", "minimal", "low", "medium", "high", "xhigh"}) {
		t.Fatalf("ReasoningEffortsForProvider(openai) = %#v, want OpenAI reasoning efforts", got)
	}
	if got := NormalizeReasoningEffortForProvider("openai", TypeOpenAICompat, "xHigh"); got != "xhigh" {
		t.Fatalf("NormalizeReasoningEffortForProvider(openai, xHigh) = %q, want xhigh", got)
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

	for _, providerID := range []string{"openai", "deepseek", "openrouter", "xai", "zai", "minimax", "qwen", "kimi", "kimi-subscription", "aihubmix"} {
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
			if !entry.Capabilities.ToolCalling {
				t.Fatalf("%s should expose tool calling capability", providerID)
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

func TestOpenCrabsComparableCatalogEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id              string
		name            string
		baseURL         string
		model           string
		apiKeyEnv       string
		modelDiscovery  bool
		reasoningEffort bool
		toolCalling     bool
		normalizeModel  bool
	}{
		{
			id:             "openrouter",
			name:           "OpenRouter",
			baseURL:        "https://openrouter.ai/api/v1",
			model:          "qwen/qwen3-coder-next",
			apiKeyEnv:      "OPENROUTER_API_KEY",
			modelDiscovery: true,
			toolCalling:    true,
		},
		{
			id:             "minimax",
			name:           "MiniMax",
			baseURL:        "https://api.minimax.io/v1",
			model:          "MiniMax-M2.7",
			apiKeyEnv:      "MINIMAX_API_KEY",
			modelDiscovery: true,
			toolCalling:    true,
		},
		{
			id:             "qwen",
			name:           "Qwen / DashScope",
			baseURL:        "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
			model:          "qwen-plus",
			apiKeyEnv:      "DASHSCOPE_API_KEY",
			modelDiscovery: true,
			toolCalling:    true,
		},
		{
			id:          "kimi-subscription",
			name:        "Kimi (Subscription)",
			baseURL:     "https://api.kimi.com/coding/v1",
			model:       "kimi-for-coding",
			apiKeyEnv:   "KIMI_CODE_API_KEY",
			toolCalling: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()

			entry, ok := CatalogEntryByID(tt.id)
			if !ok {
				t.Fatalf("CatalogEntryByID(%q) not found", tt.id)
			}
			if entry.Name != tt.name || entry.Type != TypeOpenAICompat || !entry.Implemented || !entry.RequiresBaseURL {
				t.Fatalf("entry = %#v, want implemented OpenAI-compatible %s", entry, tt.name)
			}
			if entry.DefaultBaseURL != tt.baseURL || entry.DefaultModel != tt.model || entry.APIKeyEnv != tt.apiKeyEnv {
				t.Fatalf("entry defaults = base:%q model:%q env:%q", entry.DefaultBaseURL, entry.DefaultModel, entry.APIKeyEnv)
			}
			if tt.id == "qwen" && len(entry.BaseURLOptions) != 4 {
				t.Fatalf("qwen base URL options = %#v, want 4 Alibaba regions", entry.BaseURLOptions)
			}
			wantCapabilities := Capabilities{
				ModelDiscovery:  tt.modelDiscovery,
				ReasoningEffort: tt.reasoningEffort,
				ToolCalling:     tt.toolCalling,
				NormalizeModel:  tt.normalizeModel,
			}
			if entry.Capabilities != wantCapabilities {
				t.Fatalf("capabilities = %#v, want %#v", entry.Capabilities, wantCapabilities)
			}
		})
	}
}
