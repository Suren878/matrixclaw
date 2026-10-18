package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

// FileCheckpoints lists the session's file checkpoints, newest first.
func (c *Client) FileCheckpoints(ctx context.Context, sessionID string) (core.FileCheckpointsResponse, error) {
	var response core.FileCheckpointsResponse
	err := c.doJSON(ctx, http.MethodGet, "/v1/sessions/"+escapedPath(sessionID)+"/checkpoints", nil, &response)
	return response, err
}

// RestoreFileCheckpoint brings the session's files back to the checkpoint.
func (c *Client) RestoreFileCheckpoint(ctx context.Context, sessionID, checkpointID string) (core.FileCheckpointRestoreResponse, error) {
	var response core.FileCheckpointRestoreResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/"+escapedPath(sessionID)+"/checkpoints/"+escapedPath(checkpointID)+"/restore", nil, &response)
	return response, err
}
