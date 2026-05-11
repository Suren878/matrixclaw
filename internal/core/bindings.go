package core

import (
	"context"
	"fmt"
)

func (c *Core) UseBinding(ctx context.Context, input UseBindingInput) (ClientBinding, error) {
	if normalizeText(input.Client) == "" {
		return ClientBinding{}, fmt.Errorf("%w: client is required", ErrInvalidInput)
	}
	if normalizeText(input.ExternalKey) == "" {
		return ClientBinding{}, fmt.Errorf("%w: external key is required", ErrInvalidInput)
	}
	if normalizeText(input.SessionID) == "" {
		return ClientBinding{}, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	if _, err := c.store.GetSession(ctx, input.SessionID); err != nil {
		return ClientBinding{}, err
	}

	binding := ClientBinding{
		Client:      normalizeText(input.Client),
		ExternalKey: normalizeText(input.ExternalKey),
		SessionID:   normalizeText(input.SessionID),
		UpdatedAt:   c.now().UTC(),
	}
	if err := c.store.SaveBinding(ctx, binding); err != nil {
		return ClientBinding{}, err
	}
	return binding, nil
}

func (c *Core) CurrentBinding(ctx context.Context, client string, externalKey string) (ClientBinding, error) {
	if normalizeText(client) == "" || normalizeText(externalKey) == "" {
		return ClientBinding{}, fmt.Errorf("%w: client and external key are required", ErrInvalidInput)
	}
	return c.store.GetBinding(ctx, normalizeText(client), normalizeText(externalKey))
}
