package providers

import "context"

type textStreamKey struct{}

type TextStreamFunc func(delta string) error

// WithTextStream attaches an optional text delta sink to the request context.
func WithTextStream(ctx context.Context, fn TextStreamFunc) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, textStreamKey{}, fn)
}

// TextStreamFromContext returns the text delta sink attached to ctx, if any.
func TextStreamFromContext(ctx context.Context) TextStreamFunc {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(textStreamKey{}).(TextStreamFunc)
	return fn
}

// StreamText forwards a streamed text delta to the sink attached to ctx.
func StreamText(ctx context.Context, delta string) error {
	if delta == "" {
		return nil
	}
	if fn := TextStreamFromContext(ctx); fn != nil {
		return fn(delta)
	}
	return nil
}
