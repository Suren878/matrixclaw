package controlplane

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestParseGoalInput(t *testing.T) {
	for args, want := range map[string]core.GoalInput{
		"Port the parser to Go":                                        {Objective: "Port the parser to Go"},
		`Make CI green --check "go test ./..." --max-runs 20`:          {Objective: "Make CI green", CheckCommand: "go test ./...", MaxRuns: 20},
		`Fix --verbose flag handling --tokens 500000 --check 'make t'`: {Objective: "Fix --verbose flag handling", CheckCommand: "make t", TokenBudget: 500000},
	} {
		if got, ok := parseGoalInput(args); !ok || got != want {
			t.Errorf("parseGoalInput(%q) = %+v, %v; want %+v", args, got, ok, want)
		}
	}
	for _, args := range []string{"--check make", `Ship it --check "go test`, "Ship it --max-runs many", "Ship it --max-runs", "Ship it --checks x"} {
		if got, ok := parseGoalInput(args); ok {
			t.Errorf("parseGoalInput(%q) = %+v, want a usage error", args, got)
		}
	}
}
