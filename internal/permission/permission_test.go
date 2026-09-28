package permission

import "testing"

func rule(effect Effect, tool, pattern string) Rule {
	return Rule{Tool: tool, Pattern: pattern, Effect: effect, Scope: ScopeSession}
}

func command(line string) Request {
	return Request{Tool: "bash", Subject: Subject{Kind: KindCommand, Value: line}}
}

func file(tool, path string) Request {
	return Request{Tool: tool, Subject: Subject{Kind: KindFile, Value: path}}
}

func TestEvaluateOrdersDenyAskAllowThenPreset(t *testing.T) {
	for _, tc := range []struct {
		name   string
		req    Request
		rules  []Rule
		preset []Rule
		want   Effect
		rule   string
	}{
		{"no rule leaves the tool default", command("go test ./..."), nil, nil, "", ""},
		{"allow by prefix", command("go test ./..."), []Rule{rule(Allow, "bash", "go test:*")}, nil, Allow, "bash: go test:*"},
		{"exact pattern needs the exact command", command("git status -s"), []Rule{rule(Allow, "bash", "git status")}, nil, "", ""},
		{"deny beats allow", command("rm -rf /tmp/x"), []Rule{rule(Allow, "bash", "*"), rule(Deny, "bash", "rm:*")}, nil, Deny, "bash: rm:*"},
		{"ask beats allow", file("read", "/home/u/.env"), []Rule{rule(Allow, "read", "/home/u/**"), rule(Ask, "read", "/home/u/.env")}, nil, Ask, "read: /home/u/.env"},
		{"deny beats ask", file("read", "/home/u/.ssh/id"), []Rule{rule(Ask, "read", "/home/u/**"), rule(Deny, "read", "/home/u/.ssh/**")}, nil, Deny, "read: /home/u/.ssh/**"},
		{"deny inside a substitution", command("echo $(rm -rf /)"), []Rule{rule(Deny, "bash", "rm:*")}, nil, Deny, "bash: rm:*"},
		{"every simple command must be allowed", command("go test ./... && rm x"), []Rule{rule(Allow, "bash", "go test:*")}, nil, "", ""},
		{"all simple commands allowed", command("go vet ./... && go test ./..."), []Rule{rule(Allow, "bash", "go vet:*"), rule(Allow, "bash", "go test:*")}, nil, Allow, "bash: go vet:*"},
		{"redirect to a file falls back", command("go test ./... > out.txt"), []Rule{rule(Allow, "bash", "go test:*")}, nil, "", ""},
		{"leading assignment falls back", command("CGO_ENABLED=0 go test ./..."), []Rule{rule(Allow, "bash", "go test:*")}, nil, "", ""},
		{"exec flag falls back", command("find . -exec rm {} ;"), []Rule{rule(Allow, "bash", "find:*")}, nil, "", ""},
		{"a rule for the whole tool allows risky lines", command("echo x > out.txt"), []Rule{rule(Allow, "bash", "")}, nil, Allow, "bash"},
		{"preset after rules", file("edit", "/work/a.go"), nil, Preset(ModeAcceptEdits, "/work"), Allow, "edit: /work/**"},
		{"preset stops at the root", file("edit", "/etc/passwd"), nil, Preset(ModeAcceptEdits, "/work"), "", ""},
		{"rules before the preset", command("rm -rf /"), []Rule{rule(Deny, "bash", "rm:*")}, Preset(ModeFullAuto, "/work"), Deny, "bash: rm:*"},
		{"full auto allows the rest", command("echo x > out.txt"), nil, Preset(ModeFullAuto, "/work"), Allow, "*"},
		{"rules name their tool", file("read", "/work/a.go"), []Rule{rule(Deny, "edit", "/work/**")}, nil, "", ""},
		{"tool star covers every tool", file("read", "/work/a.go"), []Rule{rule(Deny, "*", "")}, nil, Deny, "*"},
		{"subject-less call matches only whole-tool rules", Request{Tool: "memory"}, []Rule{rule(Deny, "memory", "x*"), rule(Allow, "memory", "")}, nil, Allow, "memory"},
		{"domain without case", Request{Tool: "web_fetch", Subject: Subject{Kind: KindDomain, Value: "Docs.Example.com"}}, []Rule{rule(Deny, "web_fetch", "*.example.com")}, nil, Deny, "web_fetch: *.example.com"},
		{"mcp name glob", Request{Tool: "mcp", Subject: Subject{Kind: KindName, Value: "browser__browser_click"}}, []Rule{rule(Allow, "mcp", "browser__*")}, nil, Allow, "mcp: browser__*"},
	} {
		got := Evaluate(tc.req, tc.rules, tc.preset)
		if got.Effect != tc.want || (tc.want != "" && got.Rule.String() != tc.rule) {
			t.Errorf("%s: verdict = %s by %q, want %s by %q", tc.name, got.Effect, got.Rule.String(), tc.want, tc.rule)
		}
	}
}

func TestPathGlobs(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"/work/**", "/work", true},
		{"/work/**", "/work/a/b.go", true},
		{"/work/**", "/workshop/a.go", false},
		{"/work/*.go", "/work/a.go", true},
		{"/work/*.go", "/work/sub/a.go", false},
		{"/work/**/*.go", "/work/a.go", true},
		{"/work/**/*.go", "/work/sub/deep/a.go", true},
		{"/work/?.go", "/work/a.go", true},
		{"/work/?.go", "/work/ab.go", false},
		{"/home/ü/**", "/home/ü/x", true},
	} {
		if got := matchSubject(tc.pattern, Subject{Kind: KindFile, Value: tc.path}); got != tc.want {
			t.Errorf("%q vs %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestSuggestNamesTheNarrowRule(t *testing.T) {
	for _, tc := range []struct {
		req  Request
		want string
		ok   bool
	}{
		{command("go test ./internal/..."), "bash: go test:*", true},
		{command("git status"), "bash: git status:*", true},
		{command("ls -la"), "bash: ls:*", true},
		{command("cat notes.txt"), "bash: cat:*", true},
		{command("go test ./... && rm x"), "", false},
		{command("echo x > out.txt"), "", false},
		{file("edit", "/work/internal/a.go"), "edit: /work/internal/**", true},
		{Request{Tool: "grep", Subject: Subject{Kind: KindDirectory, Value: "/work"}}, "grep: /work/**", true},
		{Request{Tool: "web_fetch", Subject: Subject{Kind: KindDomain, Value: "go.dev"}}, "web_fetch: go.dev", true},
		{Request{Tool: "mcp", Subject: Subject{Kind: KindName, Value: "github__create_issue"}}, "mcp: github__create_issue", true},
		{Request{Tool: "memory"}, "memory", true},
	} {
		got, ok := Suggest(tc.req)
		if ok != tc.ok || (ok && got.String() != tc.want) {
			t.Errorf("Suggest(%+v) = %q, %v; want %q, %v", tc.req, got.String(), ok, tc.want, tc.ok)
		}
	}
}
