package core

import (
	"strings"
	"testing"
)

func TestPlanToolResultContentKeepsMutationResultsCompact(t *testing.T) {
	t.Parallel()

	plan := SessionPlan{
		Goal: "ship it",
		Items: []PlanItem{{
			ID:     "item_1",
			Text:   "first task",
			Status: PlanItemPending,
		}},
	}

	content := planToolResultContent(planUpdateToolName, plan)
	if strings.Contains(content, "first task") || strings.Contains(content, "item_1") {
		t.Fatalf("plan update content = %q, want compact result without full plan", content)
	}
	if !strings.Contains(content, "Plan updated") {
		t.Fatalf("plan update content = %q, want update acknowledgement", content)
	}
}

func TestPlanToolResultContentReturnsFullPlanForPlanGet(t *testing.T) {
	t.Parallel()

	plan := SessionPlan{
		Goal: "ship it",
		Items: []PlanItem{{
			ID:     "item_1",
			Text:   "first task",
			Status: PlanItemPending,
		}},
	}

	content := planToolResultContent(planGetToolName, plan)
	if !strings.Contains(content, "ship it") || !strings.Contains(content, "first task") || !strings.Contains(content, "item_1") {
		t.Fatalf("plan get content = %q, want full plan summary", content)
	}
}
