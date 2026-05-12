package factory

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/providers"
	anthropic "github.com/Suren878/matrixclaw/internal/providers/ai/anthropiccompat"
	"github.com/Suren878/matrixclaw/internal/providers/ai/gemini"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicompat"
)

func TestNewRuntimeRoutesToProviderBackends(t *testing.T) {
	prevOpenAI := newOpenAICompatRuntime
	prevAnthropic := newAnthropicRuntime
	prevGemini := newGeminiRuntime
	defer func() {
		newOpenAICompatRuntime = prevOpenAI
		newAnthropicRuntime = prevAnthropic
		newGeminiRuntime = prevGemini
	}()

	var openAIConfig openaicompat.Config
	var anthropicConfig anthropic.Config
	var geminiConfig gemini.Config
	openAICalls := 0
	anthropicCalls := 0
	geminiCalls := 0
	newOpenAICompatRuntime = func(_ context.Context, cfg openaicompat.Config) (providers.Runtime, error) {
		openAICalls++
		openAIConfig = cfg
		return newProviderStub(), nil
	}
	newAnthropicRuntime = func(_ context.Context, cfg anthropic.Config) (providers.Runtime, error) {
		anthropicCalls++
		anthropicConfig = cfg
		return newProviderStub(), nil
	}
	newGeminiRuntime = func(_ context.Context, cfg gemini.Config) (providers.Runtime, error) {
		geminiCalls++
		geminiConfig = cfg
		return newProviderStub(), nil
	}

	runtime, err := NewRuntime(context.Background(), Config{
		ProviderID:      " configured-openai ",
		CatalogID:       " openai ",
		Type:            providers.TypeOpenAICompat,
		APIKey:          " key ",
		BaseURL:         " https://example.com/v1 ",
		Model:           " model-id ",
		ReasoningEffort: "high",
		ToolUseMode:     providers.ToolUseDisabled,
	})
	if err != nil {
		t.Fatalf("NewRuntime(openai) error = %v", err)
	}
	if runtime == nil {
		t.Fatal("NewRuntime(openai) returned nil runtime")
	}
	if openAICalls != 1 || anthropicCalls != 0 || geminiCalls != 0 {
		t.Fatalf("backend calls after openai route: openai=%d anthropic=%d gemini=%d", openAICalls, anthropicCalls, geminiCalls)
	}
	if openAIConfig.ProviderID != "configured-openai" || openAIConfig.CatalogID != "openai" {
		t.Fatalf("openai provider identity = %#v, want trimmed ids", openAIConfig)
	}
	if openAIConfig.APIKey != "key" || openAIConfig.BaseURL != "https://example.com/v1" || openAIConfig.Model != "model-id" {
		t.Fatalf("openai config = %#v, want trimmed openai-compatible config", openAIConfig)
	}
	if openAIConfig.Profile.ProviderType != providers.TypeOpenAICompat {
		t.Fatalf("openai profile = %#v, want OpenAI-compatible provider profile", openAIConfig.Profile)
	}
	if openAIConfig.ReasoningEffort != "high" || openAIConfig.ToolUseMode != providers.ToolUseDisabled {
		t.Fatalf("openai runtime profile = %#v, want explicit overrides", openAIConfig)
	}

	runtime, err = NewRuntime(context.Background(), Config{
		ProviderID: "anthropic-local",
		CatalogID:  "anthropic",
		Type:       providers.TypeAnthropic,
		APIKey:     " anthropic-key ",
		BaseURL:    " https://api.anthropic.com/v1 ",
		Model:      " claude-sonnet ",
	})
	if err != nil {
		t.Fatalf("NewRuntime(anthropic) error = %v", err)
	}
	if runtime == nil {
		t.Fatal("NewRuntime(anthropic) returned nil runtime")
	}
	if openAICalls != 1 || anthropicCalls != 1 || geminiCalls != 0 {
		t.Fatalf("backend calls after anthropic route: openai=%d anthropic=%d gemini=%d", openAICalls, anthropicCalls, geminiCalls)
	}
	if anthropicConfig.APIKey != "anthropic-key" || anthropicConfig.BaseURL != "https://api.anthropic.com/v1" || anthropicConfig.Model != "claude-sonnet" {
		t.Fatalf("anthropic config = %#v, want trimmed anthropic config", anthropicConfig)
	}
	if anthropicConfig.Profile.RuntimeProfile.ToolUseMode != providers.ToolUseDisabled {
		t.Fatalf("anthropic profile = %#v, want disabled tool profile", anthropicConfig.Profile)
	}

	runtime, err = NewRuntime(context.Background(), Config{
		ProviderID:      "gemini-local",
		CatalogID:       "gemini",
		Type:            providers.TypeGemini,
		APIKey:          " gemini-key ",
		BaseURL:         " https://generativelanguage.googleapis.com/v1beta ",
		Model:           " gemini-2.5-flash ",
		ReasoningEffort: "high",
		ToolUseMode:     providers.ToolUseDisabled,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if runtime == nil {
		t.Fatalf("NewRuntime() returned nil runtime")
	}
	if openAICalls != 1 || anthropicCalls != 1 || geminiCalls != 1 {
		t.Fatalf("backend calls after gemini route: openai=%d anthropic=%d gemini=%d", openAICalls, anthropicCalls, geminiCalls)
	}
	if geminiConfig.APIKey != "gemini-key" || geminiConfig.BaseURL != "https://generativelanguage.googleapis.com/v1beta" || geminiConfig.Model != "gemini-2.5-flash" {
		t.Fatalf("gemini config = %#v, want trimmed Gemini config", geminiConfig)
	}
	if geminiConfig.Profile.ProviderType != providers.TypeGemini || geminiConfig.Profile.RuntimeProviderType != providers.TypeGemini {
		t.Fatalf("gemini profile = %#v, want native Gemini profile", geminiConfig.Profile)
	}
}

func TestNewRuntimeOpenAICompatibleCustomBaseURLStaysGeneric(t *testing.T) {
	prev := newOpenAICompatRuntime
	defer func() {
		newOpenAICompatRuntime = prev
	}()

	var got openaicompat.Config
	newOpenAICompatRuntime = func(_ context.Context, cfg openaicompat.Config) (providers.Runtime, error) {
		got = cfg
		return newProviderStub(), nil
	}

	runtime, err := NewRuntime(context.Background(), Config{
		Type:    providers.TypeOpenAICompat,
		APIKey:  " custom-key ",
		BaseURL: " https://api.example.com/v1 ",
		Model:   " custom-model ",
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if runtime == nil {
		t.Fatalf("NewRuntime() returned nil runtime")
	}
	if got.Profile.ProviderType != providers.TypeOpenAICompat {
		t.Fatalf("Profile.ProviderType = %q, want %q", got.Profile.ProviderType, providers.TypeOpenAICompat)
	}
	if got.Profile.RuntimeProfile.ToolSchemaDialect != providers.ToolSchemaJSONSchema {
		t.Fatalf("ToolSchemaDialect = %q, want %q", got.Profile.RuntimeProfile.ToolSchemaDialect, providers.ToolSchemaJSONSchema)
	}
}

func TestNewRuntimeUsesCatalogCapabilities(t *testing.T) {
	prev := newOpenAICompatRuntime
	defer func() {
		newOpenAICompatRuntime = prev
	}()

	var got openaicompat.Config
	newOpenAICompatRuntime = func(_ context.Context, cfg openaicompat.Config) (providers.Runtime, error) {
		got = cfg
		return newProviderStub(), nil
	}

	_, err := NewRuntime(context.Background(), Config{
		ProviderID:      "provider-1",
		CatalogID:       "openrouter",
		Type:            providers.TypeOpenAICompat,
		APIKey:          "key",
		BaseURL:         "https://openrouter.ai/api/v1",
		Model:           "qwen/qwen3-coder-next",
		ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	if got.Profile.ProviderID != "openrouter" {
		t.Fatalf("Profile.ProviderID = %q, want openrouter", got.Profile.ProviderID)
	}
	if got.Profile.SupportsReasoningEffort {
		t.Fatalf("openrouter SupportsReasoningEffort = true, want false")
	}
	if !got.Profile.Capabilities.ToolCalling {
		t.Fatalf("openrouter ToolCalling = false, want true")
	}
}

func TestNewRuntimeRejectsUnknownType(t *testing.T) {
	runtime, err := NewRuntime(context.Background(), Config{Type: "custom"})
	if err == nil {
		t.Fatalf("NewRuntime() error = nil, want non-nil")
	}
	if runtime != nil {
		t.Fatalf("NewRuntime() runtime = %#v, want nil", runtime)
	}
}
