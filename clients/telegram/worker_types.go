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
	ChatActionInterval      time.Duration
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
	deliveryRetryAt  map[string]time.Time // guarded by delivery
	deliveryReceipts map[string]time.Time // confirmed sends awaiting daemon acknowledgement
	states           map[string]*runDeliveryState
	prompts          map[string]controlplane.PromptData
	callbacks        map[string]string
	inline           map[string]string
	inlineRuns       map[string]struct{}
	messages         map[string]struct{}
	messageLog       []string
	locations        map[string]telegramLocationContext
	pendingLocations map[string]pendingLocationRequest
	chatActions      map[string]time.Time
	geo              *tools.OSMService
	now              func() time.Time
}

type runDeliveryState struct {
	statusSent        bool
	continueOffered   bool
	assistant         map[string]sentAssistantMessage
	approvals         map[string]int64
	toolCalls         map[string]sentToolCallStatus
	voiceResults      map[string]int64
	voiceFingerprints map[string]int64
	notes             map[string]struct{}
}

type sentToolCallStatus struct {
	messageID int64
	text      string
	name      string
	input     string
	done      bool
}

type sentAssistantMessage struct {
	chunks         []sentAssistantChunk
	firstSeenAt    time.Time
	nextUpdateAt   time.Time
	draftID        int64
	draftText      string
	draftUpdatedAt time.Time
	draftActive    bool
	draftDisabled  bool
}

type sentAssistantChunk struct {
	messageID int64
	text      string
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
