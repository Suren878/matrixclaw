package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/tools"
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
	if !isSubagentToolNameLocal(t.toolCall.Name) {
		return surfacedialog.FilePreviewData{}, false
	}
	params := parseDelegateTaskParams(t.toolCall.Input)
	metadata := parseSubagentTaskMetadata(t.result)
	var out strings.Builder
	if name := firstNonEmptyLocal(metadata.AgentName, metadata.DisplayName, params.Name); name != "" {
		_, _ = fmt.Fprintf(&out, "Name: %s\n", name)
	}
	if task := firstNonEmptyLocal(metadata.DisplayName, params.Name); task != "" {
		_, _ = fmt.Fprintf(&out, "Task: %s\n", task)
	}
	if goal := firstNonEmptyLocal(metadata.Goal, params.Goal); goal != "" {
		_, _ = fmt.Fprintf(&out, "Goal: %s\n", goal)
	}
	if runtime := firstNonEmptyLocal(metadata.Runtime, params.Runtime); runtime != "" {
		_, _ = fmt.Fprintf(&out, "Runtime: %s\n", runtime)
	}
	if status := firstNonEmptyLocal(metadata.Status, subagentPreviewResultStatus(t.result)); status != "" {
		_, _ = fmt.Fprintf(&out, "Status: %s\n", status)
	}
	if summary := strings.TrimSpace(metadata.Summary); summary != "" {
		out.WriteString("\nSummary:\n")
		out.WriteString(summary)
		out.WriteString("\n")
	}
	if errText := strings.TrimSpace(metadata.Error); errText != "" {
		out.WriteString("\nError:\n")
		out.WriteString(errText)
		out.WriteString("\n")
	}
	if t.result != nil {
		if content := strings.TrimSpace(t.result.Content); content != "" && !strings.Contains(out.String(), content) {
			out.WriteString("\nResult:\n")
			out.WriteString(content)
			out.WriteString("\n")
		}
	}
	content := strings.TrimSpace(out.String())
	if content == "" {
		content = "Subagent details are not available yet."
	}
	return surfacedialog.FilePreviewData{
		Title:   "Subagent Details",
		Content: content,
	}, true
}

func subagentPreviewResultStatus(result *surfacemessage.ToolResult) string {
	if result == nil {
		return ""
	}
	switch normalizedToolName(result.Status) {
	case "success":
		return "completed"
	case "error":
		return "failed"
	default:
		return strings.TrimSpace(result.Status)
	}
}

func (t *baseToolMessageItem) diffPreviewData() (surfacedialog.DiffPreviewData, bool) {
	if t.result == nil || t.result.IsError {
		return surfacedialog.DiffPreviewData{}, false
	}

	switch t.toolCall.Name {
	case "write":
		var params tools.WriteParams
		var meta tools.WriteResponseMetadata
		if err := json.Unmarshal([]byte(t.toolCall.Input), &params); err != nil {
			return surfacedialog.DiffPreviewData{}, false
		}
		if err := json.Unmarshal([]byte(t.result.Metadata), &meta); err != nil {
			return surfacedialog.DiffPreviewData{}, false
		}
		if meta.NewContent == "" {
			meta.NewContent = params.Content
		}
		return surfacedialog.DiffPreviewData{
			Title:      "Write Changes",
			FilePath:   params.FilePath,
			OldContent: meta.OldContent,
			NewContent: meta.NewContent,
			Additions:  meta.Additions,
			Removals:   meta.Removals,
		}, true
	case "edit":
		var params tools.EditParams
		var meta tools.EditResponseMetadata
		if err := json.Unmarshal([]byte(t.toolCall.Input), &params); err != nil {
			return surfacedialog.DiffPreviewData{}, false
		}
		if err := json.Unmarshal([]byte(t.result.Metadata), &meta); err != nil {
			return surfacedialog.DiffPreviewData{}, false
		}
		return surfacedialog.DiffPreviewData{
			Title:      "Edit Changes",
			FilePath:   params.FilePath,
			OldContent: meta.OldContent,
			NewContent: meta.NewContent,
			Additions:  meta.Additions,
			Removals:   meta.Removals,
		}, true
	case "multiedit":
		var params tools.MultiEditParams
		var meta tools.MultiEditResponseMetadata
		if err := json.Unmarshal([]byte(t.toolCall.Input), &params); err != nil {
			return surfacedialog.DiffPreviewData{}, false
		}
		if err := json.Unmarshal([]byte(t.result.Metadata), &meta); err != nil {
			return surfacedialog.DiffPreviewData{}, false
		}
		return surfacedialog.DiffPreviewData{
			Title:      "Multi-Edit Changes",
			FilePath:   params.FilePath,
			OldContent: meta.OldContent,
			NewContent: meta.NewContent,
			Additions:  meta.Additions,
			Removals:   meta.Removals,
		}, true
	default:
		return surfacedialog.DiffPreviewData{}, false
	}
}

func (t *baseToolMessageItem) filePreviewData() (surfacedialog.FilePreviewData, bool) {
	if t.result == nil || t.result.IsError || t.toolCall.Name != "read" {
		return surfacedialog.FilePreviewData{}, false
	}

	var params tools.ReadParams
	if err := json.Unmarshal([]byte(t.toolCall.Input), &params); err != nil {
		return surfacedialog.FilePreviewData{}, false
	}

	path, content := resolveReadResult(params.FilePath, *t.result)
	return surfacedialog.FilePreviewData{
		Title:    "Read File",
		FilePath: path,
		Content:  readPreviewContent(*t.result, content, params.Offset+1),
	}, true
}

func readPreviewContent(result surfacemessage.ToolResult, fallbackContent string, startLine int) string {
	raw := strings.TrimSpace(result.Content)
	if raw != "" {
		return unwrapTaggedFileContentPreserveLineNumbers(raw)
	}
	return addReadPreviewLineNumbers(fallbackContent, startLine)
}

func unwrapTaggedFileContentPreserveLineNumbers(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "<file>") || !strings.HasSuffix(content, "</file>") {
		return content
	}
	content = strings.TrimPrefix(content, "<file>")
	content = strings.TrimSuffix(content, "</file>")
	content = strings.TrimSpace(content)
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "(File has more lines.") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func addReadPreviewLineNumbers(content string, startLine int) string {
	content = strings.TrimRight(content, "\n")
	if strings.TrimSpace(content) == "" {
		return ""
	}
	if startLine < 1 {
		startLine = 1
	}
	lines := strings.Split(content, "\n")
	var out strings.Builder
	for i, line := range lines {
		_, _ = fmt.Fprintf(&out, "%6d\t%s", startLine+i, line)
		if i != len(lines)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
