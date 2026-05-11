package setup

import (
	"strings"
	"testing"
)

func TestClientBootstrapsFromConfigProjectsTelegram(t *testing.T) {
	clients, err := ClientBootstrapsFromConfig(Config{
		Clients: ClientsConfig{
			Telegram: TelegramConfig{
				Enabled:       true,
				BotToken:      " bot-token ",
				AllowedUserID: "12345",
			},
		},
	})
	if err != nil {
		t.Fatalf("ClientBootstrapsFromConfig() error = %v", err)
	}
	telegramClient := clients["telegram"]
	if !telegramClient.Enabled {
		t.Fatal("telegram bootstrap disabled, want enabled")
	}
	if telegramClient.Values["bot_token"] != "bot-token" {
		t.Fatalf("bot token = %q, want trimmed token", telegramClient.Values["bot_token"])
	}
	if telegramClient.Values["allowed_user_id"] != "12345" {
		t.Fatalf("allowed user id value = %q, want 12345", telegramClient.Values["allowed_user_id"])
	}
	if telegramClient.Int64Values["allowed_user_id"] != 12345 {
		t.Fatalf("allowed user id int = %d, want 12345", telegramClient.Int64Values["allowed_user_id"])
	}
}

func TestClientBootstrapsFromConfigRejectsInvalidTelegramAllowedUserID(t *testing.T) {
	_, err := ClientBootstrapsFromConfig(Config{
		Clients: ClientsConfig{
			Telegram: TelegramConfig{
				Enabled:       true,
				BotToken:      "bot-token",
				AllowedUserID: "not-an-int",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "parse telegram allowed user id") {
		t.Fatalf("ClientBootstrapsFromConfig() error = %v, want telegram allowed user id parse error", err)
	}
}
