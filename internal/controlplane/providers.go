package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

var (
	providerRoot         = valueVerb(CommandProvider, "", 0)
	providerModels       = verb(CommandProvider, "models", 2)
	providerUse          = valueVerb(CommandProvider, "use", 2)
	providerDefault      = verb(CommandProvider, "default", 1)
	providerDefaultUse   = valueVerb(CommandProvider, "default-use", 1)
	providerEdit         = verb(CommandProvider, "edit", 1)
	providerCustom       = verb(CommandProvider, "custom", 0)
	providerCustomNew    = verb(CommandProvider, "custom new", 1)
	providerCustomDelete = verb(CommandProvider, "custom delete", 1)
	providerCustomDrop   = verb(CommandProvider, "custom delete-confirm", 1)
	providerFormShow     = verb(CommandProvider, "form", 1)
	providerFormEdit     = verb(CommandProvider, "form-field", 2)
	providerFormSet      = valueVerb(CommandProvider, "form-set", 2)
	providerFormSave     = verb(CommandProvider, "form-save", 1)
	providerAuth         = verb(CommandProvider, "auth", 1)
	providerAuthComplete = verb(CommandProvider, "auth-complete", 1)
)

func (d *Dispatcher) handleProvider(ctx context.Context, r Route) (Result, error) {
	switch r.Verb {
	case providerRoot:
		return d.handleProviderRoot(ctx, r.Value)
	case providerModels, providerUse:
		session, refusal, err := d.llmSession(ctx, r.arg(0))
		if err != nil || refusal != "" {
			return textResult(refusal), err
		}
		if r.Is(providerModels) {
			return d.sessionModelPicker(ctx, session, r.arg(1))
		}
		return d.useSessionLLM(ctx, session, r.arg(1), r.Value)
	}
	if !d.owner() {
		return textResult(ownerOnlySettings), nil
	}
	switch r.Verb {
	case providerDefault:
		return d.handleProviderDefault(ctx, r.arg(0))
	case providerDefaultUse:
		return d.useDefaultLLM(ctx, r.arg(0), r.Value)
	case providerEdit:
		return d.handleProviderEdit(ctx, r.arg(0))
	case providerCustom:
		return customProviderTypePicker(), nil
	case providerCustomNew:
		return d.openCustomProviderForm(r.arg(0)), nil
	case providerCustomDelete:
		return d.customProviderDeleteConfirm(ctx, r.arg(0))
	case providerCustomDrop:
		return d.deleteCustomProvider(ctx, r.arg(0))
	case providerFormShow, providerFormEdit, providerFormSet, providerFormSave:
		return d.handleProviderForm(ctx, r)
	case providerAuth:
		return d.handleOpenAICodexAuth(ctx, r.arg(0))
	case providerAuthComplete:
		return d.handleOpenAICodexAuthComplete(ctx, r.arg(0))
	}
	return textResult("Unknown provider command."), nil
}

// handleProviderRoot lists the providers for the current session, or opens
// the models of the provider typed after /provider.
func (d *Dispatcher) handleProviderRoot(ctx context.Context, providerID string) (Result, error) {
	_, current, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if current == nil {
		return textResult("No active session. Use /new or /sessions."), nil
	}
	if core.CoreSessionIsExternalAgent(*current) {
		return textResult(externalProviderRefusal(*current)), nil
	}
	if providerID == "" {
		return d.sessionProviderPicker(ctx, *current, providerRoot.command(), "")
	}
	items, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	provider, ok := findSetupProvider(items, providerID)
	switch {
	case !ok:
		return textResult("Unknown provider: " + providerID), nil
	case !provider.Configured && d.owner():
		return d.openProviderForm(providerForm{Provider: provider}), nil
	case !provider.Configured:
		return textResult(provider.Name + " is not configured."), nil
	}
	return d.sessionModelPicker(ctx, *current, provider.ID)
}

func externalProviderRefusal(session core.Session) string {
	return "This session uses " + sessionRuntimeLabel(session) + ". Provider selection is available for MatrixClaw sessions only."
}

// llmSession is the MatrixClaw session sessionID names; refusal says why its
// provider cannot be chosen.
func (d *Dispatcher) llmSession(ctx context.Context, sessionID string) (core.Session, string, error) {
	session, err := d.findSession(ctx, sessionID)
	if err != nil {
		return core.Session{}, "", err
	}
	if core.CoreSessionIsExternalAgent(session) {
		return core.Session{}, externalProviderRefusal(session), nil
	}
	return session, "", nil
}

// sessionProviderPicker lists the providers to choose for session; command
// shows it again and back leaves it.
func (d *Dispatcher) sessionProviderPicker(ctx context.Context, session core.Session, command string, back string) (Result, error) {
	items, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerProvider, "Provider").Command(command).Back(back).Items(providerPickerItems(items, session, d.owner())...)
	if d.owner() {
		picker.Meta(defaultLLMInfo(items))
	}
	return Result{Screen: picker.Build()}, nil
}

// sessionModelPicker lists the models providerID offers session.
func (d *Dispatcher) sessionModelPicker(ctx context.Context, session core.Session, providerID string) (Result, error) {
	response, err := d.daemon.SessionModels(ctx, session.ID, providerID)
	if err != nil {
		return Result{}, err
	}
	choice := modelChoice{
		title:    "Model: " + textutil.FirstNonEmpty(response.ProviderLabel, response.ProviderID),
		command:  providerModels.command(session.ID, response.ProviderID),
		back:     sessionModel.command(session.ID),
		fallback: response.ModelID,
		failure:  response.ModelsError,
		models:   response.Models,
		choose:   func(model string) string { return providerUse.commandValue(model, session.ID, response.ProviderID) },
	}
	if strings.EqualFold(response.ProviderID, session.ProviderID) {
		choice.current = session.ModelID
	}
	if d.owner() {
		choice.settings = providerEdit.command(response.ProviderID)
	}
	return Result{Screen: choice.picker()}, nil
}

func (d *Dispatcher) useSessionLLM(ctx context.Context, session core.Session, providerID string, modelID string) (Result, error) {
	if strings.TrimSpace(providerID) == "" {
		return textResult("Provider id is required."), nil
	}
	if strings.TrimSpace(modelID) == "" {
		return modelPrompt(providerUse.command(session.ID, providerID), providerModels.command(session.ID, providerID)), nil
	}
	updated, err := d.daemon.UpdateSessionLLM(ctx, session.ID, providerID, modelID)
	if err != nil {
		return Result{}, err
	}
	text := fmt.Sprintf("✅ Model selected: %s · %s", d.providerLabel(ctx, updated.ProviderID), updated.ModelID)
	if _, err := d.daemon.CreateSystemMessage(ctx, updated.ID, text); err != nil {
		return Result{}, err
	}
	return Result{Screen: TextData{Text: text}, ReloadSnapshot: true}, nil
}

// providerLabel is the display name of providerID, or the id when the
// providers cannot be listed.
func (d *Dispatcher) providerLabel(ctx context.Context, providerID string) string {
	items, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return providerID
	}
	for _, item := range items {
		if strings.EqualFold(item.ID, providerID) {
			return providerPickerTitle(item)
		}
	}
	return providerID
}

// modelChoice is a model picker of one provider. When the provider lists no
// models it offers fallback and typing a model id; choose("") asks for one.
type modelChoice struct {
	title    string
	command  string
	back     string
	current  string
	fallback string
	failure  string
	models   []string
	choose   func(model string) string
	settings string
}

func (c modelChoice) picker() PickerData {
	picker := NewPickerData(PickerSessionModels, c.title).Command(c.command).Back(c.back)
	models := c.models
	if len(models) == 0 {
		picker.Meta(textutil.FirstNonEmpty(c.failure, "No models listed."))
		models = []string{c.fallback}
	}
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" {
			picker.Item(PickerItem{ID: model, Title: model, Selected: model == c.current, Command: c.choose(model)})
		}
	}
	if len(c.models) == 0 {
		picker.Action("type", "Enter model id", "", c.choose(""))
	}
	if c.settings != "" {
		picker.Action("settings", "Settings", "", c.settings)
	}
	return picker.Build()
}

func modelPrompt(submit string, cancel string) Result {
	return Result{Screen: PromptData{Title: "Model id", Placeholder: "model-id", Submit: submit, CancelCommand: cancel}}
}

// defaultLLMInfo names the provider and model new sessions start with.
func defaultLLMInfo(items []setup.ProviderSetupItem) string {
	for _, item := range items {
		if item.Active && item.Configured {
			return "Default for new sessions: " + providerPickerTitle(item) + " · " + item.Model
		}
	}
	return ""
}

// handleProviderDefault lists the configured providers to start new sessions
// with, or the models of providerID.
func (d *Dispatcher) handleProviderDefault(ctx context.Context, providerID string) (Result, error) {
	items, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	if providerID == "" {
		picker := NewPickerData(PickerProvider, "Default for new sessions").Command(providerDefault.command()).Back(providerRoot.command())
		for _, item := range items {
			if item.Configured {
				picker.Item(PickerItem{ID: item.ID, Title: providerPickerTitle(item), Info: item.Model, Selected: item.Active, Command: providerDefault.command(item.ID)})
			}
		}
		return Result{Screen: picker.Build()}, nil
	}
	provider, ok := findSetupProvider(items, providerID)
	if !ok || !provider.Configured {
		return textResult("Unknown provider: " + providerID), nil
	}
	response, err := d.daemon.ProviderModelCatalog(ctx, provider.ID, setup.ProviderSetupUpdate{})
	if err != nil {
		return Result{}, err
	}
	choice := modelChoice{
		title:    "Default model: " + providerPickerTitle(provider),
		command:  providerDefault.command(provider.ID),
		back:     providerDefault.command(),
		fallback: provider.Model,
		models:   response.Models,
		choose:   func(model string) string { return providerDefaultUse.commandValue(model, provider.ID) },
		settings: providerEdit.command(provider.ID),
	}
	if response.Status != setup.ProviderModelStatusOK {
		choice.models, choice.failure = nil, response.Message
	}
	if provider.Active {
		choice.current = provider.Model
	}
	return Result{Screen: choice.picker()}, nil
}

// useDefaultLLM makes providerID with modelID what new sessions start with.
func (d *Dispatcher) useDefaultLLM(ctx context.Context, providerID string, modelID string) (Result, error) {
	if strings.TrimSpace(providerID) == "" {
		return textResult("Provider id is required."), nil
	}
	if modelID = strings.TrimSpace(modelID); modelID == "" {
		return modelPrompt(providerDefaultUse.command(providerID), providerDefault.command(providerID)), nil
	}
	saved, err := d.daemon.ConfigureSetupProvider(ctx, providerID, setup.ProviderSetupUpdate{Model: &modelID, Active: true})
	if err != nil {
		return Result{}, err
	}
	return Result{Screen: TextData{Text: "✅ Default for new sessions: " + providerPickerTitle(saved) + " · " + saved.Model}, ReloadSnapshot: true}, nil
}

func isOpenAICodexProvider(provider setup.ProviderSetupItem) bool {
	policy := providers.PolicyForProvider(provider.ID, provider.Type)
	return policy.AuthMode == providers.ProviderAuthOAuth && policy.Type == providers.TypeOpenAICodex
}

func openAICodexAuthInfo() string {
	status := openaicodex.CurrentAuthStatus()
	if status.SignedIn {
		return "Signed in"
	}
	if status.Expired {
		return "Expired"
	}
	return "Not signed in"
}
