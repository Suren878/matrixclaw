package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestTargetFromClientDeliveryResolvesTelegramRoute(t *testing.T) {
	tests := []struct {
		name       string
		delivery   core.ClientDelivery
		messageID  int64
		externalID string
	}{
		{
			name:       "uses address",
			delivery:   core.ClientDelivery{ExternalKey: "old-key", Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 42, ThreadID: 7, MessageID: 99})},
			messageID:  99,
			externalID: "old-key",
		},
		{
			name:       "derives external key from address",
			delivery:   core.ClientDelivery{Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 42, ThreadID: 7})},
			externalID: "42:7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, ok := targetFromClientDelivery(tt.delivery)
			if !ok {
				t.Fatal("targetFromClientDelivery() ok = false, want true")
			}
			if target.chatID != 42 || target.threadID != 7 || target.messageID != tt.messageID || target.externalKey != tt.externalID {
				t.Fatalf("target = %+v, want chat=42 thread=7 message=%d external=%q", target, tt.messageID, tt.externalID)
			}
		})
	}
}

func TestTargetFromClientDeliveryRejectsMissingAddress(t *testing.T) {
	if target, ok := targetFromClientDelivery(core.ClientDelivery{ExternalKey: "42:7"}); ok {
		t.Fatalf("targetFromClientDelivery() = %+v, true; want false without address", target)
	}
}

func TestDispatchRestartCommandSendsDeliveryAddress(t *testing.T) {
	var restartRequest core.AdminRestartRequest
	server := newRestartRequestServer(t, &restartRequest)

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)
	worker.config.ClientName = "telegram-alias"

	err := worker.dispatchRestartCommandAndEdit(context.Background(), chatTarget{
		chatID:      42,
		threadID:    7,
		externalKey: "42:7",
	}, 0)
	if err != nil {
		t.Fatalf("dispatchRestartCommandAndEdit() error = %v", err)
	}
	if restartRequest.Notification == nil {
		t.Fatal("restart notification = nil, want delivery target")
	}
	if restartRequest.Notification.Client != "telegram-alias" {
		t.Fatalf("notification client = %q, want telegram-alias", restartRequest.Notification.Client)
	}
	if restartRequest.Notification.ExternalKey != "42:7" {
		t.Fatalf("notification external key = %q, want 42:7", restartRequest.Notification.ExternalKey)
	}

	var address DeliveryAddress
	if err := json.Unmarshal(restartRequest.Notification.Address, &address); err != nil {
		t.Fatalf("decode notification address error = %v", err)
	}
	if address.ChatID != 42 || address.ThreadID != 7 || address.MessageID != 1 {
		t.Fatalf("address = %+v, want chat=42 thread=7 message=1", address)
	}
}

func TestDispatchRestartCommandUsesEditedMessageAddress(t *testing.T) {
	var restartRequest core.AdminRestartRequest
	server := newRestartRequestServer(t, &restartRequest)

	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, server.URL)
	worker.config.ClientName = "telegram-alias"

	err := worker.dispatchRestartCommandAndEdit(context.Background(), chatTarget{
		chatID:      42,
		threadID:    7,
		externalKey: "42:7",
	}, 99)
	if err != nil {
		t.Fatalf("dispatchRestartCommandAndEdit() error = %v", err)
	}
	if len(api.editMessageRequests) != 1 || api.editMessageRequests[0].MessageID != 99 {
		t.Fatalf("edit requests = %+v, want message 99", api.editMessageRequests)
	}
	if len(api.sendMessageRequests) != 0 {
		t.Fatalf("send requests = %d, want no fallback message", len(api.sendMessageRequests))
	}
	if restartRequest.Notification == nil {
		t.Fatal("restart notification = nil, want delivery target")
	}
	if restartRequest.Notification.Client != "telegram-alias" {
		t.Fatalf("notification client = %q, want telegram-alias", restartRequest.Notification.Client)
	}
	var address DeliveryAddress
	if err := json.Unmarshal(restartRequest.Notification.Address, &address); err != nil {
		t.Fatalf("decode notification address error = %v", err)
	}
	if address.ChatID != 42 || address.ThreadID != 7 || address.MessageID != 99 {
		t.Fatalf("address = %+v, want chat=42 thread=7 message=99", address)
	}
}

func TestDispatchRestartCommandUsesFallbackMessageAddress(t *testing.T) {
	var restartRequest core.AdminRestartRequest
	server := newRestartRequestServer(t, &restartRequest)

	api := &fakeBotAPI{
		editMessageErr: &APIError{
			StatusCode:  http.StatusBadRequest,
			ErrorCode:   http.StatusBadRequest,
			Description: "message to edit not found",
		},
	}
	worker := newTestWorker(t, api, server.URL)

	err := worker.dispatchRestartCommandAndEdit(context.Background(), chatTarget{
		chatID:      42,
		threadID:    7,
		externalKey: "42:7",
	}, 99)
	if err != nil {
		t.Fatalf("dispatchRestartCommandAndEdit() error = %v", err)
	}
	if len(api.editMessageRequests) != 1 || api.editMessageRequests[0].MessageID != 99 {
		t.Fatalf("edit requests = %+v, want old message 99", api.editMessageRequests)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("send requests = %d, want fallback progress message", len(api.sendMessageRequests))
	}
	if restartRequest.Notification == nil {
		t.Fatal("restart notification = nil, want delivery target")
	}
	var address DeliveryAddress
	if err := json.Unmarshal(restartRequest.Notification.Address, &address); err != nil {
		t.Fatalf("decode notification address error = %v", err)
	}
	if address.MessageID != 1 {
		t.Fatalf("restart delivery message_id = %d, want fallback message 1", address.MessageID)
	}
}

func newRestartRequestServer(t *testing.T, restartRequest *core.AdminRestartRequest) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/admin/restart" {
			t.Fatalf("unexpected daemon request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(restartRequest); err != nil {
			t.Fatalf("decode restart request error = %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRestartDeliveryCodecRejectsMissingMessage(t *testing.T) {
	_, err := (RestartDeliveryCodec{}).NormalizeRestartDeliveryAddress(json.RawMessage(`{"chat_id":42}`))
	if err == nil {
		t.Fatal("NormalizeRestartDeliveryAddress() error = nil, want missing message_id error")
	}
}

func TestRestartDeliverySenderEditsAddressedMessage(t *testing.T) {
	api := &fakeBotAPI{}
	sender, err := NewRestartDeliverySender(RestartDeliverySenderConfig{API: api})
	if err != nil {
		t.Fatalf("NewRestartDeliverySender() error = %v", err)
	}

	err = sender.DeliverRestartNotification(context.Background(), core.ClientDelivery{
		Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 42, ThreadID: 7, MessageID: 99}),
		Summary: "Restarted",
	}, "fallback")
	if err != nil {
		t.Fatalf("DeliverRestartNotification() error = %v", err)
	}
	if len(api.editMessageRequests) != 1 {
		t.Fatalf("edit requests = %d, want one", len(api.editMessageRequests))
	}
	got := api.editMessageRequests[0]
	if got.ChatID != 42 || got.MessageID != 99 || got.Text != "Restarted" {
		t.Fatalf("edit request = %+v, want addressed restart text", got)
	}
}

func TestRestartDeliverySenderUsesFallbackWithoutSummary(t *testing.T) {
	api := &fakeBotAPI{}
	sender, err := NewRestartDeliverySender(RestartDeliverySenderConfig{API: api})
	if err != nil {
		t.Fatalf("NewRestartDeliverySender() error = %v", err)
	}

	err = sender.DeliverRestartNotification(context.Background(), core.ClientDelivery{
		Address: encodeDeliveryAddress(DeliveryAddress{ChatID: 42, MessageID: 99}),
	}, "fallback")
	if err != nil {
		t.Fatalf("DeliverRestartNotification() error = %v", err)
	}
	got := api.editMessageRequests[0]
	if got.Text != "fallback" {
		t.Fatalf("edit text = %q, want fallback", got.Text)
	}
}
