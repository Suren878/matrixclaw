package toolview

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// FileChange is what a write, edit, multiedit or apply_patch call does.
type FileChange struct {
	// Paths are the files the input names: one, or each file of a patch.
	Paths []string
	// Edits is how many edits a multiedit asked for.
	Edits int
	// Done is set once the result's metadata describes the change; Files are
	// its changed regions per file (tools.FileChange).
	Done                bool
	Files               []tools.FileChange
	Additions, Removals int
}

// FileChangeOf reads a file-changing call from its input and, once it has run,
// its result metadata; ok is false for other tools and unreadable input.
func FileChangeOf(name string, input string, metadata string) (FileChange, bool) {
	var change FileChange
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write", "edit", "multiedit":
		var params struct {
			FilePath string            `json:"file_path"`
			Edits    []json.RawMessage `json:"edits"`
		}
		if err := json.Unmarshal([]byte(input), &params); err != nil {
			return FileChange{}, false
		}
		change.Paths, change.Edits = []string{params.FilePath}, len(params.Edits)
		var meta tools.FileChange
		if json.Unmarshal([]byte(metadata), &meta) == nil {
			change.Done, change.Files = true, []tools.FileChange{meta}
		}
	case "apply_patch":
		var params tools.ApplyPatchParams
		if err := json.Unmarshal([]byte(input), &params); err != nil {
			return FileChange{}, false
		}
		change.Paths = tools.PatchPaths(params.Input)
		var meta tools.PatchChange
		if json.Unmarshal([]byte(metadata), &meta) == nil && len(meta.Files) > 0 {
			change.Done, change.Files = true, meta.Files
		}
	default:
		return FileChange{}, false
	}
	for _, file := range change.Files {
		change.Additions += file.Additions
		change.Removals += file.Removals
	}
	return change, true
}
