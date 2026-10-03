package telephony

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

type endCallTool struct {
	module *Module
}

type endCallInput struct {
	CallID string `json:"call_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func (t *endCallTool) Spec() tools.Spec {
	return tools.Spec{
		ID:          EndCallToolID,
		Description: "End the current active MatrixClaw telephony call after saying goodbye. Use only from an active phone conversation when the call objective is complete, impossible, refused, or repeatedly taken off-topic.",
		Effect:      tools.EffectMutation,
		Namespace:   "module.telephony",
		Category:    tools.CategoryAutomation,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "call_id": {"type": "string", "description": "Current MatrixClaw telephony call id. Use the call_id provided in the phone instructions."},
    "reason": {"type": "string", "description": "Short internal reason for ending the call."}
  },
  "additionalProperties": false
}`),
	}
}

func (t *endCallTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input endCallInput
	if len(call.Args) > 0 {
		_ = json.Unmarshal(call.Args, &input)
	}
	callID := strings.TrimSpace(input.CallID)
	if callID == "" && strings.EqualFold(strings.TrimSpace(call.Client), "telephony") {
		callID = strings.TrimSpace(call.ExternalKey)
	}
	if callID == "" {
		return tools.Result{Content: "No active telephony call id was provided.", Status: tools.ResultStatusError}, nil
	}
	_, gw := t.module.current()
	if gw == nil {
		return tools.Result{Content: "Telephony is not configured.", Status: tools.ResultStatusError}, nil
	}
	if err := gw.EndCall(ctx, callID); err != nil {
		return tools.Result{Content: fmt.Sprintf("Telephony end call failed: %s", err), Status: tools.ResultStatusError}, nil
	}
	return tools.Result{Content: "Phone call ended.", Status: tools.ResultStatusSuccess}, nil
}
