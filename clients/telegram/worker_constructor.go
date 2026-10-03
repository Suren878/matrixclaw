package telegram

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func NewWorker(cfg Config) (*Worker, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("telegram: daemon base URL is required")
	}
	if cfg.Geo == nil {
		cfg.Geo = tools.NewOSMServiceFromEnv()
	}
	client, err := NewClient(ClientConfig{
		Token:      cfg.BotToken,
		HTTPClient: &http.Client{Timeout: telegramHTTPTimeout},
	})
	if err != nil {
		return nil, err
	}
	return newWorker(cfg, client), nil
}

func newWorker(cfg Config, api BotAPI) *Worker {
	offset := cfg.Offset
	if offset == nil {
		offset = &atomic.Int64{}
	}
	return &Worker{
		api:              api,
		config:           cfg,
		daemonHTTP:       &http.Client{Timeout: daemonHTTPTimeout},
		flushInterval:    streamFlushInterval,
		offset:           offset,
		deliveryRetryAt:  map[string]time.Time{},
		deliveryReceipts: map[string]time.Time{},
		states:           map[string]*runDeliveryState{},
		prompts:          map[string]controlplane.PromptData{},
		callbacks:        newRecentMap[string](recentCallbackLimit),
		inline:           newRecentMap[string](recentInlineLimit),
		inlineRuns:       newRecentMap[struct{}](recentInlineLimit),
		messages:         newRecentMap[struct{}](recentMessageLimit),
		locations:        map[string]telegramLocationContext{},
		pendingLocations: map[string]pendingLocationRequest{},
		chatActions:      map[string]time.Time{},
		geo:              cfg.Geo,
	}
}
