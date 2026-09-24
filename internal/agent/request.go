package agent

import (
	"context"
	"fmt"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ToolUseAllowed reports whether the model can receive tool definitions.
func ToolUseAllowed(model Model) bool {
	if profiler, ok := model.(providers.RuntimeProfiler); ok {
		if providers.NormalizeRuntimeProfile(profiler.RuntimeProfile()).ToolUseMode == providers.ToolUseDisabled {
			return false
		}
	}
	capabilities, ok := model.(providers.RuntimeCapabilityProvider)
	if !ok {
		return true
	}
	return capabilities.ModelCapabilities().ToolCalling
}

// ImageInputAllowed reports whether the model accepts inline images.
func ImageInputAllowed(model Model) bool {
	capabilities, ok := model.(providers.RuntimeCapabilityProvider)
	return ok && capabilities.ModelCapabilities().ImageInput
}

// modelIdentity is the Provider and Model the model stamps on its replies; signed
// reasoning from any other pair is not replayed to it.
func modelIdentity(model Model) agentcontext.Identity {
	identifier, ok := model.(providers.RuntimeIdentifier)
	if !ok {
		return agentcontext.Identity{}
	}
	provider, name := identifier.Identity()
	return agentcontext.Identity{Provider: provider, Model: name}
}

func (r *run) buildRequest(ctx context.Context) (providers.Request, error) {
	summary, effective := agentcontext.LatestSummaryForRun(r.history.all(), r.task.RunID)
	system, custom := r.Prompts.System(ctx, summary, effective)
	request := providers.Request{
		RunID:              r.task.RunID,
		SessionID:          r.task.SessionID,
		SystemPrompt:       system,
		CustomInstructions: custom,
		CacheKey:           r.task.SessionID,
	}
	if !ToolUseAllowed(r.task.Model) {
		request.Messages = agentcontext.TextOnlyConversation(effective, r.task.RunID)
		request.Messages = providers.NormalizeMessages(request.Messages, providers.ToolUseDisabled)
		return request, nil
	}
	messages, err := agentcontext.Conversation(ctx, effective, r.Attachments, r.task.RunID, ImageInputAllowed(r.task.Model), modelIdentity(r.task.Model))
	if err != nil {
		return providers.Request{}, err
	}
	request.Messages = messages
	request.Tools = toolDefinitions(r.Tools.Specs(ctx))
	return request, nil
}

func toolDefinitions(specs []tools.Spec) []providers.ToolDefinition {
	if specs == nil {
		return nil
	}
	definitions := make([]providers.ToolDefinition, 0, len(specs))
	for _, spec := range specs {
		definitions = append(definitions, providers.ToolDefinition{Name: spec.ID, Description: spec.Description, InputSchema: spec.InputJSONSchema})
	}
	return definitions
}

// contextBudget is the context budget of one step: the fixed prompt cost and the model window.
type contextBudget struct {
	base, window int
}

func (r *run) budget(ctx context.Context) (contextBudget, error) {
	base, window, err := r.Prompts.Budget(ctx)
	return contextBudget{base: base, window: window}, err
}

func (r *run) autoCompact(ctx context.Context, b contextBudget) (bool, error) {
	messages := r.history.all()
	recommended, _ := agentcontext.Recommendation(agentcontext.SessionTokens(b.base, messages), b.window)
	if !recommended || agentcontext.CompactBackoffActive(messages) {
		return false, nil
	}
	return r.compactHistory(ctx, messages, b.base)
}

func requestNeedsCompact(request providers.Request, b contextBudget) bool {
	threshold := agentcontext.Threshold(b.window)
	return threshold > 0 && agentcontext.EstimateRequestTokens(request) >= threshold
}

func (r *run) compactHistory(ctx context.Context, messages []transcript.Message, base int) (bool, error) {
	if _, effective := agentcontext.LatestSummary(messages); len(effective) == 0 {
		return false, nil
	}
	content, err := agentcontext.Compact(ctx, summaryModel{r: r}, agentcontext.CompactInput{
		SessionID:    r.task.SessionID,
		History:      messages,
		BaseTokens:   base,
		PlanSnapshot: r.Prompts.PlanSnapshot(ctx),
	})
	if err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	content = strings.TrimSpace(content)
	now := r.Now()
	marker := transcript.Message{
		ID:        r.NewID("msg"),
		SessionID: r.task.SessionID,
		Role:      transcript.MessageRoleSystem,
		Content:   content,
		Parts:     []transcript.MessagePart{{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: content}}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := r.history.append(ctx, marker); err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}
