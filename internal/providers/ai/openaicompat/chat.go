package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

	for attempt := 0; ; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
		if err != nil {
			return providers.Response{}, fmt.Errorf("openaicompat: build request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
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
				if err := waitForRetry(ctx, transientRetryBackoffs[attempt]); err != nil {
					return providers.Response{}, err
				}
				continue
			}
			return providers.Response{}, fmt.Errorf("openaicompat: %s", decodeOpenAIError(httpRes.StatusCode, resBody))
		}
		defer httpRes.Body.Close()
		if payload.Stream {
			return r.decodeStream(ctx, httpRes.Body)
		}

		resBody, err := io.ReadAll(httpRes.Body)
		if err != nil {
			return providers.Response{}, fmt.Errorf("openaicompat: read response: %w", err)
		}
		return r.decodeChatResponse(resBody)
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

	if r.maxOutputTokens > 0 {
		if r.useCompletionMax {
			payload.MaxCompletionTokens = &r.maxOutputTokens
		} else {
			payload.MaxTokens = &r.maxOutputTokens
		}
	}
	payload.Tools = encodeTools(request.Tools)
	if len(payload.Tools) > 0 {
		payload.ToolChoice = "auto"
	}
	if r.reasoningEffort != "" && (len(payload.Tools) == 0 || r.capabilities.ReasoningWithTools) {
		payload.ReasoningEffort = r.reasoningEffort
	}
	if providers.TextStreamFromContext(ctx) != nil {
		payload.Stream = true
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
		return providers.Response{}, errors.New("openaicompat: empty choices")
	}

	choice := response.Choices[0]
	toolCalls := decodeToolCalls(choice.Message.ToolCalls)
	text := strings.TrimSpace(choice.Message.Content)
	if len(toolCalls) == 0 && text == "" {
		return providers.Response{}, errors.New("openaicompat: empty assistant reply")
	}

	return providers.Response{
		Text:      text,
		Model:     r.model,
		Provider:  providers.TypeOpenAICompat,
		ToolCalls: toolCalls,
		Usage:     openAIUsage(response.Usage),
	}, nil
}

func openAIUsage(usage chatCompletionUsage) providers.Usage {
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 &&
		usage.PromptTokensDetails.CachedTokens == 0 && usage.CompletionTokensDetails.ReasoningTokens == 0 {
		return providers.Usage{}
	}
	raw, _ := json.Marshal(usage)
	return providers.Usage{
		InputTokens:     usage.PromptTokens,
		OutputTokens:    usage.CompletionTokens,
		TotalTokens:     usage.TotalTokens,
		CachedTokens:    usage.PromptTokensDetails.CachedTokens,
		ReasoningTokens: usage.CompletionTokensDetails.ReasoningTokens,
		ProviderRaw:     raw,
	}
}
