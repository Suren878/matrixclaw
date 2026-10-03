package runtime

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
)

// noSession stands in for the read model before the first snapshot.
var noSession = readmodel.New(core.ClientSnapshot{})

// state is the session's read model, empty before the first snapshot.
func (m *appModel) state() *readmodel.Model {
	if m.read == nil {
		return noSession
	}
	return m.read
}

// currentSessionLLM is the session's provider and model; an external agent
// session names only its model.
func (m *appModel) currentSessionLLM() (string, string) {
	session := m.state().Session()
	if session == nil {
		return "", ""
	}
	if core.CoreSessionIsExternalAgent(*session) {
		return "", strings.TrimSpace(session.ModelID)
	}
	return strings.TrimSpace(session.ProviderID), strings.TrimSpace(session.ModelID)
}

func (m *appModel) setBusy(busy bool) {
	m.input.busy = busy
	m.input.SetWorking(busy)
}

func (m *appModel) workingTickCmd() tea.Cmd {
	return tea.Tick(workingStatusTickInterval, func(at time.Time) tea.Msg {
		return workingTickMsg{at: at}
	})
}

// syncPromptHistory fills the prompt history newest first, so Up recalls the latest prompt.
func (m *appModel) syncPromptHistory() {
	messages := m.state().Messages()
	prompts := make([]string, 0, len(messages))
	for _, msg := range slices.Backward(messages) {
		if msg.Role != surfacemessage.User {
			continue
		}
		text := strings.TrimSpace(msg.Content().Text)
		if text == "" {
			continue
		}
		prompts = append(prompts, text)
	}
	m.input.SetPromptHistory(prompts)
}

func runIsActive(run *core.Run) bool {
	if run == nil {
		return false
	}
	switch run.Status {
	case core.RunStatusAccepted, core.RunStatusRunning, core.RunStatusWaitingApproval, core.RunStatusWaitingEvents:
		return true
	default:
		return false
	}
}
