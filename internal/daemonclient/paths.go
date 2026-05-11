package daemonclient

import (
	"net/url"
	"strings"
)

func escapedPath(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}

func (c *Client) clientQuery() string {
	values := url.Values{}
	values.Set("client", c.ClientName)
	return values.Encode()
}

func (c *Client) bindingQuery() string {
	values := url.Values{}
	values.Set("client", c.ClientName)
	values.Set("external_key", c.ExternalKey)
	return values.Encode()
}
