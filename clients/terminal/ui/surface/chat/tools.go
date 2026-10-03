package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/ui/surface/anim"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
)

const (
	responseContextHeight    = 10
	toolBodyLeftPaddingTotal = 2
)

type ToolStatus int

const (
	ToolStatusAwaitingPermission ToolStatus = iota
	ToolStatusRunning
	ToolStatusSuccess
	ToolStatusError
	ToolStatusCanceled
)

type ToolMessageItem interface {
	MessageItem
	ToolCall() surfacemessage.ToolCall
	SetStatus(status ToolStatus)
}

type ToolRenderOpts struct {
	ToolCall        surfacemessage.ToolCall
	Result          *surfacemessage.ToolResult
	Subagent        *surfacemessage.Subagent
	Anim            *anim.Anim
	ExpandedContent bool
	IsSpinning      bool
	Status          ToolStatus
}

func (o *ToolRenderOpts) IsPending() bool      { return !o.ToolCall.Finished && !o.IsCanceled() }
func (o *ToolRenderOpts) IsCanceled() bool     { return o.Status == ToolStatusCanceled }
func (o *ToolRenderOpts) HasResult() bool      { return o.Result != nil }
func (o *ToolRenderOpts) HasEmptyResult() bool { return o.Result == nil || o.Result.Content == "" }

// toolRenderer draws one tool call at width.
type toolRenderer func(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string

// toolRenderers draws the tools that need more than renderGeneric.
var toolRenderers = map[string]toolRenderer{
	"bash":        renderBash,
	"task_output": renderTaskOutput,
	"task_kill":   renderTaskKill,
	"read":        renderRead,
	"write":       renderFileChange,
	"edit":        renderFileChange,
	"multiedit":   renderFileChange,
	"glob":        renderListing,
	"grep":        renderListing,
	"ls":          renderListing,
	"agent":       renderAgent,
}

type baseToolMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	render          toolRenderer
	toolCall        surfacemessage.ToolCall
	result          *surfacemessage.ToolResult
	subagent        *surfacemessage.Subagent
	status          ToolStatus
	hasCappedWidth  bool
	sty             *surfacestyles.Styles
	anim            *anim.Anim
	expandedContent bool
}

func newBaseToolMessageItem(
	sty *surfacestyles.Styles,
	toolCall surfacemessage.ToolCall,
	result *surfacemessage.ToolResult,
	render toolRenderer,
	canceled bool,
) *baseToolMessageItem {
	hasCappedWidth := normalizedToolName(toolCall.Name) != "edit" && normalizedToolName(toolCall.Name) != "multiedit"
	status := ToolStatusRunning
	if canceled {
		status = ToolStatusCanceled
	}

	t := &baseToolMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		sty:                      sty,
		render:                   render,
		toolCall:                 toolCall,
		result:                   result,
		status:                   status,
		hasCappedWidth:           hasCappedWidth,
	}
	t.anim = anim.New(anim.Settings{
		ID:          toolCall.ID,
		Size:        15,
		GradColorA:  sty.Primary,
		GradColorB:  sty.Secondary,
		LabelColor:  sty.FgBase,
		CycleColors: true,
	})
	return t
}

// NewToolMessageItem is the row of one tool call; sub is the child an agent
// call started, nil for other tools.
func NewToolMessageItem(
	sty *surfacestyles.Styles,
	toolCall surfacemessage.ToolCall,
	result *surfacemessage.ToolResult,
	sub *surfacemessage.Subagent,
	canceled bool,
) ToolMessageItem {
	render, ok := toolRenderers[normalizedToolName(toolCall.Name)]
	if !ok {
		render = renderGeneric
	}
	item := newBaseToolMessageItem(sty, toolCall, result, render, canceled)
	item.subagent = sub
	return item
}

func (t *baseToolMessageItem) ID() string { return t.toolCall.ID }

func (t *baseToolMessageItem) StartAnimation() tea.Cmd {
	if !t.isSpinning() {
		return nil
	}
	return t.anim.Start()
}

func (t *baseToolMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if !t.isSpinning() {
		return nil
	}
	return t.anim.Animate(msg)
}

func (t *baseToolMessageItem) RawRender(width int) string {
	toolItemWidth := width - MessageLeftPaddingTotal
	if t.hasCappedWidth {
		toolItemWidth = cappedMessageWidth(width)
	}

	content, height, ok := t.getCachedRender(toolItemWidth)
	if !ok || t.isSpinning() {
		content = t.render(t.sty, toolItemWidth, &ToolRenderOpts{
			ToolCall:        t.toolCall,
			Result:          t.result,
			Subagent:        t.subagent,
			Anim:            t.anim,
			ExpandedContent: t.expandedContent,
			IsSpinning:      t.isSpinning(),
			Status:          t.computeStatus(),
		})
		height = lipgloss.Height(content)
		t.setCachedRender(content, toolItemWidth, height)
	}

	return t.renderHighlighted(content, toolItemWidth, height)
}

func (t *baseToolMessageItem) Render(width int) string {
	return renderUnifiedMessageLines(t.sty, t.RawRender(width), t.focused, t.markerStyle())
}

func (t *baseToolMessageItem) ToolCall() surfacemessage.ToolCall { return t.toolCall }
func (t *baseToolMessageItem) SetStatus(status ToolStatus)       { t.status = status; t.clearCache() }

func (t *baseToolMessageItem) markerStyle() lipgloss.Style {
	if normalizedToolName(t.toolCall.Name) == "bash" {
		return toolStatusMarkerStyle(t.sty, t.computeStatus())
	}
	return t.sty.Chat.Message.ToolMarker
}

func (t *baseToolMessageItem) computeStatus() ToolStatus {
	if t.subagent != nil {
		return subagentToolStatus(t.subagent.State)
	}
	if t.result != nil {
		switch normalizedToolName(t.result.Status) {
		case "error":
			return ToolStatusError
		case "success", "neutral":
			return ToolStatusSuccess
		default:
			if t.result.IsError && !isExpectedNeutralBashResult(t.toolCall, t.result) {
				return ToolStatusError
			}
		}
		return ToolStatusSuccess
	}
	return t.status
}

func normalizedToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (t *baseToolMessageItem) isSpinning() bool {
	if normalizedToolName(t.toolCall.Name) == "agent" {
		return t.computeStatus() == ToolStatusRunning
	}
	return !t.toolCall.Finished && t.status != ToolStatusCanceled
}

func (t *baseToolMessageItem) ToggleExpanded() bool {
	t.expandedContent = !t.expandedContent
	t.clearCache()
	return t.expandedContent
}
