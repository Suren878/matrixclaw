package webtools

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestWebFetchRedirectPolicyRejectsPrivateTarget(t *testing.T) {
	t.Parallel()

	redirectURL, err := url.Parse("http://127.0.0.1/private")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: redirectURL}
	err = fetchClient(nil).CheckRedirect(req, []*http.Request{{}})
	if err == nil || !strings.Contains(err.Error(), "unsafe redirect") {
		t.Fatalf("CheckRedirect error = %v, want unsafe redirect error", err)
	}
}
