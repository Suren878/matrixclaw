package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/glob"
)

const (
	grepToolName         = "grep"
	grepDefaultLimit     = 100
	grepMaxLimit         = 1000
	grepMaxContext       = 10
	maxRenderedLineWidth = 500
	binarySniffBytes     = 8 * 1024
)

// Output modes of grep.
const (
	grepContent          = "content"
	grepFilesWithMatches = "files_with_matches"
	grepCount            = "count"
)

type GrepParams struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path,omitempty"`
	Include         string `json:"include,omitempty"`
	LiteralText     bool   `json:"literal_text,omitempty"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty"`
	Context         int    `json:"context,omitempty"`
	OutputMode      string `json:"output_mode,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	Offset          int    `json:"offset,omitempty"`
}

type GrepResponseMetadata struct {
	FilesystemPathMetadata
	NumberOfMatches int  `json:"number_of_matches"`
	NumberOfFiles   int  `json:"number_of_files"`
	Truncated       bool `json:"truncated"`
}

type grepExecutor struct{}

func NewGrepExecutor() Executor { return &grepExecutor{} }

func (e *grepExecutor) Spec() Spec {
	return coreDefinitionSpec(grepToolName)
}

// grepSearch is one grep call: what to match and which page of results to show.
type grepSearch struct {
	re      *regexp.Regexp
	mode    string
	context int
	offset  int
	limit   int
}

// grepLine is one rendered line; match is false for a context line.
type grepLine struct {
	num   int
	text  string
	match bool
}

// grepHit is a matching line with the context lines around it.
type grepHit struct {
	before []grepLine
	line   grepLine
	after  []grepLine
}

// grepFileResult counts a file's matching lines and keeps the first hits.
type grepFileResult struct {
	count int
	hits  []grepHit
}

type grepFound struct {
	path   string
	result grepFileResult
}

func (e *grepExecutor) Execute(ctx context.Context, call Call) (Result, error) {
	var params GrepParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(grepToolName, err)
	}
	search, problem := newGrepSearch(params)
	if problem != "" {
		return Result{Content: problem, Status: ResultStatusError}, nil
	}

	policy, pathErr := resolveReadablePath(call.WorkingDir, params.Path)
	if pathErr != nil {
		return *pathErr, nil
	}
	if isProtectedCredentialPath(policy.RealPath) || isProtectedCredentialPath(policy.Path) {
		return Result{
			Content:  "Searching MatrixClaw credential files is blocked. Use provider or module status controls; secret values are never returned to model tools.",
			Metadata: filesystemPathMetadata(policy),
			Status:   ResultStatusError,
		}, nil
	}
	info, err := os.Stat(policy.Path)
	if err != nil {
		return Result{Content: "path does not exist: " + policy.Path, Metadata: filesystemPathMetadata(policy), Status: ResultStatusError}, nil
	}

	scope := newSearchScope(policy, call)
	var files []string
	if info.IsDir() {
		files, err = grepCandidates(ctx, scope, strings.TrimSpace(params.Include))
		if err != nil {
			return Result{}, fmt.Errorf("grep: %w", err)
		}
	} else if info.Size() > MaxReadBytes {
		return Result{Content: fmt.Sprintf("%s is larger than %d MB; search it with rg or grep in bash", policy.Path, MaxReadBytes>>20), Metadata: filesystemPathMetadata(policy), Status: ResultStatusError}, nil
	} else {
		files = []string{policy.Path}
	}
	found, err := search.run(ctx, files)
	if err != nil {
		return Result{}, fmt.Errorf("grep: %w", err)
	}
	content, metadata := search.render(found)
	metadata.FilesystemPathMetadata = filesystemPathMetadata(policy)
	return Result{Content: content + scope.leftNote(), Metadata: metadata}, nil
}

func newGrepSearch(params GrepParams) (grepSearch, string) {
	if strings.TrimSpace(params.Pattern) == "" {
		return grepSearch{}, "pattern is required"
	}
	search := grepSearch{
		mode:    params.OutputMode,
		context: min(max(params.Context, 0), grepMaxContext),
		offset:  max(params.Offset, 0),
		limit:   params.Limit,
	}
	switch search.mode {
	case "":
		search.mode = grepContent
	case grepContent, grepFilesWithMatches, grepCount:
	default:
		return grepSearch{}, "output_mode must be content, files_with_matches or count"
	}
	if search.limit <= 0 {
		search.limit = grepDefaultLimit
	}
	search.limit = min(search.limit, grepMaxLimit)
	expr := params.Pattern
	if params.LiteralText {
		expr = regexp.QuoteMeta(expr)
	}
	flags := "(?m)"
	if params.CaseInsensitive {
		flags = "(?mi)"
	}
	re, err := regexp.Compile(flags + expr)
	if err != nil {
		return grepSearch{}, fmt.Sprintf("invalid regular expression (Go RE2 syntax; set literal_text to search for plain text): %v", err)
	}
	search.re = re
	return search, ""
}

// grepCandidates lists the files to search below the scope's root, most
// recently modified first, as absolute paths. include filters them the
// ripgrep way; hidden entries are searched only when include names them.
// Links, files over MaxReadBytes and credential files are left out.
func grepCandidates(ctx context.Context, scope *searchScope, include string) ([]string, error) {
	descend := func(rel string) bool { return !strings.HasPrefix(path.Base(rel), ".") }
	keeps := func(rel string) bool { return !hiddenElement(rel) }
	if include != "" {
		filter := glob.CompileFilter(filepath.ToSlash(include))
		descend, keeps = filter.Reaches, filter.MatchVisible
	}
	rels, err := searchFiles(ctx, scope, descend, keeps)
	if err != nil {
		return nil, err
	}
	isCredential := protectedCredentials()
	var files []string
	for _, file := range statRecent(scope.root, rels) {
		if file.info.Mode().IsRegular() && file.info.Size() <= MaxReadBytes && !isCredential(scope.real(file.rel)) {
			files = append(files, filepath.Join(scope.root, filepath.FromSlash(file.rel)))
		}
	}
	return files, nil
}

// run scans files in order, in parallel batches, and keeps the hits the page
// can show while counting every match.
func (s grepSearch) run(ctx context.Context, files []string) ([]grepFound, error) {
	workers := min(runtime.GOMAXPROCS(0), 8)
	batchSize := workers * 8
	want := s.offset + s.limit
	seen := 0
	var found []grepFound
	for start := 0; start < len(files); start += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch := files[start:min(start+batchSize, len(files))]
		results := make([]grepFileResult, len(batch))
		keep := max(want-seen, 0)
		var next atomic.Int64
		var wg sync.WaitGroup
		for range min(workers, len(batch)) {
			wg.Go(func() {
				for i := int(next.Add(1) - 1); i < len(batch); i = int(next.Add(1) - 1) {
					results[i] = s.scan(batch[i], keep)
				}
			})
		}
		wg.Wait()
		for i, result := range results {
			if result.count == 0 {
				continue
			}
			found = append(found, grepFound{path: filepath.ToSlash(batch[i]), result: result})
			if s.mode == grepContent {
				seen += result.count
			} else {
				seen++
			}
		}
	}
	return found, nil
}

// scan counts the matching lines of one file and keeps its first keep hits;
// a file with a NUL byte near the start (binary) or that cannot be read has none.
func (s grepSearch) scan(file string, keep int) grepFileResult {
	data, err := os.ReadFile(file)
	if err != nil || bytes.IndexByte(data[:min(len(data), binarySniffBytes)], 0) >= 0 || !s.re.Match(data) {
		return grepFileResult{}
	}
	if s.mode != grepContent {
		keep = 0
	}
	var result grepFileResult
	var recent []grepLine
	after := 0
	for num := 1; len(data) > 0; num++ {
		raw := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			raw, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		raw = bytes.TrimSuffix(raw, []byte("\r"))
		matched := s.re.Match(raw)
		if matched {
			result.count++
			if s.mode == grepFilesWithMatches {
				return result
			}
		}
		kept := matched && len(result.hits) < keep
		if after == 0 && !kept && s.context == 0 {
			continue
		}
		line := grepLine{num: num, text: renderedLine(raw), match: matched}
		if after > 0 {
			last := &result.hits[len(result.hits)-1]
			last.after = append(last.after, line)
			after--
		}
		if kept {
			result.hits = append(result.hits, grepHit{before: append([]grepLine(nil), recent...), line: line})
			after = s.context
		}
		if s.context > 0 {
			recent = append(recent, line)
			if len(recent) > s.context {
				recent = recent[1:]
			}
		}
	}
	return result
}

// renderedLine is a line cut to maxRenderedLineWidth bytes on a rune boundary.
func renderedLine(raw []byte) string {
	if len(raw) <= maxRenderedLineWidth {
		return string(raw)
	}
	cut := maxRenderedLineWidth
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}
	return string(raw[:cut]) + "..."
}

func (s grepSearch) render(found []grepFound) (string, GrepResponseMetadata) {
	matches := 0
	for _, file := range found {
		matches += file.result.count
	}
	metadata := GrepResponseMetadata{NumberOfMatches: matches, NumberOfFiles: len(found)}
	if len(found) == 0 {
		return "No matches found", metadata
	}
	total, unit := matches, "matches"
	if s.mode != grepContent {
		total, unit = len(found), "files"
	}
	var out strings.Builder
	if s.mode == grepFilesWithMatches {
		_, _ = fmt.Fprintf(&out, "Found %d files\n", len(found))
	} else {
		_, _ = fmt.Fprintf(&out, "Found %d matches in %d files\n", matches, len(found))
	}
	shown := 0
	if s.mode == grepContent {
		shown = s.renderContent(&out, found)
	} else {
		for _, file := range found[min(s.offset, len(found)):min(s.offset+s.limit, len(found))] {
			if s.mode == grepCount {
				_, _ = fmt.Fprintf(&out, "%s: %d\n", file.path, file.result.count)
			} else {
				_, _ = fmt.Fprintf(&out, "%s\n", file.path)
			}
			shown++
		}
	}
	switch {
	case shown == 0:
		_, _ = fmt.Fprintf(&out, "\n(Nothing at offset %d: there are %d %s.)", s.offset, total, unit)
	case s.offset > 0 || s.offset+shown < total:
		_, _ = fmt.Fprintf(&out, "\n(Showing %s %d-%d of %d, most recently modified files first.", unit, s.offset+1, s.offset+shown, total)
		if s.offset+shown < total {
			_, _ = fmt.Fprintf(&out, " Continue with offset %d, or narrow path or include.", s.offset+shown)
		}
		out.WriteString(")")
		metadata.Truncated = s.offset+shown < total
	}
	return strings.TrimRight(out.String(), "\n"), metadata
}

// renderContent writes the hits on the page grouped by file, context lines
// marked with "-" and gaps with "--", and returns how many matches it showed.
func (s grepSearch) renderContent(out *strings.Builder, found []grepFound) int {
	index, shown := 0, 0
	for _, file := range found {
		printed := 0
		for _, hit := range file.result.hits {
			if index++; index <= s.offset || shown == s.limit {
				continue
			}
			shown++
			if printed == 0 {
				_, _ = fmt.Fprintf(out, "%s\n", file.path)
			}
			lines := append(append(append([]grepLine(nil), hit.before...), hit.line), hit.after...)
			for _, line := range lines {
				if line.num <= printed {
					continue
				}
				if printed > 0 && line.num > printed+1 && s.context > 0 {
					out.WriteString("  --\n")
				}
				mark := ":"
				if !line.match {
					mark = "-"
				}
				_, _ = fmt.Fprintf(out, "  %d%s %s\n", line.num, mark, line.text)
				printed = line.num
			}
		}
		index += file.result.count - len(file.result.hits)
	}
	return shown
}
