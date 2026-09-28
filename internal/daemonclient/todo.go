package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) SessionTodo(ctx context.Context, sessionID string) (todo.List, error) {
	var response core.SessionTodoResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions/"+escapedPath(sessionID)+"/todo", nil, &response); err != nil {
		return todo.List{}, err
	}
	return response.Todo, nil
}

func (c *Client) ClearSessionTodo(ctx context.Context, sessionID string) (todo.List, error) {
	var response core.SessionTodoResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/v1/sessions/"+escapedPath(sessionID)+"/todo", nil, &response); err != nil {
		return todo.List{}, err
	}
	return response.Todo, nil
}
