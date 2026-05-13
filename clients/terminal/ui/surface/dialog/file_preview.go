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
	"github.com/charmbracelet/x/ansi"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

const FilePreviewID = "file_preview"

type FilePreviewData struct {
	Title    string
	FilePath string
	Content  string
}

type ActionOpenFilePreview struct {
	Data FilePreviewData
}

type FilePreview struct {
	com          *surfacecommon.Common
	fullscreen   bool
	viewport     viewport.Model
	viewportHash string
	help         help.Model
	keyMap       filePreviewKeyMap
	data         FilePreviewData
}

type filePreviewKeyMap struct {
	Close            key.Binding
	ToggleFullscreen key.Binding
	ScrollUp         key.Binding
	ScrollDown       key.Binding
	Scroll           key.Binding
}

func defaultFilePreviewKeyMap() filePreviewKeyMap {
	return filePreviewKeyMap{
		Close: CloseKey,
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
		Scroll: key.NewBinding(
			key.WithKeys("shift+down", "shift+up"),
			key.WithHelp("shift+↓↑", "scroll"),
		),
	}
}

var _ Dialog = (*FilePreview)(nil)

func NewFilePreview(com *surfacecommon.Common, data FilePreviewData) *FilePreview {
	if com == nil {
		com = surfacecommon.DefaultCommon()
	}

	h := help.New()
	h.Styles = com.Styles.DialogHelpStyles()
	km := defaultFilePreviewKeyMap()
	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	vp.KeyMap = viewport.KeyMap{
		Up:           km.ScrollUp,
		Down:         km.ScrollDown,
		PageUp:       key.NewBinding(key.WithDisabled()),
		PageDown:     key.NewBinding(key.WithDisabled()),
		HalfPageUp:   key.NewBinding(key.WithDisabled()),
		HalfPageDown: key.NewBinding(key.WithDisabled()),
	}

	return &FilePreview{
		com:      com,
		data:     data,
		viewport: vp,
		help:     h,
		keyMap:   km,
	}
}

func (*FilePreview) ID() string {
	return FilePreviewID
}

func (p *FilePreview) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, p.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, p.keyMap.ToggleFullscreen):
			p.fullscreen = !p.fullscreen
		default:
			p.viewport, _ = p.viewport.Update(msg)
		}
	case tea.MouseMsg:
		if isWheelMouse(msg) {
			p.viewport, _ = p.viewport.Update(msg)
		}
	}
	return nil
}

func (p *FilePreview) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	t := p.com.Styles
	forceFullscreen := area.Dx() <= minWindowWidth || area.Dy() <= minWindowHeight

	var width, maxHeight int
	if forceFullscreen || p.fullscreen {
		width = area.Dx()
		maxHeight = area.Dy()
	} else {
		width = min(int(float64(area.Dx())*diffSizeRatio), diffMaxWidth)
		maxHeight = int(float64(area.Dy()) * diffSizeRatio)
	}

	dialogStyle := t.Dialog.View.Width(width).Padding(0, 1)
	contentWidth := width - t.Dialog.View.GetHorizontalFrameSize() - 2

	header := p.renderHeader(contentWidth)
	helpView := p.help.View(p)

	headerHeight := lipgloss.Height(header)
	helpHeight := lipgloss.Height(helpView)
	frameHeight := dialogStyle.GetVerticalFrameSize() + 2
	availableHeight := max(3, maxHeight-headerHeight-helpHeight-frameHeight)

	content := p.renderContent(contentWidth)
	contentHeight := lipgloss.Height(content)
	needsScrollbar := contentHeight > availableHeight
	viewportWidth := contentWidth
	if needsScrollbar {
		viewportWidth = contentWidth - 1
		content = p.renderContent(viewportWidth)
	}

	p.viewport.SetWidth(viewportWidth)
	p.viewport.SetHeight(availableHeight)
	contentHash := fmt.Sprintf("%d:%d:%s", viewportWidth, availableHeight, content)
	if p.viewportHash != contentHash {
		p.viewport.SetContent(content)
		p.viewportHash = contentHash
	}

	body := p.viewport.View()
	if needsScrollbar {
		scrollbar := surfacecommon.Scrollbar(t, availableHeight, p.viewport.TotalLineCount(), availableHeight, p.viewport.YOffset())
		if scrollbar != "" {
			body = lipgloss.JoinHorizontal(lipgloss.Top, body, scrollbar)
		}
	}

	view := dialogStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", helpView))
	DrawCenter(scr, area, view)
	return nil
}

func (p *FilePreview) renderHeader(contentWidth int) string {
	t := p.com.Styles
	titleText := strings.TrimSpace(p.data.Title)
	if titleText == "" {
		titleText = "File"
	}
	title := surfacecommon.DialogTitle(t, titleText, contentWidth-t.Dialog.Title.GetHorizontalFrameSize(), t.Primary, t.Secondary)
	title = t.Dialog.Title.Render(title)

	lines := []string{title}
	if path := strings.TrimSpace(p.data.FilePath); path != "" {
		lines = append(lines, p.renderKeyValue("Path", prettyPath(path), contentWidth))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (p *FilePreview) renderKeyValue(keyText, value string, width int) string {
	t := p.com.Styles
	keyStr := t.Muted.Render(keyText)
	valueStr := t.Base.Width(width - lipgloss.Width(keyStr) - 1).Render(" " + value)
	return lipgloss.JoinHorizontal(lipgloss.Left, keyStr, valueStr)
}

func (p *FilePreview) renderContent(width int) string {
	content := strings.TrimRight(p.data.Content, "\n")
	if content == "" {
		return p.com.Styles.Muted.Render("(empty file)")
	}
	return wrapPreviewContent(content, width)
}

func wrapPreviewContent(content string, width int) string {
	width = max(1, width)
	return ansi.Wrap(content, width, " \t/\\._:=,;|")
}

func (p *FilePreview) ShortHelp() []key.Binding {
	return []key.Binding{
		p.keyMap.Close,
		p.keyMap.Scroll,
		p.keyMap.ToggleFullscreen,
	}
}

func (p *FilePreview) FullHelp() [][]key.Binding {
	return [][]key.Binding{p.ShortHelp()}
}
