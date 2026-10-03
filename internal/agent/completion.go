package agent

import (
	"context"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// nudgeOpenTodo keeps the run going, once, when it replies without tools while
// its chain's todo list has open items, budget remains and the model can use
// tools; the reply is kept and the next reply without tools completes the run.
// A list that cannot be read holds nothing back. It reports whether it took over the step.
func (r *run) nudgeOpenTodo(ctx context.Context, gen generation, response providers.Response) (stepResult, bool) {
	if r.counters.TodoNudged || r.exhausted() != "" || !ToolUseAllowed(r.task.Model) {
		return stepResult{}, false
	}
	open, err := r.Todos.Open(ctx, r.task.SessionID, append([]string{r.task.RunID}, r.task.Continues...))
	if err != nil || len(open) == 0 {
		return stepResult{}, false
	}
	r.counters.TodoNudged = true
	r.counters.Continuations = 0
	assistant := gen.assistant
	if err := r.finishTurn(ctx, &assistant, gen.saved, response, transcript.FinishReasonEndTurn); err != nil {
		return unfinishedTurn(ctx, gen, err), true
	}
	if err := r.appendEngineMessage(ctx, transcript.OriginEngineModel, openTodoText(open)); err != nil {
		return failedStep(err), true
	}
	return stepResult{kind: stepContinue}, true
}

func openTodoText(open []todo.Item) string {
	return fmt.Sprintf("Your todo list has %s open:\n%s\nContinue with them, or update the list with todo_write and explain why you are stopping.", quantity(int64(len(open)), "item"), todo.Text(open))
}
