package viewmodel

import (
	"encoding/json"
	"strings"
	"time"

	surfacehistory "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/history"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func ToSurfaceMessage(message core.Message) surfacemessage.Message {
	out := surfacemessage.Message{
		ID:        message.ID,
		Role:      surfaceRole(message.Role),
		SessionID: message.SessionID,
		Model:     message.Model,
		Provider:  message.Provider,
		CreatedAt: surfaceNowUnix(message.CreatedAt),
		UpdatedAt: surfaceNowUnix(message.UpdatedAt),
	}
	for _, part := range message.Parts {
		switch part.Kind {
		case core.MessagePartKindText:
			if part.Text != nil {
				out.Parts = append(out.Parts, surfacemessage.TextContent{Text: part.Text.Text})
			}
		case core.MessagePartKindReasoning:
			if part.Reasoning != nil {
				out.Parts = append(out.Parts, surfacemessage.ReasoningContent{
					Thinking:         part.Reasoning.Text,
					Signature:        part.Reasoning.Signature,
					ThoughtSignature: part.Reasoning.ThoughtSignature,
					ToolID:           part.Reasoning.ToolID,
					ResponsesData:    append(json.RawMessage(nil), part.Reasoning.ResponsesData...),
					StartedAt:        surfaceNowUnix(message.CreatedAt),
					FinishedAt:       surfaceNowUnix(message.UpdatedAt),
				})
			}
		case core.MessagePartKindToolCall:
			if part.ToolCall != nil {
				out.Parts = append(out.Parts, surfacemessage.ToolCall{
					ID:       part.ToolCall.ID,
					Name:     part.ToolCall.Name,
					Input:    part.ToolCall.Input,
					Finished: part.ToolCall.Finished,
				})
			}
		case core.MessagePartKindToolResult:
			if part.ToolResult != nil {
				out.Parts = append(out.Parts, surfacemessage.ToolResult{
					ToolCallID: part.ToolResult.ToolCallID,
					Name:       part.ToolResult.Name,
					Content:    part.ToolResult.Content,
					MIMEType:   part.ToolResult.MIMEType,
					Metadata:   toJSONString(part.ToolResult.Metadata),
					Status:     part.ToolResult.Status,
					IsError:    part.ToolResult.IsError,
				})
			}
		case core.MessagePartKindFinish:
			if part.Finish != nil {
				out.Parts = append(out.Parts, surfacemessage.Finish{
					Reason:  surfaceFinishReason(part.Finish.Reason),
					Time:    surfaceNowUnix(message.UpdatedAt),
					Message: part.Finish.Message,
					Details: toJSONString(part.Finish.Details),
				})
			}
		}
	}
	if strings.TrimSpace(out.Content().Text) == "" && strings.TrimSpace(message.Content) != "" {
		out.Parts = append([]surfacemessage.ContentPart{surfacemessage.TextContent{Text: message.Content}}, out.Parts...)
	}
	return out
}

func ToSurfaceMessages(messages []core.Message) []surfacemessage.Message {
	out := make([]surfacemessage.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, ToSurfaceMessage(message))
	}
	return out
}

func ToSurfacePermissionRequest(request core.PermissionRequest) surfacepermission.PermissionRequest {
	return surfacepermission.PermissionRequest{
		ID:          request.ID,
		SessionID:   request.SessionID,
		ToolCallID:  request.ToolCallID,
		ToolName:    request.ToolName,
		Description: request.Description,
		Action:      request.Action,
		Params:      decodePermissionParams(request.ToolName, request.Params),
		Path:        request.Path,
	}
}

func ToSurfacePermissionNotification(notification core.PermissionNotification) surfacepermission.PermissionNotification {
	return surfacepermission.PermissionNotification{
		ToolCallID: notification.ToolCallID,
		Granted:    notification.Granted,
		Denied:     notification.Denied,
	}
}

func ToSurfaceFile(file core.FileSnapshot) surfacehistory.File {
	return surfacehistory.File{
		ID:        file.ID,
		SessionID: file.SessionID,
		Path:      file.Path,
		Content:   file.Content,
		Version:   int64(file.Version),
		CreatedAt: file.CreatedAt.Unix(),
		UpdatedAt: file.UpdatedAt.Unix(),
	}
}

func ToSurfaceFiles(files []core.FileSnapshot) []surfacehistory.File {
	out := make([]surfacehistory.File, 0, len(files))
	for _, file := range files {
		out = append(out, ToSurfaceFile(file))
	}
	return out
}

func surfaceRole(role core.MessageRole) surfacemessage.MessageRole {
	switch role {
	case core.MessageRoleAssistant:
		return surfacemessage.Assistant
	case core.MessageRoleSystem:
		return surfacemessage.System
	case core.MessageRoleTool:
		return surfacemessage.Tool
	default:
		return surfacemessage.User
	}
}

func surfaceFinishReason(reason string) surfacemessage.FinishReason {
	switch reason {
	case string(surfacemessage.FinishReasonEndTurn):
		return surfacemessage.FinishReasonEndTurn
	case string(surfacemessage.FinishReasonMaxTokens):
		return surfacemessage.FinishReasonMaxTokens
	case string(surfacemessage.FinishReasonToolUse):
		return surfacemessage.FinishReasonToolUse
	case string(surfacemessage.FinishReasonCanceled):
		return surfacemessage.FinishReasonCanceled
	case string(surfacemessage.FinishReasonPermissionDenied):
		return surfacemessage.FinishReasonPermissionDenied
	case string(surfacemessage.FinishReasonError):
		return surfacemessage.FinishReasonError
	default:
		return surfacemessage.FinishReasonUnknown
	}
}

func surfaceNowUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func toJSONString(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}
	return string(value)
}

func decodePermissionParams(toolName string, raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	switch toolName {
	case "bash":
		var params tools.BashPermissionsParams
		if err := json.Unmarshal(raw, &params); err == nil {
			return params
		}
	case "write":
		var params tools.WritePermissionsParams
		if err := json.Unmarshal(raw, &params); err == nil {
			return params
		}
	case "edit":
		var params tools.EditPermissionsParams
		if err := json.Unmarshal(raw, &params); err == nil {
			return params
		}
	case "multiedit":
		var params tools.MultiEditPermissionsParams
		if err := json.Unmarshal(raw, &params); err == nil {
			return params
		}
	}

	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err == nil {
		return generic
	}
	return string(raw)
}
