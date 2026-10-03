package telephony

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/telephony/phone"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type callInput struct {
	To                string `json:"to"`
	Objective         string `json:"objective,omitempty"`
	InitialMessage    string `json:"initial_message,omitempty"`
	Profile           string `json:"profile,omitempty"`
	SystemInstruction string `json:"system_instruction,omitempty"`
}

type callTool struct {
	module *Module
}

func (t *callTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          CallToolID,
		Description: "Place a real outbound phone call through the configured MatrixClaw telephony gateway and delegate a concrete phone conversation objective. Use only when the user asks to call a phone number or explicitly delegates a phone conversation.",
		Effect:      tools.EffectMutation,
		Asks:        true,
		Namespace:   "module.telephony",
		Category:    tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "to": {"type": "string", "description": "Destination phone number in international or provider format."},
    "objective": {"type": "string", "description": "Short task for the phone conversation, for example book a table, ask opening hours, confirm an order, or leave a message."},
    "system_instruction": {"type": "string", "description": "Optional detailed prompt for how the AI should behave on this call. If omitted, objective is used."},
    "initial_message": {"type": "string", "description": "Optional first phrase the AI should say after the other side speaks first. Keep it brief and natural."},
    "profile": {"type": "string", "description": "Optional telephony gateway profile, defaults to the configured profile."}
  },
  "required": ["to"],
  "additionalProperties": false
}`),
	}
}

// readCallInput reads a telephony_call call, the number normalised.
func readCallInput(call tools.Call) (callInput, error) {
	var input callInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return input, errors.New("invalid telephony_call arguments")
	}
	input.To = phone.Normalize(input.To)
	input.Objective = strings.TrimSpace(input.Objective)
	input.InitialMessage = strings.TrimSpace(input.InitialMessage)
	input.Profile = strings.TrimSpace(input.Profile)
	input.SystemInstruction = strings.TrimSpace(input.SystemInstruction)
	if input.To == "" {
		return input, errors.New("phone number is required")
	}
	return input, nil
}

func (t *callTool) Preview(_ context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	input, err := readCallInput(call)
	return tools.ApprovalRequest{Path: input.To, Description: approvalDescription(input), Params: input}, err
}

func (t *callTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	input, err := readCallInput(call)
	if err != nil {
		return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}, nil
	}
	cfg, gw := t.module.current()
	if gw == nil {
		return tools.Result{Content: "Telephony is not configured.", Status: tools.ResultStatusError}, nil
	}
	telephonyCfg := cfg.Modules.Telephony
	placed, err := gw.PlaceCall(ctx, map[string]any{
		"to":                  input.To,
		"profile":             firstNonEmpty(input.Profile, telephonyCfg.DefaultProfile),
		"objective":           input.Objective,
		"system_instruction":  input.SystemInstruction,
		"initial_message":     input.InitialMessage,
		"external_key":        firstNonEmpty(call.ExternalKey, input.To),
		"session_id":          call.SessionID,
		"origin_client":       call.Client,
		"origin_external_key": call.ExternalKey,
		"origin_session_id":   call.SessionID,
	})
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Telephony call failed: %s", err), Status: tools.ResultStatusError}, nil
	}
	return tools.Result{
		Content:  "Phone call started: " + input.To,
		Metadata: placed,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func approvalDescription(input callInput) string {
	objective := firstNonEmpty(input.Objective, input.InitialMessage)
	if objective == "" {
		return "Place a real outbound phone call to " + input.To + "."
	}
	return "Place a real outbound phone call to " + input.To + ": " + objective
}
