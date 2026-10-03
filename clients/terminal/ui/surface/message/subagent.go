package message

// SubagentState is where a child agent is in its work.
type SubagentState string

const (
	SubagentPending         SubagentState = "pending"
	SubagentRunning         SubagentState = "running"
	SubagentWaitingApproval SubagentState = "waiting_approval"
	SubagentCompleted       SubagentState = "completed"
	SubagentFailed          SubagentState = "failed"
	SubagentCanceled        SubagentState = "canceled"
)

// Active reports whether the child still works or waits.
func (s SubagentState) Active() bool {
	return s == SubagentPending || s == SubagentRunning || s == SubagentWaitingApproval
}

// Subagent is a child agent as the terminal shows it.
type Subagent struct {
	ID string
	// Name is the agent's name, never empty.
	Name             string
	Task             string
	Goal             string
	Runtime          string
	State            SubagentState
	Summary          string
	Error            string
	Blocking         bool
	ParentRunID      string
	ParentToolCallID string
}
