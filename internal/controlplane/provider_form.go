package controlplane

import (
	"context"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// providerForm is an open provider form: the provider as saved (or a new
// custom provider's type) and the edits not saved yet.
type providerForm struct {
	Provider setup.ProviderSetupItem
	Update   setup.ProviderSetupUpdate
}

func (f providerForm) policy() providers.ProviderPolicy {
	return providers.PolicyForProvider(f.Provider.ID, f.Provider.Type)
}

func (f providerForm) custom() bool {
	return !f.policy().Known
}

func (f providerForm) name() string {
	return pendingOr(f.Update.Name, f.Provider.Name)
}

func (f providerForm) baseURL() string {
	return pendingOr(f.Update.BaseURL, f.Provider.BaseURL)
}

func (f providerForm) model() string {
	return pendingOr(f.Update.Model, textutil.FirstNonEmpty(f.Provider.Model, f.Provider.DefaultModel))
}

func (f providerForm) capabilities() providers.ModelCapabilitySet {
	return providers.ResolveModelCapabilities(providers.ModelCapabilityInput{ProviderID: f.Provider.ID, ProviderType: f.Provider.Type, ModelID: f.model()})
}

func (f providerForm) reasoningEffort() string {
	return providers.NormalizeReasoningEffortForModel(f.Provider.ID, f.Provider.Type, f.model(), pendingOr(f.Update.ReasoningEffort, f.Provider.ReasoningEffort))
}

func (f providerForm) toolUseMode() providers.ToolUseMode {
	if f.Update.ToolUseMode != nil {
		return *f.Update.ToolUseMode
	}
	return f.Provider.ToolUseMode
}

func (f providerForm) hasKey() bool {
	return f.Update.APIKey != nil || f.Provider.APIKeyPreview != ""
}

func (f providerForm) title() string {
	if f.Provider.ID != "" {
		return "Edit " + textutil.FirstNonEmpty(f.Provider.Name, f.Provider.ID)
	}
	if f.Provider.Type == providers.TypeAnthropic {
		return "Custom Anthropic-Compatible"
	}
	return "Custom OpenAI-Compatible"
}

func pendingOr(value *string, fallback string) string {
	if value != nil {
		return *value
	}
	return fallback
}

// set records a typed or picked value; an empty value changes nothing.
func (f *providerForm) set(field string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	switch field {
	case "name":
		f.Update.Name = &value
	case "base":
		f.Update.BaseURL = &value
	case "key":
		f.Update.APIKey = &value
	case "model":
		f.Update.Model = &value
	case "reasoning":
		effort := providers.NormalizeReasoningEffort(value)
		f.Update.ReasoningEffort = &effort
	case "tools":
		mode := providers.NormalizeToolUseMode(providers.ToolUseMode(value))
		f.Update.ToolUseMode = &mode
	}
}

type providerFormField struct {
	id       string
	label    string
	value    string
	required bool
}

func (f providerForm) fields() []providerFormField {
	policy := f.policy()
	capabilities := f.capabilities().ProviderCapabilities
	fields := make([]providerFormField, 0, 6)
	if f.custom() {
		fields = append(fields, providerFormField{id: "name", label: "Provider name", value: f.name(), required: true})
	}
	if options := policy.BaseURLOptions; f.custom() || len(options) > 0 {
		field := providerFormField{id: "base", label: "Base URL", value: f.baseURL(), required: true}
		if len(options) > 0 {
			field.label = "Endpoint"
			for _, option := range options {
				if option.URL == field.value {
					field.value = option.Name
				}
			}
		}
		fields = append(fields, field)
	}
	if policy.RequiresAPIKey {
		key := f.Provider.APIKeyPreview
		if f.Update.APIKey != nil {
			key = setup.MaskSecret(*f.Update.APIKey)
		}
		fields = append(fields, providerFormField{id: "key", label: "API key", value: key, required: !providers.IsLoopbackURL(f.baseURL())})
	}
	if f.custom() {
		fields = append(fields, providerFormField{id: "model", label: "Model", value: f.model(), required: true})
	}
	if capabilities.ReasoningEffort {
		fields = append(fields, providerFormField{id: "reasoning", label: "Reasoning effort", value: f.reasoningEffort()})
	}
	if capabilities.ToolCalling {
		fields = append(fields, providerFormField{id: "tools", label: "Tool use", value: f.toolUseMode().Label()})
	}
	return fields
}

func (f providerForm) missing() string {
	for _, field := range f.fields() {
		if field.required && strings.TrimSpace(field.value) == "" {
			return field.label + " is required."
		}
	}
	return ""
}

func (d *Dispatcher) openProviderForm(form providerForm) Result {
	return providerFormResult(d.forms.open(form), form, "")
}

func providerFormResult(formID string, form providerForm, message string) Result {
	fields := make([]FormField, 0, 7)
	if form.policy().AuthMode == providers.ProviderAuthOAuth {
		fields = append(fields, FormField{
			ID:          "auth",
			Label:       "Authorization",
			Value:       openAICodexAuthInfo(),
			EditCommand: providerAuth.command(form.Provider.ID),
		})
	}
	for _, field := range form.fields() {
		value := field.value
		if field.required && strings.TrimSpace(value) == "" {
			value = "Required"
		}
		fields = append(fields, FormField{ID: field.id, Label: field.label, Value: value, EditCommand: providerFormEdit.command(formID, field.id)})
	}
	return Result{
		Screen: FormData{
			Title:         form.title(),
			Fields:        fields,
			SubmitLabel:   "Save",
			CancelLabel:   "Close",
			SubmitCommand: providerFormSave.command(formID),
			CancelCommand: providerRoot.command(),
			Error:         strings.TrimSpace(message),
		},
	}
}

func (d *Dispatcher) handleProviderForm(ctx context.Context, r Route) (Result, error) {
	formID := r.arg(0)
	form, ok := formValue[providerForm](d.forms, formID)
	if !ok {
		return textResult("This provider form has expired. Open it again with /provider."), nil
	}
	switch r.Verb {
	case providerFormEdit:
		return d.providerFormField(formID, form, r.arg(1)), nil
	case providerFormSet:
		form.set(r.arg(1), r.Value)
		d.forms.put(formID, form)
	case providerFormSave:
		return d.saveProviderForm(ctx, formID, form)
	}
	return providerFormResult(formID, form, ""), nil
}

func (d *Dispatcher) providerFormField(formID string, form providerForm, field string) Result {
	back := providerFormShow.command(formID)
	set := func(value string) string { return providerFormSet.commandValue(value, formID, field) }
	picker := func(title string, items []PickerItem) Result {
		return Result{Screen: NewPickerData(PickerProviderCustom, form.title()+": "+title).Select(back).Items(items...).Build()}
	}
	switch field {
	case "base":
		options := form.policy().BaseURLOptions
		if len(options) == 0 {
			break
		}
		items := make([]PickerItem, 0, len(options))
		for _, option := range options {
			items = append(items, PickerItem{ID: option.ID, Title: option.Name, Info: option.URL, Selected: option.URL == form.baseURL(), Command: set(option.URL)})
		}
		return picker("endpoint", items)
	case "reasoning":
		efforts := form.capabilities().ReasoningEfforts
		items := make([]PickerItem, 0, len(efforts))
		for _, effort := range efforts {
			items = append(items, PickerItem{ID: effort, Title: cases.Title(language.Und).String(effort), Selected: effort == form.reasoningEffort(), Command: set(effort)})
		}
		return picker("reasoning effort", items)
	case "tools":
		current := providers.NormalizeToolUseMode(form.toolUseMode())
		items := make([]PickerItem, 0, 2)
		for _, mode := range []providers.ToolUseMode{providers.ToolUseNative, providers.ToolUseDisabled} {
			items = append(items, PickerItem{ID: string(mode), Title: mode.Label(), Focused: mode == current, Command: set(string(mode))})
		}
		return picker("tool mode", items)
	}
	return providerFieldPrompt(formID, form, field)
}

func providerFieldPrompt(formID string, form providerForm, field string) Result {
	title, value := field, ""
	var placeholder string
	switch field {
	case "name":
		title, value, placeholder = "provider name", form.name(), "Local AI"
	case "base":
		title, value, placeholder = "base URL", form.baseURL(), "https://api.example.com/v1"
	case "model":
		value, placeholder = form.model(), "model-id"
	case "key":
		title, placeholder = "API key", "API key"
		if form.hasKey() {
			placeholder = "leave empty to keep"
		}
	default:
		return providerFormResult(formID, form, "")
	}
	return Result{
		Screen: PromptData{
			Title:         form.title() + ": " + title,
			Placeholder:   placeholder,
			Value:         value,
			Submit:        providerFormSet.command(formID, field),
			CancelCommand: providerFormShow.command(formID),
			Sensitive:     field == "key",
		},
	}
}

// saveProviderForm saves the edits and returns to the provider list; the
// form stays open with the error when the daemon refuses them.
func (d *Dispatcher) saveProviderForm(ctx context.Context, formID string, form providerForm) (Result, error) {
	if message := form.missing(); message != "" {
		return providerFormResult(formID, form, message), nil
	}
	providerID := form.Provider.ID
	if providerID == "" {
		providerID = customProviderID(form.name())
	}
	if _, err := d.daemon.ConfigureSetupProvider(ctx, providerID, form.Update); err != nil {
		return providerFormResult(formID, form, err.Error()), nil
	}
	d.forms.close(formID)
	result, err := d.handleProviderRoot(ctx, "")
	result.ReloadSnapshot = true
	return result, err
}

func (d *Dispatcher) handleProviderEdit(ctx context.Context, providerID string) (Result, error) {
	provider, err := d.setupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	return d.openProviderForm(providerForm{Provider: provider}), nil
}

func (d *Dispatcher) openCustomProviderForm(kind string) Result {
	var providerType string
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai":
		providerType = providers.TypeOpenAICompat
	case "anthropic":
		providerType = providers.TypeAnthropic
	default:
		return customProviderTypePicker()
	}
	return d.openProviderForm(providerForm{
		Provider: setup.ProviderSetupItem{Type: providerType},
		Update:   setup.ProviderSetupUpdate{Type: &providerType},
	})
}
