package codexapp

import "encoding/json"

type ClientInfo struct {
	Name    string  `json:"name"`
	Title   *string `json:"title"`
	Version string  `json:"version"`
}

type InitializeCapabilities struct {
	ExperimentalAPI           bool     `json:"experimentalApi"`
	OptOutNotificationMethods []string `json:"optOutNotificationMethods,omitempty"`
}

type InitializeParams struct {
	ClientInfo   ClientInfo              `json:"clientInfo"`
	Capabilities *InitializeCapabilities `json:"capabilities"`
}

type InitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type ThreadStartParams struct {
	Model                 string         `json:"model,omitempty"`
	ModelProvider         string         `json:"modelProvider,omitempty"`
	CWD                   string         `json:"cwd,omitempty"`
	ApprovalPolicy        string         `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     string         `json:"approvalsReviewer,omitempty"`
	Sandbox               string         `json:"sandbox,omitempty"`
	BaseInstructions      string         `json:"baseInstructions,omitempty"`
	DeveloperInstructions string         `json:"developerInstructions,omitempty"`
	Ephemeral             *bool          `json:"ephemeral,omitempty"`
	Config                map[string]any `json:"config,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID              string         `json:"threadId"`
	Model                 string         `json:"model,omitempty"`
	ModelProvider         string         `json:"modelProvider,omitempty"`
	CWD                   string         `json:"cwd,omitempty"`
	ApprovalPolicy        string         `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     string         `json:"approvalsReviewer,omitempty"`
	Sandbox               string         `json:"sandbox,omitempty"`
	BaseInstructions      string         `json:"baseInstructions,omitempty"`
	DeveloperInstructions string         `json:"developerInstructions,omitempty"`
	Config                map[string]any `json:"config,omitempty"`
}

type ThreadStartResponse struct {
	Thread          Thread  `json:"thread"`
	Model           string  `json:"model"`
	ModelProvider   string  `json:"modelProvider"`
	ServiceTier     *string `json:"serviceTier"`
	CWD             string  `json:"cwd"`
	ApprovalPolicy  string  `json:"approvalPolicy"`
	Sandbox         any     `json:"sandbox"`
	ReasoningEffort *string `json:"reasoningEffort"`
}

type ThreadResumeResponse = ThreadStartResponse

type Thread struct {
	ID            string  `json:"id"`
	SessionID     string  `json:"sessionId"`
	ForkedFromID  *string `json:"forkedFromId"`
	Preview       string  `json:"preview"`
	Ephemeral     bool    `json:"ephemeral"`
	ModelProvider string  `json:"modelProvider"`
	CreatedAt     int64   `json:"createdAt"`
	UpdatedAt     int64   `json:"updatedAt"`
	Status        any     `json:"status"`
	Path          *string `json:"path"`
	CWD           string  `json:"cwd"`
	CLIVersion    string  `json:"cliVersion"`
	Name          *string `json:"name"`
}

type TurnStartParams struct {
	ThreadID          string      `json:"threadId"`
	Input             []UserInput `json:"input"`
	CWD               string      `json:"cwd,omitempty"`
	ApprovalPolicy    string      `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer string      `json:"approvalsReviewer,omitempty"`
	Model             string      `json:"model,omitempty"`
	Effort            string      `json:"effort,omitempty"`
	Summary           string      `json:"summary,omitempty"`
	OutputSchema      any         `json:"outputSchema,omitempty"`
}

type TurnStartResponse struct {
	Turn Turn `json:"turn"`
}

type TurnSteerParams struct {
	ThreadID       string      `json:"threadId"`
	ExpectedTurnID string      `json:"expectedTurnId"`
	Input          []UserInput `json:"input"`
}

type TurnSteerResponse struct{}

type UserInput struct {
	Type         string        `json:"type"`
	Text         string        `json:"text,omitempty"`
	TextElements []TextElement `json:"text_elements,omitempty"`
	URL          string        `json:"url,omitempty"`
	Path         string        `json:"path,omitempty"`
	Name         string        `json:"name,omitempty"`
}

type TextElement struct{}

func TextInput(text string) UserInput {
	return UserInput{Type: "text", Text: text, TextElements: []TextElement{}}
}

type Turn struct {
	ID        string `json:"id"`
	ThreadID  string `json:"threadId,omitempty"`
	Status    any    `json:"status"`
	StartedAt int64  `json:"startedAt,omitempty"`
}

type Notification struct {
	Method string
	Params any
	Raw    []byte
}

type ItemNotification struct {
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
	Item     json.RawMessage `json:"item"`
}

type AgentMessageDelta struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type ReasoningTextDelta struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type ToolOutputDelta struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type FileChangePatchUpdated struct {
	ThreadID string             `json:"threadId"`
	TurnID   string             `json:"turnId"`
	ItemID   string             `json:"itemId"`
	Changes  []FileUpdateChange `json:"changes"`
}

type FileUpdateChange struct {
	Path string          `json:"path"`
	Diff string          `json:"diff"`
	Kind json.RawMessage `json:"kind"`
}

type TurnCompleted struct {
	ThreadID string `json:"threadId"`
	Turn     Turn   `json:"turn"`
}
