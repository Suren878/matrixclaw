package controlplane

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleModules(ctx context.Context, args string) (Result, error) {
	step, rest := firstCommandStep(args)
	if step == "" {
		return d.modulesPicker(ctx)
	}
	switch step {
	case "agents":
		return d.handleExternalAgents(ctx, rest)
	case "storage":
		return d.handleStorage(ctx, rest)
	case "tts":
		return d.handleVoiceModule(ctx, setup.VoiceModuleTTS, rest)
	case "stt":
		return d.handleVoiceModule(ctx, setup.VoiceModuleSTT, rest)
	default:
		return d.modulesPicker(ctx)
	}
}

func (d *Dispatcher) modulesPicker(ctx context.Context) (Result, error) {
	ttsInfo, sttInfo := "", ""
	if d.voiceModules != nil {
		if module, err := d.voiceModule(ctx, setup.VoiceModuleTTS); err == nil {
			ttsInfo = voiceModuleListInfo(module)
		}
		if module, err := d.voiceModule(ctx, setup.VoiceModuleSTT); err == nil {
			sttInfo = voiceModuleListInfo(module)
		}
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerModules, "Modules").
			HideBack(true).
			Row("agents", "External Agents", "Codex", externalAgentsCommand()).
			Row("tts", "Text to Speech", ttsInfo, textToSpeechCommand()).
			Row("stt", "Speech to Text", sttInfo, speechToTextCommand()).
			Row("storage", "Storage", "Files", storageCommand()).
			CloseItem().
			Ptr(),
	}, nil
}

func voiceModuleListInfo(module setup.VoiceModuleDescriptor) string {
	if !module.Enabled {
		return ""
	}
	if module.ID == setup.VoiceModuleSTT && module.Local {
		for _, provider := range module.Providers {
			if provider.ID != module.ProviderID {
				continue
			}
			if model, ok := activeInstalledModel(provider); ok {
				return voiceModelName(provider, model.ID)
			}
			return firstNonEmptyTrimmed(module.Config.ModelID, module.ProviderName)
		}
	}
	return strings.TrimSpace(module.ProviderName)
}

func (d *Dispatcher) handleVoiceModule(ctx context.Context, moduleID string, args string) (Result, error) {
	if d.voiceModules == nil {
		return unsupportedRuntime("voice modules"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.voiceModulePicker(ctx, moduleID)
	case "enabled":
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	case "set-enabled":
		return d.setVoiceModuleEnabled(ctx, moduleID, rest)
	case "provider":
		if strings.TrimSpace(rest) == "" {
			return d.voiceModuleProviderPicker(ctx, moduleID)
		}
		return d.voiceModuleProviderForm(ctx, moduleID, rest)
	case "provider-form":
		return d.voiceModuleProviderFormFromToken(ctx, moduleID, rest)
	case "provider-field":
		return d.voiceModuleProviderField(ctx, moduleID, rest)
	case "provider-set":
		return d.voiceModuleProviderSet(ctx, moduleID, rest)
	case "provider-save":
		return d.saveVoiceModuleProvider(ctx, moduleID, rest)
	case "provider-model":
		return d.voiceLocalProviderModelPicker(ctx, moduleID, rest)
	case "provider-language":
		return d.voiceLocalProviderLanguagePicker(ctx, moduleID, rest)
	case "provider-installed":
		return d.voiceInstalledLocalPicker(ctx, moduleID, rest)
	case "provider-installed-action":
		return d.voiceInstalledLocalActionPicker(ctx, moduleID, rest)
	case "provider-use":
		return d.useInstalledLocalModel(ctx, moduleID, rest)
	case "provider-threads":
		return d.voiceLocalProviderThreadsPicker(ctx, moduleID, rest)
	case "provider-autostart", "provider-run-mode":
		return d.voiceLocalProviderRunModePicker(ctx, moduleID, rest)
	case "provider-set-local":
		return d.setVoiceLocalProviderConfig(ctx, moduleID, rest)
	case "provider-status":
		return d.voiceLocalProviderStatus(ctx, moduleID, rest)
	case "provider-action":
		return d.voiceLocalProviderAction(ctx, moduleID, rest)
	case "info":
		return d.voiceModuleInfo(ctx, moduleID)
	default:
		return d.voiceModulePicker(ctx, moduleID)
	}
}

func (d *Dispatcher) voiceModulePicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	providerInfo := module.ProviderName
	providerDisabled := !module.Enabled
	if providerDisabled {
		providerInfo = "Turn on " + module.Title + " first"
	}
	picker := NewPickerData(voiceModulePickerKind(module.ID), module.Title).
		Context(module.ID).
		Back(modulesCommand()).
		Row("enabled", module.Title, formatEnabled(module.Enabled), voiceModuleCommand(module.ID, "enabled"))
	picker.Item(PickerItem{
		ID:       "provider",
		Title:    "Provider",
		Info:     providerInfo,
		Command:  voiceModuleCommand(module.ID, "provider"),
		Disabled: providerDisabled,
	})
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceModuleEnabledPicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerVoiceEnabled, module.Title).
			Context(module.ID).
			Meta("Module is " + strings.ToLower(formatEnabled(module.Enabled))).
			Back(voiceModuleCommand(module.ID)).
			Item(PickerItem{ID: "enable", Title: "Turn on", Info: module.Title, Selected: module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "enable")}).
			Item(PickerItem{ID: "disable", Title: "Turn off", Info: module.Title, Selected: !module.Enabled, Command: voiceModuleCommand(module.ID, "set-enabled", "disable")}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) voiceModuleProviderPicker(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if !module.Enabled {
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	}
	picker := NewPickerData(PickerVoiceProvider, module.Title+" Provider").
		Context(module.ID).
		Meta("Currently " + module.ProviderName).
		Back(voiceModuleCommand(module.ID))
	for _, provider := range module.Providers {
		picker.Item(PickerItem{
			ID:       provider.ID,
			Title:    voiceProviderPickerTitle(module, provider),
			Info:     voiceProviderPickerInfo(module, provider),
			Selected: provider.ID == module.ProviderID,
		})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) setVoiceModuleEnabled(ctx context.Context, moduleID string, value string) (Result, error) {
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "enable", "enabled":
		enabled = true
	case "no", "off", "false", "disable", "disabled":
		enabled = false
	default:
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	}
	update := setup.VoiceModuleUpdate{Enabled: &enabled}
	if enabled && moduleID == setup.VoiceModuleTTS {
		module, err := d.voiceModule(ctx, moduleID)
		if err != nil {
			return Result{}, err
		}
		if module.ProviderID == "piper" {
			cfg := module.Config
			cfg.RuntimeMode = voiceRuntimeModePerTask
			update.ProviderID = module.ProviderID
			update.ProviderConfig = &cfg
		}
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, update); err != nil {
		return Result{}, err
	}
	return d.voiceModulePicker(ctx, moduleID)
}

func (d *Dispatcher) voiceModuleProviderForm(ctx context.Context, moduleID string, providerID string) (Result, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return d.voiceModuleProviderPicker(ctx, moduleID)
	}
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if !module.Enabled {
		return d.voiceModuleEnabledPicker(ctx, moduleID)
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, setup.VoiceModuleUpdate{ProviderID: providerID}); err != nil {
		return Result{}, err
	}
	if setupProviderIDForVoiceProvider(providerID) == "" {
		return d.voiceLocalProviderPicker(ctx, moduleID, providerID)
	}
	provider, err := d.voiceSetupProvider(ctx, providerID)
	if err != nil {
		return Result{}, err
	}
	return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, formFromProvider(provider), "")
}

func (d *Dispatcher) voiceLocalProviderPicker(ctx context.Context, moduleID string, providerID string) (Result, error) {
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	if module.ID == setup.VoiceModuleTTS {
		return voiceLocalTTSPicker(module, provider), nil
	}
	if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		return voiceLocalSTTPicker(module, provider), nil
	}
	cfg := provider.Config
	title := voiceProviderTitle(module, provider)
	downloaded := voiceProviderDownloaded(provider)
	runtimeState := voiceRuntimeState(provider)
	runtimeActionsAvailable := voiceRuntimeActionsAvailable(provider)
	downloadTitle, deleteTitle := voiceLocalFileActionTitles(module.ID)
	picker := NewPickerData(PickerVoiceProvider, title).
		Context(module.ID).
		Meta(voiceLocalProviderMeta(module, provider)).
		Back(voiceModuleCommand(module.ID, "provider"))
	if module.ID == setup.VoiceModuleTTS {
		picker.Item(PickerItem{ID: "voice", Title: "Voice / language", Info: voiceLocalModelStatus(provider, cfg.VoiceID), Command: voiceModuleCommand(module.ID, "provider-model", provider.ID), Role: PickerItemRoleAction})
	} else {
		picker.Item(PickerItem{ID: "model", Title: "Model", Info: voiceLocalModelStatus(provider, cfg.ModelID), Command: voiceModuleCommand(module.ID, "provider-model", provider.ID), Role: PickerItemRoleAction})
		picker.Row("language", "Language", voiceLanguageStatus(cfg.Language), voiceModuleCommand(module.ID, "provider-language", provider.ID))
		picker.Row("threads", "Threads", voiceThreadsStatus(cfg.Threads), voiceModuleCommand(module.ID, "provider-threads", provider.ID))
	}
	picker.Item(PickerItem{ID: "files", Title: "Installation", Info: voiceDownloadState(provider), Command: voiceModuleCommand(module.ID, "provider-status", provider.ID), Role: PickerItemRoleAction})
	if downloaded {
		picker.Item(PickerItem{ID: "delete", Title: deleteTitle, Info: deleteActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "delete"), Disabled: runtimeState == "running", Role: PickerItemRoleDanger})
		if voiceRunModeAlways(provider) {
			picker.Item(PickerItem{ID: "runtime", Title: "Runtime manager", Info: voiceRuntimeManagerInfo(provider), Command: voiceModuleCommand(module.ID, "provider-status", provider.ID), Role: PickerItemRoleAction})
			picker.Item(PickerItem{ID: "start", Title: "Start runtime", Info: startActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "start"), Disabled: !runtimeActionsAvailable || runtimeState == "running" || runtimeState == "starting"})
			picker.Item(PickerItem{ID: "stop", Title: "Stop runtime", Info: stopActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "stop"), Disabled: !runtimeActionsAvailable || runtimeState != "running"})
			picker.Item(PickerItem{ID: "restart", Title: "Restart runtime", Info: restartActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "restart"), Disabled: !runtimeActionsAvailable || runtimeState != "running"})
		}
		picker.Item(PickerItem{ID: "run-mode", Title: "Run mode", Info: voiceRunModeLabel(provider), Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID)})
	} else {
		picker.Item(PickerItem{ID: "download", Title: downloadTitle, Info: downloadActionInfo(provider), Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "download")})
	}
	picker.Item(PickerItem{ID: "provider", Title: "Provider", Info: provider.Name, Command: voiceModuleCommand(module.ID, "provider"), Role: PickerItemRoleAction})
	picker.Row("module", module.Title, formatEnabled(module.Enabled), voiceModuleCommand(module.ID, "enabled"))
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func voiceLocalTTSPicker(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) Result {
	installed := installedVoiceModels(provider)
	active, hasActive := activeInstalledVoice(provider)
	voiceInfo := "No voices installed"
	if len(installed) > 0 {
		voiceInfo = "Choose active voice"
	}
	if hasActive {
		voiceInfo = voiceModelName(provider, active.ID)
	}
	runtimeAction := ttsRuntimeAction(provider)
	picker := NewPickerData(PickerVoiceProvider, provider.Name).
		Context(module.ID).
		Meta(strings.Join(nonEmptyStrings("Local text to speech", voiceCatalogInfo(provider), installedVoicesSummary(installed)), " · ")).
		Back(voiceModuleCommand(module.ID, "provider"))
	picker.Item(PickerItem{
		ID:      "add-voice",
		Title:   "Add voice",
		Command: voiceModuleCommand(module.ID, "provider-language", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "voice",
		Title:   "Voice",
		Info:    voiceInfo,
		Command: voiceModuleCommand(module.ID, "provider-installed", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "status",
		Title:   "Status",
		Command: voiceModuleCommand(module.ID, "provider-status", provider.ID),
	})
	if voiceRunModeAlways(provider) {
		picker.Item(PickerItem{
			ID:       "runtime",
			Title:    ttsRuntimeActionTitle(provider),
			Command:  voiceModuleCommand(module.ID, "provider-action", provider.ID, runtimeAction),
			Disabled: !hasActive,
		})
	}
	picker.Item(PickerItem{
		ID:      "run-mode",
		Title:   "Run mode",
		Info:    voiceRunModeLabel(provider),
		Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID),
	})
	return Result{Handled: true, Picker: picker.Ptr()}
}

func voiceLocalSTTPicker(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) Result {
	installed := installedVoiceModels(provider)
	active, hasActive := activeInstalledModel(provider)
	runtimeAction := ttsRuntimeAction(provider)
	modelInfo := "No models installed"
	if len(installed) > 0 {
		modelInfo = "Choose active model"
	}
	if hasActive {
		modelInfo = voiceModelName(provider, active.ID)
	}
	picker := NewPickerData(PickerVoiceProvider, provider.Name).
		Context(module.ID).
		Back(voiceModuleCommand(module.ID, "provider"))
	picker.Item(PickerItem{
		ID:      "add-model",
		Title:   "Add model",
		Command: voiceModuleCommand(module.ID, "provider-model", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "model",
		Title:   "Model",
		Info:    modelInfo,
		Command: voiceModuleCommand(module.ID, "provider-installed", provider.ID),
	})
	picker.Item(PickerItem{
		ID:      "status",
		Title:   "Status",
		Command: voiceModuleCommand(module.ID, "provider-status", provider.ID),
	})
	if voiceRunModeAlways(provider) {
		picker.Item(PickerItem{
			ID:       "runtime",
			Title:    ttsRuntimeActionTitle(provider),
			Command:  voiceModuleCommand(module.ID, "provider-action", provider.ID, runtimeAction),
			Disabled: !hasActive || !voiceRuntimeActionsAvailable(provider),
		})
	}
	picker.Item(PickerItem{
		ID:      "run-mode",
		Title:   "Run mode",
		Info:    voiceRunModeLabel(provider),
		Command: voiceModuleCommand(module.ID, "provider-run-mode", provider.ID),
	})
	picker.Row("language", "Language", voiceLanguageStatus(provider.Config.Language), voiceModuleCommand(module.ID, "provider-language", provider.ID))
	picker.Row("threads", "Threads", voiceThreadsStatus(provider.Config.Threads), voiceModuleCommand(module.ID, "provider-threads", provider.ID))
	return Result{Handled: true, Picker: picker.Ptr()}
}

func (d *Dispatcher) voiceLocalProviderModelPicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	field := "model"
	current := provider.Config.ModelID
	title := "Model"
	if module.ID == setup.VoiceModuleTTS {
		current = provider.Config.VoiceID
		title = voiceLanguageTitleForProvider(provider, ttsLanguageCode(provider, provider.Config)) + " voices"
	}
	picker := NewPickerData(PickerVoiceProvider, title).
		Context(module.ID).
		Back(voiceModuleCommand(module.ID, "provider", provider.ID))
	models := provider.Models
	if module.ID == setup.VoiceModuleTTS {
		models = voiceModelsForLanguage(provider.Models, ttsLanguageCode(provider, provider.Config))
		picker.Back(voiceModuleCommand(module.ID, "provider-language", provider.ID))
	}
	if len(models) == 0 {
		picker.Item(PickerItem{ID: "empty", Title: "No voices found", Info: "Choose another language", Disabled: true})
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	for _, model := range models {
		info := voiceModelPickerInfo(model)
		item := PickerItem{
			ID:       model.ID,
			Title:    firstNonEmptyTrimmed(model.Name, model.ID),
			Info:     info,
			Selected: strings.EqualFold(model.ID, current),
			Command:  voiceModuleCommand(module.ID, "provider-set-local", provider.ID, field, model.ID),
		}
		if module.ID == setup.VoiceModuleTTS {
			if model.Installed {
				item.Command = voiceModuleCommand(module.ID, "provider-use", provider.ID, model.ID)
			} else {
				item.Command = voiceModuleCommand(module.ID, "provider-action", provider.ID, "download", model.ID)
			}
		} else if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
			if model.Installed {
				item.Command = voiceModuleCommand(module.ID, "provider-use", provider.ID, model.ID)
			} else {
				item.Command = voiceModuleCommand(module.ID, "provider-action", provider.ID, "download", model.ID)
			}
		}
		picker.Item(item)
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceLocalProviderLanguagePicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	current := strings.ToLower(strings.TrimSpace(provider.Config.Language))
	if current == "" {
		current = "auto"
	}
	if module.ID == setup.VoiceModuleTTS {
		current = ttsLanguageCode(provider, provider.Config)
		picker := NewPickerData(PickerVoiceProvider, "Add voice").
			Context(module.ID).
			Back(voiceModuleCommand(module.ID, "provider", provider.ID))
		for _, option := range voiceLanguageOptions(provider.Models) {
			picker.Item(PickerItem{ID: option.id, Title: option.title, Info: option.info, Selected: option.id == current, Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "language", option.id)})
		}
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	picker := NewPickerData(PickerVoiceProvider, "Language").
		Context(module.ID).
		Back(voiceModuleCommand(module.ID, "provider", provider.ID))
	for _, option := range whisperLanguageOptions() {
		picker.Item(PickerItem{ID: option.id, Title: option.title, Selected: option.id == current, Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "language", option.id)})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceInstalledLocalPicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	if module.ID != setup.VoiceModuleTTS && module.ID != setup.VoiceModuleSTT {
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
	}
	installed := installedVoiceModels(provider)
	title := "Voice"
	addTitle := "Add voice"
	if module.ID == setup.VoiceModuleSTT {
		title = "Model"
		addTitle = "Add model"
	}
	picker := NewPickerData(PickerVoiceProvider, title).
		Context(module.ID).
		Meta(activeLocalModelSummary(module.ID, provider)).
		Back(voiceModuleCommand(module.ID, "provider", provider.ID))
	if len(installed) == 0 {
		picker.Item(PickerItem{ID: "empty", Title: noInstalledLocalModelTitle(module.ID), Disabled: true})
		picker.Item(PickerItem{ID: "add", Title: addTitle, Command: addLocalModelCommand(module.ID, provider.ID)})
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	activeID := activeLocalModelID(module.ID, provider)
	for _, model := range installed {
		info := strings.TrimSpace(strings.Join(nonEmptyStrings(installedVoiceState(model.ID, activeID), model.Size), " · "))
		picker.Item(PickerItem{
			ID:       model.ID,
			Title:    firstNonEmptyTrimmed(model.Name, model.ID),
			Info:     info,
			Selected: strings.EqualFold(model.ID, activeID),
			Command:  voiceModuleCommand(module.ID, "provider-installed-action", provider.ID, model.ID),
		})
	}
	picker.Item(PickerItem{ID: "add", Title: addTitle, Command: addLocalModelCommand(module.ID, provider.ID)})
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceInstalledLocalActionPicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	modelID := firstField(rest)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	if module.ID != setup.VoiceModuleTTS && module.ID != setup.VoiceModuleSTT {
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
	}
	model, ok := voiceModelByID(provider.Models, modelID)
	if !ok || !model.Installed {
		return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
	}
	active := strings.EqualFold(activeLocalModelID(module.ID, provider), model.ID)
	picker := NewPickerData(PickerVoiceProvider, firstNonEmptyTrimmed(model.Name, model.ID)).
		Context(module.ID).
		Meta(localModelActionMeta(module.ID, provider, model)).
		Back(voiceModuleCommand(module.ID, "provider-installed", provider.ID))
	picker.Item(PickerItem{
		ID:      "use",
		Title:   useLocalModelTitle(module.ID),
		Info:    activeVoiceActionInfo(active),
		Command: voiceModuleCommand(module.ID, "provider-use", provider.ID, model.ID),
	})
	picker.Item(PickerItem{
		ID:      "delete",
		Title:   deleteLocalModelTitle(module.ID),
		Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, "delete", model.ID),
		Role:    PickerItemRoleDanger,
	})
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) useInstalledLocalModel(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	modelID := firstField(rest)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	if module.ID != setup.VoiceModuleTTS && module.ID != setup.VoiceModuleSTT {
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
	}
	model, ok := voiceModelByID(provider.Models, modelID)
	if !ok || !model.Installed {
		return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
	}
	cfg := provider.Config
	if module.ID == setup.VoiceModuleTTS {
		cfg.VoiceID = model.ID
		cfg.Language = voiceLanguageFromVoiceID(model.ID)
	} else {
		cfg.ModelID = model.ID
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg}); err != nil {
		return Result{}, err
	}
	if voicePersistentProvider(module.ID, provider.ID) && normalizeVoiceRunMode(cfg.RuntimeMode) == voiceRuntimeModeAlways {
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
			return Result{}, err
		}
	}
	return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
}

func (d *Dispatcher) voiceLocalProviderThreadsPicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	options := []struct {
		id      string
		title   string
		threads int
	}{{"auto", "Auto", 0}, {"2", "2 threads", 2}, {"4", "4 threads", 4}, {"8", "8 threads", 8}}
	picker := NewPickerData(PickerVoiceProvider, "Threads").
		Context(module.ID).
		Back(voiceModuleCommand(module.ID, "provider", provider.ID))
	for _, option := range options {
		picker.Item(PickerItem{ID: option.id, Title: option.title, Selected: option.threads == provider.Config.Threads, Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "threads", option.id)})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) voiceLocalProviderRunModePicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerVoiceEnabled, "Run mode").
			Context(module.ID).
			Meta(voiceRunModeLabel(provider)).
			Back(voiceModuleCommand(module.ID, "provider", provider.ID)).
			Item(PickerItem{ID: "per-task", Title: "Run per task", Selected: voiceRunModePerTaskSelected(provider), Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "runtime-mode", voiceRuntimeModePerTask)}).
			Item(PickerItem{ID: "always-running", Title: "Always running", Info: voicePersistentRuntimeInfo(provider), Selected: voiceRunModeAlways(provider), Disabled: !voicePersistentRuntimeAvailable(provider), Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "runtime-mode", voiceRuntimeModeAlways)}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) setVoiceLocalProviderConfig(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	field, value := firstCommandStep(rest)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	cfg := provider.Config
	nextRuntimeMode := cfg.RuntimeMode
	switch field {
	case "voice":
		cfg.VoiceID = strings.TrimSpace(value)
		if module.ID == setup.VoiceModuleTTS {
			cfg.Language = voiceLanguageFromVoiceID(cfg.VoiceID)
		}
	case "model":
		cfg.ModelID = strings.TrimSpace(value)
	case "language":
		if module.ID == setup.VoiceModuleTTS {
			cfg.Language = normalizeVoiceLanguageCode(value)
		} else {
			cfg.Language = strings.TrimSpace(value)
		}
	case "threads":
		switch strings.TrimSpace(value) {
		case "2":
			cfg.Threads = 2
		case "4":
			cfg.Threads = 4
		case "8":
			cfg.Threads = 8
		default:
			cfg.Threads = 0
		}
	case "autostart":
		cfg.Autostart = isYes(value)
	case "runtime-mode":
		nextRuntimeMode = normalizeVoiceRunMode(value)
		if nextRuntimeMode == voiceRuntimeModeAlways && !voicePersistentRuntimeAvailable(provider) {
			nextRuntimeMode = voiceRuntimeModePerTask
		}
		if nextRuntimeMode == voiceRuntimeModePerTask && voiceRunModeAlways(provider) {
			if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "stop"}); err != nil {
				return Result{}, err
			}
		}
		cfg.RuntimeMode = nextRuntimeMode
		cfg.Autostart = cfg.RuntimeMode == voiceRuntimeModeAlways
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg}); err != nil {
		return Result{}, err
	}
	if voicePersistentProvider(module.ID, provider.ID) && nextRuntimeMode == voiceRuntimeModeAlways && (field == "runtime-mode" || field == "voice" || field == "model") {
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
			return Result{}, err
		}
	}
	if module.ID == setup.VoiceModuleTTS && field == "language" {
		return d.voiceLocalProviderModelPicker(ctx, module.ID, provider.ID)
	}
	return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
}

func (d *Dispatcher) voiceLocalProviderStatus(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID := firstField(args)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	if module.ID == setup.VoiceModuleTTS && provider.ID == "piper" {
		return voiceLocalTTSStatus(provider), nil
	}
	if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
		return voiceLocalSTTStatus(provider), nil
	}
	cfg := provider.Config
	rows := []InfoRow{
		{Label: "Provider", Value: provider.Name},
		{Label: "Installation", Value: voiceDownloadState(provider)},
		{Label: "Runtime manager", Value: voiceRuntimeManagerInfo(provider)},
		{Label: "Runtime mode", Value: voiceRunModeLabel(provider)},
		{Label: "Binary", Value: firstNonEmptyTrimmed(cfg.BinaryPath, "Not configured")},
	}
	if path := strings.TrimSpace(provider.ModelPath); path != "" {
		label := "Target path"
		if voiceProviderDownloaded(provider) {
			label = "Model path"
			if module.ID == setup.VoiceModuleTTS {
				label = "Voice path"
			}
		}
		rows = append(rows, InfoRow{Label: label, Value: path})
	}
	if detail := strings.TrimSpace(provider.RuntimeDetail); detail != "" {
		rows = append(rows, InfoRow{Label: "Detail", Value: detail})
	}
	if module.ID == setup.VoiceModuleTTS {
		rows = append(rows, InfoRow{Label: "Voice / language", Value: voiceLocalModelStatus(provider, cfg.VoiceID)})
	} else {
		rows = append(rows,
			InfoRow{Label: "Model", Value: voiceLocalModelStatus(provider, cfg.ModelID)},
			InfoRow{Label: "Language", Value: voiceLanguageStatus(cfg.Language)},
			InfoRow{Label: "Threads", Value: voiceThreadsStatus(cfg.Threads)},
			InfoRow{Label: "ffmpeg", Value: "Not checked yet"},
		)
	}
	return Result{Handled: true, Info: &InfoData{Title: voiceProviderTitle(module, provider), Rows: rows, CloseCommand: voiceModuleCommand(module.ID, "provider", provider.ID)}}, nil
}

func (d *Dispatcher) voiceLocalProviderAction(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	action, actionRest := firstCommandStep(rest)
	modelID := firstField(actionRest)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	switch action {
	case "download":
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "download", ModelID: modelID}); err != nil {
			return Result{}, err
		}
		if module.ID == setup.VoiceModuleTTS && strings.TrimSpace(modelID) != "" {
			cfg := provider.Config
			cfg.VoiceID = strings.TrimSpace(modelID)
			cfg.Language = voiceLanguageFromVoiceID(modelID)
			if _, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg}); err != nil {
				return Result{}, err
			}
			if voicePersistentProvider(module.ID, provider.ID) && normalizeVoiceRunMode(cfg.RuntimeMode) == voiceRuntimeModeAlways {
				if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
					return Result{}, err
				}
			}
		}
		if module.ID == setup.VoiceModuleTTS {
			return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
		}
		if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
			cfg := provider.Config
			if strings.TrimSpace(modelID) != "" {
				cfg.ModelID = strings.TrimSpace(modelID)
				if _, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg}); err != nil {
					return Result{}, err
				}
			}
			if normalizeVoiceRunMode(cfg.RuntimeMode) == voiceRuntimeModeAlways {
				if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
					return Result{}, err
				}
			}
			return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
		}
		return Result{Handled: true, Confirm: &ConfirmData{
			Title:          "Installed",
			Message:        voiceDownloadedMessage(module, provider, modelID),
			ConfirmLabel:   confirmLabelAfterDownload(module.ID),
			CancelLabel:    "Close",
			ConfirmCommand: voiceCommandAfterDownload(module.ID, provider.ID),
			CancelCommand:  voiceModuleCommand(module.ID, "provider", provider.ID),
		}}, nil
	case "start", "stop":
		if !voiceProviderDownloaded(provider) {
			return Result{Handled: true, Text: "Choose an installed voice before using runtime action `" + action + "`."}, nil
		}
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceRuntimeConfirmMessage(provider, action),
			ConfirmLabel:   voiceRuntimeConfirmLabel(action),
			CancelLabel:    "Cancel",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, action+"-confirm"),
			CancelCommand:  voiceModuleCommand(module.ID, "provider", provider.ID),
			ConfirmDanger:  action == "stop",
		}}, nil
	case "start-confirm", "stop-confirm", "restart":
		action = strings.TrimSuffix(action, "-confirm")
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: action}); err != nil {
			return Result{}, err
		}
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
	case "delete":
		if voiceRuntimeState(provider) == "running" {
			return Result{Handled: true, Text: "Stop the local runtime before deleting model files."}, nil
		}
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceDeleteConfirmMessage(module, provider, modelID),
			ConfirmLabel:   "Delete",
			CancelLabel:    "Cancel",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, "delete-confirm", modelID),
			CancelCommand:  voiceCancelCommandAfterDelete(module.ID, provider.ID),
			ConfirmDanger:  true,
		}}, nil
	case "delete-confirm":
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "delete", ModelID: modelID}); err != nil {
			return Result{}, err
		}
		if module.ID == setup.VoiceModuleTTS {
			if err := d.reselectTTSVoiceAfterDelete(ctx, module, provider, modelID); err != nil {
				return Result{}, err
			}
			return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
		}
		if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
			if err := d.reselectSTTModelAfterDelete(ctx, module, provider, modelID); err != nil {
				return Result{}, err
			}
			return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
		}
		return Result{Handled: true, Confirm: &ConfirmData{
			Title:          "Deleted",
			Message:        voiceDeletedMessage(module, provider, modelID),
			ConfirmLabel:   confirmLabelAfterDelete(module.ID),
			CancelLabel:    "Close",
			ConfirmCommand: voiceCommandAfterDelete(module.ID, provider.ID),
			CancelCommand:  voiceModuleCommand(module.ID, "provider", provider.ID),
		}}, nil
	default:
		return Result{Handled: true, Text: provider.Name + " local runtime action `" + action + "` is not implemented yet."}, nil
	}
}

func (d *Dispatcher) reselectTTSVoiceAfterDelete(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, deletedVoiceID string) error {
	deletedVoiceID = strings.TrimSpace(deletedVoiceID)
	if deletedVoiceID == "" || !strings.EqualFold(provider.Config.VoiceID, deletedVoiceID) {
		return nil
	}
	cfg := provider.Config
	for _, model := range installedVoiceModels(provider) {
		if strings.EqualFold(model.ID, deletedVoiceID) {
			continue
		}
		cfg.VoiceID = model.ID
		cfg.Language = voiceLanguageFromVoiceID(model.ID)
		_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
		return err
	}
	cfg.VoiceID = deletedVoiceID
	cfg.Language = voiceLanguageFromVoiceID(deletedVoiceID)
	_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
	return err
}

func (d *Dispatcher) reselectSTTModelAfterDelete(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, deletedModelID string) error {
	deletedModelID = strings.TrimSpace(deletedModelID)
	if deletedModelID == "" || !strings.EqualFold(provider.Config.ModelID, deletedModelID) {
		return nil
	}
	cfg := provider.Config
	for _, model := range installedVoiceModels(provider) {
		if strings.EqualFold(model.ID, deletedModelID) {
			continue
		}
		cfg.ModelID = model.ID
		_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
		return err
	}
	cfg.ModelID = deletedModelID
	_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
	return err
}

func (d *Dispatcher) voiceLocalProvider(ctx context.Context, moduleID string, providerID string) (setup.VoiceModuleDescriptor, setup.VoiceProviderOption, bool, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return setup.VoiceModuleDescriptor{}, setup.VoiceProviderOption{}, false, err
	}
	providerID = strings.TrimSpace(providerID)
	for _, provider := range module.Providers {
		if provider.ID == providerID && provider.Local {
			return module, provider, true, nil
		}
	}
	return module, setup.VoiceProviderOption{}, false, nil
}

func (d *Dispatcher) voiceModuleProviderFormFromToken(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, "")
}

func (d *Dispatcher) voiceModuleProviderField(ctx context.Context, moduleID string, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID, rest := firstCommandToken(rest)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	title := voiceProviderFormTitle(moduleID, providerID, provider)
	switch field {
	case "base":
		if providerEditBaseURLUsesPicker(provider, data) {
			token := encodeCustomProviderFormToken(data)
			return customProviderBaseURLPicker(
				title,
				data,
				providerFormCatalogID(provider),
				voiceModuleCommandPrefix(moduleID, "provider-set", "base", providerID, token),
				voiceModuleCommand(moduleID, "provider-form", providerID, token),
			), nil
		}
	case "model":
		if providerEditModelUsesPicker(provider, data) {
			return d.voiceProviderModelPicker(ctx, moduleID, providerID, provider, data), nil
		}
	}
	token := encodeCustomProviderFormToken(data)
	placeholder := ""
	if field == "key" {
		placeholder = "leave empty to keep"
	}
	return customProviderFieldPrompt(
		title,
		field,
		data,
		placeholder,
		voiceModuleCommandPrefix(moduleID, "provider-set", field, providerID, token),
		voiceModuleCommand(moduleID, "provider-form", providerID, token),
	), nil
}

func (d *Dispatcher) voiceModuleProviderSet(ctx context.Context, moduleID string, args string) (Result, error) {
	field, rest := firstCommandStep(args)
	providerID, rest := firstCommandToken(rest)
	token := firstField(rest)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, token)
	if err != nil {
		return Result{}, err
	}
	data = data.WithField(field, strings.TrimSpace(strings.TrimPrefix(rest, token)))
	return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, "")
}

func (d *Dispatcher) saveVoiceModuleProvider(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	provider, data, err := d.voiceProviderFormData(ctx, providerID, rest)
	if err != nil {
		return Result{}, err
	}
	if message := providerEditValidationMessage(provider, data); message != "" {
		return d.voiceProviderFormResult(ctx, moduleID, providerID, provider, data, message)
	}
	if _, err := d.providers.ConfigureSetupProvider(ctx, provider.ID, providerUpdateFromForm(provider, data, false)); err != nil {
		return Result{}, err
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, moduleID, setup.VoiceModuleUpdate{ProviderID: providerID}); err != nil {
		return Result{}, err
	}
	return d.voiceModuleProviderPicker(ctx, moduleID)
}

func (d *Dispatcher) voiceProviderModelPicker(ctx context.Context, moduleID string, providerID string, provider setup.ProviderSetupItem, data setup.ProviderFormState) Result {
	token := encodeCustomProviderFormToken(data)
	models, err := d.providers.ProviderModels(ctx, provider.ID, providerUpdateFromForm(provider, data, false))
	if err != nil {
		return customProviderFieldPrompt(
			voiceProviderFormTitle(moduleID, providerID, provider),
			"model",
			data,
			"Could not load remote models: "+err.Error()+". Enter the model manually.",
			voiceModuleCommandPrefix(moduleID, "provider-set", "model", providerID, token),
			voiceModuleCommand(moduleID, "provider-form", providerID, token),
		)
	}
	current := strings.TrimSpace(data.Model)
	items := make([]PickerItem, 0, len(models))
	for _, modelID := range models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		items = append(items, PickerItem{
			ID:       modelID,
			Title:    modelID,
			Selected: modelID == current,
			Command:  voiceModuleCommand(moduleID, "provider-set", "model", providerID, token, modelID),
		})
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerProviderCustom, "Model").
			Back(voiceModuleCommand(moduleID, "provider-form", providerID, token)).
			Items(items...).
			Ptr(),
	}
}

func (d *Dispatcher) voiceProviderFormData(ctx context.Context, providerID string, token string) (setup.ProviderSetupItem, setup.ProviderFormState, error) {
	provider, err := d.voiceSetupProvider(ctx, providerID)
	if err != nil {
		return setup.ProviderSetupItem{}, setup.ProviderFormState{}, err
	}
	data, err := decodeCustomProviderFormToken(firstField(token))
	if err != nil {
		return setup.ProviderSetupItem{}, setup.ProviderFormState{}, err
	}
	return provider, data, nil
}

func (d *Dispatcher) voiceSetupProvider(ctx context.Context, providerID string) (setup.ProviderSetupItem, error) {
	setupID := setupProviderIDForVoiceProvider(providerID)
	if setupID == "" {
		return setup.ProviderSetupItem{}, nil
	}
	return d.setupProvider(ctx, setupID)
}

func (d *Dispatcher) voiceProviderFormResult(ctx context.Context, moduleID string, providerID string, provider setup.ProviderSetupItem, data setup.ProviderFormState, message string) (Result, error) {
	if strings.TrimSpace(provider.ID) == "" {
		return d.voiceModuleProviderPicker(ctx, moduleID)
	}
	capabilities := providerFormCapabilities(provider)
	result := customProviderFormResult(customProviderFormResultData{
		Title:                  voiceProviderFormTitle(moduleID, providerID, provider),
		Data:                   data.WithDefaultProviderOptions(capabilities),
		ProviderID:             provider.ID,
		CatalogID:              providerFormCatalogID(provider),
		ProviderType:           providerFormType(provider),
		KeyStatus:              editSecretFieldStatus(provider, data.APIKey),
		IncludeIdentity:        false,
		IncludeReasoningEffort: false,
		IncludeToolProfile:     false,
		SubmitCommand: func(token string) string {
			return voiceModuleCommand(moduleID, "provider-save", providerID, token)
		},
		CancelCommand: voiceModuleCommand(moduleID, "provider"),
		EditCommand: func(field string, token string) string {
			return voiceModuleCommand(moduleID, "provider-field", field, providerID, token)
		},
		Error: message,
	})
	if result.Form != nil {
		result.Form.Fields = voiceProviderFormFields(result.Form.Fields)
	}
	return result, nil
}

func voiceProviderFormFields(fields []FormField) []FormField {
	out := make([]FormField, 0, len(fields))
	for _, field := range fields {
		switch field.ID {
		case "key":
			out = append(out, field)
		}
	}
	return out
}

func setupProviderIDForVoiceProvider(providerID string) string {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "grok":
		return "xai"
	default:
		return ""
	}
}

func voiceProviderFormTitle(moduleID string, providerID string, provider setup.ProviderSetupItem) string {
	name := firstNonEmptyTrimmed(provider.Name, provider.ID, providerID)
	switch moduleID {
	case setup.VoiceModuleTTS:
		return name + " Text to Speech"
	case setup.VoiceModuleSTT:
		return name + " Speech to Text"
	default:
		return name
	}
}

func (d *Dispatcher) voiceModuleInfo(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Info: &InfoData{
			Title: module.Title,
			Rows: []InfoRow{
				{Label: "Enabled", Value: formatEnabled(module.Enabled)},
				{Label: "Provider", Value: module.ProviderName},
				{Label: "Local", Value: formatYesNo(module.Local)},
				{Label: "Status", Value: module.Status},
			},
			CloseCommand: voiceModuleCommand(module.ID),
		},
	}, nil
}

func (d *Dispatcher) voiceModule(ctx context.Context, moduleID string) (setup.VoiceModuleDescriptor, error) {
	modules, err := d.voiceModules.VoiceModules(ctx)
	if err != nil {
		return setup.VoiceModuleDescriptor{}, err
	}
	for _, module := range modules {
		if module.ID == moduleID {
			return module, nil
		}
	}
	return setup.VoiceModuleDescriptor{}, nil
}

func voiceModulePickerKind(moduleID string) PickerKind {
	switch moduleID {
	case setup.VoiceModuleTTS:
		return PickerTextToSpeech
	case setup.VoiceModuleSTT:
		return PickerSpeechToText
	default:
		return PickerModules
	}
}

func voiceProviderInfo(provider setup.VoiceProviderOption) string {
	info := provider.Status
	if provider.Local && voiceProviderDownloaded(provider) && provider.ModelPath != "" {
		info += " · " + provider.ModelPath
	} else if provider.Endpoint != "" {
		info += " · " + provider.Endpoint
	}
	return strings.TrimSpace(info)
}

func voiceProviderPickerTitle(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	if module.ID == setup.VoiceModuleTTS && provider.ID == "grok" {
		return "Grok TTS"
	}
	return provider.Name
}

func voiceProviderPickerInfo(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	if provider.ID != module.ProviderID || !module.Enabled {
		return ""
	}
	if provider.Local {
		if provider.ID == "piper" && (voiceRunModePerTaskSelected(provider) || voiceRuntimeState(provider) == "running") {
			if _, ok := activeInstalledVoice(provider); ok {
				return "Active"
			}
		}
		if provider.ID == "whispercpp" {
			if _, ok := activeInstalledModel(provider); ok {
				return "Active"
			}
		}
		return ""
	}
	return "Active"
}

func voiceProviderTitle(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	return module.Title + " · " + provider.Name
}

func voiceLocalProviderMeta(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	parts := nonEmptyStrings(
		"Module "+formatEnabled(module.Enabled),
		"Provider "+firstNonEmptyTrimmed(provider.Name, module.ProviderName),
		"Installation "+strings.ToLower(voiceDownloadState(provider)),
	)
	if voiceProviderDownloaded(provider) {
		parts = append(parts, "Runtime "+strings.ToLower(voiceRuntimeManagerInfo(provider)))
	}
	return strings.Join(parts, " · ")
}

func voiceLocalFileActionTitles(moduleID string) (string, string) {
	if moduleID == setup.VoiceModuleTTS {
		return "Install voice", "Remove voice"
	}
	return "Download model", "Delete model"
}

func voiceProviderInstalled(provider setup.VoiceProviderOption) bool {
	return voiceProviderDownloaded(provider)
}

func voiceProviderDownloaded(provider setup.VoiceProviderOption) bool {
	if provider.Downloaded {
		return true
	}
	status := strings.ToLower(provider.Status)
	if strings.Contains(status, "not downloaded") || strings.Contains(status, "not installed") {
		return false
	}
	return strings.Contains(status, "downloaded") || (strings.Contains(status, "installed") && !strings.Contains(status, "not installed"))
}

func voiceRuntimeState(provider setup.VoiceProviderOption) string {
	state := strings.ToLower(strings.TrimSpace(provider.RuntimeState))
	if state != "" {
		return state
	}
	if !voiceProviderDownloaded(provider) {
		return "unavailable"
	}
	if provider.Local {
		return "not_implemented"
	}
	return "stopped"
}

func voiceDownloadState(provider setup.VoiceProviderOption) string {
	if voiceProviderDownloaded(provider) {
		return "Installed"
	}
	return "Not installed"
}

func voiceRuntimeStateLabel(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running":
		return "Running"
	case "starting":
		return "Starting"
	case "error":
		return "Error"
	case "stopped":
		return "Stopped"
	case "not_implemented", "unsupported":
		return "Not implemented yet"
	default:
		return "Not available"
	}
}

func voiceRuntimeManagerInfo(provider setup.VoiceProviderOption) string {
	state := voiceRuntimeState(provider)
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "not_implemented", "unsupported":
		return "Not implemented yet"
	case "unavailable":
		if voiceProviderDownloaded(provider) {
			return "Not implemented yet"
		}
		return "Not available"
	}
	detail := strings.ToLower(strings.TrimSpace(provider.RuntimeDetail))
	if strings.Contains(detail, "not implemented") || strings.Contains(detail, "not enabled") {
		return "Not implemented yet"
	}
	if strings.Contains(detail, "not available") || strings.Contains(detail, "unavailable") {
		return "Not available"
	}
	return voiceRuntimeStateLabel(state)
}

func voiceRuntimeActionsAvailable(provider setup.VoiceProviderOption) bool {
	return provider.Local && voicePersistentRuntimeAvailable(provider) && voiceProviderDownloaded(provider)
}

const (
	voiceRuntimeModePerTask = "per_task"
	voiceRuntimeModeAlways  = "always_running"
)

func normalizeVoiceRunMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "always_running", "persistent", "server":
		return voiceRuntimeModeAlways
	default:
		return voiceRuntimeModePerTask
	}
}

func voiceRunModeLabel(provider setup.VoiceProviderOption) string {
	if voiceRunModeAlways(provider) {
		return "Always running"
	}
	return "Run per task"
}

func voiceRunModeAlways(provider setup.VoiceProviderOption) bool {
	return normalizeVoiceRunMode(provider.Config.RuntimeMode) == voiceRuntimeModeAlways && voicePersistentRuntimeAvailable(provider)
}

func voiceRunModePerTaskSelected(provider setup.VoiceProviderOption) bool {
	return !voiceRunModeAlways(provider)
}

func voicePersistentRuntimeAvailable(provider setup.VoiceProviderOption) bool {
	return provider.ID == "piper" || provider.ID == "whispercpp"
}

func voicePersistentProvider(moduleID string, providerID string) bool {
	switch moduleID {
	case setup.VoiceModuleTTS:
		return providerID == "piper"
	case setup.VoiceModuleSTT:
		return providerID == "whispercpp"
	default:
		return false
	}
}

func voicePersistentRuntimeInfo(provider setup.VoiceProviderOption) string {
	if voicePersistentRuntimeAvailable(provider) {
		return ""
	}
	return "Not available yet"
}

func downloadActionInfo(provider setup.VoiceProviderOption) string {
	if voiceProviderDownloaded(provider) {
		return "Already installed"
	}
	for _, model := range provider.Models {
		selected := provider.Config.ModelID == model.ID || provider.Config.VoiceID == model.ID
		if selected {
			return strings.TrimSpace(strings.Join(nonEmptyStrings(model.Size, model.RAM), " · "))
		}
	}
	return "Install local files"
}

func deleteActionInfo(provider setup.VoiceProviderOption) string {
	if !voiceProviderDownloaded(provider) {
		return "Not installed"
	}
	if voiceRuntimeState(provider) == "running" {
		return "Stop runtime first"
	}
	return "Remove local files"
}

func startActionInfo(provider setup.VoiceProviderOption) string {
	if !voiceProviderDownloaded(provider) {
		return "Install local files first"
	}
	if !voiceRuntimeActionsAvailable(provider) {
		return "Runtime manager not available"
	}
	if voiceRuntimeState(provider) == "running" {
		return "Already running"
	}
	return "Start local process"
}

func stopActionInfo(provider setup.VoiceProviderOption) string {
	if !voiceRuntimeActionsAvailable(provider) {
		return "Runtime manager not available"
	}
	if voiceRuntimeState(provider) != "running" {
		return "Not running"
	}
	return "Stop local process"
}

func restartActionInfo(provider setup.VoiceProviderOption) string {
	if !voiceRuntimeActionsAvailable(provider) {
		return "Runtime manager not available"
	}
	if voiceRuntimeState(provider) != "running" {
		return "Not running"
	}
	return "Restart local process"
}

func voiceRuntimeActionUnavailableMessage(provider setup.VoiceProviderOption, action string) string {
	if !voiceProviderDownloaded(provider) {
		return "Install local files before using runtime action `" + strings.TrimSpace(action) + "`."
	}
	return provider.Name + " runtime manager is not available yet. Start/stop/restart is not implemented."
}

func voiceRuntimeConfirmMessage(provider setup.VoiceProviderOption, action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "stop":
		return "Stop " + provider.Name + " runtime?"
	default:
		return "Start " + provider.Name + " runtime?"
	}
}

func voiceRuntimeConfirmLabel(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "stop":
		return "Stop"
	default:
		return "Start"
	}
}

func voiceDownloadedMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) string {
	kind := "Model"
	name := voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.ModelID))
	if module.ID == setup.VoiceModuleTTS {
		kind = "Voice"
		name = voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.VoiceID))
	}
	return kind + " `" + name + "` installed. Runtime start/stop is separate from local files."
}

func voiceDeletedMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) string {
	kind := "Model"
	name := voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.ModelID))
	if module.ID == setup.VoiceModuleTTS {
		kind = "Voice"
		name = voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.VoiceID))
	}
	return kind + " `" + name + "` files deleted. The provider can stay selected, but it cannot run until the files are installed again."
}

func confirmLabelAfterDownload(moduleID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return "Voices"
	}
	return "Status"
}

func voiceCommandAfterDownload(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return voiceModuleCommand(moduleID, "provider-installed", providerID)
	}
	return voiceModuleCommand(moduleID, "provider-status", providerID)
}

func confirmLabelAfterDelete(moduleID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return "Voices"
	}
	return "Status"
}

func voiceCommandAfterDelete(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return voiceModuleCommand(moduleID, "provider-installed", providerID)
	}
	return voiceModuleCommand(moduleID, "provider-status", providerID)
}

func voiceCancelCommandAfterDelete(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return voiceModuleCommand(moduleID, "provider-installed", providerID)
	}
	return voiceModuleCommand(moduleID, "provider", providerID)
}

func voiceDeleteConfirmMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, modelID string) string {
	if module.ID == setup.VoiceModuleTTS {
		return "Delete voice `" + voiceModelName(provider, firstNonEmptyTrimmed(modelID, provider.Config.VoiceID)) + "`?"
	}
	return "Delete local model for `" + provider.Name + "`?"
}

func installedVoiceModels(provider setup.VoiceProviderOption) []setup.VoiceModelOption {
	out := make([]setup.VoiceModelOption, 0, len(provider.Models))
	for _, model := range provider.Models {
		if model.Installed {
			out = append(out, model)
		}
	}
	return out
}

func installedVoicesSummary(models []setup.VoiceModelOption) string {
	switch len(models) {
	case 0:
		return "no voices installed"
	case 1:
		return "1 voice installed"
	default:
		return strconv.Itoa(len(models)) + " voices installed"
	}
}

func activeInstalledVoice(provider setup.VoiceProviderOption) (setup.VoiceModelOption, bool) {
	activeID := strings.TrimSpace(provider.Config.VoiceID)
	if activeID == "" {
		return setup.VoiceModelOption{}, false
	}
	for _, model := range provider.Models {
		if model.Installed && strings.EqualFold(model.ID, activeID) {
			return model, true
		}
	}
	return setup.VoiceModelOption{}, false
}

func activeVoiceSummary(provider setup.VoiceProviderOption) string {
	if model, ok := activeInstalledVoice(provider); ok {
		return "Active " + voiceModelName(provider, model.ID)
	}
	if len(installedVoiceModels(provider)) > 0 {
		return "No active installed voice"
	}
	return "No voices installed"
}

func activeInstalledModel(provider setup.VoiceProviderOption) (setup.VoiceModelOption, bool) {
	activeID := strings.TrimSpace(provider.Config.ModelID)
	if activeID == "" {
		return setup.VoiceModelOption{}, false
	}
	for _, model := range provider.Models {
		if model.Installed && strings.EqualFold(model.ID, activeID) {
			return model, true
		}
	}
	return setup.VoiceModelOption{}, false
}

func activeLocalModelSummary(moduleID string, provider setup.VoiceProviderOption) string {
	if moduleID == setup.VoiceModuleTTS {
		return activeVoiceSummary(provider)
	}
	if model, ok := activeInstalledModel(provider); ok {
		return "Active " + voiceModelName(provider, model.ID)
	}
	if len(installedVoiceModels(provider)) > 0 {
		return "No active installed model"
	}
	return "No models installed"
}

func activeLocalModelID(moduleID string, provider setup.VoiceProviderOption) string {
	if moduleID == setup.VoiceModuleTTS {
		return strings.TrimSpace(provider.Config.VoiceID)
	}
	return strings.TrimSpace(provider.Config.ModelID)
}

func noInstalledLocalModelTitle(moduleID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return "No voices installed"
	}
	return "No models installed"
}

func addLocalModelCommand(moduleID string, providerID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return voiceModuleCommand(moduleID, "provider-language", providerID)
	}
	return voiceModuleCommand(moduleID, "provider-model", providerID)
}

func localModelActionMeta(moduleID string, provider setup.VoiceProviderOption, model setup.VoiceModelOption) string {
	parts := nonEmptyStrings(installedVoiceState(model.ID, activeLocalModelID(moduleID, provider)), model.Size)
	if moduleID == setup.VoiceModuleTTS {
		parts = append(parts, voiceLanguageTitleForProvider(provider, model.LanguageCode))
	}
	return strings.TrimSpace(strings.Join(parts, " · "))
}

func useLocalModelTitle(moduleID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return "Use voice"
	}
	return "Use model"
}

func deleteLocalModelTitle(moduleID string) string {
	if moduleID == setup.VoiceModuleTTS {
		return "Delete voice"
	}
	return "Delete model"
}

func installedVoiceState(modelID string, activeID string) string {
	if strings.EqualFold(strings.TrimSpace(modelID), strings.TrimSpace(activeID)) {
		return "Active"
	}
	return "Installed"
}

func activeVoiceActionInfo(active bool) string {
	if active {
		return "Already active"
	}
	return "Make active"
}

func voiceModelByID(models []setup.VoiceModelOption, modelID string) (setup.VoiceModelOption, bool) {
	modelID = strings.TrimSpace(modelID)
	for _, model := range models {
		if strings.EqualFold(model.ID, modelID) {
			return model, true
		}
	}
	return setup.VoiceModelOption{}, false
}

func voiceModelPickerInfo(model setup.VoiceModelOption) string {
	state := "Download"
	if model.Installed {
		state = "Installed"
	}
	return strings.TrimSpace(strings.Join(nonEmptyStrings(state, model.Size), " · "))
}

func ttsRuntimeAction(provider setup.VoiceProviderOption) string {
	if voiceRuntimeState(provider) == "running" {
		return "stop"
	}
	return "start"
}

func ttsRuntimeActionTitle(provider setup.VoiceProviderOption) string {
	if ttsRuntimeAction(provider) == "stop" {
		return "Stop runtime"
	}
	return "Start runtime"
}

func ttsRuntimeActionInfo(provider setup.VoiceProviderOption) string {
	if _, ok := activeInstalledVoice(provider); !ok {
		return "No voice"
	}
	if ttsRuntimeAction(provider) == "stop" {
		return "Running"
	}
	return "Stopped"
}

func voiceLocalTTSStatus(provider setup.VoiceProviderOption) Result {
	model, hasActive := activeInstalledVoice(provider)
	rows := []InfoRow{}
	if hasActive {
		rows = append(rows,
			InfoRow{Label: "Storage", Value: voiceModelStorageStatus(model)},
			InfoRow{Label: "RAM", Value: voiceRuntimeRAMStatus(provider)},
		)
	} else {
		rows = append(rows,
			InfoRow{Label: "Storage", Value: "Not installed"},
			InfoRow{Label: "RAM", Value: "0 B"},
		)
	}
	return Result{Handled: true, Info: &InfoData{Title: "Piper Status", Rows: rows, CloseCommand: voiceModuleCommand(setup.VoiceModuleTTS, "provider", provider.ID)}}
}

func voiceLocalSTTStatus(provider setup.VoiceProviderOption) Result {
	model, hasActive := activeInstalledModel(provider)
	rows := []InfoRow{}
	if hasActive {
		rows = append(rows,
			InfoRow{Label: "Storage", Value: voiceModelStorageStatus(model)},
			InfoRow{Label: "RAM", Value: voiceRuntimeRAMStatus(provider)},
		)
	} else {
		rows = append(rows,
			InfoRow{Label: "Storage", Value: "Not installed"},
			InfoRow{Label: "RAM", Value: "0 B"},
		)
	}
	return Result{Handled: true, Info: &InfoData{Title: "Whisper.cpp Status", Rows: rows, CloseCommand: voiceModuleCommand(setup.VoiceModuleSTT, "provider", provider.ID)}}
}

func voiceModelStorageStatus(model setup.VoiceModelOption) string {
	if bytes := voiceModelStorageBytes(model); bytes > 0 {
		return formatBytes(bytes)
	}
	if strings.TrimSpace(model.Path) == "" {
		return "Not installed"
	}
	return "Unknown"
}

func voiceModelStorageBytes(model setup.VoiceModelOption) uint64 {
	path := strings.TrimSpace(model.Path)
	if path == "" {
		return 0
	}
	var total uint64
	for _, file := range []string{path, path + ".json"} {
		info, err := os.Stat(file)
		if err == nil && !info.IsDir() && info.Size() > 0 {
			total += uint64(info.Size())
		}
	}
	return total
}

func voiceRuntimeRAMStatus(provider setup.VoiceProviderOption) string {
	if provider.RuntimeRSS == 0 {
		return "0 B"
	}
	return formatBytes(provider.RuntimeRSS)
}

func voiceModelName(provider setup.VoiceProviderOption, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	for _, model := range provider.Models {
		if model.ID == modelID {
			return firstNonEmptyTrimmed(model.Name, model.ID, provider.Name)
		}
	}
	return firstNonEmptyTrimmed(modelID, provider.Name)
}

func voiceCatalogInfo(provider setup.VoiceProviderOption) string {
	status := strings.ToLower(strings.TrimSpace(provider.CatalogStatus))
	detail := strings.TrimSpace(provider.CatalogDetail)
	switch status {
	case "online":
		return strings.TrimSpace(strings.Join(nonEmptyStrings("catalog online", detail), " · "))
	case "fallback":
		return strings.TrimSpace(strings.Join(nonEmptyStrings("catalog fallback", detail), " · "))
	default:
		return detail
	}
}

func ttsLanguageCode(provider setup.VoiceProviderOption, cfg setup.VoiceProviderConfig) string {
	if language := normalizeVoiceLanguageCode(cfg.Language); language != "" {
		if len(voiceModelsForLanguage(provider.Models, language)) > 0 {
			return language
		}
	}
	return normalizeVoiceLanguageCode(voiceLanguageFromVoiceID(cfg.VoiceID))
}

func voiceLanguageFromVoiceID(voiceID string) string {
	voiceID = strings.TrimSpace(voiceID)
	if before, _, ok := strings.Cut(voiceID, "-"); ok && before != "" {
		return normalizeVoiceLanguageCode(before)
	}
	return "en_US"
}

func voiceLanguageTitleForProvider(provider setup.VoiceProviderOption, languageCode string) string {
	languageCode = normalizeVoiceLanguageCode(languageCode)
	for _, model := range provider.Models {
		if normalizeVoiceLanguageCode(firstNonEmptyTrimmed(model.LanguageCode, voiceLanguageFromVoiceID(model.ID))) == languageCode {
			return voiceLanguageDisplay(model, languageCode)
		}
	}
	return voiceLanguageFallbackTitle(languageCode)
}

func voiceLanguageDisplay(model setup.VoiceModelOption, languageCode string) string {
	name := firstNonEmptyTrimmed(model.LanguageName, voiceLanguageFallbackTitle(languageCode))
	country := voiceLanguageCountry(model)
	if country != "" && !strings.EqualFold(name, country) {
		return name + " (" + country + ")"
	}
	return name
}

func voiceLanguageCountry(model setup.VoiceModelOption) string {
	parts := strings.Split(model.Description, "·")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func voiceLanguageFallbackTitle(languageCode string) string {
	switch normalizeVoiceLanguageCode(languageCode) {
	case "en_US":
		return "English"
	case "ru_RU":
		return "Russian"
	default:
		return firstNonEmptyTrimmed(languageCode, "English")
	}
}

func voiceLanguageOptions(models []setup.VoiceModelOption) []struct{ id, title, info string } {
	seen := map[string]struct {
		title     string
		count     int
		installed int
	}{}
	for _, model := range models {
		code := normalizeVoiceLanguageCode(firstNonEmptyTrimmed(model.LanguageCode, voiceLanguageFromVoiceID(model.ID)))
		if code == "" {
			continue
		}
		item := seen[code]
		if item.title == "" {
			item.title = voiceLanguageDisplay(model, code)
		}
		item.count++
		if model.Installed {
			item.installed++
		}
		seen[code] = item
	}
	if len(seen) == 0 {
		seen["en_US"] = struct {
			title     string
			count     int
			installed int
		}{title: "English (United States)", count: 1}
		seen["ru_RU"] = struct {
			title     string
			count     int
			installed int
		}{title: "Russian (Russia)", count: 1}
	}
	options := make([]struct{ id, title, info string }, 0, len(seen))
	for id, item := range seen {
		info := voiceCountLabel(item.count)
		if item.installed > 0 {
			info += " · " + strconv.Itoa(item.installed) + " installed"
		}
		options = append(options, struct{ id, title, info string }{id: id, title: item.title, info: info})
	}
	sort.Slice(options, func(i, j int) bool {
		return options[i].title < options[j].title
	})
	return options
}

func voiceCountLabel(count int) string {
	if count == 1 {
		return "1 voice"
	}
	return strconv.Itoa(count) + " voices"
}

func voiceModelsForLanguage(models []setup.VoiceModelOption, languageCode string) []setup.VoiceModelOption {
	languageCode = normalizeVoiceLanguageCode(languageCode)
	if languageCode == "" {
		return models
	}
	out := make([]setup.VoiceModelOption, 0, len(models))
	for _, model := range models {
		modelLanguage := normalizeVoiceLanguageCode(firstNonEmptyTrimmed(model.LanguageCode, voiceLanguageFromVoiceID(model.ID)))
		if modelLanguage == languageCode {
			out = append(out, model)
		}
	}
	return out
}

func defaultVoiceIDForLanguage(provider setup.VoiceProviderOption, languageCode string, current string) string {
	languageCode = normalizeVoiceLanguageCode(languageCode)
	current = strings.TrimSpace(current)
	if current != "" && normalizeVoiceLanguageCode(voiceLanguageFromVoiceID(current)) == languageCode {
		return current
	}
	preferred := preferredPiperVoiceID(languageCode)
	for _, model := range provider.Models {
		if model.ID == preferred {
			return model.ID
		}
	}
	models := voiceModelsForLanguage(provider.Models, languageCode)
	for _, quality := range []string{"medium", "high", "low", "x_low"} {
		for _, model := range models {
			if strings.EqualFold(model.Quality, quality) || strings.HasSuffix(strings.ToLower(model.ID), "-"+quality) {
				return model.ID
			}
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return firstNonEmptyTrimmed(current, "en_US-lessac-medium")
}

func preferredPiperVoiceID(languageCode string) string {
	switch normalizeVoiceLanguageCode(languageCode) {
	case "ru_RU":
		return "ru_RU-ruslan-medium"
	default:
		return "en_US-lessac-medium"
	}
}

func normalizeVoiceLanguageCode(languageCode string) string {
	languageCode = strings.TrimSpace(languageCode)
	switch strings.ToLower(languageCode) {
	case "", "auto":
		return ""
	case "en", "english":
		return "en_US"
	case "ru", "russian":
		return "ru_RU"
	default:
		if before, after, ok := strings.Cut(languageCode, "_"); ok {
			before = strings.ToLower(strings.TrimSpace(before))
			after = strings.ToUpper(strings.TrimSpace(after))
			if before != "" && after != "" {
				return before + "_" + after
			}
		}
		return languageCode
	}
}

func voiceInstallInfo(provider setup.VoiceProviderOption, voiceID string) string {
	for _, model := range provider.Models {
		if model.ID == voiceID {
			return strings.TrimSpace(strings.Join(nonEmptyStrings(model.Name, model.Size), " · "))
		}
	}
	return "Download voice files"
}

func voiceLocalModelStatus(provider setup.VoiceProviderOption, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	for _, model := range provider.Models {
		if model.ID != modelID {
			continue
		}
		parts := nonEmptyStrings(model.Name, model.Size, model.RAM)
		if len(parts) == 0 {
			return model.ID
		}
		return strings.Join(parts, " · ")
	}
	return firstNonEmptyTrimmed(modelID, "Not selected")
}

func voiceLanguageStatus(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "auto":
		return "Auto"
	case "ru":
		return "Russian"
	case "en":
		return "English"
	default:
		for _, option := range whisperLanguageOptions() {
			if option.id == strings.ToLower(strings.TrimSpace(language)) {
				return option.title
			}
		}
		return strings.TrimSpace(language)
	}
}

func whisperLanguageOptions() []struct{ id, title string } {
	return []struct{ id, title string }{
		{"auto", "Auto"},
		{"af", "Afrikaans"},
		{"am", "Amharic"},
		{"ar", "Arabic"},
		{"as", "Assamese"},
		{"az", "Azerbaijani"},
		{"ba", "Bashkir"},
		{"be", "Belarusian"},
		{"bg", "Bulgarian"},
		{"bn", "Bengali"},
		{"bo", "Tibetan"},
		{"br", "Breton"},
		{"bs", "Bosnian"},
		{"ca", "Catalan"},
		{"cs", "Czech"},
		{"cy", "Welsh"},
		{"da", "Danish"},
		{"de", "German"},
		{"el", "Greek"},
		{"en", "English"},
		{"es", "Spanish"},
		{"et", "Estonian"},
		{"eu", "Basque"},
		{"fa", "Persian"},
		{"fi", "Finnish"},
		{"fo", "Faroese"},
		{"fr", "French"},
		{"gl", "Galician"},
		{"gu", "Gujarati"},
		{"ha", "Hausa"},
		{"haw", "Hawaiian"},
		{"he", "Hebrew"},
		{"hi", "Hindi"},
		{"hr", "Croatian"},
		{"ht", "Haitian Creole"},
		{"hu", "Hungarian"},
		{"hy", "Armenian"},
		{"id", "Indonesian"},
		{"is", "Icelandic"},
		{"it", "Italian"},
		{"ja", "Japanese"},
		{"jw", "Javanese"},
		{"ka", "Georgian"},
		{"kk", "Kazakh"},
		{"km", "Khmer"},
		{"kn", "Kannada"},
		{"ko", "Korean"},
		{"la", "Latin"},
		{"lb", "Luxembourgish"},
		{"ln", "Lingala"},
		{"lo", "Lao"},
		{"lt", "Lithuanian"},
		{"lv", "Latvian"},
		{"mg", "Malagasy"},
		{"mi", "Maori"},
		{"mk", "Macedonian"},
		{"ml", "Malayalam"},
		{"mn", "Mongolian"},
		{"mr", "Marathi"},
		{"ms", "Malay"},
		{"mt", "Maltese"},
		{"my", "Myanmar"},
		{"ne", "Nepali"},
		{"nl", "Dutch"},
		{"nn", "Norwegian Nynorsk"},
		{"no", "Norwegian"},
		{"oc", "Occitan"},
		{"pa", "Punjabi"},
		{"pl", "Polish"},
		{"ps", "Pashto"},
		{"pt", "Portuguese"},
		{"ro", "Romanian"},
		{"ru", "Russian"},
		{"sa", "Sanskrit"},
		{"sd", "Sindhi"},
		{"si", "Sinhala"},
		{"sk", "Slovak"},
		{"sl", "Slovenian"},
		{"sn", "Shona"},
		{"so", "Somali"},
		{"sq", "Albanian"},
		{"sr", "Serbian"},
		{"su", "Sundanese"},
		{"sv", "Swedish"},
		{"sw", "Swahili"},
		{"ta", "Tamil"},
		{"te", "Telugu"},
		{"tg", "Tajik"},
		{"th", "Thai"},
		{"tk", "Turkmen"},
		{"tl", "Tagalog"},
		{"tr", "Turkish"},
		{"tt", "Tatar"},
		{"uk", "Ukrainian"},
		{"ur", "Urdu"},
		{"uz", "Uzbek"},
		{"vi", "Vietnamese"},
		{"yi", "Yiddish"},
		{"yo", "Yoruba"},
		{"yue", "Cantonese"},
		{"zh", "Chinese"},
	}
}

func voiceThreadsStatus(threads int) string {
	if threads <= 0 {
		return "Auto"
	}
	return strconv.Itoa(threads) + " threads"
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func formatYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func isYes(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "on", "true", "enable", "enabled":
		return true
	default:
		return false
	}
}

func (d *Dispatcher) handleExternalAgents(ctx context.Context, args string) (Result, error) {
	if d.externalAgents == nil {
		return unsupportedRuntime("external agents"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.externalAgentsPicker(ctx)
	}
	step, rest := firstCommandStep(args)
	if step == "" {
		return d.externalAgentsPicker(ctx)
	}
	switch step {
	case "enable":
		agentID, _ := firstCommandToken(rest)
		if agentID == "" {
			return Result{Handled: true, Text: "Usage: /modules agents enable <agent>"}, nil
		}
		return d.updateExternalAgentEnabled(ctx, agentID, true)
	case "disable":
		agentID, _ := firstCommandToken(rest)
		if agentID == "" {
			return Result{Handled: true, Text: "Usage: /modules agents disable <agent>"}, nil
		}
		return d.updateExternalAgentEnabled(ctx, agentID, false)
	default:
		agentID, agentRest := firstCommandToken(args)
		if agentRest == "" {
			return d.externalAgentPicker(ctx, agentID)
		}
		action, actionRest := firstCommandStep(agentRest)
		switch action {
		case "enabled":
			return d.externalAgentEnabledPicker(ctx, agentID)
		case "set-enabled":
			return d.setExternalAgentEnabled(ctx, agentID, actionRest)
		case "path":
			if actionRest == "" {
				return d.externalAgentPathPrompt(ctx, agentID)
			}
			return d.updateExternalAgentPath(ctx, agentID, actionRest)
		default:
			return d.externalAgentPicker(ctx, agentID)
		}
	}
}

func (d *Dispatcher) externalAgentsPicker(ctx context.Context) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerExternalAgents, "External Agents").Back(modulesCommand())
	for _, agent := range agents {
		picker.Item(PickerItem{
			ID:       agent.ID,
			Title:    externalAgentTitle(agent),
			Info:     externalAgentInfo(agent),
			Selected: agent.Enabled,
		})
	}
	if len(agents) == 0 {
		picker.Item(PickerItem{ID: "empty", Title: "No external agents", Info: "No runtimes registered"})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) externalAgentPicker(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	picker := NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
		Context(agent.ID).
		Meta(externalAgentMeta(agent)).
		Back(externalAgentsCommand())
	addExternalAgentEditableItems(picker, agent)
	if agent.Enabled {
		picker.Action("new", "New Session", "Create session using "+agent.DisplayName)
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) externalAgentEnabledPicker(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerExternalAgentOn, "Enable "+externalAgentTitle(agent)+" module?").
			Context(agent.ID).
			Meta("Currently " + strings.ToLower(formatEnabled(agent.Enabled))).
			Back(externalAgentCommand(agent.ID)).
			Item(PickerItem{ID: "yes", Title: "Yes", Selected: agent.Enabled}).
			Item(PickerItem{ID: "no", Title: "No", Selected: !agent.Enabled}).
			Ptr(),
	}, nil
}

func (d *Dispatcher) setExternalAgentEnabled(ctx context.Context, agentID string, value string) (Result, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enable", "enabled", "on", "yes", "true":
		return d.updateExternalAgentEnabled(ctx, agentID, true)
	case "disable", "disabled", "off", "no", "false":
		return d.updateExternalAgentEnabled(ctx, agentID, false)
	default:
		return d.externalAgentEnabledPicker(ctx, agentID)
	}
}

func (d *Dispatcher) updateExternalAgentEnabled(ctx context.Context, agentID string, enabled bool) (Result, error) {
	agents, err := d.externalAgents.UpdateExternalAgent(ctx, agentID, core.UpdateExternalAgentRequest{Enabled: &enabled})
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent updated."}, nil
	}
	action := "disabled"
	if agent.Enabled {
		action = "enabled"
	}
	return Result{
		Handled: true,
		Text:    agent.DisplayName + " " + action + ".",
		Picker:  externalAgentPickerData(agent),
	}, nil
}

func (d *Dispatcher) externalAgentPathPrompt(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	return Result{Handled: true, Prompt: &PromptData{
		Title:               externalAgentTitle(agent) + " Path",
		Placeholder:         "codex binary path",
		Value:               strings.TrimSpace(agent.Path),
		SubmitCommandPrefix: externalAgentPathCommandPrefix(agent.ID),
		CancelCommand:       externalAgentCommand(agent.ID),
	}}, nil
}

func (d *Dispatcher) updateExternalAgentPath(ctx context.Context, agentID string, path string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	agents, err = d.externalAgents.UpdateExternalAgent(ctx, agent.ID, core.UpdateExternalAgentRequest{Path: strings.TrimSpace(path)})
	if err != nil {
		return Result{}, err
	}
	agent, ok = findExternalAgent(agents, agent.ID)
	if !ok {
		return Result{Handled: true, Text: "External agent path updated."}, nil
	}
	return Result{
		Handled: true,
		Text:    agent.DisplayName + " path updated.",
		Picker:  externalAgentPickerData(agent),
	}, nil
}

func externalAgentPickerData(agent core.ExternalAgentDescriptor) *PickerData {
	picker := NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
		Context(agent.ID).
		Meta(externalAgentMeta(agent)).
		Back(externalAgentsCommand())
	addExternalAgentEditableItems(picker, agent)
	if agent.Enabled {
		picker.Action("new", "New Session", "Create session using "+agent.DisplayName)
	}
	return picker.Ptr()
}

func addExternalAgentEditableItems(picker *PickerBuilder, agent core.ExternalAgentDescriptor) {
	picker.Row("path", "Path", externalAgentPathInfo(agent))
	picker.Row("enabled", "Enabled", formatEnabled(agent.Enabled))
}

func externalAgentMeta(agent core.ExternalAgentDescriptor) string {
	parts := []string{}
	if version := strings.TrimSpace(agent.Version); version != "" {
		parts = append(parts, version)
	}
	if mode := strings.TrimSpace(agent.Mode); mode != "" {
		parts = append(parts, mode)
	}
	if len(parts) == 0 {
		return externalAgentInfo(agent)
	}
	return strings.Join(parts, " · ")
}

func externalAgentPathInfo(agent core.ExternalAgentDescriptor) string {
	if path := strings.TrimSpace(agent.Path); path != "" {
		return path
	}
	if !agent.Installed {
		return firstNonEmptyTrimmed(agent.Detail, "codex binary not found")
	}
	return "Default codex"
}

func findExternalAgent(agents []core.ExternalAgentDescriptor, agentID string) (core.ExternalAgentDescriptor, bool) {
	agentID = strings.ToLower(strings.TrimSpace(agentID))
	for _, agent := range agents {
		if strings.EqualFold(strings.TrimSpace(agent.ID), agentID) {
			return agent, true
		}
		for _, alias := range agent.Aliases {
			if strings.EqualFold(strings.TrimSpace(alias), agentID) {
				return agent, true
			}
		}
	}
	return core.ExternalAgentDescriptor{}, false
}

func externalAgentTitle(agent core.ExternalAgentDescriptor) string {
	if title := strings.TrimSpace(agent.DisplayName); title != "" {
		return title
	}
	return strings.TrimSpace(agent.ID)
}

func externalAgentInfo(agent core.ExternalAgentDescriptor) string {
	switch {
	case !agent.Installed:
		return firstNonEmptyTrimmed(agent.Detail, "Not installed")
	case agent.Enabled:
		return "Enabled · " + firstNonEmptyTrimmed(agent.Version, agent.Mode, "external")
	default:
		return "Installed · disabled" + externalAgentVersionSuffix(agent)
	}
}

func externalAgentVersionSuffix(agent core.ExternalAgentDescriptor) string {
	if version := strings.TrimSpace(agent.Version); version != "" {
		return " · " + version
	}
	return ""
}

func (d *Dispatcher) handleStorage(ctx context.Context, args string) (Result, error) {
	if d.storage == nil {
		return unsupportedRuntime("storage"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.storagePicker(ctx)
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return d.storagePicker(ctx)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	switch strings.ToLower(fields[0]) {
	case "import":
		return d.storageImport(ctx, rest)
	case "temp":
		return d.storageTempPicker(ctx)
	case "temp-file":
		return d.storageTempFilePicker(ctx, rest)
	case "temp-promote":
		return d.storageTempPromote(ctx, rest)
	case "temp-delete":
		return d.storageTempDeleteConfirm(rest), nil
	case "temp-delete-confirm":
		return d.storageTempDelete(ctx, rest)
	case "temp-cleanup":
		return d.storageTempCleanup(ctx)
	case "temp-cleanup-confirm":
		return d.storageTempCleanupConfirmed(ctx)
	case "temp-cleanup-mode":
		return d.storageTempCleanupModePicker(ctx)
	case "temp-toggle":
		return d.storageTempToggle(ctx, rest)
	case "temp-days":
		return d.storageTempDays(ctx, rest)
	case "temp-max":
		return d.storageTempMax(ctx, rest)
	case "files":
		return d.storageFilesPicker(ctx)
	case "file":
		return d.storageFilePicker(ctx, rest)
	case "read":
		return d.storageRead(ctx, rest)
	case "delete":
		return d.storageDeleteConfirm(rest), nil
	case "delete-confirm":
		return d.storageDelete(ctx, rest)
	case "clear":
		return d.storageClearConfirm(), nil
	case "clear-confirm":
		return d.storageClear(ctx)
	default:
		return d.storagePicker(ctx)
	}
}
