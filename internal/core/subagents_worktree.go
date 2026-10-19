package core

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// staleWorktreeAge is how long the worktree of an ended child waits to be
// merged or discarded before the daemon start removes it.
const staleWorktreeAge = 7 * 24 * time.Hour

func normalizeSubagentIsolation(value string) SubagentIsolation {
	if strings.EqualFold(strings.TrimSpace(value), string(SubagentIsolationWorktree)) {
		return SubagentIsolationWorktree
	}
	return SubagentIsolationShared
}

// subagentWorktree is where a worktree child works: the worktree's root, the
// child's directory in it (the parent's place in its repository) and the
// commit its branch starts from.
type subagentWorktree struct {
	root, dir, base string
}

// subagentBranch is the branch of a worktree child's task.
func subagentBranch(taskID string) string {
	return "mc/" + taskID
}

// prepareSubagentWorktree makes a worktree child's worktree on a new branch
// that starts from the parent's files as they are now, uncommitted changes
// included; the parent's index and stash are left alone.
func (c *Core) prepareSubagentWorktree(ctx context.Context, parentDir string, taskID string) (subagentWorktree, error) {
	if c.worktrees == "" {
		return subagentWorktree{}, fmt.Errorf("%w: worktree isolation is not configured", ErrExecutionUnavailable)
	}
	if parentDir = normalizeWorkingDir(parentDir); parentDir == "" {
		return subagentWorktree{}, fmt.Errorf("%w: worktree isolation needs a working directory", ErrInvalidInput)
	}
	top, err := runGit(ctx, gitCall{dir: parentDir}, "rev-parse", "--show-toplevel")
	if err != nil {
		return subagentWorktree{}, fmt.Errorf("%w: worktree isolation needs a git working tree: %w", ErrInvalidInput, err)
	}
	prefix, err := runGit(ctx, gitCall{dir: parentDir}, "rev-parse", "--show-prefix")
	if err != nil {
		return subagentWorktree{}, err
	}
	base, err := c.snapshotWorkingTree(ctx, top, taskID, "matrixclaw: base of "+taskID)
	if err != nil {
		return subagentWorktree{}, fmt.Errorf("%w: snapshot the working tree: %w", ErrExecutionUnavailable, err)
	}
	root := filepath.Join(c.worktrees, subagentWorktreeRepoID(top), stableIDPart(taskID))
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return subagentWorktree{}, err
	}
	if _, err := runGit(ctx, gitCall{dir: top}, "worktree", "add", "-q", "-b", subagentBranch(taskID), root, base); err != nil {
		return subagentWorktree{}, fmt.Errorf("%w: create subagent worktree: %w", ErrExecutionUnavailable, err)
	}
	dir := filepath.Join(root, filepath.FromSlash(prefix))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return subagentWorktree{}, err
	}
	return subagentWorktree{root: root, dir: dir, base: base}, nil
}

// snapshotWorkingTree commits the repository's files as they are now,
// tracked and untracked but not ignored, through a temporary index; the
// commit is on no branch and its parent is HEAD, if any.
func (c *Core) snapshotWorkingTree(ctx context.Context, top string, name string, message string) (string, error) {
	index, release, err := c.temporaryIndex(name)
	if err != nil {
		return "", err
	}
	defer release()
	call := gitCall{dir: top, env: []string{"GIT_INDEX_FILE=" + index}}
	head, headErr := runGit(ctx, call, "rev-parse", "--verify", "-q", "HEAD")
	if err := fillTemporaryIndex(ctx, call, headErr == nil); err != nil {
		return "", err
	}
	tree, err := runGit(ctx, call, "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", message}
	if headErr == nil {
		args = append(args, "-p", head)
	}
	call.env = append(call.env, gitIdentity(ctx, top)...)
	return runGit(ctx, call, args...)
}

// fillTemporaryIndex loads HEAD (when there is one) into the call's index
// and adds every file of the working tree to it.
func fillTemporaryIndex(ctx context.Context, call gitCall, head bool) error {
	readTree := []string{"read-tree", "--empty"}
	if head {
		readTree = []string{"read-tree", "HEAD"}
	}
	if _, err := runGit(ctx, call, readTree...); err != nil {
		return err
	}
	_, err := runGit(ctx, call, "add", "-A")
	return err
}

// temporaryIndex is a path for a git index of its own under the worktrees
// directory and the function that removes it.
func (c *Core) temporaryIndex(name string) (string, func(), error) {
	if err := os.MkdirAll(c.worktrees, 0o755); err != nil {
		return "", nil, err
	}
	path := filepath.Join(c.worktrees, ".index-"+stableIDPart(name)+"-"+c.newID("idx"))
	return path, func() { _ = os.Remove(path) }, nil
}

// gitIdentity names matrixclaw as the author of the commits it makes in a
// repository whose git configuration names no user.
func gitIdentity(ctx context.Context, dir string) []string {
	name, nameErr := runGit(ctx, gitCall{dir: dir}, "config", "user.name")
	email, emailErr := runGit(ctx, gitCall{dir: dir}, "config", "user.email")
	if nameErr == nil && emailErr == nil && name != "" && email != "" {
		return nil
	}
	return []string{"GIT_AUTHOR_NAME=matrixclaw", "GIT_AUTHOR_EMAIL=matrixclaw@localhost", "GIT_COMMITTER_NAME=matrixclaw", "GIT_COMMITTER_EMAIL=matrixclaw@localhost"}
}

// finishSubagentWorktree commits what an ended worktree child left in its
// worktree and describes its branch for the parent; a branch without changes
// is removed with its worktree.
func (c *Core) finishSubagentWorktree(ctx context.Context, task Task) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	branch := subagentBranch(task.ID)
	if err := commitSubagentWork(ctx, task); err != nil {
		return fmt.Sprintf("Worktree: %s\nBranch: %s\nIts changes could not be committed: %v", task.WorkingDir, branch, err)
	}
	stat, files, err := worktreeChanges(ctx, task.WorkingDir, task.WorktreeBase, branch, nil)
	if err != nil {
		return fmt.Sprintf("Worktree: %s\nBranch: %s\nIts changes could not be listed: %v", task.WorkingDir, branch, err)
	}
	if len(files) == 0 {
		if err := c.dropSubagentWorktree(ctx, task); err != nil {
			log.Printf("core: remove the worktree of subagent task %s: %v", task.ID, err)
		}
		return "Worktree: no changes; the worktree and its branch were removed."
	}
	return fmt.Sprintf("Worktree: %s\nBranch: %s (base %s = your working tree when the child started)\nChanged: %s (%s)\nMerge with worktree_merge {\"task_id\":%q} or drop with worktree_discard.",
		task.WorkingDir, branch, shortSHA(task.WorktreeBase), stat, fileList(files, 10), task.ID)
}

// commitSubagentWork commits every change in a child's worktree onto its
// branch, without running the repository's hooks.
func commitSubagentWork(ctx context.Context, task Task) error {
	call := gitCall{dir: task.WorkingDir}
	if _, err := runGit(ctx, call, "add", "-A"); err != nil {
		return err
	}
	if _, err := runGit(ctx, call, "diff", "--cached", "--quiet"); err == nil {
		return nil
	}
	call.env = gitIdentity(ctx, task.WorkingDir)
	_, err := runGit(ctx, call, "commit", "-q", "--no-verify", "-m", cmp.Or(task.Description, "subagent "+task.ID))
	return err
}

// worktreeChanges is the diffstat of base..branch and the files it changes,
// limited to paths (relative to dir) when any are given.
func worktreeChanges(ctx context.Context, dir string, base string, branch string, paths []string) (string, []string, error) {
	pathspec := append([]string{"--"}, paths...)
	stat, err := runGit(ctx, gitCall{dir: dir}, append([]string{"diff", "--shortstat", base, branch}, pathspec...)...)
	if err != nil {
		return "", nil, err
	}
	names, err := runGit(ctx, gitCall{dir: dir}, append([]string{"diff", "--name-only", base, branch}, pathspec...)...)
	if err != nil {
		return "", nil, err
	}
	return stat, strings.Fields(names), nil
}

// keptWorktreeTask is the session's ended worktree child taskID whose
// worktree is still kept.
func (c *Core) keptWorktreeTask(ctx context.Context, sessionID string, taskID string) (Task, error) {
	task, err := c.store.GetTask(ctx, strings.TrimSpace(taskID))
	if errors.Is(err, ErrNotFound) || err == nil && (task.SessionID != sessionID || task.Kind != TaskKindSubagent || task.Isolation != SubagentIsolationWorktree) {
		return Task{}, fmt.Errorf("%w: this session has no worktree subagent task %q", ErrInvalidInput, taskID)
	}
	switch {
	case err != nil:
		return Task{}, err
	case !task.Status.Terminal():
		return Task{}, fmt.Errorf("%w: task %s is still running; await it first", ErrInvalidInput, task.ID)
	case task.WorktreeBase == "":
		return Task{}, fmt.Errorf("%w: task %s has no worktree left: it changed nothing, or it was merged or discarded", ErrInvalidInput, task.ID)
	}
	return task, nil
}

// worktreeMerge is what merging a child's branch did: the diffstat, the
// files left with conflict markers, and whether its worktree is gone.
type worktreeMerge struct {
	Stat      string
	Conflicts []string
	Removed   bool
}

// mergeSubagentWorktree applies what a child's branch changed since its base
// (only paths, relative to workingDir, when any are given) to the parent's
// working tree with a three-way merge through a temporary index, so the
// parent's index stays as it is. A full merge without conflicts removes the
// worktree and branch.
func (c *Core) mergeSubagentWorktree(ctx context.Context, sessionID string, workingDir string, taskID string, paths []string) (worktreeMerge, error) {
	task, err := c.keptWorktreeTask(ctx, sessionID, taskID)
	if err != nil {
		return worktreeMerge{}, err
	}
	top, err := runGit(ctx, gitCall{dir: workingDir}, "rev-parse", "--show-toplevel")
	if err != nil {
		return worktreeMerge{}, fmt.Errorf("%w: %s is not in a git working tree: %w", ErrInvalidInput, workingDir, err)
	}
	parentRepo, childRepo := gitCommonDir(ctx, workingDir), gitCommonDir(ctx, task.WorkingDir)
	if parentRepo == "" || parentRepo != childRepo {
		return worktreeMerge{}, fmt.Errorf("%w: %s is not in the repository of task %s", ErrInvalidInput, workingDir, task.ID)
	}
	branch := subagentBranch(task.ID)
	stat, files, err := worktreeChanges(ctx, workingDir, task.WorktreeBase, branch, paths)
	if err != nil {
		return worktreeMerge{}, err
	}
	if len(files) == 0 {
		return worktreeMerge{}, fmt.Errorf("%w: task %s changed none of those paths", ErrInvalidInput, task.ID)
	}
	patch, err := runGit(ctx, gitCall{dir: workingDir, raw: true}, append([]string{"diff", "--binary", "--full-index", task.WorktreeBase, branch, "--"}, paths...)...)
	if err != nil {
		return worktreeMerge{}, err
	}
	index, release, err := c.temporaryIndex(task.ID + "-merge")
	if err != nil {
		return worktreeMerge{}, err
	}
	defer release()
	call := gitCall{dir: top, env: []string{"GIT_INDEX_FILE=" + index}}
	_, headErr := runGit(ctx, call, "rev-parse", "--verify", "-q", "HEAD")
	if err := fillTemporaryIndex(ctx, call, headErr == nil); err != nil {
		return worktreeMerge{}, err
	}
	call.stdin = patch
	merge := worktreeMerge{Stat: stat}
	if _, err := runGit(ctx, call, "apply", "--3way", "--whitespace=nowarn"); err != nil {
		if merge.Conflicts = applyConflicts(err.Error()); len(merge.Conflicts) == 0 {
			return worktreeMerge{}, fmt.Errorf("%w: the changes of task %s do not apply: %w", ErrInvalidInput, task.ID, err)
		}
		return merge, nil
	}
	if len(paths) == 0 {
		if err := c.dropSubagentWorktree(ctx, task); err != nil {
			return worktreeMerge{}, err
		}
		merge.Removed = true
	}
	return merge, nil
}

// applyConflicts lists the files git apply --3way left with conflicts.
func applyConflicts(output string) []string {
	var files []string
	for _, line := range strings.Split(output, "\n") {
		if file, ok := strings.CutPrefix(strings.TrimSpace(line), "U "); ok {
			files = append(files, file)
		}
	}
	return files
}

// discardSubagentWorktree removes an ended child's worktree and branch.
func (c *Core) discardSubagentWorktree(ctx context.Context, sessionID string, taskID string) (Task, error) {
	task, err := c.keptWorktreeTask(ctx, sessionID, taskID)
	if err != nil {
		return Task{}, err
	}
	return task, c.dropSubagentWorktree(ctx, task)
}

// dropSubagentWorktree removes a child's worktree and branch and records
// that they are gone.
func (c *Core) dropSubagentWorktree(ctx context.Context, task Task) error {
	if err := c.removeWorktreeAndBranch(ctx, task.SessionID, task.WorkingDir, subagentBranch(task.ID)); err != nil {
		return err
	}
	return c.store.ClearTaskWorktree(ctx, task.ID, c.now().UTC())
}

// removeSubagentWorktree removes the worktree of a subagent that never
// started; a failure is only logged.
func (c *Core) removeSubagentWorktree(task Task) {
	if task.WorktreeBase == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.removeWorktreeAndBranch(ctx, task.SessionID, task.WorkingDir, subagentBranch(task.ID)); err != nil {
		log.Printf("core: remove the worktree of a subagent that did not start: %v", err)
	}
}

// removeWorktreeAndBranch removes a worktree and its branch; the repository
// is found from the worktree, or from the parent session's directory when
// the worktree is gone already.
func (c *Core) removeWorktreeAndBranch(ctx context.Context, sessionID string, root string, branch string) error {
	repo := gitCommonDir(ctx, root)
	if repo == "" {
		if parent, err := c.store.GetSession(ctx, sessionID); err == nil {
			repo = gitCommonDir(ctx, parent.WorkingDir)
		}
	}
	if root != "" {
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		// The repository's directory of worktrees goes with its last one.
		_ = os.Remove(filepath.Dir(root))
	}
	if repo == "" {
		return nil
	}
	call := gitCall{dir: repo}
	if _, err := runGit(ctx, call, "worktree", "prune"); err != nil {
		return err
	}
	if _, err := runGit(ctx, call, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err != nil {
		return nil
	}
	_, err := runGit(ctx, call, "branch", "-q", "-D", branch)
	return err
}

// pruneSubagentWorktrees removes, at daemon start, the worktrees no task
// keeps: those of unknown tasks, of tasks that dropped them, and of tasks
// that ended more than staleWorktreeAge ago; git forgets them too.
func (c *Core) pruneSubagentWorktrees(ctx context.Context) error {
	if c.worktrees == "" {
		return nil
	}
	repos, err := os.ReadDir(c.worktrees)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	cutoff := c.now().Add(-staleWorktreeAge)
	for _, repo := range repos {
		path := filepath.Join(c.worktrees, repo.Name())
		if !repo.IsDir() {
			if strings.HasPrefix(repo.Name(), ".index-") {
				_ = os.Remove(path)
			}
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range entries {
			root := filepath.Join(path, entry.Name())
			task, err := c.store.GetTask(ctx, entry.Name())
			switch {
			case err == nil && task.WorktreeBase != "" && (!task.Status.Terminal() || task.FinishedAt == nil || task.FinishedAt.After(cutoff)):
				continue
			case err == nil && task.WorktreeBase != "":
				err = c.dropSubagentWorktree(ctx, task)
			case err == nil || errors.Is(err, ErrNotFound):
				err = c.removeWorktreeAndBranch(ctx, task.SessionID, root, subagentBranch(entry.Name()))
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("worktree %s: %w", root, err))
			}
		}
		_ = os.Remove(path) // only when empty
	}
	return errors.Join(errs...)
}

func subagentWorktreeRepoID(repoRoot string) string {
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	sum := sha256.Sum256([]byte(repoRoot))
	return stableIDPart(filepath.Base(repoRoot)) + "-" + hex.EncodeToString(sum[:6])
}

// gitCommonDir is the absolute git directory the repository of dir shares
// with its worktrees; "" when dir is in none.
func gitCommonDir(ctx context.Context, dir string) string {
	if dir == "" {
		return ""
	}
	common, err := runGit(ctx, gitCall{dir: dir}, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return filepath.Clean(common)
}

// gitCall is where a git command runs, with extra environment and stdin;
// raw keeps its output as it is instead of trimmed.
type gitCall struct {
	dir   string
	env   []string
	stdin string
	raw   bool
}

// runGit runs git in call.dir and returns its output; an error carries
// what git printed.
func runGit(ctx context.Context, call gitCall, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", call.dir}, args...)...)
	if len(call.env) > 0 {
		cmd.Env = append(os.Environ(), call.env...)
	}
	if call.stdin != "" {
		cmd.Stdin = strings.NewReader(call.stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if message := strings.TrimSpace(stderr.String() + "\n" + stdout.String()); message != "" {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, message)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	if call.raw {
		return stdout.String(), nil
	}
	return strings.TrimSpace(stdout.String()), nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// fileList names up to limit files, then how many more there are.
func fileList(files []string, limit int) string {
	if len(files) <= limit {
		return strings.Join(files, ", ")
	}
	return strings.Join(files[:limit], ", ") + fmt.Sprintf(", and %d more", len(files)-limit)
}
