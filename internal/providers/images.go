package providers

import (
	"encoding/base64"
	"net/http"
	"strings"
)

// IsSupportedImageMIMEType reports whether image data can be sent inline to every
// image-capable provider. Other image/* files (for example SVG) stay attachment
// references only.
func IsSupportedImageMIMEType(mimeType string) bool {
	switch bareMediaType(mimeType) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// MediaType is the image's MIME type without parameters. When none is given
// it is read from the data's magic bytes, else image/jpeg.
func (img ImageContent) MediaType() string {
	if mediaType := bareMediaType(img.MIMEType); mediaType != "" {
		return mediaType
	}
	head := strings.TrimSpace(img.DataBase64)
	head = head[:min(len(head), 64)]
	raw, _ := base64.StdEncoding.DecodeString(head[:len(head)/4*4])
	if sniffed := http.DetectContentType(raw); IsSupportedImageMIMEType(sniffed) {
		return sniffed
	}
	return "image/jpeg"
}

// DataURL is the image as a data: URL.
func (img ImageContent) DataURL() string {
	return "data:" + img.MediaType() + ";base64," + strings.TrimSpace(img.DataBase64)
}

func bareMediaType(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = strings.TrimSpace(mimeType[:index])
	}
	return mimeType
}
