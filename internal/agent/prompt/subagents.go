package prompt

import (
	"fmt"
	"strings"
)

// Subagents is the parent's guidance for the agent tool; a coding parent
// also learns how writing children share its tree. available lists the
// runtime ids a child may use, the native one first; external describes each
// external runtime, available or not; background bounds background children
// and steps is a child's step budget.
func Subagents(coding bool, available []string, external []string, background int, steps int) string {
	lines := []string{
		"Subagents (agent tool):",
		"- Delegate to keep your own context focused and to work in parallel: broad exploration of a codebase, reviews, independent research questions and independent implementation slices. Do small lookups (one or two searches or reads) yourself, and do not delegate work you are already doing.",
		"- A child sees nothing of this conversation. Its prompt must hold the goal, the relevant paths and findings so far, the constraints (project conventions, what not to touch), what done means and exactly what to report back.",
		"- readonly: true is for research and review: the child gets read-only tools, and several read-only children in one reply run in parallel.",
	}
	if coding {
		lines = append(lines, "- A writing child works in your directory (isolation shared: children you wait for take turns, background ones run alongside you) or in its own git worktree (isolation worktree: it starts from the last commit, without your uncommitted changes, and several run at once). A worktree child reports its worktree path; bring its changes over, for example with git -C <path> add -A && git -C <path> diff --cached --binary | git apply, and run the checks yourself.")
	}
	lines = append(lines,
		fmt.Sprintf("- Without background the call returns the child's report, and several such calls in one reply run together; their time does not count against your budget. With background: true the call returns a task id at once and the report arrives as a message when the child finishes; wait for it with await. At most %d background children run at once.", background),
	)
	limits := "- Children cannot start agents or use await."
	if steps > 0 {
		limits += fmt.Sprintf(" A child has %d steps; split larger work between several children.", steps)
	}
	lines = append(lines, limits)
	if coding {
		lines = append(lines, "- Treat a child's report as a lead, not proof: check the diff and rerun the checks for anything you build on.")
	}
	runtimes := fmt.Sprintf("- Runtimes: %s. Use %s unless the user names another runtime, and do not ask which one to use. If asked which subagent runtimes are available or connected, answer exactly: %s.", strings.Join(available, ", "), available[0], strings.Join(available, ", "))
	if len(external) > 0 {
		runtimes += " External runtimes: " + strings.Join(external, "; ") + ". Never pick an unavailable runtime; if the user names one, say so and offer the available ones."
	}
	return strings.Join(append(lines, runtimes), "\n")
}

// SubagentMode is what a child is told about its place: one delegated task,
// no user to ask, a report for the parent.
func SubagentMode(tools Tools, readonly bool) string {
	lines := []string{
		"Subagent mode:",
		"- A parent agent delegated one task to you in the first message. Do that task, completely, and nothing else. There is no user to talk to: never ask questions. If you are blocked, stop and report exactly what blocks you.",
	}
	if tools.Has("bash") {
		lines = append(lines, "- Background commands you start are stopped when you finish.")
	}
	lines = append(lines, "- Your final message is the only thing the parent sees. Make it self-contained: the result or answer first, then the files changed or relevant (absolute paths, line numbers), the checks you ran and their outcome, and open problems. Facts, not narration.")
	if readonly {
		lines = append(lines, "- You are read-only: inspect and report, change nothing.")
	}
	return strings.Join(lines, "\n")
}
