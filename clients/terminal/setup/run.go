package setup

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/Suren878/matrixclaw/clients/terminal/terminalrender"
	"github.com/Suren878/matrixclaw/internal/setup"
)

var ErrAborted = errors.New("setup aborted")

func Run(ctx context.Context, service *setup.Service) (setup.ApplyResult, error) {
	profile := terminalrender.Configure()

	setupModel, err := newModel(service)
	if err != nil {
		return setup.ApplyResult{}, err
	}

	program := tea.NewProgram(
		setupModel,
		tea.WithContext(ctx),
		tea.WithColorProfile(profile),
	)
	finalModel, err := program.Run()
	if err != nil {
		return setup.ApplyResult{}, err
	}

	resultModel, ok := finalModel.(*model)
	if !ok {
		return setup.ApplyResult{}, errors.New("unexpected setup model type")
	}
	if resultModel.aborted {
		return setup.ApplyResult{}, ErrAborted
	}
	if resultModel.err != nil {
		return setup.ApplyResult{}, resultModel.err
	}
	return resultModel.result, nil
}
