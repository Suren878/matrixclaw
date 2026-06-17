package gateway

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddr        = "127.0.0.1:8090"
	defaultMatrixclawURL   = "http://127.0.0.1:8080"
	defaultARIURL          = "http://127.0.0.1:18088/ari"
	defaultARIUser         = "matrixclaw"
	defaultARIApp          = "matrixclaw"
	defaultSIPProfile      = "main"
	defaultRTPBind         = "0.0.0.0:40000"
	defaultCallTimeout     = 45 * time.Second
	defaultMaxCallDuration = 10 * time.Minute
	defaultRecordingFormat = "mp3"
	defaultRecordingPrefix = "call-records"
)

type Config struct {
	HTTPAddr         string
	GatewayToken     string
	MatrixclawURL    string
	MatrixclawToken  string
	ARIURL           string
	ARIUser          string
	ARIPassword      string
	ARIApp           string
	SIPProfile       string
	CallerID         string
	RTPBind          string
	RTPExternalHost  string
	CallTimeout      time.Duration
	MaxCallDuration  time.Duration
	InboundEnabled   bool
	InboundGreeting  string
	InboundPrompt    string
	InboundAllowed   map[string]struct{}
	PhonePrompt      string
	AssistantName    string
	RecordCalls      bool
	RecordingDir     string
	RecordingFormat  string
	RecordingPrefix  string
	RecordingStorage bool
}

func ConfigFromEnv() Config {
	return Config{
		HTTPAddr:         env("MATRIXCLAW_TELEPHONY_ADDR", defaultHTTPAddr),
		GatewayToken:     strings.TrimSpace(os.Getenv("MATRIXCLAW_TELEPHONY_TOKEN")),
		MatrixclawURL:    trimRightSlash(env("MATRIXCLAW_API_URL", defaultMatrixclawURL)),
		MatrixclawToken:  strings.TrimSpace(os.Getenv("MATRIXCLAW_API_TOKEN")),
		ARIURL:           trimRightSlash(env("MATRIXCLAW_TELEPHONY_ARI_URL", defaultARIURL)),
		ARIUser:          env("MATRIXCLAW_TELEPHONY_ARI_USER", defaultARIUser),
		ARIPassword:      strings.TrimSpace(os.Getenv("MATRIXCLAW_TELEPHONY_ARI_PASSWORD")),
		ARIApp:           env("MATRIXCLAW_TELEPHONY_ARI_APP", defaultARIApp),
		SIPProfile:       env("MATRIXCLAW_TELEPHONY_SIP_PROFILE", defaultSIPProfile),
		CallerID:         strings.TrimSpace(os.Getenv("MATRIXCLAW_TELEPHONY_CALLER_ID")),
		RTPBind:          env("MATRIXCLAW_TELEPHONY_RTP_BIND", defaultRTPBind),
		RTPExternalHost:  strings.TrimSpace(os.Getenv("MATRIXCLAW_TELEPHONY_RTP_EXTERNAL_HOST")),
		CallTimeout:      durationEnv("MATRIXCLAW_TELEPHONY_CALL_TIMEOUT", defaultCallTimeout),
		MaxCallDuration:  durationEnv("MATRIXCLAW_TELEPHONY_MAX_CALL_DURATION", defaultMaxCallDuration),
		InboundEnabled:   boolEnv("MATRIXCLAW_TELEPHONY_INBOUND_ENABLED", false),
		InboundGreeting:  env("MATRIXCLAW_TELEPHONY_INBOUND_GREETING", "Здравствуйте."),
		InboundPrompt:    strings.TrimSpace(os.Getenv("MATRIXCLAW_TELEPHONY_INBOUND_PROMPT")),
		InboundAllowed:   allowedPhoneSet(os.Getenv("MATRIXCLAW_TELEPHONY_INBOUND_ALLOWED_CALLERS")),
		PhonePrompt:      strings.TrimSpace(os.Getenv("MATRIXCLAW_TELEPHONY_PHONE_PROMPT")),
		AssistantName:    strings.TrimSpace(os.Getenv("MATRIXCLAW_TELEPHONY_ASSISTANT_NAME")),
		RecordCalls:      boolEnv("MATRIXCLAW_TELEPHONY_RECORD_CALLS", true),
		RecordingDir:     env("MATRIXCLAW_TELEPHONY_RECORDING_DIR", defaultRecordingDir()),
		RecordingFormat:  normalizeRecordingFormat(env("MATRIXCLAW_TELEPHONY_RECORDING_FORMAT", defaultRecordingFormat)),
		RecordingPrefix:  normalizeRecordingPrefix(env("MATRIXCLAW_TELEPHONY_RECORDING_PREFIX", defaultRecordingPrefix)),
		RecordingStorage: boolEnv("MATRIXCLAW_TELEPHONY_RECORDING_TEMP_STORAGE", true),
	}
}

func (c Config) InboundCallerAllowed(caller string) bool {
	if len(c.InboundAllowed) == 0 {
		return true
	}
	_, ok := c.InboundAllowed[normalizePhone(caller)]
	return ok
}

func (c Config) RTPExternalAddress(port int) string {
	host := strings.TrimSpace(c.RTPExternalHost)
	if host == "" {
		host = publicIPv4()
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		if strings.TrimSpace(p) != "" {
			return net.JoinHostPort(h, p)
		}
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

func env(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return duration
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func boolEnv(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "":
		return fallback
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	case "0", "false", "no", "off", "disable", "disabled":
		return false
	default:
		return fallback
	}
}

func allowedPhoneSet(raw string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		if phone := normalizePhone(item); phone != "" {
			out[phone] = struct{}{}
		}
	}
	return out
}

func defaultRecordingDir() string {
	stateRoot := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateRoot == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			stateRoot = filepath.Join(home, ".local", "state")
		}
	}
	if stateRoot == "" {
		stateRoot = os.TempDir()
	}
	return filepath.Join(stateRoot, "matrixclaw", "storage", "temporary", defaultRecordingPrefix)
}

func normalizeRecordingFormat(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return defaultRecordingFormat
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return defaultRecordingFormat
	}
	format := b.String()
	if supportedRecordingFormat(format) {
		return format
	}
	return defaultRecordingFormat
}

func supportedRecordingFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3", "wav", "gsm", "ulaw", "alaw", "sln":
		return true
	default:
		return false
	}
}

func normalizeRecordingPrefix(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/\\")
	if value == "" {
		return defaultRecordingPrefix
	}
	var parts []string
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if clean := recordingPathSegment(part); clean != "" && clean != "." {
			parts = append(parts, clean)
		}
	}
	if len(parts) == 0 {
		return defaultRecordingPrefix
	}
	return strings.Join(parts, "/")
}

func trimRightSlash(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func publicIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil && !ip4.IsLoopback() {
				return ip4.String()
			}
		}
	}
	return ""
}
