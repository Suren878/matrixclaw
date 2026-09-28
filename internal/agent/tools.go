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

// callState is where a call stands after runCall.
type callState int

const (
	callDone callState = iota
	// callPending waits for approval; later calls of its batch still run.
	callPending
	// callBarrier waits for approval and holds back the later calls of its batch.
	callBarrier
)

// executeBatch runs the response's tool calls in order. A call waiting for approval
// parks while the rest of the batch runs, unless it is a barrier: every later call
// is then journaled deferred and runs once the run's approvals are decided.
func (r *run) executeBatch(ctx context.Context, response providers.Response) (bool, error) {
	seen := make(map[string]providers.ToolCall)
	waiting, barrier := false, false
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
		if id == "" {
			id = r.NewID("tool")
		}
		request := callRequest{id: id, name: name, args: toolCall.Arguments, workingDir: r.task.WorkingDir}
		if barrier {
			if err := r.deferCall(ctx, request); err != nil {
				return false, err
			}
			continue
		}
		state, err := r.runCall(ctx, request)
		if err != nil {
			return false, err
		}
		waiting = waiting || state != callDone
		barrier = state == callBarrier
	}
	return waiting, nil
}

// resumeDecided answers the run's decided approvals that have no result yet: a
// granted call runs, a denied one gets the denial as its result and the calls it
// held back are not run. Once none is pending, the calls deferred behind a
// barrier run. It reports whether the run still waits for approval.
func (r *run) resumeDecided(ctx context.Context) (bool, error) {
	decided, err := r.Inbox.Peek(ctx, r.task.RunID, InputDecided)
	if err != nil {
		return false, err
	}
	for _, input := range decided {
		if r.history.hasResult(input.ToolCallID) {
			continue
		}
		request := callRequest{id: input.ToolCallID, name: input.ToolName, args: input.Args, workingDir: input.WorkingDir, approved: true}
		if input.Denied {
			err = r.denyCall(ctx, request, input.Reason)
		} else {
			_, err = r.runCall(ctx, request)
		}
		if err != nil {
			return false, err
		}
	}
	pending, err := r.Approvals.Pending(ctx, r.task.RunID)
	if err != nil || pending {
		return pending, err
	}
	return r.runDeferred(ctx)
}

// runDeferred runs the calls held back by a barrier in call order, until one of
// them is a barrier again.
func (r *run) runDeferred(ctx context.Context) (bool, error) {
	waiting := false
	for _, request := range r.deferredCalls("") {
		state, err := r.runCall(ctx, request)
		if err != nil {
			return false, err
		}
		if state == callBarrier {
			return true, nil
		}
		waiting = waiting || state == callPending
	}
	return waiting, nil
}

// denyCall answers a denied call with the denial and every call its barrier held
// back with an error, so the model re-plans instead of running them.
func (r *run) denyCall(ctx context.Context, req callRequest, reason string) error {
	for _, held := range r.deferredCalls(req.id) {
		if err := r.rejectCall(ctx, held, fmt.Sprintf("Not run: an earlier call in this batch was denied (%s).", req.name)); err != nil {
			return err
		}
	}
	return r.finishCall(ctx, req, r.toolCall(req), DenialResult(reason))
}

// deferredCalls lists the run's calls still held back, in call order: behind
// the barrier with the given ID, or behind any barrier when it is empty.
func (r *run) deferredCalls(barrier string) []callRequest {
	var out []callRequest
	behind := ""
	for _, message := range r.history.all() {
		if message.RunID != r.task.RunID {
			continue
		}
		for _, part := range message.Parts {
			call := part.ToolCall
			if call == nil {
				continue
			}
			if !call.Deferred {
				behind = call.ID
				continue
			}
			if (barrier == "" || barrier == behind) && !r.history.hasResult(call.ID) {
				out = append(out, callRequest{id: call.ID, name: call.Name, args: json.RawMessage(call.Input), workingDir: r.task.WorkingDir})
			}
		}
	}
	return out
}

// runCall authorizes, journals and executes one call and reports where it stands.
func (r *run) runCall(ctx context.Context, req callRequest) (callState, error) {
	call := r.toolCall(req)
	decision, err := r.Tools.Authorize(ctx, req.name, call)
	if err != nil {
		return callDone, err
	}
	if !decision.Allowed {
		return callDone, r.rejectCall(ctx, req, decision.Reason)
	}
	if err := r.startCall(ctx, req); err != nil {
		return callDone, err
	}
	if err := r.checkpoint(ctx, PhaseTool, req.id, req.name); err != nil {
		return callDone, err
	}
	result, err := r.Tools.Execute(ctx, req.name, call)
	if err != nil {
		return callDone, err
	}
	if result.Approval == nil || req.approved {
		return callDone, r.finishCall(ctx, req, call, result)
	}
	if err := r.Approvals.Request(ctx, Pending{RunID: r.task.RunID, SessionID: r.task.SessionID, ToolCallID: req.id, ToolName: req.name, Request: *result.Approval}); err != nil {
		return callDone, err
	}
	if decision.Barrier {
		return callBarrier, nil
	}
	return callPending, nil
}

// startCall journals a call about to run and announces it; a call held back by
// a barrier loses its deferred mark.
func (r *run) startCall(ctx context.Context, req callRequest) error {
	if existing, ok := r.history.message(req.id); ok && !deferredCall(existing) {
		return nil
	}
	if err := r.writeCall(ctx, req, false); err != nil {
		return err
	}
	r.Sink.Emit(Event{Kind: EventToolRequested, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name})
	return nil
}

// deferCall journals a call held back by an approval barrier.
func (r *run) deferCall(ctx context.Context, req callRequest) error {
	if _, ok := r.history.message(req.id); ok {
		return nil
	}
	message := r.callMessage(req, false)
	message.Parts[0].ToolCall.Deferred = true
	return r.history.append(ctx, message)
}

// writeCall journals the call's message, or updates the one already journaled.
func (r *run) writeCall(ctx context.Context, req callRequest, finished bool) error {
	message := r.callMessage(req, finished)
	existing, ok := r.history.message(req.id)
	if !ok {
		return r.history.append(ctx, message)
	}
	message.CreatedAt = existing.CreatedAt
	return r.history.finish(ctx, message)
}

func deferredCall(message transcript.Message) bool {
	for _, part := range message.Parts {
		if part.ToolCall != nil && part.ToolCall.Deferred {
			return true
		}
	}
	return false
}

func (r *run) toolCall(req callRequest) tools.Call {
	return tools.Call{
		SessionID:   r.task.SessionID,
		RunID:       r.task.RunID,
		ToolCallID:  req.id,
		Client:      r.task.Client,
		ExternalKey: r.task.ExternalKey,
		WorkingDir:  req.workingDir,
		Approved:    req.approved,
		Args:        req.args,
	}
}

// rejectCall journals a call that may not run with its error as the result, so the
// model can correct it.
func (r *run) rejectCall(ctx context.Context, req callRequest, reason string) error {
	if err := r.writeCall(ctx, req, true); err != nil {
		return err
	}
	result := tools.Result{Content: reason, IsError: true}
	if _, err := r.appendResult(ctx, req, result); err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
	return r.checkpoint(ctx, PhaseModel, "", "")
}

func (r *run) finishCall(ctx context.Context, req callRequest, call tools.Call, result tools.Result) error {
	if err := r.writeCall(ctx, req, true); err != nil {
		return err
	}
	message, err := r.appendResult(ctx, req, result)
	if err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
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
	text = strings.TrimSpace(text)
	if message == nil || text == "" {
		return
	}
	for i := range message.Parts {
		result := message.Parts[i].ToolResult
		if result == nil {
			continue
		}
		result.Content = appendGuidanceBlock(result.Content, "User guidance: "+text)
		result.Guidance = append(result.Guidance, text)
		message.Content = normalizeToolContent(result.Content)
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
