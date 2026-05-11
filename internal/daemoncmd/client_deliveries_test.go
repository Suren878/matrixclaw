package daemoncmd

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestNormalizeRestartDeliveryAddressClonesUnknownClientAddress(t *testing.T) {
	raw := json.RawMessage(`{"room":"ops","message":"restart"}`)
	normalized, err := newClientRegistry().NormalizeRestartDeliveryAddress(&core.ClientDeliveryTarget{
		Client:  "custom",
		Address: raw,
	})
	if err != nil {
		t.Fatalf("normalizeRestartDeliveryAddress() error = %v", err)
	}
	if string(normalized) != string(raw) {
		t.Fatalf("normalized address = %s, want %s", normalized, raw)
	}
	raw[0] = '['
	if string(normalized) != `{"room":"ops","message":"restart"}` {
		t.Fatalf("normalized address aliases input: %s", normalized)
	}
}

func TestStartupRecoveryMarksPullRestartDeliveriesReady(t *testing.T) {
	app := newClientDeliveryTestCore(t)
	supervisor := newSupervisor(context.Background(), nil, app)
	delivery := createRestartDelivery(t, app, "pull-client")

	supervisor.DeliverPendingStartupNotifications(bootstrapConfig{})

	if got := clientDeliveryStatus(t, app, delivery.ID); got != core.ClientDeliveryStatusReady {
		t.Fatalf("delivery status = %q, want %q", got, core.ClientDeliveryStatusReady)
	}
}

func TestStartupRecoveryLeavesPushRestartDeliveriesPending(t *testing.T) {
	app := newClientDeliveryTestCore(t)
	supervisor := newSupervisor(context.Background(), nil, app)
	delivery := createRestartDelivery(t, app, "push-client")

	supervisor.markPullClientRestartDeliveriesReady([]restartDeliverySender{
		fakeRestartDeliverySender{name: "push-client"},
	})

	if got := clientDeliveryStatus(t, app, delivery.ID); got != core.ClientDeliveryStatusPending {
		t.Fatalf("delivery status = %q, want %q", got, core.ClientDeliveryStatusPending)
	}
}

type fakeRestartDeliverySender struct {
	name string
}

func (f fakeRestartDeliverySender) ClientName() string {
	return f.name
}

func (f fakeRestartDeliverySender) DeliverRestartNotification(context.Context, core.ClientDelivery, string) error {
	return nil
}

func newClientDeliveryTestCore(t *testing.T) *core.Core {
	t.Helper()

	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "daemoncmd.db"))
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	t.Cleanup(func() {
		_ = sqliteStore.Close()
	})
	return core.New(sqliteStore)
}

func createRestartDelivery(t *testing.T, app *core.Core, client string) core.ClientDelivery {
	t.Helper()

	delivery, err := app.CreateClientDelivery(context.Background(), core.ClientDelivery{
		Type:   core.ClientDeliveryTypeDaemonRestart,
		Client: client,
		Status: core.ClientDeliveryStatusPending,
	})
	if err != nil {
		t.Fatalf("CreateClientDelivery() error = %v", err)
	}
	return delivery
}

func clientDeliveryStatus(t *testing.T, app *core.Core, deliveryID string) core.ClientDeliveryStatus {
	t.Helper()

	deliveries, err := app.ListClientDeliveries(context.Background(), core.ClientDeliveryFilter{
		Type: core.ClientDeliveryTypeDaemonRestart,
	})
	if err != nil {
		t.Fatalf("ListClientDeliveries() error = %v", err)
	}
	for _, delivery := range deliveries {
		if delivery.ID == deliveryID {
			return delivery.Status
		}
	}
	t.Fatalf("delivery %s not found", deliveryID)
	return ""
}
