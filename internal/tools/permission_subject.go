package tools

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
)

// SubjectProvider is implemented by executors whose calls permission rules can
// name more narrowly than the whole tool.
type SubjectProvider interface {
	PermissionSubject(call Call) permission.Subject
}

// SubjectsProvider is implemented by executors whose one call reaches several
// subjects, each of which must meet the rules.
type SubjectsProvider interface {
	PermissionSubjects(call Call) []permission.Subject
}

// ReadOnlyCaller is implemented by mutating tools some of whose calls change
// nothing, such as read-only bash command lines.
type ReadOnlyCaller interface {
	ReadsOnly(call Call) bool
}

// Subjects is every subject permission rules match of a call: the subjects of
// a SubjectsProvider, else the one Subject.
func (r *Registry) Subjects(toolID string, call Call) []permission.Subject {
	if r == nil {
		return []permission.Subject{{}}
	}
	r.mu.RLock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	r.mu.RUnlock()
	if provider, provides := registered.executor.(SubjectsProvider); ok && provides {
		return provider.PermissionSubjects(call)
	}
	return []permission.Subject{r.Subject(toolID, call)}
}

// ReadsOnly reports whether a call changes nothing: its tool is read-only, or
// the tool says this call only reads.
func (r *Registry) ReadsOnly(toolID string, call Call) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	r.mu.RUnlock()
	if !ok {
		return false
	}
	if !registered.spec.Mutates() {
		return true
	}
	caller, reads := registered.executor.(ReadOnlyCaller)
	return reads && caller.ReadsOnly(call)
}

// Subject is what permission rules match of a call; the zero Subject when the
// tool names none.
func (r *Registry) Subject(toolID string, call Call) permission.Subject {
	if r == nil {
		return permission.Subject{}
	}
	r.mu.RLock()
	registered, ok := r.executors[normalizeToolID(toolID)]
	r.mu.RUnlock()
	provider, provides := registered.executor.(SubjectProvider)
	if !ok || !provides {
		return permission.Subject{}
	}
	return provider.PermissionSubject(call)
}

func (e *bashExecutor) PermissionSubject(call Call) permission.Subject {
	var params BashParams
	if json.Unmarshal(call.Args, &params) != nil || strings.TrimSpace(params.Command) == "" {
		return permission.Subject{}
	}
	return permission.Subject{Kind: permission.KindCommand, Value: params.Command}
}

// ReadsOnly reports whether the command line passes the check a read-only
// subagent's bash makes.
func (e *bashExecutor) ReadsOnly(call Call) bool {
	return CheckReadOnlyCall(call) == nil
}

func (e *readExecutor) PermissionSubject(call Call) permission.Subject {
	var params ReadParams
	return fileSubject(call, &params, func() string { return params.FilePath })
}

func (e *mutationExecutor) PermissionSubject(call Call) permission.Subject {
	var params struct {
		FilePath string `json:"file_path"`
	}
	return fileSubject(call, &params, func() string { return params.FilePath })
}

func (e *globExecutor) PermissionSubject(call Call) permission.Subject {
	var params GlobParams
	return directorySubject(call, &params, func() string { return params.Path })
}

func (e *grepExecutor) PermissionSubject(call Call) permission.Subject {
	var params GrepParams
	return directorySubject(call, &params, func() string { return params.Path })
}

func (e *lsExecutor) PermissionSubject(call Call) permission.Subject {
	var params LSParams
	return directorySubject(call, &params, func() string { return params.Path })
}

func (e *repoMapExecutor) PermissionSubject(call Call) permission.Subject {
	var params RepoMapParams
	return directorySubject(call, &params, func() string { return params.Path })
}

// fileSubject is the file a call names, as an absolute path with symlinks resolved.
func fileSubject(call Call, params any, path func() string) permission.Subject {
	if json.Unmarshal(call.Args, params) != nil || strings.TrimSpace(path()) == "" {
		return permission.Subject{}
	}
	return pathSubject(permission.KindFile, call.WorkingDir, path())
}

// directorySubject is the directory a search starts from; no path is the working directory.
func directorySubject(call Call, params any, path func() string) permission.Subject {
	if len(call.Args) > 0 && json.Unmarshal(call.Args, params) != nil {
		return permission.Subject{}
	}
	return pathSubject(permission.KindDirectory, call.WorkingDir, path())
}

func pathSubject(kind permission.Kind, workingDir string, value string) permission.Subject {
	policy, err := ResolveFilesystemPath(workingDir, value)
	if err != nil {
		return permission.Subject{}
	}
	return permission.Subject{Kind: kind, Value: policy.RealPath}
}
