package lsp

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
)

// The few LSP 3.17 types the client uses; fields it does not read are left out.

// Position is zero-based; Character counts UTF-16 code units.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// locationOrLink reads a Location or a LocationLink.
type locationOrLink struct {
	URI                  string `json:"uri"`
	Range                *Range `json:"range"`
	TargetURI            string `json:"targetUri"`
	TargetSelectionRange *Range `json:"targetSelectionRange"`
}

func (l locationOrLink) location() Location {
	if l.TargetURI != "" && l.TargetSelectionRange != nil {
		return Location{URI: l.TargetURI, Range: *l.TargetSelectionRange}
	}
	if l.Range == nil {
		return Location{URI: l.URI}
	}
	return Location{URI: l.URI, Range: *l.Range}
}

// severityError is the only severity reported after edits.
const severityError = 1

type Diagnostic struct {
	Range    Range           `json:"range"`
	Severity int             `json:"severity,omitempty"`
	Code     json.RawMessage `json:"code,omitempty"`
	Source   string          `json:"source,omitempty"`
	Message  string          `json:"message"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// symbolInformation is a workspace symbol or a flat document symbol.
type symbolInformation struct {
	Name          string   `json:"name"`
	Kind          int      `json:"kind"`
	ContainerName string   `json:"containerName,omitempty"`
	Location      Location `json:"location"`
}

// documentSymbol is a hierarchical document symbol; a server answers with
// these or with symbolInformation.
type documentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []documentSymbol `json:"children,omitempty"`
	// Location is set only when the answer was symbolInformation.
	Location *Location `json:"location,omitempty"`
}

// symbolKinds names SymbolKind values 1-26.
var symbolKinds = []string{"", "file", "module", "namespace", "package", "class", "method", "property", "field",
	"constructor", "enum", "interface", "function", "variable", "constant", "string", "number", "boolean",
	"array", "object", "key", "null", "enum member", "struct", "event", "operator", "type parameter"}

func symbolKind(kind int) string {
	if kind > 0 && kind < len(symbolKinds) {
		return symbolKinds[kind]
	}
	return "symbol"
}

// hoverResult reads Hover contents: MarkupContent, a MarkedString or a list of them.
type hoverResult struct {
	Contents json.RawMessage `json:"contents"`
}

func (h hoverResult) text() string {
	var markup struct {
		Value    string `json:"value"`
		Language string `json:"language"`
	}
	var plain string
	var list []json.RawMessage
	switch {
	case json.Unmarshal(h.Contents, &plain) == nil:
		return plain
	case json.Unmarshal(h.Contents, &list) == nil:
		parts := make([]string, 0, len(list))
		for _, item := range list {
			parts = append(parts, hoverResult{Contents: item}.text())
		}
		return strings.Join(parts, "\n\n")
	case json.Unmarshal(h.Contents, &markup) == nil:
		if markup.Language != "" {
			return "```" + markup.Language + "\n" + markup.Value + "\n```"
		}
		return markup.Value
	}
	return ""
}

// serverCapabilities holds what the client checks before it asks.
type serverCapabilities struct {
	TextDocumentSync        json.RawMessage `json:"textDocumentSync,omitempty"`
	HoverProvider           json.RawMessage `json:"hoverProvider,omitempty"`
	DefinitionProvider      json.RawMessage `json:"definitionProvider,omitempty"`
	ReferencesProvider      json.RawMessage `json:"referencesProvider,omitempty"`
	ImplementationProvider  json.RawMessage `json:"implementationProvider,omitempty"`
	DocumentSymbolProvider  json.RawMessage `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider json.RawMessage `json:"workspaceSymbolProvider,omitempty"`
}

// provides reads a `boolean | Options` capability.
func provides(capability json.RawMessage) bool {
	value := strings.TrimSpace(string(capability))
	return value != "" && value != "null" && value != "false"
}

// wantsSave reports whether the server asked for didSave.
func (c serverCapabilities) wantsSave() bool {
	var sync struct {
		Save json.RawMessage `json:"save"`
	}
	return json.Unmarshal(c.TextDocumentSync, &sync) == nil && provides(sync.Save)
}

// fileURI is the file URI of an absolute path.
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

// uriPath is the path of a file URI; "" for any other URI.
func uriPath(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	return filepath.FromSlash(parsed.Path)
}
