package tools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/glob"
)

const gitListTimeout = 10 * time.Second

// heavyDirs are skipped by a walk outside a git repository, where no
// .gitignore says what is build output or dependencies; .git always is.
var heavyDirs = map[string]bool{
	".hg": true, ".svn": true, "node_modules": true, "bower_components": true,
	"vendor": true, "target": true, "dist": true, "build": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, ".gradle": true, ".next": true, ".cache": true,
}

var errStopWalk = errors.New("stop walk")

// searchScope is what one glob, grep or ls call may see below its root: it
// leaves out the paths the permission rules guard (Call.Skip) and places that
// hold keys, unless the root itself is one.
type searchScope struct {
	root     string
	realRoot string
	skip     func(string) bool
	secrets  bool
	dirs     map[string]bool
	// left counts the entries left out; scopes below the root share it.
	left *int
}

func newSearchScope(policy FilesystemPathPolicy, call Call) *searchScope {
	return &searchScope{
		root:     policy.Path,
		realRoot: policy.RealPath,
		skip:     call.Skip,
		secrets:  HoldsSecrets(policy.RealPath),
		dirs:     map[string]bool{},
		left:     new(int),
	}
}

// below is the scope of a directory under the root, given as a slash path.
func (s *searchScope) below(rel string) *searchScope {
	return &searchScope{
		root:     filepath.Join(s.root, filepath.FromSlash(rel)),
		realRoot: s.real(rel),
		skip:     s.skip,
		secrets:  s.secrets,
		dirs:     map[string]bool{},
		left:     s.left,
	}
}

// real is the resolved path of rel, a slash path below the root.
func (s *searchScope) real(rel string) string {
	return filepath.Join(s.realRoot, filepath.FromSlash(rel))
}

// drops reports whether the scope leaves out the entry at rel; only rel itself
// is checked, not its parents.
func (s *searchScope) drops(rel string) bool {
	real := s.real(rel)
	if s.skip != nil && s.skip(real) || !s.secrets && hiddenElement(rel) && HoldsSecrets(real) {
		*s.left++
		return true
	}
	return false
}

// dropsPath is drops for rel and every directory between the root and it.
func (s *searchScope) dropsPath(rel string) bool {
	for i := range len(rel) {
		if rel[i] != '/' {
			continue
		}
		dir := rel[:i]
		dropped, seen := s.dirs[dir]
		if !seen {
			dropped = s.drops(dir)
			s.dirs[dir] = dropped
		}
		if dropped {
			return true
		}
	}
	return s.drops(rel)
}

// leftNote tells the model that entries were left out, or is empty.
func (s *searchScope) leftNote() string {
	if *s.left == 0 {
		return ""
	}
	return fmt.Sprintf("\n(%d entries left out by permission rules or as places that hold keys; name one in path to be asked.)", *s.left)
}

func hiddenElement(rel string) bool {
	return strings.HasPrefix(rel, ".") || strings.Contains(rel, "/.")
}

// searchFiles lists the files below the scope's root that match keeps, as
// slash paths relative to it. Inside a git repository git lists them (tracked
// and untracked, not ignored); otherwise, or when git lists none (the root may
// be ignored itself), a walk that follows .gitignore files does, skipping
// directories descend refuses.
func searchFiles(ctx context.Context, scope *searchScope, descend func(rel string) bool, keeps func(rel string) bool) ([]string, error) {
	if listed, ok := gitFiles(ctx, scope.root); ok {
		files := listed[:0]
		for _, rel := range listed {
			if keeps(rel) && !scope.dropsPath(rel) {
				files = append(files, rel)
			}
		}
		return files, nil
	}
	var files []string
	err := walkTree(ctx, scope, func(rel string, entry fs.DirEntry) (bool, bool) {
		if entry.IsDir() {
			return false, descend(rel)
		}
		return keeps(rel), false
	}, func(rel string, _ fs.DirEntry) error {
		files = append(files, rel)
		return nil
	})
	return files, err
}

// gitFiles lists the files git sees below root, relative to it; ok is false
// outside a repository, without git, or when it lists nothing.
func gitFiles(ctx context.Context, root string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, gitListTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, false
	}
	var files []string
	for name := range strings.SplitSeq(string(out), "\x00") {
		// A trailing slash is a nested repository; a repeat is a conflicted file.
		if name == "" || strings.HasSuffix(name, "/") || len(files) > 0 && files[len(files)-1] == name {
			continue
		}
		files = append(files, name)
	}
	return files, len(files) > 0
}

// walkTree walks the entries below the scope's root breadth first, in name
// order, leaving out what .gitignore files ignore and heavy directories
// outside a repository. accept says whether an entry is wanted and whether to
// enter it; the scope then checks it, and visit takes a wanted entry.
// Unreadable directories below the root are passed over.
func walkTree(ctx context.Context, scope *searchScope, accept func(rel string, entry fs.DirEntry) (keep, enter bool), visit func(rel string, entry fs.DirEntry) error) error {
	type pending struct {
		rel    string
		ignore glob.Ignore
	}
	ignore, inRepo := repoIgnore(scope.realRoot)
	queue := []pending{{ignore: ignore}}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(filepath.Join(scope.root, filepath.FromSlash(dir.rel)))
		if err != nil {
			if dir.rel == "" {
				return err
			}
			continue
		}
		ignore := dir.ignore
		realDir := scope.real(dir.rel)
		if content, err := os.ReadFile(filepath.Join(realDir, ".gitignore")); err == nil {
			ignore = ignore.Add(filepath.ToSlash(realDir), content)
		}
		for _, entry := range entries {
			rel := path.Join(dir.rel, entry.Name())
			isDir := entry.IsDir()
			if isDir && (entry.Name() == ".git" || !inRepo && heavyDirs[entry.Name()]) ||
				ignore.Ignored(filepath.ToSlash(scope.real(rel)), isDir) {
				continue
			}
			keep, enter := accept(rel, entry)
			if !keep && !enter || scope.drops(rel) {
				continue
			}
			if keep {
				if err := visit(rel, entry); err != nil {
					return err
				}
			}
			if enter && isDir {
				queue = append(queue, pending{rel: rel, ignore: ignore})
			}
		}
	}
	return nil
}

// repoIgnore is the ignore rules dir inherits from the git repository holding
// it: info/exclude and the .gitignore files above dir. inRepo is false when no
// repository holds dir.
func repoIgnore(dir string) (glob.Ignore, bool) {
	top := dir
	for {
		if _, err := os.Lstat(filepath.Join(top, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(top)
		if parent == top {
			return glob.Ignore{}, false
		}
		top = parent
	}
	var ignore glob.Ignore
	if content, err := os.ReadFile(filepath.Join(top, ".git", "info", "exclude")); err == nil {
		ignore = ignore.Add(filepath.ToSlash(top), content)
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil || rel == "." {
		return ignore, true
	}
	current := top
	for name := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if content, err := os.ReadFile(filepath.Join(current, ".gitignore")); err == nil {
			ignore = ignore.Add(filepath.ToSlash(current), content)
		}
		current = filepath.Join(current, name)
	}
	return ignore, true
}

// foundFile is a file a search found, by its slash path below the root.
type foundFile struct {
	rel  string
	info fs.FileInfo
}

// statRecent stats files (without following links) and orders them most
// recently modified first, then by path; paths that are gone or are
// directories (git lists submodules) are dropped.
func statRecent(root string, rels []string) []foundFile {
	found := make([]foundFile, 0, len(rels))
	for _, rel := range rels {
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && !info.IsDir() {
			found = append(found, foundFile{rel: rel, info: info})
		}
	}
	slices.SortFunc(found, func(a, b foundFile) int {
		return cmp.Or(b.info.ModTime().Compare(a.info.ModTime()), strings.Compare(a.rel, b.rel))
	})
	return found
}
