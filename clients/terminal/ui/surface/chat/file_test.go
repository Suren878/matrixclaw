package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	xansi "github.com/charmbracelet/x/ansi"

	surfacedialog "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/dialog"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestReadToolRenderUsesFullPathInHeaderWithoutBody(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &ReadToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-read-1",
			Name:     "read",
			Input:    `{"file_path":"internal/api/server.go"}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-read-1",
			Name:       "read",
			Content:    "<file>\n1 package api\n2 \n3 func Serve() {}\n</file>",
		},
		Status: ToolStatusSuccess,
	}

	rendered := renderer.RenderTool(&sty, 80, opts)
	if !strings.Contains(rendered, "internal/api/server.go") {
		t.Fatalf("expected full path in read header, got %q", rendered)
	}
	if strings.Contains(xansi.Strip(rendered), "✓") {
		t.Fatalf("expected read header to omit status checkmark, got %q", rendered)
	}
	if strings.Contains(rendered, "\n") {
		t.Fatalf("expected single read render without body block, got %q", rendered)
	}
}

func TestReadToolRenderOmitsLimitFromHeader(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &ReadToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-read-limit",
			Name:     "read",
			Input:    `{"file_path":"internal/api/server.go","limit":200,"offset":10}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-read-limit",
			Name:       "read",
			Metadata:   `{"file_path":"internal/api/server.go","content":"package api"}`,
		},
		Status: ToolStatusSuccess,
	}

	rendered := renderer.RenderTool(&sty, 100, opts)
	plain := xansi.Strip(rendered)
	if strings.Contains(plain, "limit=") {
		t.Fatalf("expected read header to omit limit, got %q", plain)
	}
	if !strings.Contains(plain, "offset=10") {
		t.Fatalf("expected read header to keep offset, got %q", plain)
	}
}

func TestReadToolRenderUsesResolvedPathMetadata(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &ReadToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-read-resolved",
			Name:     "read",
			Input:    `{"file_path":"notes.txt"}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-read-resolved",
			Name:       "read",
			Metadata:   `{"requested_path":"notes.txt","resolved_path":"/Volumes/LVM/Downloads/project/notes.txt","working_dir":"/Volumes/LVM/Downloads/project","content":"hello"}`,
		},
		Status: ToolStatusSuccess,
	}

	rendered := xansi.Strip(renderer.RenderTool(&sty, 120, opts))
	if !strings.Contains(rendered, "/Volumes/LVM/Downloads/project/notes.txt") {
		t.Fatalf("expected read header to use resolved path metadata, got %q", rendered)
	}
	if strings.Contains(rendered, "Read notes.txt") {
		t.Fatalf("expected read header not to fall back to requested path, got %q", rendered)
	}
}

func TestListToolRenderUsesResolvedPathMetadata(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &LSToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-ls-resolved",
			Name:     "ls",
			Input:    `{"path":"Downloads"}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-ls-resolved",
			Name:       "ls",
			Content:    "notes.txt",
			Metadata:   `{"requested_path":"Downloads","resolved_path":"/Volumes/LVM/Downloads","working_dir":"/Volumes/LVM"}`,
		},
		Status: ToolStatusSuccess,
	}

	rendered := xansi.Strip(renderer.RenderTool(&sty, 120, opts))
	if !strings.Contains(rendered, "/Volumes/LVM/Downloads") {
		t.Fatalf("expected list header to use resolved path metadata, got %q", rendered)
	}
	if strings.Contains(rendered, "List Downloads") {
		t.Fatalf("expected list header not to fall back to requested path, got %q", rendered)
	}
}

func TestCommonReadRootAndRelativePaths(t *testing.T) {
	paths := []string{
		"/workspace/matrixclaw/internal/api/server.go",
		"/workspace/matrixclaw/internal/api/routes.go",
	}

	root := commonReadRoot(paths)
	if want := "/workspace/matrixclaw/internal/api"; root != want {
		t.Fatalf("commonReadRoot = %q, want %q", root, want)
	}

	rel := relativeReadPaths(root, paths)
	if len(rel) != 2 || rel[0] != "server.go" || rel[1] != "routes.go" {
		t.Fatalf("relativeReadPaths = %#v", rel)
	}
}

func TestReadGroupRenderShowsCommonRootAndRelativeFiles(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewReadGroupMessageItem(
		&sty,
		"msg-read-group",
		[]surfacemessage.ToolCall{
			{ID: "tool-1", Name: "read", Input: `{"file_path":"/workspace/matrixclaw/internal/api/server.go"}`, Finished: true},
			{ID: "tool-2", Name: "read", Input: `{"file_path":"/workspace/matrixclaw/internal/api/routes.go"}`, Finished: true},
		},
		[]surfacemessage.ToolResult{
			{ToolCallID: "tool-1", Name: "read", Metadata: metadataWithPath("/workspace/matrixclaw/internal/api/server.go")},
			{ToolCallID: "tool-2", Name: "read", Metadata: metadataWithPath("/workspace/matrixclaw/internal/api/routes.go")},
		},
	)

	rendered := item.RawRender(100)
	if !strings.Contains(rendered, "/workspace/matrixclaw/internal/api") {
		t.Fatalf("expected group header to include common root, got %q", rendered)
	}
	if !strings.Contains(rendered, "server.go") || !strings.Contains(rendered, "routes.go") {
		t.Fatalf("expected group read render to include relative file names, got %q", rendered)
	}
	if strings.Contains(rendered, "/workspace/matrixclaw/internal/api/server.go") ||
		strings.Contains(rendered, "/workspace/matrixclaw/internal/api/routes.go") {
		t.Fatalf("expected group read paths block to avoid full duplicated paths, got %q", rendered)
	}
}

func metadataWithPath(path string) string {
	return `{"file_path":"` + path + `","content":"package api"}`
}

var _ = tools.ReadParams{}

func TestWriteToolRenderShowsDiffSummaryInsteadOfFullContent(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &WriteToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-write-1",
			Name:     "write",
			Input:    `{"file_path":"internal/api/server.go","content":"package api\nfunc Serve() {}\n"}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-write-1",
			Name:       "write",
			Metadata:   `{"diff":"@@ ...","additions":2,"removals":1,"old_content":"package api\n","new_content":"package api\nfunc Serve() {}\n"}`,
		},
		Status: ToolStatusSuccess,
	}

	rendered := renderer.RenderTool(&sty, 100, opts)
	plain := xansi.Strip(rendered)
	if strings.Contains(xansi.Strip(rendered), "✓") {
		t.Fatalf("expected write summary to omit status checkmark, got %q", rendered)
	}
	if strings.Contains(plain, "\n") {
		t.Fatalf("expected write summary on one line, got %q", plain)
	}
	if !strings.Contains(rendered, "+2") || !strings.Contains(rendered, "-1") {
		t.Fatalf("expected diff counts in write summary, got %q", rendered)
	}
	if strings.Contains(rendered, "func Serve()") {
		t.Fatalf("expected full file content to be hidden, got %q", rendered)
	}
}

func TestEditToolRenderShowsOneLineDiffSummary(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &EditToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-edit-1",
			Name:     "edit",
			Input:    `{"file_path":"internal/api/server.go","old_string":"old","new_string":"new"}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-edit-1",
			Name:       "edit",
			Metadata:   `{"additions":19,"removals":3,"old_content":"old\n","new_content":"new\n"}`,
		},
		Status: ToolStatusSuccess,
	}

	rendered := renderer.RenderTool(&sty, 100, opts)
	plain := xansi.Strip(rendered)
	if strings.Contains(plain, "\n") {
		t.Fatalf("expected edit summary on one line, got %q", plain)
	}
	for _, want := range []string{"Edit", "internal/api/server.go", "+19", "-3", "press enter for diff"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q in edit summary, got %q", want, plain)
		}
	}
}

func TestMultiEditToolRenderShowsOneLineDiffSummary(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	renderer := &MultiEditToolRenderContext{}
	opts := &ToolRenderOpts{
		ToolCall: surfacemessage.ToolCall{
			ID:       "tool-multiedit-1",
			Name:     "multiedit",
			Input:    `{"file_path":"internal/api/server.go","edits":[{"old_string":"old","new_string":"new"},{"old_string":"before","new_string":"after"}]}`,
			Finished: true,
		},
		Result: &surfacemessage.ToolResult{
			ToolCallID: "tool-multiedit-1",
			Name:       "multiedit",
			Metadata:   `{"additions":5,"removals":2,"edits_applied":2,"old_content":"old\nbefore\n","new_content":"new\nafter\n"}`,
		},
		Status: ToolStatusSuccess,
	}

	rendered := renderer.RenderTool(&sty, 100, opts)
	plain := xansi.Strip(rendered)
	if strings.Contains(plain, "\n") {
		t.Fatalf("expected multi-edit summary on one line, got %q", plain)
	}
	for _, want := range []string{"Multi-Edit", "internal/api/server.go", "+5", "-2", "2 edits", "press enter for diff"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected %q in multi-edit summary, got %q", want, plain)
		}
	}
}

func TestWriteToolItemEnterOpensDiffPreview(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(
		&sty,
		"msg-tool",
		surfacemessage.ToolCall{
			ID:       "tool-write-1",
			Name:     "write",
			Input:    `{"file_path":"internal/api/server.go","content":"package api\nfunc Serve() {}\n"}`,
			Finished: true,
		},
		&surfacemessage.ToolResult{
			ToolCallID: "tool-write-1",
			Name:       "write",
			Metadata:   `{"diff":"@@ ...","additions":2,"removals":1,"old_content":"package api\n","new_content":"package api\nfunc Serve() {}\n"}`,
		},
		false,
	)

	handler, ok := item.(KeyEventHandler)
	if !ok {
		t.Fatalf("item does not implement KeyEventHandler: %T", item)
	}
	handled, cmd := handler.HandleKeyEvent(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected enter to open diff preview, handled=%v cmd=%v", handled, cmd)
	}
	msg := cmd()
	action, ok := msg.(surfacedialog.ActionOpenDiffPreview)
	if !ok {
		t.Fatalf("msg = %T, want ActionOpenDiffPreview", msg)
	}
	if action.Data.FilePath != "internal/api/server.go" {
		t.Fatalf("file path = %q, want internal/api/server.go", action.Data.FilePath)
	}
}

func TestReadToolItemEnterOpensFilePreview(t *testing.T) {
	sty := surfacestyles.DefaultStyles()
	item := NewToolMessageItem(
		&sty,
		"msg-tool",
		surfacemessage.ToolCall{
			ID:       "tool-read-1",
			Name:     "read",
			Input:    `{"file_path":"internal/api/server.go"}`,
			Finished: true,
		},
		&surfacemessage.ToolResult{
			ToolCallID: "tool-read-1",
			Name:       "read",
			Metadata:   `{"file_path":"internal/api/server.go","content":"package api\nfunc Serve() {}\n"}`,
		},
		false,
	)

	handler, ok := item.(KeyEventHandler)
	if !ok {
		t.Fatalf("item does not implement KeyEventHandler: %T", item)
	}
	handled, cmd := handler.HandleKeyEvent(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected enter to open file preview, handled=%v cmd=%v", handled, cmd)
	}
	msg := cmd()
	action, ok := msg.(surfacedialog.ActionOpenFilePreview)
	if !ok {
		t.Fatalf("msg = %T, want ActionOpenFilePreview", msg)
	}
	if action.Data.FilePath != "internal/api/server.go" {
		t.Fatalf("file path = %q, want internal/api/server.go", action.Data.FilePath)
	}
	if !strings.Contains(action.Data.Content, "func Serve()") {
		t.Fatalf("expected file preview content, got %q", action.Data.Content)
	}
	if !strings.Contains(action.Data.Content, "     1\tpackage api") || !strings.Contains(action.Data.Content, "     2\tfunc Serve()") {
		t.Fatalf("expected file preview line numbers, got %q", action.Data.Content)
	}
}
