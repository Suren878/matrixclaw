package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// callPermission is how the permission rules see one call and treat it.
type callPermission struct {
	request permission.Request
	verdict permission.Verdict
}

// checkPermission evaluates a call against the global rules, the rules of its
// session and the session's parents, then the preset of the session's mode.
// Engine runs and ExecuteTool (API, voice, MCP server, replays) both ask here.
func (c *Core) checkPermission(ctx context.Context, sessionID string, spec tools.Spec, call tools.Call) (callPermission, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return callPermission{}, err
	}
	rules, err := c.permissionRules(ctx, session)
	if err != nil {
		return callPermission{}, err
	}
	request := permission.Request{Tool: permissionTool(spec), Subject: c.tools.Subject(spec.ID, call)}
	root := firstNonEmpty(normalizeWorkingDir(session.WorkingDir), normalizeWorkingDir(call.WorkingDir))
	preset := permission.Preset(string(NormalizePermissionMode(string(session.PermissionMode))), realPath(root))
	return callPermission{request: request, verdict: permission.Evaluate(request, rules, preset)}, nil
}

// permissionRules are the global rules and the rules of the session and its parents.
func (c *Core) permissionRules(ctx context.Context, session Session) ([]permission.Rule, error) {
	sessionIDs := []string{session.ID}
	for parentID := normalizeText(session.ParentSessionID); parentID != "" && !slices.Contains(sessionIDs, parentID); {
		sessionIDs = append(sessionIDs, parentID)
		parent, err := c.store.GetSession(ctx, parentID)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		parentID = normalizeText(parent.ParentSessionID)
	}
	return c.store.ListPermissionRules(ctx, sessionIDs)
}

// permissionTool is the tool name rules are written for: every MCP tool is "mcp"
// (its subject names server and tool) and multiedit shares the rules of edit.
func permissionTool(spec tools.Spec) string {
	switch {
	case strings.HasPrefix(spec.Namespace, "mcp."):
		return "mcp"
	case spec.ID == "multiedit":
		return "edit"
	default:
		return spec.ID
	}
}

// realPath is path with symlinks resolved, as rules compare paths.
func realPath(path string) string {
	if path == "" {
		return ""
	}
	policy, err := tools.ResolveFilesystemPath(path, "")
	if err != nil {
		return path
	}
	return policy.RealPath
}

// blockedResult is what the model reads for a call a deny rule blocks.
func blockedResult(rule permission.Rule) tools.Result {
	return tools.Result{Content: "Blocked by rule " + rule.String(), Status: tools.ResultStatusError, IsError: true}
}

// askedByRule requests approval for a call an ask rule catches in a tool that
// does not ask on its own.
func askedByRule(prepared preparedToolCall, input ExecuteToolInput, rule permission.Rule) tools.Result {
	return tools.Result{
		Content: "Approval required",
		Approval: &tools.ApprovalRequest{
			ToolID:      prepared.ToolName,
			ToolCallID:  prepared.ToolCallID,
			Action:      "ask_rule",
			Description: fmt.Sprintf("Rule %q asks before this call", rule.String()),
			Params:      input.Args,
		},
	}
}
