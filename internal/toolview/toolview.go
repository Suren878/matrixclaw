// Package toolview says how clients present a tool call: its title, the verb of
// a progress line, its main parameter and the secondary ones.
package toolview

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Call is the presentation of one tool call.
type Call struct {
	Name   string  // tool id, lower case
	Title  string  // header title, e.g. "Search Web"
	Verb   string  // progress phrase, e.g. "Searching web"
	Detail string  // main parameter, whitespace collapsed, not shortened
	Params []Param // secondary parameters in display order
	// Known is false for tools the table does not list.
	Known bool
}

// Param is one secondary parameter of a call.
type Param struct {
	Key   string
	Value string
}

type spec struct {
	title   string
	verb    string
	primary []string
	extra   []string
	// detail replaces the primary keys when set.
	detail func(params map[string]any) string
	// verbFor replaces verb when set.
	verbFor func(params map[string]any) string
}

var specs = map[string]spec{
	"bash":                     {title: "Run", verb: "Executing command", primary: []string{"command"}},
	"task_output":              {title: "Task Output", verb: "Reading task output", primary: []string{"id"}},
	"task_kill":                {title: "Task Kill", verb: "Stopping task", primary: []string{"id"}},
	"read":                     {title: "Read", verb: "Reading file", primary: []string{"file_path"}, extra: []string{"offset"}},
	"write":                    {title: "Write", verb: "Writing file", primary: []string{"file_path"}},
	"edit":                     {title: "Edit", verb: "Editing file", primary: []string{"file_path"}},
	"multiedit":                {title: "Multi-Edit", verb: "Editing file", primary: []string{"file_path"}},
	"glob":                     {title: "Glob", verb: "Searching files", primary: []string{"pattern"}, extra: []string{"path"}},
	"grep":                     {title: "Grep", verb: "Searching files", primary: []string{"pattern"}, extra: []string{"path", "include", "literal_text"}},
	"ls":                       {title: "List", verb: "Listing files", detail: func(params map[string]any) string { return cmp.Or(Value(params, "path"), ".") }},
	"web_search":               {title: "Search Web", verb: "Searching web", primary: []string{"query"}, extra: []string{"limit"}},
	"web_fetch":                {title: "Fetch Web Page", verb: "Fetching web page", primary: []string{"url"}},
	"session_search":           {title: "Search Sessions", verb: "Searching sessions", primary: []string{"query"}, extra: []string{"session_id", "limit"}},
	"skill_search":             {title: "Search Skills", verb: "Searching skills", primary: []string{"query"}, extra: []string{"session_id", "limit"}},
	"skill_view":               {title: "Skill View", verb: "Viewing skill", primary: []string{"id"}},
	"skill_use":                {title: "Skill Use", verb: "Loading skill", primary: []string{"id"}},
	"skill_manage":             {title: "Skill Manage", verb: "Managing skills", primary: []string{"action", "query", "id", "key", "content"}, extra: []string{"scope", "working_dir", "limit"}},
	"memory":                   {title: "Memory", verb: "Using memory", primary: []string{"action", "query", "id", "key", "content"}, extra: []string{"scope", "working_dir", "limit"}},
	"create_reminder":          {title: "⏰ Reminder", verb: "Creating reminder", detail: scheduled("text")},
	"create_scheduled_ai_task": {title: "🗓 Scheduled Task", verb: "Scheduling task", detail: scheduled("prompt")},
	"reverse_geocode_osm":      {title: "Reverse Geocode", verb: "Checking address", detail: coordinates},
	"nearby_places_osm": {title: "Nearby Places", verb: "Checking nearby places", detail: func(params map[string]any) string {
		return cmp.Or(coordinates(params), Value(params, "radius_m"))
	}},
	"agent": {title: "Agent", primary: []string{"description", "prompt"}, verbFor: func(params map[string]any) string {
		if Value(params, "background") == "true" {
			return "Starting subagent"
		}
		return "Subagent is working"
	}},
}

const browserPrefix = "mcp_browser_"

var browserSpec = spec{
	primary: []string{"url", "text", "selector", "element", "query", "ref", "name", "id"},
	extra:   []string{"button", "key", "timeout", "x", "y"},
}

var fallbackSpec = spec{
	primary: []string{"query", "url", "path", "file_path", "command", "action", "name", "id", "title", "text"},
	extra:   []string{"limit", "mode", "type"},
}

// Describe presents a call of the tool name with JSON input.
func Describe(name string, input string) Call {
	id := strings.ToLower(strings.TrimSpace(name))
	var params map[string]any
	_ = json.Unmarshal([]byte(input), &params)
	view, known := specs[id]
	if !known && strings.HasPrefix(id, browserPrefix) {
		view, known = browserSpec, true
		view.verb = browserVerb(strings.TrimPrefix(id, browserPrefix))
	}
	if !known {
		view = fallbackSpec
	}
	call := Call{Name: id, Title: view.title, Verb: view.verb, Known: known}
	if call.Title == "" {
		call.Title = PrettyName(name)
	}
	if view.verbFor != nil {
		call.Verb = view.verbFor(params)
	}
	if call.Verb == "" {
		call.Verb = "Using " + strings.ToLower(PrettyName(name))
	}
	if view.detail != nil {
		call.Detail = view.detail(params)
	} else {
		call.Detail = firstValue(params, view.primary)
	}
	if call.Detail == "" {
		return call
	}
	for _, key := range view.extra {
		if value := Value(params, key); value != "" {
			call.Params = append(call.Params, Param{Key: key, Value: value})
		}
	}
	return call
}

// Value is params[key] as one line of text: whitespace collapsed, a list as its
// first item and how many follow, numbers without trailing zeros.
func Value(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.Join(strings.Fields(typed), " ")
	case []any:
		if len(typed) == 0 {
			return ""
		}
		first := strings.Join(strings.Fields(fmt.Sprint(typed[0])), " ")
		if first == "" || len(typed) == 1 {
			return first
		}
		return fmt.Sprintf("%s (+%d)", first, len(typed)-1)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return ""
	default:
		return strings.Join(strings.Fields(fmt.Sprint(value)), " ")
	}
}

// Shorten cuts text to at most maxRunes runes, ending it with "…" when cut.
func Shorten(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes || maxRunes < 1 {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
}

// PrettyName turns a tool id into words: "skill_view" becomes "Skill View".
func PrettyName(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' || unicode.IsSpace(r) })
	if len(words) == 0 {
		return "Tool"
	}
	for i, word := range words {
		runes := []rune(strings.ToLower(word))
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func firstValue(params map[string]any, keys []string) string {
	for _, key := range keys {
		if value := Value(params, key); value != "" {
			return value
		}
	}
	return ""
}

// scheduled shows a scheduled item by its title and time, else by textKey.
func scheduled(textKey string) func(map[string]any) string {
	return func(params map[string]any) string {
		var parts []string
		if title := Value(params, "title"); title != "" {
			parts = append(parts, title)
		}
		if runAt := Value(params, "run_at"); runAt != "" {
			if parsed, err := time.Parse(time.RFC3339, runAt); err == nil {
				runAt = parsed.Format("2006-01-02 15:04 -07:00")
			}
			parts = append(parts, runAt)
		}
		if len(parts) == 0 {
			return Value(params, textKey)
		}
		return strings.Join(parts, " · ")
	}
}

func coordinates(params map[string]any) string {
	lat, lon := Value(params, "latitude"), Value(params, "longitude")
	if lat == "" || lon == "" {
		return ""
	}
	return lat + ", " + lon
}

func browserVerb(action string) string {
	switch action {
	case "navigate", "goto", "open":
		return "Opening page"
	case "click":
		return "Clicking in browser"
	case "type", "fill":
		return "Typing in browser"
	case "screenshot":
		return "Taking browser screenshot"
	case "wait":
		return "Waiting in browser"
	default:
		return "Using browser"
	}
}
