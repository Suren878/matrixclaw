package setup

import (
	"fmt"
	"strconv"
	"strings"
)

type ClientBootstrap struct {
	Enabled     bool
	Values      map[string]string
	Int64Values map[string]int64
}

func ClientBootstrapsFromConfig(cfg Config) (map[string]ClientBootstrap, error) {
	telegramClient, err := telegramBootstrapFromConfig(cfg.Clients.Telegram)
	if err != nil {
		return nil, err
	}
	return map[string]ClientBootstrap{
		"telegram": telegramClient,
	}, nil
}

func telegramBootstrapFromConfig(cfg TelegramConfig) (ClientBootstrap, error) {
	client := ClientBootstrap{
		Enabled: cfg.Enabled,
		Values: map[string]string{
			"bot_token": strings.TrimSpace(cfg.BotToken),
		},
	}
	if raw := strings.TrimSpace(cfg.AllowedUserID); raw != "" {
		allowedUserID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return ClientBootstrap{}, fmt.Errorf("parse telegram allowed user id: %w", err)
		}
		client.Values["allowed_user_id"] = raw
		client.Int64Values = map[string]int64{
			"allowed_user_id": allowedUserID,
		}
	}
	return client, nil
}
