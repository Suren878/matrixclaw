package agent

import "testing"

func TestShellEffect(t *testing.T) {
	for _, tc := range []struct {
		command        string
		checks, writes bool
	}{
		{"go test ./... 2>&1 | tail -50", true, false},
		{"CGO_ENABLED=0 timeout 600 go build ./cmd/x", true, false},
		{"npm test", true, false},
		{"pnpm run test:unit", true, false},
		{"yarn lint --fix", true, false},
		{"npx tsc --noEmit", true, false},
		{"uv run pytest -q", true, false},
		{"python3 -m pytest tests", true, false},
		{"cargo clippy --all-targets", true, false},
		{"make", true, false},
		{"make test", true, false},
		{"./scripts/run_tests.sh", true, false},
		{"make install", false, false},
		{"npm install", false, false},
		{"go mod tidy", false, false},
		{"ls -la > /tmp/listing.txt", false, false},
		{"echo hi > /dev/null 2>&1", false, false},
		{"echo note >> NOTES.md", false, false},
		{"git status && git diff", false, false},
		{"echo 'package x' > x.go", false, true},
		{"sed -i.bak 's/a/b/' main.go", false, true},
		{"perl -pi -e 's/a/b/' main.go", false, true},
		{"sed -n '1,10p' main.go", false, false},
		{"git checkout -- main.go", false, true},
		{"rm -rf build && make", true, true},
		{"cat x.go | tee y.go", false, true},
	} {
		checks, writes := shellEffect(tc.command)
		if checks != tc.checks || writes != tc.writes {
			t.Errorf("%q: checks %v writes %v, want %v %v", tc.command, checks, writes, tc.checks, tc.writes)
		}
	}
}
