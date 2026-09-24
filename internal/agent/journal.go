package agent

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// history is the run's in-memory transcript; every write goes through the Journal
// port first and is then announced on the Sink.
type history struct {
	port     Journal
	sink     Sink
	messages []transcript.Message
	index    map[string]int
	calls    map[string]int
	results  map[string]struct{}
}

func newHistory(port Journal, sink Sink, window Window) *history {
	h := &history{port: port, sink: sink, index: map[string]int{}, calls: map[string]int{}, results: map[string]struct{}{}}
	for _, message := range window.Messages {
		h.add(message)
	}
	return h
}

func (h *history) all() []transcript.Message {
	return h.messages
}

func (h *history) message(id string) (transcript.Message, bool) {
	i, ok := h.index[id]
	if !ok {
		return transcript.Message{}, false
	}
	return h.messages[i], true
}

// callMessage is the newest message carrying a tool call with the ID.
func (h *history) callMessage(callID string) (transcript.Message, bool) {
	i, ok := h.calls[strings.TrimSpace(callID)]
	if !ok {
		return transcript.Message{}, false
	}
	return h.messages[i], true
}

func (h *history) hasResult(callID string) bool {
	_, ok := h.results[strings.TrimSpace(callID)]
	return ok
}

func (h *history) append(ctx context.Context, message transcript.Message) error {
	seq, err := h.port.Append(ctx, message)
	if err != nil {
		return err
	}
	message.Seq = seq
	h.add(message)
	h.emit(EventMessageCreated, message)
	return nil
}

func (h *history) beginStreaming(ctx context.Context, message transcript.Message) error {
	seq, err := h.port.BeginStreaming(ctx, message)
	if err != nil {
		return err
	}
	message.Seq = seq
	h.add(message)
	h.emit(EventMessageCreated, message)
	return nil
}

func (h *history) stream(ctx context.Context, message transcript.Message) error {
	if err := h.port.Stream(ctx, message); err != nil {
		return err
	}
	h.replace(message)
	h.emit(EventMessageUpdated, message)
	return nil
}

func (h *history) finish(ctx context.Context, message transcript.Message) error {
	if err := h.port.FinishStreaming(ctx, message); err != nil {
		return err
	}
	h.replace(message)
	h.emit(EventMessageUpdated, message)
	return nil
}

func (h *history) add(message transcript.Message) {
	h.index[message.ID] = len(h.messages)
	h.messages = append(h.messages, message)
	h.indexParts(len(h.messages) - 1)
}

func (h *history) replace(message transcript.Message) {
	i, ok := h.index[message.ID]
	if !ok {
		h.add(message)
		return
	}
	message.Seq = h.messages[i].Seq
	h.messages[i] = message
	h.indexParts(i)
}

func (h *history) indexParts(i int) {
	for _, part := range h.messages[i].Parts {
		if part.ToolCall != nil {
			if id := strings.TrimSpace(part.ToolCall.ID); id != "" {
				h.calls[id] = i
			}
		}
		if part.ToolResult != nil {
			if id := strings.TrimSpace(part.ToolResult.ToolCallID); id != "" {
				h.results[id] = struct{}{}
			}
		}
	}
}

func (h *history) emit(kind EventKind, message transcript.Message) {
	h.sink.Emit(Event{Kind: kind, SessionID: message.SessionID, RunID: message.RunID, Message: message})
}
