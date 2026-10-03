package readmodel

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func ToSurfaceMessage(message transcript.Message) surfacemessage.Message {
	out := surfacemessage.Message{
		ID:               message.ID,
		Role:             surfaceRole(message.Role),
		SessionID:        message.SessionID,
		RunID:            message.RunID,
		Model:            message.Model,
		Provider:         message.Provider,
		CreatedAt:        surfaceNowUnix(message.CreatedAt),
		UpdatedAt:        surfaceNowUnix(message.UpdatedAt),
		IsSummaryMessage: message.Origin == transcript.OriginEngine,
	}
	for _, part := range message.Parts {
		switch part.Kind {
		case transcript.MessagePartKindText:
			if part.Text != nil {
				out.Parts = append(out.Parts, surfacemessage.TextContent{Text: part.Text.Text})
			}
		case transcript.MessagePartKindImage:
			if part.Image != nil {
				out.Parts = append(out.Parts, surfacemessage.ImageContent{Name: imageName(*part.Image), MIMEType: part.Image.MIMEType})
			}
		case transcript.MessagePartKindReasoning:
			if part.Reasoning != nil {
				out.Parts = append(out.Parts, surfacemessage.ReasoningContent{
					Thinking:   part.Reasoning.Text,
					Signature:  part.Reasoning.Signature,
					StartedAt:  surfaceNowUnix(message.CreatedAt),
					FinishedAt: surfaceNowUnix(message.UpdatedAt),
				})
			}
		case transcript.MessagePartKindToolCall:
			if part.ToolCall != nil {
				if part.ToolCall.Name == todo.ToolName {
					continue
				}
				out.Parts = append(out.Parts, surfacemessage.ToolCall{
					ID:       part.ToolCall.ID,
					Name:     part.ToolCall.Name,
					Input:    part.ToolCall.Input,
					Finished: part.ToolCall.Finished,
				})
			}
		case transcript.MessagePartKindToolResult:
			if part.ToolResult != nil {
				if part.ToolResult.Name == todo.ToolName {
					continue
				}
				out.Parts = append(out.Parts, surfacemessage.ToolResult{
					ToolCallID: part.ToolResult.ToolCallID,
					Name:       part.ToolResult.Name,
					Content:    part.ToolResult.Content,
					MIMEType:   part.ToolResult.MIMEType,
					Metadata:   toJSONString(part.ToolResult.Metadata),
					Status:     part.ToolResult.Status,
				})
			}
		case transcript.MessagePartKindFinish:
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
	if compaction := message.Compaction; compaction != nil {
		out.Boundary = &surfacemessage.ContextBoundary{Summary: compaction.Summary, Cleared: compaction.Cleared, TokensBefore: compaction.TokensBefore, TokensAfter: compaction.TokensAfter}
	}
	return out
}

func imageName(image transcript.ImagePart) string {
	if name := strings.TrimSpace(image.Name); name != "" {
		return name
	}
	if path := strings.TrimSpace(image.StoragePath); path != "" {
		return filepath.Base(path)
	}
	return "image"
}

func shouldKeepSurfaceMessage(message surfacemessage.Message) bool {
	if strings.TrimSpace(message.Content().Text) != "" {
		return true
	}
	if strings.TrimSpace(message.ReasoningContent().Thinking) != "" {
		return true
	}
	for _, part := range message.Parts {
		switch part.(type) {
		case surfacemessage.ToolCall, surfacemessage.ToolResult, surfacemessage.Finish:
			return true
		}
	}
	return message.Role == surfacemessage.User
}

func permissionRequest(approval core.Approval) surfacepermission.PermissionRequest {
	return toSurfacePermissionRequest(core.PermissionRequest{
		ID:          approval.ID,
		SessionID:   approval.SessionID,
		ToolCallID:  approval.ToolCallRef,
		ToolName:    approval.ToolName,
		Description: approval.Description,
		Action:      approval.Action,
		Params:      approval.Params,
		Path:        approval.Path,
		Suggestion:  approval.Suggestion,
	})
}

func toSurfacePermissionRequest(request core.PermissionRequest) surfacepermission.PermissionRequest {
	return surfacepermission.PermissionRequest{
		ID:          request.ID,
		SessionID:   request.SessionID,
		AgentName:   request.AgentName,
		ToolCallID:  request.ToolCallID,
		ToolName:    request.ToolName,
		Description: request.Description,
		Action:      request.Action,
		Params:      decodePermissionParams(request.ToolName, request.Params),
		Path:        request.Path,
		Suggestion:  request.Suggestion,
	}
}

func permissionNotification(notification core.PermissionNotification) surfacepermission.PermissionNotification {
	return surfacepermission.PermissionNotification{
		ToolCallID: notification.ToolCallID,
		Granted:    notification.Granted,
		Denied:     notification.Denied,
	}
}

func surfaceRole(role transcript.MessageRole) surfacemessage.MessageRole {
	switch role {
	case transcript.MessageRoleAssistant:
		return surfacemessage.Assistant
	case transcript.MessageRoleSystem:
		return surfacemessage.System
	case transcript.MessageRoleTool:
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
	case "write", "edit", "multiedit":
		var change tools.FileChange
		if err := json.Unmarshal(raw, &change); err == nil {
			return change
		}
	case "skill_manage":
		var params tools.SkillManagePermissionsParams
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
