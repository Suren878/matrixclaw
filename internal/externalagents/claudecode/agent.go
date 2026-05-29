package claudecode

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

const AgentID = "claude-code"

type Agent struct {
	Path    string
	Enabled bool
}

func (a Agent) ID() string {
	return AgentID
}

func (a Agent) DisplayName() string {
	return "Claude Code"
}

func (a Agent) Aliases() []string {
	return []string{"claude"}
}

func (a Agent) Capabilities() externalagents.Capabilities {
	return externalagents.Capabilities{
		StartSession:     true,
		ResumeSession:    true,
		StreamingEvents:  false,
		ToolEvents:       false,
		Interrupt:        false,
		ConfigurablePath: true,
	}
}

func (a Agent) Models(context.Context) []string {
	return []string{
		"sonnet",
		"opus",
		"claude-sonnet-4-6",
		"claude-sonnet-4-5",
		"claude-opus-4-5",
	}
}

func (a Agent) Available(ctx context.Context) externalagents.Availability {
	resolved, err := LookupPath(a.Path)
	installed := err == nil
	detail := ""
	if !installed {
		detail = "claude binary not found"
		if errors.Is(err, errClaudeAppBundlePath) {
			detail = err.Error()
		}
	}
	return externalagents.Availability{
		Installed: installed,
		Enabled:   a.Enabled && installed,
		AuthState: "unknown",
		Mode:      "cli",
		Path:      resolved,
		Version:   Version(ctx, a.Path),
		Detail:    detail,
	}
}
