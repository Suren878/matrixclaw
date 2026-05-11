package editor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const (
	defaultEditor        = "nano"
	defaultEditorWindows = "notepad"
)

type editorOption func(editor, filename string) (args []string, pathInArgs bool)

func editorAtPosition(line, column int) editorOption {
	line = max(line, 1)
	column = max(column, 1)
	vimLike := []string{"vi", "vim", "nvim"}
	return func(editor, filename string) ([]string, bool) {
		if slices.Contains(vimLike, editor) {
			return []string{fmt.Sprintf("+call cursor(%d,%d)", line, column)}, false
		}
		switch editor {
		case "nano":
			return []string{fmt.Sprintf("+%d,%d", line, column)}, false
		case "emacs", "kak":
			return []string{fmt.Sprintf("+%d:%d", line, column)}, false
		case "gedit":
			return []string{fmt.Sprintf("+%d", line)}, false
		case "code":
			return []string{"--goto", fmt.Sprintf("%s:%d:%d", filename, line, column)}, true
		case "hx":
			return []string{fmt.Sprintf("%s:%d:%d", filename, line, column)}, true
		}
		return nil, false
	}
}

func externalEditorCommand(ctx context.Context, app, path string, options ...editorOption) (*exec.Cmd, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if os.Getenv("SNAP_REVISION") != "" {
		return nil, fmt.Errorf("did you install with Snap? %[1]s is sandboxed and unable to open an editor. Please install %[1]s with Go or another package manager to enable editing", app)
	}

	editor, args := currentEditor()
	editorName := filepath.Base(editor)

	needsToAppendPath := true
	for _, opt := range options {
		optArgs, pathInArgs := opt(editorName, path)
		if pathInArgs {
			needsToAppendPath = false
		}
		args = append(args, optArgs...)
	}
	if needsToAppendPath {
		args = append(args, path)
	}

	return exec.CommandContext(ctx, editor, args...), nil
}

func currentEditor() (string, []string) {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) > 1 {
		return editor[0], editor[1:]
	}
	if len(editor) == 1 {
		return editor[0], []string{}
	}
	switch runtime.GOOS {
	case "windows":
		return defaultEditorWindows, []string{}
	default:
		return defaultEditor, []string{}
	}
}
