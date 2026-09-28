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

// Identical consecutive (tool, arguments, result) triples mean no progress: the
// model is warned at loopWarnRepeats and the run stops at loopStopRepeats.
const (
	loopWarnRepeats = 3
	loopStopRepeats = 5
)

// observeCall records a finished call; a call whose name, arguments and result
// match the previous one extends the no-progress streak. Waiting on a task
// that is still running is not a repeat.
func (c *Counters) observeCall(name string, args []byte, result tools.Result) {
	if result.Await != nil || result.Waiting {
		return
	}
	hash := callHash(name, args, result)
	if hash == c.LoopHash {
		c.LoopRepeats++
		return
	}
	c.LoopHash, c.LoopTool, c.LoopRepeats, c.LoopWarned = hash, name, 1, false
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
	return hex.EncodeToString(sum.Sum(nil))
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
	return fmt.Sprintf("You called %s with the same arguments and got the same result %d times in a row, so this run stops here. Do not call tools. Reply briefly: what is done, what remains, and how to continue.", tool, loopStopRepeats)
}
