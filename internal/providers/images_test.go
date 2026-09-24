package providers

import "testing"

func TestIsSupportedImageMIMEType(t *testing.T) {
	tests := map[string]bool{
		"image/jpeg":                true,
		"image/png; charset=binary": true,
		"image/gif":                 true,
		"image/webp":                true,
		"image/svg+xml":             false,
		"image/tiff":                false,
		"":                          false,
	}
	for mimeType, want := range tests {
		if got := IsSupportedImageMIMEType(mimeType); got != want {
			t.Errorf("IsSupportedImageMIMEType(%q) = %t, want %t", mimeType, got, want)
		}
	}
}
