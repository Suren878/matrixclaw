package runtime

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
)

const busyInputStatusMessageID = "local:busy-input-status"

func normalizeLocalBusyInputMode(mode core.BusyInputMode) core.BusyInputMode {
	switch core.BusyInputMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case core.BusyInputModeSteer:
		return core.BusyInputModeSteer
	case core.BusyInputModeInterrupt:
		return core.BusyInputModeInterrupt
	default:
		return core.BusyInputModeQueue
	}
}

func (m *appModel) handleBusySubmitCommand(content string) (bool, tea.Cmd) {
	command := strings.TrimSpace(content)
	if command == "" || !strings.HasPrefix(command, "/") {
		return false, nil
	}
	if mode, text, ok := parseBusyMessageCommand(command); ok {
		if strings.TrimSpace(text) == "" {
			m.err = "usage: /queue <text> or /steer <text>"
			return true, nil
		}
		if strings.TrimSpace(m.session) == "" {
			m.err = "no active session"
			return true, nil
		}
		m.err = ""
		m.setBusy(true)
		if m.chat != nil {
			m.chat.ScrollToBottom()
		}
		return true, m.sendMessageCmd(text, nil, mode)
	}
	if !strings.HasPrefix(strings.ToLower(command), "/busy") {
		return false, nil
	}
	fields := strings.Fields(command)
	if len(fields) == 1 || strings.EqualFold(fields[1], "status") {
		m.showInputStatus(fmt.Sprintf("Busy mode: %s", normalizeLocalBusyInputMode(m.busyInputMode)))
		return true, nil
	}
	switch strings.ToLower(fields[1]) {
	case string(core.BusyInputModeQueue):
		m.busyInputMode = core.BusyInputModeQueue
	case string(core.BusyInputModeSteer):
		m.busyInputMode = core.BusyInputModeSteer
	case string(core.BusyInputModeInterrupt):
		m.busyInputMode = core.BusyInputModeInterrupt
	default:
		m.err = "usage: /busy [queue|steer|interrupt|status]"
		return true, nil
	}
	m.err = ""
	m.showInputStatus(fmt.Sprintf("Busy mode: %s", m.busyInputMode))
	return true, nil
}

func parseBusyMessageCommand(command string) (core.BusyInputMode, string, bool) {
	command = strings.TrimSpace(command)
	lower := strings.ToLower(command)
	for _, spec := range []struct {
		prefix string
		mode   core.BusyInputMode
	}{
		{prefix: "/queue", mode: core.BusyInputModeQueue},
		{prefix: "/steer", mode: core.BusyInputModeSteer},
	} {
		if lower == spec.prefix {
			return spec.mode, "", true
		}
		if strings.HasPrefix(lower, spec.prefix+" ") {
			text := strings.TrimSpace(command[len(spec.prefix):])
			return spec.mode, text, true
		}
	}
	return "", "", false
}

func (m *appModel) showAcceptedInputStatus(status core.AcceptRunStatus) {
	switch status {
	case core.AcceptRunStatusQueued:
		m.showInputStatus("Queued for next turn")
	case core.AcceptRunStatusSteered:
		m.showInputStatus("Steer queued")
	case core.AcceptRunStatusInterrupting:
		m.showInputStatus("Interrupting current run")
	}
}

func (m *appModel) showConsumedInputStatus(input core.SessionInput) {
	if input.Status != core.SessionInputStatusConsumed {
		return
	}
	switch input.Mode {
	case core.BusyInputModeQueue, core.BusyInputModeInterrupt:
		m.showInputStatus("Agent consumed queued message")
	}
}

func (m *appModel) showInputStatus(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.err = ""
	m.upsertTransientMessage(newBusyInputStatusMessage(text))
	m.rebuildChat()
}

func newBusyInputStatusMessage(text string) surfacemessage.Message {
	now := time.Now().Unix()
	return surfacemessage.Message{
		ID:               busyInputStatusMessageID,
		Role:             surfacemessage.System,
		Parts:            []surfacemessage.ContentPart{surfacemessage.TextContent{Text: text}},
		CreatedAt:        now,
		UpdatedAt:        now,
		IsSummaryMessage: true,
	}
}
