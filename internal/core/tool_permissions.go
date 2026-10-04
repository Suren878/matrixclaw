package core

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// callPermission is how the permission rules see one call and treat it; ask
// is set when the call waits for approval; root is the working directory
// suggestions stay inside.
type callPermission struct {
	request permission.Request
	verdict permission.Verdict
	ask     bool
	secret  bool
	root    string
	rules   []permission.Rule
	preset  []permission.Rule
}

// guard lets the call recheck the rules for subjects it reaches later.
func (check callPermission) guard(call tools.Call) tools.Call {
	call.Recheck = check.recheck
	return call
}

// recheck refuses a subject the call reaches later, such as a redirect's host,
// that a deny or ask rule catches.
func (check callPermission) recheck(_ context.Context, subject permission.Subject) error {
	verdict := permission.Evaluate(permission.Request{Tool: check.request.Tool, Subject: subject}, check.rules, check.preset)
	switch verdict.Effect {
	case permission.Deny:
		return fmt.Errorf("blocked by rule %s", verdict.Rule.String())
	case permission.Ask:
		return fmt.Errorf("rule %s asks first; call the tool with that target directly", verdict.Rule.String())
	}
	return nil
}

// checkPermission evaluates a call against the global rules, the rules of its
// session and the session's parents, then the preset of the session's mode; a
// call that no rule decides asks when its spec does. A call that asks runs once
// its grant is stored. Engine runs and ExecuteTool (API, voice, MCP server,
// replays) both ask here.
func (c *Core) checkPermission(ctx context.Context, sessionID string, spec tools.Spec, call tools.Call) (callPermission, error) {
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return callPermission{}, err
	}
	rules, err := c.permissionRules(ctx, session)
	if err != nil {
		return callPermission{}, err
	}
	if call.WorkingDir == "" {
		call.WorkingDir = session.WorkingDir
	}
	request := permission.Request{Tool: permissionTool(spec), Subject: c.tools.Subject(spec.ID, call)}
	root := realPath(cmp.Or(normalizeWorkingDir(session.WorkingDir), normalizeWorkingDir(call.WorkingDir)))
	preset := permission.Preset(string(NormalizePermissionMode(string(session.PermissionMode))), root)
	verdict := permission.Evaluate(request, rules, preset)
	check := callPermission{request: request, verdict: verdict, root: root, rules: rules, preset: preset}
	check.secret = verdict.Effect == "" && spec.Effect == tools.EffectReadOnly && holdsSecrets(request.Subject)
	check.ask = verdict.Effect == permission.Ask || verdict.Effect == "" && (spec.Asks || check.secret)
	if check.ask && call.ToolCallID != "" {
		granted, err := c.callGranted(ctx, sessionID, call.RunID, call.ToolCallID)
		if err != nil {
			return callPermission{}, err
		}
		check.ask = !granted
	}
	return check, nil
}

// callGranted reports whether the call's newest approval granted it and it has
// no result yet, so a grant runs the call once. Both lookups stay within the
// call's run (run-less calls share run "").
func (c *Core) callGranted(ctx context.Context, sessionID string, runID string, callID string) (bool, error) {
	approvals, err := c.store.ListRunApprovals(ctx, sessionID, runID)
	if err != nil {
		return false, err
	}
	if latest, ok := latestApprovalForCall(approvals, callID); !ok || latest.State != ApprovalStateApproved {
		return false, nil
	}
	done, err := c.store.HasToolResult(ctx, sessionID, runID, callID)
	return !done, err
}

// suggestRule is the rule an "Always allow" answer to a call of toolName keeps;
// nil when none can be named.
func (c *Core) suggestRule(ctx context.Context, sessionID string, toolName string, args json.RawMessage) *permission.Suggestion {
	spec, ok := c.toolSpec(toolName)
	if !ok {
		return nil
	}
	check, err := c.checkPermission(ctx, sessionID, spec, tools.Call{SessionID: sessionID, Args: args})
	if err != nil {
		return nil
	}
	suggestion, ok := permission.Suggest(check.request, check.root)
	if !ok {
		return nil
	}
	return &suggestion
}

// permissionRules are the global rules and the rules of the session and its parents.
func (c *Core) permissionRules(ctx context.Context, session Session) ([]permission.Rule, error) {
	sessionIDs := []string{session.ID}
	for parentID := session.ParentSessionID; parentID != "" && !slices.Contains(sessionIDs, parentID); {
		sessionIDs = append(sessionIDs, parentID)
		parent, err := c.store.GetSession(ctx, parentID)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		parentID = parent.ParentSessionID
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
	return tools.Result{Content: "Blocked by rule " + rule.String(), Status: tools.ResultStatusError}
}

// holdsSecrets reports whether a file or directory subject is where keys and
// login tokens live, so a read-only call on it asks unless a rule decides.
func holdsSecrets(subject permission.Subject) bool {
	return (subject.Kind == permission.KindFile || subject.Kind == permission.KindDirectory) && tools.HoldsSecrets(subject.Value)
}
