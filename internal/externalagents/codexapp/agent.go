package codexapp

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

const AgentID = "codex-app"

type Agent struct {
	Path    string
	Enabled bool
}

func (a Agent) ID() string {
	return AgentID
}

func (a Agent) DisplayName() string {
	return "Codex"
}

func (a Agent) Aliases() []string {
	return []string{"codex"}
}

func (a Agent) Capabilities() externalagents.Capabilities {
	return externalagents.Capabilities{
		StartSession:     true,
		ResumeSession:    true,
		StreamingEvents:  true,
		ToolEvents:       true,
		Interrupt:        false,
		ConfigurablePath: true,
	}
}

func (a Agent) Models(context.Context) []string {
	return []string{
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
	}
}

func (a Agent) Available(ctx context.Context) externalagents.Availability {
	resolved, err := LookupPath(a.Path)
	installed := err == nil
	detail := ""
	if !installed {
		detail = "codex binary not found"
		if errors.Is(err, errCodexAppBundlePath) {
			detail = err.Error()
		}
	}
	return externalagents.Availability{
		Installed: installed,
		Enabled:   a.Enabled && installed,
		AuthState: "unknown",
		Mode:      "app-server",
		Path:      resolved,
		Version:   Version(ctx, a.Path),
		Detail:    detail,
	}
}
