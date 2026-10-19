package core

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/sandbox"
)

// commandSandbox is the resolved sandbox of a command a session runs with
// env: read_only in plan mode and for a read-only subagent, workspace_write
// over the session's working directory as the settings say.
func (c *Core) commandSandbox(ctx context.Context, sessionID string, env []string) (sandbox.Policy, error) {
	if c.sandbox == nil {
		return sandbox.Policy{}, nil
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return sandbox.Policy{}, err
	}
	readOnly := false
	if isSubagentSession(session) {
		if readOnly, err = c.readonlySubagent(ctx, session.ID); err != nil {
			return sandbox.Policy{}, err
		}
	}
	return c.sessionSandbox(session, readOnly, env).Resolve()
}

func (c *Core) sessionSandbox(session Session, readOnly bool, env []string) sandbox.Policy {
	mode := NormalizePermissionMode(string(session.PermissionMode))
	return c.sandbox().Policy(sandbox.Run{
		WorkingDir: session.WorkingDir,
		Unattended: mode == PermissionModeFullAuto,
		ReadOnly:   readOnly || mode == PermissionModePlan,
		Env:        env,
	})
}

// sandboxNote tells a run how its commands are confined; empty when they
// are not.
func (c *Core) sandboxNote(session Session, readOnly bool) string {
	if c.sandbox == nil {
		return ""
	}
	policy, err := c.sessionSandbox(session, readOnly, nil).Resolve()
	switch {
	case err != nil:
		return "unavailable, so every bash call fails: " + err.Error() + "."
	case policy.Mode == "":
		return ""
	}
	return policy.Describe() + `. A write it refuses fails with "Permission denied" or "Read-only file system"; keep files in the project or /tmp, and do not try to get around it.`
}

// SandboxStatus says how this daemon confines shell commands.
func (c *Core) SandboxStatus() string {
	if c.sandbox == nil {
		return ""
	}
	mode := c.sandbox().Mode
	if mode == "" {
		mode = "workspace_write in full_auto, read_only in plan mode and read-only subagents"
	}
	return string(mode) + "; " + sandbox.Detect().Text()
}
