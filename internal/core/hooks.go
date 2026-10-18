package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/hooks"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/safego"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// WithHooks sets the runner of the user's hooks and the checks after edits;
// without one none run.
func (c *Core) WithHooks(runner *hooks.Runner) *Core {
	c.hooks = runner
	return c
}

// hookScope is the session a hook runs for, in the call's working directory
// when it has one.
func hookScope(session Session, runID string, workingDir string) hooks.Scope {
	if workingDir = normalizeWorkingDir(workingDir); workingDir == "" {
		workingDir = session.WorkingDir
	}
	return hooks.Scope{
		SessionID:      session.ID,
		RunID:          runID,
		WorkingDir:     workingDir,
		PermissionMode: string(NormalizePermissionMode(string(session.PermissionMode))),
	}
}

// subjectFile is the file a call targets, "" when it targets none.
func subjectFile(subject permission.Subject) string {
	if subject.Kind == permission.KindFile {
		return subject.Value
	}
	return ""
}

// afterToolUse ends a call's result with what its PreToolUse hooks added and
// what its PostToolUse hooks and the check after an edit say; a failed call
// meets no PostToolUse hook.
func (c *Core) afterToolUse(ctx context.Context, check callPermission, call tools.Call, name string, result tools.Result) tools.Result {
	added := []string{check.hook.context}
	if c.hooks != nil && !result.IsError() {
		response := hooks.ToolResponse{Content: result.Content, Status: string(agent.ToolResultStatus(result))}
		scope := hookScope(check.session, call.RunID, call.WorkingDir)
		added = append(added, c.hooks.PostToolUse(ctx, scope, hooks.ToolCall{ID: call.ToolCallID, Name: name, Input: call.Args, File: subjectFile(check.request.Subject)}, response))
	}
	for _, text := range added {
		if text != "" {
			result.Content = strings.TrimRight(result.Content, "\n") + "\n\n" + text
		}
	}
	return result
}

// SessionHooks lists the hooks the session's runs meet: setup.json's and those
// of its project's hooks file, with whether the owner trusted it.
func (c *Core) SessionHooks(ctx context.Context, sessionID string) (hooks.Listing, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return hooks.Listing{}, err
	}
	if c.hooks == nil {
		return hooks.Listing{}, fmt.Errorf("%w: hooks are not configured", ErrExecutionUnavailable)
	}
	return c.hooks.List(session.WorkingDir), nil
}

// TrustSessionHooks trusts the session's project hooks file while it has the
// SHA256 sum the owner was shown; only the owner may call it.
func (c *Core) TrustSessionHooks(ctx context.Context, sessionID string, sum string) (hooks.Listing, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return hooks.Listing{}, err
	}
	if c.hooks == nil {
		return hooks.Listing{}, fmt.Errorf("%w: hooks are not configured", ErrExecutionUnavailable)
	}
	listing, err := c.hooks.Trust(session.WorkingDir, strings.TrimSpace(sum))
	if errors.Is(err, hooks.ErrNoProjectHooks) || errors.Is(err, hooks.ErrProjectChanged) {
		return hooks.Listing{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return listing, err
}

// notifyApproval runs the Notification hooks of a call that waits for approval.
func (c *Core) notifyApproval(approval Approval) {
	if c.hooks == nil {
		return
	}
	safego.Go("hooks.notification", func() {
		session, err := c.store.GetSession(c.lifetime, approval.SessionID)
		if err != nil {
			return
		}
		c.hooks.Notify(c.lifetime, hookScope(session, approval.RunID, ""), "permission_prompt", "MatrixClaw needs your permission to use "+approval.ToolName)
	})
}

// preCompact runs the PreCompact hooks of a session before a summary.
func (c *Core) preCompact(ctx context.Context, session Session, trigger string) {
	if c.hooks != nil {
		c.hooks.PreCompact(ctx, hookScope(session, "", ""), trigger)
	}
}

// engineHooks is the Hooks port of a native run; nil without a runner.
func (c *Core) engineHooks(session Session, turn nativeTurn) agent.Hooks {
	if c.hooks == nil {
		return nil
	}
	return coreHooks{runner: c.hooks, scope: hookScope(session, turn.RunID, turn.WorkingDir), subagent: turn.Subagent}
}

// coreHooks runs the hooks of one native run's seams.
type coreHooks struct {
	runner   *hooks.Runner
	scope    hooks.Scope
	subagent bool
}

func (h coreHooks) SessionStart(ctx context.Context, source string) string {
	return h.runner.SessionStart(ctx, h.scope, source)
}

// Stop runs SubagentStop hooks for a child run and Stop hooks otherwise.
func (h coreHooks) Stop(ctx context.Context, reply string, active bool) string {
	event := hooks.Stop
	if h.subagent {
		event = hooks.SubagentStop
	}
	return h.runner.Stop(ctx, h.scope, event, active, reply)
}

func (h coreHooks) PreCompact(ctx context.Context, trigger string) {
	h.runner.PreCompact(ctx, h.scope, trigger)
}

// TrustHooksRequest trusts a session's project hooks file with the SHA256 sum
// the owner was shown.
type TrustHooksRequest struct {
	SHA256 string `json:"sha256"`
}
