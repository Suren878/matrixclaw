package mcp

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func ResultContent(result *sdk.CallToolResult) string {
	if result == nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content)+1)
	for _, content := range result.Content {
		switch value := content.(type) {
		case *sdk.TextContent:
			if text := strings.TrimSpace(value.Text); text != "" {
				parts = append(parts, text)
			}
		case *sdk.ImageContent:
			parts = append(parts, encodedMediaSummary("image", value.MIMEType, value.Data))
		case *sdk.AudioContent:
			parts = append(parts, encodedMediaSummary("audio", value.MIMEType, value.Data))
		default:
			raw, err := json.Marshal(value)
			if err == nil && len(raw) > 0 && string(raw) != "null" {
				parts = append(parts, string(raw))
			}
		}
	}
	if result.StructuredContent != nil {
		raw, err := json.MarshalIndent(result.StructuredContent, "", "  ")
		if err == nil && len(raw) > 0 && string(raw) != "null" {
			parts = append(parts, string(raw))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func encodedMediaSummary(kind string, mimeType string, data []byte) string {
	if len(data) == 0 {
		return "<" + kind + ">"
	}
	decodedLen := base64.StdEncoding.DecodedLen(len(data))
	if strings.TrimSpace(mimeType) == "" {
		return "<" + kind + " bytes=" + itoa(decodedLen) + ">"
	}
	return "<" + kind + " mime=" + strings.TrimSpace(mimeType) + " bytes=" + itoa(decodedLen) + ">"
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
