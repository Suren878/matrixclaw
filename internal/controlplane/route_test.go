package controlplane

import (
	"reflect"
	"strings"
	"testing"
)

// trickyOperands are operands that look like verbs, hold separators or
// escapes, or are empty.
var trickyOperands = []string{"add", "enabled", "create", "open", "confirm", "a b", "100%", "%41", "%", "tab\there", "ключ", "a/b c", "line\nbreak", ""}

func TestEveryRouteRoundTripsWithAnyOperands(t *testing.T) {
	for command, list := range verbs {
		for _, verb := range list {
			for i, operand := range trickyOperands {
				route := Route{Verb: verb, Args: make([]string, verb.args)}
				for j := range route.Args {
					route.Args[j] = trickyOperands[(i+j)%len(trickyOperands)]
				}
				if verb.value {
					route.Value = strings.TrimSpace("free  text with %41 and " + operand)
				}
				line := route.Encode()
				got, ok := DecodeRoute(line)
				if !ok || !reflect.DeepEqual(got, route) {
					t.Fatalf("%s %q: %q decodes to %+v, want %+v", command, verb.words, line, got, route)
				}
			}
		}
	}
}

func TestTypedCommandsReadTheirValueRaw(t *testing.T) {
	for _, tc := range []struct {
		line  string
		verb  Verb
		args  []string
		value string
	}{
		{"/goal clear", goalClear, nil, ""},
		{"/goal clear the cache", goalRoot, nil, "clear the cache"},
		{"/session rename s1  My   title ", sessionRename, []string{"s1"}, "My   title"},
		{"/modules web_search", modulesRoot, nil, "web_search"},
		{"/modules MCP Open docs", mcpOpen, []string{"docs"}, ""},
		{"/approval deny\tapproval_1\nnot on\tproduction", approvalDeny, []string{"approval_1"}, "not on\tproduction"},
		{"/mode plan", permissionsRoot, nil, "plan"},
		{"/session@matrixclaw_bot use s2", sessionUse, []string{"s2"}, ""},
	} {
		got, ok := DecodeRoute(tc.line)
		if !ok || got.Verb != tc.verb || got.Value != tc.value || strings.Join(got.Args, ",") != strings.Join(tc.args, ",") {
			t.Errorf("DecodeRoute(%q) = %+v, want %q %v %q", tc.line, got, tc.verb.words, tc.args, tc.value)
		}
	}
	if _, ok := DecodeRoute("hello /goal"); ok {
		t.Error("plain text decoded as a command")
	}
}

func TestAVerbWithOperandsCannotBeExtended(t *testing.T) {
	saved := verbs[CommandTodo]
	defer func() { verbs[CommandTodo] = saved }()
	for _, clash := range [][2]Verb{
		{{cmd: CommandTodo, words: "item", args: 1}, {cmd: CommandTodo, words: "item add"}},
		{{cmd: CommandTodo, words: "item add"}, {cmd: CommandTodo, words: "item", args: 1}},
		{{cmd: CommandTodo, words: "item"}, {cmd: CommandTodo, words: "item"}},
	} {
		verbs[CommandTodo] = nil
		register(clash[0])
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q then %q registered", clash[0].words, clash[1].words)
				}
			}()
			register(clash[1])
		}()
	}
}
