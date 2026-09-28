package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// SessionTodo returns the session's todo list.
func (c *Core) SessionTodo(ctx context.Context, sessionID string) (todo.List, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return todo.List{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return todo.List{}, err
	}
	return c.store.GetSessionTodo(ctx, sessionID)
}

// ClearSessionTodo empties the session's todo list.
func (c *Core) ClearSessionTodo(ctx context.Context, sessionID string) (todo.List, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return todo.List{}, ErrSessionRequired
	}
	if _, err := c.store.GetSession(ctx, sessionID); err != nil {
		return todo.List{}, err
	}
	return c.saveSessionTodo(ctx, todo.List{SessionID: sessionID})
}

// writeSessionTodo replaces the session's todo list with items written by
// runID; the list belongs to the chain of /continue runs that runID ends.
func (c *Core) writeSessionTodo(ctx context.Context, sessionID string, runID string, items []todo.Item) (todo.List, error) {
	list := todo.List{SessionID: sessionID, Items: items, ChainRunID: runID, UpdatedRunID: runID}
	if runID != "" {
		run, err := c.store.GetRun(ctx, runID)
		if err != nil {
			return todo.List{}, err
		}
		continues, err := c.continuedRuns(ctx, run)
		if err != nil {
			return todo.List{}, err
		}
		if len(continues) > 0 {
			list.ChainRunID = continues[len(continues)-1]
		}
	}
	return c.saveSessionTodo(ctx, list)
}

// sessionTodoPrompt is the todo list as the context note of chain (a run and
// the runs it continues) shows it; a finished list of another chain is left out.
func (c *Core) sessionTodoPrompt(ctx context.Context, sessionID string, chain []string) string {
	list, err := c.store.GetSessionTodo(ctx, sessionID)
	if err != nil || len(list.Items) == 0 || (!list.InChain(chain) && len(todo.Open(list.Items)) == 0) {
		return ""
	}
	return "Todo list (keep it current with todo_write):\n" + todo.Text(list.Items)
}

// saveSessionTodo stores the list and announces it; an empty list has items [].
func (c *Core) saveSessionTodo(ctx context.Context, list todo.List) (todo.List, error) {
	if list.Items == nil {
		list.Items = []todo.Item{}
	}
	list.UpdatedAt = c.now().UTC()
	if err := c.store.SaveSessionTodo(ctx, list); err != nil {
		return todo.List{}, err
	}
	c.publishEvent(Event{Type: EventTodoUpdated, SessionID: list.SessionID, Payload: list})
	return list, nil
}

// coreTodos is the engine's Todos port.
type coreTodos struct {
	c *Core
}

func (t coreTodos) Open(ctx context.Context, sessionID string, chain []string) ([]todo.Item, error) {
	list, err := t.c.store.GetSessionTodo(ctx, sessionID)
	if err != nil || !list.InChain(chain) {
		return nil, err
	}
	return todo.Open(list.Items), nil
}

// TodoToolExecutors exposes the session's todo list to models.
func TodoToolExecutors(app *Core) []tools.Executor {
	if app == nil {
		return nil
	}
	return []tools.Executor{&todoTool{app: app}}
}

type todoTool struct {
	app *Core
}

// Spec marks todo_write read-only: it changes only the agent's own list and
// replaces it whole, so a replay after a restart is harmless.
func (t *todoTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              todo.ToolName,
		Name:            "TodoWrite",
		Description:     "Replace this session's todo list. Use it for work of three or more steps: write the steps down, mark one in_progress before you start it and completed as soon as it is done. Send the whole list every time; an empty list clears it.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.todo",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileReadOnly, tools.ProfileCoding, tools.ProfileAutomation},
		OutputKind:      tools.OutputText,
		InputJSONSchema: todoToolSchema,
	}
}

// ConcurrencyKey serialises one session's todo writes, so the calls of a batch
// replace the list one after another.
func (t *todoTool) ConcurrencyKey(call tools.Call) string {
	return "todo:" + normalizeText(call.SessionID)
}

func (t *todoTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	items, err := todo.Parse(call.Args)
	if err != nil {
		return tools.Result{Content: "Todo list not saved: " + err.Error() + ".", IsError: true, Status: tools.ResultStatusError}, nil
	}
	if _, err := t.app.writeSessionTodo(ctx, normalizeText(call.SessionID), normalizeText(call.RunID), items); err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Content: todoToolResult(items), Status: tools.ResultStatusSuccess}, nil
}

func todoToolResult(items []todo.Item) string {
	if len(items) == 0 {
		return "Todo list cleared."
	}
	return fmt.Sprintf("Todo list saved: %d items, %d open. Keep it current as you work.", len(items), len(todo.Open(items)))
}

var todoToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "items": {
      "type": "array",
      "description": "The whole todo list in order. An empty array clears it.",
      "items": {
        "type": "object",
        "properties": {
          "content": {"type": "string", "description": "The step in the imperative, such as \"Run the tests\"."},
          "active_form": {"type": "string", "description": "The step in the present continuous, shown while it is in progress, such as \"Running the tests\"."},
          "status": {"type": "string", "enum": ["pending", "in_progress", "completed"]}
        },
        "required": ["content", "status"],
        "additionalProperties": false
      }
    }
  },
  "required": ["items"],
  "additionalProperties": false
}`)
