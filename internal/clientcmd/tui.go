package clientcmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	tuiruntime "github.com/Suren878/matrixclaw/clients/terminal/chat/runtime"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func runTUICommand(stderr io.Writer, binaryName string, service *appsetup.Service, args []string) int {
	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "tui", err)
	}
	if _, err := ensureDaemon(context.Background(), service); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: tui: ensure daemon: %v\n", binaryName, err)
		return 1
	}
	if refreshed, err := service.Load(); err == nil {
		cfg = refreshed
	}
	if len(args) > 1 {
		_, _ = fmt.Fprintf(stderr, "%s: tui: tui accepts at most one WORKDIR argument\n", binaryName)
		return 2
	}
	workingDir, err := resolveWorkingDir(strings.Join(args, ""))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: tui: %v\n", binaryName, err)
		return 2
	}
	providerName, providerModel := activeProviderInfo(cfg)
	if err := openTUI(context.Background(), tuiruntime.Config{
		BaseURL:     daemonclient.BaseURL(cfg.Daemon.HTTPAddr),
		APIToken:    cfg.Daemon.APIToken,
		ClientName:  tuiruntime.DefaultClientName,
		ExternalKey: tuiruntime.DefaultExternalKey,
		WorkingDir:  workingDir,
		Provider:    providerName,
		Model:       providerModel,
		Assistant: core.AssistantProfile{
			Name:               cfg.Assistant.Name,
			SystemPrompt:       cfg.Assistant.SystemPromptOrDefault(),
			CustomInstructions: cfg.Assistant.CustomInstructions,
		},
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: tui: %v\n", binaryName, err)
		return 1
	}
	return 0
}
