package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (w *Worker) renderToolCallUpdates(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState) error {
	if state.toolCalls == nil {
		state.toolCalls = map[string]sentToolCallStatus{}
	}
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) || message.Role != core.MessageRoleAssistant {
			continue
		}
		for _, part := range message.Parts {
			if part.ToolCall == nil || strings.TrimSpace(part.ToolCall.ID) == "" || isHiddenTelegramToolStatusName(part.ToolCall.Name) {
				continue
			}
			call := *part.ToolCall
			status := state.toolCalls[call.ID]
			if status.done {
				continue
			}
			text := renderTelegramToolCallStatus(call, false, false, "")
			if status.text == text {
				continue
			}
			messageID, err := w.editOrSendMessage(ctx, target, status.messageID, text, nil)
			if err != nil {
				return err
			}
			state.toolCalls[call.ID] = sentToolCallStatus{messageID: messageID, text: text, name: call.Name, input: call.Input}
		}
	}
	return nil
}

func (w *Worker) renderToolResultUpdates(ctx context.Context, target chatTarget, messages []core.Message, runID string, state *runDeliveryState) error {
	if state.toolCalls == nil {
		state.toolCalls = map[string]sentToolCallStatus{}
	}
	for _, message := range messages {
		if strings.TrimSpace(message.RunID) != strings.TrimSpace(runID) || message.Role != core.MessageRoleTool {
			continue
		}
		for _, part := range message.Parts {
			if part.ToolResult == nil || strings.TrimSpace(part.ToolResult.ToolCallID) == "" || isHiddenTelegramToolStatusName(part.ToolResult.Name) {
				continue
			}
			result := *part.ToolResult
			status := state.toolCalls[result.ToolCallID]
			if status.done {
				continue
			}
			call := core.ToolCallPart{ID: result.ToolCallID, Name: firstNonEmpty(status.name, result.Name), Input: status.input, Finished: true}
			text := renderTelegramToolCallStatus(call, true, result.IsError || strings.EqualFold(result.Status, "error"), result.Content)
			messageID, err := w.editOrSendMessage(ctx, target, status.messageID, text, nil)
			if err != nil {
				return err
			}
			status.messageID = messageID
			status.text = text
			status.name = call.Name
			status.input = call.Input
			status.done = true
			state.toolCalls[result.ToolCallID] = status
		}
	}
	return nil
}

func renderTelegramToolCallStatus(call core.ToolCallPart, done bool, failed bool, resultText string) string {
	action, detail := telegramToolAction(call)
	if done {
		if failed {
			return clipTelegramText(strings.TrimSpace(action + " failed" + telegramToolDetailSuffix(detail) + telegramToolFailureSuffix(resultText)))
		}
		return clipTelegramText(strings.TrimSpace(action + " completed" + telegramToolDetailSuffix(detail)))
	}
	return clipTelegramText(strings.TrimSpace(action + telegramToolDetailSuffix(detail)))
}

func telegramToolAction(call core.ToolCallPart) (string, string) {
	params := decodeTelegramToolParams(call.Input)
	name := strings.ToLower(strings.TrimSpace(call.Name))
	switch name {
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
	return "Using " + telegramPrettyToolName(call.Name), firstNonEmpty(telegramParam(params, "query"), telegramParam(params, "url"), telegramParam(params, "path"), telegramParam(params, "file_path"), telegramParam(params, "action"), telegramParam(params, "name"), telegramParam(params, "id"), telegramParam(params, "text"))
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

func telegramToolFailureSuffix(resultText string) string {
	resultText = compactTelegramToolText(resultText)
	if resultText == "" {
		return ""
	}
	return "\n" + resultText
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

func isPlanToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "plan_get", "plan_set_goal", "plan_add_item", "plan_update_item", "plan_clear":
		return true
	default:
		return false
	}
}

func isHiddenTelegramToolStatusName(name string) bool {
	return isPlanToolName(name) || isTextToSpeechToolName(name) || isWebToolName(name)
}

func isWebToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "web_search", "web_fetch", "web_research", "web_research_ask", "web_research_status":
		return true
	default:
		return false
	}
}
