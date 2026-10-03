package chat

import (
	"cmp"
	"encoding/json"
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

type agentParams struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Runtime     string `json:"runtime"`
}

// agentCard is what an agent call shows: the child once the daemon reports it,
// else what the call asked for.
type agentCard struct {
	name, task, goal, runtime string
	state                     surfacemessage.SubagentState
	detail                    string
}

func newAgentCard(call surfacemessage.ToolCall, sub *surfacemessage.Subagent, status ToolStatus) agentCard {
	var params agentParams
	_ = json.Unmarshal([]byte(call.Input), &params)
	card := agentCard{
		name:    cmp.Or(oneLine(params.Description), "subagent"),
		task:    oneLine(params.Description),
		goal:    oneLine(params.Prompt),
		runtime: strings.TrimSpace(params.Runtime),
		state:   subagentStateOf(status),
	}
	if sub != nil {
		card.name, card.state = sub.Name, sub.State
		card.task = cmp.Or(sub.Task, card.task)
		card.goal = cmp.Or(sub.Goal, card.goal)
		card.runtime = cmp.Or(sub.Runtime, card.runtime)
		card.detail = cmp.Or(sub.Summary, sub.Error)
	}
	return card
}

func renderAgent(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string {
	width = cappedMessageWidth(width)
	card := newAgentCard(opts.ToolCall, opts.Subagent, opts.Status)
	header := toolHeader(sty, card.label(), width, card.preview())
	lines := make([]string, 0, 3)
	if opts.ExpandedContent {
		if card.task != "" {
			lines = append(lines, "Task: "+card.task)
		}
		if card.goal != "" && !strings.EqualFold(card.goal, card.task) {
			lines = append(lines, "Goal: "+card.goal)
		}
	}
	detail := card.detail
	if detail == "" && opts.HasResult() && !card.state.Active() {
		detail = strings.TrimSpace(opts.Result.Content)
	}
	if detail != "" {
		lines = append(lines, detail)
	}
	if len(lines) == 0 {
		return header
	}
	body := sty.Tool.Body.Render(toolOutputPlainContent(sty, strings.Join(lines, "\n"), width-toolBodyLeftPaddingTotal, opts.ExpandedContent))
	return joinToolParts(header, body)
}

func (c agentCard) label() string {
	switch c.state {
	case surfacemessage.SubagentCompleted:
		return "✓ " + c.name + " completed"
	case surfacemessage.SubagentFailed:
		return "✕ " + c.name + " failed"
	case surfacemessage.SubagentCanceled:
		return "✕ " + c.name + " canceled"
	case surfacemessage.SubagentWaitingApproval:
		return "◇ " + c.name + " waiting for permission..."
	case surfacemessage.SubagentPending:
		return "◇ " + c.name + " starting..."
	default:
		return "◇ " + c.name + " working..."
	}
}

func (c agentCard) preview() string {
	switch {
	case c.task != "" && c.goal != "" && !strings.EqualFold(c.task, c.goal):
		return c.task + " - " + c.goal
	case c.task != "":
		return c.task
	default:
		return c.goal
	}
}

// subagentStateOf is the child's state as far as the tool call shows it.
func subagentStateOf(status ToolStatus) surfacemessage.SubagentState {
	switch status {
	case ToolStatusSuccess:
		return surfacemessage.SubagentCompleted
	case ToolStatusError:
		return surfacemessage.SubagentFailed
	case ToolStatusCanceled:
		return surfacemessage.SubagentCanceled
	case ToolStatusAwaitingPermission:
		return surfacemessage.SubagentWaitingApproval
	default:
		return surfacemessage.SubagentRunning
	}
}

// subagentToolStatus is the tool status an agent call shows for its child.
func subagentToolStatus(state surfacemessage.SubagentState) ToolStatus {
	switch state {
	case surfacemessage.SubagentWaitingApproval:
		return ToolStatusAwaitingPermission
	case surfacemessage.SubagentCompleted:
		return ToolStatusSuccess
	case surfacemessage.SubagentFailed:
		return ToolStatusError
	case surfacemessage.SubagentCanceled:
		return ToolStatusCanceled
	default:
		return ToolStatusRunning
	}
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
