package editor

import (
	"slices"
	"strings"
)

const (
	TextareaMaxHeight  = 15
	EditorHeightMargin = 2
	TextareaMinHeight  = 3
)

type Attachment struct {
	FilePath string
	FileName string
	MimeType string
	Content  []byte
}

func (a Attachment) IsText() bool {
	return strings.HasPrefix(a.MimeType, "text/")
}

func (a Attachment) IsImage() bool {
	return strings.HasPrefix(a.MimeType, "image/")
}

func ContainsTextAttachment(attachments []Attachment) bool {
	return slices.ContainsFunc(attachments, func(a Attachment) bool {
		return a.IsText()
	})
}

func ContainsMessageAttachment(attachments []Attachment) bool {
	return slices.ContainsFunc(attachments, func(a Attachment) bool {
		return a.IsText() || a.IsImage()
	})
}

type OpenEditorMsg struct {
	Text string
}

type HeightChangedMsg struct {
	PreviousTextareaHeight int
	TextareaHeight         int
	PreviousEditorHeight   int
	EditorHeight           int
}

type ExternalEditorErrorMsg struct {
	Err error
}

type ExternalEditorWarningMsg struct {
	Message string
}
