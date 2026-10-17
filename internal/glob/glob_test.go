package glob

import "testing"

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "cmd/app/main.go", true},
		{"**/*.go", "main.gox", false},
		{"src/**/*.ts", "src/a.ts", true},
		{"src/**/*.ts", "src/x/y/a.ts", true},
		{"src/**/*.ts", "lib/a.ts", false},
		{"*.go", "main.go", true},
		{"*.go", "cmd/main.go", false},
		{"cmd/*/main.go", "cmd/app/main.go", true},
		{"cmd/*/main.go", "cmd/a/b/main.go", false},
		{"*.{ts,tsx}", "a.tsx", true},
		{"*.{ts,tsx}", "a.ts", true},
		{"*.{ts,tsx}", "a.js", false},
		{"{src,lib}/**/*.go", "lib/a.go", true},
		{"{a,{b,c}}.txt", "c.txt", true},
		{"{a/b,c}.go", "a/b.go", true},
		{"file[0-9].txt", "file7.txt", true},
		{"file[0-9].txt", "filex.txt", false},
		{"file[!0-9].txt", "filex.txt", true},
		{"[ab]*", "beta", true},
		{"?.go", "a.go", true},
		{"?.go", "ab.go", false},
		{"a/**", "a", true},
		{"a/**", "a/b/c", true},
		{"a/**/b", "a/b", true},
		{"/work/**", "/workshop/a.go", false},
		{`\*.go`, "*.go", true},
		{`\*.go`, "a.go", false},
		{"unclosed[.go", "unclosed[.go", true},
		{"brace{.go", "brace{.go", true},
		{"**", ".env", true},
		{"/home/u/**", "/home/u/.ssh/id", true},
		{"*.example.com", "docs.example.com", true},
	} {
		if got := Compile(tc.pattern).Match(tc.path); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
		if got := Match(tc.pattern, tc.path); got != tc.want {
			t.Errorf("cached Match(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestMatchVisibleSkipsHiddenUnlessNamed(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.yml", ".github/workflows/ci.yml", false},
		{".github/**", ".github/workflows/ci.yml", true},
		{".github/**/*.yml", ".github/workflows/ci.yml", true},
		{"*", ".env", false},
		{".*", ".env", true},
		{"**/.env", "app/.env", true},
		{"**/*", "app/.cache/x", false},
		{"**/*.go", "main.go", true},
	} {
		if got := Compile(tc.pattern).MatchVisible(tc.path); got != tc.want {
			t.Errorf("MatchVisible(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestReaches(t *testing.T) {
	for _, tc := range []struct {
		pattern, dir string
		want         bool
	}{
		{"src/**/*.ts", "src", true},
		{"src/**/*.ts", "src/deep/er", true},
		{"src/**/*.ts", "docs", false},
		{"**/*.go", "cmd", true},
		{"**/*.go", ".git", false},
		{".github/**", ".github", true},
		{".github/**", ".github/workflows", true},
		{"cmd/*/main.go", "cmd/app", true},
		{"cmd/*/main.go", "cmd/app/sub", false},
		{"*.go", "cmd", false},
		{"{a,b}/*.go", "b", true},
	} {
		if got := Compile(tc.pattern).Reaches(tc.dir); got != tc.want {
			t.Errorf("Reaches(%q, %q) = %v, want %v", tc.pattern, tc.dir, got, tc.want)
		}
	}
}

func TestCompileFilterMatchesNamesAtAnyDepth(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "internal/tools/grep.go", true},
		{"node_modules", "web/node_modules", true},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "x/src/a.go", false},
	} {
		if got := CompileFilter(tc.pattern).Match(tc.path); got != tc.want {
			t.Errorf("CompileFilter(%q).Match(%q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestIgnore(t *testing.T) {
	ig := Ignore{}.Add("/repo", []byte("# build output\n*.log\n/dist\nbuild/\n!keep.log\ndocs/*.tmp\nspace\\ \n"))
	ig = ig.Add("/repo/web", []byte("cache\n!/dist\n"))
	for _, tc := range []struct {
		path string
		dir  bool
		want bool
	}{
		{"/repo/a.log", false, true},
		{"/repo/sub/b.log", false, true},
		{"/repo/keep.log", false, false},
		{"/repo/dist", true, true},
		{"/repo/sub/dist", true, false},
		{"/repo/build", true, true},
		{"/repo/build", false, false},
		{"/repo/docs/x.tmp", false, true},
		{"/repo/docs/sub/x.tmp", false, false},
		{"/repo/space ", false, true},
		{"/repo/web/cache", true, true},
		{"/repo/cache", true, false},
		{"/repo/web/dist", true, false},
		{"/repo/main.go", false, false},
	} {
		if got := ig.Ignored(tc.path, tc.dir); got != tc.want {
			t.Errorf("Ignored(%q, dir=%v) = %v, want %v", tc.path, tc.dir, got, tc.want)
		}
	}
}

func TestBase(t *testing.T) {
	for pattern, want := range map[string]string{
		"src/app/**/*.ts": "src/app",
		"**/*.go":         "",
		"cmd/main.go":     "cmd",
		"main.go":         "",
		"{a,b}/x.go":      "",
		".github/**":      ".github",
	} {
		if got := Base(pattern); got != want {
			t.Errorf("Base(%q) = %q, want %q", pattern, got, want)
		}
	}
}
