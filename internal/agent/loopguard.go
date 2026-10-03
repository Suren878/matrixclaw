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

// observeCall records a finished call and how often the same call with the
// same result appears in the recent window; a call not seen there ends the
// warned streak. Waiting on a task that is still running is not a repeat.
func (c *Counters) observeCall(name string, args []byte, result tools.Result) {
	if result.Await != nil || result.Waiting {
		return
	}
	hash := callHash(name, args, result)
	c.LoopRecent += hash
	if len(c.LoopRecent) > loopWindow*len(hash) {
		c.LoopRecent = c.LoopRecent[len(c.LoopRecent)-loopWindow*len(hash):]
	}
	seen := 0
	for i := 0; i < len(c.LoopRecent); i += len(hash) {
		if c.LoopRecent[i:i+len(hash)] == hash {
			seen++
		}
	}
	if seen == 1 {
		c.LoopWarned = false
	}
	c.LoopTool, c.LoopRepeats = name, seen
}

func callHash(name string, args []byte, result tools.Result) string {
	sum := sha256.New()
	sum.Write([]byte(strings.TrimSpace(name)))
	sum.Write([]byte{0})
	sum.Write(canonicalArgs(args))
	sum.Write([]byte{0})
	sum.Write([]byte(strings.TrimSpace(result.Content)))
	if result.IsError {
		sum.Write([]byte{1})
	}
	return hex.EncodeToString(sum.Sum(nil)[:8])
}

// canonicalArgs re-encodes JSON arguments with sorted keys, so a different layout
// does not hide a repeated call.
func canonicalArgs(args []byte) []byte {
	args = bytes.TrimSpace(args)
	if len(args) == 0 {
		return []byte(`{}`)
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
