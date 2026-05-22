package mcp

import (
	"strings"
	"time"
)

const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

type Config struct {
	Enabled bool
	Servers []ServerConfig
}

type ServerConfig struct {
	ID              string
	Name            string
	Enabled         bool
	Transport       string
	Command         string
	Args            []string
	Env             map[string]string
	Endpoint        string
	ToolPrefix      string
	ReadOnly        bool
	RequireApproval bool
	Timeout         time.Duration
}

func NormalizeConfig(cfg Config) Config {
	servers := make([]ServerConfig, 0, len(cfg.Servers))
	seen := map[string]struct{}{}
	for _, server := range cfg.Servers {
		server.ID = SanitizeToolPart(server.ID)
		server.Name = strings.TrimSpace(server.Name)
		server.Transport = normalizeTransport(server.Transport)
		server.Command = strings.TrimSpace(server.Command)
		server.Endpoint = strings.TrimRight(strings.TrimSpace(server.Endpoint), "/")
		server.ToolPrefix = SanitizeToolPart(server.ToolPrefix)
		if server.ToolPrefix == "" {
			server.ToolPrefix = server.ID
		}
		server.Args = trimStrings(server.Args)
		server.Env = trimMap(server.Env)
		if server.ID == "" {
			continue
		}
		if _, ok := seen[server.ID]; ok {
			continue
		}
		if server.Transport == TransportHTTP {
			if server.Endpoint == "" {
				continue
			}
		} else if server.Command == "" {
			continue
		}
		seen[server.ID] = struct{}{}
		servers = append(servers, server)
	}
	cfg.Servers = servers
	return cfg
}

func normalizeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case TransportHTTP, "streamable_http", "streamable-http":
		return TransportHTTP
	default:
		return TransportStdio
	}
}

func trimStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func trimMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
