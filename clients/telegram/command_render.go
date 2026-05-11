package telegram

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (w *Worker) renderCommandResult(ctx context.Context, target chatTarget, editMessageID int64, result controlplane.Result) error {
	return w.renderCommandResultPage(ctx, target, editMessageID, result, -1)
}

func (w *Worker) renderCommandResultPage(ctx context.Context, target chatTarget, editMessageID int64, result controlplane.Result, page int) error {
	presentation := presentCommandResult(result, page)
	if result.Prompt == nil {
		w.clearPrompt(target.externalKey)
	} else {
		w.setPrompt(target.externalKey, *result.Prompt)
	}
	return w.editOrSend(ctx, target, editMessageID, presentation.Text, presentation.ReplyMarkup)
}
