package webresearch

import (
	"context"
	"errors"
)

type urlCheckKey struct{}

// WithURLCheck makes every fetch under ctx, redirects included, ask check first.
func WithURLCheck(ctx context.Context, check func(ctx context.Context, rawURL string) error) context.Context {
	if check == nil {
		return ctx
	}
	return context.WithValue(ctx, urlCheckKey{}, check)
}

// CheckURL applies the URL check of ctx; without one it allows the URL.
func CheckURL(ctx context.Context, rawURL string) error {
	return checkURL(ctx, rawURL, false)
}

func checkURL(ctx context.Context, rawURL string, required bool) error {
	check, ok := ctx.Value(urlCheckKey{}).(func(context.Context, string) error)
	if !ok {
		if required {
			return errors.New("permission checks are unavailable for this job")
		}
		return nil
	}
	return check(ctx, rawURL)
}
