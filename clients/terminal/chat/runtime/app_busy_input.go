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

func (m *appModel) handleBusySubmitCommand(content string) (bool, tea.Cmd) {
	command := strings.TrimSpace(content)
	if command == "" || !strings.HasPrefix(command, "/") {
		return false, nil
	}
	if mode, text, ok := parseBusyMessageCommand(command); ok {
		if strings.TrimSpace(text) == "" {
			m.showError("usage: /queue <text> or /steer <text>")
			return true, nil
		}
		if m.state().SessionID() == "" {
			m.showError("no active session")
			return true, nil
		}
		m.clearNotice()
		m.setBusy(true)
		if m.chat != nil {
			m.chat.ScrollToBottom()
		}
		return true, m.sendMessageCmd(text, nil, mode)
	}
	fields := strings.Fields(command)
	if !strings.EqualFold(fields[0], "/busy") {
		return false, nil
	}
	if len(fields) == 1 || strings.EqualFold(fields[1], "status") {
		m.showInputStatus(fmt.Sprintf("Busy mode: %s", m.input.busyMode))
		return true, nil
	}
	switch strings.ToLower(fields[1]) {
	case string(core.BusyInputModeQueue):
		m.input.busyMode = core.BusyInputModeQueue
	case string(core.BusyInputModeSteer):
		m.input.busyMode = core.BusyInputModeSteer
	case string(core.BusyInputModeInterrupt):
		m.input.busyMode = core.BusyInputModeInterrupt
	default:
		m.showError("usage: /busy [queue|steer|interrupt|status]")
		return true, nil
	}
	m.clearNotice()
	m.showInputStatus(fmt.Sprintf("Busy mode: %s", m.input.busyMode))
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
	m.clearNotice()
	m.upsertTransientMessage(newBusyInputStatusMessage(text))
	m.syncChat()
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
