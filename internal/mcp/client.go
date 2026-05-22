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

	"github.com/Suren878/matrixclaw/internal/tools"
)

type ClientModule struct {
	config   Config
	sessions []*clientSession
}

type clientSession struct {
	server  ServerConfig
	session *sdk.ClientSession
	tools   []*sdk.Tool
}

func NewClientModule(ctx context.Context, cfg Config) (*ClientModule, error) {
	cfg = NormalizeConfig(cfg)
	module := &ClientModule{config: cfg}
	if !cfg.Enabled {
		return module, nil
	}
	for _, server := range cfg.Servers {
		if !server.Enabled {
			continue
		}
		session, err := connectServer(ctx, server)
		if err != nil {
			return nil, err
		}
		module.sessions = append(module.sessions, session)
	}
	return module, nil
}

func (m *ClientModule) RegisterTools(registry *tools.Registry) error {
	if m == nil || registry == nil {
		return nil
	}
	var registerErr error
	for _, session := range m.sessions {
		for _, remoteTool := range session.tools {
			if remoteTool == nil {
				continue
			}
			if err := registry.Register(newRemoteToolExecutor(session.server, session.session, remoteTool)); err != nil {
				registerErr = err
			}
		}
	}
	return registerErr
}

func (m *ClientModule) Context() string {
	if m == nil || len(m.sessions) == 0 {
		return ""
	}
	count := 0
	names := make([]string, 0, len(m.sessions))
	for _, session := range m.sessions {
		count += len(session.tools)
		names = append(names, firstNonEmpty(session.server.Name, session.server.ID))
	}
	return fmt.Sprintf("MCP module:\n- Connected MCP servers: %s.\n- Remote MCP tools are available as matrixclaw tools prefixed with mcp_<server>_. Use them when they directly match the task.", strings.Join(names, ", ")) +
		fmt.Sprintf("\n- Remote MCP tool count: %d.", count)
}

func (m *ClientModule) Close() error {
	if m == nil {
		return nil
	}
	var closeErr error
	for _, session := range m.sessions {
		if session != nil && session.session != nil {
			if err := session.session.Close(); err != nil {
				closeErr = err
			}
		}
	}
	return closeErr
}

func connectServer(ctx context.Context, cfg ServerConfig) (*clientSession, error) {
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
		return nil, fmt.Errorf("mcp: connect %s: %w", firstNonEmpty(cfg.Name, cfg.ID), err)
	}
	toolsResult, err := session.ListTools(connectCtx, nil)
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("mcp: list tools for %s: %w", firstNonEmpty(cfg.Name, cfg.ID), err)
	}
	return &clientSession{server: cfg, session: session, tools: toolsResult.Tools}, nil
}

func serverTransport(cfg ServerConfig) sdk.Transport {
	if cfg.Transport == TransportHTTP {
		return &sdk.StreamableClientTransport{Endpoint: cfg.Endpoint}
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = append(os.Environ(), envPairs(cfg.Env)...)
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
	session    *sdk.ClientSession
	remoteName string
	spec       tools.Spec
	mu         sync.Mutex
}

func newRemoteToolExecutor(server ServerConfig, session *sdk.ClientSession, remoteTool *sdk.Tool) tools.Executor {
	inputSchema := toolInputSchema(remoteTool.InputSchema)
	effect := tools.EffectMutation
	risk := tools.RiskApproval
	approval := tools.ApprovalOnRequest
	if server.ReadOnly {
		effect = tools.EffectReadOnly
		risk = tools.RiskSafe
		approval = tools.ApprovalNever
	}
	name := strings.TrimSpace(remoteTool.Name)
	return &remoteToolExecutor{
		server:     server,
		session:    session,
		remoteName: name,
		spec: tools.Spec{
			ID:              ToolID(server.ToolPrefix, name),
			Name:            "MCP " + firstNonEmpty(server.Name, server.ID) + " " + name,
			Description:     remoteToolDescription(server, remoteTool),
			Risk:            risk,
			Effect:          effect,
			ApprovalMode:    approval,
			Namespace:       "mcp." + server.ID,
			Category:        tools.CategoryWeb,
			Profiles:        []tools.Profile{tools.ProfileWeb, tools.ProfileCoding},
			OutputKind:      tools.OutputText,
			InputJSONSchema: inputSchema,
		},
	}
}

func (e *remoteToolExecutor) Spec() tools.Spec {
	return e.spec
}

func (e *remoteToolExecutor) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if e == nil || e.session == nil {
		return tools.Result{}, fmt.Errorf("mcp: remote session is not connected")
	}
	if e.spec.RequiresApproval() && !call.Approved {
		return tools.Result{
			Content: "Approval required before calling remote MCP tool " + e.remoteName,
			Status:  tools.ResultStatusNeutral,
			Approval: &tools.ApprovalRequest{
				ToolID:      e.spec.ID,
				Action:      "mcp:" + e.server.ID + ":" + e.remoteName,
				Description: "Call remote MCP tool " + e.remoteName + " on " + firstNonEmpty(e.server.Name, e.server.ID),
				Params:      rawJSONMap(call.Args),
			},
		}, nil
	}
	timeout := e.server.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	e.mu.Lock()
	result, err := e.session.CallTool(callCtx, &sdk.CallToolParams{
		Name:      e.remoteName,
		Arguments: rawJSONMap(call.Args),
	})
	e.mu.Unlock()
	if err != nil {
		return tools.Result{}, fmt.Errorf("mcp: call %s: %w", e.remoteName, err)
	}
	content := ResultContent(result)
	if content == "" {
		content = "MCP tool returned no content."
	}
	out := tools.Result{Content: content, Status: tools.ResultStatusSuccess}
	if result != nil && result.IsError {
		out.IsError = true
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
	return raw
}

func remoteToolDescription(server ServerConfig, remoteTool *sdk.Tool) string {
	description := strings.TrimSpace(remoteTool.Description)
	if description == "" {
		description = "Remote MCP tool"
	}
	return description + " (remote MCP server: " + firstNonEmpty(server.Name, server.ID) + ", tool: " + strings.TrimSpace(remoteTool.Name) + ")"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
