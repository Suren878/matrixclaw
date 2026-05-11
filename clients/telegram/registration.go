package telegram

import (
	"context"
	"log"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (w *Worker) registerCommands(ctx context.Context) {
	views := controlplane.PublicCommandView()
	commands := make([]BotCommand, 0, len(views))
	for _, view := range views {
		commands = append(commands, BotCommand{
			Command:     controlplane.CommandName(view.Command),
			Description: view.Title,
		})
	}
	if err := w.api.SetMyCommands(ctx, SetMyCommandsRequest{Commands: commands}); err != nil {
		log.Printf("telegram: set bot commands failed: %v", err)
	}
}
