// Package workspace reads what an agent run is told about its working
// directory: git state, platform and project instruction files.
package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/textutil"
)

// gitTimeout bounds all git calls of one snapshot.
const gitTimeout = 2 * time.Second

const (
	maxStatusLines = 20
	maxLineRunes   = 200
	recentCommits  = 5
)

// Git is a snapshot of the repository a directory belongs to.
type Git struct {
	Root string
	// Branch describes HEAD, such as "branch main (upstream origin/main, ahead 1)".
	Branch string
	// Status holds porcelain status lines; MoreStatus counts the ones left out.
	Status     []string
	MoreStatus int
	StatusErr  string
	Commits    []string
}

// ReadGit snapshots the repository of dir; false outside a repository. Each
// part that fails or times out is left out rather than failing the snapshot.
func ReadGit(ctx context.Context, dir string) (Git, bool) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(root) == "" {
		return Git{}, false
	}
	snapshot := Git{Root: strings.TrimSpace(root)}
	status, err := git(ctx, dir, "--no-optional-locks", "status", "--porcelain=v1", "--branch")
	if err != nil {
		snapshot.StatusErr = shortError(err)
	} else {
		snapshot.Branch, snapshot.Status, snapshot.MoreStatus = parseStatus(status)
	}
	if log, err := git(ctx, dir, "--no-pager", "log", "--oneline", "--no-decorate", "-n", strconv.Itoa(recentCommits)); err == nil {
		snapshot.Commits = boundedLines(log, recentCommits)
	}
	return snapshot, true
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(out), err
}

func shortError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "git timed out"
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if message := firstLine(string(exit.Stderr)); message != "" {
			return message
		}
	}
	return err.Error()
}

// parseStatus reads `git status --porcelain=v1 --branch`.
func parseStatus(out string) (string, []string, int) {
	var branch string
	var entries []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if header, ok := strings.CutPrefix(line, "## "); ok {
			branch = describeBranch(header)
			continue
		}
		if strings.TrimSpace(line) != "" {
			entries = append(entries, textutil.Clip(line, maxLineRunes, "..."))
		}
	}
	if len(entries) <= maxStatusLines {
		return branch, entries, 0
	}
	return branch, entries[:maxStatusLines], len(entries) - maxStatusLines
}

// describeBranch turns a porcelain branch header such as
// "main...origin/main [ahead 1]" into words.
func describeBranch(header string) string {
	if name, ok := strings.CutPrefix(header, "No commits yet on "); ok {
		return "branch " + name + " (no commits yet)"
	}
	if strings.HasPrefix(header, "HEAD (no branch)") {
		return "detached HEAD"
	}
	head, track, _ := strings.Cut(header, " [")
	track = strings.TrimSuffix(track, "]")
	name, upstream, _ := strings.Cut(head, "...")
	var details []string
	if upstream != "" {
		details = append(details, "upstream "+upstream)
	}
	if track != "" {
		details = append(details, track)
	}
	if len(details) == 0 {
		return "branch " + name
	}
	return "branch " + name + " (" + strings.Join(details, ", ") + ")"
}

func boundedLines(out string, limit int) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" && len(lines) < limit {
			lines = append(lines, textutil.Clip(line, maxLineRunes, "..."))
		}
	}
	return lines
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}

// Platform names the OS and architecture, with the distribution on Linux.
func Platform() string {
	platformOnce.Do(func() {
		platform = runtime.GOOS + "/" + runtime.GOARCH
		if name := osReleaseName(); name != "" {
			platform += " (" + name + ")"
		}
	})
	return platform
}

var (
	platformOnce sync.Once
	platform     string
)

func osReleaseName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}
