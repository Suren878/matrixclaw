package runtime

import (
	"context"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	surfaceheader "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/header"
	surfaceinput "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/input"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacemodel "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/model"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	"github.com/Suren878/matrixclaw/internal/updater"
)

const reconnectDelay = time.Second
const compactModeWidthBreakpoint = 120
const compactModeHeightBreakpoint = 30
const workingStatusTickInterval = 120 * time.Millisecond
const serverStatusRefreshInterval = time.Second
const serverRestartPollInterval = time.Second
const serverRestartProgressText = "Daemon is restarting..."
const serverRestartCompleteText = "Daemon restarted."

type loadInitialMsg struct {
	snapshot core.ClientSnapshot
	err      error
}

type subscribeReadyMsg struct {
	sessionID string
	streamID  uint64
	events    <-chan daemonclient.LiveEvent
	errs      <-chan error
	err       error
}

type liveEventMsg struct {
	streamID uint64
	event    daemonclient.LiveEvent
	err      error
	done     bool
}

type reconnectMsg struct{}

type resolveApprovalMsg struct {
	approval   core.Approval
	approved   bool
	approvalID string
	err        error
}

type sendMessageResultMsg struct {
	content     string
	attachments []surfaceeditor.Attachment
	result      core.AcceptRunResult
	planRun     bool
	err         error
}

type cancelRunResultMsg struct {
	run core.Run
	err error
}

type workingTickMsg struct {
	at time.Time
}

type controlplaneResultMsg struct {
	command string
	seq     uint64
	result  controlplane.Result
	err     error
}

type serverStatusRefreshMsg struct {
	text string
	rows []surfacedialog.InfoRow
	err  error
}

type serverStatusTickMsg struct{}

type serverRestartPollMsg struct {
	deliveries []core.ClientDelivery
	err        error
}

type serverRestartRequestMsg struct {
	err error
}

type serverRestartTickMsg struct{}

type serverRestartAckMsg struct {
	err error
}

type terminalRestartMsg struct {
	err error
}

type updateCheckMsg struct {
	update updater.Update
	ok     bool
	err    error
}

type updateInstallMsg struct {
	version string
	output  string
	err     error
}

type appFocus int

const (
	appFocusChat appFocus = iota
	appFocusEditor
	appFocusPlan
)

type appModel struct {
	ctx context.Context
	rt  *Runtime

	com    *surfacecommon.Common
	header *surfaceheader.Header
	status *surfaceheader.Status
	dialog *surfacedialog.Overlay
	help   help.Model
	styles surfacestyles.Styles

	width  int
	height int

	loading  bool
	err      string
	session  string
	read     *viewmodel.ReadModel
	chat     *surfacemodel.Chat
	input    surfaceinput.Model
	events   <-chan daemonclient.LiveEvent
	eventErr <-chan error

	transientMessages   []surfacemessage.Message
	workingDir          string
	version             string
	providerName        string
	providerModel       string
	suppressedApprovals map[string]struct{}
	autoEditSessions    map[string]struct{}
	focus               appFocus
	busy                bool
	streamID            uint64
	lastEventID         uint64
	now                 time.Time
	spinnerFrame        int
	restartPending      bool
	restartRequestedAt  time.Time
	restartTUIPending   bool
	returnToCommands    bool
	updatePrompted      bool
	updateInstalling    bool
	controlplaneSeq     uint64
	planPanelOpen       bool
	planAutoRun         bool
	planResumePrompted  bool
	skipPlanResumeOnce  bool
	initialLoadComplete bool
	planSelected        int
	planActionSelected  int
}

func newApp(ctx context.Context, rt *Runtime) *appModel {
	styles := surfacestyles.DefaultStyles()
	com := &surfacecommon.Common{Styles: &styles}
	workingDir, _ := os.Getwd()
	version := runtimeVersion("")
	providerName := ""
	providerModel := ""
	if rt != nil {
		if cfgWorkingDir := strings.TrimSpace(rt.config.WorkingDir); cfgWorkingDir != "" {
			workingDir = cfgWorkingDir
		}
		version = runtimeVersion(rt.config.Version)
		providerName = strings.TrimSpace(rt.config.Provider)
		providerModel = strings.TrimSpace(rt.config.Model)
	}
	input := surfaceinput.New(com)
	h := help.New()
	h.Styles = styles.Help
	return &appModel{
		ctx:                 ctx,
		rt:                  rt,
		com:                 com,
		header:              surfaceheader.New(&styles, version),
		status:              surfaceheader.NewStatus(&styles),
		dialog:              surfacedialog.NewOverlay(),
		help:                h,
		styles:              styles,
		loading:             true,
		input:               input,
		workingDir:          strings.TrimSpace(workingDir),
		version:             version,
		providerName:        providerName,
		providerModel:       providerModel,
		suppressedApprovals: map[string]struct{}{},
		autoEditSessions:    map[string]struct{}{},
		focus:               appFocusEditor,
		now:                 time.Now(),
	}
}

func (m *appModel) Init() tea.Cmd {
	return tea.Batch(m.loadInitialCmd(), m.input.Focus(), m.workingTickCmd(), m.checkUpdateCmd())
}
