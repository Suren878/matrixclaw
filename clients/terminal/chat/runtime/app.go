package runtime

import (
	"context"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/readmodel"
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
	"github.com/Suren878/matrixclaw/internal/updater"
)

const reconnectDelay = time.Second
const compactModeHeightBreakpoint = 30
const workingStatusTickInterval = 120 * time.Millisecond
const serverStatusRefreshInterval = time.Second
const serverRestartPollInterval = time.Second
const serverRestartProgressText = "Daemon is restarting..."
const serverRestartCompleteText = "Daemon restarted."

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
	// frame is the layout of the last update; chatWidth the chat's width in it.
	frame     appLayout
	chatWidth int

	loading bool
	err     string
	read    *readmodel.Model
	chat    *surfacemodel.Chat
	rows    keptRows
	input   surfaceinput.Model
	stream  stream

	transientMessages   []surfacemessage.Message
	workingDir          string
	version             string
	suppressedApprovals map[string]struct{}
	focus               appFocus
	busy                bool
	busyInputMode       core.BusyInputMode
	now                 time.Time
	spinnerFrame        int
	restartPending      bool
	restartRequestedAt  time.Time
	restartTUIPending   bool
	commandsDialogRoot  bool
	returnToCommands    bool
	updatePrompted      bool
	updateInstalling    bool
	controlplaneSeq     uint64
	todoPanel           todoPanelChoice
}

func newApp(ctx context.Context, rt *Runtime) *appModel {
	styles := surfacestyles.DefaultStyles()
	com := &surfacecommon.Common{Styles: &styles}
	workingDir, _ := os.Getwd()
	version := runtimeVersion("")
	if rt != nil {
		if cfgWorkingDir := strings.TrimSpace(rt.config.WorkingDir); cfgWorkingDir != "" {
			workingDir = cfgWorkingDir
		}
		version = runtimeVersion(rt.config.Version)
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
		suppressedApprovals: map[string]struct{}{},
		rows:                keptRows{},
		focus:               appFocusEditor,
		busyInputMode:       core.BusyInputModeSteer,
		now:                 time.Now(),
	}
}

func (m *appModel) Init() tea.Cmd {
	return tea.Batch(m.reload(), m.input.Focus(), m.workingTickCmd(), m.checkUpdateCmd())
}
