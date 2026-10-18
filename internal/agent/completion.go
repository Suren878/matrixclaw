package agent

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// checkCompletion keeps the run going when it replies without tools while work
// is left: open items on its chain's todo list, files it changed without a
// successful check since, or a Stop hook that blocks. The first two speak once
// per run, Stop hooks up to stopHookLimit times, and only while budget remains
// and the model can use tools; the reply is kept and the next reply without
// tools completes the run. It reports whether it took over the step.
func (r *run) checkCompletion(ctx context.Context, gen generation, response providers.Response) (stepResult, bool) {
	if r.exhausted() != "" || !ToolUseAllowed(r.task.Model) {
		return stepResult{}, false
	}
	note := r.openTodoNote(ctx)
	if note == "" {
		note = r.verifyNote()
	}
	if note == "" {
		note = r.stopHookNote(ctx, response.Text)
	}
	if note == "" {
		return stepResult{}, false
	}
	r.state.Continuations = 0
	assistant := gen.assistant
	if err := r.finishTurn(ctx, &assistant, gen.saved, response, transcript.FinishReasonEndTurn); err != nil {
		return unfinishedTurn(ctx, gen, err), true
	}
	if err := r.appendEngineMessage(ctx, transcript.OriginEngineModel, note); err != nil {
		return failedStep(err), true
	}
	return stepResult{kind: stepContinue}, true
}

// openTodoNote points out the open items of the chain's todo list, once; a list
// that cannot be read holds nothing back.
func (r *run) openTodoNote(ctx context.Context) string {
	if r.state.TodoNudged {
		return ""
	}
	open, err := r.Todos.Open(ctx, r.task.SessionID, r.chain())
	if err != nil || len(open) == 0 {
		return ""
	}
	r.state.TodoNudged = true
	return fmt.Sprintf("Your todo list has %s open:\n%s\nContinue with them, or update the list with todo_write and explain why you are stopping.", quantity(int64(len(open)), "item"), todo.Text(open))
}

// verifyNote asks, once, for a check of files the run changed after its last
// successful build, test or lint command, when the run can run one.
func (r *run) verifyNote() string {
	if r.state.VerifyNudged || !r.state.Unverified || !r.hasTool(bashTool) {
		return ""
	}
	r.state.VerifyNudged = true
	return "You changed files after the last successful build, test or lint command of this run. Run the checks that cover your changes now; if they cannot run here, say why in your reply."
}

func (r *run) hasTool(name string) bool {
	for _, tool := range r.tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
