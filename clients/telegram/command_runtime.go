package telegram

import "github.com/Suren878/matrixclaw/internal/controlplane"

func (w *Worker) dispatcher(target chatTarget) *controlplane.Dispatcher {
	return controlplane.New(w.daemonFor(target, target.externalKey), "")
}
