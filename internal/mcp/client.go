package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Suren878/matrixclaw/internal/procsup"
	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// Session is a connection to one MCP server and the tools it offers.
type Session struct {
	server  ServerConfig
	session *sdk.ClientSession
	tools   []*sdk.Tool
	// mu serialises calls on the session.
	mu sync.Mutex
}

// Connect starts or reaches the server and lists its tools.
func Connect(ctx context.Context, cfg ServerConfig) (*Session, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := sdk.NewClient(&sdk.Implementation{
		Name:    "matrixclaw",
		Version: "1.0.0",
	}, &sdk.ClientOptions{Capabilities: &sdk.ClientCapabilities{}})
	session, err := client.Connect(connectCtx, serverTransport(cfg), nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: connect %s: %w", textutil.FirstNonEmpty(cfg.Name, cfg.ID), err)
	}
	toolsResult, err := session.ListTools(connectCtx, nil)
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("mcp: list tools for %s: %w", textutil.FirstNonEmpty(cfg.Name, cfg.ID), err)
	}
	return &Session{server: cfg, session: session, tools: toolsResult.Tools}, nil
}

// Name is the server's display name.
func (s *Session) Name() string { return textutil.FirstNonEmpty(s.server.Name, s.server.ID) }

// Tools are the server's tools as matrixclaw tools.
func (s *Session) Tools() []tools.Executor {
	out := make([]tools.Executor, 0, len(s.tools))
	for _, remoteTool := range s.tools {
		if remoteTool != nil {
			out = append(out, newRemoteToolExecutor(s.server, s, remoteTool))
		}
	}
	return out
}

func (s *Session) Close() error {
	return s.session.Close()
}

func serverTransport(cfg ServerConfig) sdk.Transport {
	if cfg.Transport == TransportHTTP {
		return &sdk.StreamableClientTransport{Endpoint: cfg.Endpoint}
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = append(os.Environ(), envPairs(cfg.Env)...)
	procsup.Prepare(cmd)
	return &sdk.CommandTransport{Command: cmd}
}

func envPairs(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" {
			out = append(out, key+"="+value)
		}
	}
	return out
}

type remoteToolExecutor struct {
	server     ServerConfig
	session    *Session
	remoteName string
	spec       tools.Spec
}

func newRemoteToolExecutor(server ServerConfig, session *Session, remoteTool *sdk.Tool) tools.Executor {
	inputSchema := toolInputSchema(remoteTool.InputSchema)
	name := strings.TrimSpace(remoteTool.Name)
	effect := remoteToolEffect(server, name)
	return &remoteToolExecutor{
		server:     server,
		session:    session,
		remoteName: name,
		spec: tools.Spec{
			ID:              ToolID(server.ToolPrefix, name),
			Description:     remoteToolDescription(server, remoteTool),
			Effect:          effect,
			Asks:            effect == tools.EffectMutation,
			Namespace:       "mcp." + server.ID,
			Category:        tools.CategoryWeb,
			InputJSONSchema: inputSchema,
		},
	}
}

// remoteToolEffect treats a remote tool as mutating, and so asking for
// approval, unless its server is marked read-only or it only reads a page.
func remoteToolEffect(server ServerConfig, remoteName string) tools.Effect {
	if server.ReadOnly || server.ID == "browser" && browserReadsPageOnly(remoteName) {
		return tools.EffectReadOnly
	}
	return tools.EffectMutation
}

// browserReadsPageOnly names the browser tools that only inspect the open page.
// Navigation reaches arbitrary hosts, private ones included, so it asks.
func browserReadsPageOnly(remoteName string) bool {
	switch strings.TrimSpace(remoteName) {
	case "browser_snapshot", "browser_console_messages", "browser_network_requests", "browser_network_request":
		return true
	default:
		return false
	}
}

func (e *remoteToolExecutor) Spec() tools.Spec {
	return e.spec
}

func (e *remoteToolExecutor) Preview(_ context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	return tools.ApprovalRequest{
		Description: "Call remote MCP tool " + e.remoteName + " on " + textutil.FirstNonEmpty(e.server.Name, e.server.ID),
		Params:      rawJSONMap(call.Args),
	}, nil
}

func (e *remoteToolExecutor) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if e == nil || e.session == nil || e.session.session == nil {
		return tools.Result{}, fmt.Errorf("mcp: remote session is not connected")
	}
	timeout := e.server.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	e.session.mu.Lock()
	result, err := e.session.session.CallTool(callCtx, &sdk.CallToolParams{
		Name:      e.remoteName,
		Arguments: rawJSONMap(call.Args),
	})
	e.session.mu.Unlock()
	if err != nil {
		return tools.Result{}, fmt.Errorf("mcp: call %s: %w", e.remoteName, err)
	}
	content := ResultContent(result)
	if content == "" {
		content = "MCP tool returned no content."
	}
	out := tools.Result{Content: content, Status: tools.ResultStatusSuccess}
	if result != nil && result.IsError {
		out.Status = tools.ResultStatusError
	}
	if result != nil && result.StructuredContent != nil {
		out.Metadata = result.StructuredContent
	}
	return out, nil
}

func rawJSONMap(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{"_raw": string(raw)}
	}
	if value == nil {
		return map[string]any{}
	}
	return value
}

func toolInputSchema(schema any) json.RawMessage {
	if schema == nil {
		return json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	raw, err := json.Marshal(schema)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	return compactToolInputSchema(raw)
}

func compactToolInputSchema(raw json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	stripJSONSchemaAnnotations(value, false)
	compact, err := json.Marshal(value)
	if err != nil || len(compact) == 0 || string(compact) == "null" {
		return raw
	}
	return compact
}

func stripJSONSchemaAnnotations(value any, inProperties bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if !inProperties && isJSONSchemaAnnotationKey(key) {
				delete(typed, key)
				continue
			}
			stripJSONSchemaAnnotations(child, key == "properties")
		}
	case []any:
		for _, child := range typed {
			stripJSONSchemaAnnotations(child, false)
		}
	}
}

func isJSONSchemaAnnotationKey(key string) bool {
	switch key {
	case "$schema", "description", "title", "markdownDescription", "examples":
		return true
	default:
		return false
	}
}

func remoteToolDescription(server ServerConfig, remoteTool *sdk.Tool) string {
	description := strings.TrimSpace(remoteTool.Description)
	if description == "" {
		description = "Remote MCP tool"
	}
	return description + " (remote MCP server: " + textutil.FirstNonEmpty(server.Name, server.ID) + ", tool: " + strings.TrimSpace(remoteTool.Name) + ")"
}
