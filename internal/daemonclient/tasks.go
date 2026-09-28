package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) SessionTasks(ctx context.Context, sessionID string) ([]core.Task, error) {
	var response core.SessionTasksResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions/"+escapedPath(sessionID)+"/tasks", nil, &response); err != nil {
		return nil, err
	}
	return response.Tasks, nil
}

func (c *Client) TaskDetail(ctx context.Context, taskID string) (core.TaskDetailResponse, error) {
	var response core.TaskDetailResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tasks/"+escapedPath(taskID), nil, &response); err != nil {
		return core.TaskDetailResponse{}, err
	}
	return response, nil
}

func (c *Client) CancelTask(ctx context.Context, taskID string) (core.Task, error) {
	var response core.TaskResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/tasks/"+escapedPath(taskID)+"/cancel", nil, &response); err != nil {
		return core.Task{}, err
	}
	return response.Task, nil
}
