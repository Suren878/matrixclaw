// Package shelltask runs shell commands in process groups of their own and keeps
// their output in a size-capped file.
package shelltask

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// An output file holds at most MaxBytes: once full it keeps the first HeadBytes,
// a marker naming how much was dropped, and the newest output after it.
const (
	MaxBytes  = 20 << 20
	HeadBytes = 1 << 20
)

const (
	markerPrefix = "\n[... "
	markerSuffix = " bytes of output dropped ...]\n"
	markerDigits = 19
	markerLen    = int64(len(markerPrefix) + markerDigits + len(markerSuffix))
)

// Output is the file a command writes its stdout and stderr to.
type Output struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	size    int64
	dropped int64
}

// CreateOutput creates the file at path, readable by the owner only.
func CreateOutput(path string) (*Output, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &Output{path: path, file: file}, nil
}

// Path is where the output is kept.
func (o *Output) Path() string {
	return o.path
}

// Write appends p; output past MaxBytes drops the oldest bytes after the head.
func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.file == nil {
		return 0, os.ErrClosed
	}
	if o.size+int64(len(p)) <= MaxBytes {
		n, err := o.file.Write(p)
		o.size += int64(n)
		return n, err
	}
	if err := o.compact(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes the file; the output stays readable.
func (o *Output) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.file == nil {
		return nil
	}
	err := o.file.Close()
	o.file = nil
	return err
}

// compact rewrites the file as its head, the marker and the newest half of the
// room left, ending with p.
func (o *Output) compact(p []byte) error {
	tailStart := int64(HeadBytes)
	if o.dropped > 0 {
		tailStart += markerLen
	}
	tail := o.size - tailStart
	keep := (MaxBytes - HeadBytes - markerLen) / 2
	fromFile := min(max(keep-int64(len(p)), 0), tail)
	o.dropped += tail - fromFile
	if int64(len(p)) > keep {
		o.dropped += int64(len(p)) - keep
		p = p[int64(len(p))-keep:]
	}

	source, err := os.Open(o.path)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	temp, err := os.CreateTemp(filepath.Dir(o.path), ".output-*")
	if err != nil {
		return err
	}
	written, err := o.writeCompacted(temp, source, tailStart+tail-fromFile, fromFile, p)
	if err = errors.Join(err, temp.Close()); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	if err := os.Rename(temp.Name(), o.path); err != nil {
		_ = os.Remove(temp.Name())
		return err
	}
	_ = o.file.Close()
	o.file, err = os.OpenFile(o.path, os.O_WRONLY|os.O_APPEND, 0o600)
	o.size = written
	return err
}

func (o *Output) writeCompacted(dst io.Writer, source *os.File, keptFrom, kept int64, p []byte) (int64, error) {
	var written int64
	for _, part := range []io.Reader{
		io.NewSectionReader(source, 0, HeadBytes),
		strings.NewReader(marker(o.dropped)),
		io.NewSectionReader(source, keptFrom, kept),
		bytes.NewReader(p),
	} {
		n, err := io.Copy(dst, part)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func marker(dropped int64) string {
	return fmt.Sprintf("%s%0*d%s", markerPrefix, markerDigits, dropped, markerSuffix)
}

// layout says where the dropped part of an output file is: dropped bytes of
// the output are missing between HeadBytes and the rest of the file.
type layout struct {
	size    int64
	dropped int64
}

func readLayout(file *os.File) (layout, error) {
	info, err := file.Stat()
	if err != nil {
		return layout{}, err
	}
	out := layout{size: info.Size()}
	if out.size < HeadBytes+markerLen {
		return out, nil
	}
	buf := make([]byte, markerLen)
	if _, err := file.ReadAt(buf, HeadBytes); err != nil {
		return layout{}, err
	}
	text := string(buf)
	if !strings.HasPrefix(text, markerPrefix) || !strings.HasSuffix(text, markerSuffix) {
		return out, nil
	}
	dropped, err := strconv.ParseInt(text[len(markerPrefix):len(markerPrefix)+markerDigits], 10, 64)
	if err != nil {
		return out, nil
	}
	out.dropped = dropped
	return out, nil
}

// total is how many bytes the command has written.
func (l layout) total() int64 {
	if l.dropped == 0 {
		return l.size
	}
	return l.size - markerLen + l.dropped
}

// fileOffset maps an output offset to the file; skipped counts output bytes
// that were dropped before it.
func (l layout) fileOffset(cursor int64) (offset int64, skipped int64) {
	if l.dropped == 0 || cursor < HeadBytes {
		return cursor, 0
	}
	if cursor < HeadBytes+l.dropped {
		skipped = HeadBytes + l.dropped - cursor
		cursor = HeadBytes + l.dropped
	}
	return cursor - l.dropped + markerLen, skipped
}

// Chunk is output read from a cursor on.
type Chunk struct {
	Text string
	// Next is the cursor after Text; Skipped counts bytes dropped before it.
	Next    int64
	Skipped int64
	// More is set when output past Next was already written.
	More bool
}

// Read returns up to limit bytes of the output at path from cursor on, an
// output offset that survives dropping; it never splits a UTF-8 character.
func Read(path string, cursor int64, limit int) (Chunk, error) {
	file, err := os.Open(path)
	if err != nil {
		return Chunk{}, err
	}
	defer func() { _ = file.Close() }()
	l, err := readLayout(file)
	if err != nil {
		return Chunk{}, err
	}
	cursor = min(max(cursor, 0), l.total())
	offset, skipped := l.fileOffset(cursor)
	cursor += skipped
	end := l.size
	if l.dropped > 0 && offset < HeadBytes {
		end = HeadBytes
	}
	n := min(int64(limit), end-offset)
	buf := make([]byte, n)
	if _, err := file.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return Chunk{}, err
	}
	buf = completeRunes(buf, offset+n < l.size)
	next := cursor + int64(len(buf))
	return Chunk{Text: string(buf), Next: next, Skipped: skipped, More: next < l.total()}, nil
}

// Tail returns the last limit bytes of the output at path.
func Tail(path string, limit int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	n := min(int64(limit), info.Size())
	buf := make([]byte, n)
	if _, err := file.ReadAt(buf, info.Size()-n); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	for len(buf) > 0 && !utf8.RuneStart(buf[0]) {
		buf = buf[1:]
	}
	return string(buf), nil
}

// completeRunes drops a character cut at the end of buf when more follows.
func completeRunes(buf []byte, more bool) []byte {
	if !more {
		return buf
	}
	for i := len(buf) - 1; i >= 0 && i >= len(buf)-utf8.UTFMax; i-- {
		if utf8.RuneStart(buf[i]) {
			if !utf8.FullRune(buf[i:]) {
				return buf[:i]
			}
			break
		}
	}
	return buf
}
