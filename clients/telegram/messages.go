package telegram

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/commandcatalog"
	"github.com/Suren878/matrixclaw/internal/controlplane"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
	voicemodule "github.com/Suren878/matrixclaw/internal/modules/voice"
)

const maxTelegramAudioBytes int64 = 25 << 20

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
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(message.Document.MIMEType)), "audio/") {
			return w.handleDocumentAudioMessage(ctx, message)
		}
		return w.handleDocumentMessage(ctx, message)
	}
	if message.Voice != nil {
		return w.handleVoiceMessage(ctx, message)
	}
	if message.Audio != nil {
		return w.handleAudioMessage(ctx, message)
	}
	if strings.TrimSpace(message.Text) == "" {
		return nil
	}

	target := targetFromMessage(message)
	text := strings.TrimSpace(message.Text)
	if handled, err := w.handleTextToSpeechCommand(ctx, target, text); handled || err != nil {
		return err
	}
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

func (w *Worker) handleTextToSpeechCommand(ctx context.Context, target chatTarget, text string) (bool, error) {
	command, args, ok := splitTelegramCommand(text)
	if !ok || command != "tts" {
		return false, nil
	}
	if args == "" {
		return true, w.sendText(ctx, target, "Usage: /tts text to speak")
	}
	return true, w.sendSpeech(ctx, target, args)
}

func (w *Worker) handlePhotoMessage(ctx context.Context, message *Message) error {
	target := targetFromMessage(message)
	photo := largestPhoto(message.Photo)
	upload, err := w.downloadTelegramUpload(ctx, target, telegramUploadRequest{
		fileID:               photo.FileID,
		fileSize:             photo.FileSize,
		maxBytes:             maxTelegramImageBytes,
		tooLargeFormat:       "Image is too large: %d bytes",
		lookupFailedFormat:   "Telegram image lookup failed: %v",
		downloadFailedFormat: "Telegram image download failed: %v",
		inferMIMEFromPath:    true,
		fallbackMIME:         "image/jpeg",
	})
	if upload == nil || err != nil {
		return err
	}
	return w.sendImageMessage(ctx, target, strings.TrimSpace(message.Caption), upload.content, upload.name, upload.mimeType)
}

func (w *Worker) handleDocumentImageMessage(ctx context.Context, message *Message) error {
	target := targetFromMessage(message)
	doc := message.Document
	if doc == nil {
		return nil
	}
	upload, err := w.downloadTelegramUpload(ctx, target, telegramUploadRequest{
		fileID:               doc.FileID,
		fileSize:             doc.FileSize,
		fileName:             doc.FileName,
		mimeType:             doc.MIMEType,
		maxBytes:             maxTelegramImageBytes,
		tooLargeFormat:       "Image is too large: %d bytes",
		lookupFailedFormat:   "Telegram image lookup failed: %v",
		downloadFailedFormat: "Telegram image download failed: %v",
	})
	if upload == nil || err != nil {
		return err
	}
	return w.sendImageMessage(ctx, target, strings.TrimSpace(message.Caption), upload.content, upload.name, upload.mimeType)
}

func (w *Worker) handleDocumentMessage(ctx context.Context, message *Message) error {
	target := targetFromMessage(message)
	doc := message.Document
	if doc == nil {
		return nil
	}
	upload, err := w.downloadTelegramUpload(ctx, target, telegramUploadRequest{
		fileID:               doc.FileID,
		fileSize:             doc.FileSize,
		fileName:             doc.FileName,
		mimeType:             doc.MIMEType,
		maxBytes:             maxTelegramStorageUploadBytes,
		tooLargeFormat:       "File is too large for storage upload: %d bytes",
		lookupFailedFormat:   "Telegram file lookup failed: %v",
		downloadFailedFormat: "Telegram file download failed: %v",
	})
	if upload == nil || err != nil {
		return err
	}
	tempPath := fmt.Sprintf("telegram/files/chat%d-%d-%s", target.chatID, time.Now().UnixNano(), safeStorageFileName(upload.name))
	entry, err := w.saveTemporaryTelegramUpload(ctx, target, tempPath, upload, []string{"telegram", "temporary"})
	if err != nil {
		return err
	}
	return w.sendText(ctx, target, fmt.Sprintf("Temporary file saved: %s\nUse /modules -> Storage -> Temporary Files to save it permanently or delete it.", entry.Path))
}

func (w *Worker) handleVoiceMessage(ctx context.Context, message *Message) error {
	if message == nil || message.Voice == nil {
		return nil
	}
	voice := message.Voice
	return w.handleTelegramAudioUpload(ctx, message, telegramUploadRequest{
		fileID:               voice.FileID,
		fileSize:             voice.FileSize,
		maxBytes:             maxTelegramAudioBytes,
		tooLargeFormat:       "Voice message is too large: %d bytes",
		lookupFailedFormat:   "Telegram voice lookup failed: %v",
		downloadFailedFormat: "Telegram voice download failed: %v",
		inferMIMEFromPath:    true,
		fallbackMIME:         firstNonEmpty(voice.MIMEType, "audio/ogg"),
	})
}

func (w *Worker) handleAudioMessage(ctx context.Context, message *Message) error {
	if message == nil || message.Audio == nil {
		return nil
	}
	audio := message.Audio
	return w.handleTelegramAudioUpload(ctx, message, telegramUploadRequest{
		fileID:               audio.FileID,
		fileSize:             audio.FileSize,
		fileName:             audio.FileName,
		mimeType:             audio.MIMEType,
		maxBytes:             maxTelegramAudioBytes,
		tooLargeFormat:       "Audio file is too large: %d bytes",
		lookupFailedFormat:   "Telegram audio lookup failed: %v",
		downloadFailedFormat: "Telegram audio download failed: %v",
		inferMIMEFromPath:    audio.MIMEType == "",
		fallbackMIME:         "audio/mpeg",
	})
}

func (w *Worker) handleDocumentAudioMessage(ctx context.Context, message *Message) error {
	target := targetFromMessage(message)
	doc := message.Document
	if doc == nil {
		return nil
	}
	upload, err := w.downloadTelegramUpload(ctx, target, telegramUploadRequest{
		fileID:               doc.FileID,
		fileSize:             doc.FileSize,
		fileName:             doc.FileName,
		mimeType:             doc.MIMEType,
		maxBytes:             maxTelegramAudioBytes,
		tooLargeFormat:       "Audio file is too large: %d bytes",
		lookupFailedFormat:   "Telegram audio lookup failed: %v",
		downloadFailedFormat: "Telegram audio download failed: %v",
	})
	if upload == nil || err != nil {
		return err
	}
	return w.transcribeAndSendUserMessage(ctx, target, upload)
}

func (w *Worker) handleTelegramAudioUpload(ctx context.Context, message *Message, req telegramUploadRequest) error {
	target := targetFromMessage(message)
	upload, err := w.downloadTelegramUpload(ctx, target, req)
	if upload == nil || err != nil {
		return err
	}
	return w.transcribeAndSendUserMessage(ctx, target, upload)
}

func (w *Worker) transcribeAndSendUserMessage(ctx context.Context, target chatTarget, upload *telegramUpload) error {
	if upload == nil || len(upload.content) == 0 {
		return nil
	}
	if err := w.api.SendChatAction(ctx, SendChatActionRequest{
		ChatID:          target.chatID,
		MessageThreadID: target.threadID,
		Action:          "typing",
	}); err != nil {
		log.Printf("telegram: typing indicator failed: %v", err)
	}
	result, err := w.daemon(target.externalKey).SpeechToText(ctx, voicemodule.NewSpeechToTextRequest(upload.content, upload.name, upload.mimeType))
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Speech to text failed: %v", err))
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return w.sendText(ctx, target, "Speech to text returned empty text.")
	}
	if err := w.sendText(ctx, target, "Transcribed: "+text); err != nil {
		return err
	}
	return w.sendUserMessage(ctx, target, text)
}

func (w *Worker) sendSpeech(ctx context.Context, target chatTarget, text string) error {
	if err := w.api.SendChatAction(ctx, SendChatActionRequest{
		ChatID:          target.chatID,
		MessageThreadID: target.threadID,
		Action:          "upload_voice",
	}); err != nil {
		log.Printf("telegram: upload_voice indicator failed: %v", err)
	}
	response, err := w.daemon(target.externalKey).TextToSpeech(ctx, voicemodule.TextToSpeechRequest{Text: text})
	if err != nil {
		return w.sendText(ctx, target, fmt.Sprintf("Text to speech failed: %v", err))
	}
	if _, err := w.sendGeneratedSpeech(ctx, target, response); err != nil {
		return err
	}
	return nil
}

func (w *Worker) sendGeneratedSpeech(ctx context.Context, target chatTarget, response voicemodule.TextToSpeechResponse) (SentMessage, error) {
	content, err := response.ContentBytes()
	if err != nil {
		return SentMessage{}, w.sendText(ctx, target, fmt.Sprintf("Text to speech returned invalid audio: %v", err))
	}
	if len(content) == 0 {
		return SentMessage{}, w.sendText(ctx, target, "Text to speech returned empty audio.")
	}
	if int64(len(content)) > maxTelegramAudioBytes {
		return SentMessage{}, w.sendText(ctx, target, fmt.Sprintf("Generated audio is too large: %d bytes", len(content)))
	}
	fileName := firstNonEmpty(response.FileName, "matrixclaw-tts.mp3")
	mimeType := firstNonEmpty(response.MIMEType, "audio/mpeg")
	var sent SentMessage
	if useTelegramVoiceUpload(fileName, mimeType) {
		req := SendVoiceRequest{
			ChatID:          target.chatID,
			MessageThreadID: target.threadID,
			Voice:           content,
			FileName:        fileName,
			MIMEType:        mimeType,
		}
		sent, err = w.api.SendVoice(ctx, req)
	} else {
		req := SendAudioRequest{
			ChatID:          target.chatID,
			MessageThreadID: target.threadID,
			Audio:           content,
			FileName:        fileName,
			MIMEType:        mimeType,
		}
		sent, err = w.api.SendAudio(ctx, req)
	}
	if err != nil {
		log.Printf("telegram: send generated speech failed chat=%d thread=%d file=%s mime=%s bytes=%d: %v", target.chatID, target.threadID, fileName, mimeType, len(content), err)
		return SentMessage{}, err
	}
	log.Printf("telegram: sent generated speech chat=%d thread=%d message=%d file=%s mime=%s bytes=%d", target.chatID, target.threadID, sent.MessageID, fileName, mimeType, len(content))
	w.saveGeneratedSpeechToStorage(ctx, target, response, content)
	return sent, nil
}

func useTelegramVoiceUpload(fileName string, mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	name := strings.ToLower(strings.TrimSpace(fileName))
	return strings.Contains(mimeType, "ogg") || strings.Contains(mimeType, "opus") || strings.HasSuffix(name, ".ogg") || strings.HasSuffix(name, ".opus")
}

func (w *Worker) saveGeneratedSpeechToStorage(ctx context.Context, target chatTarget, response voicemodule.TextToSpeechResponse, content []byte) {
	name := safeStorageFileName(firstNonEmpty(response.FileName, "matrixclaw-tts.mp3"))
	if name == "" {
		name = "matrixclaw-tts.mp3"
	}
	mimeType := firstNonEmpty(response.MIMEType, "audio/mpeg")
	storagePath := fmt.Sprintf("telegram/audio/chat%d-%d-%s", target.chatID, time.Now().UnixNano(), name)
	if _, err := w.daemon(target.externalKey).SaveStorageFile(ctx, storagePath, content, name, []string{"telegram", "generated", "audio", "tts"}, mimeType); err != nil {
		log.Printf("telegram: save generated speech to storage failed: %v", err)
	}
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
	entry, err := w.saveTemporaryTelegramUpload(ctx, target, tempPath, &telegramUpload{
		content:  content,
		name:     name,
		mimeType: mimeType,
	}, []string{"telegram", "temporary", "image"})
	if err != nil {
		return err
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

type telegramUploadRequest struct {
	fileID               string
	fileSize             int64
	fileName             string
	mimeType             string
	maxBytes             int64
	tooLargeFormat       string
	lookupFailedFormat   string
	downloadFailedFormat string
	inferMIMEFromPath    bool
	fallbackMIME         string
}

type telegramUpload struct {
	content  []byte
	name     string
	mimeType string
}

type savedTelegramUpload struct {
	Path     string
	Title    string
	MIMEType string
	Size     int64
}

func (w *Worker) downloadTelegramUpload(ctx context.Context, target chatTarget, req telegramUploadRequest) (*telegramUpload, error) {
	fileID := strings.TrimSpace(req.fileID)
	if fileID == "" {
		return nil, nil
	}
	if req.maxBytes > 0 && req.fileSize > req.maxBytes {
		return nil, w.sendText(ctx, target, fmt.Sprintf(req.tooLargeFormat, req.fileSize))
	}
	file, err := w.api.GetFile(ctx, req.fileID)
	if err != nil {
		return nil, w.sendText(ctx, target, fmt.Sprintf(req.lookupFailedFormat, err))
	}
	content, err := w.api.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return nil, w.sendText(ctx, target, fmt.Sprintf(req.downloadFailedFormat, err))
	}
	if req.maxBytes > 0 && int64(len(content)) > req.maxBytes {
		return nil, w.sendText(ctx, target, fmt.Sprintf(req.tooLargeFormat, len(content)))
	}
	name := strings.TrimSpace(req.fileName)
	if name == "" {
		name = filepath.Base(file.FilePath)
	}
	mimeType := req.mimeType
	if req.inferMIMEFromPath {
		mimeType = mimeTypeFromPath(file.FilePath, req.fallbackMIME)
	}
	return &telegramUpload{
		content:  content,
		name:     name,
		mimeType: mimeType,
	}, nil
}

func (w *Worker) saveTemporaryTelegramUpload(ctx context.Context, target chatTarget, tempPath string, upload *telegramUpload, tags []string) (*savedTelegramUpload, error) {
	entry, err := w.daemon(target.externalKey).SaveTemporaryStorageFile(ctx, tempPath, upload.content, upload.name, tags, upload.mimeType)
	if err != nil {
		return nil, w.sendText(ctx, target, fmt.Sprintf("Storage upload failed: %v", err))
	}
	return &savedTelegramUpload{
		Path:     entry.Path,
		Title:    entry.Title,
		MIMEType: entry.MIMEType,
		Size:     entry.Size,
	}, nil
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

func splitTelegramCommand(text string) (string, string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	text = strings.TrimPrefix(text, "/")
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", "", false
	}
	command := parts[0]
	if idx := strings.IndexByte(command, '@'); idx >= 0 {
		command = command[:idx]
	}
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(text[len(parts[0]):])
	}
	return strings.ToLower(strings.TrimSpace(command)), args, true
}

func (w *Worker) handleSessionSelectionRequired(ctx context.Context, target chatTarget) (bool, error) {
	daemon := w.daemon(target.externalKey)
	sessions, err := daemon.ListSessions(ctx)
	if err != nil {
		return true, w.sendText(ctx, target, fmt.Sprintf("Load sessions failed: %v", err))
	}
	if len(sessions) == 0 {
		result, err := w.dispatcher().Handle(ctx, target.externalKey, catalogCommand(commandcatalog.CommandNewSession, ""))
		if err != nil {
			return true, w.sendText(ctx, target, fmt.Sprintf("Create session failed: %v", err))
		}
		return true, w.renderCommandResult(ctx, target, 0, withSessionSelectionPrompt(result))
	}
	result, err := w.dispatcher().Handle(ctx, target.externalKey, catalogCommand(commandcatalog.CommandSessions, ""))
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
