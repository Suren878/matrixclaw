package runtime

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

const (
	DefaultClientName  = "terminal:local"
	DefaultExternalKey = "local"
)

type Config struct {
	BaseURL     string
	APIToken    string
	ClientName  string
	ExternalKey string
	WorkingDir  string
	Version     string
}

type Runtime struct {
	config Config
	client *daemonclient.Client
}

func (r *Runtime) daemon() (*daemonclient.Client, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("terminal runtime is not configured")
	}
	return r.client, nil
}

func New(config Config) *Runtime {
	clientName := strings.TrimSpace(config.ClientName)
	if clientName == "" {
		clientName = DefaultClientName
	}
	externalKey := strings.TrimSpace(config.ExternalKey)
	if externalKey == "" {
		externalKey = DefaultExternalKey
	}
	rt := &Runtime{
		config: Config{
			BaseURL:     strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"),
			APIToken:    strings.TrimSpace(config.APIToken),
			ClientName:  clientName,
			ExternalKey: externalKey,
			WorkingDir:  strings.TrimSpace(config.WorkingDir),
			Version:     strings.TrimSpace(config.Version),
		},
		client: daemonclient.New(strings.TrimRight(strings.TrimSpace(config.BaseURL), "/"), clientName, externalKey).WithAPIToken(config.APIToken),
	}
	return rt
}
