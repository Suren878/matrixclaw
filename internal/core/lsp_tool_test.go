package core_test

import (
	"context"
	"slices"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type lspTool struct{}

func (lspTool) Spec() tools.Spec { return recoveryToolSpec("lsp", tools.EffectReadOnly) }

func (lspTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Content: "ok"}, nil
}

func TestLSPToolIsOfferedOnlyWhereAServerServesTheProject(t *testing.T) {
	t.Parallel()
	for name, served := range map[string]bool{"served": true, "not served": false} {
		var dir string
		app, db, bash, dir := permissionCore(t, core.Config{LSPAvailable: func(workingDir string) bool { return served && workingDir == dir }})
		app.SetTools(tools.NewRegistry(append(tools.CoreExecutors(), bash, lspTool{})...))
		var offered []string
		app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
			for _, tool := range request.Tools {
				offered = append(offered, tool.Name)
			}
			return providers.Response{Text: "Done."}, nil
		})})
		session := permissionSession(t, db, "session_lsp", dir, core.PermissionModeDefault, "")
		if err := app.ExecuteRun(context.Background(), acceptRun(t, db, session, "where is A used?").ID); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(offered, "lsp") != served {
			t.Errorf("%s: offered %v", name, offered)
		}
	}
}
