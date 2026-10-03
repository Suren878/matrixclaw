package telegram

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Suren878/matrixclaw/internal/modules/geo"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type Config struct {
	BaseURL  string
	APIToken string
	BotToken string
	// TelegramBaseURL overrides the Bot API address; empty means Telegram's.
	TelegramBaseURL         string
	AllowedUserID           int64
	InlineCachePath         string
	Geo                     *geo.OSMService
	Offset                  *atomic.Int64
	SkipCommandRegistration bool
}

type Worker struct {
	api              BotAPI
	config           Config
	daemonHTTP       *http.Client
	flushInterval    time.Duration // assistant stream edits are at least this far apart
	offset           *atomic.Int64
	mu               sync.Mutex
	delivery         sync.Mutex
	deliveryRetryAt  map[string]time.Time // guarded by delivery
	deliveryReceipts map[string]time.Time // confirmed sends awaiting daemon acknowledgement
	states           map[string]*runDeliveryState
	prompts          map[string]pendingPrompt
	callbacks        *recentMap[string]   // long callback data by short ref
	inline           *recentMap[string]   // inline query text by button token
	inlineRuns       *recentMap[struct{}] // inline messages that already started a run
	messages         *recentMap[struct{}] // handled updates, for dedupe
	locations        map[string]telegramLocationContext
	pendingLocations map[string]pendingLocationRequest
	chatActions      map[string]time.Time
	geo              *geo.OSMService
	now              func() time.Time
}

type runDeliveryState struct {
	messages          []transcript.Message // the run's messages loaded so far, by seq
	messageIndex      map[string]int
	messagesLoaded    bool
	afterSeq          int64 // the next load asks for messages above this seq
	status            runStatusMessage
	errorSent         bool
	continueOffered   bool
	assistant         map[string]sentAssistantMessage
	approvals         map[string]int64
	voiceResults      map[string]int64
	voiceFingerprints map[string]int64
	notes             map[string]struct{}
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
	kind            targetKind
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
