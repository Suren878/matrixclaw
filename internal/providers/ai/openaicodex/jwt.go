package openaicodex

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

func tokenExpiring(token string, skew time.Duration) bool {
	exp, ok := jwtExpiry(token)
	if !ok {
		return false
	}
	return time.Now().Add(skew).After(exp)
}

func jwtExpiry(token string) (time.Time, bool) {
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if !jwtClaims(token, &claims) || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

func jwtChatGPTAccountID(token string) string {
	var claims map[string]any
	if !jwtClaims(token, &claims) {
		return ""
	}
	auth, _ := claims["https://api.openai.com/auth"].(map[string]any)
	accountID, _ := auth["chatgpt_account_id"].(string)
	return strings.TrimSpace(accountID)
}

func jwtClaims(token string, out any) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}
