package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestApplyEditsFallsBackToLooserMatches(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		edit          EditOperation
		want, how     string
	}{
		{
			name:    "indentation, new text moved to the file's indentation",
			content: "func a() {\n\tif x {\n\t\treturn 1\n\t}\n}\n",
			edit:    EditOperation{OldString: "if x {\n\treturn 1\n}", NewString: "if y {\n\treturn 2\n\tlog()\n}"},
			want:    "func a() {\n\tif y {\n\t\treturn 2\n\t\tlog()\n\t}\n}\n",
			how:     "ignoring indentation",
		},
		{
			name:    "spaces for tabs",
			content: "func a() {\n\tif x {\n\t\treturn 1\n\t}\n}\n",
			edit:    EditOperation{OldString: "    if x {\n        return 1\n    }", NewString: "    if y {\n        return 2\n    }"},
			want:    "func a() {\n\tif y {\n\t\treturn 2\n\t}\n}\n",
			how:     "ignoring leading and trailing whitespace",
		},
		{
			name:    "trailing whitespace and uneven indentation",
			content: "a := 1   \n  b := 2\nc := 3\n",
			edit:    EditOperation{OldString: "a := 1\nb := 2\n", NewString: "a := 10\nb := 20\n"},
			want:    "a := 10\nb := 20\nc := 3\n",
			how:     "ignoring leading and trailing whitespace",
		},
		{
			name:    "inner whitespace and typographic quotes",
			content: "msg := \"it's  done\"\nnext()\n",
			edit:    EditOperation{OldString: "msg := “it’s done”", NewString: `msg := "finished"`},
			want:    "msg := \"finished\"\nnext()\n",
			how:     "ignoring whitespace and quote or dash style",
		},
		{
			name:    "doubled escapes",
			content: "line one\nline two\n",
			edit:    EditOperation{OldString: `line one\nline two`, NewString: `line 1\nline 2`},
			want:    "line 1\nline 2\n",
			how:     "after unescaping",
		},
		{
			name:    "CRLF file keeps its line ends and byte order mark",
			content: "\uFEFFone\r\ntwo\r\nthree\r\n",
			edit:    EditOperation{OldString: "two\r\nthree", NewString: "2\n3"},
			want:    "\uFEFFone\r\n2\r\n3\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, notes, err := applyEdits(tc.content, []EditOperation{tc.edit})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("content = %q, want %q", got, tc.want)
			}
			if joined := strings.Join(notes, "\n"); tc.how == "" && joined != "" || !strings.Contains(joined, tc.how) {
				t.Fatalf("notes = %q, want %q", notes, tc.how)
			}
		})
	}
}

func TestApplyEditsRefusesAmbiguousAndPointlessEdits(t *testing.T) {
	content := "if a {\n\treturn\n}\nif a {\n    return\n}\n"
	for _, tc := range []struct {
		name string
		edit EditOperation
		want string
	}{
		{"exact twice", EditOperation{OldString: "if a {", NewString: "if b {"}, "matches 2 places (lines 1, 4)"},
		{"loose twice", EditOperation{OldString: "if a {\n  return\n}", NewString: "x"}, "matches 2 places ignoring"},
		{"same text", EditOperation{OldString: "return", NewString: "return"}, "the same"},
		{"replace_all is exact only", EditOperation{OldString: "if  a {", NewString: "x", ReplaceAll: true}, "not found"},
	} {
		if _, _, err := applyEdits(content, []EditOperation{tc.edit}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	got, _, err := applyEdits(content, []EditOperation{{OldString: "if a {", NewString: "if b {", ReplaceAll: true}})
	if err != nil || strings.Count(got, "if b {") != 2 {
		t.Fatalf("replace_all = %q, %v", got, err)
	}
}

func TestApplyEditsSeesAnEditAppliedAlready(t *testing.T) {
	content := "total := compute(items)\n"
	edit := EditOperation{OldString: "total := sum(items)", NewString: "total := compute(items)"}
	var unchanged unchangedError
	if _, _, err := applyEdits(content, []EditOperation{edit}); !errors.As(err, &unchanged) {
		t.Fatalf("err = %v, want unchanged", err)
	}
	got, notes, err := applyEdits(content, []EditOperation{edit, {OldString: "items", NewString: "list"}})
	if err != nil || got != "total := compute(list)\n" || len(notes) != 1 || !strings.Contains(notes[0], "Edit 1 seems applied already") {
		t.Fatalf("multiedit = %q, %q, %v", got, notes, err)
	}
}

func TestEditMissShowsTheClosestLines(t *testing.T) {
	content := "package a\n\nfunc Sum(xs []int) int {\n\ttotal := 0\n\tfor _, x := range xs {\n\t\ttotal += x\n\t}\n\treturn total\n}\n"
	_, _, err := applyEdits(content, []EditOperation{{OldString: "\tfor _, x := range values {\n\t\ttotal += x\n\t}", NewString: "x"}})
	if err == nil || !strings.Contains(err.Error(), "old_string not found") || !strings.Contains(err.Error(), "lines 5-7") || !strings.Contains(err.Error(), "     5\t\tfor _, x := range xs {") {
		t.Fatalf("err = %v", err)
	}
	_, _, err = applyEdits(content, []EditOperation{{OldString: "     4\ttotal := 0", NewString: "x"}})
	if err == nil || !strings.Contains(err.Error(), "line-number prefixes") {
		t.Fatalf("prefixed old_string: err = %v", err)
	}
}
