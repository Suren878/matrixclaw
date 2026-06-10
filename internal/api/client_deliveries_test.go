package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestClientDeliveryFailEndpointMarksFailed(t *testing.T) {
	ctx := context.Background()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	app := core.New(sqliteStore).WithIDGenerator(func(prefix string) string { return prefix + "_test" })
	delivery, err := app.CreateClientDelivery(ctx, core.ClientDelivery{
		Type:   core.ClientDeliveryTypeDocument,
		Client: "telegram",
	})
	if err != nil {
		t.Fatalf("create delivery: %v", err)
	}
	server := New(app)

	body, _ := json.Marshal(core.ClientDeliveryFailRequest{Error: "storage_path is empty"})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/client-deliveries/"+delivery.ID+"/fail", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("fail status = %d body=%s", recorder.Code, recorder.Body.String())
	}

	deliveries, err := app.ListClientDeliveries(ctx, core.ClientDeliveryFilter{Client: "telegram", Status: core.ClientDeliveryStatusFailed})
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("failed deliveries = %d, want 1", len(deliveries))
	}
	if deliveries[0].Status != core.ClientDeliveryStatusFailed || deliveries[0].Error != "storage_path is empty" || deliveries[0].FinishedAt == nil {
		t.Fatalf("delivery = %#v, want failed with error and finished_at", deliveries[0])
	}
}
