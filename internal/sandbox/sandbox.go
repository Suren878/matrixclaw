// Package sandbox confines the agent's shell commands: what they may write and
// whether they reach the network (docs/SANDBOX.md).
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Mode is how much of the machine a command may change.
type Mode string

const (
	Off Mode = "off"
	// WorkspaceWrite reads everything and writes only the workspace, the
	// temporary directories and the package caches.
	WorkspaceWrite Mode = "workspace_write"
	// ReadOnly writes only the temporary directories and the package caches,
	// which read-only tools such as go vet need.
	ReadOnly Mode = "read_only"
)

// DefaultCaches are the package caches confined commands may write unless
// Settings.Caches replaces them.
var DefaultCaches = []string{
	"~/.cache",
	"~/Library/Caches",
	"~/go/pkg/mod",
	"~/.npm",
	"~/.local/share/pnpm/store",
	"~/.yarn/berry/cache",
	"~/.cargo/registry",
	"~/.cargo/git",
	"~/.m2/repository",
	"~/.gradle/caches",
}

// Settings is setup.json's daemon.sandbox.
type Settings struct {
	// Mode confines every native command; empty confines unattended ones only
	// (full_auto sessions, plan mode and read-only subagents).
	Mode Mode `json:"mode,omitempty"`
	// Network false cuts confined commands off the network.
	Network *bool `json:"network,omitempty"`
	// Writable are more directories workspace_write commands may write.
	Writable []string `json:"writable,omitempty"`
	// Caches replaces DefaultCaches when set; an empty list allows none.
	Caches *[]string `json:"caches,omitempty"`
}

// Validate rejects an unknown mode and paths that are neither absolute nor
// start with ~/.
func (s Settings) Validate() error {
	var problems []error
	switch s.Mode {
	case "", Off, WorkspaceWrite, ReadOnly:
	default:
		problems = append(problems, fmt.Errorf("mode is %q, want one of %q", s.Mode, []Mode{Off, WorkspaceWrite, ReadOnly}))
	}
	paths := s.Writable
	if s.Caches != nil {
		paths = append(slices.Clone(paths), *s.Caches...)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) && !strings.HasPrefix(path, "~/") {
			problems = append(problems, fmt.Errorf("path %q is neither absolute nor under ~/", path))
		}
	}
	return errors.Join(problems...)
}

// Run is what a command runs for.
type Run struct {
	// WorkingDir is the session's working directory.
	WorkingDir string
	// Unattended is a full_auto session: nobody approves its commands.
	Unattended bool
	// ReadOnly is plan mode or a read-only subagent.
	ReadOnly bool
	// Env is the command's environment, read for TMPDIR.
	Env []string
}

// Policy is how one command is confined; the zero Policy leaves it alone.
type Policy struct {
	Mode Mode
	// Workspace are the project directories a workspace_write command may
	// write: the working directory and its git directories.
	Workspace []string
	// Scratch are the other writable directories: temporary ones, package
	// caches and, for workspace_write, Settings.Writable.
	Scratch   []string
	NoNetwork bool
	// Strict refuses to run a command this system cannot confine.
	Strict bool
}

// Policy is how s confines a command of r.
func (s Settings) Policy(r Run) Policy {
	var p Policy
	switch {
	case s.Mode == Off:
		return Policy{}
	case r.ReadOnly:
		p.Mode = ReadOnly
	case s.Mode == "" && !r.Unattended:
		return Policy{}
	default:
		p.Mode = s.Mode
		if p.Mode == "" {
			p.Mode = WorkspaceWrite
		}
	}
	p.Strict = s.Mode != ""
	p.NoNetwork = s.Network != nil && !*s.Network
	home, _ := os.UserHomeDir()
	caches := DefaultCaches
	if s.Caches != nil {
		caches = *s.Caches
	}
	scratch := []string{"/tmp", "/var/tmp", envValue(r.Env, "TMPDIR")}
	for _, path := range caches {
		scratch = append(scratch, expandHome(path, home))
	}
	if p.Mode == WorkspaceWrite {
		p.Workspace = existing(WorkspaceRoots(r.WorkingDir, home), "")
		for _, path := range s.Writable {
			scratch = append(scratch, expandHome(path, home))
		}
	}
	p.Scratch = existing(scratch, home)
	return p
}

// WorkspaceRoots are the directories a workspace_write command run for dir
// may write: dir itself unless it holds home, and the git directories of the
// repository dir is in, so that commits work in a worktree too.
func WorkspaceRoots(dir, home string) []string {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil
	}
	var roots []string
	if home == "" || !within(home, dir) {
		roots = append(roots, dir)
	}
	return append(roots, gitDirs(dir)...)
}

// gitDirs are the git directory of the repository holding dir and, for a
// worktree, the main repository's one, which holds its objects and refs.
func gitDirs(dir string) []string {
	for d := dir; ; d = filepath.Dir(d) {
		dotGit := filepath.Join(d, ".git")
		info, err := os.Stat(dotGit)
		if err == nil && info.IsDir() {
			return []string{dotGit}
		}
		if err == nil {
			return linkedGitDirs(dotGit)
		}
		if parent := filepath.Dir(d); parent == d {
			return nil
		}
	}
}

// linkedGitDirs reads a worktree's .git file ("gitdir: <path>") and the
// commondir file in the directory it names.
func linkedGitDirs(dotGit string) []string {
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return nil
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return nil
	}
	gitDir = absoluteFrom(filepath.Dir(dotGit), strings.TrimSpace(gitDir))
	dirs := []string{gitDir}
	if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		dirs = append(dirs, absoluteFrom(gitDir, strings.TrimSpace(string(common))))
	}
	return dirs
}

func absoluteFrom(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

// within reports whether path is dir or below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func expandHome(path, home string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home == "" {
			return ""
		}
		return filepath.Join(home, rest)
	}
	return path
}

func envValue(env []string, name string) string {
	for _, pair := range slices.Backward(env) {
		if value, ok := strings.CutPrefix(pair, name+"="); ok {
			return value
		}
	}
	return ""
}

// existing resolves the symlinks of the absolute paths that exist and drops
// the others and duplicates. With home set, a directory missing below an
// existing one other than home and / is created first, so that the tool
// whose cache it is can fill it.
func existing(paths []string, home string) []string {
	var out []string
	for _, path := range paths {
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		if parent := filepath.Dir(path); home != "" && parent != home && parent != filepath.Dir(parent) {
			if _, err := os.Stat(parent); err == nil {
				_ = os.Mkdir(path, 0o755)
			}
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || slices.Contains(out, resolved) {
			continue
		}
		out = append(out, resolved)
	}
	return out
}

// Writable are all the directories p lets a command write.
func (p Policy) Writable() []string {
	return append(slices.Clone(p.Workspace), p.Scratch...)
}

// Describe says in a sentence what p lets a command do; empty for the zero Policy.
func (p Policy) Describe() string {
	var text string
	switch p.Mode {
	case WorkspaceWrite:
		where := "the temporary directories and package caches"
		if len(p.Workspace) > 0 {
			where = strings.Join(p.Workspace, ", ") + ", " + where
		}
		text = "workspace_write: commands read everything but write only in " + where
	case ReadOnly:
		text = "read_only: commands read everything but write only in the temporary directories and package caches"
	default:
		return ""
	}
	if p.NoNetwork {
		text += ", without network access"
	}
	return text
}

// deniedMarks are what tools print when the kernel refuses a write or, with
// the network cut, a connection.
var deniedMarks = []string{"permission denied", "operation not permitted", "read-only file system"}

var networkMarks = []string{"network is unreachable", "could not resolve host", "temporary failure in name resolution", "name or service not known"}

// DenialHint explains a failed command's output that reads like p refused
// it; empty when it does not.
func (p Policy) DenialHint(output string) string {
	if p.Mode != WorkspaceWrite && p.Mode != ReadOnly {
		return ""
	}
	lower := strings.ToLower(output)
	denied := slices.ContainsFunc(deniedMarks, func(mark string) bool { return strings.Contains(lower, mark) })
	if !denied && p.NoNetwork {
		denied = slices.ContainsFunc(networkMarks, func(mark string) bool { return strings.Contains(lower, mark) })
	}
	if !denied {
		return ""
	}
	advice := "Use a path inside the project or /tmp, or ask the user to run the command themselves."
	if p.Mode == ReadOnly {
		advice = "This run may not change files; report what needs changing instead."
	}
	return "This command ran in the shell sandbox (" + p.Describe() + "). A refused write fails with \"Permission denied\" or \"Read-only file system\". " + advice
}
