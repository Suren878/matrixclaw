package telegram

import (
	"context"
	"fmt"
)

func (w *Worker) dispatchCommandAndEdit(ctx context.Context, target chatTarget, messageID int64, command string) error {
	return w.dispatchCommandAndEditPage(ctx, target, messageID, command, 0)
}

func (w *Worker) dispatchCommandAndEditPage(ctx context.Context, target chatTarget, messageID int64, command string, page int) error {
	result, err := w.dispatcher().Handle(ctx, target.externalKey, command)
	if err != nil {
		return w.editOrSend(ctx, target, messageID, fmt.Sprintf("Command failed: %v", err), nil)
	}
	return w.renderCommandResultPage(ctx, target, messageID, result, page)
}
