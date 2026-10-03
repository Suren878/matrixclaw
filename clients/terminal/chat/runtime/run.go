package runtime

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/terminalrender"
)

func Run(ctx context.Context, config Config) error {
	profile := terminalrender.Configure()

	model := newApp(ctx, New(config))
	filter := newMouseEventFilter()
	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithColorProfile(profile),
		tea.WithFilter(filter.Filter),
	)
	_, err := program.Run()
	return err
}
