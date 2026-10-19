package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseAgentFile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file, text string
		want             AgentProfile
		wantErr          bool
	}{
		{
			name: "list", file: "/a/db-expert.md",
			text: "---\ndescription: knows   the schema\ntools: [Read, grep, read]\nreadonly: true\nmodel: small-model\nbudget_steps: 20\ncolor: blue\n---\nYou know the database.\n",
			want: AgentProfile{Name: "db-expert", Description: "knows the schema", Prompt: "You know the database.", Tools: []string{"read", "grep"}, Readonly: true, Model: "small-model", BudgetSteps: 20, Source: "/a/db-expert.md"},
		},
		{
			name: "comma tools", file: "/a/x.md",
			text: "\ufeff---\r\nname: Docs\r\ntools: Read, Glob\r\nmodel: inherit\r\n---\r\nWrite docs.\r\n",
			want: AgentProfile{Name: "docs", Description: "Write docs.", Prompt: "Write docs.", Tools: []string{"read", "glob"}, Source: "/a/x.md"},
		},
		{name: "no frontmatter", file: "/a/x.md", text: "Just a prompt.", wantErr: true},
		{name: "unclosed", file: "/a/x.md", text: "---\nname: x\nprompt", wantErr: true},
		{name: "bad name", file: "/a/x.md", text: "---\nname: ../evil\n---\nhi", wantErr: true},
		{name: "empty prompt", file: "/a/x.md", text: "---\nname: x\n---\n\n", wantErr: true},
		{name: "tools map", file: "/a/x.md", text: "---\ntools: {read: true}\n---\nhi", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAgentFile(tc.file, []byte(tc.text))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parsed %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tc.want.Name || got.Description != tc.want.Description || got.Prompt != tc.want.Prompt || !slices.Equal(got.Tools, tc.want.Tools) ||
				got.Readonly != tc.want.Readonly || got.Model != tc.want.Model || got.BudgetSteps != tc.want.BudgetSteps || got.Source != tc.want.Source {
				t.Fatalf("parsed %+v, want %+v", got, tc.want)
			}
		})
	}
}

func writeAgentFile(t *testing.T, dir string, name string, text string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentProfilesLayerUserAndProjectFiles(t *testing.T) {
	t.Parallel()
	home, project := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	userDir, projectDir := filepath.Join(home, "agents"), filepath.Join(project, agentsSubdir)
	writeAgentFile(t, userDir, "explore.md", "---\nreadonly: true\nmodel: cheap\ntools: [read]\n---\nSearch cheaply.")
	writeAgentFile(t, userDir, "shared.md", "---\ndescription: from the user\n---\nUser version.")
	writeAgentFile(t, projectDir, "shared.md", "---\ndescription: from the project\n---\nProject version.")
	writeAgentFile(t, projectDir, "plan.md", "---\nreadonly: false\n---\nI plan and edit.")
	writeAgentFile(t, projectDir, "broken.md", "no frontmatter")
	c := New(Config{PromptEnvironment: PromptEnvironment{AgentsDir: userDir}})
	sub := filepath.Join(project, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	names := func(profiles []AgentProfile) string { return strings.Join(agentTypeNames(profiles), ",") }
	if got := names(c.agentProfiles(sub, false)); got != "general,explore,plan,review,verify" {
		t.Fatalf("without user agents: %s", got)
	}
	profiles := c.agentProfiles(sub, true)
	if got := names(profiles); got != "general,explore,plan,review,verify,shared" {
		t.Fatalf("with user agents: %s", got)
	}
	explore, _ := c.agentProfile(sub, true, "explore")
	if explore.Model != "cheap" || !slices.Equal(explore.Tools, []string{"read"}) {
		t.Fatalf("the user's explore = %+v, want it to replace the builtin", explore)
	}
	plan, _ := c.agentProfile(sub, true, "plan")
	if plan.Source != builtinAgentSource || !plan.Readonly {
		t.Fatalf("plan = %+v, want the builtin: a project cannot redefine it", plan)
	}
	shared, _ := c.agentProfile(sub, true, "shared")
	if shared.Prompt != "Project version." {
		t.Fatalf("shared = %+v, want the project's", shared)
	}
	if _, ok := c.agentProfile(sub, false, "shared"); ok {
		t.Fatal("a user type is offered without user agents")
	}
	if general, ok := c.agentProfile(sub, false, ""); !ok || general.Name != defaultAgentType || !general.allows("anything") {
		t.Fatalf("the default type = %+v, %v", general, ok)
	}
}

func TestBuiltinAgentToolAllowlists(t *testing.T) {
	t.Parallel()
	byName := map[string]AgentProfile{}
	for _, profile := range builtinAgentProfiles(true) {
		byName[profile.Name] = profile
	}
	for name, want := range map[string]struct {
		readonly bool
		allows   []string
		denies   []string
	}{
		"explore": {true, []string{"read", "grep", "glob", "ls", "bash"}, []string{"write", "edit", "web_fetch", checkpointDiffToolName}},
		"plan":    {true, []string{"read", "bash", checkpointDiffToolName}, []string{"write", "edit", "multiedit"}},
		"review":  {true, []string{"read", "bash", checkpointDiffToolName}, []string{"write", "edit", "restore"}},
		"verify":  {false, []string{"read", "bash", "task_output", "todo_write"}, []string{"write", "edit", "multiedit", "restore"}},
	} {
		profile := byName[name]
		if profile.Readonly != want.readonly {
			t.Errorf("%s readonly = %v", name, profile.Readonly)
		}
		for _, tool := range want.allows {
			if !profile.allows(tool) {
				t.Errorf("%s does not allow %s", name, tool)
			}
		}
		for _, tool := range want.denies {
			if profile.allows(tool) {
				t.Errorf("%s allows %s", name, tool)
			}
		}
	}
	for _, profile := range builtinAgentProfiles(false) {
		if profile.allows(checkpointDiffToolName) && len(profile.Tools) > 0 {
			t.Errorf("%s offers checkpoint_diff without it", profile.Name)
		}
	}
}
