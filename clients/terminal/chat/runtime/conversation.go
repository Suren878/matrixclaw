package runtime

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	surfacechat "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/core"
)

// chatRow is one chat item and what it was built from: a row with the same ID
// and signature as before is reused with its render cache.
type chatRow struct {
	item surfacechat.MessageItem
	sig  rowSig
}

type rowSig struct {
	rev       uint64
	resultRev uint64
	tool      core.ToolUpdate
	pending   bool
	note      surfacepermission.PermissionNotification
	sub       surfacemessage.Subagent
	text      string
}

// chatSource is what the chat rows are built from.
type chatSource struct {
	read     *readmodel.Model
	tools    surfacechat.ToolContext
	updates  map[string]core.ToolUpdate
	pending  map[string]bool
	notes    map[string]surfacepermission.PermissionNotification
	messages []surfacemessage.Message
}

// buildChatRows turns the read model's transcript, with the terminal's own
// notes merged in by time, into chat rows.
func buildChatRows(sty *surfacestyles.Styles, read *readmodel.Model, notes []surfacemessage.Message) []chatRow {
	t := chatSource{
		read:     read,
		messages: read.Messages(),
		updates:  map[string]core.ToolUpdate{},
		pending:  map[string]bool{},
		notes:    map[string]surfacepermission.PermissionNotification{},
	}
	if len(notes) > 0 {
		t.messages = append(slices.Clone(t.messages), notes...)
		slices.SortStableFunc(t.messages, func(a, b surfacemessage.Message) int { return cmp.Compare(a.CreatedAt, b.CreatedAt) })
	}
	t.tools = surfacechat.ToolContext{Results: surfacechat.BuildToolResultMap(t.messages), Subagents: map[string]surfacemessage.Subagent{}}
	for _, sub := range read.Subagents() {
		if sub.ParentToolCallID != "" {
			t.tools.Subagents[sub.ParentToolCallID] = sub
		}
	}
	for _, update := range read.ToolUpdates() {
		t.updates[update.ToolCallID] = update
	}
	for _, approval := range read.Approvals() {
		t.pending[approval.ToolCallID] = true
	}
	for _, note := range read.ApprovalNotifications() {
		t.notes[note.ToolCallID] = note
	}
	return t.rows(sty)
}

func (t chatSource) rows(sty *surfacestyles.Styles) []chatRow {
	rows := make([]chatRow, 0, len(t.messages))
	var lastUserMessageTime time.Time
	for i := 0; i < len(t.messages); {
		if grouped, sig, next, ok := t.readGroup(sty, i); ok {
			rows = append(rows, chatRow{item: grouped, sig: sig})
			i = next
			continue
		}
		msg := &t.messages[i]
		if msg.Role == surfacemessage.User && msg.CreatedAt > 0 {
			lastUserMessageTime = time.Unix(msg.CreatedAt, 0)
		}
		sig := rowSig{rev: t.read.Revision(msg.ID)}
		if sig.rev == 0 {
			sig.text = msg.Content().Text + "|" + string(msg.FinishReason())
		}
		items := surfacechat.ExtractMessageItems(sty, msg, t.tools)
		for _, item := range items {
			if tool, ok := item.(surfacechat.ToolMessageItem); ok {
				rows = append(rows, chatRow{item: item, sig: t.toolRow(tool, sig.rev)})
				continue
			}
			rows = append(rows, chatRow{item: item, sig: sig})
		}
		if msg.Role == surfacemessage.Assistant && len(items) > 0 {
			if finish := msg.FinishPart(); finish != nil && finish.Reason == surfacemessage.FinishReasonEndTurn {
				info := sig
				info.text = lastUserMessageTime.String()
				rows = append(rows, chatRow{item: surfacechat.NewAssistantInfoItem(sty, msg, lastUserMessageTime), sig: info})
			}
		}
		i++
	}
	return rows
}

// toolRow sets a tool row's status from the daemon's tool state and approvals
// and returns its signature.
func (t chatSource) toolRow(item surfacechat.ToolMessageItem, rev uint64) rowSig {
	id := item.ToolCall().ID
	sig := rowSig{rev: rev, resultRev: t.read.ResultRevision(id), tool: t.updates[id], pending: t.pending[id], note: t.notes[id], sub: t.tools.Subagents[id]}
	if sig.pending {
		item.SetStatus(surfacechat.ToolStatusAwaitingPermission)
		return sig
	}
	switch sig.tool.State {
	case core.ToolLifecycleWaitingApproval:
		item.SetStatus(surfacechat.ToolStatusAwaitingPermission)
	case core.ToolLifecycleRequested:
		item.SetStatus(surfacechat.ToolStatusRunning)
	case core.ToolLifecycleFailed:
		item.SetStatus(surfacechat.ToolStatusError)
	case core.ToolLifecycleCompleted:
		item.SetStatus(surfacechat.ToolStatusSuccess)
	}
	switch {
	case sig.note.Granted:
		item.SetStatus(surfacechat.ToolStatusRunning)
	case sig.note.Denied:
		item.SetStatus(surfacechat.ToolStatusCanceled)
	}
	return sig
}

// readGroup folds two or more consecutive successful reads starting at start
// into one row.
func (t chatSource) readGroup(sty *surfacestyles.Styles, start int) (surfacechat.MessageItem, rowSig, int, bool) {
	messages := t.messages
	if !isStandaloneReadToolCall(messages[start]) {
		return nil, rowSig{}, start, false
	}
	var calls []surfacemessage.ToolCall
	var results []surfacemessage.ToolResult
	var sig strings.Builder
	next := start
	for next < len(messages) && isStandaloneReadToolCall(messages[next]) {
		call := messages[next].ToolCalls()[0]
		result, ok := t.tools.Results[call.ID]
		if !ok || result.IsError || result.Name != "read" {
			break
		}
		calls, results = append(calls, call), append(results, result)
		fmt.Fprintf(&sig, "%s:%d:%d:%s;", call.ID, t.read.Revision(messages[next].ID), t.read.ResultRevision(call.ID), t.updates[call.ID].State)
		next++
		if next < len(messages) && isReadToolResultMessage(messages[next], call.ID) {
			next++
		}
	}
	if len(calls) < 2 {
		return nil, rowSig{}, start, false
	}
	item := surfacechat.NewReadGroupMessageItem(sty, messages[start].ID, calls, results)
	item.SetStatus(surfacechat.ToolStatusSuccess)
	for _, call := range calls {
		if state := t.updates[call.ID].State; state == core.ToolLifecycleRequested || state == core.ToolLifecycleWaitingApproval {
			item.SetStatus(surfacechat.ToolStatusRunning)
			break
		}
	}
	return item, rowSig{text: sig.String()}, next, true
}

func isStandaloneReadToolCall(message surfacemessage.Message) bool {
	if message.Role != surfacemessage.Assistant {
		return false
	}
	if strings.TrimSpace(message.Content().Text) != "" {
		return false
	}
	toolCalls := message.ToolCalls()
	return len(toolCalls) == 1 && toolCalls[0].Name == "read"
}

func isReadToolResultMessage(message surfacemessage.Message, toolCallID string) bool {
	if message.Role != surfacemessage.Tool {
		return false
	}
	toolResults := message.ToolResults()
	return len(toolResults) == 1 && toolResults[0].ToolCallID == toolCallID && toolResults[0].Name == "read"
}
