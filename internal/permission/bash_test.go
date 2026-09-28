package permission

import (
	"reflect"
	"testing"
)

func TestParseLineFindsEverySimpleCommand(t *testing.T) {
	for _, tc := range []struct {
		line  string
		want  [][]string
		risky bool
	}{
		{"go test ./...", [][]string{{"go", "test", "./..."}}, false},
		{"go vet ./... && go test ./... | tee /dev/null", [][]string{{"go", "vet", "./..."}, {"go", "test", "./..."}, {"tee", "/dev/null"}}, false},
		{"if true; then ls -la; fi", [][]string{{"true"}, {"ls", "-la"}}, false},
		{`'r'\m -rf "/tmp/x"`, [][]string{{"rm", "-rf", "/tmp/x"}}, false},
		{"go test ./... 2>&1 >/dev/null", [][]string{{"go", "test", "./..."}}, false},
		{"echo hi > out.txt", [][]string{{"echo", "hi"}}, true},
		{"echo hi >> out.txt", [][]string{{"echo", "hi"}}, true},
		{"cat <<EOF\nhi\nEOF", [][]string{{"cat"}}, false},
		{"GOFLAGS=-x go build", [][]string{{"go", "build"}}, true},
		{"echo $(rm -rf /)", [][]string{{"echo", ""}, {"rm", "-rf", "/"}}, true},
		{"diff <(ls a) b", [][]string{{"diff", "", "b"}, {"ls", "a"}}, true},
		{`find . -name x -exec rm {} \;`, [][]string{{"find", ".", "-name", "x", "-exec", "rm", "{}", ";"}}, true},
		{"go build -toolexec=/tmp/x ./...", [][]string{{"go", "build", "-toolexec=/tmp/x", "./..."}}, true},
		{"sudo ls", [][]string{{"sudo", "ls"}}, true},
		{"xargs rm < files", [][]string{{"xargs", "rm"}}, true},
		{"ls $HOME", [][]string{{"ls", ""}}, true},
		{"export X=1", nil, true},
		{"f() { rm -rf /; }", [][]string{{"rm", "-rf", "/"}}, true},
		{"echo 'unterminated", [][]string{{"echo", "'unterminated"}}, true},
		{"", nil, false},
	} {
		got := ParseLine(tc.line)
		if !reflect.DeepEqual(got.Commands, tc.want) || got.Risky != tc.risky {
			t.Errorf("ParseLine(%q) = %q risky=%v, want %q risky=%v", tc.line, got.Commands, got.Risky, tc.want, tc.risky)
		}
	}
}
