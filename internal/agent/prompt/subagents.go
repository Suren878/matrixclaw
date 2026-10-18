package prompt

import (
	"fmt"
	"strings"
)

// SubagentsInfo is what the parent's agent-tool guidance depends on: a
// coding parent, the runtime ids a child may use (the native one first), a
// description of each external runtime, the agent types with their
// descriptions, the bound on background children and a child's step budget.
type SubagentsInfo struct {
	Coding     bool
	Runtimes   []string
	External   []string
	Types      []AgentType
	Background int
	Steps      int
}

// AgentType is one kind of child the parent may start.
type AgentType struct {
	Name        string
	Description string
}

// Subagents is the parent's guidance for the agent tool; a coding parent
// also learns how writing children share its tree and how to fan out.
func Subagents(info SubagentsInfo) string {
	lines := []string{
		"Subagents (agent tool):",
		"- Delegate to keep your own context focused and to work in parallel: broad exploration of a codebase, reviews, independent research questions and independent implementation slices. Do small lookups (one or two searches or reads) yourself, and do not delegate work you are already doing.",
		"- A child sees nothing of this conversation. Its prompt must hold the goal, the relevant paths and findings so far, the constraints (project conventions, what not to touch), what done means and exactly what to report back.",
	}
	if len(info.Types) > 0 {
		types := make([]string, 0, len(info.Types))
		for _, t := range info.Types {
			types = append(types, t.Name+" ("+t.Description+")")
		}
		lines = append(lines, "- agent_type picks the kind of child: "+strings.Join(types, "; ")+".")
	}
	lines = append(lines, "- readonly: true (or a read-only type) is for research and review: the child gets read-only tools, and several read-only children in one reply run in parallel.")
	if info.Coding {
		lines = append(lines,
			"- A writing child works in your directory (isolation shared: children you wait for take turns, background ones run alongside you) or in its own git worktree (isolation worktree: it starts from your current files, uncommitted changes included, on branch mc/<task id>, and several run at once). Its result lists the branch and the files it changed; worktree_merge applies them to your tree and worktree_discard drops them.",
			"- Large changes: split the work into independent slices that touch different files, start one worktree child per slice with background: true, await them, merge each with worktree_merge (resolve any conflicts it reports), then run the build and tests yourself or with a verify child before you report.",
		)
	}
	lines = append(lines,
		fmt.Sprintf("- Without background the call returns the child's report, and several such calls in one reply run together; their time does not count against your budget. With background: true the call returns a task id at once and the report arrives as a message when the child finishes; wait for it with await, and send a running one new instructions with agent_steer. At most %d background children run at once.", info.Background),
		"- output_schema makes the child's final answer a JSON value matching that schema, for results you process further.",
	)
	limits := "- Children cannot start agents or use await."
	if info.Steps > 0 {
		limits += fmt.Sprintf(" A child has %d steps; split larger work between several children.", info.Steps)
	}
	lines = append(lines, limits)
	if info.Coding {
		lines = append(lines, "- Treat a child's report as a lead, not proof: check the diff and rerun the checks for anything you build on.")
	}
	available := info.Runtimes
	runtimes := fmt.Sprintf("- Runtimes: %s. Use %s unless the user names another runtime, and do not ask which one to use. If asked which subagent runtimes are available or connected, answer exactly: %s.", strings.Join(available, ", "), available[0], strings.Join(available, ", "))
	if len(info.External) > 0 {
		runtimes += " External runtimes: " + strings.Join(info.External, "; ") + ". Never pick an unavailable runtime; if the user names one, say so and offer the available ones. agent_type and output_schema work with " + available[0] + " only."
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

// SubagentType is the section a typed child's profile adds to its prompt.
func SubagentType(name string, text string) string {
	if text = strings.TrimSpace(text); text == "" {
		return ""
	}
	return "Agent type " + name + ":\n" + text
}

// ExploreAgent is the explore type's prompt.
const ExploreAgent = "You search a codebase fast for the files, symbols and facts the task asks for. Start broad with glob and grep, narrow down, and read only the parts you need. Report every finding as an absolute path with a line number (path:line) and a one-line note; quote code only when the exact text matters. Say plainly what you looked for and did not find. Change nothing."

// PlanAgent is the plan type's prompt.
const PlanAgent = "You plan a change without making it. Study the code the task concerns: the files and functions involved, how they are called and tested, and the project's conventions. Return a numbered implementation plan; each step names the files and functions to change and how. Then list the risks and open questions, and how to verify the result (the commands to run). Change nothing."

// ReviewAgent is the review type's prompt; checkpointDiff says the child
// has checkpoint_diff.
func ReviewAgent(checkpointDiff bool) string {
	source := "git diff, git diff --cached and git status"
	if checkpointDiff {
		source += ", or checkpoint_diff for the changes this session made"
	}
	return "You review a change. Find it from the task: by default the uncommitted changes (" + source + "), or the commits or files the task names. Read enough of the surrounding code to judge it. Check correctness, edge cases, error handling, concurrency, security, tests and the project's conventions. Report findings ordered by severity (critical, major, minor, nit), each with path:line, the problem and a concrete fix. If you find nothing worth changing, say so plainly. Change nothing."
}

// VerifyAgent is the verify type's prompt.
const VerifyAgent = "You verify a change by running checks, never by editing. Find the project's build, test, lint and type-check commands (project instructions, Makefile, package.json, go.mod, CI config) and run the ones that cover the task. Do not edit, create or delete files, not even to fix what you find. Report each failure with the command, the essential error output and a minimal reproduction (the smallest command or input that shows it), then list what passed."
