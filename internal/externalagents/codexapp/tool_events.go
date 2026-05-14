package codexapp

import (
	"encoding/json"
	"fmt"
	"strings"
)

type codexToolEvent struct {
	ID     string
	Name   string
	Input  string
	Output string
	Error  string
}

func codexToolFromItem(raw json.RawMessage, completed bool) (codexToolEvent, bool) {
	var item map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &item) != nil {
		return codexToolEvent{}, false
	}
	id := stringField(item, "id")
	typ := stringField(item, "type")
	if id == "" || typ == "" {
		return codexToolEvent{}, false
	}
	switch typ {
	case "commandExecution":
		output := ""
		if completed {
			output = stringField(item, "aggregatedOutput")
		}
		return codexToolEvent{
			ID:     id,
			Name:   "bash",
			Input:  mustJSONString(map[string]any{"command": stringField(item, "command"), "cwd": stringField(item, "cwd")}),
			Output: output,
			Error:  toolErrorFromStatus(item),
		}, true
	case "fileChange":
		output := ""
		if completed {
			output = fileChangesOutput(item)
		}
		return codexToolEvent{
			ID:     id,
			Name:   "edit",
			Input:  fileChangeInput(item),
			Output: output,
			Error:  toolErrorFromStatus(item),
		}, true
	case "mcpToolCall":
		name := stringField(item, "tool")
		if server := stringField(item, "server"); server != "" {
			name = server + "." + name
		}
		return codexToolEvent{ID: id, Name: defaultString(name, "mcp_tool"), Input: rawJSONString(item["arguments"]), Output: rawJSONString(item["result"]), Error: toolErrorFromStatus(item)}, true
	case "dynamicToolCall":
		return codexToolEvent{ID: id, Name: defaultString(stringField(item, "tool"), "dynamic_tool"), Input: rawJSONString(item["arguments"]), Output: rawJSONString(item["contentItems"]), Error: toolErrorFromStatus(item)}, true
	case "collabAgentToolCall":
		return codexToolEvent{ID: id, Name: defaultString(stringField(item, "tool"), "agent_task"), Input: mustJSONString(map[string]any{
			"prompt":           item["prompt"],
			"model":            item["model"],
			"reasoning_effort": item["reasoningEffort"],
		}), Output: rawJSONString(item["agentsStates"]), Error: toolErrorFromStatus(item)}, true
	case "webSearch":
		return codexToolEvent{ID: id, Name: "web_search", Input: mustJSONString(map[string]any{"query": stringField(item, "query")}), Output: rawJSONString(item["action"])}, true
	case "imageView":
		return codexToolEvent{ID: id, Name: "view_image", Input: mustJSONString(map[string]any{"path": stringField(item, "path")})}, true
	case "imageGeneration":
		return codexToolEvent{ID: id, Name: "image_generation", Input: mustJSONString(map[string]any{"saved_path": item["savedPath"]}), Output: stringField(item, "result"), Error: toolErrorFromStatus(item)}, true
	default:
		return codexToolEvent{}, false
	}
}

func fileChangeInput(item map[string]any) string {
	paths := make([]string, 0)
	if changes, ok := item["changes"].([]any); ok {
		for _, change := range changes {
			changeMap, ok := change.(map[string]any)
			if !ok {
				continue
			}
			if path := stringField(changeMap, "path"); path != "" {
				paths = append(paths, path)
			}
		}
	}
	filePath := ""
	if len(paths) > 0 {
		filePath = paths[0]
	}
	return mustJSONString(map[string]any{"file_path": filePath, "paths": paths})
}

func fileChangesOutput(item map[string]any) string {
	changes, ok := item["changes"].([]any)
	if !ok {
		return ""
	}
	var out strings.Builder
	for _, change := range changes {
		changeMap, ok := change.(map[string]any)
		if !ok {
			continue
		}
		path := stringField(changeMap, "path")
		if path == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(path)
		if diff := stringField(changeMap, "diff"); strings.TrimSpace(diff) != "" {
			out.WriteByte('\n')
			out.WriteString(diff)
		}
	}
	return out.String()
}

func toolErrorFromStatus(item map[string]any) string {
	status := strings.ToLower(strings.TrimSpace(stringField(item, "status")))
	if status == "failed" || status == "declined" {
		if errText := rawJSONString(item["error"]); errText != "" {
			return errText
		}
		return status
	}
	success, ok := item["success"].(bool)
	if ok && !success {
		return "failed"
	}
	return ""
}

func formatFileChanges(changes []FileUpdateChange) string {
	var out strings.Builder
	for _, change := range changes {
		path := strings.TrimSpace(change.Path)
		if path == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(path)
		if diff := strings.TrimSpace(change.Diff); diff != "" {
			out.WriteByte('\n')
			out.WriteString(diff)
		}
	}
	return out.String()
}

func stringField(item map[string]any, name string) string {
	value, ok := item[name]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func rawJSONString(value any) string {
	if value == nil {
		return ""
	}
	return mustJSONString(value)
}

func mustJSONString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
