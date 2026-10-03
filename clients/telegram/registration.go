package telegram

import (
	"context"
	"log"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (w *Worker) registerCommands(ctx context.Context) {
	specs := controlplane.BotCommands()
	commands := make([]BotCommand, 0, len(specs)+1)
	for _, spec := range specs {
		commands = append(commands, BotCommand{
			Command:     controlplane.CommandName(spec.Command),
			Description: spec.Title,
		})
	}
	commands = append(commands, BotCommand{Command: "cancel", Description: "Cancel the running task"})
	if err := w.api.SetMyCommands(ctx, SetMyCommandsRequest{Commands: commands}); err != nil {
		log.Printf("telegram: set bot commands failed: %v", err)
	}
}
