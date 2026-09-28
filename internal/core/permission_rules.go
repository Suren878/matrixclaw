package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// SessionPermissionRules lists the rules a session's calls follow: the global
// rules, its own and its parents'.
func (c *Core) SessionPermissionRules(ctx context.Context, sessionID string) ([]permission.Rule, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return nil, err
	}
	return c.permissionRules(ctx, session)
}

// AddPermissionRule saves a rule for the session, or for every session when its
// scope is global. File tool patterns become absolute against the session's
// working directory.
func (c *Core) AddPermissionRule(ctx context.Context, sessionID string, request PermissionRuleRequest) (permission.Rule, error) {
	session, err := c.store.GetSession(ctx, normalizeText(sessionID))
	if err != nil {
		return permission.Rule{}, err
	}
	rule := permission.Rule{
		ID:        c.newID("rule"),
		Tool:      normalizeText(request.Tool),
		Pattern:   normalizeText(request.Pattern),
		Effect:    request.Effect,
		Scope:     request.Scope,
		CreatedAt: c.now().UTC(),
	}
	if !rule.Effect.Valid() || !rule.Scope.Valid() || rule.Tool == "" {
		return permission.Rule{}, fmt.Errorf("%w: a rule needs a tool, an effect (allow, ask, deny) and a scope (session, global)", ErrInvalidInput)
	}
	if rule.Tool != "*" && rule.Tool != "mcp" {
		spec, ok := c.toolSpec(rule.Tool)
		if !ok {
			return permission.Rule{}, fmt.Errorf("%w: unknown tool %q", ErrInvalidInput, rule.Tool)
		}
		rule.Tool = permissionTool(spec)
		if spec.Category == tools.CategoryFilesystem {
			rule.Pattern = absolutePattern(rule.Pattern, session.WorkingDir)
		}
	}
	if rule.Scope == permission.ScopeSession {
		rule.SessionID = session.ID
	}
	if err := c.store.CreatePermissionRule(ctx, rule); err != nil {
		return permission.Rule{}, err
	}
	return rule, nil
}

// DeletePermissionRule removes a rule.
func (c *Core) DeletePermissionRule(ctx context.Context, ruleID string) error {
	return c.store.DeletePermissionRule(ctx, normalizeText(ruleID))
}

func (c *Core) toolSpec(toolID string) (tools.Spec, bool) {
	if c.tools == nil {
		return tools.Spec{}, false
	}
	return c.tools.Spec(toolID)
}

// absolutePattern resolves a path pattern against the working directory: "~/"
// is the home directory, and symlinks leave the part before the first glob.
func absolutePattern(pattern string, workingDir string) string {
	if pattern == "" || pattern == "*" {
		return pattern
	}
	if rest, ok := strings.CutPrefix(pattern, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			pattern = home + "/" + rest
		}
	}
	base, glob := pattern, ""
	if i := strings.IndexAny(pattern, "*?"); i >= 0 {
		slash := strings.LastIndex(pattern[:i], "/")
		switch {
		case slash < 0:
			base, glob = "", pattern
		case slash == 0:
			base, glob = "/", pattern[1:]
		default:
			base, glob = pattern[:slash], pattern[slash+1:]
		}
	}
	policy, err := tools.ResolveFilesystemPath(workingDir, base)
	if err != nil {
		return pattern
	}
	if glob == "" {
		return policy.RealPath
	}
	return strings.TrimSuffix(policy.RealPath, "/") + "/" + filepath.ToSlash(glob)
}
