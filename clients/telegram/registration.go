package telegram

import (
	"context"
	"log"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (w *Worker) registerCommands(ctx context.Context) {
	menu := controlplane.CommandMenuView(controlplane.SurfaceTelegramBotCommands, controlplane.MenuState{})
	commands := make([]BotCommand, len(menu.Items))
	for index, item := range menu.Items {
		commands[index] = BotCommand{
			Command:     controlplane.CommandName(item.Command),
			Description: item.Title,
		}
	}
	if err := w.api.SetMyCommands(ctx, SetMyCommandsRequest{Commands: commands}); err != nil {
		log.Printf("telegram: set bot commands failed: %v", err)
	}
}
