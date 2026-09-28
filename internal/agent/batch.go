package agent

import (
	"context"
	"errors"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// maxParallelCalls bounds how many calls of one batch run at once.
const maxParallelCalls = 8

// detachedWriteTimeout bounds the writes that keep a stopped batch's results.
const detachedWriteTimeout = 5 * time.Second

// panicResult answers a call whose tool panicked.
var panicResult = tools.Result{Content: "The tool failed unexpectedly; the daemon log has the details.", Status: tools.ResultStatusError, IsError: true}

// callState is where a call of a batch stands.
type callState int

const (
	callWaiting   callState = iota // not admitted yet
	callRunning                    // started, its tool has not returned
	callFinished                   // its result waits to be journaled
	callRejected                   // refused before it ran; its error waits to be journaled
	callAsked                      // waits for approval; it has no result
	callStopped                    // did not run or return because the run stopped
	callJournaled                  // its result is journaled
)

type batchCall struct {
	req    callRequest
	call   tools.Call
	key    string
	state  callState
	result tools.Result
}

type callOutcome struct {
	result tools.Result
	err    error
}

// batch is one runCalls: calls are admitted in call order, run concurrently by
// key and journaled in call order by the engine goroutine alone.
type batch struct {
	r     *run
	calls []*batchCall
	sched *toolsched.Batch[callOutcome]
	// next is the first call not admitted; barrier is the admitted barrier whose
	// approval is not known yet, or -1; held is set once a barrier asked.
	next, barrier int
	held          bool
	// journaled is the first admitted call whose result is not journaled.
	journaled int
	waiting   bool
}

// runCalls runs the calls of one batch: admitted in call order, run at once
// (calls sharing a key one at a time), journaled in call order. A call waiting
// for approval parks while the rest runs, unless it is a barrier: the calls
// after it wait, and stay deferred if it asks. It reports whether the run waits.
func (r *run) runCalls(ctx context.Context, requests []callRequest) (bool, error) {
	if len(requests) == 0 {
		return false, nil
	}
	batchCtx, stop := context.WithCancel(ctx)
	defer stop()
	b := &batch{r: r, sched: toolsched.NewBatch[callOutcome](batchCtx, r.Locks, maxParallelCalls), barrier: -1}
	for _, request := range requests {
		b.calls = append(b.calls, &batchCall{req: request, call: r.toolCall(request)})
	}
	err := b.run(ctx)
	if err == nil {
		return b.waiting, nil
	}
	stop()
	b.drain()
	if ctx.Err() != nil {
		return false, errors.Join(ctx.Err(), b.keep(ctx))
	}
	return false, err
}

func (b *batch) run(ctx context.Context) error {
	if err := b.admit(ctx); err != nil {
		return err
	}
	for {
		if err := b.journal(ctx); err != nil {
			return err
		}
		if b.sched.Running() == 0 {
			return nil
		}
		if err := b.settle(ctx, b.sched.Next()); err != nil {
			return err
		}
		if err := b.admit(ctx); err != nil {
			return err
		}
	}
}

// admit authorizes and journals calls in call order until one is a barrier
// whose approval is not known yet, journals the calls behind it deferred and
// checkpoints the batch; only then do the admitted calls start.
func (b *batch) admit(ctx context.Context) error {
	if b.held || b.barrier >= 0 || b.next == len(b.calls) {
		return nil
	}
	var started []int
	for b.next < len(b.calls) && b.barrier < 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		c := b.calls[b.next]
		decision, err := b.r.Tools.Authorize(ctx, c.req.name, c.call)
		if err != nil {
			return err
		}
		if !decision.Allowed {
			if err := b.r.writeCall(ctx, c.req, true); err != nil {
				return err
			}
			c.state, c.result = callRejected, tools.Result{Content: decision.Reason, IsError: true}
			b.next++
			continue
		}
		if err := b.r.startCall(ctx, c.req); err != nil {
			return err
		}
		c.state, c.key = callRunning, decision.Key
		if decision.Barrier && !c.req.approved {
			b.barrier = b.next
		}
		started = append(started, b.next)
		b.next++
	}
	for _, c := range b.calls[b.next:] {
		if err := b.r.deferCall(ctx, c.req); err != nil {
			return err
		}
	}
	if err := b.r.checkpoint(ctx, PhaseToolBatch, b.checkpoint()); err != nil {
		return err
	}
	for _, i := range started {
		b.launch(i)
	}
	return nil
}

func (b *batch) checkpoint() *ToolBatch {
	batch := &ToolBatch{CallIDs: make([]string, 0, len(b.calls))}
	for i, c := range b.calls {
		batch.CallIDs = append(batch.CallIDs, c.req.id)
		if i >= b.next {
			batch.DeferredIDs = append(batch.DeferredIDs, c.req.id)
		}
	}
	return batch
}

// launch runs call i on its own goroutine, which only executes the tool.
func (b *batch) launch(i int) {
	c := b.calls[i]
	name, call := c.req.name, c.call
	b.sched.Go(i, c.key, func(ctx context.Context) callOutcome {
		result, err := b.r.Tools.Execute(ctx, name, call)
		return callOutcome{result: result, err: err}
	})
}

// settle takes a finished call: one that asks for approval is requested at
// once, and a barrier that asked holds back the calls not admitted yet. A call
// that returns after the run's context stopped counts as stopped.
func (b *batch) settle(ctx context.Context, done toolsched.Done[callOutcome]) error {
	c := b.calls[done.Index]
	if err := ctx.Err(); err != nil {
		c.state = callStopped
		return err
	}
	asked, err := b.record(done)
	if err != nil {
		return err
	}
	if asked {
		if err := b.r.Approvals.Request(ctx, Pending{RunID: b.r.task.RunID, SessionID: b.r.task.SessionID, ToolCallID: c.req.id, ToolName: c.req.name, Request: *done.Value.result.Approval}); err != nil {
			return err
		}
		b.waiting = true
		b.held = b.held || done.Index == b.barrier
	}
	if done.Index == b.barrier {
		b.barrier = -1
	}
	return nil
}

// record stores a finished call's outcome and reports whether it asks for
// approval; a tool error is returned, as it fails the run.
func (b *batch) record(done toolsched.Done[callOutcome]) (bool, error) {
	c := b.calls[done.Index]
	switch {
	case errors.Is(done.Err, toolsched.ErrPanicked):
		c.state, c.result = callFinished, panicResult
	case done.Err != nil:
		c.state = callStopped
		return false, done.Err
	case done.Value.err != nil:
		c.state = callStopped
		return false, done.Value.err
	case done.Value.result.Approval != nil && !c.req.approved:
		c.state = callAsked
		return true, nil
	default:
		c.state, c.result = callFinished, done.Value.result
	}
	return false, nil
}

// journal writes the results ready so far in call order, up to the first call
// still running; a call waiting for approval has none.
func (b *batch) journal(ctx context.Context) error {
	for ; b.journaled < b.next; b.journaled++ {
		c := b.calls[b.journaled]
		switch c.state {
		case callRunning:
			return nil
		case callFinished:
			if err := b.r.finishCall(ctx, c.req, c.call, c.result); err != nil {
				return err
			}
			c.state = callJournaled
		case callRejected:
			if err := b.r.answerCall(ctx, c.req, c.result); err != nil {
				return err
			}
			c.state = callJournaled
		}
	}
	return nil
}

// drain waits for the calls still running once the batch stopped and drops
// what they return: they were stopped too.
func (b *batch) drain() {
	for b.sched.Running() > 0 {
		b.calls[b.sched.Next().Index].state = callStopped
	}
}

// keep journals, once the run's context stopped, the results of the calls that
// finished before; the others are left to crash recovery.
func (b *batch) keep(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()
	for _, c := range b.calls[b.journaled:] {
		var err error
		switch c.state {
		case callFinished:
			err = b.r.finishCall(ctx, c.req, c.call, c.result)
		case callRejected:
			err = b.r.answerCall(ctx, c.req, c.result)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
