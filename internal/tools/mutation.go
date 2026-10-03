package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

const (
	writeToolName     = "write"
	editToolName      = "edit"
	multiEditToolName = "multiedit"
)

type WriteParams struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type EditParams struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type MultiEditParams struct {
	FilePath string          `json:"file_path"`
	Edits    []EditOperation `json:"edits"`
}

type EditOperation struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// FileChange is what write, edit and multiedit do to one file: the result's
// metadata and, cut to approvalPreviewMaxBytes, what their approval shows.
type FileChange struct {
	Path       string `json:"file_path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
	Additions  int    `json:"additions"`
	Removals   int    `json:"removals"`
}

// fileMutation is one call of a file tool: write may create the file (and its
// directory); apply turns the old content into the new one.
type fileMutation struct {
	path   string
	create bool
	apply  func(old string) (string, error)
}

// mutationExecutor is write, edit or multiedit; plan reads a call's arguments.
type mutationExecutor struct {
	tool string
	plan func(args json.RawMessage) (file string, m fileMutation, err error)
	// describe is the approval's description, done the result's content.
	describe func(path string) string
	done     func(path string, args json.RawMessage) string
}

func NewWriteExecutor() Executor {
	return &mutationExecutor{
		tool: writeToolName,
		plan: func(args json.RawMessage) (string, fileMutation, error) {
			var params WriteParams
			err := json.Unmarshal(args, &params)
			return params.FilePath, fileMutation{create: true, apply: func(string) (string, error) { return params.Content, nil }}, err
		},
		describe: func(path string) string { return "Create or replace " + path },
		done:     func(path string, _ json.RawMessage) string { return "File written: " + path },
	}
}

func NewEditExecutor() Executor {
	return &mutationExecutor{
		tool: editToolName,
		plan: func(args json.RawMessage) (string, fileMutation, error) {
			var params EditParams
			err := json.Unmarshal(args, &params)
			edits := []EditOperation{{OldString: params.OldString, NewString: params.NewString, ReplaceAll: params.ReplaceAll}}
			return params.FilePath, fileMutation{apply: func(old string) (string, error) { return applyEdits(old, edits) }}, err
		},
		describe: func(path string) string { return "Edit " + path },
		done:     func(path string, _ json.RawMessage) string { return "File edited: " + path },
	}
}

func NewMultiEditExecutor() Executor {
	return &mutationExecutor{
		tool: multiEditToolName,
		plan: func(args json.RawMessage) (string, fileMutation, error) {
			var params MultiEditParams
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fileMutation{}, err
			}
			if len(params.Edits) == 0 {
				return params.FilePath, fileMutation{}, errors.New("edits is required")
			}
			return params.FilePath, fileMutation{apply: func(old string) (string, error) { return applyEdits(old, params.Edits) }}, nil
		},
		describe: func(path string) string { return "Apply multiple edits to " + path },
		done: func(path string, args json.RawMessage) string {
			var params MultiEditParams
			_ = json.Unmarshal(args, &params)
			return fmt.Sprintf("Applied %d edits to %s", len(params.Edits), path)
		},
	}
}

func (e *mutationExecutor) Spec() Spec {
	return coreDefinitionSpec(e.tool)
}

func (e *mutationExecutor) Execute(_ context.Context, call Call) (Result, error) {
	m, err := e.mutation(call)
	if err != nil {
		return failure(err)
	}
	if !call.Approved {
		change, err := mutateFile(m, false)
		if err != nil {
			return failure(err)
		}
		return approvalResult(e.tool, e.tool, change.Path, e.describe(change.Path), change.preview()), nil
	}
	change, err := mutateFile(m, true)
	if err != nil {
		return failure(err)
	}
	return Result{Content: e.done(change.Path, call.Args), Metadata: change}, nil
}

// mutation plans a call against its file, resolved in the call's working directory.
func (e *mutationExecutor) mutation(call Call) (fileMutation, error) {
	file, m, err := e.plan(call.Args)
	var syntax *json.SyntaxError
	var mistyped *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax) || errors.As(err, &mistyped):
		return fileMutation{}, InvalidArgs(e.tool, err)
	case strings.TrimSpace(file) == "":
		return fileMutation{}, errors.New("file_path is required")
	case err != nil:
		return fileMutation{}, err
	}
	policy, err := ResolveFilesystemPath(call.WorkingDir, file)
	if err != nil {
		return fileMutation{}, fmt.Errorf("invalid path: %w", err)
	}
	m.path = policy.Path
	return m, nil
}

// failure answers a call whose arguments or file do not allow the change; bad
// JSON goes to the registry, which explains the schema.
func failure(err error) (Result, error) {
	if errors.Is(err, ErrInvalidArgs) {
		return Result{}, err
	}
	return Result{Content: err.Error(), Status: ResultStatusError}, nil
}

// mutateFile reads m's file and applies m to it; commit also writes the result.
func mutateFile(m fileMutation, commit bool) (FileChange, error) {
	old, err := os.ReadFile(m.path)
	switch {
	case err == nil, m.create && os.IsNotExist(err):
	case os.IsNotExist(err):
		return FileChange{}, fmt.Errorf("file not found: %s", m.path)
	default:
		return FileChange{}, fmt.Errorf("read %s: %w", m.path, err)
	}
	next, err := m.apply(string(old))
	if err != nil {
		return FileChange{}, err
	}
	change := FileChange{Path: m.path, OldContent: string(old), NewContent: next}
	change.Additions, change.Removals = diffCounts(change.OldContent, next)
	if !commit {
		return change, nil
	}
	if m.create {
		if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
			return FileChange{}, fmt.Errorf("create dir: %w", err)
		}
	}
	if err := ensureMutationWriteTarget(m.path, m.create); err != nil {
		return FileChange{}, fmt.Errorf("validate target: %w", err)
	}
	if err := os.WriteFile(m.path, []byte(next), 0o644); err != nil {
		return FileChange{}, fmt.Errorf("write %s: %w", m.path, err)
	}
	return change, nil
}

// preview is the change as an approval shows it, each side cut to approvalPreviewMaxBytes.
func (c FileChange) preview() FileChange {
	c.OldContent, c.NewContent = cutPreview(c.OldContent), cutPreview(c.NewContent)
	return c
}

// applyEdits applies edits in order; any that does not apply fails them all.
func applyEdits(content string, edits []EditOperation) (string, error) {
	for i, edit := range edits {
		next, err := applyEdit(content, edit)
		if err != nil && len(edits) > 1 {
			return "", fmt.Errorf("edit %d: %w", i+1, err)
		}
		if err != nil {
			return "", err
		}
		content = next
	}
	return content, nil
}

func applyEdit(content string, edit EditOperation) (string, error) {
	switch count := strings.Count(content, edit.OldString); {
	case edit.OldString == "":
		return edit.NewString, nil
	case count == 0:
		return "", errors.New("old_string not found in file")
	case edit.ReplaceAll:
		return strings.ReplaceAll(content, edit.OldString, edit.NewString), nil
	case count > 1:
		return "", errors.New("old_string appears multiple times in the file; set replace_all to true or provide more context")
	}
	return strings.Replace(content, edit.OldString, edit.NewString, 1), nil
}

// diffCounts counts the lines a unified diff of the change adds and removes.
func diffCounts(oldContent string, newContent string) (int, int) {
	oldContent = strings.ReplaceAll(oldContent, "\r\n", "\n")
	newContent = strings.ReplaceAll(newContent, "\r\n", "\n")
	unified, err := udiff.ToUnifiedDiff("a", "b", oldContent, udiff.Lines(oldContent, newContent), 0)
	if err != nil {
		return 0, 0
	}
	additions, removals := 0, 0
	for _, hunk := range unified.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case udiff.Insert:
				additions++
			case udiff.Delete:
				removals++
			}
		}
	}
	return additions, removals
}
