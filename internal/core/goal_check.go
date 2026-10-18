package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/shelltask"
)

const (
	// goalCheckTimeout bounds one run of a goal's check command.
	goalCheckTimeout = 10 * time.Minute
	// goalCheckTail is how much of a failed check's output the model reads.
	goalCheckTail = 4000
	// goalCheckRepeats identical failures in a row block the goal.
	goalCheckRepeats = 3
)

// goalCheck is how one run of a goal's check command ended.
type goalCheck struct {
	command  string
	exitCode int
	timedOut bool
	output   string
}

func (g goalCheck) passed() bool {
	return !g.timedOut && g.exitCode == 0
}

// failure describes a failed check for the model: the command, how it ended
// and the tail of its output.
func (g goalCheck) failure() string {
	text := fmt.Sprintf("The goal check `%s` failed (%s).", g.command, g.end())
	if g.output != "" {
		text += "\nOutput (last part):\n" + g.output
	}
	return text
}

// end is how the check ended: its exit code or its timeout.
func (g goalCheck) end() string {
	if g.timedOut {
		return "timed out after " + goalCheckTimeout.String()
	}
	return fmt.Sprintf("exit %d", g.exitCode)
}

// digits are ignored when failures are compared, so durations and counters
// do not make the same failure look new.
var digits = regexp.MustCompile(`[0-9]+`)

// failureKey identifies a failure by how it ended and its output.
func (g goalCheck) failureKey() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d %t\n%s", g.exitCode, g.timedOut, digits.ReplaceAllString(g.output, "#"))))
	return hex.EncodeToString(sum[:8])
}

// runGoalCheck runs command with bash in dir with the environment of shell
// commands, stderr joined to stdout, within goalCheckTimeout. Only ctx
// stopping is an error; a command that cannot start fails the check.
func (c *Core) runGoalCheck(ctx context.Context, dir, command string) (goalCheck, error) {
	checkCtx, cancel := context.WithTimeout(ctx, goalCheckTimeout)
	defer cancel()
	var env []string
	if c.commandEnv != nil {
		env = c.commandEnv()
	}
	// The pipe keeps the output's tail; pipefail keeps the command's exit code.
	script := fmt.Sprintf("set -o pipefail\n{\n%s\n} 2>&1 | tail -c %d", command, 4*goalCheckTail)
	ran, err := shelltask.Run(checkCtx, script, dir, env, nil, 8*goalCheckTail)
	if ctx.Err() != nil {
		return goalCheck{}, ctx.Err()
	}
	check := goalCheck{command: command, exitCode: ran.ExitCode, output: outputTail(string(ran.Stdout))}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		check.timedOut = true
	case err != nil:
		check.exitCode, check.output = -1, err.Error()
	}
	return check, nil
}

// outputTail is the last goalCheckTail bytes of output, from a line start
// when the cut leaves one close by.
func outputTail(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= goalCheckTail {
		return output
	}
	tail := strings.ToValidUTF8(output[len(output)-goalCheckTail:], "")
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < 200 {
		tail = tail[i+1:]
	}
	return tail
}

// checkGoal runs the active goal's check and records it: a pass clears the
// failures; the same failure goalCheckRepeats times in a row blocks the goal.
// It reports the goal as recorded.
func (c *Core) checkGoal(ctx context.Context, goal SessionGoal) (goalCheck, SessionGoal, error) {
	session, err := c.store.GetSession(ctx, goal.SessionID)
	if err != nil {
		return goalCheck{}, SessionGoal{}, err
	}
	check, err := c.runGoalCheck(ctx, session.WorkingDir, goal.CheckCommand)
	if err != nil {
		return goalCheck{}, SessionGoal{}, err
	}
	recorded, err := c.updateGoal(ctx, goal.SessionID, func(current *SessionGoal) error {
		if current.Status != GoalActive || current.CheckCommand != check.command {
			return nil
		}
		if check.passed() {
			current.LastCheck, current.CheckFailure, current.CheckFailures = "passed", "", 0
			return nil
		}
		key := check.failureKey()
		if key == current.CheckFailure {
			current.CheckFailures++
		} else {
			current.CheckFailure, current.CheckFailures = key, 1
		}
		current.LastCheck = "failed (" + check.end() + ")"
		if current.CheckFailures >= goalCheckRepeats {
			current.Status = GoalBlocked
			current.Reason = fmt.Sprintf("the check `%s` failed the same way %d times in a row", check.command, current.CheckFailures)
			current.LastBlocker = current.Reason
		}
		return nil
	})
	return check, recorded, err
}

// coreVerifier is the engine's Verifier port: the session goal's check.
type coreVerifier struct {
	c *Core
}

func (v coreVerifier) Verify(ctx context.Context, sessionID, _ string) (agent.Verdict, error) {
	goal, active, err := v.c.activeGoal(ctx, sessionID)
	if err != nil || !active || goal.CheckCommand == "" {
		return agent.Verdict{}, err
	}
	check, recorded, err := v.c.checkGoal(ctx, goal)
	switch {
	case err != nil:
		return agent.Verdict{}, err
	case check.passed():
		return agent.Verdict{Note: fmt.Sprintf("Verification passed: the goal check `%s` exited 0.", check.command)}, nil
	case recorded.Status == GoalBlocked:
		return agent.Verdict{Note: check.failure() + "\nThe goal is blocked: " + recorded.Reason + ". It waits for the user."}, nil
	}
	return agent.Verdict{Hold: true, Note: check.failure() + "\nFix what fails and reply again: the goal is complete only when this check passes."}, nil
}
