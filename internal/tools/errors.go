package tools

import (
	"errors"
	"fmt"
)

var ErrInvalidArgs = errors.New("invalid tool arguments")

type InvalidArgsError struct {
	ToolID string
	Err    error
}

func InvalidArgs(toolID string, err error) error {
	return &InvalidArgsError{ToolID: normalizeToolID(toolID), Err: err}
}

func (e *InvalidArgsError) Error() string {
	if e == nil {
		return ErrInvalidArgs.Error()
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: invalid args", e.ToolID)
	}
	return fmt.Sprintf("%s: invalid args: %v", e.ToolID, e.Err)
}

func (e *InvalidArgsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *InvalidArgsError) Is(target error) bool {
	return target == ErrInvalidArgs
}

func invalidArgsResult(toolID string, err error) Result {
	tool := normalizeToolID(toolID)
	var invalid *InvalidArgsError
	if errors.As(err, &invalid) && invalid.ToolID != "" {
		tool = invalid.ToolID
	}
	content := fmt.Sprintf("Invalid %s arguments: expected one JSON object matching the tool input schema.", tool)
	if errors.As(err, &invalid) && invalid.Err != nil {
		content += " Parser error: " + invalid.Err.Error() + "."
	}
	content += " Fix the arguments and call the tool again."
	return Result{
		Content: content,
		Status:  ResultStatusError,
		IsError: true,
	}
}
