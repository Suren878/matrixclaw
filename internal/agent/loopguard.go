package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// The loop guard watches the last loopWindow calls and long replies. The same
// call with the same result, the same call failing whatever the error says, or
// the same long reply text seen loopWarnRepeats times warns the model, and
// loopStopRepeats times stops the run; so do two calls taking turns with the
// same results over the last loopCycleCalls calls.
const (
	loopWindow      = 20
	loopWarnRepeats = 3
	loopStopRepeats = 5
	loopCycleCalls  = 6
	// loopReplyRunes is the shortest reply text the guard compares.
	loopReplyRunes = 60
)

// LoopKind is the pattern the loop guard saw.
type LoopKind string

const (
	LoopSame    LoopKind = "same"
	LoopFailing LoopKind = "failing"
	LoopCycle   LoopKind = "cycle"
	LoopReply   LoopKind = "reply"
)

// loopEntry is one line of the guard's window: "hash failed tool", failed
// being "!" for an error result and "=" otherwise.
type loopEntry struct {
	hash   string
	failed bool
	tool   string
}

// observeCall records a finished call. Waiting on a task that is still running
// is not a repeat.
func (s *RunState) observeCall(name string, args []byte, result tools.Result) {
	if result.Await != nil || result.Waiting {
		return
	}
	flag := "="
	if result.IsError() {
		flag = "!"
	}
	s.LoopRecent = pushWindow(s.LoopRecent, callHash(name, args, result)+" "+flag+" "+name)
	s.judgeLoop()
}

// observeReply records the text of a reply long enough to compare.
func (s *RunState) observeReply(text string) {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) < loopReplyRunes {
		return
	}
	sum := sha256.Sum256([]byte(text))
	s.LoopReplies = pushWindow(s.LoopReplies, hex.EncodeToString(sum[:8]))
	s.judgeLoop()
}

// pushWindow appends line to the newline-separated window, keeping the newest
// loopWindow lines.
func pushWindow(window string, line string) string {
	lines := append(strings.Split(window, "\n"), line)
	if window == "" {
		lines = lines[1:]
	}
	return strings.Join(lines[max(0, len(lines)-loopWindow):], "\n")
}

// judgeLoop finds the strongest pattern in the windows, whatever the order of
// the calls of a batch; the warning is re-armed once nothing repeats that often.
func (s *RunState) judgeLoop() {
	calls := loopEntries(s.LoopRecent)
	s.LoopKind, s.LoopTool, s.LoopRepeats = "", "", 0
	seen := make(map[string]int, len(calls))
	for _, entry := range calls {
		if seen[entry.hash]++; seen[entry.hash] >= s.LoopRepeats {
			s.LoopKind, s.LoopTool, s.LoopRepeats = LoopSame, entry.tool, seen[entry.hash]
			if entry.failed {
				s.LoopKind = LoopFailing
			}
		}
	}
	if pair, ok := cycle(calls); ok {
		s.LoopKind, s.LoopTool, s.LoopRepeats = LoopCycle, pair, loopStopRepeats
	}
	replies := map[string]int{}
	for _, hash := range strings.Split(s.LoopReplies, "\n") {
		if replies[hash]++; hash != "" && replies[hash] > s.LoopRepeats {
			s.LoopKind, s.LoopTool, s.LoopRepeats = LoopReply, "", replies[hash]
		}
	}
	if s.LoopRepeats < loopWarnRepeats {
		s.LoopWarned = false
	}
}

func loopEntries(window string) []loopEntry {
	if window == "" {
		return nil
	}
	lines := strings.Split(window, "\n")
	entries := make([]loopEntry, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, " ", 3)
		if len(fields) == 3 {
			entries = append(entries, loopEntry{hash: fields[0], failed: fields[1] == "!", tool: fields[2]})
		}
	}
	return entries
}

// cycle reports whether the last loopCycleCalls calls are two calls, each
// repeated as often, and names their tools.
func cycle(calls []loopEntry) (string, bool) {
	if len(calls) < loopCycleCalls {
		return "", false
	}
	counts := map[string]int{}
	var names []string
	for _, entry := range calls[len(calls)-loopCycleCalls:] {
		if counts[entry.hash]++; counts[entry.hash] == 1 {
			names = append(names, entry.tool)
		}
	}
	if len(counts) != 2 {
		return "", false
	}
	for _, count := range counts {
		if count != loopCycleCalls/2 {
			return "", false
		}
	}
	return names[0] + " and " + names[1], true
}

// callHash identifies a call by tool and arguments, and by its result unless
// the call failed: failing again with other words is no progress either.
func callHash(name string, args []byte, result tools.Result) string {
	sum := sha256.New()
	sum.Write([]byte(strings.TrimSpace(name)))
	sum.Write([]byte{0})
	sum.Write(canonicalArgs(args))
	sum.Write([]byte{0})
	if result.IsError() {
		sum.Write([]byte{1})
	} else {
		sum.Write([]byte(strings.TrimSpace(result.Content)))
	}
	return hex.EncodeToString(sum.Sum(nil)[:8])
}

// canonicalArgs re-encodes JSON arguments with sorted keys and exact numbers, so
// a different layout does not hide the same call; text that is not JSON stays
// as it is.
func canonicalArgs(args []byte) []byte {
	args = bytes.TrimSpace(args)
	if len(args) == 0 {
		return []byte(`{}`)
	}
	if !json.Valid(args) {
		return args
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return args
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return args
	}
	return canonical
}

func (s RunState) loopWarningText() string {
	switch s.LoopKind {
	case LoopFailing:
		return "You are repeating " + s.LoopTool + " with the same arguments and it keeps failing. Read the error and change the call or your approach instead of sending it again."
	case LoopReply:
		return "Your recent replies repeat the same text word for word. If you are stuck, change your approach; otherwise move on to the next piece of work."
	default:
		return "You are repeating " + s.LoopTool + " with the same arguments and getting the same result. Change your approach instead of calling it again."
	}
}

func (s RunState) loopStopText() string {
	const next = " Do not call tools. Reply briefly: what is done, what remains, and how to continue."
	switch s.LoopKind {
	case LoopFailing:
		return fmt.Sprintf("You called %s with the same arguments and it failed %d times among your recent calls, so this run stops here.", s.LoopTool, s.LoopRepeats) + next
	case LoopCycle:
		return "You kept alternating " + s.LoopTool + " with the same arguments and the same results, so this run stops here." + next
	case LoopReply:
		return fmt.Sprintf("Your replies repeated the same text word for word %d times, so this run stops here.", s.LoopRepeats) + next
	default:
		return fmt.Sprintf("You called %s with the same arguments and got the same result %d times among your recent calls, so this run stops here.", s.LoopTool, s.LoopRepeats) + next
	}
}
