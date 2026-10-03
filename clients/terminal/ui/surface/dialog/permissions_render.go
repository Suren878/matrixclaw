package dialog

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	components "github.com/Suren878/matrixclaw/clients/terminal/ui/components"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/toolview"
)

func (p *Permissions) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	t := p.com.Styles

	forceFullscreen := area.Dx() <= minWindowWidth || area.Dy() <= minWindowHeight

	var width, maxHeight int
	if forceFullscreen || (p.fullscreen && p.hasDiffView()) {
		width = area.Dx()
		maxHeight = area.Dy()
	} else if p.hasDiffView() {
		width = min(int(float64(area.Dx())*diffSizeRatio), diffMaxWidth)
		maxHeight = int(float64(area.Dy()) * diffSizeRatio)
	} else {
		width = min(int(float64(area.Dx())*simpleSizeRatio), simpleMaxWidth)
		maxHeight = int(float64(area.Dy()) * simpleHeightRatio)
	}

	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)

	contentWidth := p.calculateContentWidth(width)
	header := p.renderHeader(contentWidth)
	buttons := p.renderButtons(contentWidth)
	helpView := p.help.View(p)

	headerHeight := lipgloss.Height(header)
	buttonsHeight := lipgloss.Height(buttons)
	helpHeight := lipgloss.Height(helpView)
	frameHeight := dialogStyle.GetVerticalFrameSize() + layoutSpacingLines

	renderedContent := p.renderContent(contentWidth)
	contentHeight := lipgloss.Height(renderedContent)

	var availableHeight int
	if !p.hasDiffView() && !forceFullscreen {
		fixedHeight := headerHeight + buttonsHeight + helpHeight + frameHeight
		neededHeight := fixedHeight + contentHeight
		if neededHeight < maxHeight {
			availableHeight = contentHeight
		} else {
			availableHeight = maxHeight - fixedHeight
		}
		availableHeight = max(availableHeight, 3)
	} else {
		availableHeight = maxHeight - headerHeight - buttonsHeight - helpHeight - frameHeight
	}

	needsScrollbar := p.hasDiffView() || contentHeight > availableHeight
	viewportWidth := contentWidth
	if needsScrollbar {
		viewportWidth = contentWidth - 1
	}

	if p.viewport.Width() != viewportWidth {
		p.viewportDirty = true
		renderedContent = p.renderContent(viewportWidth)
	}

	p.viewport.SetWidth(viewportWidth)
	p.viewport.SetHeight(availableHeight)
	if p.viewportDirty {
		p.viewport.SetContent(renderedContent)
		p.viewportDirty = false
	}

	content := p.viewport.View()
	scrollbar := ""
	if needsScrollbar {
		scrollbar = surfacecommon.Scrollbar(t, availableHeight, p.viewport.TotalLineCount(), availableHeight, p.viewport.YOffset())
	}
	if scrollbar != "" {
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
	}

	parts := []string{header}
	if content != "" {
		parts = append(parts, "", content)
	}
	parts = append(parts, "", buttons, "", helpView)

	innerContent := lipgloss.JoinVertical(lipgloss.Left, parts...)
	view := dialogStyle.Render(innerContent)
	p.lastView = view
	p.lastViewRect = surfacecommon.CenterRect(area, lipgloss.Width(view), lipgloss.Height(view))
	DrawCenterCursor(scr, area, view, nil)
	return nil
}

func (p *Permissions) renderHeader(contentWidth int) string {
	t := p.com.Styles

	title := surfacecommon.DialogTitle(t, "Permission Required", contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)

	toolLine := p.renderToolName(contentWidth)
	pathLine := p.renderKeyValue("Path", prettyPath(p.permission.Path), contentWidth)
	sourceLine := p.renderKeyValue("Source", permissionSourceLabel(p.permission), contentWidth)

	lines := []string{title, "", sourceLine, toolLine, pathLine}
	if suggestion := p.permission.Suggestion; suggestion != nil {
		lines = append(lines, p.renderKeyValue("Always", suggestion.String(), contentWidth))
	}

	switch p.permission.ToolName {
	case toolNameBash:
		if params, ok := surfacepermission.DecodeParams[tools.BashPermissionsParams](p.permission.Params); ok {
			lines = append(lines, p.renderKeyValue("Desc", params.Description, contentWidth))
		}
	case toolNameEdit, toolNameWrite, toolNameMultiEdit, toolNameRead:
		var filePath string
		switch p.permission.ToolName {
		case toolNameEdit:
			if params, ok := surfacepermission.DecodeParams[tools.EditPermissionsParams](p.permission.Params); ok {
				filePath = params.FilePath
			}
		case toolNameWrite:
			if params, ok := surfacepermission.DecodeParams[tools.WritePermissionsParams](p.permission.Params); ok {
				filePath = params.FilePath
			}
		case toolNameMultiEdit:
			if params, ok := surfacepermission.DecodeParams[tools.MultiEditPermissionsParams](p.permission.Params); ok {
				filePath = params.FilePath
			}
		case toolNameRead:
			if params, ok := surfacepermission.DecodeParams[surfacepermission.ReadPermissionsParams](p.permission.Params); ok {
				filePath = params.FilePath
			}
		}
		if filePath != "" {
			lines = append(lines, p.renderKeyValue("File", prettyPath(filePath), contentWidth))
		}
	case toolNameLS:
		if params, ok := surfacepermission.DecodeParams[surfacepermission.LSPermissionsParams](p.permission.Params); ok {
			lines = append(lines, p.renderKeyValue("Directory", prettyPath(params.Path), contentWidth))
		}
	case toolNameSkillManage:
		if params, ok := surfacepermission.DecodeParams[tools.SkillManagePermissionsParams](p.permission.Params); ok {
			if params.Action != "" {
				lines = append(lines, p.renderKeyValue("Action", strings.TrimSpace(params.Action), contentWidth))
			}
			if params.Name != "" {
				lines = append(lines, p.renderKeyValue("Skill", strings.TrimSpace(params.Name), contentWidth))
			}
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func permissionSourceLabel(permission surfacepermission.PermissionRequest) string {
	params, ok := permission.Params.(map[string]any)
	if !ok || permissionParamString(params, "source") != "subagent_approval_bridge" {
		return "Agent"
	}
	label := permissionParamString(params, "subagent_title")
	if label == "" {
		label = permissionParamString(params, "runtime")
	}
	label = strings.TrimSpace(strings.TrimPrefix(label, "Subagent:"))
	if label == "" {
		return "Subagent"
	}
	return "Subagent: " + label
}

func permissionParamString(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (p *Permissions) renderKeyValue(keyText, value string, width int) string {
	t := p.com.Styles
	keyStyle := t.Muted
	valueStyle := t.Base

	keyStr := keyStyle.Render(keyText)
	valueStr := valueStyle.Width(width - lipgloss.Width(keyStr) - 1).Render(" " + value)

	return lipgloss.JoinHorizontal(lipgloss.Left, keyStr, valueStr)
}

func (p *Permissions) renderToolName(width int) string {
	toolName := toolview.Describe(p.permission.ToolName, "").Title
	if p.permission.ToolName == toolNameSkillManage {
		toolName = "Skill Approval"
	} else if parts := strings.SplitN(p.permission.ToolName, "_", 3); len(parts) == 3 && parts[0] == "mcp" {
		toolName = fmt.Sprintf("%s %s %s", toolview.PrettyName(parts[1]), surfacestyles.ArrowRightIcon, toolview.PrettyName(parts[2]))
	}

	return p.renderKeyValue("Tool", toolName, width)
}

func (p *Permissions) renderButtons(contentWidth int) string {
	options := p.permissionOptions()
	buttons := make([]components.Button, 0, len(options))
	for i, option := range options {
		buttons = append(buttons, components.Button{
			Label:   option.label,
			Danger:  option.action == PermissionDeny || option.action == PermissionDenyWithReason,
			Focused: p.selectedOption == i,
		})
	}

	return components.RenderButtons(components.DefaultStyles(), contentWidth, buttons...)
}
