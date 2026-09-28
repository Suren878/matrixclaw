package telegram

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// runMessagesFirstLoad bounds the first load of a run the worker holds no cursor
// for yet, such as after a restart; later loads ask only for what is newer.
const runMessagesFirstLoad = 200

// runMessages returns the run's messages. After the first load it asks only for
// the messages above the state's cursor: new ones and those that may still change.
func (w *Worker) runMessages(ctx context.Context, daemon *daemonclient.Client, sessionID string, runID string, state *runDeliveryState) ([]transcript.Message, error) {
	var batch []transcript.Message
	var err error
	if state.messagesLoaded {
		batch, err = daemon.ListMessagesAfter(ctx, sessionID, state.afterSeq, 0)
	} else {
		batch, err = daemon.ListMessages(ctx, sessionID, runMessagesFirstLoad)
	}
	if err != nil {
		return nil, err
	}
	state.messagesLoaded = true
	state.mergeMessages(batch, runID)
	return state.messages, nil
}

func (s *runDeliveryState) mergeMessages(batch []transcript.Message, runID string) {
	if s.messageIndex == nil {
		s.messageIndex = map[string]int{}
	}
	latest := s.afterSeq
	for _, message := range batch {
		latest = max(latest, message.Seq)
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) {
			continue
		}
		if i, ok := s.messageIndex[message.ID]; ok {
			s.messages[i] = message
			continue
		}
		s.messageIndex[message.ID] = len(s.messages)
		s.messages = append(s.messages, message)
	}
	s.afterSeq = settledSeq(s.messages, latest)
}

// settledSeq is the seq just below the run's first message that may still change
// (a tool call without a result, a reply still streaming, or the run's last
// message), or latest when there is none.
func settledSeq(messages []transcript.Message, latest int64) int64 {
	answered := map[string]bool{}
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil {
				answered[part.ToolResult.ToolCallID] = true
			}
		}
	}
	for i, message := range messages {
		if i == len(messages)-1 || messageMayChange(message, answered) {
			return min(latest, message.Seq-1)
		}
	}
	return latest
}

func messageMayChange(message transcript.Message, answered map[string]bool) bool {
	if message.Role != transcript.MessageRoleAssistant {
		return false
	}
	calls, finished := false, false
	for _, part := range message.Parts {
		if part.ToolCall != nil {
			calls = true
			if !answered[part.ToolCall.ID] {
				return true
			}
		}
		if part.Finish != nil {
			finished = true
		}
	}
	return !calls && !finished
}
