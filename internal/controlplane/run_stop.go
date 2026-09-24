package controlplane

import "github.com/Suren878/matrixclaw/internal/agent"

// StopNotice explains why a completed run stopped before its work was done; ""
// when it finished normally. Clients add how to continue.
func StopNotice(reason agent.StopReason) string {
	if !reason.Continuable() {
		return ""
	}
	if reason == agent.StopLoopDetected {
		return "The run stopped because it kept repeating the same step."
	}
	return "The run stopped at its budget."
}
