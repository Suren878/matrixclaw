package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestUpdateInstallRequestsTerminalRestartAfterDaemonRestart(t *testing.T) {
	m := newApp(context.Background(), nil)

	m.handleUpdateInstall(updateInstallMsg{version: "v9.9.9"})
	if !m.restartTUIPending {
		t.Fatalf("restartTUIPending = false, want true")
	}

	m.restartPending = true
	m.restartRequestedAt = time.Now().UTC()
	cmd := m.handleServerRestartPoll(serverRestartPollMsg{
		deliveries: []core.ClientDelivery{{
			ID:        "delivery_test",
			Type:      core.ClientDeliveryTypeDaemonRestart,
			Status:    core.ClientDeliveryStatusReady,
			Summary:   "Daemon restarted.",
			CreatedAt: m.restartRequestedAt,
		}},
	})
	if cmd == nil {
		t.Fatalf("handleServerRestartPoll returned nil cmd, want terminal restart command")
	}
	if m.restartTUIPending {
		t.Fatalf("restartTUIPending = true after scheduling restart, want false")
	}
}
