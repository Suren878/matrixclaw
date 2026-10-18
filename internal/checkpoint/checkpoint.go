// Package checkpoint snapshots working directories into shadow git
// repositories kept apart from the user's own repository, so file changes can
// be reviewed and undone. It never touches the user's index or history.
package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxFiles is the most files a directory may hold to be snapshotted.
const MaxFiles = 50000

// refPrefix holds one ref per kept snapshot, named <unix seconds>-<tree>.
const refPrefix = "refs/matrixclaw/"

// refGrace keeps a ref this young through Prune: its row may not be saved yet.
const refGrace = time.Hour

// gcInterval spaces the garbage collections of one shadow repository.
const gcInterval = time.Hour

// ErrUnsupported is returned for a directory that is not snapshotted: the
// home directory, a directory above it, the file system root, or one holding
// more than MaxFiles files.
var ErrUnsupported = errors.New("checkpoints are not taken in this directory")

// ErrMissing is returned for a snapshot the shadow repository no longer holds.
var ErrMissing = errors.New("the checkpoint is no longer available")

// Change is one file a diff changes, by path relative to the working directory.
type Change struct {
	Path    string `json:"path"`
	Status  Status `json:"status"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary,omitempty"`
}

// Status says how a diff changes a file.
type Status string

const (
	StatusAdded    Status = "added"
	StatusModified Status = "modified"
	StatusDeleted  Status = "deleted"
)

// Diff is what changed between two states of a working directory.
type Diff struct {
	Changes []Change `json:"changes"`
	Patch   string   `json:"patch,omitempty"`
}

// Store keeps one shadow repository per work tree under its root.
type Store struct {
	root     string
	home     string
	maxFiles int
	now      func() time.Time

	mu    sync.Mutex
	repos map[string]*repo
}

// repo serialises the git commands on one shadow repository.
type repo struct {
	mu     sync.Mutex
	lastGC time.Time
}

// New keeps shadow repositories under root.
func New(root string) *Store {
	home, _ := os.UserHomeDir()
	return &Store{root: root, home: home, maxFiles: MaxFiles, now: time.Now, repos: map[string]*repo{}}
}

// Snapshot records the current state of dir and returns its tree ID.
func (s *Store) Snapshot(ctx context.Context, dir string) (string, error) {
	w, unlock, err := s.open(ctx, dir)
	if err != nil {
		return "", err
	}
	defer unlock()
	tree, err := w.writeTree(ctx, s.maxFiles)
	if err != nil {
		return "", err
	}
	ref := refPrefix + strconv.FormatInt(s.now().Unix(), 10) + "-" + tree
	if _, err := w.git(ctx, nil, "update-ref", ref, tree); err != nil {
		return "", err
	}
	return tree, nil
}

// Diff compares two snapshots of dir; an empty from or to is its current
// state. Paths, relative to dir, limit the diff.
func (s *Store) Diff(ctx context.Context, dir, from, to string, paths ...string) (Diff, error) {
	w, unlock, err := s.open(ctx, dir)
	if err != nil {
		return Diff{}, err
	}
	defer unlock()
	if from == "" || to == "" {
		current, err := w.writeTree(ctx, s.maxFiles)
		if err != nil {
			return Diff{}, err
		}
		from, to = cmp(from, current), cmp(to, current)
	}
	pathspec, err := w.pathspec(paths)
	if err != nil {
		return Diff{}, err
	}
	changes, err := w.changes(ctx, from, to, pathspec)
	if err != nil || len(changes) == 0 {
		return Diff{Changes: changes}, err
	}
	patch, err := w.git(ctx, nil, append([]string{"diff-tree", "-r", "-p", "--no-renames", "--no-color", w.relative(), from, to, "--"}, pathspec...)...)
	if err != nil {
		return Diff{}, w.missing(err)
	}
	return Diff{Changes: changes, Patch: string(patch)}, nil
}

// Compare reports, for each tree, what restoring it would change in dir now.
func (s *Store) Compare(ctx context.Context, dir string, trees []string) ([][]Change, error) {
	w, unlock, err := s.open(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := w.writeTree(ctx, s.maxFiles)
	if err != nil {
		return nil, err
	}
	out := make([][]Change, len(trees))
	for i, tree := range trees {
		if out[i], err = w.changes(ctx, current, tree, w.pathspecAll()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Restore brings dir back to tree: files changed or deleted since are written
// back, files added since are deleted. Ignored files and nested repositories
// are left alone. It returns what it changed.
func (s *Store) Restore(ctx context.Context, dir, tree string) ([]Change, error) {
	w, unlock, err := s.open(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := w.writeTree(ctx, s.maxFiles)
	if err != nil {
		return nil, err
	}
	changes, err := w.changes(ctx, current, tree, w.pathspecAll())
	if err != nil {
		return nil, err
	}
	// Deletions go first, so a file and a directory may swap places.
	var checkout []string
	for _, change := range changes {
		if change.Status != StatusDeleted {
			checkout = append(checkout, w.topPath(change.Path))
			continue
		}
		if err := w.remove(change.Path); err != nil {
			return nil, err
		}
	}
	if len(checkout) > 0 {
		stdin := []byte(strings.Join(checkout, "\x00"))
		if _, err := w.git(ctx, stdin, "checkout", tree, "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// Prune drops the refs of the snapshots of dir that keep does not name, and
// repeated refs of one tree, and at most once per gcInterval collects the
// objects no ref reaches.
func (s *Store) Prune(ctx context.Context, dir string, keep []string) error {
	w, unlock, err := s.open(ctx, dir)
	if err != nil {
		return err
	}
	defer unlock()
	out, err := w.git(ctx, nil, "for-each-ref", "--format=%(refname)", refPrefix)
	if err != nil {
		return err
	}
	var drop strings.Builder
	seen := map[string]bool{}
	refs := strings.Fields(string(out))
	slices.Sort(refs)
	for _, ref := range slices.Backward(refs) {
		stamp, tree, ok := strings.Cut(strings.TrimPrefix(ref, refPrefix), "-")
		seconds, _ := strconv.ParseInt(stamp, 10, 64)
		young := s.now().Sub(time.Unix(seconds, 0)) < refGrace
		if !ok || (!seen[tree] && (young || slices.Contains(keep, tree))) {
			seen[tree] = ok
			continue
		}
		fmt.Fprintf(&drop, "delete %s\n", ref)
	}
	if drop.Len() > 0 {
		if _, err := w.git(ctx, []byte(drop.String()), "update-ref", "--stdin"); err != nil {
			return err
		}
	}
	if s.now().Sub(w.repo.lastGC) < gcInterval {
		return nil
	}
	w.repo.lastGC = s.now()
	_, err = w.git(ctx, nil, "gc", "--prune=now", "--quiet")
	return err
}

func cmp(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// workspace is a working directory inside the work tree of its shadow
// repository: the enclosing git repository's top level, or dir itself.
type workspace struct {
	dir    string
	top    string
	scope  string // dir relative to top, slash-separated; "." when equal
	gitDir string
	repo   *repo
}

// open resolves dir, refuses an unsupported one, creates its shadow
// repository once and locks it until unlock.
func (s *Store) open(ctx context.Context, dir string) (*workspace, func(), error) {
	dir, err := resolveDir(dir)
	if err != nil {
		return nil, nil, err
	}
	if reason := s.refusal(dir); reason != "" {
		return nil, nil, fmt.Errorf("%w: %s %s", ErrUnsupported, dir, reason)
	}
	top, exclude := enclosingRepository(ctx, dir)
	scope, err := filepath.Rel(top, dir)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256([]byte(top))
	w := &workspace{dir: dir, top: top, scope: filepath.ToSlash(scope), gitDir: filepath.Join(s.root, hex.EncodeToString(sum[:8]))}
	s.mu.Lock()
	w.repo = s.repos[w.gitDir]
	if w.repo == nil {
		w.repo = &repo{}
		s.repos[w.gitDir] = w.repo
	}
	s.mu.Unlock()
	w.repo.mu.Lock()
	unlock := w.repo.mu.Unlock
	if err := w.init(ctx, exclude); err != nil {
		unlock()
		return nil, nil, err
	}
	// Only this process uses the shadow repository, so a lock file left
	// behind is from a command that was killed.
	_ = os.Remove(filepath.Join(w.gitDir, "index.lock"))
	return w, unlock, nil
}

func resolveDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("%w: no working directory", ErrUnsupported)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s is not a directory", ErrUnsupported, resolved)
	}
	return resolved, nil
}

// refusal says why dir is not snapshotted, or "" when it is: the file system
// root, the home directory or one above it, and the home directory's
// Desktop, Documents and Downloads hold too much that is not a project.
func (s *Store) refusal(dir string) string {
	if filepath.Dir(dir) == dir {
		return "is the file system root"
	}
	if s.home == "" {
		return ""
	}
	home := s.home
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	if dir == home || strings.HasPrefix(home, dir+string(filepath.Separator)) {
		return "is the home directory or above it"
	}
	for _, name := range []string{"Desktop", "Documents", "Downloads"} {
		if dir == filepath.Join(home, name) {
			return "is a personal folder"
		}
	}
	return ""
}

// enclosingRepository returns the top level of the git repository holding
// dir and the path of its info/exclude file; dir itself and "" outside one.
func enclosingRepository(ctx context.Context, dir string) (top, exclude string) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel", "--git-path", "info/exclude")
	cmd.Dir = dir
	cmd.Env = cleanEnv()
	out, err := cmd.Output()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err != nil || len(lines) != 2 {
		return dir, ""
	}
	top, err = filepath.EvalSymlinks(lines[0])
	if err != nil {
		return dir, ""
	}
	exclude = lines[1]
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(dir, exclude)
	}
	return top, exclude
}

// shadowConfig keeps snapshots fast on large trees and byte-exact: no line
// end conversion, no file system monitor, no automatic gc or reflogs.
var shadowConfig = [][2]string{
	{"core.autocrlf", "false"},
	{"core.fsmonitor", "false"},
	{"core.untrackedCache", "true"},
	{"core.logAllRefUpdates", "false"},
	{"core.quotePath", "false"},
	{"feature.manyFiles", "true"},
	{"index.version", "4"},
	{"gc.auto", "0"},
}

// shadowAttributes override the project's .gitattributes, so files are
// stored and written back as they are, without filters or conversions.
const shadowAttributes = "* -text !eol -filter -ident -working-tree-encoding\n"

func (w *workspace) init(ctx context.Context, exclude string) error {
	// info/attributes is written last, so it marks a finished init.
	if _, err := os.Stat(filepath.Join(w.gitDir, "info", "attributes")); err == nil {
		return nil
	}
	if err := os.MkdirAll(w.gitDir, 0o700); err != nil {
		return err
	}
	if _, err := w.git(ctx, nil, "init", "--quiet"); err != nil {
		return err
	}
	config := shadowConfig
	if exclude != "" {
		config = append(slices.Clone(config), [2]string{"core.excludesFile", exclude})
	}
	for _, kv := range config {
		if _, err := w.git(ctx, nil, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(w.gitDir, "info", "attributes"), []byte(shadowAttributes), 0o600)
}

// writeTree stages the scope and writes its tree. Nested repositories are
// left out: their files are theirs to keep.
func (w *workspace) writeTree(ctx context.Context, maxFiles int) (string, error) {
	out, err := w.git(ctx, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", w.scope)
	if err != nil {
		return "", err
	}
	var nested []string
	files := 0
	for _, entry := range strings.Split(string(out), "\x00") {
		switch {
		case entry == "":
		case strings.HasSuffix(entry, "/"):
			nested = append(nested, strings.TrimSuffix(entry, "/"))
		default:
			files++
		}
	}
	if files > maxFiles {
		return "", fmt.Errorf("%w: %s has more than %d files", ErrUnsupported, w.dir, maxFiles)
	}
	if len(nested) > 0 {
		if err := w.excludeNested(ctx, nested); err != nil {
			return "", err
		}
	}
	if _, err := w.git(ctx, nil, "add", "--all", "--", w.scope); err != nil {
		return "", err
	}
	tree, err := w.git(ctx, nil, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(tree)), nil
}

// excludeNested keeps the nested repositories found so far out of the shadow
// index: listed in info/exclude and dropped from the index if a directory
// became a repository after it was staged.
func (w *workspace) excludeNested(ctx context.Context, nested []string) error {
	file := filepath.Join(w.gitDir, "info", "exclude")
	existing, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := strings.Split(string(existing), "\n")
	added := false
	for _, dir := range nested {
		pattern := "/" + dir + "/"
		if !slices.Contains(lines, pattern) {
			lines = append(lines, pattern)
			added = true
		}
	}
	if !added {
		return nil
	}
	if err := os.WriteFile(file, []byte(strings.TrimLeft(strings.Join(lines, "\n"), "\n")+"\n"), 0o600); err != nil {
		return err
	}
	_, err = w.git(ctx, nil, append([]string{"rm", "-r", "--cached", "--quiet", "--ignore-unmatch", "--"}, nested...)...)
	return err
}

// changes lists what differs from tree from to tree to inside the scope.
func (w *workspace) changes(ctx context.Context, from, to string, pathspec []string) ([]Change, error) {
	raw, err := w.git(ctx, nil, append([]string{"diff-tree", "-r", "-z", "--no-renames", "--raw", w.relative(), from, to, "--"}, pathspec...)...)
	if err != nil {
		return nil, w.missing(err)
	}
	numstat, err := w.git(ctx, nil, append([]string{"diff-tree", "-r", "-z", "--no-renames", "--numstat", w.relative(), from, to, "--"}, pathspec...)...)
	if err != nil {
		return nil, w.missing(err)
	}
	var changes []Change
	fields := strings.Split(string(raw), "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		meta := strings.Fields(fields[i])
		if len(meta) == 0 {
			continue
		}
		change := Change{Path: fields[i+1], Status: StatusModified}
		switch meta[len(meta)-1] {
		case "A":
			change.Status = StatusAdded
		case "D":
			change.Status = StatusDeleted
		}
		changes = append(changes, change)
	}
	counts := map[string][2]int{}
	binary := map[string]bool{}
	for _, record := range strings.Split(string(numstat), "\x00") {
		parts := strings.SplitN(record, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "-" {
			binary[parts[2]] = true
			continue
		}
		added, _ := strconv.Atoi(parts[0])
		deleted, _ := strconv.Atoi(parts[1])
		counts[parts[2]] = [2]int{added, deleted}
	}
	for i := range changes {
		c := counts[changes[i].Path]
		changes[i].Added, changes[i].Deleted, changes[i].Binary = c[0], c[1], binary[changes[i].Path]
	}
	return changes, nil
}

// relative limits a diff to the scope and names files relative to it.
func (w *workspace) relative() string {
	if w.scope == "." {
		return "--relative"
	}
	return "--relative=" + w.scope + "/"
}

func (w *workspace) pathspecAll() []string {
	return []string{w.scope}
}

// pathspec turns paths relative to dir, or absolute inside it, into
// pathspecs relative to the work tree.
func (w *workspace) pathspec(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return w.pathspecAll(), nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if filepath.IsAbs(p) {
			rel, err := filepath.Rel(w.dir, filepath.Clean(p))
			if err != nil {
				return nil, err
			}
			p = rel
		}
		p = path.Clean(filepath.ToSlash(p))
		if p == ".." || strings.HasPrefix(p, "../") {
			return nil, fmt.Errorf("%s is outside the working directory %s", p, w.dir)
		}
		out = append(out, w.topPath(p))
	}
	return out, nil
}

// topPath turns a path relative to dir into one relative to the work tree.
func (w *workspace) topPath(p string) string {
	return path.Join(w.scope, p)
}

// remove deletes a file added since a snapshot and the directories it leaves
// empty, up to the working directory.
func (w *workspace) remove(p string) error {
	file := filepath.Join(w.dir, filepath.FromSlash(p))
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for parent := filepath.Dir(file); parent != w.dir && strings.HasPrefix(parent, w.dir); parent = filepath.Dir(parent) {
		if os.Remove(parent) != nil {
			break
		}
	}
	return nil
}

// missing turns git's error for an absent object into ErrMissing.
func (w *workspace) missing(err error) error {
	if strings.Contains(err.Error(), "bad object") || strings.Contains(err.Error(), "not a tree object") {
		return fmt.Errorf("%w: %w", ErrMissing, err)
	}
	return err
}

// git runs a git command on the shadow repository with the work tree as its
// directory, isolated from the user's git configuration.
func (w *workspace) git(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = w.top
	cmd.Env = append(cleanEnv(),
		"GIT_DIR="+w.gitDir,
		"GIT_WORK_TREE="+w.top,
		"GIT_LITERAL_PATHSPECS=1",
	)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("checkpoint: git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// cleanEnv is the daemon's environment without GIT_ variables, and without
// the user's global and system git configuration (filters, hooks, excludes).
func cleanEnv() []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}
