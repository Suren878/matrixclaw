package telegram

import (
	"github.com/Suren878/matrixclaw/internal/controlplane"
)

// dispatcher is the chat's command dispatcher for the role its updates come
// with; it is kept, with its open forms, while the chat is among the recent.
func (w *Worker) dispatcher(target chatTarget) *controlplane.Dispatcher {
	daemon := w.daemonFor(target, target.externalKey)
	key := target.externalKey + "\x00" + string(daemon.Role)
	w.mu.Lock()
	defer w.mu.Unlock()
	if dispatcher, ok := w.dispatchers.get(key); ok {
		return dispatcher
	}
	dispatcher := controlplane.New(daemon, "")
	w.dispatchers.put(key, dispatcher)
	return dispatcher
}
