package goworkflows

import goworkflow "github.com/cschleiden/go-workflows/workflow"

const executeRunActivityName = "matrixclaw.execute_run"

func runWorkflow(ctx goworkflow.Context, runID string) error {
	_, err := goworkflow.ExecuteActivity[bool](ctx, goworkflow.ActivityOptions{
		RetryOptions: goworkflow.RetryOptions{
			MaxAttempts: 1,
		},
	}, executeRunActivityName, runID).Get(ctx)
	return err
}
