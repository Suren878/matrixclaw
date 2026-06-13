package telephony

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const CallToolID = "telephony_call"

type setupLoader interface {
	Load() (setup.Config, error)
}

type callTool struct {
	setup setupLoader
	http  *http.Client
}

type callInput struct {
	To                string `json:"to"`
	Objective         string `json:"objective,omitempty"`
	InitialMessage    string `json:"initial_message,omitempty"`
	Profile           string `json:"profile,omitempty"`
	SystemInstruction string `json:"system_instruction,omitempty"`
}

type callResponse struct {
	Call any `json:"call"`
}

func NewCallTool(setupService setupLoader) tools.Executor {
	return &callTool{
		setup: setupService,
		http:  &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *callTool) Spec() tools.Spec {
	return tools.Spec{
		ID:           CallToolID,
		Name:         "Telephony Call",
		Description:  "Place a real outbound phone call through the configured MatrixClaw telephony gateway and delegate a concrete phone conversation objective. Use only when the user asks to call a phone number or explicitly delegates a phone conversation.",
		Risk:         tools.RiskApproval,
		Effect:       tools.EffectMutation,
		ApprovalMode: tools.ApprovalOnRequest,
		Namespace:    "module.telephony",
		Category:     tools.CategoryAutomation,
		Profiles:     []tools.Profile{tools.ProfileAutomation, tools.ProfileCoding},
		OutputKind:   tools.OutputText,
		InputJSONSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "to": {"type": "string", "description": "Destination phone number in international or provider format."},
    "objective": {"type": "string", "description": "Short task for the phone conversation, for example book a table, ask opening hours, confirm an order, or leave a message."},
    "system_instruction": {"type": "string", "description": "Optional detailed prompt for how the AI should behave on this call. If omitted, objective is used."},
    "initial_message": {"type": "string", "description": "Optional first phrase the AI should say after the call is answered. Keep it brief and natural."},
    "profile": {"type": "string", "description": "Optional telephony gateway profile, defaults to the configured profile."}
  },
  "required": ["to"],
  "additionalProperties": false
}`),
	}
}

func (t *callTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input callInput
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid telephony_call arguments.", IsError: true, Status: tools.ResultStatusError}, nil
	}
	input.To = normalizePhone(input.To)
	input.Objective = strings.TrimSpace(input.Objective)
	input.InitialMessage = strings.TrimSpace(input.InitialMessage)
	input.Profile = strings.TrimSpace(input.Profile)
	input.SystemInstruction = strings.TrimSpace(input.SystemInstruction)
	if input.To == "" {
		return tools.Result{Content: "Phone number is required.", IsError: true, Status: tools.ResultStatusError}, nil
	}
	if !call.Approved {
		return tools.Result{
			Content: "Approval required",
			Approval: &tools.ApprovalRequest{
				ToolID:      CallToolID,
				Action:      "place_phone_call",
				Path:        input.To,
				Description: approvalDescription(input),
				Params:      input,
			},
		}, nil
	}
	cfg, err := t.config()
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true, Status: tools.ResultStatusError}, nil
	}
	telephonyCfg := cfg.Modules.Telephony
	requestBody := map[string]any{
		"to":                            input.To,
		"profile":                       firstNonEmpty(input.Profile, telephonyCfg.DefaultProfile),
		"objective":                     input.Objective,
		"system_instruction":            input.SystemInstruction,
		"initial_message":               input.InitialMessage,
		"external_key":                  firstNonEmpty(call.ExternalKey, input.To),
		"session_id":                    call.SessionID,
		"origin_client":                 call.Client,
		"origin_external_key":           call.ExternalKey,
		"origin_session_id":             call.SessionID,
		"phone_prompt":                  telephonyCfg.PhonePrompt,
		"assistant_custom_instructions": cfg.Assistant.CustomInstructions,
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return tools.Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(telephonyCfg.GatewayURL, "/")+"/v1/calls", bytes.NewReader(payload))
	if err != nil {
		return tools.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(telephonyCfg.GatewayToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(telephonyCfg.GatewayToken))
	}
	res, err := t.http.Do(req)
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("Telephony call failed: %s", err), IsError: true, Status: tools.ResultStatusError}, nil
	}
	defer res.Body.Close()
	var response callResponse
	_ = json.NewDecoder(res.Body).Decode(&response)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return tools.Result{Content: fmt.Sprintf("Telephony gateway returned HTTP %d.", res.StatusCode), Metadata: response, IsError: true, Status: tools.ResultStatusError}, nil
	}
	return tools.Result{
		Content:  "Phone call started: " + input.To,
		Metadata: response.Call,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

func (t *callTool) config() (setup.Config, error) {
	if t == nil || t.setup == nil {
		return setup.Config{}, fmt.Errorf("Telephony setup is not configured.")
	}
	cfg, err := t.setup.Load()
	if err != nil {
		return setup.Config{}, err
	}
	module := setup.TelephonyModuleFromConfig(cfg.Modules)
	if !module.Enabled {
		return setup.Config{}, fmt.Errorf("Telephony module is disabled.")
	}
	if strings.TrimSpace(cfg.Modules.Telephony.GatewayURL) == "" {
		return setup.Config{}, fmt.Errorf("Telephony gateway URL is not configured.")
	}
	return cfg, nil
}

func approvalDescription(input callInput) string {
	objective := firstNonEmpty(input.Objective, input.InitialMessage)
	if objective == "" {
		return "Place a real outbound phone call to " + input.To + "."
	}
	return "Place a real outbound phone call to " + input.To + ": " + objective
}

func normalizePhone(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
