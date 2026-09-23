package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var transientRetryBackoffs = []time.Duration{
	200 * time.Millisecond,
	750 * time.Millisecond,
}

func (r *Runtime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	request = providers.NormalizeRequest(request, r.profile)
	payload := r.chatPayload(ctx, request)
	if len(payload.Messages) == 0 {
		return providers.Response{}, errors.New("openaicompat: no messages")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
	}

	retriedWithoutReasoning := false
	retriedWithMaxCompletionTokens := false
	retriedWithReasoningContent := false
	retriedWithoutMaxTokens := false
	retriedWithoutStreamOptions := false
	for attempt := 0; ; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
		if err != nil {
			return providers.Response{}, fmt.Errorf("openaicompat: build request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		applyDefaultHeaders(httpReq, r.headers)
		if payload.Stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		}

		httpRes, err := r.client.Do(httpReq)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openaicompat: request failed: %w", err)
		}

		if httpRes.StatusCode < 200 || httpRes.StatusCode >= 300 {
			resBody, err := io.ReadAll(httpRes.Body)
			_ = httpRes.Body.Close()
			if err != nil {
				return providers.Response{}, fmt.Errorf("openaicompat: read response: %w", err)
			}
			if shouldRetryStatus(httpRes.StatusCode) && attempt < len(transientRetryBackoffs) {
				delay := providers.RetryAfterDelay(httpRes.Header.Get("Retry-After"), time.Now(), transientRetryBackoffs[attempt])
				if delay > 30*time.Second {
					return providers.Response{}, fmt.Errorf("openaicompat: %s (retry after %s)", decodeOpenAIError(httpRes.StatusCode, resBody), delay)
				}
				if err := waitForRetry(ctx, delay); err != nil {
					return providers.Response{}, err
				}
				continue
			}
			if r.quirks.RetryUnsupportedReasoningEffort && !retriedWithoutReasoning && shouldRetryWithoutReasoningEffort(payload, httpRes.StatusCode, resBody) {
				payload.ReasoningEffort = ""
				body, err = json.Marshal(payload)
				if err != nil {
					return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
				}
				retriedWithoutReasoning = true
				attempt = -1
				continue
			}
			if r.quirks.RetryMaxTokensField && !retriedWithMaxCompletionTokens && shouldRetryWithMaxCompletionTokens(payload, httpRes.StatusCode, resBody) {
				payload.MaxCompletionTokens = payload.MaxTokens
				payload.MaxTokens = nil
				body, err = json.Marshal(payload)
				if err != nil {
					return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
				}
				retriedWithMaxCompletionTokens = true
				attempt = -1
				continue
			}
			if r.quirks.RetryWithoutStreamOptions && !retriedWithoutStreamOptions && shouldRetryWithoutStreamOptions(payload, httpRes.StatusCode, resBody) {
				payload.StreamOptions = nil
				body, err = json.Marshal(payload)
				if err != nil {
					return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
				}
				retriedWithoutStreamOptions = true
				attempt = -1
				continue
			}
			if r.quirks.RetryWithoutMaxTokens && !retriedWithoutMaxTokens {
				if retry, capTokens, omit := maxTokensRejection(payload, httpRes.StatusCode, resBody); retry {
					r.rememberMaxTokensRejection(capTokens, omit)
					payload.MaxTokens = nil
					payload.MaxCompletionTokens = nil
					body, err = json.Marshal(payload)
					if err != nil {
						return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
					}
					retriedWithoutMaxTokens = true
					attempt = -1
					continue
				}
			}
			if r.quirks.RetryAssistantReasoningContent && !retriedWithReasoningContent && shouldRetryWithReasoningContent(payload, httpRes.StatusCode, resBody) {
				var changed bool
				payload, changed = withMissingAssistantReasoningContent(payload)
				if changed {
					body, err = json.Marshal(payload)
					if err != nil {
						return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
					}
					retriedWithReasoningContent = true
					attempt = -1
					continue
				}
			}
			return providers.Response{}, fmt.Errorf("openaicompat: %s", decodeOpenAIError(httpRes.StatusCode, resBody))
		}
		defer func() { _ = httpRes.Body.Close() }()
		if payload.Stream && !strings.Contains(strings.ToLower(httpRes.Header.Get("Content-Type")), "application/json") {
			return r.decodeStream(ctx, httpRes.Body)
		}

		resBody, err := io.ReadAll(httpRes.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openaicompat: read response: %w", err)
		}
		return r.decodeChatResponse(resBody)
	}
}

func applyDefaultHeaders(req *http.Request, headers map[string]string) {
	for name, value := range headers {
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" || reservedHeader(name) {
			continue
		}
		req.Header.Set(name, value)
	}
}

func reservedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "content-type", "accept":
		return true
	default:
		return false
	}
}

func shouldRetryWithReasoningContent(payload chatCompletionRequest, statusCode int, body []byte) bool {
	if statusCode < 400 || statusCode >= 500 {
		return false
	}
	if _, changed := withMissingAssistantReasoningContent(payload); !changed {
		return false
	}
	text := strings.ToLower(decodeOpenAIError(statusCode, body) + "\n" + string(body))
	return strings.Contains(text, "reasoning_content") &&
		(strings.Contains(text, "must be passed back") || strings.Contains(text, "thinking mode"))
}

func withMissingAssistantReasoningContent(payload chatCompletionRequest) (chatCompletionRequest, bool) {
	var emptyReasoning string
	out := payload
	out.Messages = make([]chatCompletionMessage, len(payload.Messages))
	copy(out.Messages, payload.Messages)

	changed := false
	for i := range out.Messages {
		message := &out.Messages[i]
		if normalizeOpenAIRole(message.Role) != "assistant" || message.ReasoningContent != nil || len(message.ToolCalls) == 0 {
			continue
		}
		message.ReasoningContent = &emptyReasoning
		changed = true
	}
	return out, changed
}

func shouldRetryWithoutReasoningEffort(payload chatCompletionRequest, statusCode int, body []byte) bool {
	if strings.TrimSpace(payload.ReasoningEffort) == "" || statusCode < 400 || statusCode >= 500 {
		return false
	}
	text := strings.ToLower(decodeOpenAIError(statusCode, body) + "\n" + string(body))
	if !strings.Contains(text, "reasoning_effort") && !strings.Contains(text, "reasoning effort") && !strings.Contains(text, "reasoning") {
		return false
	}
	for _, marker := range []string{
		"unsupported",
		"not supported",
		"unrecognized",
		"unknown parameter",
		"invalid parameter",
		"does not support",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func shouldRetryWithMaxCompletionTokens(payload chatCompletionRequest, statusCode int, body []byte) bool {
	if payload.MaxTokens == nil || payload.MaxCompletionTokens != nil || statusCode < 400 || statusCode >= 500 {
		return false
	}
	text := strings.ToLower(decodeOpenAIError(statusCode, body) + "\n" + string(body))
	if !strings.Contains(text, "max_tokens") {
		return false
	}
	for _, marker := range []string{
		"unsupported",
		"not supported",
		"not compatible",
		"incompatible",
		"invalid parameter",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func shouldRetryWithoutStreamOptions(payload chatCompletionRequest, statusCode int, body []byte) bool {
	if payload.StreamOptions == nil || statusCode < 400 || statusCode >= 500 {
		return false
	}
	text := strings.ToLower(decodeOpenAIError(statusCode, body) + "\n" + string(body))
	return strings.Contains(text, "stream_options") || strings.Contains(text, "include_usage")
}

// maxTokensRejection classifies a max_tokens/max_completion_tokens rejection:
// a named upper bound to remember and retry under, or the field itself being
// unsupported, which is remembered and omitted from now on.
func maxTokensRejection(payload chatCompletionRequest, statusCode int, body []byte) (retry bool, capTokens int64, omit bool) {
	sent := sentMaxTokens(payload)
	if sent == 0 || statusCode < 400 || statusCode >= 500 {
		return false, 0, false
	}
	message := decodeOpenAIError(statusCode, body)
	text := strings.ToLower(message + "\n" + string(body))
	if !strings.Contains(text, "max_tokens") && !strings.Contains(text, "max_completion_tokens") {
		return false, 0, false
	}
	for _, marker := range []string{"unsupported", "not supported", "unrecognized", "unknown parameter", "does not support"} {
		if strings.Contains(text, marker) {
			return true, 0, true
		}
	}
	for _, marker := range []string{"range", "exceed", "too large", "less than or equal", "at most", "maximum", "<="} {
		if strings.Contains(text, marker) {
			return true, largestIntBelow(stripStatusPrefix(message), sent), false
		}
	}
	return false, 0, false
}

func sentMaxTokens(payload chatCompletionRequest) int64 {
	if payload.MaxTokens != nil {
		return *payload.MaxTokens
	}
	if payload.MaxCompletionTokens != nil {
		return *payload.MaxCompletionTokens
	}
	return 0
}

var integerPattern = regexp.MustCompile(`\d+`)

// largestIntBelow returns the largest integer literal in text that is
// strictly less than ceiling (the value that was sent). A message may also
// name unrelated larger numbers, such as a context-length limit, which are
// not an output cap and must not be learned as one.
func largestIntBelow(text string, ceiling int64) int64 {
	var best int64
	for _, match := range integerPattern.FindAllString(text, -1) {
		value, err := strconv.ParseInt(match, 10, 64)
		if err != nil || value <= 0 || value >= ceiling {
			continue
		}
		if value > best {
			best = value
		}
	}
	return best
}

// stripStatusPrefix removes decodeOpenAIError's leading "status <code>: ".
func stripStatusPrefix(message string) string {
	if idx := strings.Index(message, ": "); idx != -1 {
		return message[idx+2:]
	}
	return message
}

func shouldRetryStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusRequestTimeout || (statusCode >= 500 && statusCode <= 599)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("openaicompat: retry canceled: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (r *Runtime) chatPayload(ctx context.Context, request providers.Request) chatCompletionRequest {
	payload := chatCompletionRequest{
		Model:    r.model,
		Messages: make([]chatCompletionMessage, 0, len(request.Messages)+2),
	}
	if systemPrompt := combinedSystemPrompt(request.SystemPrompt, request.CustomInstructions); systemPrompt != "" {
		payload.Messages = append(payload.Messages, chatCompletionMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	for _, message := range request.Messages {
		chatMessage := r.chatMessage(message)
		if len(message.ToolCalls) > 0 {
			chatMessage.ToolCalls = encodeToolCalls(message.ToolCalls)
		}
		if chatMessageHasContent(chatMessage) || len(chatMessage.ToolCalls) > 0 || chatMessage.ToolCallID != "" {
			payload.Messages = append(payload.Messages, chatMessage)
		}
	}

	maxTokens := providers.ResolveMaxOutputTokens(request.MaxOutputTokens, r.maxOutputTokens, r.metadataID, providers.TypeOpenAICompat, r.model)
	if capTokens, omit := r.learnedMaxTokensLimit(); !omit {
		if capTokens > 0 && capTokens < maxTokens {
			maxTokens = capTokens
		}
		if r.useCompletionMax {
			payload.MaxCompletionTokens = &maxTokens
		} else {
			payload.MaxTokens = &maxTokens
		}
	}
	payload.Tools = encodeTools(request.Tools)
	if request.ToolChoice == providers.ToolChoiceNone && len(payload.Tools) > 0 {
		payload.ToolChoice = string(providers.ToolChoiceNone)
	}
	if r.promptCacheKey {
		payload.PromptCacheKey = strings.TrimSpace(request.CacheKey)
	}
	if r.reasoningEffort != "" && (len(payload.Tools) == 0 || r.capabilities.ReasoningWithTools) {
		payload.ReasoningEffort = r.reasoningEffort
	}
	if providers.TextStreamFromContext(ctx) != nil {
		payload.Stream = true
		// Streamed chat completions report usage only when asked to.
		payload.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}
	return payload
}

func chatMessageHasContent(message chatCompletionMessage) bool {
	switch content := message.Content.(type) {
	case string:
		return strings.TrimSpace(content) != ""
	case []chatCompletionContentPart:
		return len(content) > 0
	default:
		return content != nil
	}
}

func encodeToolCalls(toolCalls []providers.ToolCall) []chatCompletionToolCall {
	out := make([]chatCompletionToolCall, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		out = append(out, chatCompletionToolCall{
			ID:   strings.TrimSpace(toolCall.ID),
			Type: "function",
			Function: chatCompletionToolFunctionCall{
				Name:      strings.TrimSpace(toolCall.Name),
				Arguments: string(compactJSONRaw(string(toolCall.Arguments))),
			},
		})
	}
	return out
}

func encodeTools(tools []providers.ToolDefinition) []chatCompletionTool {
	out := make([]chatCompletionTool, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		out = append(out, chatCompletionTool{
			Type: "function",
			Function: chatCompletionToolDefinition{
				Name:        name,
				Description: strings.TrimSpace(tool.Description),
				Parameters:  tool.InputSchema,
			},
		})
	}
	return out
}

func (r *Runtime) decodeChatResponse(body []byte) (providers.Response, error) {
	var response chatCompletionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return providers.Response{}, fmt.Errorf("openaicompat: decode response: %w", err)
	}
	if len(response.Choices) == 0 {
		return providers.Response{}, fmt.Errorf("openaicompat: empty choices: %w", providers.ErrEmptyResponse)
	}

	choice := response.Choices[0]
	return r.finishResponse(choice.Message.Content, cloneStringPtr(choice.Message.ReasoningContent), decodeToolCalls(choice.Message.ToolCalls), choice.FinishReason, openAIUsage(response.Usage))
}

// finishResponse applies the finish reason: a reply cut by the output limit keeps
// its text and drops tool calls whose arguments were cut off.
func (r *Runtime) finishResponse(text string, reasoning *string, calls []providers.ToolCall, finishReason string, usage providers.Usage) (providers.Response, error) {
	if openAIIncompleteFinishReason(finishReason) {
		return providers.Response{}, fmt.Errorf("openaicompat: finish_reason %q: %w", finishReason, providers.ErrIncompleteResponse)
	}
	stop := openAIStopReason(finishReason)
	if stop == providers.StopMaxTokens {
		calls = completeToolCalls(calls)
	} else if err := validateToolCalls(calls); err != nil {
		return providers.Response{}, err
	}
	text = strings.TrimSpace(text)
	stop = providers.ResolveStopReason(stop, len(calls))
	if text == "" && len(calls) == 0 && !stop.AllowsEmptyReply() {
		return providers.Response{}, fmt.Errorf("openaicompat: %w", providers.ErrEmptyResponse)
	}
	return providers.Response{
		Text:             text,
		ReasoningContent: reasoning,
		Model:            r.model,
		Provider:         providers.TypeOpenAICompat,
		ToolCalls:        calls,
		StopReason:       stop,
		Usage:            usage,
	}, nil
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func openAIUsage(usage chatCompletionUsage) providers.Usage {
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 &&
		usage.PromptTokensDetails.CachedTokens == 0 && usage.CompletionTokensDetails.ReasoningTokens == 0 {
		return providers.Usage{}
	}
	raw, _ := json.Marshal(usage)
	cacheRead := usage.PromptTokensDetails.CachedTokens
	if cacheRead == 0 {
		cacheRead = usage.PromptCacheHitTokens
	}
	return providers.Usage{
		PromptTokens:     usage.PromptTokens,
		OutputTokens:     usage.CompletionTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: usage.PromptTokensDetails.CacheWriteTokens,
		ReasoningTokens:  usage.CompletionTokensDetails.ReasoningTokens,
		ProviderRaw:      raw,
	}
}
