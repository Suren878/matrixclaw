package webtools

import (
	"context"
	"encoding/json"
	"net/http"
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
	return domainSubject(parsed)
}

func domainSubject(target *url.URL) permission.Subject {
	return permission.Subject{Kind: permission.KindDomain, Value: permission.NormalizeDomain(target.Hostname())}
}

type recheckKey struct{}

// withRecheck carries a call's Recheck to the redirects of its fetches.
func withRecheck(ctx context.Context, recheck func(context.Context, permission.Subject) error) context.Context {
	if recheck == nil {
		return ctx
	}
	return context.WithValue(ctx, recheckKey{}, recheck)
}

// recheckRedirect applies the permission rules to a redirect's host.
func recheckRedirect(req *http.Request) error {
	recheck, ok := req.Context().Value(recheckKey{}).(func(context.Context, permission.Subject) error)
	if !ok {
		return nil
	}
	return recheck(req.Context(), domainSubject(req.URL))
}
