package telegram

import (
	"context"
	"log"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (w *Worker) registerCommands(ctx context.Context) {
	specs := controlplane.BotCommands()
	commands := make([]BotCommand, len(specs))
	for index, spec := range specs {
		commands[index] = BotCommand{
			Command:     controlplane.CommandName(spec.Command),
			Description: spec.Title,
		}
	}
	if err := w.api.SetMyCommands(ctx, SetMyCommandsRequest{Commands: commands}); err != nil {
		log.Printf("telegram: set bot commands failed: %v", err)
	}
}
