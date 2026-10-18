package core

import (
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

func marshalJSONRaw(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func errorText(result tools.Result) string {
	if result.IsError() {
		return strings.TrimSpace(result.Content)
	}
	return ""
}
