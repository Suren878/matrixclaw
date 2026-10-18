package lsp

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Timing and limits of the diagnostics after an edit, after opencode's.
const (
	// diagnosticsDebounce is the quiet time that ends the wait once every
	// edited file has its fresh diagnostics.
	diagnosticsDebounce = 150 * time.Millisecond
	// documentWait bounds the wait for the edited files' diagnostics, server
	// start included; fullWait the whole wait.
	documentWait     = 5 * time.Second
	fullWait         = 10 * time.Second
	maxErrorsPerFile = 20
	maxOtherFiles    = 5
)

// Report is what the language servers said after an edit: the edited files
// they checked and the errors in them, and the other files their servers
// know errors in, most recently published first.
type Report struct {
	Checked []string
	Errors  map[string][]Diagnostic
	Others  []FileErrors
}

// FileErrors are the errors of one file.
type FileErrors struct {
	Path   string
	Errors []Diagnostic
}

// Diagnose sends the edited files to their servers and waits for their
// diagnostics. A file whose server is missing, not ready within documentWait
// or silent about it is left out of Checked, so another check can take it.
// The errors in other files are all a server knows when it can tell that it
// diagnosed everything the edit affects (gopls), else those it published
// while it checked the edited files.
func (m *Manager) Diagnose(ctx context.Context, files []string) Report {
	report := Report{Errors: map[string][]Diagnostic{}}
	if m == nil {
		return report
	}
	ctx, cancel := context.WithTimeout(ctx, fullWait)
	defer cancel()
	groups := map[*client][]string{}
	var order []*client
	for _, file := range files {
		c, err := m.clientFor(file)
		if err != nil {
			continue
		}
		if groups[c] == nil {
			order = append(order, c)
		}
		groups[c] = append(groups[c], file)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range order {
		wg.Go(func() {
			checked, errs, others := c.diagnose(ctx, groups[c])
			mu.Lock()
			defer mu.Unlock()
			report.Checked = append(report.Checked, checked...)
			maps.Copy(report.Errors, errs)
			report.Others = append(report.Others, others...)
		})
	}
	wg.Wait()
	report.Others = slices.DeleteFunc(report.Others, func(f FileErrors) bool { return slices.Contains(files, f.Path) })
	slices.SortFunc(report.Checked, func(a, b string) int { return slices.Index(files, a) - slices.Index(files, b) })
	return report
}

// diagnose syncs files and waits until each has a push newer than its
// change, then until the server settled or was quiet for diagnosticsDebounce.
func (c *client) diagnose(ctx context.Context, files []string) (checked []string, errs map[string][]Diagnostic, others []FileErrors) {
	docCtx, cancel := context.WithTimeout(ctx, documentWait)
	defer cancel()
	if c.wait(docCtx) != nil || c.refresh() != nil {
		return nil, nil, nil
	}
	c.mu.Lock()
	before := c.pushes
	c.mu.Unlock()
	changes := map[string]change{}
	for _, file := range files {
		ch, ok, err := c.sync(file)
		if err != nil {
			return nil, nil, nil
		}
		if ok {
			changes[file] = ch
		}
	}
	c.waitFor(docCtx, func() bool {
		for _, ch := range changes {
			if !c.diags[ch.uri].fresh(ch) {
				return false
			}
		}
		return true
	})
	settled := false
	if c.lang.settle != nil && len(changes) > 0 {
		uris := make([]string, 0, len(changes))
		for _, ch := range changes {
			uris = append(uris, ch.uri)
		}
		settled = c.lang.settle(ctx, c, uris) == nil
	}
	if !settled {
		c.waitQuiet(ctx)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	errs = map[string][]Diagnostic{}
	for _, file := range files {
		ch, ok := changes[file]
		if !ok || !c.diags[ch.uri].fresh(ch) {
			continue
		}
		checked = append(checked, file)
		if list := errorsOnly(c.diags[ch.uri].diagnostics); len(list) > 0 {
			errs[file] = list
		}
	}
	pushes := map[string]int{}
	for uri, push := range c.diags {
		path := uriPath(uri)
		if _, err := os.Stat(path); path == "" || err != nil || !settled && push.push <= before {
			continue
		}
		if list := errorsOnly(push.diagnostics); len(list) > 0 {
			others = append(others, FileErrors{Path: path, Errors: list})
			pushes[path] = push.push
		}
	}
	slices.SortFunc(others, func(a, b FileErrors) int {
		return cmp.Or(cmp.Compare(pushes[b.Path], pushes[a.Path]), cmp.Compare(a.Path, b.Path))
	})
	return checked, errs, others
}

// waitFor waits until done (checked under c.mu) or ctx ends.
func (c *client) waitFor(ctx context.Context, done func() bool) {
	for {
		c.mu.Lock()
		ok, changed := done(), c.changed
		c.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		case <-c.conn.done:
			return
		}
	}
}

// waitQuiet waits until no push came for diagnosticsDebounce or ctx ends.
func (c *client) waitQuiet(ctx context.Context) {
	timer := time.NewTimer(diagnosticsDebounce)
	defer timer.Stop()
	for {
		c.mu.Lock()
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
			timer.Reset(diagnosticsDebounce)
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		case <-c.conn.done:
			return
		}
	}
}

// errorsOnly keeps the errors, once each, in file order.
func errorsOnly(list []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, d := range list {
		if d.Severity != severityError {
			continue
		}
		if !slices.ContainsFunc(out, func(o Diagnostic) bool { return o.Range == d.Range && o.Message == d.Message }) {
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Range.Start.Line, b.Range.Start.Line), cmp.Compare(a.Range.Start.Character, b.Range.Start.Character))
	})
	return out
}

// Lines renders the errors of the edited files, at most maxErrorsPerFile each.
func (r Report) Lines() []string {
	var lines []string
	for _, file := range r.Checked {
		lines = append(lines, fileLines(file, r.Errors[file])...)
	}
	return lines
}

// OtherLines renders the errors of at most maxOtherFiles other files under a
// heading; nil when there are none.
func (r Report) OtherLines() []string {
	if len(r.Others) == 0 {
		return nil
	}
	lines := []string{"Errors in other files:"}
	for _, file := range r.Others[:min(len(r.Others), maxOtherFiles)] {
		lines = append(lines, fileLines(file.Path, file.Errors)...)
	}
	if len(r.Others) > maxOtherFiles {
		lines = append(lines, fmt.Sprintf("... and %d more files with errors", len(r.Others)-maxOtherFiles))
	}
	return lines
}

func fileLines(file string, list []Diagnostic) []string {
	if len(list) == 0 {
		return nil
	}
	text, _ := os.ReadFile(file)
	var lines []string
	for _, d := range list[:min(len(list), maxErrorsPerFile)] {
		message := strings.Split(strings.TrimSpace(d.Message), "\n")
		lines = append(lines, fmt.Sprintf("%s:%d:%d: %s", file, d.Range.Start.Line+1, column(text, d.Range.Start), message[0]))
		for _, more := range message[1:] {
			if more = strings.TrimSpace(more); more != "" {
				lines = append(lines, "\t"+more)
			}
		}
	}
	if len(list) > maxErrorsPerFile {
		lines = append(lines, fmt.Sprintf("... and %d more errors in %s", len(list)-maxErrorsPerFile, file))
	}
	return lines
}

// column is the 1-based character column of pos, whose Character counts
// UTF-16 units, in text; pos.Character+1 when text lacks the line.
func column(text []byte, pos Position) int {
	line, ok := lineOf(text, pos.Line)
	if !ok {
		return pos.Character + 1
	}
	units, runes := 0, 0
	for len(line) > 0 && units < pos.Character {
		r, size := utf8.DecodeRune(line)
		units += max(utf16.RuneLen(r), 1)
		runes++
		line = line[size:]
	}
	return runes + 1
}

// utf16Character is the UTF-16 offset of the 1-based character column col on
// line (0-based) of text.
func utf16Character(text []byte, line, col int) int {
	content, ok := lineOf(text, line)
	if !ok {
		return max(col-1, 0)
	}
	units := 0
	for i := 0; i < col-1 && len(content) > 0; i++ {
		r, size := utf8.DecodeRune(content)
		units += max(utf16.RuneLen(r), 1)
		content = content[size:]
	}
	return units
}

// lineOf is line n (0-based) of text without its line end.
func lineOf(text []byte, n int) ([]byte, bool) {
	for range n {
		at := bytes.IndexByte(text, '\n')
		if at < 0 {
			return nil, false
		}
		text = text[at+1:]
	}
	if at := bytes.IndexByte(text, '\n'); at >= 0 {
		text = text[:at]
	}
	return bytes.TrimSuffix(text, []byte("\r")), true
}
