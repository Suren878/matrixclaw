// Package image is the module that generates images through a configured
// provider's OpenAI-compatible images endpoint.
package image

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/modules"
	localstorage "github.com/Suren878/matrixclaw/internal/modules/storage"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/providers/discovery"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const ModuleID = "image"

type imageStore interface {
	SaveTemporary(rawPath string, content []byte, title string, tags []string, mimeType string) (localstorage.TempEntry, error)
}

type deliveryCreator interface {
	CreateClientDelivery(ctx context.Context, delivery core.ClientDelivery) (core.ClientDelivery, error)
}

type state struct {
	image     setup.ImageConfig
	providers []setup.ProviderConfig
}

type Module struct {
	state      atomic.Pointer[state]
	store      imageStore
	deliveries deliveryCreator
}

func New(store imageStore, deliveries deliveryCreator) *Module {
	m := &Module{store: store, deliveries: deliveries}
	m.state.Store(&state{})
	return m
}

func (m *Module) ID() string { return ModuleID }

func (m *Module) Apply(_ context.Context, cfg setup.Config) error {
	m.state.Store(&state{image: cfg.Modules.Image, providers: cfg.Providers})
	return nil
}

func (m *Module) Tools() []tools.Executor {
	if _, err := m.endpoint(); err != nil {
		return nil
	}
	return []tools.Executor{&generateTool{module: m}}
}

func (m *Module) Close() error { return nil }

func (m *Module) Status(context.Context) modules.Status {
	current := m.state.Load()
	status := modules.Status{ID: ModuleID, Title: "Image generation", Enabled: current.image.Enabled, State: "Off"}
	if !status.Enabled {
		return status
	}
	endpoint, err := m.endpoint()
	if err != nil {
		status.State, status.Detail = "Not ready", err.Error()
		return status
	}
	status.Ready, status.State = true, endpoint.providerName+" · "+endpoint.model
	status.Facts = []modules.Fact{
		{Key: "provider", Label: "Provider", Value: endpoint.providerID},
		{Key: "model", Label: "Model", Value: endpoint.model},
	}
	return status
}

// endpoint is where the module sends requests now; an error says what is
// missing while the module is on.
type endpoint struct {
	providerID, providerName string
	url, apiKey, model, size string
}

func (m *Module) endpoint() (endpoint, error) {
	current := m.state.Load()
	cfg := current.image
	if !cfg.Enabled {
		return endpoint{}, fmt.Errorf("image generation is off")
	}
	provider, ok := findProvider(current.providers, cfg.Provider)
	if !ok {
		return endpoint{}, fmt.Errorf("pick a provider")
	}
	runtime, hasKey := provider.Runtime()
	url, ok := imagesURL(runtime)
	switch {
	case !ok:
		return endpoint{}, fmt.Errorf("%s has no images endpoint", runtime.Name)
	case !hasKey:
		return endpoint{}, fmt.Errorf("%s has no API key", runtime.Name)
	case strings.TrimSpace(cfg.Model) == "":
		return endpoint{}, fmt.Errorf("pick a model")
	}
	return endpoint{
		providerID:   runtime.ID,
		providerName: runtime.Name,
		url:          url,
		apiKey:       runtime.APIKey,
		model:        strings.TrimSpace(cfg.Model),
		size:         strings.TrimSpace(cfg.Size),
	}, nil
}

func findProvider(configured []setup.ProviderConfig, id string) (setup.ProviderConfig, bool) {
	id = providers.CanonicalProviderID(strings.TrimSpace(id))
	for _, provider := range configured {
		if id != "" && providers.CanonicalProviderID(provider.ID) == id {
			return provider, true
		}
	}
	return setup.ProviderConfig{}, false
}

// imagesURL is the provider's OpenAI-compatible images endpoint; Gemini
// serves one under its openai path. Other provider types have none.
func imagesURL(provider setup.ProviderConfig) (string, bool) {
	base := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if base == "" {
		return "", false
	}
	switch providers.NormalizeOptionalProviderType(provider.Type) {
	case providers.TypeOpenAICompat:
		return base + "/images/generations", true
	case providers.TypeGemini:
		return base + "/openai/images/generations", true
	default:
		return "", false
	}
}

func (m *Module) Settings(ctx context.Context) []modules.Item {
	current := m.state.Load()
	cfg := current.image
	provider := modules.Item{Key: "provider", Kind: modules.ItemChoice, Label: "Provider", Value: cfg.Provider, Display: "Not set"}
	for _, configured := range current.providers {
		runtime, _ := configured.Runtime()
		if _, ok := imagesURL(runtime); ok {
			provider.Options = append(provider.Options, modules.Option{Value: runtime.ID, Label: runtime.Name})
			if runtime.ID == cfg.Provider {
				provider.Display = runtime.Name
			}
		}
	}
	return []modules.Item{
		{Key: "enabled", Kind: modules.ItemToggle, Label: "Enabled", Value: modules.OnOff(cfg.Enabled)},
		provider,
		m.modelItem(ctx, current),
		{Key: "size", Kind: modules.ItemText, Label: "Size", Value: cfg.Size, Display: displayOr(cfg.Size, "Provider default"), Hint: "1024x1024"},
	}
}

// modelItem picks from the provider's model list, or takes a typed name
// when the provider does not list its models.
func (m *Module) modelItem(ctx context.Context, current *state) modules.Item {
	cfg := current.image
	item := modules.Item{Key: "model", Kind: modules.ItemText, Label: "Model", Value: cfg.Model, Display: displayOr(cfg.Model, "Not set"), Hint: "model id"}
	provider, ok := findProvider(current.providers, cfg.Provider)
	if !ok {
		return item
	}
	runtime, _ := provider.Runtime()
	models, err := discovery.Models(ctx, discovery.ModelDiscoveryInput{ID: runtime.ID, Type: runtime.Type, BaseURL: runtime.BaseURL, APIKey: runtime.APIKey, Model: runtime.Model})
	if err != nil || len(models) == 0 {
		return item
	}
	item.Kind = modules.ItemChoice
	for _, model := range models {
		item.Options = append(item.Options, modules.Option{Value: model, Label: model})
	}
	return item
}

var sizePattern = regexp.MustCompile(`^[1-9][0-9]{1,4}x[1-9][0-9]{1,4}$`)

// Change sets one setting. Turning the module on before it has a provider
// and model opens what is missing; a new provider clears the model.
func (m *Module) Change(_ context.Context, path []string, value string) (modules.Change, error) {
	if len(path) != 1 {
		return modules.Change{}, modules.Unknown(path)
	}
	value = strings.TrimSpace(value)
	current := m.state.Load().image
	switch path[0] {
	case "enabled":
		on, err := modules.Toggle(value)
		if err != nil {
			return modules.Change{}, err
		}
		if on && current.Provider == "" {
			return modules.Change{Open: []string{"provider"}, Message: "Pick a provider first."}, nil
		}
		if on && current.Model == "" {
			return modules.Change{Open: []string{"model"}, Message: "Pick a model first."}, nil
		}
		return modules.Change{Config: func(c *setup.Config) error { c.Modules.Image.Enabled = on; return nil }}, nil
	case "provider":
		provider, ok := findProvider(m.state.Load().providers, value)
		if !ok {
			return modules.Change{}, fmt.Errorf("%w: unknown provider %q", modules.ErrInvalidSetting, value)
		}
		runtime, _ := provider.Runtime()
		if _, ok := imagesURL(runtime); !ok {
			return modules.Change{}, fmt.Errorf("%w: %s has no images endpoint", modules.ErrInvalidSetting, runtime.Name)
		}
		change := modules.Change{Config: func(c *setup.Config) error {
			if c.Modules.Image.Provider != runtime.ID {
				c.Modules.Image.Provider, c.Modules.Image.Model = runtime.ID, ""
			}
			return nil
		}}
		if current.Provider != runtime.ID {
			change.Open = []string{"model"}
		}
		return change, nil
	case "model":
		return modules.Change{Config: func(c *setup.Config) error { c.Modules.Image.Model = value; return nil }}, nil
	case "size":
		if value != "" && value != "auto" && !sizePattern.MatchString(value) {
			return modules.Change{}, fmt.Errorf("%w: size is WIDTHxHEIGHT, such as 1024x1024", modules.ErrInvalidSetting)
		}
		return modules.Change{Config: func(c *setup.Config) error { c.Modules.Image.Size = value; return nil }}, nil
	}
	return modules.Change{}, modules.Unknown(path)
}

func displayOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
