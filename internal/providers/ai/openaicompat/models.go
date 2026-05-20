package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func ListModels(ctx context.Context, cfg Config) ([]string, error) {
	client, apiKey, baseURL, _, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: build models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openaicompat: models request failed: %w", err)
	}
	defer res.Body.Close()

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
			ID                    string `json:"id"`
			ContextLength         int    `json:"context_length"`
			ContextWindow         int    `json:"context_window"`
			ContextSize           int    `json:"context_size"`
			MaxContextLength      int    `json:"max_context_length"`
			MaxPositionEmbeddings int    `json:"max_position_embeddings"`
			MaxModelLen           int    `json:"max_model_len"`
			MaxInputTokens        int    `json:"max_input_tokens"`
			MaxSequenceLength     int    `json:"max_sequence_length"`
			MaxSeqLen             int    `json:"max_seq_len"`
			NCtxTrain             int    `json:"n_ctx_train"`
			NCtx                  int    `json:"n_ctx"`
			CtxSize               int    `json:"ctx_size"`
			MaxTokens             int    `json:"max_tokens"`
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
			providers.RegisterContextWindowTokens(cfg.ProviderID, providers.TypeOpenAICompat, id, contextWindow)
			providers.RegisterContextWindowTokens(cfg.CatalogID, providers.TypeOpenAICompat, id, contextWindow)
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("openaicompat: no models available")
	}
	registerLocalContextWindows(ctx, cfg, client, baseURL, apiKey, models)
	return models, nil
}

func modelContextWindowTokens(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
