package core

import (
	"context"
	"fmt"
	"strings"
)

func (c *Core) UseBinding(ctx context.Context, input UseBindingInput) (ClientBinding, error) {
	binding := ClientBinding{
		Client:      strings.TrimSpace(input.Client),
		ExternalKey: strings.TrimSpace(input.ExternalKey),
		SessionID:   strings.TrimSpace(input.SessionID),
		UpdatedAt:   c.now().UTC(),
	}
	if binding.Client == "" {
		return ClientBinding{}, fmt.Errorf("%w: client is required", ErrInvalidInput)
	}
	if binding.ExternalKey == "" {
		return ClientBinding{}, fmt.Errorf("%w: external key is required", ErrInvalidInput)
	}
	if binding.SessionID == "" {
		return ClientBinding{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if _, err := c.store.GetSession(ctx, binding.SessionID); err != nil {
		return ClientBinding{}, err
	}
	if err := c.store.SaveBinding(ctx, binding); err != nil {
		return ClientBinding{}, err
	}
	return binding, nil
}

func (c *Core) CurrentBinding(ctx context.Context, client string, externalKey string) (ClientBinding, error) {
	client, externalKey = strings.TrimSpace(client), strings.TrimSpace(externalKey)
	if client == "" || externalKey == "" {
		return ClientBinding{}, fmt.Errorf("%w: client and external key are required", ErrInvalidInput)
	}
	return c.store.GetBinding(ctx, client, externalKey)
}
