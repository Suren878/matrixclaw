package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// callRequest is one tool call the engine is about to run.
type callRequest struct {
	id         string
	name       string
	args       json.RawMessage
	workingDir string
	approved   bool
}

// executeBatch runs the response's tool calls in order; approval-bound calls park,
// the rest of the batch still runs.
func (r *run) executeBatch(ctx context.Context, response providers.Response) (bool, error) {
	seen := make(map[string]providers.ToolCall)
	waiting := false
	for _, toolCall := range response.ToolCalls {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		id := strings.TrimSpace(toolCall.ID)
		if id != "" {
			if prior, ok := seen[id]; ok {
				if !sameRequestedTool(prior.Name, prior.Arguments, toolCall) {
					return false, fmt.Errorf("tool call ID %q reused with different arguments", id)
				}
				continue
			}
			if prior, ok := r.history.callMessage(id); ok {
				if prior.RunID != r.task.RunID {
					return false, fmt.Errorf("tool call ID %q belongs to another run", id)
				}
				for _, part := range prior.Parts {
					if part.ToolCall != nil && part.ToolCall.ID == id && !sameRequestedTool(part.ToolCall.Name, []byte(part.ToolCall.Input), toolCall) {
						return false, fmt.Errorf("tool call ID %q reused with different arguments", id)
					}
				}
				if r.history.hasResult(id) {
					continue
				}
			}
			seen[id] = toolCall
		}
		name := strings.TrimSpace(toolCall.Name)
		if name == "" {
			return false, errors.New("provider returned tool call without a name")
		}
		request := callRequest{id: id, name: name, args: toolCall.Arguments, workingDir: r.task.WorkingDir}
		pending, err := r.runCall(ctx, request)
		if err != nil {
			return false, err
		}
		waiting = waiting || pending
	}
	return waiting, nil
}

// resumeApproved runs granted calls that have no result yet and reports whether
// approvals of the run are still open.
func (r *run) resumeApproved(ctx context.Context) (bool, error) {
	pending, err := r.Approvals.Pending(ctx, r.task.RunID)
	if err != nil {
		return false, err
	}
	approved, err := r.Inbox.Peek(ctx, r.task.RunID, InputApproved)
	if err != nil {
		return false, err
	}
	for _, input := range approved {
		if r.history.hasResult(input.ToolCallID) {
			continue
		}
		request := callRequest{id: input.ToolCallID, name: input.ToolName, args: input.Args, workingDir: input.WorkingDir, approved: true}
		if _, err := r.runCall(ctx, request); err != nil {
			return false, err
		}
	}
	return pending, nil
}

// runCall authorizes, journals and executes one call; it reports whether the call
// now waits for approval.
func (r *run) runCall(ctx context.Context, req callRequest) (bool, error) {
	if req.id == "" {
		req.id = r.NewID("tool")
	}
	call := tools.Call{
		SessionID:   r.task.SessionID,
		RunID:       r.task.RunID,
		ToolCallID:  req.id,
		Client:      r.task.Client,
		ExternalKey: r.task.ExternalKey,
		WorkingDir:  req.workingDir,
		Approved:    req.approved,
		Args:        req.args,
	}
	decision, err := r.Tools.Authorize(ctx, req.name, call)
	if err != nil {
		return false, err
	}
	if !decision.Allowed {
		if req.approved {
			return false, errors.New(decision.Reason)
		}
		return false, r.rejectCall(ctx, req, decision.Reason)
	}
	if _, exists := r.history.message(req.id); !exists {
		if err := r.history.append(ctx, r.callMessage(req, false)); err != nil {
			return false, err
		}
		r.Sink.Emit(Event{Kind: EventToolRequested, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name})
	}
	if err := r.checkpoint(ctx, PhaseTool, req.id, req.name); err != nil {
		return false, err
	}
	result, err := r.Tools.Execute(ctx, req.name, call)
	if err != nil {
		return false, err
	}
	if result.Approval != nil && !req.approved {
		err := r.Approvals.Request(ctx, Pending{RunID: r.task.RunID, SessionID: r.task.SessionID, ToolCallID: req.id, ToolName: req.name, Request: *result.Approval})
		return err == nil, err
	}
	return false, r.finishCall(ctx, req, call, result)
}

// rejectCall journals a call that may not run with its error as the result, so the
// model can correct it.
func (r *run) rejectCall(ctx context.Context, req callRequest, reason string) error {
	if err := r.history.append(ctx, r.callMessage(req, true)); err != nil {
		return err
	}
	_, err := r.appendResult(ctx, req, tools.Result{Content: reason, IsError: true})
	return err
}

func (r *run) finishCall(ctx context.Context, req callRequest, call tools.Call, result tools.Result) error {
	finished := ToolCallMessage(req.id, r.task.SessionID, r.task.RunID, req.name, req.args, true, r.Now())
	if existing, ok := r.history.message(req.id); ok {
		finished.CreatedAt = existing.CreatedAt
	}
	if err := r.history.finish(ctx, finished); err != nil {
		return err
	}
	message, err := r.appendResult(ctx, req, result)
	if err != nil {
		return err
	}
	r.Sink.Emit(Event{Kind: EventToolFinished, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name, ResultMessageID: message.ID, Result: result})
	if err := r.Tools.Finish(ctx, req.name, call, result, message); err != nil {
		return err
	}
	return r.checkpoint(ctx, PhaseModel, "", "")
}

// appendResult writes a tool result with any pending steer guidance merged into it.
// The steers are consumed only once the result is written, even if ctx stops then.
func (r *run) appendResult(ctx context.Context, req callRequest, result tools.Result) (transcript.Message, error) {
	message, err := ToolResultMessage(r.NewID("tool_result"), r.task.SessionID, r.task.RunID, req.id, req.name, result, r.Now())
	if err != nil {
		return transcript.Message{}, err
	}
	steers, err := r.Inbox.Peek(ctx, r.task.RunID, InputSteer)
	if err != nil {
		return transcript.Message{}, err
	}
	ids := make([]string, 0, len(steers))
	for _, steer := range steers {
		appendUserGuidanceToToolResult(&message, steer.Text)
		ids = append(ids, steer.ID)
	}
	if err := r.history.append(ctx, message); err != nil {
		return transcript.Message{}, err
	}
	if len(ids) == 0 {
		return message, nil
	}
	return message, r.Inbox.Consume(context.WithoutCancel(ctx), r.task.RunID, ids)
}

func (r *run) callMessage(req callRequest, finished bool) transcript.Message {
	return ToolCallMessage(req.id, r.task.SessionID, r.task.RunID, req.name, req.args, finished, r.Now())
}

func sameRequestedTool(name string, arguments []byte, call providers.ToolCall) bool {
	if strings.TrimSpace(name) != strings.TrimSpace(call.Name) {
		return false
	}
	var left, right any
	if len(arguments) == 0 {
		arguments = []byte(`{}`)
	}
	if len(call.Arguments) == 0 {
		call.Arguments = []byte(`{}`)
	}
	if !json.Valid(arguments) || !json.Valid(call.Arguments) {
		return strings.TrimSpace(string(arguments)) == strings.TrimSpace(string(call.Arguments))
	}
	leftDecoder := json.NewDecoder(bytes.NewReader(arguments))
	rightDecoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	leftDecoder.UseNumber()
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&left) != nil || rightDecoder.Decode(&right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func appendUserGuidanceToToolResult(message *transcript.Message, text string) {
	if message == nil {
		return
	}
	guidance := "User guidance: " + strings.TrimSpace(text)
	if strings.TrimSpace(guidance) == "User guidance:" {
		return
	}
	for i := range message.Parts {
		if message.Parts[i].ToolResult == nil {
			continue
		}
		content := appendGuidanceBlock(message.Parts[i].ToolResult.Content, guidance)
		message.Parts[i].ToolResult.Content = content
		message.Content = normalizeToolContent(content)
		return
	}
}

func appendGuidanceBlock(content string, guidance string) string {
	content = strings.TrimSpace(content)
	guidance = strings.TrimSpace(guidance)
	if guidance == "" {
		return content
	}
	if content == "" {
		return guidance
	}
	return content + "\n\n" + guidance
}
