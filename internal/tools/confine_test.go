package tools

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
)

func TestCheckConfinedCallKeepsWorkInsideTheRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	bash := Spec{ID: bashToolName, Effect: EffectMutation}
	write := Spec{ID: "write", Effect: EffectMutation}
	read := Spec{ID: "read", Effect: EffectReadOnly}
	bashCall := func(command string, dir string) Call {
		args, _ := json.Marshal(BashParams{Command: command, WorkingDir: dir})
		return Call{WorkingDir: filepath.Join(root, "pkg"), Args: args}
	}
	file := func(path string) permission.Subject {
		return permission.Subject{Kind: permission.KindFile, Value: path}
	}
	for _, tc := range []struct {
		name    string
		spec    Spec
		call    Call
		subject permission.Subject
		ok      bool
	}{
		{"bash inside", bash, bashCall("go test ./... 2>/dev/null && cat ../go.mod", ""), permission.Subject{}, true},
		{"bash to /dev/null", bash, bashCall("cp x /dev/null", ""), permission.Subject{}, true},
		{"bash cd out", bash, bashCall("cd ../.. && rm -rf x", ""), permission.Subject{}, false},
		{"bash git -C", bash, bashCall("git -C "+outside+" status", ""), permission.Subject{}, false},
		{"bash working_dir", bash, bashCall("ls", outside), permission.Subject{}, false},
		{"write inside", write, Call{WorkingDir: root}, file(filepath.Join(root, "a.go")), true},
		{"write outside", write, Call{WorkingDir: root}, file(filepath.Join(outside, "a.go")), false},
		{"read outside", read, Call{WorkingDir: root}, file(filepath.Join(outside, "a.go")), true},
		{"call outside", read, Call{WorkingDir: outside}, permission.Subject{}, false},
	} {
		err := CheckConfinedCall(tc.spec, tc.call, []permission.Subject{tc.subject}, tc.spec.Mutates(), root)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok %v", tc.name, err, tc.ok)
		}
	}
}
