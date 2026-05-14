package codexapp

import (
	"context"

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

func (a Agent) Available(ctx context.Context) externalagents.Availability {
	resolved, err := LookupPath(a.Path)
	installed := err == nil
	detail := ""
	if !installed {
		detail = "codex binary not found"
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
