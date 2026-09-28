package webtools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/webresearch"
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

// withRecheck makes every URL fetched under ctx, redirects included, meet the
// call's permission rules.
func withRecheck(ctx context.Context, recheck func(context.Context, permission.Subject) error) context.Context {
	if recheck == nil {
		return ctx
	}
	return webresearch.WithURLCheck(ctx, func(ctx context.Context, rawURL string) error {
		parsed, err := url.Parse(strings.TrimSpace(rawURL))
		if err != nil || parsed.Hostname() == "" {
			return fmt.Errorf("cannot name the host of %q", rawURL)
		}
		return recheck(ctx, domainSubject(parsed))
	})
}
