package claudecode

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/externalagents"
)

const AgentID = "claude-code"

func (r *Runtime) ID() string {
	return AgentID
}

func (r *Runtime) DisplayName() string {
	return "Claude Code"
}

func (r *Runtime) Models(context.Context) []string {
	return models()
}

func models() []string {
	return []string{
		"sonnet",
		"opus",
		"claude-sonnet-4-6",
		"claude-sonnet-4-5",
		"claude-opus-4-5",
	}
}

func (r *Runtime) Available(ctx context.Context) externalagents.Availability {
	resolved, version, err := r.binary.Probe(ctx)
	detail := ""
	if err != nil {
		detail = "claude binary not found"
		if strings.Contains(err.Error(), ".app bundle") {
			detail = err.Error()
		}
	}
	return externalagents.Availability{
		Installed: err == nil,
		Enabled:   r.enabled && err == nil,
		Mode:      "cli",
		Path:      resolved,
		Version:   version,
		Detail:    detail,
	}
}
