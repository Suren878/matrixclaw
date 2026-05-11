package runtime

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (m *appModel) serverStatusCmd() tea.Cmd {
	return func() tea.Msg {
		if m.rt == nil {
			return serverStatusRefreshMsg{err: fmt.Errorf("terminal runtime is not configured")}
		}
		status, err := m.rt.ServerStatus(m.ctx)
		if err != nil {
			return serverStatusRefreshMsg{err: err}
		}
		return serverStatusRefreshMsg{
			text: controlplane.FormatServerStatus(status),
			rows: controlplane.FormatServerStatusRows(status),
		}
	}
}

func (m *appModel) serverStatusTickCmd() tea.Cmd {
	return tea.Tick(serverStatusRefreshInterval, func(time.Time) tea.Msg {
		return serverStatusTickMsg{}
	})
}

func (m *appModel) openServerStatusDialog() tea.Cmd {
	m.dialog.CloseAll()
	m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
		ID:          surfacedialog.ServerStatusInfoID,
		Title:       "Server Status",
		Rows:        []surfacedialog.InfoRow{{Label: "Status", Value: "Loading..."}},
		CloseAction: surfacedialog.ActionRunControlplaneCommand{Command: "/server"},
	}))
	return tea.Batch(m.serverStatusCmd(), m.serverStatusTickCmd())
}

func (m *appModel) openServerRestartDialog() tea.Cmd {
	m.dialog.CloseAll()
	m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
		ID:    surfacedialog.ServerRestartInfoID,
		Title: "Restart",
		Text:  serverRestartProgressText,
	}))
	m.restartPending = true
	m.restartRequestedAt = time.Now().UTC()
	return tea.Batch(m.restartDaemonCmd(), m.serverRestartTickCmd())
}

func isDaemonRestartCommand(text string) bool {
	spec, args, ok := controlplane.Parse(text)
	return ok && spec.ID == controlplane.CommandRestart && strings.EqualFold(strings.TrimSpace(args), "confirm")
}

func (m *appModel) restartDaemonCmd() tea.Cmd {
	return func() tea.Msg {
		if m.rt == nil {
			return serverRestartRequestMsg{err: fmt.Errorf("terminal runtime is not configured")}
		}
		return serverRestartRequestMsg{err: m.rt.RestartDaemonWithNotification(m.ctx)}
	}
}

func (m *appModel) serverRestartTickCmd() tea.Cmd {
	return tea.Tick(serverRestartPollInterval, func(time.Time) tea.Msg {
		return serverRestartTickMsg{}
	})
}

func (m *appModel) serverRestartDeliveryCmd() tea.Cmd {
	return func() tea.Msg {
		if m.rt == nil {
			return serverRestartPollMsg{err: fmt.Errorf("terminal runtime is not configured")}
		}
		deliveries, err := m.rt.ListClientDeliveries(m.ctx, core.ClientDeliveryFilter{
			Type:         core.ClientDeliveryTypeDaemonRestart,
			CreatedAfter: m.restartRequestedAt.Add(-2 * time.Second),
			Limit:        20,
		})
		return serverRestartPollMsg{deliveries: deliveries, err: err}
	}
}

func (m *appModel) acknowledgeRestartDeliveryCmd(deliveryID string) tea.Cmd {
	return func() tea.Msg {
		if m.rt == nil {
			return serverRestartAckMsg{err: fmt.Errorf("terminal runtime is not configured")}
		}
		return serverRestartAckMsg{err: m.rt.AcknowledgeClientDelivery(m.ctx, deliveryID)}
	}
}

func (m *appModel) handleServerStatusRefresh(msg serverStatusRefreshMsg) tea.Cmd {
	if msg.err != nil {
		m.err = msg.err.Error()
		return nil
	}
	if dialog, ok := m.dialog.Dialog(surfacedialog.ServerStatusInfoID).(*surfacedialog.Info); ok {
		if len(msg.rows) > 0 {
			dialog.SetRows(msg.rows)
		} else {
			dialog.SetText(msg.text)
		}
	}
	return nil
}

func (m *appModel) handleServerStatusTick() tea.Cmd {
	if m.dialog.ContainsDialog(surfacedialog.ServerStatusInfoID) {
		return tea.Batch(m.serverStatusCmd(), m.serverStatusTickCmd())
	}
	return nil
}

func (m *appModel) handleServerRestartRequest(msg serverRestartRequestMsg) tea.Cmd {
	if msg.err != nil {
		m.restartPending = false
		m.setServerRestartDialogText("Daemon restart failed: " + msg.err.Error())
	}
	return nil
}

func (m *appModel) handleServerRestartTick() tea.Cmd {
	if m.restartPending && m.dialog.ContainsDialog(surfacedialog.ServerRestartInfoID) {
		return m.serverRestartDeliveryCmd()
	}
	return nil
}

func (m *appModel) handleServerRestartPoll(msg serverRestartPollMsg) tea.Cmd {
	if !m.restartPending {
		return nil
	}
	if msg.err != nil {
		return m.serverRestartTickCmd()
	}
	delivery, ok := latestRestartDelivery(msg.deliveries, m.restartRequestedAt)
	if !ok {
		return m.serverRestartTickCmd()
	}
	switch delivery.Status {
	case core.ClientDeliveryStatusReady:
		m.restartPending = false
		m.setServerRestartDialogText(deliveryDisplayText(delivery, serverRestartCompleteText))
		return tea.Batch(m.acknowledgeRestartDeliveryCmd(delivery.ID), m.loadInitialCmd())
	case core.ClientDeliveryStatusFailed:
		m.restartPending = false
		if strings.TrimSpace(delivery.Error) != "" {
			m.setServerRestartDialogText("Daemon restart failed: " + delivery.Error)
		} else {
			m.setServerRestartDialogText("Daemon restart failed.")
		}
	}
	return nil
}

func (m *appModel) handleServerRestartAck(msg serverRestartAckMsg) tea.Cmd {
	m.autoEditSessions = map[string]struct{}{}
	if msg.err != nil {
		m.err = msg.err.Error()
	}
	return nil
}

func (m *appModel) setServerRestartDialogText(text string) {
	if dialog, ok := m.dialog.Dialog(surfacedialog.ServerRestartInfoID).(*surfacedialog.Info); ok {
		dialog.SetText(text)
		return
	}
	m.dialog.OpenDialog(surfacedialog.NewInfo(m.com, surfacedialog.InfoData{
		ID:    surfacedialog.ServerRestartInfoID,
		Title: "Restart",
		Text:  text,
	}))
}

func latestRestartDelivery(deliveries []core.ClientDelivery, requestedAt time.Time) (core.ClientDelivery, bool) {
	var latest core.ClientDelivery
	for _, delivery := range deliveries {
		if delivery.Type != core.ClientDeliveryTypeDaemonRestart {
			continue
		}
		if !requestedAt.IsZero() && delivery.CreatedAt.Before(requestedAt.Add(-2*time.Second)) {
			continue
		}
		if latest.ID == "" || delivery.CreatedAt.After(latest.CreatedAt) {
			latest = delivery
		}
	}
	return latest, latest.ID != ""
}

func deliveryDisplayText(delivery core.ClientDelivery, fallback string) string {
	if summary := strings.TrimSpace(delivery.Summary); summary != "" {
		return summary
	}
	return fallback
}
