package toolview

import (
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/tools"
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
		{"repo_map", `{"focus_files":["a.go","b.go"]}`, Call{Name: "repo_map", Title: "Repo Map", Verb: "Mapping repository", Detail: ".", Params: []Param{{"focus_files", "a.go (+1)"}}, Known: true}},
		{"mcp_browser_click", `{"selector":"#go","ref":"e5"}`, Call{Name: "mcp_browser_click", Title: "Mcp Browser Click", Verb: "Clicking in browser", Detail: "#go", Known: true}},
		{"mcp_browser_browser_navigate", `{"url":"https://example.com"}`, Call{Name: "mcp_browser_browser_navigate", Title: "Mcp Browser Browser Navigate", Verb: "Opening page", Detail: "https://example.com", Known: true}},
		{"mcp_browser_browser_take_screenshot", `{}`, Call{Name: "mcp_browser_browser_take_screenshot", Title: "Mcp Browser Browser Take Screenshot", Verb: "Taking browser screenshot", Known: true}},
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
	want := FileChange{Paths: []string{"a.go"}, Edits: 3, Done: true, Files: []tools.FileChange{{Path: "/w/a.go", OldContent: "x", NewContent: "y", Additions: 2, Removals: 1}}, Additions: 2, Removals: 1}
	if !ok || !reflect.DeepEqual(change, want) {
		t.Fatalf("change = %+v, want %+v", change, want)
	}
	if pending, ok := FileChangeOf("write", `{"file_path":"b.go","content":"z"}`, ""); !ok || pending.Done || pending.Paths[0] != "b.go" {
		t.Fatalf("pending write = %+v", pending)
	}
	patch := `{"input":"*** Begin Patch\n*** Add File: a.go\n+x\n*** Update File: b.go\n*** Move to: c.go\n@@\n-y\n+z\n*** End Patch"}`
	meta := `{"files":[{"file_path":"/w/a.go","new_content":"x\n","additions":1},{"file_path":"/w/c.go","moved_from":"/w/b.go","additions":1,"removals":1}]}`
	if change, ok := FileChangeOf("apply_patch", patch, meta); !ok || !reflect.DeepEqual(change.Paths, []string{"a.go", "b.go", "c.go"}) || len(change.Files) != 2 || change.Additions != 2 || change.Removals != 1 {
		t.Fatalf("patch = %+v", change)
	}
	if _, ok := FileChangeOf("bash", `{}`, ""); ok {
		t.Fatal("bash is not a file change")
	}
}
