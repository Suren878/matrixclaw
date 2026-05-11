package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/automation"
)

func (c *Client) CreateAutomationJob(ctx context.Context, input automation.CreateJobInput) (automation.Job, error) {
	var response automation.JobResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/automation/jobs", automation.NewCreateJobRequest(input), &response); err != nil {
		return automation.Job{}, err
	}
	return response.Job, nil
}

func (c *Client) ListAutomationJobs(ctx context.Context) ([]automation.Job, error) {
	var response automation.JobsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/automation/jobs", nil, &response); err != nil {
		return nil, err
	}
	return response.Jobs, nil
}

func (c *Client) PauseAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	return c.postAutomationJobAction(ctx, jobID, "pause")
}

func (c *Client) ResumeAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	return c.postAutomationJobAction(ctx, jobID, "resume")
}

func (c *Client) CompleteAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	return c.postAutomationJobAction(ctx, jobID, "complete")
}

func (c *Client) DeleteAutomationJob(ctx context.Context, jobID string) (automation.Job, error) {
	var response automation.JobResponse
	path := "/v1/automation/jobs/" + escapedPath(jobID)
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, &response); err != nil {
		return automation.Job{}, err
	}
	return response.Job, nil
}

func (c *Client) RunAutomationJobNow(ctx context.Context, jobID string) (automation.Fire, error) {
	var response automation.FireResponse
	path := "/v1/automation/jobs/" + escapedPath(jobID) + "/run-now"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return automation.Fire{}, err
	}
	return response.Fire, nil
}

func (c *Client) postAutomationJobAction(ctx context.Context, jobID string, action string) (automation.Job, error) {
	var response automation.JobResponse
	path := "/v1/automation/jobs/" + escapedPath(jobID) + "/" + action
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return automation.Job{}, err
	}
	return response.Job, nil
}
