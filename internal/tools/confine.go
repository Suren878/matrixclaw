package tools

import (
	"encoding/json"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/permission"
)

// CheckConfinedCall refuses a call of an agent confined to root (a child's
// git worktree) that would work outside it: a working directory outside root,
// a call that changes files naming a file or directory outside it, or a bash
// line with a path argument outside it (/dev/null aside). Reads elsewhere
// stay allowed. It guards against mistakes, not a sandbox: variables and redirections are
// not followed.
func CheckConfinedCall(spec Spec, call Call, subjects []permission.Subject, changes bool, root string) error {
	root = comparableFilesystemPath(root)
	outside := func(path string) error {
		return fmt.Errorf("%s is outside your worktree %s; work only inside it", path, root)
	}
	if !pathWithin(root, comparableFilesystemPath(call.WorkingDir)) {
		return outside(call.WorkingDir)
	}
	if spec.ID == bashToolName {
		var params BashParams
		if err := json.Unmarshal(call.Args, &params); err != nil {
			return InvalidArgs(bashToolName, err)
		}
		dir := resolvedPath(call.WorkingDir, params.WorkingDir)
		if !pathWithin(root, comparableFilesystemPath(dir)) {
			return outside(dir)
		}
		return commandPaths(params.Command, dir, func(word, resolved string) error {
			if resolved == "/dev/null" || pathWithin(root, resolved) {
				return nil
			}
			return outside(word)
		})
	}
	if !changes {
		return nil
	}
	for _, subject := range subjects {
		if (subject.Kind == permission.KindFile || subject.Kind == permission.KindDirectory) && !pathWithin(root, comparableFilesystemPath(subject.Value)) {
			return outside(subject.Value)
		}
	}
	return nil
}
