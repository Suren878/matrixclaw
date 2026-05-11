package dialog

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
)

const (
	defaultDialogHeight = 20
)

// CloseKey is the default key binding to close dialogs.
var CloseKey = key.NewBinding(
	key.WithKeys("esc", "alt+esc"),
	key.WithHelp("esc", "exit"),
)

// Action is a dialog action returned after handling a message.
type Action any

// Dialog is a component that can be displayed on top of the UI.
type Dialog interface {
	ID() string
	HandleMsg(msg tea.Msg) Action
	Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor
}

// LoadingDialog is a dialog that can show a loading state.
type LoadingDialog interface {
	StartLoading() tea.Cmd
	StopLoading()
}

// Overlay manages multiple stacked dialogs.
type Overlay struct {
	dialogs []Dialog
}

// NewOverlay creates a new dialog overlay.
func NewOverlay(dialogs ...Dialog) *Overlay {
	return &Overlay{dialogs: dialogs}
}

// HasDialogs reports whether any dialogs are open.
func (d *Overlay) HasDialogs() bool {
	return len(d.dialogs) > 0
}

// ContainsDialog reports whether a dialog with the given ID exists.
func (d *Overlay) ContainsDialog(dialogID string) bool {
	for _, dialog := range d.dialogs {
		if dialog.ID() == dialogID {
			return true
		}
	}
	return false
}

// OpenDialog pushes a dialog to the front.
func (d *Overlay) OpenDialog(dialog Dialog) {
	d.dialogs = append(d.dialogs, dialog)
}

// CloseDialog closes a dialog by ID.
func (d *Overlay) CloseDialog(dialogID string) {
	for i, dialog := range d.dialogs {
		if dialog.ID() == dialogID {
			d.removeDialog(i)
			return
		}
	}
}

// CloseFrontDialog closes the top-most dialog.
func (d *Overlay) CloseFrontDialog() {
	if len(d.dialogs) == 0 {
		return
	}
	d.removeDialog(len(d.dialogs) - 1)
}

// CloseAll closes every open dialog.
func (d *Overlay) CloseAll() {
	d.dialogs = nil
}

// Dialog returns the dialog with the given ID.
func (d *Overlay) Dialog(dialogID string) Dialog {
	for _, dialog := range d.dialogs {
		if dialog.ID() == dialogID {
			return dialog
		}
	}
	return nil
}

// DialogLast returns the top-most dialog.
func (d *Overlay) DialogLast() Dialog {
	if len(d.dialogs) == 0 {
		return nil
	}
	return d.dialogs[len(d.dialogs)-1]
}

// BringToFront moves a dialog to the front.
func (d *Overlay) BringToFront(dialogID string) {
	for i, dialog := range d.dialogs {
		if dialog.ID() != dialogID {
			continue
		}
		d.dialogs = append(d.dialogs[:i], d.dialogs[i+1:]...)
		d.dialogs = append(d.dialogs, dialog)
		return
	}
}

// Update routes a message to the top-most dialog.
func (d *Overlay) Update(msg tea.Msg) tea.Msg {
	if len(d.dialogs) == 0 {
		return nil
	}
	dialog := d.dialogs[len(d.dialogs)-1]
	if dialog == nil {
		return nil
	}
	return dialog.HandleMsg(msg)
}

// StartLoading starts the top dialog loading state when supported.
func (d *Overlay) StartLoading() tea.Cmd {
	dialog := d.DialogLast()
	if ld, ok := dialog.(LoadingDialog); ok {
		return ld.StartLoading()
	}
	return nil
}

// StopLoading stops the top dialog loading state when supported.
func (d *Overlay) StopLoading() {
	dialog := d.DialogLast()
	if ld, ok := dialog.(LoadingDialog); ok {
		ld.StopLoading()
	}
}

// DrawCenterCursor draws the view centered in the area.
func DrawCenterCursor(scr uv.Screen, area uv.Rectangle, view string, cur *uv.Cursor) {
	width, height := lipgloss.Size(view)
	center := surfacecommon.CenterRect(area, width, height)
	if cur != nil {
		cur.X += center.Min.X
		cur.Y += center.Min.Y
	}
	uv.NewStyledString(view).Draw(scr, center)
}

// DrawCenter draws the view centered in the area.
func DrawCenter(scr uv.Screen, area uv.Rectangle, view string) {
	DrawCenterCursor(scr, area, view, nil)
}

// DrawOnboarding draws the view bottom-left aligned in the area.
func DrawOnboarding(scr uv.Screen, area uv.Rectangle, view string) {
	DrawOnboardingCursor(scr, area, view, nil)
}

// DrawOnboardingCursor draws the view bottom-left aligned and offsets the cursor.
func DrawOnboardingCursor(scr uv.Screen, area uv.Rectangle, view string, cur *uv.Cursor) {
	width, height := lipgloss.Size(view)
	bottomLeft := surfacecommon.BottomLeftRect(area, width, height)
	if cur != nil {
		cur.X += bottomLeft.Min.X
		cur.Y += bottomLeft.Min.Y
	}
	uv.NewStyledString(view).Draw(scr, bottomLeft)
}

// Draw renders all open dialogs.
func (d *Overlay) Draw(scr uv.Screen, area uv.Rectangle) *uv.Cursor {
	var cur *uv.Cursor
	for _, dialog := range d.dialogs {
		cur = dialog.Draw(scr, area)
	}
	return cur
}

func (d *Overlay) removeDialog(idx int) {
	if idx < 0 || idx >= len(d.dialogs) {
		return
	}
	d.dialogs = append(d.dialogs[:idx], d.dialogs[idx+1:]...)
}
