package clientcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/mcp"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func runMCPCommand(stdout io.Writer, stderr io.Writer, binaryName string, service *setup.Service, args []string) int {
	subcommand := ""
	if len(args) > 0 {
		subcommand = strings.TrimSpace(args[0])
	}
	switch subcommand {
	case "serve":
		return runMCPServeCommand(stderr, binaryName, service, args[1:])
	case "help", "-h", "--help":
		printMCPUsage(stdout, binaryName)
		return 0
	default:
		printMCPUsage(stdout, binaryName)
		return 2
	}
}

func printMCPUsage(w io.Writer, binaryName string) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintf(w, "  %s mcp serve --session SESSION_ID [--workdir DIR]\n", binaryName)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Runs a stdio MCP server that proxies the matrixclaw daemon tool registry.")
}

func runMCPServeCommand(stderr io.Writer, binaryName string, service *setup.Service, args []string) int {
	sessionID, workingDir, err := parseMCPServeArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: mcp serve: %v\n", binaryName, err)
		return 2
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = strings.TrimSpace(os.Getenv("MATRIXCLAW_MCP_SESSION_ID"))
	}
	if strings.TrimSpace(sessionID) == "" {
		_, _ = fmt.Fprintf(stderr, "%s: mcp serve: --session or MATRIXCLAW_MCP_SESSION_ID is required\n", binaryName)
		return 2
	}

	ctx := context.Background()
	if _, err := ensureDaemon(ctx, service); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: mcp serve: ensure daemon: %v\n", binaryName, err)
		return 1
	}
	cfg, err := service.Load()
	if err != nil {
		return handleSetupReadError(stderr, binaryName, service, "mcp serve", err)
	}
	client := configuredDaemonClient(cfg)
	runtime := &daemonToolRuntime{
		client:     client,
		sessionID:  sessionID,
		workingDir: workingDir,
	}
	if err := mcp.RunStdioServer(ctx, runtime); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: mcp serve: %v\n", binaryName, err)
		return 1
	}
	return 0
}

func parseMCPServeArgs(args []string) (string, string, error) {
	sessionID := ""
	workingDir := ""
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch arg {
		case "--session", "-s":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("%s requires a value", arg)
			}
			sessionID = strings.TrimSpace(args[i])
		case "--workdir", "-C":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("%s requires a value", arg)
			}
			workingDir = strings.TrimSpace(args[i])
		case "":
			continue
		default:
			return "", "", fmt.Errorf("unknown argument %q", arg)
		}
	}
	return sessionID, workingDir, nil
}

type daemonToolRuntime struct {
	client interface {
		Tools(ctx context.Context) ([]tools.Spec, error)
		ExecuteTool(ctx context.Context, input core.ExecuteToolInput) (core.ExecuteToolResult, error)
	}
	sessionID  string
	workingDir string
}

func (r *daemonToolRuntime) List() []tools.Spec {
	specs, err := r.client.Tools(context.Background())
	if err != nil {
		return nil
	}
	return specs
}

func (r *daemonToolRuntime) Execute(ctx context.Context, toolID string, call tools.Call) (tools.Result, error) {
	result, err := r.client.ExecuteTool(ctx, core.ExecuteToolInput{
		SessionID:  r.sessionID,
		ToolName:   toolID,
		WorkingDir: firstNonEmptyTrimmed(call.WorkingDir, r.workingDir),
		Args:       call.Args,
	})
	if err != nil {
		return tools.Result{}, err
	}
	if result.Approval != nil {
		return tools.Result{
			Content: "matrixclaw approval required: " + result.Approval.Description,
			Status:  tools.ResultStatusNeutral,
			IsError: true,
			Metadata: map[string]any{
				"approval": result.Approval,
			},
		}, nil
	}
	if result.ToolResultMessage != nil {
		return tools.Result{
			Content: result.ToolResultMessage.Content,
			Status:  tools.ResultStatusSuccess,
		}, nil
	}
	return tools.Result{Content: "matrixclaw tool completed.", Status: tools.ResultStatusSuccess}, nil
}
