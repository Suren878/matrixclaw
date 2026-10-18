package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/permission"
)

func TestParsePatch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch string
		want  []patchHunk
		err   string
	}{
		{name: "no begin marker", patch: "bad", err: "the first line of the patch must be '*** Begin Patch'"},
		{name: "no end marker", patch: "*** Begin Patch\nbad", err: "the last line of the patch must be '*** End Patch'"},
		{name: "empty update", patch: "*** Begin Patch\n*** Update File: test.py\n*** End Patch", err: "line 2: the update of 'test.py' changes nothing"},
		{name: "empty patch", patch: "*** Begin Patch\n*** End Patch"},
		{name: "every kind of section", patch: "*** Begin Patch\n*** Add File: path/add.py\n+abc\n+def\n*** Delete File: path/delete.py\n*** Update File: path/update.py\n*** Move to: path/update2.py\n@@ def f():\n-    pass\n+    return 123\n*** End Patch", want: []patchHunk{
			{kind: hunkAdd, path: "path/add.py", content: "abc\ndef\n"},
			{kind: hunkDelete, path: "path/delete.py"},
			{kind: hunkUpdate, path: "path/update.py", moveTo: "path/update2.py", chunks: []patchChunk{{context: "def f():", hasContext: true, old: []string{"    pass"}, new: []string{"    return 123"}}}},
		}},
		{name: "update then add", patch: "*** Begin Patch\n*** Update File: file.py\n@@\n+line\n*** Add File: other.py\n+content\n*** End Patch", want: []patchHunk{
			{kind: hunkUpdate, path: "file.py", chunks: []patchChunk{{new: []string{"line"}}}},
			{kind: hunkAdd, path: "other.py", content: "content\n"},
		}},
		{name: "first chunk without @@", patch: "*** Begin Patch\n*** Update File: file2.py\n import foo\n+bar\n*** End Patch", want: []patchHunk{
			{kind: hunkUpdate, path: "file2.py", chunks: []patchChunk{{old: []string{"import foo"}, new: []string{"import foo", "bar"}}}},
		}},
		{name: "heredoc wrapper", patch: "<<'EOF'\n*** Begin Patch\n*** Delete File: a.txt\n*** End Patch\nEOF\n", want: []patchHunk{{kind: hunkDelete, path: "a.txt"}}},
		{name: "heredoc with double quotes", patch: "<<\"EOF\"\n*** Begin Patch\n*** Delete File: a.txt\n*** End Patch\nEOF", want: []patchHunk{{kind: hunkDelete, path: "a.txt"}}},
		{name: "heredoc that does not close", patch: "<<EOF\n*** Begin Patch\n*** Delete File: a.txt\n*** End Patch\n", err: "the first line"},
		{name: "whitespace around markers and CRLF", patch: "  *** Begin Patch  \r\n*** Update File: a.txt \r\n@@\r\n-a\r\n+b\r\n\r\n  *** Delete File: b.txt\r\n *** End Patch\r\n", want: []patchHunk{
			{kind: hunkUpdate, path: "a.txt", chunks: []patchChunk{{old: []string{"a", ""}, new: []string{"b", ""}}}},
			{kind: hunkDelete, path: "b.txt"},
		}},
		{name: "unknown header", patch: "*** Begin Patch\n*** Frobnicate File: a\n*** End Patch", err: "line 2: '*** Frobnicate File: a' is not a valid section header"},
		{name: "second chunk without @@", patch: "*** Begin Patch\n*** Update File: a\n@@\n-a\n+b\n*** End of File\nbad\n*** End Patch", err: "expected the chunk to start with a @@ line, got 'bad'"},
		{name: "chunk without lines", patch: "*** Begin Patch\n*** Update File: a\n@@\n*** End of File\n*** End Patch", err: "a chunk without lines"},
		{name: "unexpected line", patch: "*** Begin Patch\n*** Update File: a\n@@\nbad\n*** End Patch", err: "unexpected line 'bad'"},
		{name: "context, blank line and end of file", patch: "*** Begin Patch\n*** Update File: a\n@@ change_context\n\n context\n-remove\n+add\n context2\n@@\n+line\n*** End of File\n*** End Patch", want: []patchHunk{
			{kind: hunkUpdate, path: "a", chunks: []patchChunk{
				{context: "change_context", hasContext: true, old: []string{"", "context", "remove", "context2"}, new: []string{"", "context", "add", "context2"}},
				{new: []string{"line"}, eof: true},
			}},
		}},
	} {
		got, err := parsePatch(tc.patch)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, %v\nwant %+v", tc.name, got, err, tc.want)
		}
	}
}

func TestApplyChunks(t *testing.T) {
	for _, tc := range []struct {
		name, file, patch, want, err string
	}{
		{name: "update", file: "foo\nbar\n", patch: "@@\n foo\n-bar\n+baz", want: "foo\nbaz\n"},
		{name: "two chunks", file: "foo\nbar\nbaz\nqux\n", patch: "@@\n foo\n-bar\n+BAR\n@@\n baz\n-qux\n+QUX", want: "foo\nBAR\nbaz\nQUX\n"},
		{name: "interleaved with end of file", file: "a\nb\nc\nd\ne\nf\n", patch: "@@\n a\n-b\n+B\n@@\n c\n d\n-e\n+E\n@@\n f\n+g\n*** End of File", want: "a\nB\nc\nd\nE\nf\ng\n"},
		{name: "pure addition goes to the end", file: "line1\nline2\nline3\n", patch: "@@\n+after-context\n+second-line\n@@\n line1\n-line2\n-line3\n+line2-replacement", want: "line1\nline2-replacement\nafter-context\nsecond-line\n"},
		{name: "pure addition after an @@ line", file: "func a() {\n}\nfunc b() {\n}\n", patch: "@@ func b() {\n+\tb()", want: "func a() {\n}\nfunc b() {\n\tb()\n}\n"},
		{name: "unicode dashes", file: "import asyncio  # local import – avoids top‑level dep\n", patch: "@@\n-import asyncio  # local import - avoids top-level dep\n+import asyncio  # HELLO", want: "import asyncio  # HELLO\n"},
		{name: "trailing whitespace", file: "a  \nb\n", patch: "@@\n-a\n+A", want: "A\nb\n"},
		{name: "indentation", file: "\tif x {\n\t\treturn\n\t}\n", patch: "@@\n if x {\n-    return\n+    return 1\n }", want: "\tif x {\n\t\treturn 1\n\t}\n"},
		{name: "trailing empty line retried without it", file: "a\nb", patch: "@@\n-b\n+c\n", want: "a\nc\n"},
		{name: "@@ anchor seeks forward", file: "x = 1\nfunc f() {\nx = 1\n}\n", patch: "@@ func f() {\n-x = 1\n+x = 2", want: "x = 1\nfunc f() {\nx = 2\n}\n"},
		{name: "end of file pins the match", file: "x\ny\nx\n", patch: "@@\n-x\n+z\n*** End of File", want: "x\ny\nz\n"},
		{name: "CRLF and BOM kept", file: "\uFEFFa\r\nb\r\n", patch: "@@\n-b\n+c", want: "\uFEFFa\r\nc\r\n"},
		{name: "missing lines", file: "alpha\nbeta\n", patch: "@@\n-gamma\n+delta", err: "these lines are not in the file"},
		{name: "missing anchor", file: "alpha\n", patch: "@@ nowhere\n-alpha\n+beta", err: `the @@ line "nowhere" is not in the file`},
		{name: "chunks out of order", file: "a\nb\nc\n", patch: "@@\n-c\n+C\n@@\n-a\n+A", err: "before the previous chunk; chunks must follow the file's order"},
	} {
		hunks, err := parsePatch("*** Begin Patch\n*** Update File: f\n" + tc.patch + "\n*** End Patch")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got, _, err := applyChunks(tc.file, "f", hunks[0].chunks)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v, want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestSeekLinesTriesLooserLevels(t *testing.T) {
	lines := []string{"  a := 1  ", "b := “x”"}
	for _, tc := range []struct {
		pattern []string
		at      int
		how     string
	}{
		{[]string{"  a := 1  "}, 0, ""},
		{[]string{"  a := 1"}, 0, "ignoring trailing whitespace"},
		{[]string{"a := 1"}, 0, "ignoring surrounding whitespace"},
		{[]string{`b  :=  "x"`}, 1, "ignoring whitespace and quote or dash style"},
		{[]string{"c"}, -1, ""},
	} {
		if at, how := seekLines(lines, tc.pattern, 0, false); at != tc.at || how != tc.how {
			t.Errorf("%q: at %d by %q, want %d by %q", tc.pattern, at, how, tc.at, tc.how)
		}
	}
}

func patchArgs(patch string) json.RawMessage {
	raw, _ := json.Marshal(ApplyPatchParams{Input: patch})
	return raw
}

func TestApplyPatchAddsUpdatesMovesAndDeletes(t *testing.T) {
	s := newSessionTools(t)
	for name, content := range map[string]string{"update.txt": "one\ntwo\n", "move.txt": "line\n", "delete.txt": "gone\n"} {
		if err := os.WriteFile(filepath.Join(s.dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		s.run("read", ReadParams{FilePath: name})
	}
	patch := "*** Begin Patch\n*** Add File: sub/new.txt\n+hello\n*** Update File: update.txt\n@@\n one\n-two\n+TWO\n*** Update File: move.txt\n*** Move to: moved/dst.txt\n@@\n-line\n+line2\n*** Delete File: delete.txt\n*** End Patch"
	result := s.run("apply_patch", ApplyPatchParams{Input: patch})
	if result.IsError() {
		t.Fatalf("result = %+v", result)
	}
	for name, want := range map[string]string{"sub/new.txt": "hello\n", "update.txt": "one\nTWO\n", "moved/dst.txt": "line2\n"} {
		if got := readFile(t, filepath.Join(s.dir, name)); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"move.txt", "delete.txt"} {
		if _, err := os.Stat(filepath.Join(s.dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", name, err)
		}
	}
	change, ok := result.Metadata.(PatchChange)
	if !ok || len(change.Files) != 4 {
		t.Fatalf("metadata = %+v", result.Metadata)
	}
	moved := change.Files[2]
	if moved.Path != filepath.Join(s.dir, "moved/dst.txt") || moved.MovedFrom != filepath.Join(s.dir, "move.txt") || moved.OldContent != "line\n" || moved.NewContent != "line2\n" {
		t.Fatalf("move = %+v", moved)
	}
	if deleted := change.Files[3]; deleted.OldContent != "gone\n" || deleted.NewContent != "" || deleted.Removals != 1 {
		t.Fatalf("delete = %+v", deleted)
	}
	if result := s.run("apply_patch", ApplyPatchParams{Input: "*** Begin Patch\n*** Update File: update.txt\n@@\n-TWO\n+2\n*** End Patch"}); result.IsError() {
		t.Fatalf("update of a file the patch wrote = %+v", result)
	}
}

func TestApplyPatchWritesNothingUnlessEverySectionApplies(t *testing.T) {
	s := newSessionTools(t)
	path := filepath.Join(s.dir, "a.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.run("read", ReadParams{FilePath: "a.txt"})
	for _, tc := range []struct{ patch, want string }{
		{"*** Begin Patch\n*** Update File: a.txt\n@@\n-a\n+b\n*** Update File: missing.txt\n@@\n-x\n+y\n*** End Patch", "cannot update"},
		{"*** Begin Patch\n*** Add File: new.txt\n+x\n*** Update File: a.txt\n@@\n-zzz\n+b\n*** End Patch", "not in the file"},
		{"*** Begin Patch\n*** Update File: a.txt\n@@\n-a\n+b\n*** Delete File: nothing.txt\n*** End Patch", "cannot delete"},
	} {
		if result := s.run("apply_patch", ApplyPatchParams{Input: tc.patch}); !result.IsError() || !strings.Contains(result.Content, tc.want) {
			t.Fatalf("result = %+v, want %q", result, tc.want)
		}
		if got := readFile(t, path); got != "a\n" {
			t.Fatalf("a.txt = %q after a failed patch", got)
		}
		if _, err := os.Stat(filepath.Join(s.dir, "new.txt")); !os.IsNotExist(err) {
			t.Fatal("new.txt was created by a failed patch")
		}
	}
}

func TestApplyPatchNeedsAFreshReadOfExistingFiles(t *testing.T) {
	s := newSessionTools(t)
	if err := os.WriteFile(filepath.Join(s.dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, patch := range []string{
		"*** Begin Patch\n*** Update File: a.txt\n@@\n-a\n+b\n*** End Patch",
		"*** Begin Patch\n*** Delete File: a.txt\n*** End Patch",
		"*** Begin Patch\n*** Add File: a.txt\n+b\n*** End Patch",
	} {
		if result := s.run("apply_patch", ApplyPatchParams{Input: patch}); !result.IsError() || !strings.Contains(result.Content, "has not been read") {
			t.Fatalf("%q: result = %+v", patch, result)
		}
	}
	if _, refused := s.registry.Preview(context.Background(), "apply_patch", Call{SessionID: s.session, WorkingDir: s.dir, Args: patchArgs("*** Begin Patch\n*** Delete File: a.txt\n*** End Patch")}); refused == nil || !strings.Contains(refused.Content, "has not been read") {
		t.Fatalf("preview = %+v", refused)
	}
	if result := s.run("apply_patch", ApplyPatchParams{Input: "*** Begin Patch\n*** Add File: b.txt\n+b\n*** End Patch"}); result.IsError() {
		t.Fatalf("new file = %+v", result)
	}
}

func TestApplyPatchPreviewShowsEveryFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(CoreExecutors()...)
	call := Call{WorkingDir: dir, Args: patchArgs("*** Begin Patch\n*** Update File: a.txt\n@@\n-a\n+b\n*** Add File: b.txt\n+b\n*** End Patch")}
	request, refused := registry.Preview(context.Background(), "apply_patch", call)
	if refused != nil {
		t.Fatalf("refused = %+v", refused)
	}
	change, ok := request.Params.(PatchChange)
	if !ok || len(change.Files) != 2 || change.Files[0].NewContent != "b\n" || change.Files[1].Path != filepath.Join(dir, "b.txt") {
		t.Fatalf("request = %+v", request)
	}
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "a\n" {
		t.Fatalf("preview wrote a.txt: %q", got)
	}
	want := []permission.Subject{{Kind: permission.KindFile, Value: filepath.Join(dir, "a.txt")}, {Kind: permission.KindFile, Value: filepath.Join(dir, "b.txt")}}
	if got := registry.Subjects("apply_patch", call); !reflect.DeepEqual(got, want) {
		t.Fatalf("subjects = %+v, want %+v", got, want)
	}
	if _, refused := registry.Preview(context.Background(), "apply_patch", Call{WorkingDir: dir, Args: patchArgs("*** Begin Patch\n*** Update File: a.txt\n@@\n-a\n+a\n*** End Patch")}); refused == nil || refused.Status != ResultStatusNeutral {
		t.Fatalf("unchanged = %+v", refused)
	}
}

func TestPrefersApplyPatch(t *testing.T) {
	for model, want := range map[string]bool{
		"gpt-5": true, "openai/gpt-5.1-codex": true, "gpt-5-mini": true, "codex-mini-latest": true,
		"gpt-4o": false, "gpt-4.1": false, "openai/gpt-oss-120b": false, "claude-sonnet-4-5": false, "": false,
	} {
		if got := PrefersApplyPatch(model); got != want {
			t.Errorf("%q: %v, want %v", model, got, want)
		}
	}
}
