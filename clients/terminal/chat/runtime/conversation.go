package runtime

import (
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	surfacechat "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/chat"
	surfacecommon "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/common"
	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	surfacemodel "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/model"
	surfacepermission "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/permission"
	surfacestyles "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/styles"
	"github.com/Suren878/matrixclaw/internal/core"
)

func buildChatModel(sty *surfacestyles.Styles, snapshot viewmodel.Snapshot) *surfacemodel.Chat {
	styles := ensureConversationStyles(sty)
	chatModel := surfacemodel.NewChat(&surfacecommon.Common{Styles: styles})
	chatModel.SetMessages(buildChatItems(styles, snapshot)...)
	return chatModel
}

func ensureConversationStyles(sty *surfacestyles.Styles) *surfacestyles.Styles {
	if sty != nil {
		return sty
	}
	defaultStyles := surfacestyles.DefaultStyles()
	return &defaultStyles
}

func buildChatItems(sty *surfacestyles.Styles, snapshot viewmodel.Snapshot) []surfacechat.MessageItem {
	styles := ensureConversationStyles(sty)
	messages := append([]surfacemessage.Message(nil), snapshot.Messages...)
	toolUpdates := indexToolUpdates(snapshot.ToolUpdates)
	toolResults := surfacechat.BuildToolResultMap(messages)
	items := buildConversationItems(styles, messages, toolResults, toolUpdates)
	applyToolStatuses(items, pendingApprovalToolCalls(snapshot.Approvals), indexApprovalNotifications(snapshot.ApprovalNotifications), toolUpdates)
	return items
}

func indexToolUpdates(updates []core.ToolUpdate) map[string]core.ToolUpdate {
	index := make(map[string]core.ToolUpdate, len(updates))
	for _, update := range updates {
		if update.ToolCallID == "" {
			continue
		}
		index[update.ToolCallID] = update
	}
	return index
}

func pendingApprovalToolCalls(approvals []surfacepermission.PermissionRequest) map[string]struct{} {
	pending := make(map[string]struct{}, len(approvals))
	for _, approval := range approvals {
		if approval.ToolCallID == "" {
			continue
		}
		pending[approval.ToolCallID] = struct{}{}
	}
	return pending
}

func indexApprovalNotifications(notifications []surfacepermission.PermissionNotification) map[string]surfacepermission.PermissionNotification {
	index := make(map[string]surfacepermission.PermissionNotification, len(notifications))
	for _, notification := range notifications {
		if notification.ToolCallID == "" {
			continue
		}
		index[notification.ToolCallID] = notification
	}
	return index
}

func applyToolStatuses(items []surfacechat.MessageItem, pendingApprovals map[string]struct{}, approvalNotifications map[string]surfacepermission.PermissionNotification, toolUpdates map[string]core.ToolUpdate) {
	for _, item := range items {
		if _, ok := item.(*surfacechat.ReadGroupMessageItem); ok {
			continue
		}
		toolItem, ok := item.(surfacechat.ToolMessageItem)
		if !ok {
			continue
		}
		toolCallID := toolItem.ToolCall().ID
		if _, waiting := pendingApprovals[toolCallID]; waiting {
			toolItem.SetStatus(surfacechat.ToolStatusAwaitingPermission)
			continue
		}
		if update, ok := toolUpdates[toolCallID]; ok {
			switch update.State {
			case core.ToolLifecycleWaitingApproval:
				toolItem.SetStatus(surfacechat.ToolStatusAwaitingPermission)
			case core.ToolLifecycleRequested:
				toolItem.SetStatus(surfacechat.ToolStatusRunning)
			case core.ToolLifecycleFailed:
				toolItem.SetStatus(surfacechat.ToolStatusError)
			case core.ToolLifecycleCompleted:
				toolItem.SetStatus(surfacechat.ToolStatusSuccess)
			}
		}
		if notification, ok := approvalNotifications[toolCallID]; ok {
			switch {
			case notification.Granted:
				toolItem.SetStatus(surfacechat.ToolStatusRunning)
			case notification.Denied:
				toolItem.SetStatus(surfacechat.ToolStatusCanceled)
			}
		}
	}
}

func buildConversationItems(sty *surfacestyles.Styles, messages []surfacemessage.Message, toolResults map[string]surfacemessage.ToolResult, toolUpdates map[string]core.ToolUpdate) []surfacechat.MessageItem {
	items := make([]surfacechat.MessageItem, 0, len(messages))
	var lastUserMessageTime time.Time
	for i := 0; i < len(messages); {
		if grouped, next, ok := buildReadGroupItem(sty, messages, i, toolResults, toolUpdates); ok {
			items = append(items, grouped)
			i = next
			continue
		}
		msg := &messages[i]
		if msg.Role == surfacemessage.User && msg.CreatedAt > 0 {
			lastUserMessageTime = time.Unix(msg.CreatedAt, 0)
		}
		items = append(items, surfacechat.ExtractMessageItems(sty, msg, toolResults)...)
		if msg.Role == surfacemessage.Assistant {
			finish := msg.FinishPart()
			if finish != nil && finish.Reason == surfacemessage.FinishReasonEndTurn {
				items = append(items, surfacechat.NewAssistantInfoItem(sty, msg, lastUserMessageTime))
			}
		}
		i++
	}
	return items
}

func buildReadGroupItem(sty *surfacestyles.Styles, messages []surfacemessage.Message, start int, toolResults map[string]surfacemessage.ToolResult, toolUpdates map[string]core.ToolUpdate) (surfacechat.MessageItem, int, bool) {
	if start < 0 || start >= len(messages) || !isStandaloneReadToolCall(messages[start]) {
		return nil, start, false
	}

	groupedCalls := make([]surfacemessage.ToolCall, 0, 2)
	groupedResults := make([]surfacemessage.ToolResult, 0, 2)
	next := start
	for next < len(messages) {
		message := messages[next]
		if !isStandaloneReadToolCall(message) {
			break
		}

		toolCall := message.ToolCalls()[0]
		result, ok := toolResults[toolCall.ID]
		if !ok || result.IsError || result.Name != "read" {
			break
		}

		groupedCalls = append(groupedCalls, toolCall)
		groupedResults = append(groupedResults, result)
		next++

		if next < len(messages) && isReadToolResultMessage(messages[next], toolCall.ID) {
			next++
		}
	}

	if len(groupedCalls) < 2 {
		return nil, start, false
	}
	item := surfacechat.NewReadGroupMessageItem(sty, messages[start].ID, groupedCalls, groupedResults)
	for _, toolCall := range groupedCalls {
		update, ok := toolUpdates[toolCall.ID]
		if !ok {
			continue
		}
		if update.State == core.ToolLifecycleRequested || update.State == core.ToolLifecycleWaitingApproval {
			item.SetStatus(surfacechat.ToolStatusRunning)
			return item, next, true
		}
	}
	item.SetStatus(surfacechat.ToolStatusSuccess)
	return item, next, true
}

func isStandaloneReadToolCall(message surfacemessage.Message) bool {
	if message.Role != surfacemessage.Assistant {
		return false
	}
	if strings.TrimSpace(message.Content().Text) != "" {
		return false
	}
	if strings.TrimSpace(message.ReasoningContent().Thinking) != "" {
		return false
	}
	toolCalls := message.ToolCalls()
	return len(toolCalls) == 1 && toolCalls[0].Name == "read"
}

func isReadToolResultMessage(message surfacemessage.Message, toolCallID string) bool {
	if message.Role != surfacemessage.Tool {
		return false
	}
	toolResults := message.ToolResults()
	return len(toolResults) == 1 && toolResults[0].ToolCallID == toolCallID && toolResults[0].Name == "read"
}
