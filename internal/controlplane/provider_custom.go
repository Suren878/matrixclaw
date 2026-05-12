package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleCustomProvider(ctx context.Context, session *core.Session, args string) (Result, error) {
	args = strings.TrimSpace(args)
	if args == "" {
		return customProviderTypePicker(), nil
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return customProviderTypePicker(), nil
	}

	switch strings.ToLower(strings.TrimSpace(fields[0])) {
	case "edit":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Provider id is required."}, nil
		}
		return d.handleProviderEdit(ctx, session, fields[1])
	case "delete":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Provider id is required."}, nil
		}
		return d.customProviderDeleteConfirm(ctx, fields[1])
	case "delete-confirm":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Provider id is required."}, nil
		}
		return d.deleteCustomProvider(ctx, fields[1])
	default:
		return d.handleCustomProviderCreate(ctx, session, args, fields[0])
	}
}

func customProviderTypePicker() Result {
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, "Custom Provider").
			Back("/provider").
			Row("openai", "OpenAI Compatible", "").
			Row("anthropic", "Anthropic Compatible", "").
			Ptr(),
	}
}
