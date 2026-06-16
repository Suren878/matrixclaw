package clientcmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	tuiruntime "github.com/Suren878/matrixclaw/clients/terminal/chat/runtime"
	"github.com/Suren878/matrixclaw/internal/core"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func runTUICommand(stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	cfg, err := service.Load()
	if err != nil {
		if errors.Is(err, appsetup.ErrConfigNotFound) {
			_, _ = fmt.Fprintf(stderr, "%s: setup not found at %s\n", binaryName, service.Path())
			_, _ = fmt.Fprintf(stderr, "%s: run `%s setup` first\n", binaryName, binaryName)
			return 1
		}
		if errors.Is(err, appsetup.ErrUnsupportedConfigVersion) {
			_, _ = fmt.Fprintf(stderr, "%s: setup at %s uses an unsupported version\n", binaryName, service.Path())
			_, _ = fmt.Fprintf(stderr, "%s: reopen `%s setup` to recreate the setup file\n", binaryName, binaryName)
			return 1
		}
		_, _ = fmt.Fprintf(stderr, "%s: tui: %v\n", binaryName, err)
		return 1
	}
	if _, err := ensureDaemon(context.Background(), service); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: tui: ensure daemon: %v\n", binaryName, err)
		return 1
	}
	if refreshed, err := service.Load(); err == nil {
		cfg = refreshed
	}
	workingDir, err := resolveTUIWorkingDir(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: tui: %v\n", binaryName, err)
		return 2
	}
	providerName, providerModel := activeProviderInfo(cfg)
	if err := openTUI(context.Background(), tuiruntime.Config{
		BaseURL:     daemonBaseURL(cfg.Daemon.HTTPAddr),
		APIToken:    cfg.Daemon.APIToken,
		ClientName:  tuiruntime.DefaultClientName,
		ExternalKey: tuiruntime.DefaultExternalKey,
		WorkingDir:  workingDir,
		Provider:    providerName,
		Model:       providerModel,
		Assistant: core.AssistantProfile{
			Name:               cfg.Assistant.Name,
			SystemPrompt:       cfg.Assistant.SystemPrompt,
			CustomInstructions: cfg.Assistant.CustomInstructions,
		},
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: tui: %v\n", binaryName, err)
		return 1
	}
	return 0
}
