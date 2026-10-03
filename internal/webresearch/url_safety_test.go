package webresearch

import (
	"context"
	"net"
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
			if err := ValidatePublicURL(context.Background(), rawURL); err == nil {
				t.Fatalf("ValidatePublicURL(%q) unexpectedly succeeded", rawURL)
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
