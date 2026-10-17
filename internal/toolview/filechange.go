package toolview

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// FileChange is what a write, edit or multiedit call does to its file.
type FileChange struct {
	Path string
	// Edits is how many edits a multiedit asked for.
	Edits int
	// Done is set once the result's metadata describes the change; Old and
	// New are its changed regions (tools.FileChange).
	Done                bool
	Old, New            string
	Additions, Removals int
}

// FileChangeOf reads a file-changing call from its input and, once it has run,
// its result metadata; ok is false for other tools and unreadable input.
func FileChangeOf(name string, input string, metadata string) (FileChange, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write", "edit", "multiedit":
	default:
		return FileChange{}, false
	}
	var params struct {
		FilePath string            `json:"file_path"`
		Edits    []json.RawMessage `json:"edits"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return FileChange{}, false
	}
	change := FileChange{Path: params.FilePath, Edits: len(params.Edits)}
	var meta tools.FileChange
	if json.Unmarshal([]byte(metadata), &meta) != nil {
		return change, true
	}
	change.Done = true
	change.Old, change.New = meta.OldContent, meta.NewContent
	change.Additions, change.Removals = meta.Additions, meta.Removals
	return change, true
}
