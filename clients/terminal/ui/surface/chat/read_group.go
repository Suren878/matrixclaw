package chat

import (
	"encoding/json"
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type ReadGroupMessageItem struct {
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	messageID        string
	toolCalls        []surfacemessage.ToolCall
	results          []surfacemessage.ToolResult
	sty              *surfacestyles.Styles
	expandedContents bool
	status           ToolStatus
}

func NewReadGroupMessageItem(sty *surfacestyles.Styles, messageID string, toolCalls []surfacemessage.ToolCall, results []surfacemessage.ToolResult) *ReadGroupMessageItem {
	return &ReadGroupMessageItem{
		highlightableMessageItem: defaultHighlighter(sty),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     &focusableMessageItem{},
		messageID:                messageID,
		toolCalls:                append([]surfacemessage.ToolCall(nil), toolCalls...),
		results:                  append([]surfacemessage.ToolResult(nil), results...),
		sty:                      sty,
		status:                   ToolStatusSuccess,
	}
}

func (m *ReadGroupMessageItem) ID() string {
	return m.messageID + ":read-group"
}

func (m *ReadGroupMessageItem) ToggleExpanded() bool {
	m.expandedContents = !m.expandedContents
	m.clearCache()
	return m.expandedContents
}

func (m *ReadGroupMessageItem) RawRender(width int) string {
	cappedWidth := cappedMessageWidth(width)
	content, height, ok := m.getCachedRender(cappedWidth)
	if ok {
		return m.renderHighlighted(content, cappedWidth, height)
	}
	paths := make([]string, 0, len(m.toolCalls))
	for _, toolCall := range m.toolCalls {
		var params tools.ReadParams
		_ = json.Unmarshal([]byte(toolCall.Input), &params)

		path := prettyPath(params.FilePath)
		if path != "" {
			paths = append(paths, path)
		}
	}

	headerParams := []string{}
	if root := commonReadRoot(paths); root != "" {
		headerParams = append(headerParams, root)
		paths = relativeReadPaths(root, paths)
	}
	header := readHeader(m.sty, m.status, cappedWidth, false, headerParams...)
	content = header
	if pathsBlock := renderReadPathsBlock(m.sty, cappedWidth, paths...); pathsBlock != "" {
		content = strings.Join([]string{header, pathsBlock}, "\n")
	}
	height = strings.Count(content, "\n") + 1
	m.setCachedRender(content, cappedWidth, height)
	return m.renderHighlighted(content, cappedWidth, height)
}

func (m *ReadGroupMessageItem) Render(width int) string {
	return renderUnifiedMessageLines(m.sty, m.RawRender(width), m.focused, "●", m.sty.Chat.Message.ToolMarker)
}

func (m *ReadGroupMessageItem) SetStatus(status ToolStatus) {
	m.status = status
	m.clearCache()
}
