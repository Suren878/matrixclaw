package controlplane

import "github.com/Suren878/matrixclaw/internal/agent"

// StopNotice explains why a completed run stopped before its work was done; ""
// when it finished normally.
func StopNotice(reason agent.StopReason) string {
	switch reason {
	case agent.StopBudgetExhausted:
		return "The run stopped at its budget."
	case agent.StopLoopDetected:
		return "The run stopped because it kept repeating the same step."
	default:
		return ""
	}
}
