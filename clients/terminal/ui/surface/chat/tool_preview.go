package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/toolview"
)

func (t *baseToolMessageItem) HandleKeyEvent(key tea.KeyPressMsg) (bool, tea.Cmd) {
	switch key.String() {
	case "enter", "v":
	default:
		return false, nil
	}

	subagentData, ok := t.subagentPreviewData()
	if ok {
		return true, func() tea.Msg {
			return surfacedialog.ActionOpenFilePreview{Data: subagentData}
		}
	}

	errorData, ok := t.errorPreviewData()
	if ok {
		return true, func() tea.Msg {
			return surfacedialog.ActionOpenFilePreview{Data: errorData}
		}
	}

	data, ok := t.diffPreviewData()
	if ok {
		return true, func() tea.Msg {
			return surfacedialog.ActionOpenDiffPreview{Data: data}
		}
	}

	fileData, ok := t.filePreviewData()
	if ok {
		return true, func() tea.Msg {
			return surfacedialog.ActionOpenFilePreview{Data: fileData}
		}
	}

	return false, nil
}

func (t *baseToolMessageItem) errorPreviewData() (surfacedialog.FilePreviewData, bool) {
	if t.result == nil || t.computeStatus() != ToolStatusError {
		return surfacedialog.FilePreviewData{}, false
	}

	var out strings.Builder
	_, _ = fmt.Fprintf(&out, "Tool: %s\n", strings.TrimSpace(t.toolCall.Name))
	if status := strings.TrimSpace(t.result.Status); status != "" {
		_, _ = fmt.Fprintf(&out, "Status: %s\n", status)
	}

	if content := strings.TrimSpace(t.result.Content); content != "" {
		out.WriteString("\n")
		out.WriteString(content)
		out.WriteString("\n")
	}

	if metadata := strings.TrimSpace(t.result.Metadata); metadata != "" {
		out.WriteString("\nMetadata:\n")
		out.WriteString(metadata)
		out.WriteString("\n")
	}

	content := strings.TrimSpace(out.String())
	if content == "" {
		content = "Tool failed without error details."
	}
	return surfacedialog.FilePreviewData{
		Title:   "Tool Error",
		Content: content,
	}, true
}

func (t *baseToolMessageItem) subagentPreviewData() (surfacedialog.FilePreviewData, bool) {
	if normalizedToolName(t.toolCall.Name) != "agent" {
		return surfacedialog.FilePreviewData{}, false
	}
	card := newAgentCard(t.toolCall, t.subagent, t.computeStatus())
	var out strings.Builder
	for _, field := range [][2]string{{"Name", card.name}, {"Task", card.task}, {"Goal", card.goal}, {"Runtime", card.runtime}, {"Status", string(card.state)}} {
		if field[1] != "" {
			_, _ = fmt.Fprintf(&out, "%s: %s\n", field[0], field[1])
		}
	}
	if t.subagent != nil {
		for _, section := range [][2]string{{"Summary", t.subagent.Summary}, {"Error", t.subagent.Error}} {
			if section[1] != "" {
				_, _ = fmt.Fprintf(&out, "\n%s:\n%s\n", section[0], section[1])
			}
		}
	}
	if t.result != nil {
		if content := strings.TrimSpace(t.result.Content); content != "" && !strings.Contains(out.String(), content) {
			_, _ = fmt.Fprintf(&out, "\nResult:\n%s\n", content)
		}
	}
	return surfacedialog.FilePreviewData{Title: "Subagent Details", Content: strings.TrimSpace(out.String())}, true
}

func (t *baseToolMessageItem) diffPreviewData() (surfacedialog.DiffPreviewData, bool) {
	if t.result == nil || t.result.IsError() {
		return surfacedialog.DiffPreviewData{}, false
	}
	change, ok := toolview.FileChangeOf(t.toolCall.Name, t.toolCall.Input, t.result.Metadata)
	if !ok || !change.Done {
		return surfacedialog.DiffPreviewData{}, false
	}
	return surfacedialog.NewDiffPreviewData(toolview.Describe(t.toolCall.Name, "").Title+" Changes", change.Files...), true
}

func (t *baseToolMessageItem) filePreviewData() (surfacedialog.FilePreviewData, bool) {
	if t.result == nil || t.result.IsError() || t.toolCall.Name != "read" {
		return surfacedialog.FilePreviewData{}, false
	}

	var params tools.ReadParams
	if err := json.Unmarshal([]byte(t.toolCall.Input), &params); err != nil {
		return surfacedialog.FilePreviewData{}, false
	}

	return surfacedialog.FilePreviewData{
		Title:    "Read File",
		FilePath: metadataDisplayPath(t.result.Metadata, params.FilePath),
		Content:  readFileBody(t.result.Content),
	}, true
}
