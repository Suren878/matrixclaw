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

// unchangedError is a call that would leave its file as it is; it answers the
// call as neutral rather than failed.
type unchangedError struct{ reason string }

func (e unchangedError) Error() string { return e.reason }

// fileMutation is one call of a file tool on path, a real path: write may
// create the file and its directory; apply turns the old content into the new
// one and notes how its edits matched.
type fileMutation struct {
	path   string
	create bool
	apply  func(old string) (string, []string, error)
}

// mutationOutcome is a change of one file, planned or done.
type mutationOutcome struct {
	path      string
	old, next string
	existed   bool
	notes     []string
	diff      udiff.UnifiedDiff
}

// mutationExecutor is write, edit or multiedit; plan reads a call's arguments.
type mutationExecutor struct {
	tool  string
	files *fileLedger
	plan  func(args json.RawMessage) (file string, m fileMutation, err error)
	// describe is the approval's description, report the result's content.
	describe func(path string) string
	report   func(o mutationOutcome) string
}

func newWriteExecutor(files *fileLedger) Executor {
	return &mutationExecutor{
		tool:  writeToolName,
		files: files,
		plan: func(args json.RawMessage) (string, fileMutation, error) {
			var params WriteParams
			err := json.Unmarshal(args, &params)
			return params.FilePath, fileMutation{create: true, apply: func(string) (string, []string, error) { return params.Content, nil, nil }}, err
		},
		describe: func(path string) string { return "Create or replace " + path },
		report: func(o mutationOutcome) string {
			if !o.existed {
				return fmt.Sprintf("Created %s (%d lines).", o.path, countLines(o.next))
			}
			additions, removals := diffCounts(o.diff)
			return fmt.Sprintf("Overwrote %s (+%d -%d lines).", o.path, additions, removals)
		},
	}
}

func newEditExecutor(files *fileLedger) Executor {
	return &mutationExecutor{
		tool:  editToolName,
		files: files,
		plan: func(args json.RawMessage) (string, fileMutation, error) {
			var params EditParams
			err := json.Unmarshal(args, &params)
			edits := []EditOperation{{OldString: params.OldString, NewString: params.NewString, ReplaceAll: params.ReplaceAll}}
			return params.FilePath, fileMutation{apply: func(old string) (string, []string, error) { return applyEdits(old, edits) }}, err
		},
		describe: func(path string) string { return "Edit " + path },
		report:   func(o mutationOutcome) string { return editReport("Edited "+o.path, o) },
	}
}

func newMultiEditExecutor(files *fileLedger) Executor {
	return &mutationExecutor{
		tool:  multiEditToolName,
		files: files,
		plan: func(args json.RawMessage) (string, fileMutation, error) {
			var params MultiEditParams
			if err := json.Unmarshal(args, &params); err != nil {
				return "", fileMutation{}, err
			}
			if len(params.Edits) == 0 {
				return params.FilePath, fileMutation{}, errors.New("edits is required")
			}
			return params.FilePath, fileMutation{apply: func(old string) (string, []string, error) { return applyEdits(old, params.Edits) }}, nil
		},
		describe: func(path string) string { return "Apply multiple edits to " + path },
		report:   func(o mutationOutcome) string { return editReport("Edited "+o.path+" (multiedit)", o) },
	}
}

// editReport is an edit's result: what changed, how edits matched when not
// byte for byte, and the changed lines as they read now.
func editReport(title string, o mutationOutcome) string {
	additions, removals := diffCounts(o.diff)
	var out strings.Builder
	fmt.Fprintf(&out, "%s (+%d -%d lines).\n", title, additions, removals)
	for _, note := range o.notes {
		out.WriteString(note + ".\n")
	}
	out.WriteString(changeSnippet(o.diff))
	return out.String()
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
}

func (e *mutationExecutor) Spec() Spec {
	return coreDefinitionSpec(e.tool)
}

// Preview applies the call to the file as it is now, without writing it.
func (e *mutationExecutor) Preview(_ context.Context, call Call) (ApprovalRequest, error) {
	o, err := e.run(call, false)
	if err != nil {
		return ApprovalRequest{}, err
	}
	change := newFileChange(o.path, o.old, o.next, o.diff, approvalPreviewMaxBytes, approvalPreviewMaxBytes)
	return ApprovalRequest{Description: e.describe(o.path), Path: o.path, Params: change}, nil
}

func (e *mutationExecutor) Execute(_ context.Context, call Call) (Result, error) {
	o, err := e.run(call, true)
	if err != nil {
		return failure(err)
	}
	return Result{Content: e.report(o), Metadata: newFileChange(o.path, o.old, o.next, o.diff, 0, resultChangeMaxBytes)}, nil
}

// failure answers a call whose arguments or file do not allow the change; bad
// JSON goes to the registry, which explains the schema.
func failure(err error) (Result, error) {
	var unchanged unchangedError
	switch {
	case errors.Is(err, ErrInvalidArgs):
		return Result{}, err
	case errors.As(err, &unchanged):
		return Result{Content: unchanged.reason, Status: ResultStatusNeutral}, nil
	}
	return Result{Content: err.Error(), Status: ResultStatusError}, nil
}

// run plans the call against its file and, with commit, writes the result.
func (e *mutationExecutor) run(call Call, commit bool) (mutationOutcome, error) {
	m, err := e.mutation(call)
	if err != nil {
		return mutationOutcome{}, err
	}
	return mutateFile(m, call.SessionID, e.files, commit)
}

// mutation reads a call's arguments and resolves its file, symlinks included,
// in the call's working directory.
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
	if isProtectedCredentialPath(policy.RealPath) {
		return fileMutation{}, errors.New("changing MatrixClaw credential files is blocked; use the setup and provider controls")
	}
	m.path = policy.RealPath
	return m, nil
}

// mutateFile applies m to its file as it is now. An existing file must be one
// the session has seen as it is (files); commit writes the result atomically
// and records it as seen.
func mutateFile(m fileMutation, session string, files *fileLedger, commit bool) (mutationOutcome, error) {
	info, err := os.Stat(m.path)
	var old []byte
	switch {
	case err == nil && info.IsDir():
		return mutationOutcome{}, fmt.Errorf("%s is a directory", m.path)
	case err == nil && !info.Mode().IsRegular():
		return mutationOutcome{}, fmt.Errorf("%s is not a regular file", m.path)
	case err == nil:
		if old, err = os.ReadFile(m.path); err != nil {
			return mutationOutcome{}, fmt.Errorf("read %s: %w", m.path, err)
		}
		if err := files.check(session, m.path, old); err != nil {
			return mutationOutcome{}, err
		}
	case errors.Is(err, os.ErrNotExist) && m.create:
		if err := checkCreatable(filepath.Dir(m.path)); err != nil {
			return mutationOutcome{}, err
		}
	case errors.Is(err, os.ErrNotExist):
		return mutationOutcome{}, fmt.Errorf("file not found: %s", m.path)
	default:
		return mutationOutcome{}, fmt.Errorf("inspect %s: %w", m.path, err)
	}
	existed := info != nil
	next, notes, err := m.apply(string(old))
	if err != nil {
		return mutationOutcome{}, err
	}
	if existed && next == string(old) {
		return mutationOutcome{}, unchangedError{reason: m.path + " already has this content; nothing changed."}
	}
	o := mutationOutcome{path: m.path, old: string(old), next: next, existed: existed, notes: notes, diff: lineDiff(string(old), next)}
	if !commit {
		return o, nil
	}
	mode := os.FileMode(0o644)
	if existed {
		mode = info.Mode().Perm()
	} else if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return mutationOutcome{}, fmt.Errorf("create dir: %w", err)
	}
	if err := writeFileAtomic(m.path, []byte(next), mode); err != nil {
		return mutationOutcome{}, fmt.Errorf("write %s: %w", m.path, err)
	}
	files.saw(session, m.path, versionOf([]byte(next)))
	return o, nil
}

// checkCreatable reports why a file cannot be created in dir: its nearest
// existing ancestor must be a directory.
func checkCreatable(dir string) error {
	for {
		info, err := os.Stat(dir)
		switch {
		case err == nil && !info.IsDir():
			return fmt.Errorf("%s is not a directory", dir)
		case err == nil:
			return nil
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("inspect %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// writeFileAtomic replaces path with content through a temporary file in the
// same directory, so a reader never sees a half-written file.
func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := tmp.Write(content)
	if err := errors.Join(writeErr, tmp.Chmod(mode), tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
