package telephony

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

type endCallInput struct {
	CallID string `json:"call_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

func (t *endCallTool) Spec() tools.Spec {
	return tools.Spec{
		ID:           EndCallToolID,
		Name:         "Telephony End Call",
		Description:  "End the current active MatrixClaw telephony call after saying goodbye. Use only from an active phone conversation when the call objective is complete, impossible, refused, or repeatedly taken off-topic.",
		Risk:         tools.RiskSafe,
		Effect:       tools.EffectMutation,
		ApprovalMode: tools.ApprovalNever,
		Namespace:    "module.telephony",
		Category:     tools.CategoryAutomation,
		Profiles:     []tools.Profile{tools.ProfileAutomation, tools.ProfileCoding},
		OutputKind:   tools.OutputText,
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
		return tools.Result{Content: "No active telephony call id was provided.", IsError: true, Status: tools.ResultStatusError}, nil
	}
	cfg, err := t.gateway.config()
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true, Status: tools.ResultStatusError}, nil
	}
	telephonyCfg := cfg.Modules.Telephony
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(telephonyCfg.GatewayURL, "/")+"/v1/calls/"+url.PathEscape(callID), nil)
	if err != nil {
		return tools.Result{}, err
	}
	if strings.TrimSpace(telephonyCfg.GatewayToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(telephonyCfg.GatewayToken))
	}
	res, err := t.gateway.http.Do(req)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Telephony end call failed: %s", err), IsError: true, Status: tools.ResultStatusError}, nil
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return tools.Result{Content: fmt.Sprintf("Telephony gateway returned HTTP %d while ending the call.", res.StatusCode), IsError: true, Status: tools.ResultStatusError}, nil
	}
	return tools.Result{Content: "Phone call ended.", Status: tools.ResultStatusSuccess}, nil
}
