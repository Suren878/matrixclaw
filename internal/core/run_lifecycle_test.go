package core

import "testing"

func TestRunStatusEdges(t *testing.T) {
	t.Parallel()
	statuses := []RunStatus{RunStatusAccepted, RunStatusRunning, RunStatusWaitingApproval, RunStatusWaitingEvents, RunStatusCompleted, RunStatusFailed, RunStatusCanceled}
	allowed := map[RunStatus][]RunStatus{
		RunStatusAccepted:        {RunStatusRunning, RunStatusWaitingApproval, RunStatusFailed, RunStatusCanceled},
		RunStatusRunning:         {RunStatusWaitingApproval, RunStatusWaitingEvents, RunStatusCompleted, RunStatusAccepted, RunStatusFailed, RunStatusCanceled},
		RunStatusWaitingApproval: {RunStatusRunning, RunStatusAccepted, RunStatusFailed, RunStatusCanceled},
		RunStatusWaitingEvents:   {RunStatusRunning, RunStatusFailed, RunStatusCanceled},
	}
	for _, from := range statuses {
		for _, to := range statuses {
			want := false
			for _, ok := range allowed[from] {
				want = want || ok == to
			}
			if got := from.canBecome(to); got != want {
				t.Errorf("%s -> %s allowed = %v, want %v", from, to, got, want)
			}
		}
	}
}
