package webtools

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestValidateFetchURLRejectsPrivateTargets(t *testing.T) {
	t.Parallel()

	tests := []string{
		"http://127.0.0.1/admin",
		"http://[::1]/admin",
		"http://[::]/admin",
		"http://10.0.0.10/",
		"http://198.18.0.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://100.100.100.200/latest/meta-data/",
	}
	for _, rawURL := range tests {
		rawURL := rawURL
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()
			if err := validateFetchURL(context.Background(), rawURL); err == nil {
				t.Fatalf("validateFetchURL(%q) unexpectedly succeeded", rawURL)
			}
		})
	}
}

func TestValidatePublicIPsRejectsMixedPublicAndPrivateResults(t *testing.T) {
	t.Parallel()

	err := validatePublicIPs("rebind.example", []net.IPAddr{
		{IP: net.ParseIP("8.8.8.8")},
		{IP: net.ParseIP("127.0.0.1")},
	})
	if err == nil {
		t.Fatal("validatePublicIPs unexpectedly accepted a private fallback address")
	}
}

func TestWebFetchRedirectPolicyRejectsPrivateTarget(t *testing.T) {
	t.Parallel()

	redirectURL, err := url.Parse("http://127.0.0.1/private")
	if err != nil {
		t.Fatal(err)
	}
	req := &http.Request{URL: redirectURL}
	err = webFetchClient.CheckRedirect(req, []*http.Request{{}})
	if err == nil || !strings.Contains(err.Error(), "unsafe redirect") {
		t.Fatalf("CheckRedirect error = %v, want unsafe redirect error", err)
	}
}
