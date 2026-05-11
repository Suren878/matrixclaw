package runtime

import (
	"context"

	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (r *Runtime) subscribeEvents(ctx context.Context, sessionID string, afterID uint64) (<-chan daemonclient.LiveEvent, <-chan error, error) {
	client, err := r.daemon()
	if err != nil {
		return nil, nil, err
	}
	return client.SubscribeEvents(ctx, sessionID, afterID)
}

func (r *Runtime) sendMessage(ctx context.Context, sessionID string, text string, attachments ...surfaceeditor.Attachment) (core.AcceptRunResult, error) {
	client, err := r.daemon()
	if err != nil {
		return core.AcceptRunResult{}, err
	}
	payload, err := prepareSendPayload(ctx, client, text, attachments)
	if err != nil {
		return core.AcceptRunResult{}, err
	}
	if len(payload.parts) > 0 {
		return client.SendMessageParts(ctx, sessionID, payload.content, payload.parts, r.config.WorkingDir)
	}
	return client.SendMessage(ctx, sessionID, payload.content, r.config.WorkingDir)
}

func (r *Runtime) cancelRun(ctx context.Context, runID string) (core.Run, error) {
	client, err := r.daemon()
	if err != nil {
		return core.Run{}, err
	}
	return client.CancelRun(ctx, runID)
}

func (r *Runtime) resolveApproval(ctx context.Context, approvalID string, approved bool) (core.Approval, error) {
	client, err := r.daemon()
	if err != nil {
		return core.Approval{}, err
	}
	return client.ResolveApproval(ctx, approvalID, approved)
}
