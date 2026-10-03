package webtools

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// PermissionSubject is the host the call fetches from.
func (webFetchExecutor) PermissionSubject(call tools.Call) permission.Subject {
	var params WebFetchParams
	if json.Unmarshal(call.Args, &params) != nil {
		return permission.Subject{}
	}
	parsed, err := url.Parse(strings.TrimSpace(params.URL))
	if err != nil || parsed.Hostname() == "" {
		return permission.Subject{}
	}
	return domainSubject(parsed)
}

func domainSubject(target *url.URL) permission.Subject {
	return permission.Subject{Kind: permission.KindDomain, Value: permission.NormalizeDomain(target.Hostname())}
}
