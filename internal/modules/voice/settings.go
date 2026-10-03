package voice

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/modules/localruntime"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// Settings are the provider choice, a page per provider (engine, models or
// voices, language, threads, run mode) and the module's status.
func (m *Module) Settings(context.Context) []modules.Item {
	module := m.descriptor()
	items := []modules.Item{m.providerChoice(module)}
	for _, provider := range module.Providers {
		items = append(items, m.providerPage(provider))
	}
	used := "Disabled"
	facts := []modules.Fact{}
	if provider, ok := providerByID(module, module.ProviderID); ok && module.Enabled {
		used = provider.Name
		facts = append(facts,
			modules.Fact{Key: "mode", Label: "Mode", Value: runModeLabel(provider)},
			modules.Fact{Key: "ram", Label: "Used RAM", Value: formatBytes(provider.RuntimeRSS)})
	}
	return append(items, modules.Item{Key: "status", Kind: modules.ItemInfo, Label: "Status", Display: used, Facts: facts})
}

func (m *Module) providerChoice(module localruntime.VoiceModule) modules.Item {
	choice := modules.Item{Key: "provider", Kind: modules.ItemChoice, Label: strings.ToUpper(m.id) + " Provider", Value: "off",
		Display: "Disabled", Options: []modules.Option{{Value: "off", Label: "Disabled"}}}
	for _, provider := range module.Providers {
		option := modules.Option{Value: provider.ID, Label: provider.Name, Info: ramEstimate(provider)}
		if !provider.RuntimeInstalled {
			option.Confirm = engineQuestion(provider)
		}
		choice.Options = append(choice.Options, option)
		if module.Enabled && provider.ID == module.ProviderID {
			choice.Value, choice.Display = provider.ID, provider.Name+" · "+runModeLabel(provider)
		}
	}
	return choice
}

func (m *Module) providerPage(provider localruntime.VoiceProvider) modules.Item {
	engine := modules.Item{Key: "engine", Kind: modules.ItemAction, Label: "Engine", Display: "Not Installed", Confirm: engineQuestion(provider)}
	if provider.ID == "whispercpp" {
		engine.Display = "Not Installed · Builds Locally"
	}
	if provider.RuntimeInstalled {
		engine.Display, engine.Danger, engine.Confirm = "Installed", true, "Delete the "+provider.Name+" engine?"
	}
	items := []modules.Item{engine}
	switch provider.ID {
	case "supertonic":
		style := modules.Item{Key: "voice", Kind: modules.ItemChoice, Label: "Voice Style", Value: provider.Config.VoiceID}
		for _, model := range provider.Models {
			style.Options = append(style.Options, modules.Option{Value: model.ID, Label: model.Name})
		}
		items = append(items, style, languageChoice(setup.SupertonicLanguages, provider.Config.Language), threadsChoice(provider))
	case "whispercpp":
		items = append(items, m.modelsPage(provider, "models", "Model"), languageChoice(setup.WhisperLanguages, provider.Config.Language), threadsChoice(provider))
	default:
		items = append(items, m.modelsPage(provider, "voices", "Voice"))
	}
	items = append(items,
		modules.Item{Key: "run_mode", Kind: modules.ItemChoice, Label: "Run Mode", Value: provider.Config.RuntimeMode, Options: []modules.Option{
			{Value: "per_task", Label: perTaskLabel(provider)},
			{Value: "always_running", Label: "Always Running"},
		}},
		modules.Item{Key: "status", Kind: modules.ItemInfo, Label: "Status", Display: provider.Status, Facts: providerFacts(provider)},
	)
	display := strings.Join(textutil.NonBlank(installedLabel(provider), ramEstimate(provider)), " · ")
	return modules.Item{Key: provider.ID, Kind: modules.ItemPage, Label: provider.Name, Display: display, Items: items}
}

// modelsPage lists the installed models or voices (use, delete) and the
// catalog to add one from; Piper voices are picked by language first.
func (m *Module) modelsPage(provider localruntime.VoiceProvider, key string, noun string) modules.Item {
	active := m.activeModelID(provider)
	page := modules.Item{Key: key, Kind: modules.ItemPage, Label: noun, Display: "No " + strings.ToLower(noun) + "s installed"}
	add := modules.Item{Key: "add", Kind: modules.ItemChoice, Label: "Add " + noun}
	for _, model := range provider.Models {
		option := modules.Option{Value: model.ID, Label: cmp.Or(model.Name, model.ID), Group: languageGroup(provider, model),
			Info: strings.Join(textutil.NonBlank("Download", model.Size, model.RAM), " · ")}
		switch {
		case model.Installed:
			option.Info = "Installed"
		case !provider.RuntimeInstalled && provider.ID == "whispercpp":
			option.Info = "Download Engine + Model"
			option.Confirm = "Install the " + provider.Name + " engine and the " + option.Label + " model?"
		}
		add.Options = append(add.Options, option)
		if !model.Installed {
			continue
		}
		state, use := "Installed", "Make active"
		if strings.EqualFold(model.ID, active) {
			state, use = "Active", "Already active"
			page.Display = cmp.Or(model.Name, model.ID)
		}
		page.Items = append(page.Items, modules.Item{Key: model.ID, Kind: modules.ItemPage, Label: cmp.Or(model.Name, model.ID),
			Display: strings.Join(textutil.NonBlank(state, model.Size), " · "), Items: []modules.Item{
				{Key: "use", Kind: modules.ItemAction, Label: "Use " + strings.ToLower(noun), Display: use},
				{Key: "delete", Kind: modules.ItemAction, Label: "Delete " + strings.ToLower(noun), Danger: true,
					Confirm: "Delete the " + cmp.Or(model.Name, model.ID) + " " + strings.ToLower(noun) + "?"},
			}})
	}
	page.Items = append(page.Items, add)
	return page
}

// Change chooses the provider, installs or deletes an engine, adds, uses or
// deletes a model or voice, or sets one of a provider's settings.
func (m *Module) Change(ctx context.Context, path []string, value string) (modules.Change, error) {
	module := m.descriptor()
	if len(path) == 1 && path[0] == "provider" {
		return m.chooseProvider(ctx, module, value)
	}
	if len(path) < 2 {
		return modules.Change{}, modules.Unknown(path)
	}
	provider, ok := providerByID(module, path[0])
	if !ok {
		return modules.Change{}, modules.Unknown(path)
	}
	switch {
	case len(path) == 2 && path[1] == "engine":
		return m.engine(ctx, module, provider)
	case len(path) == 3 && path[2] == "add" && (path[1] == "voices" || path[1] == "models"):
		return m.addModel(ctx, provider, value)
	case len(path) == 4 && path[3] == "use":
		return m.useModel(provider, path[2])
	case len(path) == 4 && path[3] == "delete":
		return m.deleteModel(ctx, module, provider, path[2])
	case len(path) == 2:
		return m.setProviderSetting(provider, path[1], value)
	}
	return modules.Change{}, modules.Unknown(path)
}

func (m *Module) chooseProvider(ctx context.Context, module localruntime.VoiceModule, value string) (modules.Change, error) {
	if value == "off" {
		return modules.Change{Config: m.edit("", nil, func(v *setup.VoiceModuleConfig) { v.Enabled = false })}, nil
	}
	provider, ok := providerByID(module, value)
	if !ok {
		return modules.Change{}, fmt.Errorf("%w: unknown provider %q", modules.ErrInvalidSetting, value)
	}
	if !provider.RuntimeInstalled {
		installed, err := m.runtime.ApplyVoiceAction(ctx, m.id, provider, localruntime.VoiceActionRequest{Action: localruntime.ActionInstallRuntime})
		if err != nil {
			return modules.Change{}, err
		}
		provider = installed
	}
	enable := func(v *setup.VoiceModuleConfig) { v.Enabled, v.ProviderID = true, provider.ID }
	if len(provider.Models) == 0 || provider.ID == "supertonic" || m.activeModelInstalled(provider) {
		return modules.Change{Config: m.edit(provider.ID, nil, enable)}, nil
	}
	for _, model := range provider.Models {
		if model.Installed {
			return modules.Change{Config: m.edit(provider.ID, m.selectModel(provider, model.ID), enable)}, nil
		}
	}
	return modules.Change{Open: []string{provider.ID, m.modelsKey(provider), "add"},
		Message: provider.Name + " needs an installed " + m.modelNoun(provider) + " first."}, nil
}

func (m *Module) engine(ctx context.Context, module localruntime.VoiceModule, provider localruntime.VoiceProvider) (modules.Change, error) {
	if provider.RuntimeInstalled {
		if _, err := m.runtime.ApplyVoiceAction(ctx, m.id, provider, localruntime.VoiceActionRequest{Action: localruntime.ActionDeleteRuntime}); err != nil {
			return modules.Change{}, err
		}
		if module.Enabled && module.ProviderID == provider.ID {
			return modules.Change{Config: m.edit("", nil, func(v *setup.VoiceModuleConfig) { v.Enabled = false })}, nil
		}
		return modules.Change{}, nil
	}
	installed, err := m.runtime.ApplyVoiceAction(ctx, m.id, provider, localruntime.VoiceActionRequest{Action: localruntime.ActionInstallRuntime})
	if err != nil {
		return modules.Change{}, err
	}
	switch {
	case installed.ID == "supertonic":
		return modules.Change{Config: m.edit(installed.ID, nil, func(v *setup.VoiceModuleConfig) { v.Enabled, v.ProviderID = true, installed.ID })}, nil
	case len(installed.Models) > 0 && !m.activeModelInstalled(installed):
		return modules.Change{Open: []string{installed.ID, m.modelsKey(installed), "add"},
			Message: installed.Name + " needs an installed " + m.modelNoun(installed) + " first."}, nil
	}
	m.reconcileRuntimes(m.settings())
	return modules.Change{}, nil
}

// addModel downloads a model or voice (and Whisper's engine first), then
// uses it.
func (m *Module) addModel(ctx context.Context, provider localruntime.VoiceProvider, modelID string) (modules.Change, error) {
	if !hasModel(provider, modelID) {
		return modules.Change{}, fmt.Errorf("%w: unknown %s %q", modules.ErrInvalidSetting, m.modelNoun(provider), modelID)
	}
	if provider.ID == "whispercpp" && !provider.RuntimeInstalled {
		installed, err := m.runtime.ApplyVoiceAction(ctx, m.id, provider, localruntime.VoiceActionRequest{Action: localruntime.ActionInstallRuntime})
		if err != nil {
			return modules.Change{}, err
		}
		provider = installed
	}
	downloaded, err := m.runtime.ApplyVoiceAction(ctx, m.id, provider, localruntime.VoiceActionRequest{Action: localruntime.ActionDownload, ModelID: modelID})
	if err != nil {
		return modules.Change{}, err
	}
	var enable func(*setup.VoiceModuleConfig)
	if downloaded.RuntimeInstalled {
		enable = func(v *setup.VoiceModuleConfig) { v.Enabled, v.ProviderID = true, provider.ID }
	}
	return modules.Change{Config: m.edit(provider.ID, m.selectModel(provider, modelID), enable),
		Open: []string{provider.ID, m.modelsKey(provider)}}, nil
}

func (m *Module) useModel(provider localruntime.VoiceProvider, modelID string) (modules.Change, error) {
	if !installedModel(provider, modelID) {
		return modules.Change{}, fmt.Errorf("%w: %s is not installed", modules.ErrInvalidSetting, modelID)
	}
	return modules.Change{Config: m.edit(provider.ID, m.selectModel(provider, modelID), func(v *setup.VoiceModuleConfig) { v.ProviderID = provider.ID }),
		Open: []string{provider.ID, m.modelsKey(provider)}}, nil
}

// deleteModel removes a model or voice; when it was the active one another
// installed one takes over, or the module turns off if it used it.
func (m *Module) deleteModel(ctx context.Context, module localruntime.VoiceModule, provider localruntime.VoiceProvider, modelID string) (modules.Change, error) {
	if !installedModel(provider, modelID) {
		return modules.Change{}, fmt.Errorf("%w: %s is not installed", modules.ErrInvalidSetting, modelID)
	}
	if _, err := m.runtime.ApplyVoiceAction(ctx, m.id, provider, localruntime.VoiceActionRequest{Action: localruntime.ActionDelete, ModelID: modelID}); err != nil {
		return modules.Change{}, err
	}
	open := []string{provider.ID, m.modelsKey(provider)}
	if !strings.EqualFold(m.activeModelID(provider), modelID) {
		return modules.Change{Open: open}, nil
	}
	for _, model := range provider.Models {
		if model.Installed && !strings.EqualFold(model.ID, modelID) {
			return modules.Change{Config: m.edit(provider.ID, m.selectModel(provider, model.ID), nil), Open: open}, nil
		}
	}
	if module.Enabled && module.ProviderID == provider.ID {
		return modules.Change{Config: m.edit("", nil, func(v *setup.VoiceModuleConfig) { v.Enabled = false }), Open: open,
			Message: module.Title + " is off: no " + m.modelNoun(provider) + " is left."}, nil
	}
	return modules.Change{Open: open}, nil
}

func (m *Module) setProviderSetting(provider localruntime.VoiceProvider, key string, value string) (modules.Change, error) {
	var set func(*setup.VoiceProviderConfig)
	switch {
	case key == "voice" && provider.ID == "supertonic" && hasModel(provider, value):
		set = func(c *setup.VoiceProviderConfig) { c.VoiceID = value }
	case key == "language" && provider.ID == "supertonic" && knownLanguage(setup.SupertonicLanguages, value),
		key == "language" && provider.ID == "whispercpp" && knownLanguage(setup.WhisperLanguages, value):
		set = func(c *setup.VoiceProviderConfig) { c.Language = value }
	case key == "threads" && provider.ID != "piper":
		threads := 0
		if value != "auto" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return modules.Change{}, fmt.Errorf("%w: threads must be auto or a number", modules.ErrInvalidSetting)
			}
			threads = parsed
		}
		set = func(c *setup.VoiceProviderConfig) { c.Threads = threads }
	case key == "run_mode" && (value == "per_task" || value == "always_running"):
		set = func(c *setup.VoiceProviderConfig) { c.RuntimeMode, c.Autostart = value, value == "always_running" }
	case key == "voice" || key == "language" || key == "threads" || key == "run_mode":
		return modules.Change{}, fmt.Errorf("%w: %s cannot be %q", modules.ErrInvalidSetting, key, value)
	default:
		return modules.Change{}, modules.Unknown([]string{provider.ID, key})
	}
	return modules.Change{Config: m.edit(provider.ID, set, nil)}, nil
}

// edit changes provider's settings and then the module's in setup.json.
func (m *Module) edit(providerID string, provider func(*setup.VoiceProviderConfig), module func(*setup.VoiceModuleConfig)) func(*setup.Config) error {
	return func(cfg *setup.Config) error {
		target := &cfg.Modules.TextToSpeech
		if m.id == setup.VoiceModuleSTT {
			target = &cfg.Modules.SpeechToText
		}
		if provider != nil {
			current := setup.EffectiveVoiceConfig(m.id, providerID, target.Providers[providerID])
			provider(&current)
			if target.Providers == nil {
				target.Providers = map[string]setup.VoiceProviderConfig{}
			}
			target.Providers[providerID] = current
		}
		if module != nil {
			module(target)
		}
		return nil
	}
}

func (m *Module) selectModel(provider localruntime.VoiceProvider, modelID string) func(*setup.VoiceProviderConfig) {
	return func(c *setup.VoiceProviderConfig) {
		switch {
		case m.id == setup.VoiceModuleSTT:
			c.ModelID = modelID
		case provider.ID == "piper":
			c.VoiceID, c.Language = modelID, localruntime.VoiceLanguageOfVoice(modelID)
		default:
			c.VoiceID = modelID
		}
	}
}

func (m *Module) activeModelID(provider localruntime.VoiceProvider) string {
	if m.id == setup.VoiceModuleSTT {
		return provider.Config.ModelID
	}
	return provider.Config.VoiceID
}

func (m *Module) activeModelInstalled(provider localruntime.VoiceProvider) bool {
	return installedModel(provider, m.activeModelID(provider))
}

func (m *Module) modelsKey(provider localruntime.VoiceProvider) string {
	if provider.ID == "whispercpp" {
		return "models"
	}
	return "voices"
}

func (m *Module) modelNoun(provider localruntime.VoiceProvider) string {
	if provider.ID == "whispercpp" {
		return "model"
	}
	return "voice"
}

func hasModel(provider localruntime.VoiceProvider, modelID string) bool {
	for _, model := range provider.Models {
		if strings.EqualFold(model.ID, modelID) {
			return true
		}
	}
	return false
}

func installedModel(provider localruntime.VoiceProvider, modelID string) bool {
	for _, model := range provider.Models {
		if model.Installed && strings.EqualFold(model.ID, modelID) {
			return true
		}
	}
	return false
}

// languageGroup is the language a Piper voice is listed under.
func languageGroup(provider localruntime.VoiceProvider, model localruntime.VoiceModel) string {
	if provider.ID != "piper" {
		return ""
	}
	name := cmp.Or(model.LanguageName, model.LanguageCode, localruntime.VoiceLanguageOfVoice(model.ID))
	if model.Country != "" && !strings.EqualFold(model.Country, name) {
		return name + " (" + model.Country + ")"
	}
	return name
}

func languageChoice(languages []setup.Language, current string) modules.Item {
	choice := modules.Item{Key: "language", Kind: modules.ItemChoice, Label: "Language", Value: cmp.Or(current, "auto")}
	for _, language := range languages {
		choice.Options = append(choice.Options, modules.Option{Value: language.Code, Label: language.Name})
	}
	return choice
}

func knownLanguage(languages []setup.Language, code string) bool {
	for _, language := range languages {
		if language.Code == code {
			return true
		}
	}
	return false
}

func threadsChoice(provider localruntime.VoiceProvider) modules.Item {
	value := "auto"
	if provider.Config.Threads > 0 {
		value = strconv.Itoa(provider.Config.Threads)
	}
	return modules.Item{Key: "threads", Kind: modules.ItemChoice, Label: "Threads", Value: value, Options: []modules.Option{
		{Value: "auto", Label: "Auto"}, {Value: "2", Label: "2 threads"}, {Value: "4", Label: "4 threads"}, {Value: "8", Label: "8 threads"},
	}}
}

func providerFacts(provider localruntime.VoiceProvider) []modules.Fact {
	facts := []modules.Fact{
		{Key: "engine", Label: "Engine", Value: installedLabel(provider)},
		{Key: "mode", Label: "Mode", Value: runModeLabel(provider)},
		{Key: "ram", Label: "Used RAM", Value: formatBytes(provider.RuntimeRSS)},
	}
	if provider.RuntimeDetail != "" {
		facts = append(facts, modules.Fact{Key: "detail", Label: "Detail", Value: provider.RuntimeDetail})
	}
	return facts
}

func installedLabel(provider localruntime.VoiceProvider) string {
	if provider.RuntimeInstalled {
		return "Installed"
	}
	return "Not installed"
}

func engineQuestion(provider localruntime.VoiceProvider) string {
	if provider.ID == "whispercpp" {
		return "Build the " + provider.Name + " engine?"
	}
	return "Download the " + provider.Name + " engine?"
}

func runModeLabel(provider localruntime.VoiceProvider) string {
	if provider.Config.RuntimeMode == "always_running" {
		return "Always running"
	}
	return "Run per task"
}

func perTaskLabel(provider localruntime.VoiceProvider) string {
	switch provider.ID {
	case "piper":
		return "Run Per Task (~1.4s)"
	case "supertonic":
		return "Run Per Task (~1.2s)"
	default:
		return "Run Per Task"
	}
}

// ramEstimate is what the provider's runtime takes while it runs.
func ramEstimate(provider localruntime.VoiceProvider) string {
	switch provider.ID {
	case "piper":
		return "≈130 MB RAM"
	case "supertonic":
		return "≈550 MB RAM"
	}
	for _, model := range provider.Models {
		if model.ID == provider.Config.ModelID && model.RAM != "" {
			return model.RAM + " RAM"
		}
	}
	return ""
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
