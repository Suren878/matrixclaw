package prompt

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/Suren878/matrixclaw/internal/workspace"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var (
	parentTools   = NewTools("read", "glob", "grep", "ls", "repo_map", "lsp", "write", "edit", "multiedit", "bash", "task_output", "task_kill", "todo_write", "await", "agent", "web_fetch", "web_search")
	writerTools   = NewTools("read", "glob", "grep", "ls", "write", "edit", "multiedit", "bash", "task_output", "task_kill", "todo_write", "web_fetch", "web_search")
	readonlyTools = NewTools("read", "glob", "grep", "ls", "repo_map", "bash", "task_output", "todo_write", "web_fetch", "web_search")
)

// golden compares got with testdata/name.golden; -update rewrites it. The
// files are the prompt texts as a model reads them, for review in diffs.
func golden(t *testing.T, name string, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if got+"\n" != string(want) {
		t.Fatalf("%s differs from %s (run go test -update and review the diff):\n%s", name, path, got)
	}
}

func TestGoldenPrompts(t *testing.T) {
	git := &workspace.Git{
		Branch:     "branch main (upstream origin/main, ahead 1)",
		Status:     []string{" M internal/foo.go", "?? notes.txt"},
		MoreStatus: 3,
		Commits:    []string{"a1b2c3d fix(parser): handle empty input"},
	}
	almaty, err := time.LoadLocation("Asia/Almaty")
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"coding_parent": JoinSections(
			Persona("matrixclaw", "", true),
			CodingGuidance(parentTools, false),
			Environment("/home/user/project", "/home/user/project", "linux/amd64 (Ubuntu 24.04 LTS)"),
			ProjectInstructions([]workspace.InstructionFile{{Path: "/home/user/AGENTS.md", Content: "Use tabs."}, {Path: "/home/user/project/AGENTS.md", Content: "Run make test.", Truncated: true}}),
			WebGuidance(parentTools),
			Subagents(SubagentsInfo{
				Coding: true, Runtimes: []string{"matrixclaw", "codex"},
				External:   []string{"codex: available (Codex; models: gpt-5.1-codex)", "claude: unavailable (not installed)"},
				Types:      []AgentType{{"general", "every child tool"}, {"explore", "read-only search"}},
				Background: 4, Steps: 100,
			}),
		),
		"coding_writer_child":   JoinSections(SubagentPersona(true, false), CodingGuidance(writerTools, true), SubagentMode(writerTools, false)),
		"coding_readonly_child": JoinSections(SubagentPersona(true, true), CodingGuidance(readonlyTools, true), SubagentMode(readonlyTools, true), SubagentType("review", ReviewAgent(true))),
		"chat": JoinSections(
			Persona("Jarvis", "", false),
			ToolUseDiscipline(NewTools("todo_write", "web_fetch", "web_search"), false),
			Subagents(SubagentsInfo{Runtimes: []string{"matrixclaw"}, Background: 4, Steps: 100}),
			Persona("Jarvis", "Answer like a pirate.", false),
		),
		"run_context": RunContext{Now: time.Date(2026, 10, 18, 14, 5, 0, 0, almaty), Client: "terminal:local", PermissionMode: "default", Git: git}.Text(),
	} {
		t.Run(name, func(t *testing.T) { golden(t, name, text) })
	}
}

func TestGuidanceNamesOnlyToolsTheRunHas(t *testing.T) {
	// Tool names that are not also plain words (read, edit, agent, ...).
	names := []string{"glob", "repo_map", "lsp", "multiedit", "bash", "task_output", "task_kill", "todo_write", "await", "web_fetch"}
	for name, tools := range map[string]Tools{"writer": writerTools, "readonly": readonlyTools, "files only": NewTools("read", "grep")} {
		texts := []string{CodingGuidance(tools, true), SubagentMode(tools, name == "readonly"), ToolUseDiscipline(tools, true)}
		for _, tool := range names {
			if tools.Has(tool) {
				continue
			}
			pattern := regexp.MustCompile(`\b` + tool + `\b`)
			for _, text := range texts {
				if pattern.MatchString(text) {
					t.Fatalf("%s guidance names %s, which the run lacks:\n%s", name, tool, text)
				}
			}
		}
	}
}

func TestPatchToolsGetTheGuidanceOfEditTools(t *testing.T) {
	patchTools := NewTools("read", "glob", "grep", "ls", "write", "apply_patch", "bash", "task_output", "task_kill", "todo_write", "web_fetch", "web_search")
	patch, edit := CodingGuidance(patchTools, true), CodingGuidance(writerTools, true)
	if sections := strings.Count(patch, "\n\n"); sections != strings.Count(edit, "\n\n") || len(strings.Split(patch, "\n")) != len(strings.Split(edit, "\n")) {
		t.Fatalf("apply_patch guidance:\n%s\n\nedit guidance:\n%s", patch, edit)
	}
}
