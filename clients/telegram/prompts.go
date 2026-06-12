package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func (w *Worker) setPrompt(externalKey string, prompt controlplane.PromptData) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prompts[externalKey] = prompt
}

func (w *Worker) clearPrompt(externalKey string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.prompts, externalKey)
}

func (w *Worker) prompt(externalKey string) (controlplane.PromptData, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	prompt, ok := w.prompts[externalKey]
	return prompt, ok
}

func (w *Worker) handlePendingPrompt(ctx context.Context, target chatTarget, text string) (bool, error) {
	prompt, ok := w.prompt(target.externalKey)
	if !ok {
		return false, nil
	}
	if prompt.Sensitive && target.isChat() {
		_ = w.api.DeleteMessage(ctx, DeleteMessageRequest{
			ChatID:    target.chatID,
			MessageID: target.messageID,
		})
	}
	if isPromptCloseCommand(text) {
		w.clearPrompt(target.externalKey)
		if strings.TrimSpace(prompt.CancelCommand) != "" {
			result, err := w.dispatcher().Handle(ctx, target.externalKey, strings.TrimSpace(prompt.CancelCommand))
			if err != nil {
				return true, w.sendText(ctx, target, fmt.Sprintf("Command failed: %v", err))
			}
			return true, w.renderCommandResult(ctx, target, result)
		}
		return true, w.sendText(ctx, target, "Closed.")
	}
	if strings.HasPrefix(strings.TrimSpace(text), "/") {
		w.clearPrompt(target.externalKey)
		return false, nil
	}
	w.clearPrompt(target.externalKey)
	result, err := w.dispatcher().Handle(ctx, target.externalKey, prompt.SubmitCommandPrefix+strings.TrimSpace(text))
	if err != nil {
		return true, w.sendText(ctx, target, fmt.Sprintf("Command failed: %v", err))
	}
	return true, w.renderCommandResult(ctx, target, result)
}

func isPromptCloseCommand(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return text == "/close" || text == "/cancel"
}
