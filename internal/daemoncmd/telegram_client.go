package daemoncmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/clients/telegram"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/modules/geo"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type telegramClientAdapter struct {
	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	applied     telegram.Config
	offset      atomic.Int64
	commandsSet bool
	geo         *geo.OSMService
	// botAPIURL overrides the Telegram Bot API address in tests.
	botAPIURL string
}

// Apply restarts the worker only when its config changed or it stopped, and
// waits for the old worker to stop so two never poll at once.
func (a *telegramClientAdapter) Apply(ctx context.Context, bootstrap bootstrapConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	boot := bootstrap.Telegram
	cfg := telegram.Config{}
	if boot.Enabled {
		cfg = telegram.Config{
			BaseURL:         daemonclient.BaseURL(bootstrap.Addr),
			APIToken:        bootstrap.APIToken,
			BotToken:        boot.BotToken,
			TelegramBaseURL: a.botAPIURL,
			AllowedUserID:   boot.AllowedUserID,
			InlineCachePath: telegramInlineCachePath(bootstrap.DBPath),
			Offset:          &a.offset,
			Geo:             a.geo,
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if cfg == a.applied && (!boot.Enabled || a.workerRunning()) {
		return nil
	}
	a.stopWorker()
	a.applied = telegram.Config{}
	if !boot.Enabled {
		return nil
	}

	workerCfg := cfg
	workerCfg.SkipCommandRegistration = a.commandsSet
	worker, err := telegram.NewWorker(workerCfg)
	if err != nil {
		return err
	}
	a.commandsSet = true
	a.applied = cfg

	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	a.cancel = cancel
	a.done = done
	safego.Go("telegram.worker", func() {
		defer close(done)
		if err := worker.Run(workerCtx); err != nil && workerCtx.Err() == nil {
			log.Printf("matrixclaw telegram worker stopped: %v", err)
		}
	})
	return nil
}

func (a *telegramClientAdapter) workerRunning() bool {
	if a.done == nil {
		return false
	}
	select {
	case <-a.done:
		return false
	default:
		return true
	}
}

func (a *telegramClientAdapter) stopWorker() {
	if a.cancel == nil {
		return
	}
	a.cancel()
	<-a.done
	a.cancel = nil
	a.done = nil
}

type telegramClientBootstrap struct {
	Enabled       bool
	BotToken      string
	AllowedUserID int64
}

func telegramBootstrapFromSetup(cfg setup.TelegramConfig) (telegramClientBootstrap, error) {
	client := telegramClientBootstrap{
		Enabled:  cfg.Enabled,
		BotToken: strings.TrimSpace(cfg.BotToken),
	}
	if raw := strings.TrimSpace(cfg.AllowedUserID); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return telegramClientBootstrap{}, fmt.Errorf("parse telegram allowed user id: %w", err)
		}
		client.AllowedUserID = id
	}
	return client, nil
}

func telegramInlineCachePath(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(dbPath), "telegram-inline-cache.json")
}

func automationDeliveryTargets(bootstrap bootstrapConfig) []core.ClientDeliveryTarget {
	targets := []core.ClientDeliveryTarget{}
	cfg := bootstrap.Telegram
	if cfg.Enabled && cfg.BotToken != "" && cfg.AllowedUserID != 0 {
		address, err := json.Marshal(telegram.ChatDeliveryAddress(cfg.AllowedUserID))
		if err == nil {
			targets = append(targets, core.ClientDeliveryTarget{
				Client:      telegram.ClientName,
				ExternalKey: strconv.FormatInt(cfg.AllowedUserID, 10),
				Address:     address,
			})
		}
	}
	return targets
}
