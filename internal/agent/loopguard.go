package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// A (tool, arguments, result) triple seen again among the last loopWindow
// calls means no progress, whether repeated in a row or in a cycle: the model
// is warned at loopWarnRepeats sightings and the run stops at loopStopRepeats.
const (
	loopWindow      = 20
	loopWarnRepeats = 3
	loopStopRepeats = 5
)

// observeCall records a finished call and how often the most repeated call with
// the same result appears in the recent window, whatever the order of the calls
// of a batch; the warning is re-armed once no call repeats that often. Waiting
// on a task that is still running is not a repeat.
func (s *RunState) observeCall(name string, args []byte, result tools.Result) {
	if result.Await != nil || result.Waiting {
		return
	}
	window := append(strings.Split(s.LoopRecent, "\n"), callHash(name, args, result)+" "+name)
	if s.LoopRecent == "" {
		window = window[1:]
	}
	window = window[max(0, len(window)-loopWindow):]
	s.LoopRecent = strings.Join(window, "\n")
	seen := make(map[string]int, len(window))
	s.LoopRepeats = 0
	for _, entry := range window {
		hash, tool, _ := strings.Cut(entry, " ")
		if seen[hash]++; seen[hash] >= s.LoopRepeats {
			s.LoopTool, s.LoopRepeats = tool, seen[hash]
		}
	}
	if s.LoopRepeats < loopWarnRepeats {
		s.LoopWarned = false
	}
}

func callHash(name string, args []byte, result tools.Result) string {
	sum := sha256.New()
	sum.Write([]byte(strings.TrimSpace(name)))
	sum.Write([]byte{0})
	sum.Write(canonicalArgs(args))
	sum.Write([]byte{0})
	sum.Write([]byte(strings.TrimSpace(result.Content)))
	if result.IsError() {
		sum.Write([]byte{1})
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

func loopWarningText(tool string) string {
	return "You are repeating " + tool + " with the same arguments and getting the same result. Change your approach instead of calling it again."
}

func loopStopText(tool string) string {
	return fmt.Sprintf("You called %s with the same arguments and got the same result %d times among your recent calls, so this run stops here. Do not call tools. Reply briefly: what is done, what remains, and how to continue.", tool, loopStopRepeats)
}
