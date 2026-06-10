package telegram

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func renderApprovalText(approval core.Approval) string {
	lines := []string{}
	if approval.ToolName != "" {
		lines = append(lines, "Tool: "+approval.ToolName)
	}
	if approval.Action != "" {
		lines = append(lines, "Action: "+approval.Action)
	}
	if approval.Path != "" {
		lines = append(lines, "Path: "+approval.Path)
	}
	if approval.Description != "" {
		lines = append(lines, "")
		lines = append(lines, approval.Description)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func renderAssistantMessage(message core.Message) string {
	if content := strings.TrimSpace(message.Content); content != "" {
		return content
	}
	parts := make([]string, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch {
		case part.Text != nil && strings.TrimSpace(part.Text.Text) != "":
			parts = append(parts, strings.TrimSpace(part.Text.Text))
		case part.ToolResult != nil && strings.TrimSpace(part.ToolResult.Content) != "":
			parts = append(parts, strings.TrimSpace(part.ToolResult.Content))
		case part.Finish != nil && strings.TrimSpace(part.Finish.Message) != "":
			parts = append(parts, strings.TrimSpace(part.Finish.Message))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func renderRunStatus(run core.Run) string {
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning:
		return "Thinking..."
	case core.RunStatusWaitingApproval:
		return "Approval required."
	case core.RunStatusCanceled:
		return "Run canceled."
	case core.RunStatusFailed:
		if strings.TrimSpace(run.Error) != "" {
			return "Run failed: " + strings.TrimSpace(run.Error)
		}
		return "Run failed."
	default:
		return "Run status: " + string(run.Status)
	}
}

func clipTelegramText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= defaultMessageLimit {
		return text
	}
	return strings.TrimSpace(string(runes[:defaultMessageLimit-1])) + "…"
}
