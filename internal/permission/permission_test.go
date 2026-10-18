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
		{"every mode lists memory", Request{Tool: "memory", Subject: Subject{Kind: KindName, Value: "list"}}, nil, Preset(ModeDefault, "/work"), Allow, "memory: list"},
		{"memory changes keep their default", Request{Tool: "memory", Subject: Subject{Kind: KindName, Value: "add"}}, nil, Preset(ModeAcceptEdits, "/work"), "", ""},
		{"an ask rule beats the built-in", Request{Tool: "memory", Subject: Subject{Kind: KindName, Value: "list"}}, []Rule{rule(Ask, "memory", "")}, Preset(ModeDefault, ""), Ask, "memory"},
		{"mcp name glob", Request{Tool: "mcp", Subject: Subject{Kind: KindName, Value: "browser__browser_click"}}, []Rule{rule(Allow, "mcp", "browser__*")}, nil, Allow, "mcp: browser__*"},
	} {
		got := Evaluate(tc.req, tc.rules, tc.preset)
		if got.Effect != tc.want || (tc.want != "" && got.Rule.String() != tc.rule) {
			t.Errorf("%s: verdict = %s by %q, want %s by %q", tc.name, got.Effect, got.Rule.String(), tc.want, tc.rule)
		}
	}
}

func TestBashGuardRulesHoldWhateverAllows(t *testing.T) {
	deny := []Rule{rule(Deny, "bash", "rm:*")}
	full := Preset(ModeFullAuto, "/work")
	for _, tc := range []struct {
		line   string
		rules  []Rule
		preset []Rule
		want   Effect
	}{
		{"/bin/rm -rf ~", deny, full, Deny},
		{"timeout 5 rm x", deny, full, Deny},
		{"command rm x", deny, full, Deny},
		{"$'rm' x", deny, full, Ask},
		{"'rm' x", deny, full, Deny},
		{`\rm x`, deny, full, Deny},
		{"{rm,-rf,x}", deny, full, Ask},
		{"echo|xargs rm", deny, full, Deny},
		{"bash -c 'rm x'", deny, full, Deny},
		{"bash -lc 'cd /; rm x'", deny, full, Deny},
		{"eval rm x", deny, full, Deny},
		{"env rm x", append([]Rule{rule(Allow, "bash", "")}, deny...), nil, Deny},
		{"env X=1 ls", append([]Rule{rule(Allow, "bash", "")}, deny...), nil, Ask},
		{"pnpm exec rm -rf /", deny, full, Deny},
		{"pnpm --filter web exec rm -rf /", deny, full, Deny},
		{"uv run rm x", deny, full, Deny},
		{"docker run --rm alpine rm -rf /data", deny, full, Deny},
		{"npx rimraf dist", deny, full, Ask},
		{"go run ./cmd/x", deny, full, Ask},
		{"cargo run --release", deny, full, Ask},
		{"pipx run cowsay", deny, full, Ask},
		{"sed -i s/a/b/ f.txt", []Rule{rule(Allow, "bash", "sed:*")}, nil, ""},
		{"perl -pi -e s/a/b/ f.txt", []Rule{rule(Allow, "bash", "perl:*")}, nil, ""},
		{`awk '{print > "out"}' in`, []Rule{rule(Allow, "bash", "awk:*")}, nil, ""},
		{`awk '{print | "sh"}' in`, []Rule{rule(Allow, "bash", "awk:*")}, nil, ""},
		{`awk 'BEGIN{system("id")}'`, []Rule{rule(Allow, "bash", "awk:*")}, nil, ""},
		{`sed -n 1,5p f.txt`, []Rule{rule(Allow, "bash", "sed:*")}, nil, Allow},
		{`awk '$3 > 100 {print $1}' in`, []Rule{rule(Allow, "bash", "awk:*")}, nil, Allow},
		{"sudo git push", []Rule{rule(Ask, "bash", "git push:*")}, full, Ask},
		{"echo x > out.txt", deny, full, Ask},
		{"go test ./...", deny, full, Allow},
		{"echo x > out.txt", []Rule{rule(Deny, "*", "/home/u/secrets/**")}, full, Ask},
		{"echo x > out.txt", []Rule{rule(Deny, "read", "/home/u/secrets/**")}, full, Allow},
		{"./go test ./...", []Rule{rule(Allow, "bash", "go test:*")}, nil, ""},
		{"git -C . push --force origin main", []Rule{rule(Deny, "bash", "git push:*")}, full, Deny},
		{"git --no-pager push --force", []Rule{rule(Deny, "bash", "git push:*")}, full, Deny},
		{"git --git-dir .git --work-tree . push", []Rule{rule(Ask, "bash", "git push")}, full, Ask},
		{"cargo +nightly publish", []Rule{rule(Deny, "bash", "cargo publish:*")}, full, Deny},
		{"git log --grep push", []Rule{rule(Deny, "bash", "git push:*")}, full, Allow},
		{"git --version", []Rule{rule(Deny, "bash", "git push:*")}, full, Allow},
		{"git -C . push", []Rule{rule(Allow, "bash", "git push:*")}, nil, ""},
	} {
		if got := Evaluate(command(tc.line), tc.rules, tc.preset); got.Effect != tc.want {
			t.Errorf("%q: verdict = %q by %q, want %q", tc.line, got.Effect, got.Rule.String(), tc.want)
		}
	}
}

func search(tool, dir string) Request {
	return Request{Tool: tool, CoveredBy: []string{"read"}, Subject: Subject{Kind: KindDirectory, Value: dir}}
}

func TestSearchesMeetTheRulesOfTheirDirectory(t *testing.T) {
	secrets := []Rule{rule(Deny, "read", "/home/u/secrets/**")}
	for _, tc := range []struct {
		name  string
		req   Request
		rules []Rule
		want  Effect
	}{
		{"grep above the secrets runs", search("grep", "/home/u"), secrets, ""},
		{"glob from the root runs", search("glob", "/"), secrets, ""},
		{"ls of the secrets", search("ls", "/home/u/secrets"), secrets, Deny},
		{"ls inside the secrets", search("ls", "/home/u/secrets/sub"), secrets, Deny},
		{"an ask rule on the directory", search("grep", "/work"), []Rule{rule(Ask, "read", "/work/**")}, Ask},
		{"read allows a search inside", search("grep", "/work/sub"), []Rule{rule(Allow, "read", "/work/**")}, Allow},
		{"allow does not reach down", search("grep", "/"), []Rule{rule(Allow, "read", "/work/**")}, ""},
		{"grep rules leave ls alone", search("ls", "/home/u/secrets"), []Rule{rule(Deny, "grep", "/home/u/secrets/**")}, ""},
		{"read rules cover only what names them", Request{Tool: "grep", Subject: Subject{Kind: KindDirectory, Value: "/home/u/secrets"}}, secrets, ""},
	} {
		if got := Evaluate(tc.req, tc.rules, nil); got.Effect != tc.want {
			t.Errorf("%s: verdict = %q, want %q", tc.name, got.Effect, tc.want)
		}
	}
}

func TestSearchSkipLeavesOutWhatRulesGuard(t *testing.T) {
	rules := []Rule{
		rule(Deny, "read", "/home/u/secrets/**"),
		rule(Ask, "*", "/home/u/**/.env"),
		rule(Ask, "read", "/home/u/project/**"),
		rule(Deny, "edit", "/home/u/project/a.go"),
		rule(Allow, "read", "/home/u/open/**"),
	}
	skip := SearchSkip(search("grep", "/home/u/project"), rules)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/home/u/project/a.go", false},
		{"/home/u/project/app/.env", true},
		{"/home/u/project/main.go", false},
	} {
		if got := skip(tc.path); got != tc.want {
			t.Errorf("skip(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
	if skip := SearchSkip(search("grep", "/home/u"), rules); skip == nil || !skip("/home/u/secrets/key") || !skip("/home/u/project/x.go") || skip("/home/u/notes.txt") {
		t.Error("a search above the guarded paths must leave them out and keep the rest")
	}
	if SearchSkip(search("grep", "/tmp"), []Rule{rule(Allow, "read", "/tmp/**")}) != nil {
		t.Error("allow rules leave nothing out")
	}
	if SearchSkip(file("read", "/home/u/secrets/key"), rules) != nil {
		t.Error("only searches skip")
	}
}

func TestDomainsCompareNormalised(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Docs.Example.COM.", "docs.example.com"},
		{"Bücher.example", "xn--bcher-kva.example"},
		{"*.BÜCHER.example.", "*.xn--bcher-kva.example"},
	} {
		if got := NormalizeDomain(tc.in); got != tc.want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	req := Request{Tool: "web_fetch", Subject: Subject{Kind: KindDomain, Value: "WWW.XN--BCHER-KVA.EXAMPLE."}}
	if got := Evaluate(req, []Rule{rule(Deny, "web_fetch", "*.bücher.example")}, nil); got.Effect != Deny {
		t.Errorf("verdict = %q, want deny", got.Effect)
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
	t.Setenv("HOME", "/home/u")
	dir := func(tool, path string) Request {
		return Request{Tool: tool, Subject: Subject{Kind: KindDirectory, Value: path}}
	}
	for _, tc := range []struct {
		req  Request
		root string
		want string
		ok   bool
	}{
		{command("go test ./internal/..."), "/work", "bash: go test:*", true},
		{command("git status"), "/work", "bash: git status:*", true},
		{command("npm test"), "/work", "bash: npm test:*", true},
		{command("ls -la"), "/work", "bash: ls -la", true},
		{command("cat notes.txt"), "/work", "bash: cat notes.txt", true},
		{command("rm tmp"), "/work", "bash: rm tmp", true},
		{command("python3 script.py"), "/work", "bash: python3 script.py", true},
		{command("python3.12 -m pytest"), "/work", "bash: python3.12 -m pytest", true},
		{command("node"), "/work", "bash: node", true},
		{command("go run ./cmd/x"), "/work", "", false},
		{command("docker compose up -d"), "/work", "bash: docker compose up -d", true},
		{command("kubectl run x --image=alpine"), "/work", "", false},
		{command("perl -e 1"), "/work", "bash: perl -e 1", true},
		{command("grep 'a b' notes.txt"), "/work", "", false},
		{command("go test ./... && rm x"), "/work", "", false},
		{command("echo x > out.txt"), "/work", "", false},
		{file("edit", "/work/internal/a.go"), "/work", "edit: /work/internal/**", true},
		{file("edit", "/work/a.go"), "/work", "edit: /work/**", true},
		{file("read", "/etc/hosts"), "/work", "read: /etc/hosts", true},
		{file("read", "/home/u/.bashrc"), "/home/u", "read: /home/u/.bashrc", true},
		{file("read", "/a.txt"), "/", "read: /a.txt", true},
		{file("read", "/a.txt"), "", "read: /a.txt", true},
		{dir("grep", "/work"), "/work", "grep: /work/**", true},
		{dir("grep", "/home/u"), "/work", "grep: /home/u", true},
		{dir("glob", "/"), "/", "glob: /", true},
		{Request{Tool: "web_fetch", Subject: Subject{Kind: KindDomain, Value: "go.dev"}}, "/work", "web_fetch: go.dev", true},
		{Request{Tool: "mcp", Subject: Subject{Kind: KindName, Value: "github__create_issue"}}, "/work", "mcp: github__create_issue", true},
		{Request{Tool: "memory"}, "/work", "memory", true},
	} {
		got, ok := Suggest(tc.req, tc.root)
		if ok != tc.ok || (ok && got.String() != tc.want) {
			t.Errorf("Suggest(%+v, %q) = %q, %v; want %q, %v", tc.req, tc.root, got.String(), ok, tc.want, tc.ok)
		}
	}
}

func TestPlanModeDeniesEveryChangeAndRunsReadOnlyBash(t *testing.T) {
	plan := Preset(ModePlan, "/work")
	readOnly := func(req Request) Request { req.ReadOnly = true; return req }
	for _, tc := range []struct {
		name  string
		req   Request
		rules []Rule
		want  Effect
	}{
		{"edit", file("edit", "/work/a.go"), nil, Deny},
		{"a mutating command", command("go test ./..."), nil, Deny},
		{"over an allow rule", command("go test ./..."), []Rule{rule(Allow, "bash", "")}, Deny},
		{"a read-only command", readOnly(command("git log")), nil, Allow},
		{"a read-only command a rule asks for", readOnly(command("git log")), []Rule{rule(Ask, "bash", "git log:*")}, Ask},
		{"read keeps its default", readOnly(file("read", "/work/a.go")), nil, ""},
		{"todo_write keeps its default", readOnly(Request{Tool: "todo_write"}), nil, ""},
		{"memory list", readOnly(Request{Tool: "memory", Subject: Subject{Kind: KindName, Value: "list"}}), nil, Allow},
		{"memory add", Request{Tool: "memory", Subject: Subject{Kind: KindName, Value: "add"}}, nil, Deny},
	} {
		got := Evaluate(tc.req, tc.rules, plan)
		if got.Effect != tc.want {
			t.Errorf("%s: verdict = %s by %q, want %s", tc.name, got.Effect, got.Rule.String(), tc.want)
		}
		if tc.want == Deny && tc.rules == nil && got.Rule.ID != PlanRuleID {
			t.Errorf("%s: denied by %+v, want the plan rule", tc.name, got.Rule)
		}
	}
}

func TestEvaluateEachNeedsEverySubjectAllowed(t *testing.T) {
	a, b := Subject{Kind: KindFile, Value: "/work/a.go"}, Subject{Kind: KindFile, Value: "/etc/b.conf"}
	req := Request{Tool: "apply_patch", CoveredBy: []string{"edit"}}
	edits := Preset(ModeAcceptEdits, "/work")
	for _, tc := range []struct {
		name     string
		subjects []Subject
		rules    []Rule
		want     Effect
	}{
		{"all inside", []Subject{a}, nil, Allow},
		{"one outside", []Subject{a, b}, nil, ""},
		{"one denied", []Subject{a, b}, []Rule{rule(Deny, "edit", "/etc/**")}, Deny},
		{"one asked", []Subject{a, b}, []Rule{rule(Ask, "edit", "/work/**"), rule(Allow, "edit", "/etc/**")}, Ask},
		{"all allowed", []Subject{a, b}, []Rule{rule(Allow, "apply_patch", "/etc/**")}, Allow},
	} {
		if got := EvaluateEach(req, tc.subjects, tc.rules, edits); got.Effect != tc.want {
			t.Errorf("%s: verdict = %s, want %s", tc.name, got.Effect, tc.want)
		}
	}
	if _, ok := SuggestEach(req, []Subject{a, b}, "/work"); ok {
		t.Error("files in two places suggested one rule")
	}
	if got, ok := SuggestEach(req, []Subject{a, {Kind: KindFile, Value: "/work/c.go"}}, "/work"); !ok || got.String() != "apply_patch: /work/**" {
		t.Errorf("suggestion = %v, %v", got, ok)
	}
}
