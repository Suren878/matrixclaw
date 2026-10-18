package agent

import (
	"context"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ToolUseAllowed reports whether the model can receive tool definitions.
func ToolUseAllowed(model Model) bool {
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

// buildRequest assembles the step's request from the run's fixed prompt and tools:
// the boundary's summary first, then the history after it; the final turn keeps
// the tools defined but forbids calling them, so the cached prefix survives.
func (r *run) buildRequest(ctx context.Context, final StopReason) (providers.Request, error) {
	compaction, messages := r.history.window()
	if len(messages) > 0 {
		r.requestSeq = messages[len(messages)-1].Seq
	}
	messages = r.sent(messages)
	request := providers.Request{
		SystemPrompt:    r.system,
		CacheKey:        r.task.SessionID,
		MaxOutputTokens: r.state.OutputLimit,
	}
	if final != "" {
		request.ToolChoice = providers.ToolChoiceNone
	}
	summary := agentcontext.SummaryMessages(compaction)
	if !ToolUseAllowed(r.task.Model) {
		request.Messages = append(summary, agentcontext.TextOnlyConversation(messages)...)
		return request, nil
	}
	conversation, err := agentcontext.Conversation(ctx, messages, r.images, ImageInputAllowed(r.task.Model), modelIdentity(r.task.Model))
	if err != nil {
		return providers.Request{}, err
	}
	request.Messages = append(summary, conversation...)
	request.Tools = r.tools
	return request, nil
}

// withCustomInstructions appends the user's own instructions to the system prompt.
func withCustomInstructions(system string, custom string) string {
	system, custom = strings.TrimSpace(system), strings.TrimSpace(custom)
	switch {
	case custom == "":
		return system
	case system == "":
		return "User custom instructions:\n" + custom
	default:
		return system + "\n\nUser custom instructions:\n" + custom
	}
}

// sent is messages as the run's requests carry them: elided, each tool round's
// results within their share of the window, and without the reasoning signed
// before the last history edit.
func (r *run) sent(messages []transcript.Message) []transcript.Message {
	capped := agentcontext.CapRoundResults(agentcontext.Elide(messages, r.elision()), r.contextLimit())
	return agentcontext.WithoutSignedReasoning(capped, r.state.HistoryEdit)
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
