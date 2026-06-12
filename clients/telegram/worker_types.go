package telegram

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type Config struct {
	BaseURL                 string
	APIToken                string
	BotToken                string
	TelegramBaseURL         string
	AllowedUserID           int64
	ClientName              string
	WorkingDir              string
	InlineCachePath         string
	PollTimeout             time.Duration
	PollLimit               int
	PollRetryDelay          time.Duration
	StreamFlushInterval     time.Duration
	BotHTTPClient           HTTPDoer
	DaemonHTTPClient        *http.Client
	Geo                     *tools.OSMService
	Offset                  *atomic.Int64
	SkipCommandRegistration bool
}

type Worker struct {
	api              BotAPI
	config           Config
	offset           *atomic.Int64
	mu               sync.Mutex
	delivery         sync.Mutex
	states           map[string]*runDeliveryState
	prompts          map[string]controlplane.PromptData
	callbacks        map[string]string
	inline           map[string]string
	inlineRuns       map[string]struct{}
	messages         map[string]struct{}
	messageLog       []string
	autoEdits        map[string]struct{}
	locations        map[string]telegramLocationContext
	pendingLocations map[string]pendingLocationRequest
	geo              *tools.OSMService
	now              func() time.Time
}

type runDeliveryState struct {
	assistant         map[string]sentAssistantMessage
	drafts            map[string]sentAssistantDraft
	approvals         map[string]int64
	toolCalls         map[string]sentToolCallStatus
	voiceResults      map[string]int64
	voiceFingerprints map[string]int64
}

type sentToolCallStatus struct {
	messageID int64
	text      string
	name      string
	input     string
	done      bool
}

type sentAssistantMessage struct {
	messageID int64
	text      string
}

type sentAssistantDraft struct {
	text   string
	sentAt time.Time
}

type chatTarget struct {
	kind            string
	chatID          int64
	messageID       int64
	guestQueryID    string
	inlineMessageID string
	externalKey     string
}

type telegramLocationContext struct {
	Location Location
	SharedAt time.Time
}

type pendingLocationRequest struct {
	Text string
}
