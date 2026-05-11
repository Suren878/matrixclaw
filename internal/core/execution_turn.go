package core

import (
	"context"
	"errors"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

var errRunCanceled = errors.New("run canceled")

type turnExecution struct {
	RunID      string
	SessionID  string
	WorkingDir string
	Runtime    providers.Runtime
}

type turnStepOutcome int

const (
	turnStepContinue turnStepOutcome = iota
	turnStepWaitingApproval
	turnStepCompleted
)

type turnStepResult struct {
	Outcome              turnStepOutcome
	Assistant            *Message
	AssistantSaved       bool
	Response             providers.Response
	Err                  error
	MarkAssistantErrored bool
}

func newTurnExecution(run Run, session Session, runtime providers.Runtime) turnExecution {
	return turnExecution{
		RunID:      run.ID,
		SessionID:  session.ID,
		WorkingDir: session.WorkingDir,
		Runtime:    runtime,
	}
}

func (t turnExecution) hasRunScope() bool {
	return strings.TrimSpace(t.RunID) != "" && strings.TrimSpace(t.SessionID) != ""
}

func (t turnExecution) toolInput(toolCall providers.ToolCall) (ExecuteToolInput, error) {
	toolName := strings.TrimSpace(toolCall.Name)
	if toolName == "" {
		return ExecuteToolInput{}, errors.New("provider returned tool call without a name")
	}
	return ExecuteToolInput{
		SessionID:  t.SessionID,
		RunID:      t.RunID,
		ToolName:   toolName,
		ToolCallID: strings.TrimSpace(toolCall.ID),
		WorkingDir: t.WorkingDir,
		Args:       toolCall.Arguments,
	}, nil
}

func (c *Core) handleAssistantTurnResponse(runCtx context.Context, turn turnExecution, assistant *Message, assistantSaved bool, response providers.Response) turnStepResult {
	response.Text = sanitizeAssistantOutput(response.Text)
	if len(response.ToolCalls) > 0 {
		waitingApproval, execErr := c.executeRequestedTools(runCtx, turn, response.ToolCalls)
		if execErr != nil {
			return turnStepResult{
				Outcome:        turnStepCompleted,
				Assistant:      assistant,
				AssistantSaved: assistantSaved,
				Response:       response,
				Err:            execErr,
			}
		}
		if waitingApproval {
			return turnStepResult{Outcome: turnStepWaitingApproval}
		}
		return turnStepResult{Outcome: turnStepContinue}
	}

	assistantText := normalizeText(response.Text)
	if assistantText == "" {
		return turnStepResult{
			Outcome:              turnStepCompleted,
			Assistant:            assistant,
			AssistantSaved:       assistantSaved,
			Response:             response,
			Err:                  errors.New("empty assistant reply"),
			MarkAssistantErrored: true,
		}
	}
	return turnStepResult{
		Outcome:        turnStepCompleted,
		Assistant:      assistant,
		AssistantSaved: assistantSaved,
		Response:       response,
	}
}

func (c *Core) resumeApprovedTools(ctx context.Context, turn turnExecution) (bool, error) {
	if !turn.hasRunScope() {
		return false, nil
	}

	approvedApprovals, err := c.store.ListApprovals(ctx, turn.SessionID, ApprovalStateApproved)
	if err != nil {
		return false, err
	}
	pendingApprovals, err := c.store.ListApprovals(ctx, turn.SessionID, ApprovalStatePending)
	if err != nil {
		return false, err
	}

	messages, err := c.store.ListMessages(ctx, turn.SessionID, 0)
	if err != nil {
		return false, err
	}
	completedToolCalls := toolResultCallIDs(messages)

	waitingApproval := false
	for _, approval := range approvalsForRun(approvedApprovals, turn.RunID) {
		toolCallID := strings.TrimSpace(approval.ToolCallRef)
		if toolCallID == "" {
			continue
		}
		if _, exists := completedToolCalls[toolCallID]; exists {
			continue
		}

		result, err := c.replayApprovedTool(ctx, approval)
		if err != nil {
			return false, err
		}
		completedToolCalls[toolCallID] = struct{}{}
		if result.Approval != nil {
			waitingApproval = true
		}
	}

	if len(approvalsForRun(pendingApprovals, turn.RunID)) > 0 {
		return true, nil
	}
	return waitingApproval, nil
}

func (c *Core) generateAssistantTurn(ctx context.Context, turn turnExecution, request providers.Request) (Message, bool, providers.Response, error) {
	assistant := Message{
		ID:        c.newID("msg"),
		SessionID: turn.SessionID,
		RunID:     turn.RunID,
		Role:      MessageRoleAssistant,
	}
	assistantSaved := false
	streamSanitizer := newAssistantStreamSanitizer()
	streamCtx := providers.WithTextStream(ctx, func(delta string) error {
		canceled, err := c.isRunCanceled(ctx, turn.RunID)
		if err == nil && canceled {
			return errRunCanceled
		}
		if !assistantSaved && assistant.Content == "" {
			delta = strings.TrimPrefix(delta, "\n")
		}
		delta = streamSanitizer.Push(delta)
		if delta == "" {
			return nil
		}

		now := c.now().UTC()
		assistant.Content += delta
		assistant.Parts = NormalizeMessageParts(assistant.Content, nil)
		assistant.UpdatedAt = now

		if !assistantSaved {
			assistant.CreatedAt = now
			if err := c.store.SaveMessage(ctx, assistant); err != nil {
				return err
			}
			assistantSaved = true
			c.publishEvent(Event{
				Type:      EventMessageCreated,
				SessionID: turn.SessionID,
				RunID:     turn.RunID,
				Payload:   assistant,
			})
			return nil
		}

		if err := c.store.UpdateMessage(ctx, assistant); err != nil {
			return err
		}
		c.publishEvent(Event{
			Type:      EventMessageUpdated,
			SessionID: turn.SessionID,
			RunID:     turn.RunID,
			Payload:   assistant,
		})
		return nil
	})

	response, err := turn.Runtime.Generate(streamCtx, request)
	return assistant, assistantSaved, response, err
}

func (c *Core) executeRequestedTools(ctx context.Context, turn turnExecution, toolCalls []providers.ToolCall) (bool, error) {
	waitingApproval := false
	for _, toolCall := range toolCalls {
		input, err := turn.toolInput(toolCall)
		if err != nil {
			return false, err
		}
		result, err := c.ExecuteTool(ctx, input)
		if err != nil {
			return false, err
		}
		if result.Approval != nil {
			waitingApproval = true
		}
	}
	return waitingApproval, nil
}
