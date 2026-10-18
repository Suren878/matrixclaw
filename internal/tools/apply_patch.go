package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/Suren878/matrixclaw/internal/permission"
)

const applyPatchToolName = "apply_patch"

type ApplyPatchParams struct {
	Input string `json:"input"`
}

// PatchChange is what one apply_patch call does, a FileChange per file it
// adds, updates, moves or deletes, as its approval shows it and its result's
// metadata keeps it.
type PatchChange struct {
	Files []FileChange `json:"files"`
}

// PrefersApplyPatch reports whether a model was trained on apply_patch and
// gets it instead of edit and multiedit: GPT models from GPT-5 on and Codex
// models, but not the open-weight gpt-oss.
func PrefersApplyPatch(modelID string) bool {
	id := strings.ToLower(modelID)
	return strings.Contains(id, "codex") || strings.Contains(id, "gpt-") && !strings.Contains(id, "oss") && !strings.Contains(id, "gpt-4")
}

// seekLevels compare a file line with a patch line ever more loosely; a chunk's
// lines are sought at each level in turn, from the chunk's starting line on.
var seekLevels = []struct {
	how  string
	norm func(string) string
}{
	{"", func(line string) string { return line }},
	{"ignoring trailing whitespace", func(line string) string { return strings.TrimRightFunc(line, unicode.IsSpace) }},
	{"ignoring surrounding whitespace", strings.TrimSpace},
	{"ignoring whitespace and quote or dash style", looseLine},
}

// seekLines finds pattern in lines at or after start, at the end of lines when
// eof; how names the level that matched ("" is exact). -1 when not found.
func seekLines(lines []string, pattern []string, start int, eof bool) (int, string) {
	if len(pattern) == 0 {
		return start, ""
	}
	if len(pattern) > len(lines) {
		return -1, ""
	}
	if eof {
		start = len(lines) - len(pattern)
	}
	for _, level := range seekLevels {
		for i := start; i+len(pattern) <= len(lines); i++ {
			if equalBy(lines[i:i+len(pattern)], pattern, level.norm) {
				return i, level.how
			}
		}
	}
	return -1, ""
}

// applyChunks applies an update's chunks to content in file order: an @@
// context line is sought first and the chunk's old lines after it. Old lines
// that end with an empty line are sought again without it. The result ends
// with a line end; CRLF line ends and a byte order mark are kept. Lines that
// matched loosely are replaced at the file's indentation.
func applyChunks(content string, path string, chunks []patchChunk) (string, []string, error) {
	text, ends := splitLineEnds(content)
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	type replacement struct {
		at, n int
		with  []string
	}
	var replacements []replacement
	var notes []string
	next := 0
	for k, chunk := range chunks {
		label := fmt.Sprintf("chunk %d of %s", k+1, path)
		if chunk.hasContext {
			at, how := seekLines(lines, []string{chunk.context}, next, false)
			if at < 0 {
				return "", nil, fmt.Errorf("%s: the @@ line %q is not in the file after the previous chunk", label, chunk.context)
			}
			if how != "" {
				notes = append(notes, "The @@ line of "+label+" matched "+how)
			}
			next = at + 1
		}
		if len(chunk.old) == 0 {
			at := len(lines)
			if chunk.hasContext {
				at = next
			}
			replacements = append(replacements, replacement{at: at, with: chunk.new})
			continue
		}
		old, with := chunk.old, chunk.new
		at, how := seekLines(lines, old, next, chunk.eof)
		if at < 0 && old[len(old)-1] == "" {
			old = old[:len(old)-1]
			if len(with) > 0 && with[len(with)-1] == "" {
				with = with[:len(with)-1]
			}
			at, how = seekLines(lines, old, next, chunk.eof)
		}
		if at < 0 {
			return "", nil, chunkMiss(label, text, lines, chunk, next)
		}
		if how != "" {
			notes = append(notes, strings.ToUpper(label[:1])+label[1:]+" matched "+how)
			if len(with) > 0 {
				with = strings.Split(reindent(strings.Join(with, "\n"), old, lines[at:at+len(old)]), "\n")
			}
		}
		replacements = append(replacements, replacement{at: at, n: len(old), with: with})
		next = at + len(old)
	}
	slices.SortStableFunc(replacements, func(a, b replacement) int { return a.at - b.at })
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		lines = slices.Concat(lines[:r.at], r.with, lines[r.at+r.n:])
	}
	return ends.restore(strings.Join(lines, "\n") + "\n"), notes, nil
}

// chunkMiss explains a chunk whose old lines were not found: they sit before
// an earlier chunk, or the closest lines of the file.
func chunkMiss(label string, text string, lines []string, chunk patchChunk, next int) error {
	if next > 0 {
		if at, _ := seekLines(lines[:min(len(lines), next+len(chunk.old))], chunk.old, 0, false); at >= 0 {
			return fmt.Errorf("%s: its lines are at line %d, before the previous chunk; chunks must follow the file's order", label, at+1)
		}
	}
	return fmt.Errorf("%s: these lines are not in the file:\n%s\n%s", label, strings.Join(chunk.old, "\n"), missHint(text, strings.Join(chunk.old, "\n")))
}

// patchFile is one file a patch touches: as it is on disk and as the patch
// leaves it.
type patchFile struct {
	path            string
	old, next       string
	existed, exists bool
	mode            os.FileMode
	movedFrom       string
	notes           []string
}

func (f *patchFile) changed() bool {
	return f.exists != f.existed || f.next != f.old
}

// patchPlan is a parsed patch checked against the files as they are now; files
// keeps the order the patch first touches them.
type patchPlan struct {
	files  []*patchFile
	byPath map[string]*patchFile
}

type patchExecutor struct {
	files *fileLedger
}

func newApplyPatchExecutor(files *fileLedger) Executor {
	return &patchExecutor{files: files}
}

func (e *patchExecutor) Spec() Spec {
	return coreDefinitionSpec(applyPatchToolName)
}

// PermissionSubjects is every file the patch names, moves included; a patch
// that does not parse names none, so only rules for the whole tool apply.
func (e *patchExecutor) PermissionSubjects(call Call) []permission.Subject {
	hunks, err := patchHunks(call)
	if err != nil {
		return []permission.Subject{{}}
	}
	var subjects []permission.Subject
	for _, hunk := range hunks {
		for _, path := range []string{hunk.path, hunk.moveTo} {
			if path == "" {
				continue
			}
			subject := pathSubject(permission.KindFile, call.WorkingDir, path)
			if !slices.Contains(subjects, subject) {
				subjects = append(subjects, subject)
			}
		}
	}
	return subjects
}

// Preview checks the whole patch against the files as they are now, without
// writing anything.
func (e *patchExecutor) Preview(_ context.Context, call Call) (ApprovalRequest, error) {
	plan, err := e.plan(call)
	if err != nil {
		return ApprovalRequest{}, err
	}
	changed := plan.changed()
	request := ApprovalRequest{Description: "Apply patch: " + patchSummary(changed), Params: patchChange(changed, approvalPreviewMaxBytes, approvalPreviewMaxBytes)}
	if files := listed(changed); len(files) == 1 {
		request.Path = files[0].path
	}
	return request, nil
}

// Execute applies the patch only when every section of it applies; a failed
// write puts back the files written before it.
func (e *patchExecutor) Execute(_ context.Context, call Call) (Result, error) {
	plan, err := e.plan(call)
	if err != nil {
		return failure(err)
	}
	changed := plan.changed()
	if err := commitPatch(changed); err != nil {
		return Result{Content: err.Error(), Status: ResultStatusError}, nil
	}
	for _, f := range changed {
		if f.exists {
			e.files.saw(call.SessionID, f.path, versionOf([]byte(f.next)))
		}
	}
	return Result{Content: patchReport(changed), Metadata: patchChange(changed, 0, resultChangeMaxBytes)}, nil
}

func patchHunks(call Call) ([]patchHunk, error) {
	var params ApplyPatchParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return nil, InvalidArgs(applyPatchToolName, err)
	}
	if strings.TrimSpace(params.Input) == "" {
		return nil, errors.New("input is required: the whole patch, from '" + beginPatchMarker + "' to '" + endPatchMarker + "'")
	}
	return parsePatch(params.Input)
}

// plan parses the call's patch and applies it in memory, section by section,
// to the files as they are now.
func (e *patchExecutor) plan(call Call) (*patchPlan, error) {
	hunks, err := patchHunks(call)
	if err != nil {
		return nil, err
	}
	if len(hunks) == 0 {
		return nil, errors.New("the patch has no sections; nothing to change")
	}
	plan := &patchPlan{byPath: map[string]*patchFile{}}
	for _, hunk := range hunks {
		if err := plan.apply(hunk, call, e.files); err != nil {
			return nil, err
		}
	}
	changed := plan.changed()
	if len(changed) == 0 {
		return nil, unchangedError{reason: "the files already have this content; nothing changed."}
	}
	for _, f := range changed {
		if f.exists && !f.existed {
			if err := checkCreatable(filepath.Dir(f.path)); err != nil {
				return nil, err
			}
		}
	}
	return plan, nil
}

func (p *patchPlan) apply(hunk patchHunk, call Call, ledger *fileLedger) error {
	f, err := p.file(hunk.path, call, ledger)
	if err != nil {
		return err
	}
	switch hunk.kind {
	case hunkAdd:
		f.next, f.exists = hunk.content, true
		return nil
	case hunkDelete:
		if !f.exists {
			return fmt.Errorf("cannot delete %s: file not found", f.path)
		}
		f.next, f.exists = "", false
		return nil
	}
	if !f.exists {
		return fmt.Errorf("cannot update %s: file not found", f.path)
	}
	next, notes, err := applyChunks(f.next, f.path, hunk.chunks)
	if err != nil {
		return err
	}
	target := f
	if hunk.moveTo != "" {
		if target, err = p.file(hunk.moveTo, call, ledger); err != nil {
			return err
		}
		if target != f {
			f.next, f.exists = "", false
			target.movedFrom = f.path
		}
	}
	target.next, target.exists = next, true
	target.notes = append(target.notes, notes...)
	return nil
}

// file is the file at path as the patch has left it so far, read from disk
// the first time: an existing file must be one the session has seen as it is.
func (p *patchPlan) file(path string, call Call, ledger *fileLedger) (*patchFile, error) {
	policy, err := ResolveFilesystemPath(call.WorkingDir, path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}
	real := policy.RealPath
	if f, ok := p.byPath[real]; ok {
		return f, nil
	}
	if isProtectedCredentialPath(real) {
		return nil, errors.New("changing MatrixClaw credential files is blocked; use the setup and provider controls")
	}
	f := &patchFile{path: real, mode: 0o644}
	info, err := os.Stat(real)
	switch {
	case err == nil && info.IsDir():
		return nil, fmt.Errorf("%s is a directory", real)
	case err == nil && !info.Mode().IsRegular():
		return nil, fmt.Errorf("%s is not a regular file", real)
	case err == nil:
		content, err := os.ReadFile(real)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", real, err)
		}
		if err := ledger.check(call.SessionID, real, content); err != nil {
			return nil, err
		}
		f.old, f.next, f.existed, f.exists, f.mode = string(content), string(content), true, true, info.Mode().Perm()
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("inspect %s: %w", real, err)
	}
	p.files = append(p.files, f)
	p.byPath[real] = f
	return f, nil
}

func (p *patchPlan) changed() []*patchFile {
	var out []*patchFile
	for _, f := range p.files {
		if f.changed() {
			out = append(out, f)
		}
	}
	return out
}

// commitPatch writes the changed files in order; when one fails, those
// written before it are put back as they were.
func commitPatch(changed []*patchFile) error {
	for i, f := range changed {
		if err := commitFile(f, f.exists, f.next); err != nil {
			var undo error
			for _, done := range slices.Backward(changed[:i]) {
				undo = errors.Join(undo, commitFile(done, done.existed, done.old))
			}
			if undo != nil {
				return fmt.Errorf("%w; putting back the files written before it failed too: %w", err, undo)
			}
			return fmt.Errorf("%w; no file was changed", err)
		}
	}
	return nil
}

// commitFile makes f exist with content, or not exist.
func commitFile(f *patchFile, exists bool, content string) error {
	if !exists {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", f.path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if err := writeFileAtomic(f.path, []byte(content), f.mode); err != nil {
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	return nil
}

// fileAction names what the patch does to f.
func fileAction(f *patchFile) string {
	switch {
	case f.movedFrom != "":
		return "move " + f.movedFrom + " to " + f.path
	case !f.exists:
		return "delete " + f.path
	case !f.existed:
		return "add " + f.path
	}
	return "update " + f.path
}

// listed is the changed files a summary, preview and result name: the source
// of a move is part of the move.
func listed(changed []*patchFile) []*patchFile {
	var out []*patchFile
	for _, f := range changed {
		if !slices.ContainsFunc(changed, func(g *patchFile) bool { return g.movedFrom == f.path && !f.exists }) {
			out = append(out, f)
		}
	}
	return out
}

// before is the content f's change starts from: its source's for a move.
func before(changed []*patchFile, f *patchFile) string {
	if f.movedFrom == "" {
		return f.old
	}
	for _, g := range changed {
		if g.path == f.movedFrom {
			return g.old
		}
	}
	return f.old
}

func patchSummary(changed []*patchFile) string {
	var actions []string
	for _, f := range listed(changed) {
		actions = append(actions, fileAction(f))
	}
	return strings.Join(actions, ", ")
}

func patchChange(changed []*patchFile, whole int, limit int) PatchChange {
	var change PatchChange
	for _, f := range listed(changed) {
		old := before(changed, f)
		file := newFileChange(f.path, old, f.next, lineDiff(old, f.next), whole, limit)
		file.MovedFrom = f.movedFrom
		change.Files = append(change.Files, file)
	}
	return change
}

// patchReport is the result of an applied patch: what it did to each file,
// how chunks matched when not exactly, and each updated file's changed lines.
func patchReport(changed []*patchFile) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Applied the patch: %s.\n", patchSummary(changed))
	for _, f := range changed {
		for _, note := range f.notes {
			out.WriteString(note + ".\n")
		}
	}
	for _, f := range listed(changed) {
		if !f.exists || !f.existed && f.movedFrom == "" {
			continue
		}
		diff := lineDiff(before(changed, f), f.next)
		additions, removals := diffCounts(diff)
		fmt.Fprintf(&out, "\n%s (+%d -%d lines):\n%s\n", f.path, additions, removals, changeSnippet(diff))
	}
	return strings.TrimSuffix(out.String(), "\n")
}
