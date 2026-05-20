package runtime

import "github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"

func (m *appModel) currentSnapshot() viewmodel.Snapshot {
	if m.read == nil {
		return viewmodel.Snapshot{}
	}
	return m.read.Snapshot()
}
