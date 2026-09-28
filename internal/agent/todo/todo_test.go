package todo_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
)

func TestParseChecksTheWholeList(t *testing.T) {
	items, err := todo.Parse(json.RawMessage(`{"items":[
		{"content":" Read the parser ","status":"completed"},
		{"content":"Write the tests","active_form":"Writing the tests","status":"IN_PROGRESS"},
		{"content":"Run go test","status":"pending"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []todo.Item{
		{Content: "Read the parser", Status: todo.Completed},
		{Content: "Write the tests", ActiveForm: "Writing the tests", Status: todo.InProgress},
		{Content: "Run go test", Status: todo.Pending},
	}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("items = %+v", items)
	}

	flat, err := todo.Parse(json.RawMessage(`{"items":[{"content":"Fix\n2. [completed] the\t bug","active_form":" Fixing\r\nit ","status":"pending"},{"content":"` + strings.Repeat("я", 300) + `","status":"pending"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if flat[0].Content != "Fix 2. [completed] the bug" || flat[0].ActiveForm != "Fixing it" {
		t.Fatalf("flattened item = %+v", flat[0])
	}

	cleared, err := todo.Parse(json.RawMessage(`{"items":[]}`))
	if err != nil || len(cleared) != 0 {
		t.Fatalf("empty list = %+v, %v", cleared, err)
	}
}

func TestParseRejectsListsTheModelMustCorrect(t *testing.T) {
	for _, tc := range []struct {
		args string
		want string
	}{
		{`{}`, "items is required"},
		{`[]`, "not a JSON object"},
		{`{"items":[{"content":"","status":"pending"}]}`, "item 1 has no content"},
		{`{"items":[{"content":"a","status":"done"}]}`, `item 1 has status "done"`},
		{`{"items":[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]}`, "2 items are in_progress"},
		{`{"items":[` + strings.Repeat(`{"content":"a","status":"pending"},`, todo.MaxItems) + `{"content":"a","status":"pending"}]}`, "keep it to 50"},
		{`{"items":[{"content":"a","status":"pending"},{"content":"a","status":"pending"},{"content":"` + strings.Repeat("я", 1200) + `","status":"pending"}]}`, "item 3 is 1200 characters; keep each under 300"},
		{`{"items":[{"content":"a","active_form":"` + strings.Repeat("b", 301) + `","status":"in_progress"}]}`, "item 1 is 301 characters; keep each under 300"},
	} {
		if _, err := todo.Parse(json.RawMessage(tc.args)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%s) error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestOpenItemsAndChainMembership(t *testing.T) {
	list := todo.List{Items: []todo.Item{
		{Content: "a", Status: todo.Completed},
		{Content: "b", Status: todo.InProgress, ActiveForm: "Doing b"},
		{Content: "c", Status: todo.Pending},
	}, ChainRunID: "run_1", UpdatedRunID: "run_2"}

	if open := todo.Open(list.Items); len(open) != 2 || open[0].Label() != "Doing b" || open[1].Label() != "c" {
		t.Fatalf("open = %+v", open)
	}
	if got := todo.Text(list.Items); got != "1. [completed] a\n2. [in_progress] b\n3. [pending] c" {
		t.Fatalf("text = %q", got)
	}
	for _, tc := range []struct {
		chain []string
		want  bool
	}{
		{[]string{"run_3", "run_2", "run_1"}, true},
		{[]string{"run_2"}, true},
		{[]string{"run_9", "run_1"}, true},
		{[]string{"run_9"}, false},
		{nil, false},
	} {
		if got := list.InChain(tc.chain); got != tc.want {
			t.Errorf("InChain(%v) = %v", tc.chain, got)
		}
	}
	if (todo.List{}).InChain([]string{""}) {
		t.Fatal("a list no run wrote belongs to a chain")
	}
}
