package daemoncmd

import (
	"encoding/json"
	"testing"

	"github.com/Suren878/matrixclaw/clients/telegram"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestAutomationDeliveryTargetsIncludesConfiguredTelegram(t *testing.T) {
	targets := automationDeliveryTargets(bootstrapConfig{
		Clients: map[string]setup.ClientBootstrap{
			telegram.ClientName: {
				Enabled: true,
				Values: map[string]string{
					"bot_token": "token",
				},
				Int64Values: map[string]int64{
					"allowed_user_id": 42,
				},
			},
		},
	})
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(targets))
	}
	if targets[0].Client != telegram.ClientName || targets[0].ExternalKey != "42" {
		t.Fatalf("target = %s/%s, want telegram/42", targets[0].Client, targets[0].ExternalKey)
	}
	var address struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := json.Unmarshal(targets[0].Address, &address); err != nil {
		t.Fatalf("target address decode error = %v", err)
	}
	if address.ChatID != 42 {
		t.Fatalf("target chat id = %d, want 42", address.ChatID)
	}
}

func TestAutomationDeliveryTargetsSkipsIncompleteTelegram(t *testing.T) {
	targets := automationDeliveryTargets(bootstrapConfig{
		Clients: map[string]setup.ClientBootstrap{
			telegram.ClientName: {
				Enabled: true,
				Values: map[string]string{
					"bot_token": "token",
				},
			},
		},
	})
	if len(targets) != 0 {
		t.Fatalf("targets = %d, want 0", len(targets))
	}
}
