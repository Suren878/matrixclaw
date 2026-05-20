package openaicodex

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
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	baseURL := strings.TrimRight(firstNonEmpty(cfg.BaseURL, DefaultBaseURL), "/")
	creds, err := ResolveCredentials(ctx, client, baseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(creds.BaseURL, "/")+"/models?client_version="+codexClientVersion, nil)
	if err != nil {
		return nil, fmt.Errorf("openai-codex: build models request: %w", err)
	}
	setCodexHeaders(req, creds.AccessToken)
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai-codex: models request failed: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("openai-codex: read models response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-codex: %s", decodeError(raw))
	}
	models, err := decodeModels(raw)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("openai-codex: no models available")
	}
	return models, nil
}

func decodeModels(raw []byte) ([]string, error) {
	var payload struct {
		Data   []modelItem `json:"data"`
		Models []modelItem `json:"models"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		var values []string
		if listErr := json.Unmarshal(raw, &values); listErr == nil {
			return cleanModels(values), nil
		}
		return nil, fmt.Errorf("openai-codex: decode models response: %w", err)
	}
	values := make([]string, 0, len(payload.Data)+len(payload.Models))
	for _, item := range payload.Data {
		if id := item.modelID(); id != "" {
			providers.RegisterContextWindowTokens("openai-codex", providers.TypeOpenAICodex, id, item.contextWindowTokens())
			values = append(values, id)
		}
	}
	for _, item := range payload.Models {
		if id := item.modelID(); id != "" {
			providers.RegisterContextWindowTokens("openai-codex", providers.TypeOpenAICodex, id, item.contextWindowTokens())
			values = append(values, id)
		}
	}
	return cleanModels(values), nil
}

type modelItem struct {
	ID            string `json:"id"`
	Model         string `json:"model"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	ContextWindow int    `json:"context_window"`
	ContextLength int    `json:"context_length"`
	MaxTokens     int    `json:"max_tokens"`
}

func (m modelItem) modelID() string {
	return firstNonEmpty(m.ID, m.Model, m.Name, m.Slug)
}

func (m modelItem) contextWindowTokens() int {
	switch {
	case m.ContextWindow > 0:
		return m.ContextWindow
	case m.ContextLength > 0:
		return m.ContextLength
	case m.MaxTokens > 0:
		return m.MaxTokens
	default:
		return 0
	}
}

func cleanModels(values []string) []string {
	models := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		models = append(models, value)
	}
	return models
}
