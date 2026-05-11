package clientcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	tuiruntime "github.com/Suren878/matrixclaw/clients/terminal/chat/runtime"
	"github.com/Suren878/matrixclaw/internal/core"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func runTUICommand(stderr io.Writer, binaryName string, service *appsetup.Service) int {
	cfg, err := service.Load()
	if err != nil {
		if errors.Is(err, appsetup.ErrConfigNotFound) {
			fmt.Fprintf(stderr, "%s: setup not found at %s\n", binaryName, service.Path())
			fmt.Fprintf(stderr, "%s: run `%s setup` first\n", binaryName, binaryName)
			return 1
		}
		if errors.Is(err, appsetup.ErrUnsupportedConfigVersion) {
			fmt.Fprintf(stderr, "%s: setup at %s uses an unsupported version\n", binaryName, service.Path())
			fmt.Fprintf(stderr, "%s: reopen `%s setup` to recreate the setup file\n", binaryName, binaryName)
			return 1
		}
		fmt.Fprintf(stderr, "%s: tui: %v\n", binaryName, err)
		return 1
	}
	if _, err := ensureDaemon(context.Background(), service); err != nil {
		fmt.Fprintf(stderr, "%s: tui: ensure daemon: %v\n", binaryName, err)
		return 1
	}
	if refreshed, err := service.Load(); err == nil {
		cfg = refreshed
	}
	workingDir, _ := os.Getwd()
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
		fmt.Fprintf(stderr, "%s: tui: %v\n", binaryName, err)
		return 1
	}
	return 0
}
