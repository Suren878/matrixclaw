package tools

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// blockedCIDRs lists IP ranges that must never be fetched.
// Mirrors Hermes agent's url_safety.py approach.
var blockedCIDRs = func() []*net.IPNet {
	raw := []string{
		"127.0.0.0/8",    // loopback
		"::1/128",         // IPv6 loopback
		"10.0.0.0/8",     // RFC1918 private
		"172.16.0.0/12",  // RFC1918 private
		"192.168.0.0/16", // RFC1918 private
		"169.254.0.0/16", // link-local / cloud metadata (AWS 169.254.169.254)
		"fc00::/7",        // IPv6 unique local
		"fe80::/10",       // IPv6 link-local
		"100.64.0.0/10",  // CGNAT
		"0.0.0.0/8",      // unspecified
		"240.0.0.0/4",    // reserved
		"224.0.0.0/4",    // multicast
	}
	nets := make([]*net.IPNet, 0, len(raw))
	for _, cidr := range raw {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil {
			nets = append(nets, ipnet)
		}
	}
	return nets
}()

// blockedHosts lists explicit hostnames that must never be fetched.
var blockedHosts = []string{
	"metadata.google.internal",
	"169.254.169.254",
	"100.100.100.200", // Alibaba cloud metadata
}

func validateFetchURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("url scheme %q is not allowed; only http and https are supported", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url has no host")
	}
	for _, blocked := range blockedHosts {
		if strings.EqualFold(host, blocked) {
			return fmt.Errorf("url host %q is not allowed", host)
		}
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		for _, blocked := range blockedCIDRs {
			if blocked.Contains(ip) {
				return fmt.Errorf("url resolves to a private or reserved address (%s)", addr)
			}
		}
	}
	return nil
}
