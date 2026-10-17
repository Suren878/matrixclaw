package agent

import (
	"strings"
)

// reasoningTags name the blocks some models write their reasoning into.
var reasoningTags = map[string]bool{
	"think": true, "thought": true, "thinking": true, "reasoning": true, "analysis": true,
	"scratchpad": true, "internal_monologue": true, "chain_of_thought": true, "cot": true,
}

// Ollama-style reasoning opens with this line and closes with closeThinking.
const (
	openThinking  = "thinking..."
	closeThinking = "done thinking!"
)

// maxOpenTag bounds how much of a line is held to see whether it opens a block.
const maxOpenTag = 256

func sanitizeAssistantOutput(text string) string {
	return strings.TrimSpace(cleanAssistantOutput(text))
}

// cleanAssistantOutput strips reasoning blocks and extra blank lines but keeps the
// whitespace at both ends.
func cleanAssistantOutput(text string) string {
	s := replySanitizer{keepLeading: true}
	return s.Push(text) + s.Flush()
}

// replySanitizer strips reasoning blocks from a reply as it streams: a tag block
// the reply opens with, closed or not, and any closed block opened at the start
// of a line. Nothing inside a code fence is touched, and a block opened later
// that never closes is kept as written. Line ends become \n and runs of blank
// lines outside fences shrink to one. Each byte is looked at a bounded number
// of times, so a long stream costs linear time.
type replySanitizer struct {
	// keepLeading keeps whitespace before the first visible text.
	keepLeading bool
	// pending is input not decided yet: a line start that may open a block or
	// fence, or the tail of a block that may hold the start of its close.
	pending  string
	midLine  bool
	visible  bool
	afterCR  bool
	newlines int
	fence    string
	// closeTag is the lower-case close of the block being skipped, leading marks
	// one the reply opened with, and held keeps a later one as written until it
	// closes.
	closeTag string
	leading  bool
	held     strings.Builder
}

// Push takes the next delta and returns the text that is now clean.
func (s *replySanitizer) Push(delta string) string {
	var out strings.Builder
	s.pending += s.normalize(delta)
	s.run(&out, false)
	return out.String()
}

// Flush returns what the reply's end decides: a held line start as text, a
// later-line block that never closed as written, a leading one not at all.
func (s *replySanitizer) Flush() string {
	var out strings.Builder
	s.run(&out, true)
	return out.String()
}

// normalize turns \r\n and lone \r into \n, also across deltas.
func (s *replySanitizer) normalize(delta string) string {
	if s.afterCR && strings.HasPrefix(delta, "\n") {
		delta = delta[1:]
	}
	if delta == "" {
		return ""
	}
	s.afterCR = delta[len(delta)-1] == '\r'
	if strings.IndexByte(delta, '\r') < 0 {
		return delta
	}
	return strings.ReplaceAll(strings.ReplaceAll(delta, "\r\n", "\n"), "\r", "\n")
}

func (s *replySanitizer) run(out *strings.Builder, final bool) {
	for s.pending != "" {
		var ok bool
		switch {
		case s.closeTag != "":
			ok = s.skipBlock(out, final)
		case s.midLine:
			s.copyLine(out)
			ok = true
		case s.fence != "":
			ok = s.fenceLineStart(final)
		default:
			ok = s.lineStart(final)
		}
		if !ok {
			return
		}
	}
	if final && s.closeTag != "" {
		s.endUnclosedBlock(out)
	}
}

// copyLine emits text up to and including the next line end.
func (s *replySanitizer) copyLine(out *strings.Builder) {
	end := strings.IndexByte(s.pending, '\n')
	if end < 0 {
		s.emit(out, s.pending)
		s.pending = ""
		return
	}
	s.emit(out, s.pending[:end+1])
	s.pending = s.pending[end+1:]
	s.midLine = false
}

// lineStart decides what a line outside a fence starts: a reasoning block, a
// fence or plain text. False means it needs more input; a line start held for
// maxOpenTag bytes is decided as it stands.
func (s *replySanitizer) lineStart(final bool) bool {
	final = final || len(s.pending) >= maxOpenTag
	body := strings.TrimLeft(s.pending, " \t")
	if body == "" && !final {
		return false
	}
	indent := s.pending[:len(s.pending)-len(body)]
	if closeTag, n, decided := openingTag(body, final); !decided {
		return false
	} else if n > 0 {
		s.closeTag, s.leading = closeTag, !s.visible && closeTag != closeThinking
		if !s.leading {
			s.held.Reset()
			s.held.WriteString(indent + body[:n])
		}
		s.pending = body[n:]
		return true
	}
	if marker, decided := fenceMarker(body, final); !decided {
		return false
	} else if marker != "" {
		s.fence = marker
	}
	s.midLine = true
	return true
}

// fenceLineStart looks for the line that closes the open fence; the line is
// emitted either way.
func (s *replySanitizer) fenceLineStart(final bool) bool {
	final = final || len(s.pending) >= maxOpenTag
	body := strings.TrimLeft(s.pending, " \t")
	marker := strings.TrimLeft(body, s.fence[:1])
	run := len(body) - len(marker)
	rest := strings.TrimLeft(marker, " \t")
	switch {
	case run < len(s.fence) && run < len(body):
	case rest == "" && !final:
		return false
	case rest == "" || rest[0] == '\n':
		if run >= len(s.fence) {
			s.fence = ""
		}
	}
	s.midLine = true
	return true
}

// skipBlock drops a reasoning block up to its close, holding a later-line
// block's text in case it never closes. False means it needs more input.
func (s *replySanitizer) skipBlock(out *strings.Builder, final bool) bool {
	if end := strings.Index(lowerASCII(s.pending), s.closeTag); end >= 0 {
		s.pending = s.pending[end+len(s.closeTag):]
		s.closeTag = ""
		s.held.Reset()
		return true
	}
	if final {
		s.endUnclosedBlock(out)
		return true
	}
	keep := min(len(s.pending), len(s.closeTag)-1)
	if !s.leading {
		s.held.WriteString(s.pending[:len(s.pending)-keep])
	}
	s.pending = s.pending[len(s.pending)-keep:]
	return false
}

// endUnclosedBlock settles a block the reply ended inside: a leading one is
// all reasoning, a later one is text after all.
func (s *replySanitizer) endUnclosedBlock(out *strings.Builder) {
	if !s.leading {
		s.emit(out, s.held.String()+s.pending)
	}
	s.closeTag, s.pending = "", ""
	s.held.Reset()
}

// emit writes clean text: leading whitespace is dropped unless kept, and
// outside a fence more than one blank line in a row is.
func (s *replySanitizer) emit(out *strings.Builder, text string) {
	for len(text) > 0 {
		if !s.visible {
			trimmed := strings.TrimLeft(text, " \t\n")
			if !s.keepLeading {
				text = trimmed
			} else if lead := text[:len(text)-len(trimmed)]; lead != "" {
				s.write(out, lead)
				text = trimmed
			}
			if text == "" {
				return
			}
			s.visible = true
		}
		end := strings.IndexByte(text, '\n')
		if end < 0 {
			out.WriteString(text)
			s.newlines = 0
			return
		}
		if end > 0 {
			out.WriteString(text[:end])
			s.newlines = 0
		}
		s.write(out, "\n")
		text = text[end+1:]
	}
}

// write emits whitespace, counting line ends for the blank-line limit.
func (s *replySanitizer) write(out *strings.Builder, text string) {
	for _, r := range text {
		if r != '\n' {
			out.WriteRune(r)
			continue
		}
		s.newlines++
		if s.newlines <= 2 || s.fence != "" {
			out.WriteByte('\n')
		}
	}
}

// openingTag reports whether body starts with a reasoning block's opening: its
// lower-case close and the opening's length. decided is false while more input
// could still make it one.
func openingTag(body string, final bool) (closeTag string, n int, decided bool) {
	lower := lowerASCII(body[:min(len(body), maxOpenTag)])
	if strings.HasPrefix(lower, openThinking) {
		return closeThinking, len(openThinking), true
	}
	if !final && strings.HasPrefix(openThinking, lower) {
		return "", 0, false
	}
	if !strings.HasPrefix(lower, "<") {
		return "", 0, true
	}
	name := 1
	for name < len(lower) && (lower[name] >= 'a' && lower[name] <= 'z' || lower[name] == '_') {
		name++
	}
	if name == len(lower) {
		return "", 0, final || len(lower) >= maxOpenTag || !couldNameTag(lower[1:])
	}
	tag := lower[1:name]
	if !reasoningTags[tag] {
		return "", 0, true
	}
	switch lower[name] {
	case '>':
		return "</" + tag + ">", name + 1, true
	case ' ', '\t', '\n':
	default:
		return "", 0, true
	}
	end := strings.IndexByte(lower[name:], '>')
	if end < 0 {
		return "", 0, final || len(lower) >= maxOpenTag
	}
	end += name
	if lower[end-1] == '/' {
		return "", 0, true
	}
	return "</" + tag + ">", end + 1, true
}

// lowerASCII lower-cases ASCII letters only, so byte offsets stay the same.
func lowerASCII(text string) string {
	upper := false
	for i := 0; i < len(text) && !upper; i++ {
		upper = text[i] >= 'A' && text[i] <= 'Z'
	}
	if !upper {
		return text
	}
	b := []byte(text)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func couldNameTag(prefix string) bool {
	for tag := range reasoningTags {
		if strings.HasPrefix(tag, prefix) {
			return true
		}
	}
	return false
}

// fenceMarker reports the ``` or ~~~ run a line opens a fence with, or "".
func fenceMarker(body string, final bool) (string, bool) {
	if body == "" || body[0] != '`' && body[0] != '~' {
		return "", true
	}
	run := len(body) - len(strings.TrimLeft(body, body[:1]))
	if run == len(body) && !final {
		return "", false
	}
	if run < 3 {
		return "", true
	}
	return body[:run], true
}
