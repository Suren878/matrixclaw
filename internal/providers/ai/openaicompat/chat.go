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

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (r *Runtime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	request = providers.NormalizeRequest(request, r.profile)
	payload := r.chatPayload(ctx, request)
	if len(payload.Messages) == 0 {
		return providers.Response{}, errors.New("openaicompat: no messages")
	}

	var applied [len(requestFixes)]bool
	for {
		body, err := json.Marshal(payload)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openaicompat: marshal request: %w", err)
		}
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
			if r.fixRequest(&payload, &applied, httpRes.StatusCode, resBody) {
				continue
			}
			return providers.Response{}, providers.NewAPIError("openaicompat", httpRes.StatusCode, openAIErrorMessage(resBody), httpRes.Header)
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

// requestFix adapts a payload a gateway rejected with a 4xx and reports
// whether it changed anything; each fix runs at most once per generation.
type requestFix func(r *Runtime, payload *chatCompletionRequest, rejection rejection) bool

// rejection is a gateway's 4xx: its error message, and the lower-cased
// message and body to search for markers.
type rejection struct {
	message string
	text    string
}

var requestFixes = [...]requestFix{
	dropUnsupportedReasoningEffort,
	switchToMaxCompletionTokens,
	dropStreamOptions,
	dropRejectedMaxTokens,
	addMissingReasoningContent,
}

func (r *Runtime) fixRequest(payload *chatCompletionRequest, applied *[len(requestFixes)]bool, statusCode int, body []byte) bool {
	if statusCode < 400 || statusCode >= 500 {
		return false
	}
	message := openAIErrorMessage(body)
	rejected := rejection{message: message, text: strings.ToLower(message + "\n" + string(body))}
	for i, fix := range requestFixes {
		if !applied[i] && fix(r, payload, rejected) {
			applied[i] = true
			return true
		}
	}
	return false
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

func addMissingReasoningContent(_ *Runtime, payload *chatCompletionRequest, rejected rejection) bool {
	text := rejected.text
	if !strings.Contains(text, "reasoning_content") || !strings.Contains(text, "must be passed back") && !strings.Contains(text, "thinking mode") {
		return false
	}
	fixed, changed := withMissingAssistantReasoningContent(*payload)
	*payload = fixed
	return changed
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

func dropUnsupportedReasoningEffort(_ *Runtime, payload *chatCompletionRequest, rejected rejection) bool {
	text := rejected.text
	if strings.TrimSpace(payload.ReasoningEffort) == "" || !strings.Contains(text, "reasoning") ||
		!containsAny(text, "unsupported", "not supported", "unrecognized", "unknown parameter", "invalid parameter", "does not support") {
		return false
	}
	payload.ReasoningEffort = ""
	return true
}

func switchToMaxCompletionTokens(_ *Runtime, payload *chatCompletionRequest, rejected rejection) bool {
	text := rejected.text
	if payload.MaxTokens == nil || payload.MaxCompletionTokens != nil || !strings.Contains(text, "max_tokens") ||
		!containsAny(text, "unsupported", "not supported", "not compatible", "incompatible", "invalid parameter") {
		return false
	}
	payload.MaxCompletionTokens, payload.MaxTokens = payload.MaxTokens, nil
	return true
}

func dropStreamOptions(_ *Runtime, payload *chatCompletionRequest, rejected rejection) bool {
	text := rejected.text
	if payload.StreamOptions == nil || !strings.Contains(text, "stream_options") && !strings.Contains(text, "include_usage") {
		return false
	}
	payload.StreamOptions = nil
	return true
}

func dropRejectedMaxTokens(r *Runtime, payload *chatCompletionRequest, rejected rejection) bool {
	retry, capTokens, omit := maxTokensRejection(*payload, rejected)
	if !retry {
		return false
	}
	r.rememberMaxTokensRejection(capTokens, omit)
	payload.MaxTokens, payload.MaxCompletionTokens = nil, nil
	return true
}

func containsAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// maxTokensRejection classifies a max_tokens/max_completion_tokens rejection:
// a named upper bound to remember and retry under, or the field itself being
// unsupported, which is remembered and omitted from now on.
func maxTokensRejection(payload chatCompletionRequest, rejected rejection) (retry bool, capTokens int64, omit bool) {
	sent, text := sentMaxTokens(payload), rejected.text
	if sent == 0 || !strings.Contains(text, "max_tokens") && !strings.Contains(text, "max_completion_tokens") {
		return false, 0, false
	}
	if containsAny(text, "unsupported", "not supported", "unrecognized", "unknown parameter", "does not support") {
		return true, 0, true
	}
	if containsAny(text, "range", "exceed", "too large", "less than or equal", "at most", "maximum", "<=") {
		return true, largestIntBelow(rejected.message, sent), false
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

func (r *Runtime) chatPayload(ctx context.Context, request providers.Request) chatCompletionRequest {
	payload := chatCompletionRequest{
		Model:    r.model,
		Messages: make([]chatCompletionMessage, 0, len(request.Messages)+2),
	}
	if systemPrompt := strings.TrimSpace(request.SystemPrompt); systemPrompt != "" {
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
	if r.contentCacheControl && strings.TrimSpace(request.CacheKey) != "" {
		markContentCacheBreakpoints(payload.Messages)
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
	stop = providers.ResolveStopReason(stop, len(calls))
	if strings.TrimSpace(text) == "" && len(calls) == 0 && !stop.AllowsEmptyReply() {
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
