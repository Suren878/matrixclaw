package gemini

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

const skipThoughtSignatureValidator = "skip_thought_signature_validator"

var transientRetryBackoffs = []time.Duration{
	200 * time.Millisecond,
	750 * time.Millisecond,
}

type Config struct {
	ProviderID      string
	CatalogID       string
	APIKey          string
	BaseURL         string
	Model           string
	MaxOutputTokens int64
	ToolUseMode     providers.ToolUseMode
	Profile         providers.ProviderProfile
	HTTPClient      *http.Client
}

type Runtime struct {
	client          *http.Client
	endpoint        string
	apiKey          string
	model           string
	providerID      string
	maxOutputTokens int64
	profile         providers.RuntimeProfile
	capabilities    providers.ModelCapabilities
}

func New(_ context.Context, cfg Config) (providers.Runtime, error) {
	client, apiKey, baseURL, model, maxOutputTokens, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	providerProfile := cfg.Profile
	if providerProfile.IsZero() {
		providerProfile = providers.ProfileForProvider(providers.TypeGemini)
	}

	return &Runtime{
		client:          client,
		endpoint:        strings.TrimRight(baseURL, "/") + "/" + modelResource(model) + ":streamGenerateContent?alt=sse",
		apiKey:          apiKey,
		model:           model,
		providerID:      metadataProviderID(cfg),
		maxOutputTokens: maxOutputTokens,
		profile: providerProfile.RuntimeProfileWithOverrides(providers.RuntimeProfile{
			ToolUseMode: cfg.ToolUseMode,
		}),
		capabilities: providerProfile.Capabilities,
	}, nil
}

func (r *Runtime) RuntimeProfile() providers.RuntimeProfile {
	return r.profile
}

func (r *Runtime) Identity() (string, string) {
	return providers.TypeGemini, r.model
}

func (r *Runtime) OutputLimits() (int64, int64) {
	current := providers.ResolveMaxOutputTokens(0, r.maxOutputTokens, r.providerID, providers.TypeGemini, r.model)
	return current, int64(providers.ResolveModelMetadata(r.providerID, providers.TypeGemini, r.model).MaxOutputTokens)
}

func (r *Runtime) ModelCapabilities() providers.ModelCapabilities {
	return r.capabilities
}

func ListModels(ctx context.Context, cfg Config) ([]string, error) {
	client, apiKey, baseURL, _, _, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	var models []string
	pageToken := ""
	for {
		endpoint := strings.TrimRight(baseURL, "/") + "/models"
		if pageToken != "" {
			endpoint += "?pageToken=" + pageToken
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("gemini: build models request: %w", err)
		}
		req.Header.Set("x-goog-api-key", apiKey)
		req.Header.Set("Accept", "application/json")

		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("gemini: models request failed: %w", err)
		}
		body, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("gemini: read models response: %w", readErr)
		}
		if res.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("gemini: model listing unavailable: %s", decodeGeminiError(res.StatusCode, body))
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, fmt.Errorf("gemini: %s", decodeGeminiError(res.StatusCode, body))
		}

		var payload geminiModelsResponse
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("gemini: decode models response: %w", err)
		}
		for _, item := range payload.Models {
			if supportsGenerateContent(item.SupportedGenerationMethods) {
				if name := strings.TrimSpace(item.Name); name != "" {
					metadata := providers.ModelMetadataRegistration{ContextWindow: item.InputTokenLimit, MaxOutputTokens: item.OutputTokenLimit}
					providers.RegisterModelMetadata(cfg.ProviderID, providers.TypeGemini, name, metadata)
					providers.RegisterModelMetadata(cfg.CatalogID, providers.TypeGemini, name, metadata)
					models = append(models, name)
				}
			}
		}
		pageToken = strings.TrimSpace(payload.NextPageToken)
		if pageToken == "" {
			break
		}
	}
	if len(models) == 0 {
		return nil, errors.New("gemini: no models available")
	}
	return models, nil
}

func (r *Runtime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	request = providers.NormalizeRequest(request, r.profile)
	payload := r.generatePayload(request)
	if len(payload.Contents) == 0 {
		return providers.Response{}, errors.New("gemini: no messages")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return providers.Response{}, fmt.Errorf("gemini: marshal request: %w", err)
	}
	for attempt := 0; ; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
		if err != nil {
			return providers.Response{}, fmt.Errorf("gemini: build request: %w", err)
		}
		httpReq.Header.Set("x-goog-api-key", r.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		httpRes, err := r.client.Do(httpReq)
		if err != nil {
			return providers.Response{}, fmt.Errorf("gemini: request failed: %w", err)
		}
		if httpRes.StatusCode < 200 || httpRes.StatusCode >= 300 {
			resBody, err := io.ReadAll(httpRes.Body)
			_ = httpRes.Body.Close()
			if err != nil {
				return providers.Response{}, fmt.Errorf("gemini: read response: %w", err)
			}
			if shouldRetryStatus(httpRes.StatusCode) && attempt < len(transientRetryBackoffs) {
				if err := waitForRetry(ctx, transientRetryBackoffs[attempt]); err != nil {
					return providers.Response{}, err
				}
				continue
			}
			return providers.Response{}, fmt.Errorf("gemini: %s", decodeGeminiError(httpRes.StatusCode, resBody))
		}
		response, err := r.decodeStream(ctx, httpRes.Body)
		_ = httpRes.Body.Close()
		return response, err
	}
}

func shouldRetryStatus(statusCode int) bool {
	return statusCode >= 500 && statusCode <= 599
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("gemini: retry canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (r *Runtime) generatePayload(request providers.Request) generateContentRequest {
	payload := generateContentRequest{
		Contents: make([]geminiContent, 0, len(request.Messages)),
		GenerationConfig: &generationConfig{
			MaxOutputTokens: providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxOutputTokens, r.providerID, providers.TypeGemini, r.model),
		},
	}
	if systemPrompt := combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions); systemPrompt != "" {
		payload.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		}
	}

	toolNames := map[string]string{}
	for _, message := range request.Messages {
		content := encodeMessage(message, toolNames)
		if len(content.Parts) > 0 {
			payload.Contents = appendGeminiContent(payload.Contents, content)
		}
	}

	functions := encodeTools(request.Tools)
	if len(functions) > 0 {
		payload.Tools = []geminiTool{{FunctionDeclarations: functions}}
		if request.ToolChoice == providers.ToolChoiceNone {
			payload.ToolConfig = &geminiToolConfig{FunctionCallingConfig: geminiFunctionCallingConfig{Mode: "NONE"}}
		}
	}
	return payload
}

func encodeMessage(message providers.Message, toolNames map[string]string) geminiContent {
	if len(message.ToolCalls) > 0 {
		parts := make([]geminiPart, 0, len(message.ToolCalls)+1)
		if content := strings.TrimSpace(message.Content); content != "" {
			parts = append(parts, geminiPart{Text: content})
		}
		signature := thoughtSignature(message.Reasoning)
		if signature == "" {
			// Steps saved without a signature; Gemini documents this value for them.
			signature = skipThoughtSignatureValidator
		}
		for _, toolCall := range message.ToolCalls {
			name := strings.TrimSpace(toolCall.Name)
			if name == "" {
				continue
			}
			id := strings.TrimSpace(toolCall.ID)
			if id != "" {
				toolNames[id] = name
			}
			// Gemini signs the first function call of a step.
			parts = append(parts, geminiPart{
				FunctionCall: &geminiFunctionCall{
					Name: name,
					Args: rawObject(toolCall.Arguments),
				},
				ThoughtSignature: signature,
			})
			signature = ""
		}
		return geminiContent{Role: "model", Parts: parts}
	}

	if strings.EqualFold(strings.TrimSpace(message.Role), "tool") || strings.TrimSpace(message.ToolCallID) != "" {
		name := toolNames[strings.TrimSpace(message.ToolCallID)]
		if name == "" {
			name = strings.TrimSpace(message.ToolCallID)
		}
		if name == "" {
			name = "tool_result"
		}
		key := "content"
		if message.IsError {
			key = "error"
		}
		return geminiContent{
			Role: "user",
			Parts: []geminiPart{{
				FunctionResponse: &geminiFunctionResponse{
					Name:     name,
					Response: map[string]any{key: strings.TrimSpace(message.Content)},
				},
			}},
		}
	}

	content := strings.TrimSpace(message.Content)
	if content == "" && len(message.Images) == 0 {
		return geminiContent{}
	}
	parts := make([]geminiPart, 0, 1+len(message.Images))
	if content != "" {
		parts = append(parts, geminiPart{Text: content})
	}
	for _, image := range message.Images {
		data := strings.TrimSpace(image.DataBase64)
		if data == "" {
			continue
		}
		mimeType := strings.TrimSpace(image.MIMEType)
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		parts = append(parts, geminiPart{InlineData: &geminiInlineData{
			MIMEType: mimeType,
			Data:     data,
		}})
	}
	if len(parts) == 0 {
		return geminiContent{}
	}
	return geminiContent{
		Role:  normalizeGeminiRole(message.Role),
		Parts: parts,
	}
}

// appendGeminiContent sends all function responses of one step in one content.
func appendGeminiContent(contents []geminiContent, next geminiContent) []geminiContent {
	if n := len(contents); n > 0 && functionResponsesOnly(contents[n-1]) && functionResponsesOnly(next) {
		contents[n-1].Parts = append(contents[n-1].Parts, next.Parts...)
		return contents
	}
	return append(contents, next)
}

func functionResponsesOnly(content geminiContent) bool {
	for _, part := range content.Parts {
		if part.FunctionResponse == nil {
			return false
		}
	}
	return len(content.Parts) > 0
}

func thoughtSignature(blocks []providers.ReasoningBlock) string {
	for _, block := range blocks {
		if block.Signature != "" {
			return block.Signature
		}
	}
	return ""
}

func encodeTools(tools []providers.ToolDefinition) []geminiFunctionDeclaration {
	out := make([]geminiFunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, geminiFunctionDeclaration{
			Name:        name,
			Description: strings.TrimSpace(tool.Description),
			Parameters:  geminiParameters(tool.InputSchema),
		})
	}
	return out
}

// decodeStream reads streamGenerateContent SSE. The stream has no terminal event,
// so a reply is complete only once a chunk carries finishReason or blocks the prompt.
func (r *Runtime) decodeStream(ctx context.Context, body io.Reader) (providers.Response, error) {
	var parts []geminiPart
	var usage geminiUsageMetadata
	finishReason, blockReason := "", ""
	err := providers.ScanSSE(ctx, body, func(event providers.SSEEvent) error {
		var chunk generateContentResponse
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return fmt.Errorf("gemini: decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return streamError(chunk.Error.Code, chunk.Error.Status, chunk.Error.Message)
		}
		if chunk.UsageMetadata != (geminiUsageMetadata{}) {
			usage = chunk.UsageMetadata
		}
		if chunk.PromptFeedback.BlockReason != "" {
			blockReason = chunk.PromptFeedback.BlockReason
		}
		if len(chunk.Candidates) == 0 {
			return nil
		}
		candidate := chunk.Candidates[0]
		for _, part := range candidate.Content.Parts {
			parts = append(parts, part)
			if !part.Thought {
				if err := providers.StreamText(ctx, part.Text); err != nil {
					return err
				}
			}
		}
		if candidate.FinishReason != "" {
			finishReason = candidate.FinishReason
		}
		return nil
	})
	if err != nil {
		return providers.Response{}, err
	}
	if blockReason != "" {
		return r.reply(nil, providers.StopContentFilter, usage)
	}
	if finishReason == "" {
		return providers.Response{}, fmt.Errorf("gemini: %w", providers.ErrIncompleteResponse)
	}
	stop, err := geminiStopReason(finishReason)
	if err != nil {
		return providers.Response{}, err
	}
	return r.reply(parts, stop, usage)
}

// streamError reports a mid-stream error chunk; like an HTTP 5xx, a server-side
// failure is retryable while nothing has been generated.
func streamError(code int, status string, message string) error {
	detail := strings.Join(strings.Fields(fmt.Sprintf("stream error %d %s %s", code, status, message)), " ")
	if shouldRetryStatus(code) || status == "UNAVAILABLE" || status == "INTERNAL" {
		return fmt.Errorf("gemini: %s: %w", detail, providers.ErrIncompleteResponse)
	}
	return fmt.Errorf("gemini: %s", detail)
}

func (r *Runtime) reply(parts []geminiPart, stop providers.StopReason, usage geminiUsageMetadata) (providers.Response, error) {
	var text strings.Builder
	var toolCalls []providers.ToolCall
	var reasoning []providers.ReasoningBlock
	// Gemini function calls have no protocol call ID. A run may invoke the
	// same function in the same output slot on several turns; give each
	// response its own namespace so later calls cannot overwrite old results.
	responseID := rand.Text()
	for i, part := range parts {
		if !part.Thought {
			text.WriteString(part.Text)
		}
		if part.FunctionCall != nil && strings.TrimSpace(part.FunctionCall.Name) != "" {
			if part.ThoughtSignature != "" && len(reasoning) == 0 {
				reasoning = []providers.ReasoningBlock{{Signature: part.ThoughtSignature}}
			}
			toolCalls = append(toolCalls, providers.ToolCall{
				ID:        fmt.Sprintf("gemini_%s_%d", responseID, i),
				Name:      strings.TrimSpace(part.FunctionCall.Name),
				Arguments: part.FunctionCall.Args,
			})
		}
	}

	reply := strings.TrimSpace(text.String())
	stop = providers.ResolveStopReason(stop, len(toolCalls))
	if reply == "" && len(toolCalls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("gemini: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{
		Text:       reply,
		Model:      r.model,
		Provider:   providers.TypeGemini,
		ToolCalls:  toolCalls,
		Reasoning:  reasoning,
		StopReason: stop,
		Usage:      geminiUsage(usage),
	}, nil
}

// geminiStopReason maps finishReason; a malformed or unexpected function call is
// a retryable generation failure, not a reply.
func geminiStopReason(reason string) (providers.StopReason, error) {
	switch reason {
	case "MAX_TOKENS":
		return providers.StopMaxTokens, nil
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		return providers.StopContentFilter, nil
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL":
		return "", fmt.Errorf("gemini: %s: %w", strings.ToLower(reason), providers.ErrMalformedToolCall)
	default:
		return providers.StopEndTurn, nil
	}
}

func normalizeConfig(cfg Config) (*http.Client, string, string, string, int64, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, "", "", "", 0, errors.New("gemini: api key is required")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, "", "", "", 0, errors.New("gemini: base url is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = providers.DefaultGeminiModel
	}
	maxOutputTokens := cfg.MaxOutputTokens
	client := cfg.HTTPClient
	if client == nil {
		client = providers.NewHTTPClient()
	}
	return client, apiKey, baseURL, model, maxOutputTokens, nil
}

func metadataProviderID(cfg Config) string {
	if id := strings.TrimSpace(cfg.ProviderID); id != "" {
		return id
	}
	return strings.TrimSpace(cfg.CatalogID)
}

func modelResource(model string) string {
	model = strings.Trim(strings.TrimSpace(model), "/")
	if strings.HasPrefix(model, "models/") {
		return model
	}
	return "models/" + model
}

func normalizeGeminiRole(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), "assistant") {
		return "model"
	}
	return "user"
}

func combinedSystemPrompt(systemPrompt string, customInstructions string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	customInstructions = strings.TrimSpace(customInstructions)
	if customInstructions == "" {
		return systemPrompt
	}
	block := "User custom instructions:\n" + customInstructions
	if systemPrompt == "" {
		return block
	}
	return systemPrompt + "\n\n" + block
}

func rawObject(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || strings.TrimSpace(string(value)) == "" {
		return json.RawMessage(`{}`)
	}
	var decoded map[string]any
	if err := json.Unmarshal(value, &decoded); err != nil || decoded == nil {
		return json.RawMessage(`{}`)
	}
	raw, err := json.Marshal(decoded)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func supportsGenerateContent(methods []string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, method := range methods {
		if strings.EqualFold(strings.TrimSpace(method), "generateContent") {
			return true
		}
	}
	return false
}

func geminiUsage(usage geminiUsageMetadata) providers.Usage {
	if usage.PromptTokenCount == 0 && usage.CandidatesTokenCount == 0 && usage.TotalTokenCount == 0 &&
		usage.CachedContentTokenCount == 0 && usage.ThoughtsTokenCount == 0 {
		return providers.Usage{}
	}
	raw, _ := json.Marshal(usage)
	return providers.Usage{
		PromptTokens:    usage.PromptTokenCount,
		OutputTokens:    usage.CandidatesTokenCount + usage.ThoughtsTokenCount,
		CacheReadTokens: usage.CachedContentTokenCount,
		ReasoningTokens: usage.ThoughtsTokenCount,
		ProviderRaw:     raw,
	}
}

func decodeGeminiError(statusCode int, body []byte) string {
	var envelope geminiErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		return fmt.Sprintf("status %d: %s", statusCode, strings.TrimSpace(envelope.Error.Message))
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Sprintf("status %d", statusCode)
	}
	return fmt.Sprintf("status %d: %s", statusCode, text)
}
