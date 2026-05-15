package runtime

import "github.com/Suren878/matrixclaw/internal/core"

func (m *appModel) shouldPromptPlanResume(snapshot core.ClientSnapshot) bool {
	if m.planResumePrompted || m.busy {
		return false
	}
	if snapshot.Run != nil && runIsActive(snapshot.Run) {
		return false
	}
	return planHasOpenWork(snapshot.Plan)
}

func shouldShowStoredPlanSummary(snapshot core.ClientSnapshot) bool {
	if snapshot.Run != nil && runIsActive(snapshot.Run) {
		return false
	}
	plan := snapshot.Plan
	if plan == nil || planHasOpenWork(plan) {
		return false
	}
	return len(plan.Items) > 0 || plan.Goal != ""
}
