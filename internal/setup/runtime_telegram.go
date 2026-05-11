package setup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func newTelegramHTTPValidator() *telegramHTTPValidator {
	return &telegramHTTPValidator{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (v *telegramHTTPValidator) Validate(ctx context.Context, token string) (TelegramSummary, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return TelegramSummary{Status: "Disabled"}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org/bot"+token+"/getMe", nil)
	if err != nil {
		return TelegramSummary{}, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return TelegramSummary{}, fmt.Errorf("validate telegram bot token: %s", redactTelegramBotToken(err, token))
	}
	defer resp.Body.Close()

	var payload struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
			IsBot    bool   `json:"is_bot"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return TelegramSummary{}, fmt.Errorf("decode telegram getMe response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !payload.OK {
		return TelegramSummary{}, errors.New("telegram bot token is invalid")
	}
	if !payload.Result.IsBot {
		return TelegramSummary{}, errors.New("telegram token resolved to a non-bot account")
	}

	return TelegramSummary{
		Status:   "Configured",
		Username: payload.Result.Username,
	}, nil
}

func redactTelegramBotToken(err error, token string) string {
	if err == nil {
		return ""
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return err.Error()
	}
	return strings.ReplaceAll(err.Error(), "/bot"+token+"/", "/bot<redacted>/")
}
