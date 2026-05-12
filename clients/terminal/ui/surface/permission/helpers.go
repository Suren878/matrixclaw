package permission

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

type DownloadPermissionsParams struct {
	URL      string `json:"url"`
	FilePath string `json:"file_path"`
	Timeout  int    `json:"timeout,omitempty"`
}

type FetchPermissionsParams struct {
	URL string `json:"url"`
}

type AgenticFetchPermissionsParams struct {
	URL    string `json:"url,omitempty"`
	Prompt string `json:"prompt"`
}

type ReadPermissionsParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

type LSPermissionsParams struct {
	Path   string   `json:"path,omitempty"`
	Ignore []string `json:"ignore,omitempty"`
}

func DecodeParams[T any](raw any) (T, bool) {
	var zero T
	if raw == nil {
		return zero, false
	}
	if typed, ok := raw.(T); ok {
		return typed, true
	}

	var body []byte
	switch value := raw.(type) {
	case json.RawMessage:
		body = value
	case []byte:
		body = value
	case string:
		body = []byte(value)
	default:
		marshaled, err := json.Marshal(value)
		if err != nil {
			return zero, false
		}
		body = marshaled
	}

	if len(body) == 0 {
		return zero, false
	}

	var typed T
	if err := json.Unmarshal(body, &typed); err != nil {
		return zero, false
	}
	return typed, true
}

func FormatParams(raw any) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case json.RawMessage:
		return strings.TrimSpace(string(value))
	case []byte:
		return strings.TrimSpace(string(value))
	default:
		body, err := json.Marshal(value)
		if err != nil {
			return strings.TrimSpace(fmt.Sprintf("%v", value))
		}
		return strings.TrimSpace(string(body))
	}
}

func CanAllowSessionApproval(request PermissionRequest) bool {
	switch strings.ToLower(strings.TrimSpace(request.ToolName)) {
	case "write":
		params, ok := DecodeParams[tools.WritePermissionsParams](request.Params)
		return ok && params.WithinWorkingDir
	case "edit":
		params, ok := DecodeParams[tools.EditPermissionsParams](request.Params)
		return ok && params.WithinWorkingDir
	case "multiedit", "multi_edit":
		params, ok := DecodeParams[tools.MultiEditPermissionsParams](request.Params)
		return ok && params.WithinWorkingDir
	default:
		return false
	}
}
