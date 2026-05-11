package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (c *Client) ResolveApproval(ctx context.Context, approvalID string, approved bool) (core.Approval, error) {
	var response core.ApprovalResponse
	path := "/v1/approvals/" + escapedPath(approvalID) + "/resolve"
	request := core.ApprovalResolveRequest{Approved: approved}
	if err := c.doJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return core.Approval{}, err
	}
	return response.Approval, nil
}

func (c *Client) GetRun(ctx context.Context, runID string) (core.Run, error) {
	var response core.RunResponse
	path := "/v1/runs/" + escapedPath(runID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return core.Run{}, err
	}
	return response.Run, nil
}

func (c *Client) CancelRun(ctx context.Context, runID string) (core.Run, error) {
	var response core.RunResponse
	path := "/v1/runs/" + escapedPath(runID) + "/cancel"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return core.Run{}, err
	}
	return response.Run, nil
}
