package agent

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// maxParallelCalls bounds how many calls of one batch run at once.
const maxParallelCalls = 8

// detachedWriteTimeout bounds the writes that keep a stopped batch's results.
const detachedWriteTimeout = 5 * time.Second

// panicResult answers a call whose tool panicked.
var panicResult = tools.Result{Content: "The tool failed unexpectedly; the daemon log has the details.", Status: tools.ResultStatusError}

// canceledResult answers a call a canceled run did not finish.
var canceledResult = tools.Result{Content: "Canceled by user.", Status: tools.ResultStatusError}

// failedResult answers a call a failed run did not finish.
var failedResult = tools.Result{Content: "The run failed before this call finished.", Status: tools.ResultStatusError}

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
	req       callRequest
	call      tools.Call
	key       string
	delegated bool
	state     callState
	result    tools.Result
}

type callOutcome struct {
	result tools.Result
	ask    *tools.ApprovalRequest
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
	// delegating and working count the running delegated and other calls;
	// since is when only delegated calls began to run.
	delegating, working int
	since               time.Time
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
	b := &batch{r: r, sched: toolsched.NewBatch[callOutcome](batchCtx, r.Locks, maxParallelCalls), barrier: -1}
	defer func() {
		stop()
		b.drain()
	}()
	for _, request := range requests {
		b.calls = append(b.calls, &batchCall{req: request, call: r.toolCall(request)})
	}
	err := b.run(ctx)
	if err == nil {
		return b.waiting, nil
	}
	stop()
	b.drain()
	return false, b.stopped(ctx, err)
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
			c.state, c.result = callRejected, tools.Result{Content: decision.Reason, Status: tools.ResultStatusError}
			b.next++
			continue
		}
		if err := b.r.startCall(ctx, c.req); err != nil {
			return err
		}
		c.state, c.key, c.delegated = callRunning, decision.Key, decision.Delegated
		if decision.Barrier {
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
	b.track(c, 1)
	name, call := c.req.name, c.call
	b.sched.Go(i, c.key, func(ctx context.Context) callOutcome {
		result, ask, err := b.r.Tools.Execute(ctx, name, call)
		return callOutcome{result: result, ask: ask, err: err}
	})
}

// settle takes a finished call: one that asks for approval is requested at
// once, and a barrier that asked holds back the calls not admitted yet. A call
// that returns after the run's context stopped keeps only a result it finished with.
func (b *batch) settle(ctx context.Context, done toolsched.Done[callOutcome]) error {
	c := b.calls[done.Index]
	b.track(c, -1)
	if err := ctx.Err(); err != nil {
		c.stop(done)
		return err
	}
	asked, err := b.record(done)
	if err != nil {
		return err
	}
	if asked {
		if err := b.r.Approvals.Request(ctx, Pending{RunID: b.r.task.RunID, SessionID: b.r.task.SessionID, ToolCallID: c.req.id, ToolName: c.req.name, Request: *done.Value.ask}); err != nil {
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
	case done.Value.ask != nil:
		c.state = callAsked
		return true, nil
	default:
		c.state, c.result = callFinished, done.Value.result
		if await := c.result.Await; await != nil {
			b.r.counters.Await = &tools.Await{TaskIDs: slices.Clone(await.TaskIDs), Until: await.Until}
		}
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
			if err := b.r.finishCall(ctx, c.req, c.result); err != nil {
				return err
			}
			c.state = callJournaled
		case callRejected:
			if err := b.r.rejectCall(ctx, c.req, c.result); err != nil {
				return err
			}
			c.state = callJournaled
		}
	}
	return nil
}

// drain waits for the calls still running once the batch stopped; each keeps
// only a result it finished with.
func (b *batch) drain() {
	for b.sched.Running() > 0 {
		done := b.sched.Next()
		c := b.calls[done.Index]
		b.track(c, -1)
		c.stop(done)
	}
}

// stop settles a call that returned once its batch stopped: a result it
// finished with stands; a failure, likely the stop's own, counts as stopped.
func (c *batchCall) stop(done toolsched.Done[callOutcome]) {
	if done.Err == nil && done.Value.err == nil && done.Value.ask == nil && done.Value.result.Status != tools.ResultStatusError {
		c.state, c.result = callFinished, done.Value.result
		return
	}
	c.state = callStopped
}

// stopped journals, once the batch stopped on err, the results of the calls
// that finished before; a failed or canceled run also answers every other call,
// an interrupted one leaves them to recovery.
func (b *batch) stopped(ctx context.Context, err error) error {
	interrupted, byUser := ctx.Err(), canceled(ctx)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachedWriteTimeout)
	defer cancel()
	rest := &failedResult
	if interrupted != nil {
		err, rest = interrupted, nil
		if byUser {
			rest = &canceledResult
		}
	}
	return errors.Join(err, b.keep(ctx, rest))
}

// keep journals the calls of a stopped batch that have no result yet: finished
// and rejected ones with theirs, the others with rest unless it is nil.
func (b *batch) keep(ctx context.Context, rest *tools.Result) error {
	for _, c := range b.calls {
		if b.r.history.hasResult(c.req.id) {
			continue
		}
		var err error
		switch {
		case c.state == callFinished:
			err = b.r.finishCall(ctx, c.req, c.result)
		case c.state == callRejected:
			err = b.r.rejectCall(ctx, c.req, c.result)
		case rest == nil:
			continue
		case c.state == callWaiting:
			err = b.r.rejectCall(ctx, c.req, *rest)
		default:
			err = b.r.finishCall(ctx, c.req, *rest)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// track counts a call that starts (+1) or ends (-1); the time in which only
// delegated calls run belongs to the agents they run, not to this run.
func (b *batch) track(c *batchCall, delta int) {
	now := b.r.Now()
	if !b.since.IsZero() {
		b.r.delegated += now.Sub(b.since)
		b.since = time.Time{}
	}
	if c.delegated {
		b.delegating += delta
	} else {
		b.working += delta
	}
	if b.delegating > 0 && b.working == 0 {
		b.since = now
	}
}
