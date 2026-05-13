package telegram

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

type Config struct {
	BaseURL                 string
	APIToken                string
	BotToken                string
	TelegramBaseURL         string
	AllowedUserID           int64
	ClientName              string
	WorkingDir              string
	PollTimeout             time.Duration
	PollLimit               int
	PollRetryDelay          time.Duration
	StreamFlushInterval     time.Duration
	BotHTTPClient           HTTPDoer
	DaemonHTTPClient        *http.Client
	Offset                  *atomic.Int64
	SkipCommandRegistration bool
}

type Worker struct {
	api       BotAPI
	config    Config
	offset    *atomic.Int64
	mu        sync.Mutex
	runs      map[string]context.CancelFunc
	states    map[string]*runDeliveryState
	prompts   map[string]controlplane.PromptData
	callbacks map[string]string
	autoEdits map[string]struct{}
}

type runDeliveryState struct {
	assistant      map[string]sentAssistantMessage
	approvals      map[string]int64
	statusNotified bool
	deliveryID     string
}

type sentAssistantMessage struct {
	messageID int64
	text      string
}

type chatTarget struct {
	chatID      int64
	threadID    int64
	messageID   int64
	externalKey string
}
