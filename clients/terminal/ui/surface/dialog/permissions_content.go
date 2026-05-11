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
	case toolNameEdit:
		return p.renderEditContent(width)
	case toolNameWrite:
		return p.renderWriteContent(width)
	case toolNameMultiEdit:
		return p.renderMultiEditContent(width)
	case toolNameDownload:
		return p.renderDownloadContent(width)
	case toolNameFetch:
		return p.renderFetchContent(width)
	case toolNameAgenticFetch:
		return p.renderAgenticFetchContent(width)
	case toolNameRead:
		return p.renderReadContent(width)
	case toolNameLS:
		return p.renderLSContent(width)
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

func (p *Permissions) renderEditContent(contentWidth int) string {
	params, ok := surfacepermission.DecodeParams[tools.EditPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}
	return p.renderDiff(params.FilePath, params.OldContent, params.NewContent, contentWidth)
}

func (p *Permissions) renderWriteContent(contentWidth int) string {
	params, ok := surfacepermission.DecodeParams[tools.WritePermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}
	return p.renderDiff(params.FilePath, params.OldContent, params.NewContent, contentWidth)
}

func (p *Permissions) renderMultiEditContent(contentWidth int) string {
	params, ok := surfacepermission.DecodeParams[tools.MultiEditPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}
	return p.renderDiff(params.FilePath, params.OldContent, params.NewContent, contentWidth)
}

func (p *Permissions) renderDiff(filePath, oldContent, newContent string, contentWidth int) string {
	if !p.viewportDirty {
		if p.isSplitMode() {
			return p.splitDiffContent
		}
		return p.unifiedDiffContent
	}

	formatter := surfacecommon.DiffFormatter(p.com.Styles).
		Before(prettyPath(filePath), oldContent).
		After(prettyPath(filePath), newContent).
		XOffset(p.diffXOffset).
		Width(contentWidth)

	var result string
	if p.isSplitMode() {
		formatter = formatter.Split()
		p.splitDiffContent = formatter.String()
		result = p.splitDiffContent
	} else {
		formatter = formatter.Unified()
		p.unifiedDiffContent = formatter.String()
		result = p.unifiedDiffContent
	}

	return result
}

func (p *Permissions) renderDownloadContent(width int) string {
	params, ok := surfacepermission.DecodeParams[surfacepermission.DownloadPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}

	content := fmt.Sprintf("URL: %s\nFile: %s", params.URL, prettyPath(params.FilePath))
	if params.Timeout > 0 {
		content += fmt.Sprintf("\nTimeout: %ds", params.Timeout)
	}

	return p.renderContentPanel(content, width)
}

func (p *Permissions) renderFetchContent(width int) string {
	params, ok := surfacepermission.DecodeParams[surfacepermission.FetchPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}

	return p.renderContentPanel(params.URL, width)
}

func (p *Permissions) renderAgenticFetchContent(width int) string {
	params, ok := surfacepermission.DecodeParams[surfacepermission.AgenticFetchPermissionsParams](p.permission.Params)
	if !ok {
		return ""
	}

	var content string
	if params.URL != "" {
		content = fmt.Sprintf("URL: %s\n\nPrompt: %s", params.URL, params.Prompt)
	} else {
		content = fmt.Sprintf("Prompt: %s", params.Prompt)
	}

	return p.renderContentPanel(content, width)
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
