package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

const DiffPreviewID = "diff_preview"

type DiffPreviewData struct {
	Title      string
	FilePath   string
	OldContent string
	NewContent string
	Additions  int
	Removals   int
}

type ActionOpenDiffPreview struct {
	Data DiffPreviewData
}

type DiffPreview struct {
	com          *surfacecommon.Common
	windowWidth  int
	windowHeight int
	fullscreen   bool

	data DiffPreviewData

	viewport      viewport.Model
	viewportDirty bool
	viewportWidth int

	diffSplitMode        *bool
	defaultDiffSplitMode bool
	diffXOffset          int
	unifiedDiffContent   string
	splitDiffContent     string

	help   help.Model
	keyMap diffPreviewKeyMap
}

type diffPreviewKeyMap struct {
	Close            key.Binding
	ToggleDiffMode   key.Binding
	ToggleFullscreen key.Binding
	ScrollUp         key.Binding
	ScrollDown       key.Binding
	ScrollLeft       key.Binding
	ScrollRight      key.Binding
	Scroll           key.Binding
}

func defaultDiffPreviewKeyMap() diffPreviewKeyMap {
	return diffPreviewKeyMap{
		Close: CloseKey,
		ToggleDiffMode: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "toggle diff view"),
		),
		ToggleFullscreen: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "toggle fullscreen"),
		),
		ScrollUp: key.NewBinding(
			key.WithKeys("shift+up", "K"),
			key.WithHelp("shift+↑", "scroll up"),
		),
		ScrollDown: key.NewBinding(
			key.WithKeys("shift+down", "J"),
			key.WithHelp("shift+↓", "scroll down"),
		),
		ScrollLeft: key.NewBinding(
			key.WithKeys("shift+left", "H"),
			key.WithHelp("shift+←", "scroll left"),
		),
		ScrollRight: key.NewBinding(
			key.WithKeys("shift+right", "L"),
			key.WithHelp("shift+→", "scroll right"),
		),
		Scroll: key.NewBinding(
			key.WithKeys("shift+left", "shift+down", "shift+up", "shift+right"),
			key.WithHelp("shift+←↓↑→", "scroll"),
		),
	}
}

var _ Dialog = (*DiffPreview)(nil)

func NewDiffPreview(com *surfacecommon.Common, data DiffPreviewData) *DiffPreview {
	if com == nil {
		com = surfacecommon.DefaultCommon()
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	km := defaultDiffPreviewKeyMap()
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	vp.KeyMap = viewport.KeyMap{
		Up:           km.ScrollUp,
		Down:         km.ScrollDown,
		Left:         km.ScrollLeft,
		Right:        km.ScrollRight,
		PageUp:       key.NewBinding(key.WithDisabled()),
		PageDown:     key.NewBinding(key.WithDisabled()),
		HalfPageUp:   key.NewBinding(key.WithDisabled()),
		HalfPageDown: key.NewBinding(key.WithDisabled()),
	}

	return &DiffPreview{
		com:      com,
		data:     data,
		viewport: vp,
		help:     h,
		keyMap:   km,
	}
}

func (*DiffPreview) ID() string {
	return DiffPreviewID
}

func (d *DiffPreview) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, d.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, d.keyMap.ToggleDiffMode):
			if d.hasStructuredDiff() {
				newMode := !d.isSplitMode()
				d.diffSplitMode = &newMode
				d.viewportDirty = true
			}
		case key.Matches(msg, d.keyMap.ToggleFullscreen):
			d.fullscreen = !d.fullscreen
		case key.Matches(msg, d.keyMap.ScrollDown), key.Matches(msg, d.keyMap.ScrollUp):
			d.viewport, _ = d.viewport.Update(msg)
		case key.Matches(msg, d.keyMap.ScrollLeft):
			if d.hasStructuredDiff() {
				d.scrollLeft()
			} else {
				d.viewport, _ = d.viewport.Update(msg)
			}
		case key.Matches(msg, d.keyMap.ScrollRight):
			if d.hasStructuredDiff() {
				d.scrollRight()
			} else {
				d.viewport, _ = d.viewport.Update(msg)
			}
		default:
			d.viewport, _ = d.viewport.Update(msg)
			d.viewportDirty = true
		}
	case tea.MouseMsg:
		if isWheelMouse(msg) {
			mouse := msg.Mouse()
			if d.hasStructuredDiff() {
				switch mouse.Button {
				case tea.MouseWheelLeft:
					d.scrollLeft()
				case tea.MouseWheelRight:
					d.scrollRight()
				default:
					d.viewport, _ = d.viewport.Update(msg)
				}
			} else {
				d.viewport, _ = d.viewport.Update(msg)
			}
		}
	}

	return nil
}

func (d *DiffPreview) hasStructuredDiff() bool {
	return d.data.OldContent != "" || d.data.NewContent != ""
}

func (d *DiffPreview) isSplitMode() bool {
	if d.diffSplitMode != nil {
		return *d.diffSplitMode
	}
	return d.defaultDiffSplitMode
}

func (d *DiffPreview) scrollLeft() {
	d.diffXOffset = max(0, d.diffXOffset-horizontalScrollStep)
	d.viewportDirty = true
}

func (d *DiffPreview) scrollRight() {
	d.diffXOffset += horizontalScrollStep
	d.viewportDirty = true
}

func (d *DiffPreview) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	t := d.com.Styles
	d.windowWidth = area.Dx()
	d.windowHeight = area.Dy()

	forceFullscreen := area.Dx() <= minWindowWidth || area.Dy() <= minWindowHeight

	var width, maxHeight int
	if forceFullscreen || d.fullscreen {
		width = area.Dx()
		maxHeight = area.Dy()
	} else {
		width = min(int(float64(area.Dx())*diffSizeRatio), diffMaxWidth)
		maxHeight = int(float64(area.Dy()) * diffSizeRatio)
	}

	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - t.Dialog.View.GetHorizontalFrameSize() - 2

	header := d.renderHeader(contentWidth)
	helpView := d.help.View(d)

	headerHeight := lipgloss.Height(header)
	helpHeight := lipgloss.Height(helpView)
	frameHeight := dialogStyle.GetVerticalFrameSize() + 2

	d.defaultDiffSplitMode = false

	renderedContent := d.renderContent(contentWidth)
	contentHeight := lipgloss.Height(renderedContent)
	availableHeight := max(3, maxHeight-headerHeight-helpHeight-frameHeight)

	needsScrollbar := contentHeight > availableHeight
	viewportWidth := contentWidth
	if needsScrollbar {
		viewportWidth = contentWidth - 1
	}

	if d.viewport.Width() != viewportWidth {
		d.viewportDirty = true
		renderedContent = d.renderContent(viewportWidth)
	}

	d.viewport.SetWidth(viewportWidth)
	d.viewport.SetHeight(availableHeight)
	if d.viewportDirty {
		d.viewport.SetContent(renderedContent)
		d.viewportWidth = d.viewport.Width()
		d.viewportDirty = false
	}

	content := d.viewport.View()
	scrollbar := ""
	if needsScrollbar {
		scrollbar = surfacecommon.Scrollbar(t, availableHeight, d.viewport.TotalLineCount(), availableHeight, d.viewport.YOffset())
	}
	if scrollbar != "" {
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)
	}

	view := dialogStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, "", content, "", helpView))
	DrawCenter(scr, area, view)
	return nil
}

func (d *DiffPreview) renderHeader(contentWidth int) string {
	t := d.com.Styles
	titleText := strings.TrimSpace(d.data.Title)
	if titleText == "" {
		titleText = "Changes"
	}
	title := surfacecommon.DialogTitle(t, titleText, contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)

	lines := []string{title}
	if path := strings.TrimSpace(d.data.FilePath); path != "" {
		lines = append(lines, d.renderKeyValue("Path", prettyPath(path), contentWidth))
	}
	lines = append(lines, d.renderKeyValue("Delta", d.renderDelta(), contentWidth))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (d *DiffPreview) renderDelta() string {
	return fmt.Sprintf("+%d  -%d", d.data.Additions, d.data.Removals)
}

func (d *DiffPreview) renderKeyValue(keyText, value string, width int) string {
	t := d.com.Styles
	keyStr := t.Muted.Render(keyText)
	valueStr := t.Base.Width(width - lipgloss.Width(keyStr) - 1).Render(" " + value)
	return lipgloss.JoinHorizontal(lipgloss.Left, keyStr, valueStr)
}

func (d *DiffPreview) renderContent(contentWidth int) string {
	if !d.viewportDirty {
		if d.isSplitMode() && d.hasStructuredDiff() {
			return d.splitDiffContent
		}
		if d.unifiedDiffContent != "" {
			return d.unifiedDiffContent
		}
	}

	if d.hasStructuredDiff() {
		formatter := surfacecommon.DiffFormatter(d.com.Styles).
			Before(prettyPath(d.data.FilePath), d.data.OldContent).
			After(prettyPath(d.data.FilePath), d.data.NewContent).
			XOffset(d.diffXOffset).
			Width(contentWidth)
		if d.isSplitMode() {
			d.splitDiffContent = formatter.Split().String()
			return d.splitDiffContent
		}
		d.unifiedDiffContent = formatter.Unified().String()
		return d.unifiedDiffContent
	}

	d.unifiedDiffContent = ""
	return d.unifiedDiffContent
}

func (d *DiffPreview) ShortHelp() []key.Binding {
	bindings := []key.Binding{
		d.keyMap.Close,
		d.keyMap.Scroll,
	}
	if d.hasStructuredDiff() {
		bindings = append(bindings, d.keyMap.ToggleDiffMode, d.keyMap.ToggleFullscreen)
	}
	return bindings
}

func (d *DiffPreview) FullHelp() [][]key.Binding {
	return [][]key.Binding{d.ShortHelp()}
}
