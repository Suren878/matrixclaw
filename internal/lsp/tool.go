package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// ToolName is the code navigation tool.
const ToolName = "lsp"

const (
	toolTimeout    = 60 * time.Second
	maxLocations   = 100
	maxSymbols     = 300
	maxHoverRunes  = 6000
	maxSnippetRune = 200
)

var operations = []string{"definition", "references", "hover", "implementation", "document_symbols", "workspace_symbols"}

const toolDescription = "Ask the project's language server (gopls, typescript-language-server, pyright or pylsp, rust-analyzer) about code: " +
	"`definition` and `implementation` of the symbol at a position, its `references`, `hover` (type and documentation), " +
	"`document_symbols` (the outline of file_path) and `workspace_symbols` (symbols matching `query` in the whole project; file_path names the language and project). " +
	"line and character are 1-based, as in read output; point at the symbol's name. " +
	"Prefer it to grep for finding where a symbol is defined, used or implemented: it follows the language's scopes and types. " +
	"Results are paths with line numbers and the source line."

var inputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "operation": {"type": "string", "enum": ["definition", "references", "hover", "implementation", "document_symbols", "workspace_symbols"]},
    "file_path": {"type": "string", "description": "Absolute path, or relative to the working directory."},
    "line": {"type": "integer", "minimum": 1, "description": "1-based line of the symbol (definition, references, hover, implementation)."},
    "character": {"type": "integer", "minimum": 1, "description": "1-based character column of the symbol on that line."},
    "query": {"type": "string", "description": "Symbol name or part of it (workspace_symbols)."}
  },
  "required": ["operation", "file_path"],
  "additionalProperties": false
}`)

type toolParams struct {
	Operation string `json:"operation"`
	FilePath  string `json:"file_path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
	Query     string `json:"query"`
}

type tool struct{ m *Manager }

// NewTool is the `lsp` tool over m's servers.
func NewTool(m *Manager) tools.Executor { return tool{m: m} }

func (tool) Spec() tools.Spec {
	return tools.Spec{
		ID:              ToolName,
		Description:     toolDescription,
		Effect:          tools.EffectReadOnly,
		CoveredBy:       []string{"read"},
		Namespace:       "core.filesystem",
		Category:        tools.CategoryFilesystem,
		InputJSONSchema: inputSchema,
	}
}

// PermissionSubject is the file the call asks about, as for read.
func (tool) PermissionSubject(call tools.Call) permission.Subject {
	var params toolParams
	if json.Unmarshal(call.Args, &params) != nil || strings.TrimSpace(params.FilePath) == "" {
		return permission.Subject{}
	}
	policy, err := tools.ResolveFilesystemPath(call.WorkingDir, params.FilePath)
	if err != nil {
		return permission.Subject{}
	}
	return permission.Subject{Kind: permission.KindFile, Value: policy.RealPath}
}

func failed(format string, args ...any) (tools.Result, error) {
	return tools.Result{Content: fmt.Sprintf(format, args...), Status: tools.ResultStatusError}, nil
}

func (t tool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var params toolParams
	if err := json.Unmarshal(call.Args, &params); err != nil {
		return tools.Result{}, tools.InvalidArgs(ToolName, err)
	}
	positional := slices.Contains([]string{"definition", "references", "hover", "implementation"}, params.Operation)
	switch {
	case !slices.Contains(operations, params.Operation):
		return failed("operation must be one of %s", strings.Join(operations, ", "))
	case strings.TrimSpace(params.FilePath) == "":
		return failed("file_path is required")
	case positional && (params.Line < 1 || params.Character < 1):
		return failed("%s needs line and character, both 1-based", params.Operation)
	case params.Operation == "workspace_symbols" && strings.TrimSpace(params.Query) == "":
		return failed("workspace_symbols needs a query")
	}
	policy, err := tools.ResolveFilesystemPath(call.WorkingDir, params.FilePath)
	if err != nil {
		return failed("Invalid path: %v", err)
	}
	file := policy.RealPath
	if tools.IsCredentialPath(file) || tools.IsCredentialPath(policy.Path) {
		return failed("Reading MatrixClaw credential files is blocked.")
	}
	text, err := os.ReadFile(file)
	if err != nil {
		return failed("Cannot read %s: %v", policy.Path, err)
	}
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	c, err := t.m.ready(ctx, file)
	if errors.Is(err, errNoServer) {
		return failed("No language server serves %s: its language has none installed, or no project file (go.mod, package.json, pyproject.toml, Cargo.toml, ...) was found above it.", policy.Path)
	}
	if err != nil {
		return failed("The language server is not available: %v", err)
	}
	if _, _, err := c.sync(file); err != nil {
		return failed("The language server is not available: %v", err)
	}
	out := output{call: call, ctx: ctx}
	position := textDocumentPositionParams{
		TextDocument: textDocumentIdentifier{URI: fileURI(file)},
		Position:     Position{Line: params.Line - 1, Character: utf16Character(text, params.Line-1, params.Character)},
	}
	capability := map[string]json.RawMessage{
		"definition": c.caps.DefinitionProvider, "implementation": c.caps.ImplementationProvider,
		"references": c.caps.ReferencesProvider, "hover": c.caps.HoverProvider,
		"document_symbols": c.caps.DocumentSymbolProvider, "workspace_symbols": c.caps.WorkspaceSymbolProvider,
	}[params.Operation]
	if !provides(capability) {
		return failed("This language server does not support %s.", strings.ReplaceAll(params.Operation, "_", " "))
	}
	var content string
	switch params.Operation {
	case "definition", "implementation":
		locations, err := c.locations(ctx, "textDocument/"+params.Operation, position)
		if err != nil {
			return failed("%v", err)
		}
		content = out.locations(locations, params.Operation)
	case "references":
		var locations []Location
		err := c.conn.call(ctx, "textDocument/references", map[string]any{
			"textDocument": position.TextDocument, "position": position.Position,
			"context": map[string]bool{"includeDeclaration": true},
		}, &locations)
		if err != nil {
			return failed("%v", err)
		}
		content = out.locations(locations, "references")
	case "hover":
		var hover *hoverResult
		if err := c.conn.call(ctx, "textDocument/hover", position, &hover); err != nil {
			return failed("%v", err)
		}
		content = "No hover information at this position."
		if hover != nil && strings.TrimSpace(hover.text()) != "" {
			content = clipRunes(strings.TrimSpace(hover.text()), maxHoverRunes)
		}
	case "document_symbols":
		var symbols []documentSymbol
		if err := c.conn.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": position.TextDocument}, &symbols); err != nil {
			return failed("%v", err)
		}
		content = out.documentSymbols(symbols)
	case "workspace_symbols":
		var symbols []symbolInformation
		if err := c.conn.call(ctx, "workspace/symbol", map[string]string{"query": params.Query}, &symbols); err != nil {
			return failed("%v", err)
		}
		content = out.workspaceSymbols(symbols)
	}
	return tools.Result{Content: content + out.leftNote()}, nil
}

// locations asks for definition or implementation, which may answer with a
// Location, a list of them or LocationLinks.
func (c *client) locations(ctx context.Context, method string, position textDocumentPositionParams) ([]Location, error) {
	var raw json.RawMessage
	if err := c.conn.call(ctx, method, position, &raw); err != nil {
		return nil, err
	}
	var list []locationOrLink
	if json.Unmarshal(raw, &list) != nil {
		var one locationOrLink
		if json.Unmarshal(raw, &one) != nil || (one.URI == "" && one.TargetURI == "") {
			return nil, nil
		}
		list = []locationOrLink{one}
	}
	out := make([]Location, 0, len(list))
	for _, item := range list {
		out = append(out, item.location())
	}
	return out, nil
}

// output renders results, leaving out files the call may not read.
type output struct {
	call  tools.Call
	ctx   context.Context
	left  int
	files map[string][]byte
}

// readable reports whether the result may show path: not a credential place,
// not where keys live, and not caught by a deny or ask rule of the call.
func (o *output) readable(path string) bool {
	ok := path != "" && !tools.IsCredentialPath(path) && !tools.HoldsSecrets(path)
	if ok && o.call.Recheck != nil {
		ok = o.call.Recheck(o.ctx, permission.Subject{Kind: permission.KindFile, Value: path}) == nil
	}
	if !ok {
		o.left++
	}
	return ok
}

func (o *output) leftNote() string {
	if o.left == 0 {
		return ""
	}
	return fmt.Sprintf("\n(%d results in files you may not read were left out.)", o.left)
}

// text is the content of path, read once per call.
func (o *output) text(path string) []byte {
	if o.files == nil {
		o.files = map[string][]byte{}
	}
	text, ok := o.files[path]
	if !ok {
		text, _ = os.ReadFile(path)
		o.files[path] = text
	}
	return text
}

// where is a location as path:line:column and its source line.
func (o *output) where(loc Location) (at, snippet string, ok bool) {
	path := uriPath(loc.URI)
	if !o.readable(path) {
		return "", "", false
	}
	text, pos := o.text(path), loc.Range.Start
	line, _ := lineOf(text, pos.Line)
	return fmt.Sprintf("%s:%d:%d", path, pos.Line+1, column(text, pos)), clipRunes(strings.TrimSpace(string(line)), maxSnippetRune), true
}

func (o *output) locations(locations []Location, what string) string {
	if len(locations) == 0 {
		return "No " + what + " found."
	}
	var lines []string
	for _, loc := range locations {
		if len(lines) == maxLocations {
			lines = append(lines, fmt.Sprintf("... and %d more", len(locations)-maxLocations))
			break
		}
		if at, snippet, ok := o.where(loc); ok {
			lines = append(lines, at+": "+snippet)
		}
	}
	if len(lines) == 0 {
		return "No " + what + " found in files you may read."
	}
	header := fmt.Sprintf("%d %s:", len(locations), what)
	if len(locations) == 1 {
		header = ""
	}
	return strings.TrimPrefix(header+"\n"+strings.Join(lines, "\n"), "\n")
}

func (o *output) documentSymbols(symbols []documentSymbol) string {
	var lines []string
	var walk func(list []documentSymbol, depth int)
	walk = func(list []documentSymbol, depth int) {
		for _, s := range list {
			if len(lines) == maxSymbols {
				return
			}
			start := s.SelectionRange.Start
			if s.Location != nil {
				start = s.Location.Range.Start
			}
			entry := fmt.Sprintf("%s%s %s (line %d)", strings.Repeat("  ", depth), symbolKind(s.Kind), s.Name, start.Line+1)
			if s.Detail != "" {
				entry += " " + clipRunes(s.Detail, maxSnippetRune)
			}
			lines = append(lines, entry)
			walk(s.Children, depth+1)
		}
	}
	walk(symbols, 0)
	if len(lines) == 0 {
		return "No symbols found."
	}
	if len(lines) == maxSymbols {
		lines = append(lines, fmt.Sprintf("(showing the first %d symbols)", maxSymbols))
	}
	return strings.Join(lines, "\n")
}

func (o *output) workspaceSymbols(symbols []symbolInformation) string {
	var lines []string
	for _, s := range symbols {
		if len(lines) == maxLocations {
			lines = append(lines, fmt.Sprintf("... and %d more; narrow the query", len(symbols)-maxLocations))
			break
		}
		at, _, ok := o.where(s.Location)
		if !ok {
			continue
		}
		name := s.Name
		if s.ContainerName != "" && !strings.HasPrefix(name, s.ContainerName) {
			name = s.ContainerName + "." + s.Name
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", at, symbolKind(s.Kind), name))
	}
	if len(lines) == 0 {
		return "No symbols found."
	}
	return strings.Join(lines, "\n")
}

func clipRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "..."
}
