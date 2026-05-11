package daemoncmd

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/clients/telegram"
)

type clientRegistry struct {
	clients []clientAdapter
}

func newClientRegistry() *clientRegistry {
	return &clientRegistry{
		clients: []clientAdapter{
			&telegramClientAdapter{},
		},
	}
}

func (r *clientRegistry) Apply(ctx context.Context, bootstrap bootstrapConfig) error {
	if r == nil {
		return nil
	}
	for _, client := range r.clients {
		if client == nil {
			continue
		}
		if err := client.Apply(ctx, bootstrap); err != nil {
			return err
		}
	}
	return nil
}

func (r *clientRegistry) RestartDeliverySenders(bootstrap bootstrapConfig) []restartDeliverySender {
	if r == nil {
		return nil
	}
	senders := []restartDeliverySender{}
	for _, client := range r.clients {
		if client == nil {
			continue
		}
		sender, ok := client.RestartDeliverySender(bootstrap)
		if ok {
			senders = append(senders, sender)
		}
	}
	return senders
}

type clientAdapter interface {
	Apply(context.Context, bootstrapConfig) error
	RestartDeliveryAddressNormalizer() restartDeliveryAddressNormalizer
	RestartDeliverySender(bootstrapConfig) (restartDeliverySender, bool)
}

type telegramClientAdapter struct {
	mu          sync.Mutex
	cancel      context.CancelFunc
	offset      atomic.Int64
	commandsSet bool
}

func (a *telegramClientAdapter) Apply(ctx context.Context, bootstrap bootstrapConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := telegramBootstrap(bootstrap)
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if !cfg.Enabled {
		return nil
	}

	worker, err := telegram.NewWorker(telegram.Config{
		BaseURL:                 daemonBaseURL(bootstrap.Addr),
		APIToken:                bootstrap.APIToken,
		BotToken:                cfg.BotToken,
		AllowedUserID:           cfg.AllowedUserID,
		Offset:                  &a.offset,
		SkipCommandRegistration: a.commandsSet,
	})
	if err != nil {
		return err
	}
	a.commandsSet = true

	workerCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go func() {
		if err := worker.Run(workerCtx); err != nil && workerCtx.Err() == nil {
			log.Printf("matrixclaw telegram worker stopped: %v", err)
		}
	}()
	return nil
}

func (a *telegramClientAdapter) RestartDeliveryAddressNormalizer() restartDeliveryAddressNormalizer {
	return telegram.RestartDeliveryCodec{}
}

func (a *telegramClientAdapter) RestartDeliverySender(bootstrap bootstrapConfig) (restartDeliverySender, bool) {
	cfg := telegramBootstrap(bootstrap)
	if !cfg.Enabled {
		return nil, false
	}
	sender, err := telegram.NewRestartDeliverySender(telegram.RestartDeliverySenderConfig{
		BotToken: cfg.BotToken,
	})
	if err != nil {
		log.Printf("matrixclawd telegram restart delivery sender failed: %v", err)
		return nil, false
	}
	return sender, true
}

type telegramClientBootstrap struct {
	Enabled       bool
	BotToken      string
	AllowedUserID int64
}

func telegramBootstrap(bootstrap bootstrapConfig) telegramClientBootstrap {
	client := bootstrap.Clients[telegram.ClientName]
	cfg := telegramClientBootstrap{
		Enabled:       client.Enabled,
		BotToken:      strings.TrimSpace(client.Values["bot_token"]),
		AllowedUserID: client.Int64Values["allowed_user_id"],
	}
	return cfg
}
