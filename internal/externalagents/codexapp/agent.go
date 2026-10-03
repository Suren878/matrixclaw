package codexapp

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

const AgentID = "codex-app"

func (r *Runtime) ID() string {
	return AgentID
}

func (r *Runtime) DisplayName() string {
	return "Codex"
}

func (r *Runtime) Models(context.Context) []string {
	return []string{
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
	}
}

func (r *Runtime) Available(ctx context.Context) externalagents.Availability {
	resolved, version, err := r.binary.Probe(ctx)
	detail := ""
	if err != nil {
		detail = "codex binary not found"
		if strings.Contains(err.Error(), ".app bundle") {
			detail = err.Error()
		}
	}
	return externalagents.Availability{
		Installed: err == nil,
		Enabled:   r.enabled && err == nil,
		Mode:      "app-server",
		Path:      resolved,
		Version:   version,
		Detail:    detail,
	}
}
