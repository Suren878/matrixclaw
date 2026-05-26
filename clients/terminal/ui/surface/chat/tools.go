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
	SetToolCall(tc surfacemessage.ToolCall)
	SetResult(res *surfacemessage.ToolResult)
	MessageID() string
	SetMessageID(id string)
	SetStatus(status ToolStatus)
	Status() ToolStatus
}

type ToolRenderOpts struct {
	ToolCall        surfacemessage.ToolCall
	Result          *surfacemessage.ToolResult
	Anim            *anim.Anim
	ExpandedContent bool
	Compact         bool
	IsSpinning      bool
	Status          ToolStatus
}

func (o *ToolRenderOpts) IsPending() bool      { return !o.ToolCall.Finished && !o.IsCanceled() }
func (o *ToolRenderOpts) IsCanceled() bool     { return o.Status == ToolStatusCanceled }
func (o *ToolRenderOpts) HasResult() bool      { return o.Result != nil }
func (o *ToolRenderOpts) HasEmptyResult() bool { return o.Result == nil || o.Result.Content == "" }

type ToolRenderer interface {
	RenderTool(sty *surfacestyles.Styles, width int, opts *ToolRenderOpts) string
}

type baseToolMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	toolRenderer    ToolRenderer
	toolCall        surfacemessage.ToolCall
	result          *surfacemessage.ToolResult
	messageID       string
	status          ToolStatus
	hasCappedWidth  bool
	isCompact       bool
	sty             *surfacestyles.Styles
	anim            *anim.Anim
	expandedContent bool
}

func newBaseToolMessageItem(
	sty *surfacestyles.Styles,
	toolCall surfacemessage.ToolCall,
	result *surfacemessage.ToolResult,
	toolRenderer ToolRenderer,
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
		toolRenderer:             toolRenderer,
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

func NewToolMessageItem(
	sty *surfacestyles.Styles,
	messageID string,
	toolCall surfacemessage.ToolCall,
	result *surfacemessage.ToolResult,
	canceled bool,
) ToolMessageItem {
	var item ToolMessageItem
	switch normalizedToolName(toolCall.Name) {
	case "bash":
		item = NewBashToolMessageItem(sty, toolCall, result, canceled)
	case "job_output":
		item = NewJobOutputToolMessageItem(sty, toolCall, result, canceled)
	case "job_kill":
		item = NewJobKillToolMessageItem(sty, toolCall, result, canceled)
	case "read":
		item = NewReadToolMessageItem(sty, toolCall, result, canceled)
	case "write":
		item = NewWriteToolMessageItem(sty, toolCall, result, canceled)
	case "edit":
		item = NewEditToolMessageItem(sty, toolCall, result, canceled)
	case "multiedit":
		item = NewMultiEditToolMessageItem(sty, toolCall, result, canceled)
	case "glob":
		item = NewGlobToolMessageItem(sty, toolCall, result, canceled)
	case "grep":
		item = NewGrepToolMessageItem(sty, toolCall, result, canceled)
	case "ls":
		item = NewLSToolMessageItem(sty, toolCall, result, canceled)
	case "delegate_task":
		item = NewDelegateTaskToolMessageItem(sty, toolCall, result, canceled)
	default:
		item = NewGenericToolMessageItem(sty, toolCall, result, canceled)
	}
	item.SetMessageID(messageID)
	return item
}

func (t *baseToolMessageItem) SetCompact(compact bool) {
	t.isCompact = compact
	t.clearCache()
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
		content = t.toolRenderer.RenderTool(t.sty, toolItemWidth, &ToolRenderOpts{
			ToolCall:        t.toolCall,
			Result:          t.result,
			Anim:            t.anim,
			ExpandedContent: t.expandedContent,
			Compact:         t.isCompact,
			IsSpinning:      t.isSpinning(),
			Status:          t.computeStatus(),
		})
		height = lipgloss.Height(content)
		t.setCachedRender(content, toolItemWidth, height)
	}

	return t.renderHighlighted(content, toolItemWidth, height)
}

func (t *baseToolMessageItem) Render(width int) string {
	return renderUnifiedMessageLines(t.sty, t.RawRender(width), t.focused && !t.isCompact, "●", t.markerStyle())
}

func (t *baseToolMessageItem) ToolCall() surfacemessage.ToolCall { return t.toolCall }
func (t *baseToolMessageItem) SetToolCall(tc surfacemessage.ToolCall) {
	t.toolCall = tc
	t.clearCache()
}
func (t *baseToolMessageItem) SetResult(res *surfacemessage.ToolResult) {
	t.result = res
	t.clearCache()
}
func (t *baseToolMessageItem) MessageID() string           { return t.messageID }
func (t *baseToolMessageItem) SetMessageID(id string)      { t.messageID = id }
func (t *baseToolMessageItem) SetStatus(status ToolStatus) { t.status = status; t.clearCache() }
func (t *baseToolMessageItem) Status() ToolStatus          { return t.status }

func (t *baseToolMessageItem) markerStyle() lipgloss.Style {
	if normalizedToolName(t.toolCall.Name) == "bash" {
		return toolStatusMarkerStyle(t.sty, t.computeStatus())
	}
	return t.sty.Chat.Message.ToolMarker
}

func (t *baseToolMessageItem) computeStatus() ToolStatus {
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
	return !t.toolCall.Finished && t.status != ToolStatusCanceled
}

func (t *baseToolMessageItem) ToggleExpanded() bool {
	t.expandedContent = !t.expandedContent
	t.clearCache()
	return t.expandedContent
}
