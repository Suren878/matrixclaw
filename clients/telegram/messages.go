package telegram

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func (w *Worker) handleTextMessage(ctx context.Context, message *Message) error {
	if message == nil || !w.allowMessage(message) {
		return nil
	}
	if len(message.Photo) > 0 {
		return w.handlePhotoMessage(ctx, message)
	}
	if message.Document != nil {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(message.Document.MIMEType)), "image/") {
			return w.handleDocumentImageMessage(ctx, message)
		}
		return w.handleDocumentMessage(ctx, message)
	}
	if strings.TrimSpace(message.Text) == "" {
		return nil
	}

	target := targetFromMessage(message)
	text := strings.TrimSpace(message.Text)
	if handled, err := w.handlePendingPrompt(ctx, target, text); handled || err != nil {
		return err
	}
	if isDaemonRestartCommand(text) {
		return w.dispatchRestartCommandAndEdit(ctx, target, 0)
	}
	if result, err := w.dispatcher().Handle(ctx, target.externalKey, text); err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Command failed: %v", err))
	} else if result.Handled {
		return w.renderCommandResult(ctx, target, 0, result)
	}
	return w.sendUserMessage(ctx, target, text)
}

func (w *Worker) handlePhotoMessage(ctx context.Context, message *Message) error {
	target := targetFromMessage(message)
	photo := largestPhoto(message.Photo)
	if strings.TrimSpace(photo.FileID) == "" {
		return nil
	}
	if photo.FileSize > maxTelegramImageBytes {
		return w.sendText(ctx, target, fmt.Sprintf("Image is too large: %d bytes", photo.FileSize))
	}
	file, err := w.api.GetFile(ctx, photo.FileID)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Telegram image lookup failed: %v", err))
	}
	content, err := w.api.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Telegram image download failed: %v", err))
	}
	return w.sendImageMessage(ctx, target, strings.TrimSpace(message.Caption), content, filepath.Base(file.FilePath), mimeTypeFromPath(file.FilePath, "image/jpeg"))
}

func (w *Worker) handleDocumentImageMessage(ctx context.Context, message *Message) error {
	target := targetFromMessage(message)
	doc := message.Document
	if doc == nil || strings.TrimSpace(doc.FileID) == "" {
		return nil
	}
	if doc.FileSize > maxTelegramImageBytes {
		return w.sendText(ctx, target, fmt.Sprintf("Image is too large: %d bytes", doc.FileSize))
	}
	file, err := w.api.GetFile(ctx, doc.FileID)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Telegram image lookup failed: %v", err))
	}
	content, err := w.api.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Telegram image download failed: %v", err))
	}
	name := strings.TrimSpace(doc.FileName)
	if name == "" {
		name = filepath.Base(file.FilePath)
	}
	return w.sendImageMessage(ctx, target, strings.TrimSpace(message.Caption), content, name, doc.MIMEType)
}

func (w *Worker) handleDocumentMessage(ctx context.Context, message *Message) error {
	target := targetFromMessage(message)
	doc := message.Document
	if doc == nil || strings.TrimSpace(doc.FileID) == "" {
		return nil
	}
	if doc.FileSize > maxTelegramStorageUploadBytes {
		return w.sendText(ctx, target, fmt.Sprintf("File is too large for storage upload: %d bytes", doc.FileSize))
	}
	file, err := w.api.GetFile(ctx, doc.FileID)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Telegram file lookup failed: %v", err))
	}
	content, err := w.api.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Telegram file download failed: %v", err))
	}
	if len(content) > maxTelegramStorageUploadBytes {
		return w.sendText(ctx, target, fmt.Sprintf("File is too large for storage upload: %d bytes", len(content)))
	}
	name := strings.TrimSpace(doc.FileName)
	if name == "" {
		name = filepath.Base(file.FilePath)
	}
	tempPath := "telegram/" + safeStorageFileName(name)
	entry, err := w.daemon(target.externalKey).SaveTemporaryStorageFile(ctx, tempPath, content, name, []string{"telegram", "temporary"}, doc.MIMEType)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Storage upload failed: %v", err))
	}
	return w.sendText(ctx, target, fmt.Sprintf("Temporary file saved: %s\nUse /modules -> Storage -> Temporary Files to save it permanently or delete it.", entry.Path))
}

func (w *Worker) sendUserMessage(ctx context.Context, target chatTarget, text string) error {
	return w.sendUserMessageParts(ctx, target, text, nil)
}

func (w *Worker) sendImageMessage(ctx context.Context, target chatTarget, caption string, content []byte, name string, mimeType string) error {
	if len(content) > maxTelegramImageBytes {
		return w.sendText(ctx, target, fmt.Sprintf("Image is too large: %d bytes", len(content)))
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "image/jpeg"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "image"
	}
	tempPath := fmt.Sprintf("telegram/images/chat%d-%d-%s", target.chatID, time.Now().UnixNano(), safeStorageFileName(name))
	entry, err := w.daemon(target.externalKey).SaveTemporaryStorageFile(ctx, tempPath, content, name, []string{"telegram", "temporary", "image"}, mimeType)
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Storage upload failed: %v", err))
	}
	text := strings.TrimSpace(caption)
	if text == "" {
		text = "Describe this image."
	}
	parts := []core.MessagePart{
		{Kind: core.MessagePartKindText, Text: &core.TextPart{Text: text}},
		{Kind: core.MessagePartKindImage, Image: &core.ImagePart{
			MIMEType:    entry.MIMEType,
			Name:        entry.Title,
			StoragePath: entry.Path,
			Temporary:   true,
			Size:        entry.Size,
		}},
	}
	return w.sendUserMessageParts(ctx, target, text, parts)
}

func (w *Worker) sendUserMessageParts(ctx context.Context, target chatTarget, text string, parts []core.MessagePart) error {
	daemon := w.daemon(target.externalKey)
	if err := w.api.SendChatAction(ctx, SendChatActionRequest{
		ChatID:          target.chatID,
		MessageThreadID: target.threadID,
		Action:          "typing",
	}); err != nil {
		log.Printf("telegram: typing indicator failed: %v", err)
	}

	result, err := daemon.SendMessageParts(ctx, "", text, parts, w.config.WorkingDir)
	if err != nil {
		if daemonclient.IsAPIStatus(err, http.StatusConflict) {
			if handled, handleErr := w.handleSessionSelectionRequired(ctx, target); handled || handleErr != nil {
				return handleErr
			}
		}
		return w.sendText(ctx, target, fmt.Sprintf("Request failed: %v", err))
	}

	w.startMonitor(ctx, target, result.SessionID, result.Run.ID)
	return nil
}

func largestPhoto(photos []PhotoSize) PhotoSize {
	var best PhotoSize
	bestArea := -1
	for _, photo := range photos {
		area := photo.Width * photo.Height
		if area > bestArea {
			best = photo
			bestArea = area
		}
	}
	return best
}

func mimeTypeFromPath(path string, fallback string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return fallback
	}
}

func (w *Worker) handleSessionSelectionRequired(ctx context.Context, target chatTarget) (bool, error) {
	daemon := w.daemon(target.externalKey)
	sessions, err := daemon.ListSessions(ctx)
	if err != nil {
		return true, w.sendText(ctx, target, fmt.Sprintf("Load sessions failed: %v", err))
	}
	if len(sessions) == 0 {
		session, err := daemon.CreateSession(ctx, "Telegram chat "+time.Now().Format("2006-01-02 15:04"), w.config.WorkingDir)
		if err != nil {
			return true, w.sendText(ctx, target, fmt.Sprintf("Create session failed: %v", err))
		}
		if _, err := daemon.UseSession(ctx, session.ID); err != nil {
			return true, w.sendText(ctx, target, fmt.Sprintf("Bind session failed: %v", err))
		}
		return true, w.sendText(ctx, target, fmt.Sprintf("Created session %s. Send the message again.", session.Title))
	}
	result, err := w.dispatcher().Handle(ctx, target.externalKey, "/sessions")
	if err != nil {
		return true, w.sendText(ctx, target, fmt.Sprintf("Load sessions failed: %v", err))
	}
	return true, w.renderCommandResult(ctx, target, 0, withSessionSelectionPrompt(result))
}

func withSessionSelectionPrompt(result controlplane.Result) controlplane.Result {
	message := "Choose a session or create a new one, then send your message again."
	if result.Picker == nil {
		result.Text = message
		return result
	}
	title := strings.TrimSpace(result.Picker.Title)
	if title == "" {
		title = "Sessions"
	}
	result.Picker.Title = message + "\n\n" + title
	return result
}
