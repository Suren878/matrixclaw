package controlplane

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleWebSearch(ctx context.Context, args string) (Result, error) {
	if d.webSearch == nil {
		return unsupportedRuntime("web search"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.webSearchPicker(ctx)
	case setup.WebSearchProviderDDG:
		return d.webSearchUse(ctx, setup.WebSearchProviderDDG)
	case setup.WebSearchProviderTavily, setup.WebSearchProviderSerper, setup.WebSearchProviderSearXNG:
		return d.webSearchProviderDetail(ctx, step, rest)
	default:
		return d.webSearchPicker(ctx)
	}
}

func (d *Dispatcher) webSearchPicker(ctx context.Context) (Result, error) {
	resp, err := d.webSearch.GetWebSearchConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	current := resp.Provider

	return Result{
		Handled: true,
		Picker: NewPickerData(PickerWebSearch, "Web Search").
			Back(modulesCommand()).
			Item(PickerItem{
				ID:       setup.WebSearchProviderDDG,
				Title:    "DuckDuckGo",
				Info:     "Free",
				Selected: current == setup.WebSearchProviderDDG,
				Command:  webSearchCommand(setup.WebSearchProviderDDG),
			}).
			Item(PickerItem{
				ID:       setup.WebSearchProviderTavily,
				Title:    "Tavily",
				Info:     "1000 req/mo free",
				Selected: current == setup.WebSearchProviderTavily,
				Command:  webSearchCommand(setup.WebSearchProviderTavily),
			}).
			Item(PickerItem{
				ID:       setup.WebSearchProviderSerper,
				Title:    "Serper",
				Info:     "2500 req/mo free",
				Selected: current == setup.WebSearchProviderSerper,
				Command:  webSearchCommand(setup.WebSearchProviderSerper),
			}).
			Item(PickerItem{
				ID:       setup.WebSearchProviderSearXNG,
				Title:    "SearXNG",
				Info:     "Self-hosted",
				Selected: current == setup.WebSearchProviderSearXNG,
				Command:  webSearchCommand(setup.WebSearchProviderSearXNG),
			}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) webSearchProviderDetail(ctx context.Context, provider, args string) (Result, error) {
	step, rest := firstCommandStep(args)
	switch step {
	case "use":
		return d.webSearchUse(ctx, provider)
	case "key":
		if rest == "" {
			return d.webSearchKeyPrompt(ctx, provider)
		}
		return d.webSearchSetKey(ctx, provider, rest)
	case "url":
		if rest == "" {
			return d.webSearchURLPrompt(ctx, provider)
		}
		return d.webSearchSetURL(ctx, provider, rest)
	}
	return d.webSearchDetailPicker(ctx, provider)
}

func (d *Dispatcher) webSearchDetailPicker(ctx context.Context, provider string) (Result, error) {
	resp, err := d.webSearch.GetWebSearchConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	current := resp.Provider
	keyPreview := webSearchKeyPreview(resp, provider)

	var useDisabled bool
	switch provider {
	case setup.WebSearchProviderTavily, setup.WebSearchProviderSerper:
		useDisabled = keyPreview == ""
	case setup.WebSearchProviderSearXNG:
		useDisabled = resp.BaseURL == ""
	}

	picker := NewPickerData(PickerWebSearchProvider, webSearchProviderLabel(provider)).
		Back(webSearchCommand()).
		Item(PickerItem{
			ID:       "use",
			Title:    "Use",
			Selected: current == provider,
			Disabled: useDisabled,
			Command:  webSearchCommand(provider, "use"),
		})

	switch provider {
	case setup.WebSearchProviderTavily, setup.WebSearchProviderSerper:
		picker.Row("key", "API Key", firstNonEmptyTrimmed(keyPreview, "Not set"), webSearchCommand(provider, "key"))
	case setup.WebSearchProviderSearXNG:
		urlInfo := resp.BaseURL
		if urlInfo == "" {
			urlInfo = "Not set"
		}
		picker.Row("url", "Base URL", urlInfo, webSearchCommand(provider, "url"))
	}

	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) webSearchUse(ctx context.Context, provider string) (Result, error) {
	_, err := d.webSearch.UpdateWebSearchConfig(ctx, setup.WebSearchConfigUpdate{Provider: &provider})
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	if provider == setup.WebSearchProviderDDG {
		return d.webSearchPicker(ctx)
	}
	return d.webSearchDetailPicker(ctx, provider)
}

func (d *Dispatcher) webSearchKeyPrompt(ctx context.Context, provider string) (Result, error) {
	resp, err := d.webSearch.GetWebSearchConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	title, placeholder := webSearchKeyPromptText(provider)
	if preview := webSearchKeyPreview(resp, provider); preview != "" {
		placeholder = preview + " · - to clear"
	}
	return Result{Handled: true, Prompt: &PromptData{
		Title:               title,
		Placeholder:         placeholder,
		SubmitCommandPrefix: webSearchCommand(provider, "key") + " ",
		CancelCommand:       webSearchCommand(provider),
		Sensitive:           true,
	}}, nil
}

func (d *Dispatcher) webSearchSetKey(ctx context.Context, provider, key string) (Result, error) {
	update := setup.WebSearchConfigUpdate{Provider: &provider}
	switch provider {
	case setup.WebSearchProviderTavily:
		update.TavilyKey = clearableInput(key)
	case setup.WebSearchProviderSerper:
		update.SerperKey = clearableInput(key)
	}
	_, err := d.webSearch.UpdateWebSearchConfig(ctx, update)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.webSearchDetailPicker(ctx, provider)
}

func (d *Dispatcher) webSearchURLPrompt(ctx context.Context, provider string) (Result, error) {
	resp, err := d.webSearch.GetWebSearchConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "SearXNG Base URL",
		Placeholder:         "http://localhost:8888",
		Value:               resp.BaseURL,
		SubmitCommandPrefix: webSearchCommand(provider, "url") + " ",
		CancelCommand:       webSearchCommand(provider),
	}}, nil
}

func (d *Dispatcher) webSearchSetURL(ctx context.Context, provider, rawURL string) (Result, error) {
	_, err := d.webSearch.UpdateWebSearchConfig(ctx, setup.WebSearchConfigUpdate{
		Provider: &provider,
		BaseURL:  clearableInput(rawURL),
	})
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.webSearchDetailPicker(ctx, provider)
}

func webSearchProviderLabel(provider string) string {
	switch provider {
	case setup.WebSearchProviderTavily:
		return "Tavily"
	case setup.WebSearchProviderSerper:
		return "Serper"
	case setup.WebSearchProviderSearXNG:
		return "SearXNG"
	default:
		return "DuckDuckGo"
	}
}

func webSearchKeyPromptText(provider string) (title, placeholder string) {
	switch provider {
	case setup.WebSearchProviderTavily:
		return "Tavily API Key", "tvly-..."
	case setup.WebSearchProviderSerper:
		return "Serper API Key", "..."
	default:
		return "API Key", "..."
	}
}

func webSearchKeyPreview(resp setup.WebSearchConfigResponse, provider string) string {
	switch provider {
	case setup.WebSearchProviderTavily:
		return resp.TavilyKeyPreview
	case setup.WebSearchProviderSerper:
		return resp.SerperKeyPreview
	default:
		return ""
	}
}
