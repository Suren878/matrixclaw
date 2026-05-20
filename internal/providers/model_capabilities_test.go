package providers

import "testing"

func TestProviderRuntimeCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		providerID        string
		providerType      string
		wantToolCalling   bool
		wantSchema        ToolSchemaDialect
		wantReasoning     bool
		wantReasoningMode ReasoningMode
		wantWithTools     bool
		wantThoughts      bool
	}{
		{
			name:              "openai catalog supports tools and reasoning effort together",
			providerID:        "openai",
			providerType:      TypeOpenAICompat,
			wantToolCalling:   true,
			wantSchema:        ToolSchemaJSONSchema,
			wantReasoning:     true,
			wantReasoningMode: ReasoningModeOpenAIEffort,
			wantWithTools:     true,
		},
		{
			name:            "openrouter keeps tools but avoids generic reasoning effort",
			providerID:      "openrouter",
			providerType:    TypeOpenAICompat,
			wantToolCalling: true,
			wantSchema:      ToolSchemaJSONSchema,
		},
		{
			name:              "gemini uses native schema and decodes thought parts",
			providerID:        "gemini",
			providerType:      TypeGemini,
			wantToolCalling:   true,
			wantSchema:        ToolSchemaGemini,
			wantReasoningMode: ReasoningModeGeminiThinking,
		},
		{
			name:         "anthropic adapter currently runs without tools or thinking",
			providerID:   "anthropic",
			providerType: TypeAnthropic,
			wantSchema:   ToolSchemaJSONSchema,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ProviderRuntimeCapabilities(tt.providerID, tt.providerType)
			if got.ToolCalling != tt.wantToolCalling {
				t.Fatalf("ToolCalling = %v, want %v", got.ToolCalling, tt.wantToolCalling)
			}
			if got.ToolSchemaDialect != tt.wantSchema {
				t.Fatalf("ToolSchemaDialect = %q, want %q", got.ToolSchemaDialect, tt.wantSchema)
			}
			if got.ReasoningEffort != tt.wantReasoning {
				t.Fatalf("ReasoningEffort = %v, want %v", got.ReasoningEffort, tt.wantReasoning)
			}
			if got.ReasoningMode != tt.wantReasoningMode {
				t.Fatalf("ReasoningMode = %q, want %q", got.ReasoningMode, tt.wantReasoningMode)
			}
			if got.ReasoningWithTools != tt.wantWithTools {
				t.Fatalf("ReasoningWithTools = %v, want %v", got.ReasoningWithTools, tt.wantWithTools)
			}
			if got.ThoughtSignatures != tt.wantThoughts {
				t.Fatalf("ThoughtSignatures = %v, want %v", got.ThoughtSignatures, tt.wantThoughts)
			}
		})
	}
}

func TestResolveModelCapabilitiesUsesSelectedModel(t *testing.T) {
	t.Parallel()

	reasoningModel := ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   "openai",
		ProviderType: TypeOpenAICompat,
		ModelID:      "gpt-5.4-mini",
	})
	if !reasoningModel.ProviderCapabilities.ReasoningEffort || len(reasoningModel.ReasoningEfforts) == 0 {
		t.Fatalf("gpt-5.4-mini capabilities = %#v, want OpenAI reasoning effort options", reasoningModel)
	}
	if reasoningModel.DefaultReasoningEffort != DefaultReasoningEffort {
		t.Fatalf("DefaultReasoningEffort = %q, want %q", reasoningModel.DefaultReasoningEffort, DefaultReasoningEffort)
	}

	chatModel := ResolveModelCapabilities(ModelCapabilityInput{
		ProviderID:   "openai",
		ProviderType: TypeOpenAICompat,
		ModelID:      "gpt-4.1",
	})
	if chatModel.ProviderCapabilities.ReasoningEffort || chatModel.RuntimeCapabilities.ReasoningEffort || len(chatModel.ReasoningEfforts) != 0 {
		t.Fatalf("gpt-4.1 capabilities = %#v, want no reasoning effort field", chatModel)
	}
	if !chatModel.ProviderCapabilities.ToolCalling {
		t.Fatalf("gpt-4.1 capabilities = %#v, want tool calling preserved", chatModel)
	}
}
