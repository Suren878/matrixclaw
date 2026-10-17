package permission

import "testing"

func TestCheckReadOnlyAllowsOnlyCommandsThatRead(t *testing.T) {
	for line, readOnly := range map[string]bool{
		"git status": true,
		"git -C ../repo --no-pager log --oneline": true,
		"git diff HEAD~1 -- a.go | head -50":      true,
		"git branch --show-current":               true,
		"git branch feature":                      false,
		"git stash":                               false,
		"git tag v1":                              false,
		"git push":                                false,
		"git diff --output=x":                     false,
		"go list ./... && go vet ./internal/...":  true,
		"go env GOPATH GOMODCACHE":                true,
		"go env -w GOFLAGS=-x":                    false,
		"go test ./...":                           false,
		"rg -n 'func main' --glob '*.go'":         true,
		"find . -name '*.go' -type f | wc -l":     true,
		"find . -name '*.tmp' -delete":            false,
		"find . -exec rm {} ;":                    false,
		"ls -la src && cat go.mod":                true,
		"cat go.mod > copy":                       false,
		"ls $HOME":                                false,
		"ls *.go":                                 false,
		"sed -n 1,5p a.go":                        false,
		"tree -o out.txt":                         false,
		"./ls":                                    false,
		"rm -rf /":                                false,
		"xargs cat < files":                       false,
		"":                                        false,
	} {
		if err := CheckReadOnly(line); (err == nil) != readOnly {
			t.Errorf("CheckReadOnly(%q) = %v, want read-only %v", line, err, readOnly)
		}
	}
}
