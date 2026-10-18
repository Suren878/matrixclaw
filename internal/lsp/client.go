package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/procsup"
)

const (
	initializeTimeout = 45 * time.Second
	shutdownTimeout   = 2 * time.Second
	// maxOpenDocuments bounds the documents kept open on one server; the one
	// used longest ago is closed first.
	maxOpenDocuments = 64
)

// client is one running language server for one root.
type client struct {
	lang *language
	root string
	argv []string
	proc *procsup.Process
	conn *conn
	caps serverCapabilities

	// ready is closed when initialize ended; err says how.
	ready chan struct{}
	err   error

	// procKey names the process in the supervisor; lastUsed and idle (the
	// idle stop) belong to the manager's lock.
	procKey  string
	lastUsed time.Time
	idle     *time.Timer

	// send keeps the document messages in order; mu guards the state below.
	send sync.Mutex
	mu   sync.Mutex
	docs map[string]*document
	// pushes counts publishDiagnostics; diags keeps the latest push per URI
	// and changed is closed and replaced on every push.
	pushes  int
	diags   map[string]published
	changed chan struct{}
	tick    int
}

// document is an open document: the version and text last sent, the file's
// modification time then, the push count when it was sent, and when it was
// last used.
type document struct {
	version int
	text    []byte
	modTime time.Time
	sent    int
	used    int
}

type published struct {
	push        int
	version     *int
	diagnostics []Diagnostic
}

func newClient(lang *language, root string, argv []string) *client {
	return &client{lang: lang, root: root, argv: argv, ready: make(chan struct{}),
		docs: map[string]*document{}, diags: map[string]published{}, changed: make(chan struct{})}
}

// start runs the server under procs and initializes it.
func (c *client) start(ctx context.Context, procs *procsup.Supervisor, key string, env []string) error {
	proc, err := procs.Start(ctx, procsup.Spec{Key: key, Path: c.argv[0], Args: c.argv[1:], Env: env,
		Stdin: true, Stdout: true, Log: "lsp-" + c.lang.name + ".log"})
	if err != nil {
		return err
	}
	c.proc = proc
	c.conn = newConn(proc.Stdout(), proc.Use, c.notification, c.request)
	ctx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	var result struct {
		Capabilities serverCapabilities `json:"capabilities"`
	}
	if err := c.conn.call(ctx, "initialize", c.initializeParams(), &result); err != nil {
		return err
	}
	c.caps = result.Capabilities
	return c.conn.notify("initialized", struct{}{})
}

func (c *client) initializeParams() map[string]any {
	uri := fileURI(c.root)
	return map[string]any{
		"processId":             os.Getpid(),
		"clientInfo":            map[string]string{"name": "matrixclaw"},
		"rootUri":               uri,
		"rootPath":              c.root,
		"workspaceFolders":      []map[string]string{{"uri": uri, "name": filepath.Base(c.root)}},
		"initializationOptions": c.lang.settings,
		"capabilities": map[string]any{
			"workspace": map[string]any{"configuration": true, "workspaceFolders": true, "symbol": map[string]any{}},
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didSave": true},
				"publishDiagnostics": map[string]any{"versionSupport": true},
				"hover":              map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
				"definition":         map[string]any{"linkSupport": true},
				"implementation":     map[string]any{"linkSupport": true},
				"references":         map[string]any{},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
			},
			"window": map[string]any{"workDoneProgress": false},
		},
	}
}

// alive reports whether the server is initialized and still connected.
func (c *client) alive() bool {
	select {
	case <-c.ready:
	default:
		return true
	}
	if c.err != nil {
		return false
	}
	select {
	case <-c.conn.done:
		return false
	default:
		return true
	}
}

// wait waits until initialize ended.
func (c *client) wait(ctx context.Context) error {
	select {
	case <-c.ready:
		return c.err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (c *client) notification(method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var push publishDiagnosticsParams
	if json.Unmarshal(params, &push) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pushes++
	c.diags[normalURI(push.URI)] = published{push: c.pushes, version: push.Version, diagnostics: push.Diagnostics}
	close(c.changed)
	c.changed = make(chan struct{})
}

// request answers what a server asks the client; anything not listed is
// refused as not found.
func (c *client) request(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "workspace/configuration":
		var req struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		_ = json.Unmarshal(params, &req)
		out := make([]any, len(req.Items))
		for i, item := range req.Items {
			if item.Section == c.lang.section {
				out[i] = c.lang.settings
			}
		}
		return out, nil
	case "workspace/workspaceFolders":
		return []map[string]string{{"uri": fileURI(c.root), "name": filepath.Base(c.root)}}, nil
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create", "window/showMessageRequest":
		return nil, nil
	case "workspace/applyEdit":
		return map[string]bool{"applied": false}, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "method not supported: " + method}
}

// change is one document sent to the server: its URI, the version it was
// sent with, and the push count at that moment.
type change struct {
	uri     string
	version int
	pushes  int
}

// sync sends the server the current text of file: open it, or change it when
// it differs from what was sent; a missing file is closed. ok is false when
// there is nothing to wait for. Messages go out under c.send and outside
// c.mu, which the reading goroutine takes for every push.
func (c *client) sync(file string) (change, bool, error) {
	c.send.Lock()
	defer c.send.Unlock()
	uri := fileURI(file)
	text, err := os.ReadFile(file)
	info, statErr := os.Stat(file)
	c.mu.Lock()
	doc := c.docs[uri]
	if err != nil || statErr != nil {
		delete(c.docs, uri)
		c.mu.Unlock()
		if doc == nil {
			return change{}, false, nil
		}
		return change{}, false, c.conn.notify("textDocument/didClose", map[string]any{"textDocument": textDocumentIdentifier{URI: uri}})
	}
	c.tick++
	var messages []message
	switch {
	case doc == nil:
		if closed := c.oldest(); closed != "" {
			delete(c.docs, closed)
			messages = append(messages, notification("textDocument/didClose", map[string]any{"textDocument": textDocumentIdentifier{URI: closed}}))
		}
		doc = &document{version: 1, text: text, modTime: info.ModTime(), sent: c.pushes, used: c.tick}
		c.docs[uri] = doc
		messages = append(messages, notification("textDocument/didOpen", map[string]any{"textDocument": textDocumentItem{
			URI: uri, LanguageID: c.lang.ids[strings.ToLower(filepath.Ext(file))], Version: 1, Text: string(text)}}))
	case !bytes.Equal(doc.text, text):
		doc.version++
		doc.text = text
		doc.sent = c.pushes
		messages = append(messages, notification("textDocument/didChange", map[string]any{
			"textDocument":   versionedTextDocumentIdentifier{URI: uri, Version: doc.version},
			"contentChanges": []map[string]string{{"text": string(text)}},
		}))
	}
	doc.used, doc.modTime = c.tick, info.ModTime()
	ch := change{uri: uri, version: doc.version, pushes: doc.sent}
	c.mu.Unlock()
	if len(messages) > 0 && c.caps.wantsSave() {
		messages = append(messages, notification("textDocument/didSave", map[string]any{"textDocument": textDocumentIdentifier{URI: uri}}))
	}
	for _, msg := range messages {
		if err := c.conn.sendRaw(msg); err != nil {
			return change{}, false, err
		}
	}
	return ch, true, nil
}

// refresh syncs the open documents whose files changed on disk since they
// were sent (a shell command or another tool wrote them).
func (c *client) refresh() error {
	c.mu.Lock()
	var stale []string
	for uri, doc := range c.docs {
		if info, err := os.Stat(uriPath(uri)); err != nil || !info.ModTime().Equal(doc.modTime) {
			stale = append(stale, uriPath(uri))
		}
	}
	c.mu.Unlock()
	var errs []error
	for _, file := range stale {
		_, _, err := c.sync(file)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// oldest is the least recently used document when the server has
// maxOpenDocuments open, "" otherwise; c.mu is held.
func (c *client) oldest() string {
	if len(c.docs) < maxOpenDocuments {
		return ""
	}
	oldest := ""
	for uri, doc := range c.docs {
		if oldest == "" || doc.used < c.docs[oldest].used {
			oldest = uri
		}
	}
	return oldest
}

// fresh reports whether a push answers ch: it names ch's version or a later
// one, or, from servers that send no version, came after ch was sent.
func (p published) fresh(ch change) bool {
	if p.version != nil {
		return *p.version >= ch.version
	}
	return p.push > ch.pushes
}

// shutdown asks the server to exit; the supervisor kills it afterwards.
func (c *client) shutdown() {
	if c.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if c.conn.call(ctx, "shutdown", nil, nil) == nil {
		_ = c.conn.notify("exit", nil)
	}
}

// normalURI spells a file URI the way fileURI does, so a server's spelling
// finds the document.
func normalURI(uri string) string {
	if path := uriPath(uri); path != "" {
		return fileURI(path)
	}
	return uri
}
