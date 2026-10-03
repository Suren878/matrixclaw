package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/setup"
)

// providerForm is an open provider form: the provider as saved (or a new
// custom provider's type) and the edits not saved yet.
type providerForm struct {
	Provider setup.ProviderSetupItem
	Update   setup.ProviderSetupUpdate
}

// formStore keeps open provider forms by a random id, so commands carry the
// id and never the form's values, the API key among them.
type formStore struct {
	mu    sync.Mutex
	forms map[string]storedProviderForm
	ttl   time.Duration
	limit int
	now   func() time.Time
}

type storedProviderForm struct {
	form    providerForm
	expires time.Time
}

var providerForms = &formStore{forms: map[string]storedProviderForm{}, ttl: 30 * time.Minute, limit: 64, now: time.Now}

func (s *formStore) open(form providerForm) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	for len(s.forms) >= s.limit {
		oldest := ""
		for id, entry := range s.forms {
			if oldest == "" || entry.expires.Before(s.forms[oldest].expires) {
				oldest = id
			}
		}
		delete(s.forms, oldest)
	}
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	id := hex.EncodeToString(raw[:])
	s.forms[id] = storedProviderForm{form: form, expires: s.now().Add(s.ttl)}
	return id
}

func (s *formStore) get(id string) (providerForm, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	entry, ok := s.forms[id]
	if !ok {
		return providerForm{}, false
	}
	entry.expires = s.now().Add(s.ttl)
	s.forms[id] = entry
	return entry.form, true
}

func (s *formStore) put(id string, form providerForm) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.forms[id]; ok {
		s.forms[id] = storedProviderForm{form: form, expires: s.now().Add(s.ttl)}
	}
}

func (s *formStore) close(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.forms, id)
}

func (s *formStore) pruneLocked() {
	now := s.now()
	for id, entry := range s.forms {
		if !now.Before(entry.expires) {
			delete(s.forms, id)
		}
	}
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
	return pendingOr(f.Update.Model, firstNonEmptyTrimmed(f.Provider.Model, f.Provider.DefaultModel))
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

func (f providerForm) modelPicker() bool {
	return !f.custom() && f.capabilities().ProviderCapabilities.ModelDiscovery
}

func (f providerForm) modelNeedsKey() bool {
	policy := f.policy()
	return f.modelPicker() && policy.RequiresAPIKey && !policy.PublicModelCatalog && !f.hasKey()
}

func (f providerForm) title() string {
	if f.Provider.ID != "" {
		return "Edit " + firstNonEmptyTrimmed(f.Provider.Name, f.Provider.ID)
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
	disabled bool
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
		fields = append(fields, providerFormField{id: "key", label: "API key", value: key, required: true})
	}
	model := providerFormField{id: "model", label: "Model", value: f.model(), required: true}
	if f.modelNeedsKey() {
		model.value, model.disabled = "API key required", true
	}
	fields = append(fields, model)
	if capabilities.ReasoningEffort {
		fields = append(fields, providerFormField{id: "reasoning", label: "Reasoning effort", value: f.reasoningEffort()})
	}
	if capabilities.ToolCalling {
		fields = append(fields, providerFormField{id: "tools", label: "Tool use", value: toolUseLabel(f.toolUseMode())})
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

func toolUseLabel(mode providers.ToolUseMode) string {
	if providers.NormalizeToolUseMode(mode) == providers.ToolUseDisabled {
		return "Disabled"
	}
	return "Enabled"
}

func providerFormCommand(formID string, parts ...string) string {
	return providerCommand(append([]string{"form", formID}, parts...)...)
}

func openProviderForm(form providerForm) Result {
	formID := providerForms.open(form)
	return providerFormResult(formID, form, "")
}

func providerFormResult(formID string, form providerForm, message string) Result {
	fields := make([]FormField, 0, 7)
	if form.policy().AuthMode == providers.ProviderAuthOAuth {
		fields = append(fields, FormField{
			ID:          "auth",
			Label:       "Authorization",
			Value:       openAICodexAuthInfo(),
			EditCommand: providerCommand("auth", providerEncodedID(form.Provider.ID)),
		})
	}
	for _, field := range form.fields() {
		value := field.value
		if field.required && strings.TrimSpace(value) == "" {
			value = "Required"
		}
		edit := ""
		if !field.disabled {
			edit = providerFormCommand(formID, "field", field.id)
		}
		fields = append(fields, FormField{ID: field.id, Label: field.label, Value: value, EditCommand: edit, Disabled: field.disabled})
	}
	return Result{
		Handled: true,
		Form: &FormData{
			Title:         form.title(),
			Fields:        fields,
			SubmitLabel:   "Save",
			CancelLabel:   "Close",
			SubmitCommand: providerFormCommand(formID, "save"),
			CancelCommand: providerCommand(),
			Error:         strings.TrimSpace(message),
		},
	}
}

func (d *Dispatcher) handleProviderForm(ctx context.Context, session *core.Session, args string) (Result, error) {
	formID, rest := firstCommandToken(args)
	form, ok := providerForms.get(formID)
	if !ok {
		return Result{Handled: true, Text: "This provider form has expired. Open it again with /provider."}, nil
	}
	step, rest := firstCommandStep(rest)
	switch step {
	case "field":
		return d.providerFormField(ctx, formID, form, firstField(rest))
	case "set":
		field, value := firstCommandStep(rest)
		form.set(field, value)
		providerForms.put(formID, form)
	case "save":
		return d.saveProviderForm(ctx, session, formID, form)
	}
	return providerFormResult(formID, form, ""), nil
}

func (d *Dispatcher) providerFormField(ctx context.Context, formID string, form providerForm, field string) (Result, error) {
	back := providerFormCommand(formID)
	set := func(value string) string { return providerFormCommand(formID, "set", field, value) }
	picker := func(title string, items []PickerItem) Result {
		return Result{Handled: true, Picker: NewPickerData(PickerProviderCustom, form.title()+": "+title).Select(back).Items(items...).Ptr()}
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
		return picker("endpoint", items), nil
	case "model":
		if form.modelNeedsKey() {
			return providerFormResult(formID, form, "Enter an API key before loading models."), nil
		}
		if form.modelPicker() {
			return d.providerModelPicker(ctx, formID, form)
		}
	case "reasoning":
		efforts := form.capabilities().ReasoningEfforts
		items := make([]PickerItem, 0, len(efforts))
		for _, effort := range efforts {
			items = append(items, PickerItem{ID: effort, Title: cases.Title(language.Und).String(effort), Selected: effort == form.reasoningEffort(), Command: set(effort)})
		}
		return picker("reasoning effort", items), nil
	case "tools":
		current := providers.NormalizeToolUseMode(form.toolUseMode())
		items := make([]PickerItem, 0, 2)
		for _, mode := range []providers.ToolUseMode{providers.ToolUseNative, providers.ToolUseDisabled} {
			items = append(items, PickerItem{ID: string(mode), Title: toolUseLabel(mode), Focused: mode == current, Command: set(string(mode))})
		}
		return picker("tool mode", items), nil
	}
	return providerFieldPrompt(formID, form, field, ""), nil
}

func providerFieldPrompt(formID string, form providerForm, field string, placeholder string) Result {
	title, value := field, ""
	switch field {
	case "name":
		title, value, placeholder = "provider name", form.name(), firstNonEmptyTrimmed(placeholder, "Local AI")
	case "base":
		title, value, placeholder = "base URL", form.baseURL(), firstNonEmptyTrimmed(placeholder, "https://api.example.com/v1")
	case "model":
		value, placeholder = form.model(), firstNonEmptyTrimmed(placeholder, "model-id")
	case "key":
		title, placeholder = "API key", "API key"
		if form.hasKey() {
			placeholder = "leave empty to keep"
		}
	default:
		return providerFormResult(formID, form, "")
	}
	return Result{
		Handled: true,
		Prompt: &PromptData{
			Title:               form.title() + ": " + title,
			Placeholder:         placeholder,
			Value:               value,
			SubmitCommandPrefix: providerFormCommand(formID, "set", field) + " ",
			CancelCommand:       providerFormCommand(formID),
			Sensitive:           field == "key",
		},
	}
}

func (d *Dispatcher) providerModelPicker(ctx context.Context, formID string, form providerForm) (Result, error) {
	response, err := d.daemon.ProviderModelCatalog(ctx, form.Provider.ID, form.Update)
	if err != nil {
		return providerFormResult(formID, form, "Could not load remote models: "+err.Error()), nil
	}
	if response.Status != setup.ProviderModelStatusOK {
		if response.ManualInput {
			return providerFieldPrompt(formID, form, "model", strings.TrimRight(response.Message, ".!?")+". Enter the model manually."), nil
		}
		return providerFormResult(formID, form, response.Message), nil
	}
	items := make([]PickerItem, 0, len(response.Models))
	for _, modelID := range response.Models {
		items = append(items, PickerItem{ID: modelID, Title: modelID, Selected: modelID == form.model(), Command: providerFormCommand(formID, "set", "model", modelID)})
	}
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerProviderCustom, "Model").Select(providerFormCommand(formID)).Items(items...).Ptr(),
	}, nil
}

// saveProviderForm saves the edits; the form closes once the daemon has them,
// and stays open with the error otherwise.
func (d *Dispatcher) saveProviderForm(ctx context.Context, session *core.Session, formID string, form providerForm) (Result, error) {
	if message := form.missing(); message != "" {
		return providerFormResult(formID, form, message), nil
	}
	providerID := form.Provider.ID
	if providerID == "" {
		providerID = customProviderID(form.name())
	}
	update := form.Update
	update.Active = !form.Provider.Configured
	configured, err := d.daemon.ConfigureSetupProvider(ctx, providerID, update)
	if err != nil {
		return providerFormResult(formID, form, err.Error()), nil
	}
	providerForms.close(formID)
	return d.providerSaved(ctx, session, configured)
}

func (d *Dispatcher) providerSaved(ctx context.Context, session *core.Session, configured setup.ProviderSetupItem) (Result, error) {
	selectedSession := session
	if session != nil {
		updated, err := d.daemon.UpdateSessionProvider(ctx, session.ID, configured.ID)
		if err != nil {
			return Result{}, err
		}
		selectedSession = &updated
	}
	list, err := d.daemon.ListSetupProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled:        true,
		Picker:         NewPickerData(PickerProvider, "Provider").Command(providerCommand()).Items(providerPickerItems(list, selectedSession, d.owner())...).Ptr(),
		ReloadSnapshot: true,
	}, nil
}

func (d *Dispatcher) handleProviderEdit(ctx context.Context, args string) (Result, error) {
	provider, err := d.setupProvider(ctx, firstField(args))
	if err != nil {
		return Result{}, err
	}
	return openProviderForm(providerForm{Provider: provider}), nil
}

func openCustomProviderForm(kind string) Result {
	var providerType string
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai":
		providerType = providers.TypeOpenAICompat
	case "anthropic":
		providerType = providers.TypeAnthropic
	default:
		return customProviderTypePicker()
	}
	return openProviderForm(providerForm{
		Provider: setup.ProviderSetupItem{Type: providerType},
		Update:   setup.ProviderSetupUpdate{Type: &providerType},
	})
}
