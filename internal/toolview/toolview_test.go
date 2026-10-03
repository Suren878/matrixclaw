package toolview

import (
	"reflect"
	"testing"
)

func TestDescribe(t *testing.T) {
	tests := []struct {
		name, input string
		want        Call
	}{
		{"web_search", `{"query":"go   parser\noff by one","limit":3}`, Call{Name: "web_search", Title: "Search Web", Verb: "Searching web", Detail: "go parser off by one", Params: []Param{{"limit", "3"}}, Known: true}},
		{"web_fetch", `{"url":"https://example.com","task":"ignored"}`, Call{Name: "web_fetch", Title: "Fetch Web Page", Verb: "Fetching web page", Detail: "https://example.com", Known: true}},
		{"agent", `{"description":"Scan","prompt":"scan","background":true}`, Call{Name: "agent", Title: "Agent", Verb: "Starting subagent", Detail: "Scan", Known: true}},
		{"ls", `{}`, Call{Name: "ls", Title: "List", Verb: "Listing files", Detail: ".", Known: true}},
		{"mcp_browser_click", `{"selector":"#go","ref":"e5"}`, Call{Name: "mcp_browser_click", Title: "Mcp Browser Click", Verb: "Clicking in browser", Detail: "#go", Known: true}},
		{"nearby_places_osm", `{"latitude":55.75,"longitude":37.6}`, Call{Name: "nearby_places_osm", Title: "Nearby Places", Verb: "Checking nearby places", Detail: "55.75, 37.6", Known: true}},
		{"create_reminder", `{"title":"Call","run_at":"2026-10-03T09:00:00+03:00","text":"call mom"}`, Call{Name: "create_reminder", Title: "⏰ Reminder", Verb: "Creating reminder", Detail: "Call · 2026-10-03 09:00 +03:00", Known: true}},
		{"lookup_thing", `{"urls":["a","b","c"],"path":"x","mode":"fast"}`, Call{Name: "lookup_thing", Title: "Lookup Thing", Verb: "Using lookup thing", Detail: "x", Params: []Param{{"mode", "fast"}}}},
		{"write", `not json`, Call{Name: "write", Title: "Write", Verb: "Writing file", Known: true}},
	}
	for _, tt := range tests {
		if got := Describe(tt.name, tt.input); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Describe(%s, %s)\n got %+v\nwant %+v", tt.name, tt.input, got, tt.want)
		}
	}
}

func TestValueShowsTheFirstListItemAndTheRestAsACount(t *testing.T) {
	if got := Value(map[string]any{"urls": []any{"a", "b", "c"}}, "urls"); got != "a (+2)" {
		t.Fatalf("Value = %q", got)
	}
}

func TestShortenCutsAtRunesWithAnEllipsis(t *testing.T) {
	if got := Shorten("привет мир", 7); got != "привет…" {
		t.Fatalf("Shorten = %q", got)
	}
	if got := Shorten("short", 7); got != "short" {
		t.Fatalf("Shorten = %q", got)
	}
}

func TestFileChangeOfReadsInputAndResult(t *testing.T) {
	change, ok := FileChangeOf("multiedit", `{"file_path":"a.go","edits":[{},{},{}]}`, `{"file_path":"/w/a.go","additions":2,"removals":1,"old_content":"x","new_content":"y"}`)
	want := FileChange{Path: "a.go", Edits: 3, Done: true, Old: "x", New: "y", Additions: 2, Removals: 1}
	if !ok || change != want {
		t.Fatalf("change = %+v, want %+v", change, want)
	}
	if pending, ok := FileChangeOf("write", `{"file_path":"b.go","content":"z"}`, ""); !ok || pending.Done || pending.Path != "b.go" {
		t.Fatalf("pending write = %+v", pending)
	}
	if written, _ := FileChangeOf("write", `{"file_path":"b.go","content":"z"}`, `{"additions":1}`); written.New != "z" {
		t.Fatalf("write without new content in metadata = %+v", written)
	}
	if _, ok := FileChangeOf("bash", `{}`, ""); ok {
		t.Fatal("bash is not a file change")
	}
}
