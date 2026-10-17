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

func TestImageWithoutMIMETypeIsSentAsWhatItsBytesAre(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	for _, tc := range []struct {
		image ImageContent
		want  string
	}{
		{ImageContent{DataBase64: png}, "data:image/png;base64," + png},
		{ImageContent{MIMEType: "Image/WebP; q=1", DataBase64: "AAAA"}, "data:image/webp;base64,AAAA"},
		{ImageContent{DataBase64: "AAAA"}, "data:image/jpeg;base64,AAAA"},
	} {
		if got := tc.image.DataURL(); got != tc.want {
			t.Errorf("DataURL = %q, want %q", got, tc.want)
		}
	}
}
