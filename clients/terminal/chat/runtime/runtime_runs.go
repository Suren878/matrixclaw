package runtime

import (
	"context"

	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (r *Runtime) subscribeEvents(ctx context.Context, sessionID string) (<-chan daemonclient.LiveEvent, <-chan error, error) {
	client, err := r.daemon()
	if err != nil {
		return nil, nil, err
	}
	return client.SubscribeEvents(ctx, sessionID)
}

func (r *Runtime) sendMessageMode(ctx context.Context, sessionID string, text string, busyMode core.BusyInputMode, attachments ...surfaceeditor.Attachment) (core.AcceptRunResult, error) {
	client, err := r.daemon()
	if err != nil {
		return core.AcceptRunResult{}, err
	}
	payload, err := prepareSendPayload(ctx, client, text, attachments)
	if err != nil {
		return core.AcceptRunResult{}, err
	}
	return client.Send(ctx, core.HandleMessageInput{SessionID: sessionID, Text: payload.content, Parts: payload.parts, WorkingDir: r.config.WorkingDir, BusyMode: busyMode})
}

func (r *Runtime) cancelRun(ctx context.Context, runID string) (core.Run, error) {
	client, err := r.daemon()
	if err != nil {
		return core.Run{}, err
	}
	return client.CancelRun(ctx, runID)
}
