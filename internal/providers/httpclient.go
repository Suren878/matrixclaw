package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

const (
	defaultStreamIdleTimeout = 120 * time.Second
	// Non-streaming replies send their headers only after the whole generation.
	defaultResponseHeaderTimeout = 10 * time.Minute
)

var ErrStreamIdle = errors.New("provider stream idle timeout")

// NewHTTPClient returns the client for model requests: no whole-request
// deadline, but a response body silent for defaultStreamIdleTimeout fails.
func NewHTTPClient() *http.Client {
	return newHTTPClient(defaultResponseHeaderTimeout, defaultStreamIdleTimeout)
}

func newHTTPClient(headerTimeout time.Duration, idleTimeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Transport: idleTimeoutTransport{base: transport, idle: idleTimeout}}
}

type idleTimeoutTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t idleTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	res, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel(nil)
		return nil, err
	}
	res.Body = &idleTimeoutBody{
		body:   res.Body,
		ctx:    ctx,
		cancel: cancel,
		idle:   t.idle,
		timer:  time.AfterFunc(t.idle, func() { cancel(ErrStreamIdle) }),
	}
	return res, nil
}

// idleTimeoutBody cancels the request when no Read returns for idle.
type idleTimeoutBody struct {
	body   io.ReadCloser
	ctx    context.Context
	cancel context.CancelCauseFunc
	idle   time.Duration
	timer  *time.Timer
}

func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if err != nil && errors.Is(context.Cause(b.ctx), ErrStreamIdle) {
		return n, ErrStreamIdle
	}
	b.timer.Reset(b.idle)
	return n, err
}

func (b *idleTimeoutBody) Close() error {
	b.timer.Stop()
	b.cancel(nil)
	return b.body.Close()
}
