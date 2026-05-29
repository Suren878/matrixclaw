// Package safego runs background goroutines with panic recovery.
//
// matrixclawd is a long-running daemon. A panic in any unguarded background
// goroutine (scheduler ticks, process readers, supervisors, fire-and-forget
// run execution, etc.) would otherwise unwind to the top of that goroutine
// and crash the entire process, taking down every active session. Wrapping
// those goroutines with Go/Run isolates the panic: it is recovered, logged
// with a stack trace, and the rest of the daemon keeps serving.
package safego

import (
	"log"
	"runtime/debug"
)

// Go starts fn in a new goroutine with panic recovery. name identifies the
// goroutine in recovery logs. Use it everywhere a raw `go func(){...}()` would
// otherwise be used for background work in the daemon.
func Go(name string, fn func()) {
	go Run(name, fn)
}

// Run executes fn synchronously with panic recovery. It returns false if fn
// panicked (the panic is recovered and logged), true otherwise. Useful for
// guarding a single unit of work that already runs on its own goroutine.
func Run(name string, fn func()) (completed bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("safego: recovered panic in %q: %v\n%s", name, r, debug.Stack())
		}
	}()
	fn()
	return true
}
