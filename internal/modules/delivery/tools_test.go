package delivery

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type fakeDeliveryCreator struct {
	deliveries []core.ClientDelivery
}

func (f *fakeDeliveryCreator) CreateClientDelivery(_ context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error) {
	delivery.ID = "delivery_1"
	delivery.Status = core.ClientDeliveryStatusPending
	f.deliveries = append(f.deliveries, delivery)
	return delivery, nil
}

func TestSendFileRequestsApproval(t *testing.T) {
	store := newSendFileTestStore(t)
	deliveries := &fakeDeliveryCreator{}
	executor := NewSendFileTool(store, deliveries)

	result, err := executor.Execute(context.Background(), tools.Call{
		SessionID:   "session_1",
		RunID:       "run_1",
		ToolCallID:  "tool_1",
		Client:      "telegram",
		ExternalKey: "123",
		Args:        json.RawMessage(`{"path":"vpn/amneziawg-client.conf","caption":"VPN config"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Approval == nil {
		t.Fatal("Execute() did not request approval")
	}
	if result.Approval.ToolID != sendFileToolID {
		t.Fatalf("approval ToolID = %q, want %q", result.Approval.ToolID, sendFileToolID)
	}
	if result.Approval.Path != "vpn/amneziawg-client.conf" {
		t.Fatalf("approval Path = %q", result.Approval.Path)
	}
	if len(deliveries.deliveries) != 0 {
		t.Fatalf("created %d deliveries before approval", len(deliveries.deliveries))
	}
}

func TestSendFileCreatesDocumentDeliveryAfterApproval(t *testing.T) {
	store := newSendFileTestStore(t)
	deliveries := &fakeDeliveryCreator{}
	executor := NewSendFileTool(store, deliveries)

	result, err := executor.Execute(context.Background(), tools.Call{
		SessionID:   "session_1",
		RunID:       "run_1",
		ToolCallID:  "tool_1",
		Client:      "telegram",
		ExternalKey: "123",
		Approved:    true,
		Args:        json.RawMessage(`{"path":"vpn/amneziawg-client.conf","file_name":"client.conf","caption":"VPN config"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() returned error result: %s", result.Content)
	}
	if result.Status != tools.ResultStatusSuccess {
		t.Fatalf("result Status = %q, want success", result.Status)
	}
	if len(deliveries.deliveries) != 1 {
		t.Fatalf("created %d deliveries, want 1", len(deliveries.deliveries))
	}
	delivery := deliveries.deliveries[0]
	if delivery.Type != core.ClientDeliveryTypeDocument {
		t.Fatalf("delivery Type = %q, want document", delivery.Type)
	}
	if delivery.Client != "telegram" || delivery.ExternalKey != "123" {
		t.Fatalf("delivery target = %q/%q", delivery.Client, delivery.ExternalKey)
	}
	var payload core.DocumentDeliveryPayload
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.StoragePath != "vpn/amneziawg-client.conf" {
		t.Fatalf("payload StoragePath = %q", payload.StoragePath)
	}
	if payload.FileName != "client.conf" || payload.Caption != "VPN config" || payload.MIMEType != "application/octet-stream" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Size != int64(len("secret config")) {
		t.Fatalf("payload Size = %d", payload.Size)
	}
}

func newSendFileTestStore(t *testing.T) *localstorage.LocalStore {
	t.Helper()
	store, err := localstorage.NewLocalStore(filepath.Join(t.TempDir(), "storage"), 0)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	if _, err := store.SaveBytes("vpn/amneziawg-client.conf", []byte("secret config"), "", nil, ""); err != nil {
		t.Fatalf("SaveBytes: %v", err)
	}
	return store
}
