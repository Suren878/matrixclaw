package runtime

import (
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
)

const runStopNoticeMessageID = "run-stop-notice"

// showRunStopNotice tells how to continue a run that stopped early and drops the
// hint once a run is active again.
func (m *appModel) showRunStopNotice(run core.Run) {
	if runIsActive(&run) {
		m.removeTransientMessage(runStopNoticeMessageID)
		return
	}
	notice := controlplane.StopNotice(run.StopReason)
	if notice == "" {
		return
	}
	now := time.Now().Unix()
	m.upsertTransientMessage(surfacemessage.Message{
		ID:               runStopNoticeMessageID,
		Role:             surfacemessage.System,
		Parts:            []surfacemessage.ContentPart{surfacemessage.TextContent{Text: notice + " Send /continue to go on."}},
		CreatedAt:        now,
		UpdatedAt:        now,
		IsSummaryMessage: true,
	})
}
