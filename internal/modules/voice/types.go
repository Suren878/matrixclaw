package voice

import (
	"encoding/base64"
	"strings"
)

type TextToSpeechRequest struct {
	Text     string `json:"text"`
	VoiceID  string `json:"voice_id,omitempty"`
	Language string `json:"language,omitempty"`
}

type TextToSpeechResponse struct {
	ContentBase64 string `json:"content_base64"`
	MIMEType      string `json:"mime_type"`
	FileName      string `json:"file_name"`
}

func NewTextToSpeechResponse(content []byte, mimeType string, fileName string) TextToSpeechResponse {
	return TextToSpeechResponse{
		ContentBase64: base64.StdEncoding.EncodeToString(content),
		MIMEType:      strings.TrimSpace(mimeType),
		FileName:      strings.TrimSpace(fileName),
	}
}

func (r TextToSpeechResponse) ContentBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(r.ContentBase64))
}

type SpeechToTextRequest struct {
	ContentBase64 string `json:"content_base64"`
	FileName      string `json:"file_name,omitempty"`
	MIMEType      string `json:"mime_type,omitempty"`
	Language      string `json:"language,omitempty"`
}

func NewSpeechToTextRequest(content []byte, fileName string, mimeType string) SpeechToTextRequest {
	return SpeechToTextRequest{
		ContentBase64: base64.StdEncoding.EncodeToString(content),
		FileName:      strings.TrimSpace(fileName),
		MIMEType:      strings.TrimSpace(mimeType),
	}
}

func (r SpeechToTextRequest) ContentBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(r.ContentBase64))
}

type SpeechToTextResponse struct {
	Text string `json:"text"`
}
