package dialog

import (
	"encoding/json"
	"fmt"
	"strings"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func (p *Permissions) renderContent(width int) string {
	switch p.permission.ToolName {
	case toolNameBash:
		return p.renderBashContent(width)
	case toolNameEdit, toolNameWrite, toolNameMultiEdit:
		return p.renderFileChangeContent(width)
	case toolNameRead:
		return p.renderReadContent(width)
	case toolNameLS:
		return p.renderLSContent(width)
	case toolNameSkillManage:
		return p.renderSkillManageContent(width)
	default:
		return p.renderDefaultContent(width)
	}
}

func (p *Permissions) renderBashContent(width int) string {
	params, ok := surfacepermission.DecodeParams[tools.BashPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}

	return p.renderContentPanel(params.Command, width)
}

func (p *Permissions) renderFileChangeContent(contentWidth int) string {
	change, ok := surfacepermission.DecodeParams[tools.FileChange](p.permission.Params)
	if !ok {
		return ""
	}
	return p.renderDiff(change.Path, change.OldContent, change.NewContent, contentWidth)
}

func (p *Permissions) renderDiff(filePath, oldContent, newContent string, contentWidth int) string {
	return p.diff.render(p.com.Styles, filePath, oldContent, newContent, contentWidth)
}

func (p *Permissions) renderReadContent(width int) string {
	params, ok := surfacepermission.DecodeParams[surfacepermission.ReadPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}

	content := surfacecommon.RenderPathTree(prettyPath(params.FilePath))
	if params.Offset > 0 {
		content += fmt.Sprintf("\n\nStart line: %d", params.Offset+1)
	}
	if params.Limit > 0 && params.Limit != 2000 {
		content += fmt.Sprintf("\nRead lines: %d", params.Limit)
	}

	return p.renderContentPanel(content, width)
}

func (p *Permissions) renderLSContent(width int) string {
	params, ok := surfacepermission.DecodeParams[surfacepermission.LSPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}

	content := fmt.Sprintf("Directory: %s", prettyPath(params.Path))
	if len(params.Ignore) > 0 {
		content += fmt.Sprintf("\nIgnore patterns: %s", strings.Join(params.Ignore, ", "))
	}

	return p.renderContentPanel(content, width)
}

func (p *Permissions) renderSkillManageContent(width int) string {
	params, ok := surfacepermission.DecodeParams[tools.SkillManagePermissionsParams](p.permission.Params)
	if !ok {
		return p.renderDefaultContent(width)
	}
	switch strings.ToLower(strings.TrimSpace(params.Action)) {
	case "create":
		lines := []string{}
		if name := strings.TrimSpace(params.Name); name != "" {
			lines = append(lines, "Name: "+name)
		}
		if description := strings.TrimSpace(params.Description); description != "" {
			lines = append(lines, "Description: "+description)
		}
		if body := strings.TrimSpace(params.Content); body != "" {
			lines = append(lines, "SKILL.md:", "", body)
		}
		return p.renderContentPanel(strings.TrimSpace(strings.Join(lines, "\n")), width)
	default:
		return p.renderDefaultContent(width)
	}
}

func (p *Permissions) renderDefaultContent(width int) string {
	t := p.com.Styles
	content := ""
	if !strings.HasPrefix(p.permission.ToolName, "mcp_") {
		content = p.permission.Description
	}

	if paramStr := surfacepermission.FormatParams(p.permission.Params); paramStr != "" {
		var parsed any
		if err := json.Unmarshal([]byte(paramStr), &parsed); err == nil {
			if b, err := json.MarshalIndent(parsed, "", "  "); err == nil {
				jsonContent := string(b)
				highlighted, err := surfacecommon.SyntaxHighlight(t, jsonContent, "params.json", t.BgSubtle)
				if err == nil {
					jsonContent = highlighted
				}
				if content != "" {
					content += "\n\n"
				}
				content += jsonContent
			}
		} else {
			if content != "" {
				content += "\n\n"
			}
			content += paramStr
		}
	}

	if content == "" {
		return ""
	}

	return p.renderContentPanel(strings.TrimSpace(content), width)
}

func (p *Permissions) renderContentPanel(content string, width int) string {
	panelStyle := p.com.Styles.Dialog.ContentPanel
	return panelStyle.Width(width).Render(content)
}
