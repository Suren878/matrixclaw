package providers

import "testing"

func TestResolveModelCapabilities(t *testing.T) {
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
			name:              "openrouter keeps tools but avoids generic reasoning effort",
			providerID:        "openrouter",
			providerType:      TypeOpenAICompat,
			wantToolCalling:   true,
			wantSchema:        ToolSchemaJSONSchema,
			wantReasoningMode: ReasoningModeOpenRouter,
		},
		{
			name:            "kimi subscription keeps native tools without reasoning effort",
			providerID:      "kimi-subscription",
			providerType:    TypeOpenAICompat,
			wantToolCalling: true,
			wantSchema:      ToolSchemaJSONSchema,
		},
		{
			name:              "gemini uses native schema and thought signatures",
			providerID:        "gemini",
			providerType:      TypeGemini,
			wantToolCalling:   true,
			wantSchema:        ToolSchemaGemini,
			wantReasoningMode: ReasoningModeGeminiThinking,
			wantThoughts:      true,
		},
		{
			name:              "anthropic adapter currently runs without tools",
			providerID:        "anthropic",
			providerType:      TypeAnthropic,
			wantSchema:        ToolSchemaJSONSchema,
			wantReasoningMode: ReasoningModeAnthropicThinking,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveModelCapabilities(tt.providerID, tt.providerType, "model")
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
