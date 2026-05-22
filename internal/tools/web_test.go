package tools

import (
	"testing"
)

func TestValidateFetchURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com/page", false},
		{"http://example.com", false},
		{"ftp://example.com", true},
		{"", true},
		{"javascript:alert(1)", true},
		{"https://127.0.0.1/secret", true},
		{"https://localhost/secret", true},
		{"http://192.168.1.1/router", true},
		{"http://10.0.0.1/internal", true},
		{"http://172.16.0.5/internal", true},
		{"http://169.254.169.254/latest/meta-data", true},
		{"http://metadata.google.internal", true},
	}
	for _, tc := range tests {
		err := validateFetchURL(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateFetchURL(%q) err=%v, wantErr=%v", tc.url, err, tc.wantErr)
		}
	}
}

func TestExtractHTMLContent(t *testing.T) {
	html := []byte(`<html><head><title>Test Page</title></head>
<body>
  <h1>Hello World</h1>
  <p>This is a <strong>paragraph</strong> with <a href="https://example.com">a link</a>.</p>
  <script>alert("skip me")</script>
  <ul><li>Item 1</li><li>Item 2</li></ul>
</body></html>`)

	title, text := extractHTMLContent(html)
	if title != "Test Page" {
		t.Errorf("expected title 'Test Page', got %q", title)
	}
	if text == "" {
		t.Error("expected non-empty text")
	}
	for _, want := range []string{"Hello World", "paragraph", "Item 1", "Item 2"} {
		if !containsStr(text, want) {
			t.Errorf("expected text to contain %q, got:\n%s", want, text)
		}
	}
	if containsStr(text, "alert") {
		t.Error("extracted text should not contain script content")
	}
}

func TestDecodeDDGRedirect(t *testing.T) {
	tests := []struct {
		href string
		want string
	}{
		{"/l/?kh=-1&uddg=https%3A%2F%2Fexample.com%2Fpage", "https://example.com/page"},
		{"https://direct.com", "https://direct.com"},
		{"", ""},
		{"/l/?kh=-1", "/l/?kh=-1"},
	}
	for _, tc := range tests {
		got := decodeDDGRedirect(tc.href)
		if got != tc.want {
			t.Errorf("decodeDDGRedirect(%q) = %q, want %q", tc.href, got, tc.want)
		}
	}
}

func TestFormatSearchResults(t *testing.T) {
	results := []WebSearchResult{
		{Position: 1, Title: "Example", URL: "https://example.com", Description: "An example site"},
		{Position: 2, Title: "Another", URL: "https://another.com"},
	}
	out := formatSearchResults("test query", "duckduckgo", results)
	for _, want := range []string{"test query", "duckduckgo", "Example", "https://example.com", "An example site", "Another"} {
		if !containsStr(out, want) {
			t.Errorf("format output missing %q:\n%s", want, out)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
