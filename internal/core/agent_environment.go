package core

import (
	"context"
	"os"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/workspace"
)

// PromptEnvironment is what native runs are told about the daemon's place:
// the timezone their clock reads in, the user's global instruction file and
// the directory of the user's agent files.
type PromptEnvironment struct {
	Timezone         *time.Location
	UserInstructions string
	AgentsDir        string
}

// runFacts is what a native run is told about where it works, read once
// when it starts.
type runFacts struct {
	// coding is a run with tools in a working directory that answers more
	// than one reply-once chat message; it gets the coding manual.
	coding       bool
	gitRoot      string
	instructions []workspace.InstructionFile
	context      prompt.RunContext
}

func (c *Core) readRunFacts(ctx context.Context, turn nativeTurn) runFacts {
	location := c.promptEnv.Timezone
	if location == nil {
		location = time.UTC
	}
	facts := runFacts{context: prompt.RunContext{Now: c.now().In(location), Model: turn.Model}}
	if session, err := c.store.GetSession(ctx, turn.SessionID); err == nil {
		if !turn.Readonly {
			facts.context.PermissionMode = string(NormalizePermissionMode(string(session.PermissionMode)))
		}
		facts.context.Sandbox = c.sandboxNote(session, turn.Readonly)
	}
	if run, err := c.store.GetRun(ctx, turn.RunID); err == nil && !turn.Subagent {
		facts.context.Client = run.Client
	}
	facts.coding = c.codingRun(ctx, turn)
	if !facts.coding {
		return facts
	}
	if git, ok := workspace.ReadGit(ctx, turn.WorkingDir); ok {
		facts.gitRoot = git.Root
		facts.context.Git = &git
	}
	home, _ := os.UserHomeDir()
	facts.instructions = workspace.Instructions(turn.WorkingDir, facts.gitRoot, home, c.promptEnv.UserInstructions)
	if turn.Agent.LeanContext && instructionsSize(facts.instructions) > leanInstructionsBytes {
		facts.instructions = nil
	}
	return facts
}

// leanInstructionsBytes is the most project instructions a LeanContext
// child still gets.
const leanInstructionsBytes = 2048

func instructionsSize(files []workspace.InstructionFile) int {
	size := 0
	for _, file := range files {
		size += len(file.Content)
	}
	return size
}

// codingRun reports whether the turn's run gets the coding manual: it has
// tools and a working directory and does not answer a reply-once address.
func (c *Core) codingRun(ctx context.Context, turn nativeTurn) bool {
	return turn.ToolUse && turn.WorkingDir != "" && !c.runRepliesOnce(ctx, turn.RunID)
}

// runRepliesOnce reports whether the run answers a reply-once address (a
// Telegram guest query or inline message): a chat answer, often for someone
// other than the owner, which gets neither the coding manual nor the
// project's instruction files.
func (c *Core) runRepliesOnce(ctx context.Context, runID string) bool {
	deliveries, err := c.store.ListClientDeliveries(ctx, ClientDeliveryFilter{RunID: runID, Type: ClientDeliveryTypeRun, Limit: 1})
	return err == nil && len(deliveries) > 0 && deliveries[0].ReplyOnce
}
