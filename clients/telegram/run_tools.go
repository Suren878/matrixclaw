package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func telegramToolAction(call transcript.ToolCallPart) (string, string) {
	params := decodeTelegramToolParams(call.Input)
	name := strings.ToLower(strings.TrimSpace(call.Name))
	switch name {
	case "agent":
		action := "Subagent is working"
		if telegramParam(params, "background") == "true" {
			action = "Starting subagent"
		}
		return action, firstNonEmpty(telegramParam(params, "description"), telegramParam(params, "prompt"))
	case "web_search":
		return "Searching web", telegramParam(params, "query")
	case "web_fetch":
		return "Fetching page", telegramParam(params, "url")
	case "web_research":
		return "Researching web", firstNonEmpty(telegramParam(params, "query"), telegramParam(params, "task"), telegramParam(params, "urls"))
	case "web_research_ask":
		return "Checking research", telegramParam(params, "question")
	case "web_research_status":
		return "Checking research", telegramParam(params, "research_id")
	case "reverse_geocode_osm":
		return "Checking address", telegramCoordinatesDetail(params)
	case "nearby_places_osm":
		return "Checking nearby places", firstNonEmpty(telegramCoordinatesDetail(params), telegramParam(params, "radius_m"))
	case "session_search":
		return "Searching sessions", telegramParam(params, "query")
	case "skill_search":
		return "Searching skills", telegramParam(params, "query")
	case "skill_view":
		return "Viewing skill", telegramParam(params, "id")
	case "skill_use":
		return "Loading skill", telegramParam(params, "id")
	case "memory":
		return "Using memory", firstNonEmpty(telegramParam(params, "action"), telegramParam(params, "query"), telegramParam(params, "content"))
	}
	if strings.HasPrefix(name, "mcp_browser_") {
		return telegramBrowserToolAction(name), firstNonEmpty(telegramParam(params, "url"), telegramParam(params, "text"), telegramParam(params, "selector"), telegramParam(params, "element"), telegramParam(params, "query"), telegramParam(params, "ref"))
	}
	return "Using " + telegramPrettyToolName(call.Name), firstNonEmpty(telegramParam(params, "query"), telegramParam(params, "url"), telegramParam(params, "path"), telegramParam(params, "file_path"), telegramParam(params, "command"), telegramParam(params, "action"), telegramParam(params, "name"), telegramParam(params, "id"), telegramParam(params, "text"))
}

func telegramCoordinatesDetail(params map[string]any) string {
	lat := telegramParam(params, "latitude")
	lon := telegramParam(params, "longitude")
	if lat == "" || lon == "" {
		return ""
	}
	return lat + ", " + lon
}

func decodeTelegramToolParams(input string) map[string]any {
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return nil
	}
	return params
}

func telegramParam(params map[string]any, key string) string {
	if len(params) == 0 {
		return ""
	}
	value, ok := params[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return compactTelegramToolText(typed)
	case []any:
		return compactTelegramToolList(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return compactTelegramToolText(fmt.Sprint(value))
	}
}

func compactTelegramToolList(values []any) string {
	if len(values) == 0 {
		return ""
	}
	first := compactTelegramToolText(fmt.Sprint(values[0]))
	if first == "" || len(values) == 1 {
		return first
	}
	return first + fmt.Sprintf(" (+%d)", len(values)-1)
}

func compactTelegramToolText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= 180 {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:179])) + "…"
}

func telegramToolDetailSuffix(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	return ": " + detail
}

func telegramBrowserToolAction(name string) string {
	suffix := strings.TrimPrefix(name, "mcp_browser_")
	switch suffix {
	case "navigate", "goto", "open":
		return "Opening page"
	case "click":
		return "Clicking in browser"
	case "type", "fill":
		return "Typing in browser"
	case "screenshot":
		return "Taking browser screenshot"
	case "wait":
		return "Waiting in browser"
	default:
		return "Using browser"
	}
}

func telegramPrettyToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool"
	}
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	return strings.Join(strings.Fields(name), " ")
}
