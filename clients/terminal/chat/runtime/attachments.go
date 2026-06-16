package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	surfaceeditor "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/editor"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

const maxAttachmentSize = 5 << 20

var errUnsupportedAttachment = errors.New("only text and image attachments are supported")

type sendPayload struct {
	content string
	parts   []core.MessagePart
}

func prepareSendPayload(ctx context.Context, client *daemonclient.Client, prompt string, attachments []surfaceeditor.Attachment) (sendPayload, error) {
	content, err := prepareSendContent(prompt, attachments)
	if err != nil {
		return sendPayload{}, err
	}
	if !hasImageAttachment(attachments) {
		return sendPayload{content: content}, nil
	}
	parts, err := uploadImageMessageParts(ctx, client, content, attachments)
	if err != nil {
		return sendPayload{}, err
	}
	return sendPayload{content: content, parts: parts}, nil
}

func prepareSendContent(prompt string, attachments []surfaceeditor.Attachment) (string, error) {
	if len(attachments) == 0 {
		return strings.TrimSpace(prompt), nil
	}

	for _, attachment := range attachments {
		if !attachment.IsText() && !attachment.IsImage() {
			return "", errUnsupportedAttachment
		}
	}

	return promptWithTextAttachments(strings.TrimSpace(prompt), attachments), nil
}

func uploadImageMessageParts(ctx context.Context, client *daemonclient.Client, content string, attachments []surfaceeditor.Attachment) ([]core.MessagePart, error) {
	if client == nil {
		return nil, fmt.Errorf("terminal runtime is not configured")
	}
	var parts []core.MessagePart
	if strings.TrimSpace(content) != "" {
		parts = append(parts, core.MessagePart{Kind: core.MessagePartKindText, Text: &core.TextPart{Text: content}})
	}
	for _, attachment := range attachments {
		if !attachment.IsImage() {
			continue
		}
		name := imageAttachmentFileName(attachment)
		mimeType := strings.TrimSpace(attachment.MimeType)
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		tempPath := fmt.Sprintf("terminal/images/%d-%s", time.Now().UnixNano(), name)
		entry, err := client.SaveTemporaryStorageFile(ctx, tempPath, attachment.Content, name, []string{"terminal", "temporary", "image"}, mimeType)
		if err != nil {
			return nil, err
		}
		parts = append(parts, core.MessagePart{Kind: core.MessagePartKindImage, Image: &core.ImagePart{
			MIMEType:    entry.MIMEType,
			Name:        entry.Title,
			StoragePath: entry.Path,
			Temporary:   true,
			Size:        entry.Size,
		}})
	}
	return parts, nil
}

func hasImageAttachment(attachments []surfaceeditor.Attachment) bool {
	for _, attachment := range attachments {
		if attachment.IsImage() {
			return true
		}
	}
	return false
}

func promptWithTextAttachments(prompt string, attachments []surfaceeditor.Attachment) string {
	var sb strings.Builder
	sb.WriteString(prompt)
	addedAttachments := false
	for _, attachment := range attachments {
		if !attachment.IsText() {
			continue
		}
		if !addedAttachments {
			sb.WriteString("\n<system_info>The files below have been attached by the user, consider them in your response</system_info>\n")
			addedAttachments = true
		}
		if strings.TrimSpace(attachment.FilePath) != "" {
			_, _ = fmt.Fprintf(&sb, "<file path='%s'>\n", attachment.FilePath)
		} else {
			sb.WriteString("<file>\n")
		}
		sb.WriteString("\n")
		sb.Write(attachment.Content)
		sb.WriteString("\n</file>\n")
	}
	return sb.String()
}

func parsePastedFiles(content string) []string {
	lines := strings.Split(content, "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "\"")
		line = strings.ReplaceAll(line, "\\ ", " ")
		if line == "" {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}

func attachmentFromPath(path string) (surfaceeditor.Attachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return surfaceeditor.Attachment{}, err
	}
	if info.IsDir() {
		return surfaceeditor.Attachment{}, fmt.Errorf("cannot attach a directory")
	}
	if info.Size() > maxAttachmentSize {
		return surfaceeditor.Attachment{}, fmt.Errorf("file is too large (>5mb)")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return surfaceeditor.Attachment{}, err
	}
	mimeBufferSize := len(content)
	if mimeBufferSize > 512 {
		mimeBufferSize = 512
	}
	mimeType := http.DetectContentType(content[:mimeBufferSize])
	return surfaceeditor.Attachment{
		FilePath: path,
		FileName: filepath.Base(path),
		MimeType: mimeType,
		Content:  content,
	}, nil
}

func safeAttachmentFileName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Trim(name, "/")
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

func imageAttachmentFileName(attachment surfaceeditor.Attachment) string {
	name := strings.TrimSpace(attachment.FileName)
	if name == "" {
		if path := strings.TrimSpace(attachment.FilePath); path != "" {
			name = filepath.Base(path)
		}
	}
	if name == "" {
		name = "image"
	}
	return safeAttachmentFileName(name)
}
