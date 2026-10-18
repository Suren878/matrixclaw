package core

import "strings"

// Tuning is the daemon's run limits from setup.json; a zero field keeps the
// default.
type Tuning struct {
	Budgets RunBudgets
	// CompactProvider and CompactModel are the summary model; empty is the
	// run's own model.
	CompactProvider  string
	CompactModel     string
	WindowCap        int
	ModelConcurrency int
	BackgroundTasks  int
	BackgroundAgents int
}

// SetTuning applies tuning at once and is safe at any time: runs read the
// limits as they go, and the model slots resize in place.
func (c *Core) SetTuning(tuning Tuning) {
	if tuning.Budgets == (RunBudgets{}) {
		tuning.Budgets = DefaultRunBudgets()
	}
	tuning.CompactProvider = strings.TrimSpace(tuning.CompactProvider)
	tuning.CompactModel = strings.TrimSpace(tuning.CompactModel)
	if tuning.WindowCap <= 0 {
		tuning.WindowCap = DefaultContextWindowCap
	}
	if tuning.ModelConcurrency <= 0 {
		tuning.ModelConcurrency = DefaultModelConcurrency
	}
	if tuning.BackgroundTasks <= 0 {
		tuning.BackgroundTasks = DefaultBackgroundTasks
	}
	if tuning.BackgroundAgents <= 0 {
		tuning.BackgroundAgents = DefaultBackgroundAgents
	}
	c.effectiveTuning.Store(&tuning)
	c.modelSlots.Resize(tuning.ModelConcurrency)
}

// Tuning is the run limits the daemon follows now.
func (c *Core) Tuning() Tuning {
	return *c.effectiveTuning.Load()
}
