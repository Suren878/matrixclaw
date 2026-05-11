package runtime

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/internal/terminalrender"
)

func Run(ctx context.Context, config Config) error {
	terminalrender.Configure()

	model := newApp(ctx, New(config))
	filter := newMouseEventFilter()
	program := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithEnvironment(terminalrender.Environment()),
		tea.WithColorProfile(terminalrender.ColorProfile()),
		tea.WithFilter(filter.Filter),
	)
	_, err := program.Run()
	return err
}
