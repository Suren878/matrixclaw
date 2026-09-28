package tools

import (
	"path/filepath"
	"strings"
)

// ConcurrencyKeyProvider is implemented by executors whose calls need another
// concurrency key than their spec's default.
type ConcurrencyKeyProvider interface {
	ConcurrencyKey(call Call) string
}

// ConcurrencyKey names what a call must not share with a concurrent call of the
// same key: "" runs freely (read-only tools), an MCP tool shares its server, a
// mutating filesystem or shell tool its working directory and any other
// mutating tool its namespace.
func (s Spec) ConcurrencyKey(call Call) string {
	namespace := strings.ToLower(strings.TrimSpace(s.Namespace))
	switch {
	case strings.HasPrefix(namespace, "mcp."):
		return namespace
	case !s.Mutates():
		return ""
	}
	switch normalizeCategory(s.Category) {
	case CategoryFilesystem, CategoryShell:
		return WorkingDirKey(call.WorkingDir)
	}
	return "tool:" + namespace
}

// WorkingDirKey is the concurrency key of calls that change dir.
func WorkingDirKey(dir string) string {
	return "dir:" + filepath.Clean(dir)
}

// ConcurrencyKey is the key a call of the tool runs under; "" for an unknown tool.
func (r *Registry) ConcurrencyKey(toolID string, call Call) string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	r.mu.RUnlock()
	if !ok {
		return ""
	}
	if provider, provides := registered.executor.(ConcurrencyKeyProvider); provides {
		return provider.ConcurrencyKey(call)
	}
	return registered.spec.ConcurrencyKey(call)
}
