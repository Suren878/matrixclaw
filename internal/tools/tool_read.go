package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeebo/xxh3"
)

const (
	readToolName     = "read"
	defaultReadLimit = 2000
	// readPageBytes keeps a page well under core's cut of a result (about
	// 8k tokens), so read never spills to a file.
	readPageBytes = 24 * 1024
	// maxReadLineRunes is where a long line is cut.
	maxReadLineRunes = 2000
	// readScanMaxBytes is the largest file read scans to the end, counting its
	// lines and remembering its version for edits.
	readScanMaxBytes = 64 << 20
	// readSniffBytes is how much of a file is checked for NUL bytes.
	readSniffBytes = 8000
)

type ReadParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// ReadResponseMetadata is the file a read showed and which of its lines;
// TotalLines is 0 when the file was too large to count.
type ReadResponseMetadata struct {
	FilesystemPathMetadata
	StartLine  int `json:"start_line,omitempty"`
	EndLine    int `json:"end_line,omitempty"`
	TotalLines int `json:"total_lines,omitempty"`
}

type readExecutor struct {
	files *fileLedger
}

func newReadExecutor(files *fileLedger) Executor { return &readExecutor{files: files} }

func (e *readExecutor) Spec() Spec {
	return coreDefinitionSpec(readToolName)
}

func (e *readExecutor) Execute(_ context.Context, call Call) (Result, error) {
	var params ReadParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return Result{}, InvalidArgs(readToolName, err)
	}
	switch {
	case strings.TrimSpace(params.FilePath) == "":
		return readError("file_path is required"), nil
	case params.Offset < 0:
		return readError("offset is the number of lines to skip and cannot be negative; pass 0 to start at line 1"), nil
	case params.Limit < 0:
		return readError("limit cannot be negative"), nil
	}
	policy, pathErr := resolveReadablePath(call.WorkingDir, params.FilePath)
	if pathErr != nil {
		return *pathErr, nil
	}
	path := policy.Path
	meta := ReadResponseMetadata{FilesystemPathMetadata: filesystemPathMetadata(policy)}
	fail := func(format string, args ...any) (Result, error) {
		return Result{Content: fmt.Sprintf(format, args...), Metadata: meta, Status: ResultStatusError}, nil
	}
	if isProtectedCredentialPath(policy.RealPath) || isProtectedCredentialPath(path) {
		return fail("Reading MatrixClaw credential files is blocked. Use provider or module status controls; secret values are never returned to model tools.")
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return fail("File not found: %s", path)
	}
	if err != nil {
		return fail("Cannot open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	switch {
	case err != nil:
		return fail("Cannot open %s: %v", path, err)
	case info.IsDir():
		return fail("%s is a directory; use ls to list it", path)
	case !info.Mode().IsRegular():
		return fail("%s is not a regular file", path)
	}

	limit := params.Limit
	if limit == 0 {
		limit = defaultReadLimit
	}
	sum := &hashCounter{hash: xxh3.New()}
	reader := bufio.NewReaderSize(io.TeeReader(file, sum), 64*1024)
	if head, _ := reader.Peek(readSniffBytes); bytes.IndexByte(head, 0) >= 0 {
		return fail("%s is a binary file (%s, %d bytes); read shows text only. Inspect it with bash (file, xxd | head) if you must.", path, http.DetectContentType(head), info.Size())
	}
	page, err := scanPage(reader, params.Offset, limit, info.Size() <= readScanMaxBytes)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", path, err)
	}
	if page.scanned {
		e.files.saw(call.SessionID, policy.RealPath, fileVersion{size: sum.n, sum: sum.hash.Sum64(), at: time.Now()})
		meta.TotalLines = page.lines
	}
	switch {
	case page.lines == 0 && page.scanned:
		return Result{Content: fmt.Sprintf("%s is empty.", path), Metadata: meta}, nil
	case page.shown == 0:
		return fail("%s has %d lines; offset %d skips all of them. Use an offset below %d.", path, page.lines, params.Offset, page.lines)
	}
	meta.StartLine, meta.EndLine = params.Offset+1, params.Offset+page.shown
	return Result{Content: page.render(params.Offset), Metadata: meta}, nil
}

func readError(content string) Result {
	return Result{Content: content, Status: ResultStatusError}
}

// hashCounter hashes and counts the bytes written to it.
type hashCounter struct {
	hash *xxh3.Hasher
	n    int64
}

func (h *hashCounter) Write(p []byte) (int, error) {
	h.n += int64(len(p))
	return h.hash.Write(p)
}

// readPage is the numbered lines a read shows: at most limit lines after
// offset and readPageBytes of output.
type readPage struct {
	body bytes.Buffer
	// shown is how many lines body holds; more is set when lines follow them.
	shown int
	more  bool
	// lines counts the file's lines when scanned (read to the end), else the
	// lines read so far.
	lines   int
	scanned bool
	cut     bool
}

// scanPage reads the page after offset; with scanAll it goes on to the end of
// the file, counting lines.
func scanPage(reader *bufio.Reader, offset int, limit int, scanAll bool) (*readPage, error) {
	page := &readPage{}
	for {
		line, cut, err := readLine(reader)
		if errors.Is(err, io.EOF) {
			page.scanned = true
			return page, nil
		}
		if err != nil {
			return nil, err
		}
		page.lines++
		if page.lines <= offset {
			continue
		}
		if page.more {
			if !scanAll {
				return page, nil
			}
			continue
		}
		numbered := fmt.Sprintf("%6d\t%s\n", page.lines, line)
		if page.shown >= limit || page.shown > 0 && page.body.Len()+len(numbered) > readPageBytes {
			page.more = true
			continue
		}
		page.body.WriteString(numbered)
		page.shown++
		page.cut = page.cut || cut
	}
}

func (p *readPage) render(offset int) string {
	var out strings.Builder
	out.WriteString("<file>\n")
	out.Write(p.body.Bytes())
	out.WriteString("</file>")
	last := offset + p.shown
	switch {
	case p.more && p.scanned:
		fmt.Fprintf(&out, "\n(Showing lines %d-%d of %d. To read on, call read with offset %d.)", offset+1, last, p.lines, last)
	case p.more:
		fmt.Fprintf(&out, "\n(Showing lines %d-%d of a large file. To read on, call read with offset %d.)", offset+1, last, last)
	case offset > 0:
		fmt.Fprintf(&out, "\n(Showing lines %d-%d of %d: the end of the file.)", offset+1, last, p.lines)
	}
	if p.cut {
		fmt.Fprintf(&out, "\n(Lines longer than %d characters were cut; see them whole with bash, e.g. cut or fold.)", maxReadLineRunes)
	}
	return out.String()
}

// readLine reads one line without its line end, cut to maxReadLineRunes
// with a note of its full length; io.EOF when no line is left.
func readLine(reader *bufio.Reader) (string, bool, error) {
	var kept []byte
	total := 0
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(kept) < maxReadLineRunes*utf8.UTFMax {
			kept = append(kept, chunk[:min(len(chunk), maxReadLineRunes*utf8.UTFMax-len(kept))]...)
		}
		total += utf8.RuneCount(chunk)
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && total == 0:
			return "", false, io.EOF
		case err != nil && !errors.Is(err, io.EOF):
			return "", false, err
		}
		if bytes.HasSuffix(chunk, []byte("\n")) {
			total--
		}
		if bytes.HasSuffix(chunk, []byte("\r\n")) {
			total--
		}
		break
	}
	kept = bytes.TrimSuffix(bytes.TrimSuffix(kept, []byte("\n")), []byte("\r"))
	if utf8.RuneCount(kept) <= maxReadLineRunes {
		return string(kept), false, nil
	}
	runes := []rune(string(kept))
	return fmt.Sprintf("%s… [line cut: %d of %d characters shown]", string(runes[:maxReadLineRunes]), maxReadLineRunes, total), true, nil
}
