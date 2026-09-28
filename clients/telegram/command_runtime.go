package telegram

import (
	"github.com/Suren878/matrixclaw/internal/clientruntime"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (w *Worker) dispatcher(target chatTarget) *controlplane.Dispatcher {
	runtime := clientruntime.ControlplaneRuntime{
		Client:     w.config.ClientName,
		WorkingDir: w.config.WorkingDir,
		Daemon: func(externalKey string) (*daemonclient.Client, error) {
			return w.daemon(externalKey), nil
		},
		Owner: w.ownerChat(target),
		Guest: !target.isChat(),
	}
	return controlplane.New(runtime, w.config.WorkingDir)
}
