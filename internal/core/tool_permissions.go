package core

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/hooks"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// callPermission is how the permission rules and the PreToolUse hooks see one
// call and treat it; subjects are all the call reaches (request names the
// first); ask is set when the call waits for approval; root is the working
// directory suggestions stay inside.
type callPermission struct {
	request  permission.Request
	subjects []permission.Subject
	verdict  permission.Verdict
	ask      bool
	granted  bool
	secret   bool
	root     string
	rules    []permission.Rule
	preset   []permission.Rule
	session  Session
	hook     hookVerdict
}

// hookVerdict is what the call's PreToolUse hooks decided: why they block or
// ask, the arguments they rewrote the call to and context for its result.
type hookVerdict struct {
	blocked string
	asked   string
	args    json.RawMessage
	context string
}

// input is the call with the arguments its hooks rewrote it to.
func (check callPermission) input(call tools.Call) tools.Call {
	if check.hook.args != nil {
		call.Args = check.hook.args
	}
	return call
}

// guard lets the call recheck the rules for subjects it reaches later and
// tells a search which paths below its directory the rules leave out.
func (check callPermission) guard(call tools.Call) tools.Call {
	call.Recheck = check.recheck
	call.Skip = permission.SearchSkip(check.request, check.rules)
	return call
}

// refusal is what the model reads for a call a deny rule or a hook blocks.
func (check callPermission) refusal() (string, bool) {
	switch {
	case check.verdict.Effect == permission.Deny && check.verdict.Rule.ID == permission.PlanRuleID:
		return planModeBlocked, true
	case check.verdict.Effect == permission.Deny:
		return "Blocked by rule " + check.verdict.Rule.String(), true
	case check.hook.blocked != "":
		return "Blocked by hook: " + check.hook.blocked, true
	}
	return "", false
}

// recheck refuses a subject the call reaches later, such as a redirect's host,
// that a deny or ask rule catches.
func (check callPermission) recheck(_ context.Context, subject permission.Subject) error {
	request := check.request
	request.Subject = subject
	verdict := permission.Evaluate(request, check.rules, check.preset)
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
	subjects := c.tools.Subjects(spec.ID, call)
	request := permission.Request{Tool: permissionTool(spec), CoveredBy: spec.CoveredBy, Subject: subjects[0], ReadOnly: !spec.Mutates() || c.tools.ReadsOnly(spec.ID, call)}
	root := realPath(cmp.Or(normalizeWorkingDir(session.WorkingDir), normalizeWorkingDir(call.WorkingDir)))
	preset := permission.Preset(string(NormalizePermissionMode(string(session.PermissionMode))), root)
	verdict := permission.EvaluateEach(request, subjects, rules, preset)
	check := callPermission{request: request, subjects: subjects, verdict: verdict, root: root, rules: rules, preset: preset, session: session}
	check.secret = verdict.Effect == "" && spec.Effect == tools.EffectReadOnly && holdsSecrets(request.Subject)
	check.ask = verdict.Effect == permission.Ask || verdict.Effect == "" && (spec.Asks || check.secret)
	if check.ask && call.ToolCallID != "" {
		if check.granted, err = c.callGranted(ctx, sessionID, call.RunID, call.ToolCallID); err != nil {
			return callPermission{}, err
		}
		check.ask = !check.granted
	}
	return check, nil
}

// permitCall is checkPermission with the call's PreToolUse hooks as a rule
// source below the stored deny rules: they run for a call no deny rule blocks
// and may block it, make it ask or allow it. Arguments they rewrite must pass
// the call's checks and meet the rules again.
func (c *Core) permitCall(ctx context.Context, name string, spec tools.Spec, call tools.Call) (callPermission, error) {
	check, err := c.checkPermission(ctx, call.SessionID, spec, call)
	if err != nil || check.verdict.Effect == permission.Deny || c.hooks == nil {
		return check, err
	}
	scope := hookScope(check.session, call.RunID, call.WorkingDir)
	decision := c.hooks.PreToolUse(ctx, scope, hooks.ToolCall{ID: call.ToolCallID, Name: name, Input: call.Args, Files: subjectFiles(check.subjects)})
	if decision.Input != nil {
		call.Args = decision.Input
		if _, _, err := c.checkToolCall(ctx, name, call); err != nil {
			if !errors.Is(err, ErrInvalidInput) {
				return callPermission{}, err
			}
			check.hook.blocked = "its PreToolUse hook rewrote the input to one that fails: " + err.Error()
			return check, nil
		}
		if check, err = c.checkPermission(ctx, call.SessionID, spec, call); err != nil || check.verdict.Effect == permission.Deny {
			return check, err
		}
		check.hook.args = decision.Input
	}
	check.hook.context = decision.Context
	switch decision.Permission {
	case hooks.Deny:
		check.hook.blocked = decision.Reason
	case hooks.Ask:
		if !check.granted && call.ToolCallID != "" {
			if check.granted, err = c.callGranted(ctx, call.SessionID, call.RunID, call.ToolCallID); err != nil {
				return callPermission{}, err
			}
		}
		check.ask, check.hook.asked = !check.granted, cmp.Or(decision.Reason, "a PreToolUse hook")
	case hooks.Allow:
		check.ask = false
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
	return check.suggestion()
}

// suggestion is the rule an "Always allow" answer to the call keeps; nil when
// none can be named, and for exit_plan_mode, whose approval is the user's
// review of one plan.
func (check callPermission) suggestion() *permission.Suggestion {
	if check.request.Tool == exitPlanModeToolName {
		return nil
	}
	suggestion, ok := permission.SuggestEach(check.request, check.subjects, check.root)
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

// holdsSecrets reports whether a file or directory subject is where keys and
// login tokens live, so a read-only call on it asks unless a rule decides.
func holdsSecrets(subject permission.Subject) bool {
	return (subject.Kind == permission.KindFile || subject.Kind == permission.KindDirectory) && tools.HoldsSecrets(subject.Value)
}
