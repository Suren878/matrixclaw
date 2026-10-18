package openaicompat

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// openAIError is an error body or stream chunk; OpenRouter's code is the HTTP
// status, OpenAI's a name such as "insufficient_quota".
type openAIError struct {
	Code    json.RawMessage `json:"code"`
	Type    string          `json:"type"`
	Message string          `json:"message"`
}

// status is the HTTP status a numeric code carries, or 0.
func (e openAIError) status() int {
	var status int
	_ = json.Unmarshal(e.Code, &status)
	return status
}

// name is a named code, else the error type.
func (e openAIError) name() string {
	var name string
	_ = json.Unmarshal(e.Code, &name)
	return textutil.FirstNonEmpty(name, e.Type)
}

// parseOpenAIError reads an error body; message falls back to the body itself.
func parseOpenAIError(body []byte) openAIError {
	var envelope struct {
		Error openAIError `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && strings.TrimSpace(envelope.Error.Message) != "" {
		envelope.Error.Message = strings.TrimSpace(envelope.Error.Message)
		return envelope.Error
	}
	return openAIError{Message: strings.TrimSpace(string(body))}
}

func responseError(res *http.Response, body []byte) *providers.APIError {
	parsed := parseOpenAIError(body)
	return providers.NewAPIError("openaicompat", res.StatusCode, parsed.name(), parsed.Message, res.Header)
}
