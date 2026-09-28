package webtools

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// PermissionSubject is the host the call fetches from.
func (e *webFetchExecutor) PermissionSubject(call tools.Call) permission.Subject {
	var params WebFetchParams
	if json.Unmarshal(call.Args, &params) != nil {
		return permission.Subject{}
	}
	parsed, err := url.Parse(strings.TrimSpace(params.URL))
	if err != nil || parsed.Hostname() == "" {
		return permission.Subject{}
	}
	return permission.Subject{Kind: permission.KindDomain, Value: strings.ToLower(parsed.Hostname())}
}
