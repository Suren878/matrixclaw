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
}

// executeBatch runs the response's tool calls as one batch.
func (r *run) executeBatch(ctx context.Context, response providers.Response) (bool, error) {
	requests, err := r.batchRequests(response)
	if err != nil {
		return false, err
	}
	return r.runCalls(ctx, requests)
}

// batchRequests turns the response's tool calls into requests, skipping repeats
// of a call already answered; a call ID reused for another call fails the run.
func (r *run) batchRequests(response providers.Response) ([]callRequest, error) {
	seen := make(map[string]providers.ToolCall)
	var requests []callRequest
	for _, toolCall := range response.ToolCalls {
		id := strings.TrimSpace(toolCall.ID)
		if id != "" {
			if prior, ok := seen[id]; ok {
				if !sameRequestedTool(prior.Name, prior.Arguments, toolCall) {
					return nil, fmt.Errorf("tool call ID %q reused with different arguments", id)
				}
				continue
			}
			if prior, ok := r.history.callMessage(id); ok {
				if prior.RunID != r.task.RunID {
					return nil, fmt.Errorf("tool call ID %q belongs to another run", id)
				}
				for _, part := range prior.Parts {
					if part.ToolCall != nil && part.ToolCall.ID == id && !sameRequestedTool(part.ToolCall.Name, []byte(part.ToolCall.Input), toolCall) {
						return nil, fmt.Errorf("tool call ID %q reused with different arguments", id)
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
			return nil, errors.New("provider returned tool call without a name")
		}
		if id == "" {
			id = r.NewID("tool")
		}
		requests = append(requests, callRequest{id: id, name: name, args: toolCall.Arguments, workingDir: r.task.WorkingDir})
	}
	return requests, nil
}

// resumeDecided answers the run's decided approvals that have no result yet: a
// denied call gets the denial as its result and the calls it held back are not
// run, the granted ones run. Once none is pending, the calls deferred behind a
// barrier run. It reports whether the run still waits for approval.
func (r *run) resumeDecided(ctx context.Context) (bool, error) {
	decided, err := r.Inbox.Peek(ctx, r.task.RunID, InputDecided)
	if err != nil {
		return false, err
	}
	var granted []callRequest
	for _, input := range decided {
		if r.history.hasResult(input.ToolCallID) {
			continue
		}
		request := callRequest{id: input.ToolCallID, name: input.ToolName, args: input.Args, workingDir: input.WorkingDir}
		if !input.Denied {
			granted = append(granted, request)
			continue
		}
		if err := r.denyCall(ctx, request, input.Reason); err != nil {
			return false, err
		}
	}
	if _, err := r.runCalls(ctx, granted); err != nil {
		return false, err
	}
	pending, err := r.Approvals.Pending(ctx, r.task.RunID)
	if err != nil || pending {
		return pending, err
	}
	return r.runCalls(ctx, r.deferredCalls(""))
}

// denyCall answers a denied call with the denial and every call its barrier held
// back with an error, so the model re-plans instead of running them.
func (r *run) denyCall(ctx context.Context, req callRequest, reason string) error {
	for _, held := range r.deferredCalls(req.id) {
		if err := r.rejectCall(ctx, held, tools.Result{Content: fmt.Sprintf("Not run: an earlier call in this batch was denied (%s).", req.name), Status: tools.ResultStatusError}); err != nil {
			return err
		}
	}
	return r.finishCall(ctx, req, DenialResult(reason))
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
		Args:        req.args,
	}
}

// rejectCall journals a call that did not run with result, an error the model
// can act on.
func (r *run) rejectCall(ctx context.Context, req callRequest, result tools.Result) error {
	if err := r.writeCall(ctx, req, true); err != nil {
		return err
	}
	return r.answerCall(ctx, req, result)
}

// answerCall journals the result of a call already journaled as finished.
func (r *run) answerCall(ctx context.Context, req callRequest, result tools.Result) error {
	if _, err := r.appendResult(ctx, req, result); err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
	return nil
}

func (r *run) finishCall(ctx context.Context, req callRequest, result tools.Result) error {
	if err := r.writeCall(ctx, req, true); err != nil {
		return err
	}
	message, err := r.appendResult(ctx, req, result)
	if err != nil {
		return err
	}
	r.counters.observeCall(req.name, req.args, result)
	r.Sink.Emit(Event{Kind: EventToolFinished, SessionID: r.task.SessionID, RunID: r.task.RunID, ToolCallID: req.id, ToolName: req.name, ResultMessageID: message.ID, Result: result})
	return nil
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
