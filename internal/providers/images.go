package providers

import "strings"

// IsSupportedImageMIMEType reports whether image data can be sent inline to every
// image-capable provider. Other image/* files (for example SVG) stay attachment
// references only.
func IsSupportedImageMIMEType(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = strings.TrimSpace(mimeType[:index])
	}
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
