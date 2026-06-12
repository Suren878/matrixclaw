package telegram

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func NewWorker(cfg Config) (*Worker, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("telegram: daemon base URL is required")
	}
	if strings.TrimSpace(cfg.ClientName) == "" {
		cfg.ClientName = defaultClientName
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = defaultPollTimeout
	}
	if cfg.PollLimit <= 0 || cfg.PollLimit > 100 {
		cfg.PollLimit = defaultPollLimit
	}
	if cfg.PollRetryDelay <= 0 {
		cfg.PollRetryDelay = defaultPollRetryDelay
	}
	if cfg.StreamFlushInterval <= 0 {
		cfg.StreamFlushInterval = defaultStreamFlushInterval
	}
	if cfg.BotHTTPClient == nil {
		cfg.BotHTTPClient = &http.Client{Timeout: defaultTelegramHTTPTimeout}
	}
	if cfg.DaemonHTTPClient == nil {
		cfg.DaemonHTTPClient = &http.Client{Timeout: defaultDaemonHTTPTimeout}
	}
	if cfg.Geo == nil {
		cfg.Geo = tools.NewOSMServiceFromEnv()
	}
	offset := cfg.Offset
	if offset == nil {
		offset = &atomic.Int64{}
	}

	client, err := NewClient(ClientConfig{
		Token:      cfg.BotToken,
		BaseURL:    cfg.TelegramBaseURL,
		HTTPClient: cfg.BotHTTPClient,
	})
	if err != nil {
		return nil, err
	}

	return &Worker{
		api:              client,
		config:           cfg,
		offset:           offset,
		states:           map[string]*runDeliveryState{},
		prompts:          map[string]controlplane.PromptData{},
		callbacks:        map[string]string{},
		inline:           map[string]string{},
		inlineRuns:       map[string]struct{}{},
		messages:         map[string]struct{}{},
		autoEdits:        map[string]struct{}{},
		locations:        map[string]telegramLocationContext{},
		pendingLocations: map[string]pendingLocationRequest{},
		geo:              cfg.Geo,
	}, nil
}
