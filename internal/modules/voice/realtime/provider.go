package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// ProviderSpec is a realtime voice provider: its catalog and its wire codec.
type ProviderSpec struct {
	ID           string
	Name         string
	Endpoint     string // default websocket URL
	DefaultModel string
	DefaultVoice string
	// KeyEnvs are the environment variables an API key is read from when the
	// setup names none; LLMProviders are setup providers whose key it reuses.
	KeyEnvs      []string
	LLMProviders []string
	Models       []string // nil: CheckKey lists them
	Voices       []string
	Languages    []Language
	// CheckKey verifies a key; it returns the live models when Models is nil.
	CheckKey func(ctx context.Context, key string) ([]string, error)
	// Dial is the websocket URL and headers of a session.
	Dial     func(cfg ProviderConfig, modelID string) (string, http.Header, error)
	NewCodec func(ProviderConnectRequest) Codec
}

// Codec is one session's provider wire protocol.
type Codec interface {
	// Setup are the messages sent after dialing.
	Setup() ([]any, error)
	// SetupDone reports whether a server message completes the setup.
	SetupDone(msg []byte) (bool, error)
	Encode(ProviderInput) ([]any, error)
	Decode(msg []byte) []ProviderOutput
}

// ProviderConfig is a provider's effective settings.
type ProviderConfig struct {
	APIKey    string
	APIKeyEnv string
	Endpoint  string
	ModelID   string
	VoiceID   string
	Language  string
}

// Language is a language a provider speaks; Aliases are other spellings.
type Language struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Aliases []string `json:"-"`
}

// NormalizeLanguage maps value to one of the spec's language codes; "auto"
// for no preference and value itself when the spec does not list it.
func (s ProviderSpec) NormalizeLanguage(value string) string {
	key := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "_", "-")))
	switch key {
	case "", "auto", "automatic", "detect", "default":
		return "auto"
	}
	for _, language := range s.Languages {
		if strings.EqualFold(language.Code, key) || slices.Contains(language.Aliases, key) {
			return language.Code
		}
	}
	if base, region, ok := strings.Cut(key, "-"); ok && len(base) == 2 && len(region) == 2 {
		return base + "-" + strings.ToUpper(region)
	}
	return strings.TrimSpace(value)
}

func (s ProviderSpec) languageName(code string) string {
	for _, language := range s.Languages {
		if language.Code == code {
			return language.Name
		}
	}
	return code
}

func (s ProviderSpec) hasModel(modelID string, models []string) bool {
	modelID = strings.TrimPrefix(strings.TrimSpace(modelID), "models/")
	return modelID != "" && slices.ContainsFunc(models, func(candidate string) bool {
		return strings.EqualFold(candidate, modelID)
	})
}

// KeyError is a failed API key check; Auth means the key was rejected.
type KeyError struct {
	Status  int
	Message string
}

func (e *KeyError) Error() string { return e.Message }

func (e *KeyError) Auth() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// BearerKeyCheck verifies a key by listing url's models with it.
func BearerKeyCheck(url string, name string) func(context.Context, string) ([]string, error) {
	return func(ctx context.Context, key string) ([]string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = res.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, &KeyError{Status: res.StatusCode, Message: fmt.Sprintf("%s models returned HTTP %d", name, res.StatusCode)}
		}
		return nil, nil
	}
}

// Instructions joins the daemon's and the session's instructions with the
// language policy for language.
func (s ProviderSpec) Instructions(base string, session string, language string) string {
	parts := []string{}
	for _, part := range []string{base, session, s.languagePolicy(language)} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (s ProviderSpec) languagePolicy(language string) string {
	code := s.NormalizeLanguage(language)
	if code == "auto" {
		return "Realtime voice language policy:\n" +
			"- Detect the human's language from the first meaningful speech and keep speaking that language for the rest of the conversation unless the human explicitly asks to change language.\n" +
			"- If the greeting or recent conversation is in a non-English language, continue in that non-English language instead of switching to English.\n" +
			"- If speech recognition is ambiguous, stay with the previously established conversation language."
	}
	name := s.languageName(code)
	return "Realtime voice language policy:\n" +
		"- Speak only in " + name + " (" + code + ") unless the human explicitly asks to change language.\n" +
		"- Keep pronunciation and accent natural for " + name + ".\n" +
		"- If speech recognition is ambiguous, stay in " + name + " instead of switching to another language."
}

const maxToolDeclarations = 128

// FunctionDeclarations are tools as provider function declarations with
// sanitized parameter schemas; typed adds "type": "function".
func FunctionDeclarations(tools []ToolDeclaration, typed bool, keepAdditionalProperties bool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		decl := map[string]any{"name": name, "description": strings.TrimSpace(tool.Description)}
		if typed {
			decl["type"] = "function"
		}
		var parameters any
		if len(tool.Parameters) > 0 && json.Unmarshal(tool.Parameters, &parameters) == nil {
			decl["parameters"] = sanitizeSchema(parameters, keepAdditionalProperties)
		}
		out = append(out, decl)
		if len(out) >= maxToolDeclarations {
			break
		}
	}
	return out
}

// sanitizeSchema keeps the JSON schema keywords realtime providers accept.
func sanitizeSchema(value any, keepAdditionalProperties bool) any {
	switch item := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, child := range item {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "properties":
				if props, ok := child.(map[string]any); ok {
					clean := make(map[string]any, len(props))
					for name, schema := range props {
						if name = strings.TrimSpace(name); name != "" {
							clean[name] = sanitizeSchema(schema, keepAdditionalProperties)
						}
					}
					out[key] = clean
				}
			case "enum":
				if values, ok := child.([]any); ok {
					values = slices.DeleteFunc(slices.Clone(values), func(v any) bool {
						text, isText := v.(string)
						return isText && strings.TrimSpace(text) == ""
					})
					if len(values) > 0 {
						out[key] = values
					}
				}
			case "additionalproperties":
				if keepAdditionalProperties {
					out[key] = sanitizeSchema(child, keepAdditionalProperties)
				}
			case "items", "type", "format", "description", "nullable", "required", "minimum", "maximum", "minitems", "maxitems", "minlength", "maxlength":
				out[key] = sanitizeSchema(child, keepAdditionalProperties)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(item))
		for _, child := range item {
			out = append(out, sanitizeSchema(child, keepAdditionalProperties))
		}
		return out
	default:
		return value
	}
}

var errAPIKeyRequired = errors.New("api key is required")
