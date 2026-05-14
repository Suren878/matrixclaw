package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/commandcatalog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
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

func catalogCommand(id commandcatalog.CommandID, args string) string {
	return commandcatalog.CommandLine(id, args)
}

func matchesCatalogCommand(text string, id commandcatalog.CommandID, args string) bool {
	spec, parsedArgs, ok := controlplane.Parse(text)
	return ok && spec.ID == id && strings.EqualFold(strings.TrimSpace(parsedArgs), strings.TrimSpace(args))
}
