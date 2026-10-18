package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as a fake language server (LSP_FAKE=1). It reports
// a line "ERROR: text" of a document as an error and "WARN: text" as a
// warning; "OTHERS: a.go b.go" also reports an error in each named file of the
// document's directory. Env: LSP_FAKE_DELAY delays every push (stale pushes
// of the previous text are sent first), LSP_FAKE_NO_VERSION leaves versions
// out of pushes.
func TestMain(m *testing.M) {
	if os.Getenv("LSP_FAKE") == "1" {
		fakeServer(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fake struct {
	out  io.Writer
	mu   sync.Mutex
	docs map[string]string
}

func fakeServer(in io.Reader, out io.Writer) {
	f := &fake{out: out, docs: map[string]string{}}
	reader := bufio.NewReader(in)
	headers := textproto.NewReader(reader)
	for {
		header, err := headers.ReadMIMEHeader()
		if err != nil {
			return
		}
		length, _ := strconv.Atoi(header.Get("Content-Length"))
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return
		}
		var msg message
		_ = json.Unmarshal(body, &msg)
		f.handle(msg)
	}
}

func (f *fake) send(msg message) {
	msg.JSONRPC = "2.0"
	body, _ := json.Marshal(msg)
	f.mu.Lock()
	defer f.mu.Unlock()
	_, _ = fmt.Fprintf(f.out, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func (f *fake) reply(msg message, result any) {
	raw, _ := json.Marshal(result)
	f.send(message{ID: msg.ID, Result: raw})
}

func (f *fake) handle(msg message) {
	var params struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Text    string `json:"text"`
			Version int    `json:"version"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
		Position Position `json:"position"`
		Query    string   `json:"query"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	uri := params.TextDocument.URI
	switch msg.Method {
	case "initialize":
		f.reply(msg, map[string]any{"capabilities": map[string]any{
			"textDocumentSync":   map[string]any{"openClose": true, "change": 1, "save": map[string]any{}},
			"definitionProvider": true, "referencesProvider": true, "hoverProvider": true,
			"documentSymbolProvider": true, "workspaceSymbolProvider": true,
		}})
		// A server request the client must answer.
		f.send(message{ID: json.RawMessage(`"cfg"`), Method: "workspace/configuration", Params: json.RawMessage(`{"items":[{"section":"fake"}]}`)})
	case "textDocument/didOpen":
		f.changed(uri, params.TextDocument.Version, params.TextDocument.Text)
	case "textDocument/didChange":
		f.changed(uri, params.TextDocument.Version, params.ContentChanges[0].Text)
	case "textDocument/definition":
		f.reply(msg, []map[string]any{{"targetUri": uri, "targetRange": Range{}, "targetSelectionRange": Range{Start: Position{Line: 2, Character: 5}}}})
	case "textDocument/references":
		other := fileURI(filepath.Join(filepath.Dir(uriPath(uri)), "b.go"))
		f.reply(msg, []Location{{URI: uri, Range: Range{Start: params.Position}}, {URI: other, Range: Range{Start: Position{Line: 0, Character: 3}}}})
	case "textDocument/hover":
		f.reply(msg, map[string]any{"contents": map[string]string{"kind": "markdown", "value": fmt.Sprintf("hover at %d:%d", params.Position.Line, params.Position.Character)}})
	case "textDocument/documentSymbol":
		f.reply(msg, []documentSymbol{{Name: "T", Kind: 23, SelectionRange: Range{Start: Position{Line: 2}}, Children: []documentSymbol{{Name: "M", Kind: 6, Detail: "func()", SelectionRange: Range{Start: Position{Line: 3}}}}}})
	case "workspace/symbol":
		f.reply(msg, []symbolInformation{{Name: params.Query, Kind: 12, ContainerName: "m", Location: Location{URI: fileURI(filepath.Join(os.Getenv("LSP_FAKE_ROOT"), "a.go")), Range: Range{Start: Position{Line: 2}}}}})
	case "shutdown":
		f.reply(msg, nil)
	case "exit":
		os.Exit(0)
	default:
		if len(msg.ID) > 0 && msg.Method != "" {
			f.send(message{ID: msg.ID, Error: &rpcError{Code: codeMethodNotFound, Message: "unsupported"}})
		}
	}
}

func (f *fake) changed(uri string, version int, text string) {
	f.mu.Lock()
	old, seen := f.docs[uri]
	f.docs[uri] = text
	f.mu.Unlock()
	delay, _ := time.ParseDuration(os.Getenv("LSP_FAKE_DELAY"))
	if delay > 0 && seen {
		f.publish(uri, version-1, old)
	}
	time.Sleep(delay)
	f.publish(uri, version, text)
}

func (f *fake) publish(uri string, version int, text string) {
	var diags []Diagnostic
	var others []string
	for i, line := range strings.Split(text, "\n") {
		for marker, severity := range map[string]int{"ERROR:": 1, "WARN:": 2} {
			if at := strings.Index(line, marker); at >= 0 {
				diags = append(diags, Diagnostic{Severity: severity, Message: strings.TrimSpace(line[at+len(marker):]),
					Range: Range{Start: Position{Line: i, Character: at}}})
			}
		}
		if names, ok := strings.CutPrefix(line, "OTHERS:"); ok {
			others = strings.Fields(names)
		}
	}
	push := map[string]any{"uri": uri, "diagnostics": diags}
	if os.Getenv("LSP_FAKE_NO_VERSION") == "" {
		push["version"] = version
	}
	raw, _ := json.Marshal(push)
	f.send(message{Method: "textDocument/publishDiagnostics", Params: raw})
	for _, name := range others {
		other := fileURI(filepath.Join(filepath.Dir(uriPath(uri)), name))
		raw, _ := json.Marshal(map[string]any{"uri": other, "diagnostics": []Diagnostic{{Severity: 1, Message: "broken by " + filepath.Base(uriPath(uri))}}})
		f.send(message{Method: "textDocument/publishDiagnostics", Params: raw})
	}
}

// fakeProject is a Go project served by the fake server; env adds to its
// environment.
func fakeProject(t *testing.T, env ...string) (*Manager, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/m\n")
	m := NewManager(t.TempDir(), func() []string {
		return append(os.Environ(), append([]string{"LSP_FAKE=1", "LSP_FAKE_ROOT=" + dir}, env...)...)
	}, func() Config { return Config{Servers: map[string][]string{"go": {exe}}} })
	m.logf = t.Logf
	t.Cleanup(m.Close)
	return m, dir
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
