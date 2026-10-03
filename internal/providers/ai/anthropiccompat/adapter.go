package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const (
	defaultAnthropicVersion = "2023-06-01"
)

type Runtime struct {
	providers.RuntimeBase
	endpoint string
}

func New(_ context.Context, cfg providers.RuntimeConfig) (providers.Runtime, error) {
	base := providers.NewRuntimeBase(cfg, providers.TypeAnthropic, providers.DefaultAnthropicModel)
	if err := base.RequireKeyAndURL("anthropic"); err != nil {
		return nil, err
	}
	return &Runtime{RuntimeBase: base, endpoint: strings.TrimRight(base.BaseURL, "/") + "/messages"}, nil
}

func (r *Runtime) OutputLimits() (int64, int64) {
	return r.CatalogOutputLimits()
}

func ListModels(ctx context.Context, cfg providers.RuntimeConfig) ([]string, error) {
	base := providers.NewRuntimeBase(cfg, providers.TypeAnthropic, providers.DefaultAnthropicModel)
	if err := base.RequireKeyAndURL("anthropic"); err != nil {
		return nil, err
	}
	client, apiKey, baseURL := base.Client, base.APIKey, base.BaseURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic: build models request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", defaultAnthropicVersion)
	req.Header.Set("Accept", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: models request failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read models response: %w", err)
	}

	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("anthropic: model listing unavailable: %s", decodeAnthropicError(res.StatusCode, body))
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic: %s", decodeAnthropicError(res.StatusCode, body))
	}

	var payload struct {
		Data []struct {
			ID             string `json:"id"`
			MaxInputTokens int    `json:"max_input_tokens"`
			MaxTokens      int    `json:"max_tokens"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("anthropic: decode models response: %w", err)
	}

	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if id := strings.TrimSpace(item.ID); id != "" {
			metadata := providers.ModelMetadataRegistration{ContextWindow: item.MaxInputTokens, MaxOutputTokens: item.MaxTokens}
			providers.RegisterModelMetadata(cfg.ProviderID, providers.TypeAnthropic, id, metadata)
			providers.RegisterModelMetadata(cfg.CatalogID, providers.TypeAnthropic, id, metadata)
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("anthropic: no models available")
	}
	return models, nil
}

func (r *Runtime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	payload := anthropicRequest{
		Model:     r.Model,
		MaxTokens: providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.MaxOutputTokens, r.MetadataID, providers.TypeAnthropic, r.Model),
	}
	if err := encodeRequest(&payload, request); err != nil {
		return providers.Response{}, err
	}
	if providers.TextStreamFromContext(ctx) != nil {
		payload.Stream = true
	}
	response, err := r.send(ctx, payload)
	if rejectsThinking(err) && stripThinking(payload.Messages) {
		// Until the prompt prefix is stable, a replayed signature can fail
		// verification; the request is still valid without thinking.
		return r.send(ctx, payload)
	}
	return response, err
}

func (r *Runtime) send(ctx context.Context, payload anthropicRequest) (providers.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("x-api-key", r.APIKey)
	httpReq.Header.Set("anthropic-version", defaultAnthropicVersion)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if payload.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	httpRes, err := r.Client.Do(httpReq)
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer func() { _ = httpRes.Body.Close() }()

	if httpRes.StatusCode < 200 || httpRes.StatusCode >= 300 {
		resBody, err := io.ReadAll(httpRes.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("anthropic: read response: %w", err)
		}
		return providers.Response{}, anthropicResponseError(httpRes.StatusCode, resBody, httpRes.Header)
	}
	if payload.Stream {
		return r.decodeStream(ctx, httpRes.Body)
	}

	resBody, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic: read response: %w", err)
	}

	return r.decodeResponse(resBody)
}

func anthropicStopReason(reason string) providers.StopReason {
	switch reason {
	case "max_tokens", "model_context_window_exceeded":
		return providers.StopMaxTokens
	case "refusal":
		return providers.StopRefusal
	case "tool_use":
		return providers.StopToolUse
	default:
		return providers.StopEndTurn
	}
}

type anthropicUsagePayload struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

func anthropicUsage(usage anthropicUsagePayload) providers.Usage {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 &&
		usage.CacheCreationInputTokens == 0 && usage.CacheReadInputTokens == 0 {
		return providers.Usage{}
	}
	raw, _ := json.Marshal(usage)
	return providers.Usage{
		PromptTokens:     usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadInputTokens,
		CacheWriteTokens: usage.CacheCreationInputTokens,
		ProviderRaw:      raw,
	}
}

// mergeAnthropicUsage applies message_delta usage, whose counts are cumulative.
func mergeAnthropicUsage(current anthropicUsagePayload, delta anthropicUsagePayload) anthropicUsagePayload {
	if delta.InputTokens > 0 {
		current.InputTokens = delta.InputTokens
	}
	if delta.CacheCreationInputTokens > 0 {
		current.CacheCreationInputTokens = delta.CacheCreationInputTokens
	}
	if delta.CacheReadInputTokens > 0 {
		current.CacheReadInputTokens = delta.CacheReadInputTokens
	}
	if delta.OutputTokens > 0 {
		current.OutputTokens = delta.OutputTokens
	}
	return current
}

type anthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (d anthropicErrorDetail) String() string {
	kind, message := strings.TrimSpace(d.Type), strings.TrimSpace(d.Message)
	if kind == "" || message == "" {
		return kind + message
	}
	return kind + ": " + message
}

type anthropicErrorEnvelope struct {
	Error anthropicErrorDetail `json:"error"`
}

// rejectsThinking reports a bad request about replayed thinking or its signature.
func rejectsThinking(err error) bool {
	var apiErr *providers.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return false
	}
	message := strings.ToLower(apiErr.Message)
	return strings.HasPrefix(message, "invalid_request_error") && (strings.Contains(message, "thinking") || strings.Contains(message, "signature"))
}

func anthropicResponseError(statusCode int, body []byte, header http.Header) error {
	var envelope anthropicErrorEnvelope
	message := strings.TrimSpace(string(body))
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		message = envelope.Error.String()
	}
	return providers.NewAPIError("anthropic", statusCode, message, header)
}

// anthropicStreamError classifies an error event by the HTTP status its type stands for.
func anthropicStreamError(detail anthropicErrorDetail) error {
	code := map[string]int{
		"overloaded_error":     529,
		"api_error":            http.StatusInternalServerError,
		"rate_limit_error":     http.StatusTooManyRequests,
		"authentication_error": http.StatusUnauthorized,
		"permission_error":     http.StatusForbidden,
	}[detail.Type]
	return providers.NewStreamError("anthropic", code, "stream error: "+detail.String())
}

func decodeAnthropicError(statusCode int, body []byte) string {
	var envelope anthropicErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return fmt.Sprintf("status %d: %s", statusCode, envelope.Error)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Sprintf("status %d", statusCode)
	}
	return fmt.Sprintf("status %d: %s", statusCode, text)
}
