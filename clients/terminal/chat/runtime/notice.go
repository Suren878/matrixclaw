package runtime

import (
	"strings"

	surfaceheader "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/header"
)

// notice is the status line's message: an error, a warning or information.
type notice struct {
	kind surfaceheader.StatusInfoType
	text string
}

func (m *appModel) showError(text string) {
	m.notice = notice{kind: surfaceheader.StatusInfoTypeError, text: strings.TrimSpace(text)}
}

func (m *appModel) showWarning(text string) {
	m.notice = notice{kind: surfaceheader.StatusInfoTypeWarn, text: strings.TrimSpace(text)}
}

func (m *appModel) showInfo(text string) {
	m.notice = notice{kind: surfaceheader.StatusInfoTypeInfo, text: strings.TrimSpace(text)}
}

func (m *appModel) clearNotice() { m.notice = notice{} }

// failure is the error shown, empty when the notice is not an error.
func (m *appModel) failure() string {
	if m.notice.kind != surfaceheader.StatusInfoTypeError {
		return ""
	}
	return m.notice.text
}
