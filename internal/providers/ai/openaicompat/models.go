package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func ListModels(ctx context.Context, cfg Config) ([]string, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	baseURL := strings.TrimSpace(cfg.BaseURL)
	modelsURL := strings.TrimSpace(cfg.ModelsURL)
	if modelsURL == "" {
		if baseURL == "" {
			return nil, errors.New("openaicompat: base url is required")
		}
		modelsURL = strings.TrimRight(baseURL, "/") + "/models"
	}
	if apiKey == "" && !cfg.PublicModels {
		return nil, errors.New("openaicompat: api key is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: build models request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")
	chatOptions := providers.ResolveOpenAIChatOptions(cfg.Profile, baseURL, cfg.Model)
	applyDefaultHeaders(req, chatOptions.DefaultHeaders)

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: models request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: read models response: %w", err)
	}

	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("openaicompat: model listing unavailable: %s", decodeOpenAIError(res.StatusCode, body))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("openaicompat: %s", decodeOpenAIError(res.StatusCode, body))
	}

	var payload struct {
		Data []struct {
			ID                        string   `json:"id"`
			ContextLength             int      `json:"context_length"`
			ContextWindow             int      `json:"context_window"`
			ContextSize               int      `json:"context_size"`
			MaxContextLength          int      `json:"max_context_length"`
			MaxPositionEmbeddings     int      `json:"max_position_embeddings"`
			MaxModelLen               int      `json:"max_model_len"`
			MaxInputTokens            int      `json:"max_input_tokens"`
			MaxSequenceLength         int      `json:"max_sequence_length"`
			MaxSeqLen                 int      `json:"max_seq_len"`
			NCtxTrain                 int      `json:"n_ctx_train"`
			NCtx                      int      `json:"n_ctx"`
			CtxSize                   int      `json:"ctx_size"`
			MaxTokens                 int      `json:"max_tokens"`
			SupportedParameters       []string `json:"supported_parameters"`
			SupportedReasoningEfforts []string `json:"supported_reasoning_efforts"`
			ReasoningEfforts          []string `json:"reasoning_efforts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("openaicompat: decode models response: %w", err)
	}

	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			contextWindow := modelContextWindowTokens(
				item.ContextLength,
				item.ContextWindow,
				item.ContextSize,
				item.MaxContextLength,
				item.MaxPositionEmbeddings,
				item.MaxModelLen,
				item.MaxInputTokens,
				item.MaxSequenceLength,
				item.MaxSeqLen,
				item.NCtxTrain,
				item.NCtx,
				item.CtxSize,
				item.MaxTokens,
			)
			metadata := providers.ModelMetadataRegistration{
				ContextWindow:       contextWindow,
				SupportedParameters: item.SupportedParameters,
				ReasoningEfforts:    firstStringSlice(item.SupportedReasoningEfforts, item.ReasoningEfforts),
			}
			if len(item.SupportedParameters) > 0 {
				toolCalling := modelParametersSupportTools(item.SupportedParameters)
				reasoningEffort := modelParametersSupportReasoning(item.SupportedParameters)
				metadata.ToolCalling = &toolCalling
				metadata.ReasoningEffort = &reasoningEffort
			}
			providers.RegisterModelMetadata(cfg.ProviderID, providers.TypeOpenAICompat, id, metadata)
			providers.RegisterModelMetadata(cfg.CatalogID, providers.TypeOpenAICompat, id, metadata)
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("openaicompat: no models available")
	}
	if baseURL != "" && apiKey != "" {
		registerLocalContextWindows(ctx, cfg, client, baseURL, apiKey, models)
	}
	return models, nil
}

func modelParametersSupportTools(values []string) bool {
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "tools", "tool_choice", "function_call", "functions", "parallel_tool_calls":
			return true
		}
	}
	return false
}

func modelParametersSupportReasoning(values []string) bool {
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "reasoning_effort", "reasoning", "include_reasoning", "thinking", "reasoning_content":
			return true
		}
	}
	return false
}

func firstStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func modelContextWindowTokens(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
