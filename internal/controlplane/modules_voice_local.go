package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voiceLocalProviderModelPicker(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	languageFilter := firstField(rest)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	field := "model"
	current := provider.Config.ModelID
	title := "Model"
	if module.ID == setup.VoiceModuleTTS {
		field = "voice"
		current = provider.Config.VoiceID
		languageCode := firstNonEmptyTrimmed(languageFilter, ttsLanguageCode(provider, provider.Config))
		title = voiceLanguageTitleForProvider(provider, languageCode) + " voices"
		if provider.ID == "supertonic" {
			title = "Voice Style"
		}
	}
	picker := NewPickerData(PickerVoiceProvider, title).
		Context(module.ID).
		Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
	models := provider.Models
	if module.ID == setup.VoiceModuleTTS {
		if provider.ID != "supertonic" {
			models = voiceModelsForLanguage(provider.Models, firstNonEmptyTrimmed(languageFilter, ttsLanguageCode(provider, provider.Config)))
			picker.Back(voiceModuleCommand(module.ID, "provider-language", provider.ID))
		}
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
		if module.ID == setup.VoiceModuleTTS && provider.ID == "supertonic" {
			if strings.EqualFold(model.ID, current) {
				item.Info = "Active"
			} else {
				item.Info = ""
			}
			item.Command = voiceModuleCommand(module.ID, "provider-set-local", provider.ID, field, model.ID)
		} else if module.ID == setup.VoiceModuleTTS {
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
		if provider.ID == "supertonic" {
			picker := NewPickerData(PickerVoiceProvider, "Language").
				Context(module.ID).
				Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
			current = normalizeSupertonicLanguageCode(provider.Config.Language)
			for _, option := range supertonicLanguageOptions() {
				picker.Item(PickerItem{ID: option.id, Title: option.title, Selected: option.id == current, Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "language", option.id)})
			}
			return Result{Handled: true, Picker: picker.Ptr()}, nil
		}
		current = ttsLanguageCode(provider, provider.Config)
		picker := NewPickerData(PickerVoiceProvider, "Add Voice").
			Context(module.ID).
			Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
		for _, option := range voiceLanguageOptions(provider.Models) {
			picker.Item(PickerItem{ID: option.id, Title: option.title, Info: option.info, Selected: option.id == current, Command: voiceModuleCommand(module.ID, "provider-model", provider.ID, option.id)})
		}
		return Result{Handled: true, Picker: picker.Ptr()}, nil
	}
	picker := NewPickerData(PickerVoiceProvider, "Language").
		Context(module.ID).
		Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
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
	addTitle := "Add Voice"
	if module.ID == setup.VoiceModuleSTT {
		title = "Model"
		addTitle = "Add Model"
	}
	picker := NewPickerData(PickerVoiceProvider, title).
		Context(module.ID).
		Meta(activeLocalModelSummary(module.ID, provider)).
		Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
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
		if provider.ID == "piper" {
			cfg.Language = voiceLanguageFromVoiceID(model.ID)
		}
	} else {
		cfg.ModelID = model.ID
	}
	if _, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg}); err != nil {
		return Result{}, err
	}
	if voicePersistentProvider(module.ID, provider.ID) && normalizeVoiceRunMode(cfg.RuntimeMode) == voiceRuntimeModeAlways && voiceLocalRuntimeStartReady(module.ID, provider, cfg) {
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
		Back(voiceProviderSettingsBackCommand(module.ID, provider.ID))
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
		Picker: NewPickerData(PickerVoiceEnabled, "Run Mode").
			Context(module.ID).
			Meta(voiceRunModeLabel(provider)).
			Back(voiceProviderSettingsBackCommand(module.ID, provider.ID)).
			Item(PickerItem{ID: "per-task", Title: voiceRunPerTaskTitle(provider), Selected: voiceRunModePerTaskSelected(provider), Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "runtime-mode", voiceRuntimeModePerTask)}).
			Item(PickerItem{ID: "always-running", Title: "Always Running", Selected: voiceRunModeAlways(provider), Disabled: !voicePersistentRuntimeAvailable(provider), Command: voiceModuleCommand(module.ID, "provider-set-local", provider.ID, "runtime-mode", voiceRuntimeModeAlways)}).
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
		if module.ID == setup.VoiceModuleTTS && provider.ID == "piper" {
			cfg.Language = voiceLanguageFromVoiceID(cfg.VoiceID)
		}
	case "model":
		cfg.ModelID = strings.TrimSpace(value)
	case "language":
		if module.ID == setup.VoiceModuleTTS {
			if provider.ID == "supertonic" {
				cfg.Language = normalizeSupertonicLanguageCode(value)
			} else {
				cfg.Language = normalizeVoiceLanguageCode(value)
			}
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
	if voicePersistentProvider(module.ID, provider.ID) && nextRuntimeMode == voiceRuntimeModeAlways && (field == "runtime-mode" || field == "voice" || field == "model") && voiceLocalRuntimeStartReady(module.ID, provider, cfg) {
		if err := d.stopOtherVoiceModuleProviders(ctx, module, provider.ID); err != nil {
			return Result{}, err
		}
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"}); err != nil {
			return Result{}, err
		}
	}
	if module.ID == setup.VoiceModuleTTS && field == "language" && provider.ID != "supertonic" {
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
	if module.ID == setup.VoiceModuleTTS && (provider.ID == "piper" || provider.ID == "supertonic") {
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
	return Result{Handled: true, Info: &InfoData{Title: voiceProviderTitle(module, provider), Rows: rows, CloseCommand: voiceProviderSettingsBackCommand(module.ID, provider.ID)}}, nil
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
	case "install-runtime":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Download " + provider.Name + " engine?",
			ConfirmLabel:   "Download",
			CancelLabel:    "Cancel",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, "install-runtime-confirm"),
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
		}}, nil
	case "install-runtime-confirm":
		updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "install-runtime"})
		if err != nil {
			return Result{}, err
		}
		provider = updated
		if !provider.RuntimeInstalled {
			if refreshed, ok, err := d.waitForVoiceProviderRuntimeInstalled(ctx, module.ID, provider.ID); err != nil {
				return Result{}, err
			} else if ok {
				provider = refreshed
			}
		}
		if module.ID == setup.VoiceModuleTTS && provider.ID == "supertonic" {
			if err := d.ensureSupertonicDefaultVoice(ctx, module, provider); err != nil {
				return Result{}, err
			}
			if refreshed, ok, err := d.waitForVoiceProviderRuntimeInstalled(ctx, module.ID, provider.ID); err != nil {
				return Result{}, err
			} else if ok {
				provider = refreshed
			}
		}
		if module.ID == setup.VoiceModuleTTS && provider.ID == "piper" {
			if _, ok := activeInstalledVoice(provider); !ok {
				return voiceProviderNeedsVoiceResult(module, provider, voiceProviderSettingsBackCommand(module.ID, provider.ID)), nil
			}
		}
		if module.ID == setup.VoiceModuleTTS && provider.ID == "supertonic" && provider.RuntimeInstalled {
			modules, err := d.activateVoiceModuleProviderState(ctx, module, provider, false)
			if err != nil {
				return Result{}, err
			}
			if activated, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
				return d.voiceLocalProviderPickerWithProvider(module, activated)
			}
			return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
		}
		if voicePersistentProvider(module.ID, provider.ID) && normalizeVoiceRunMode(provider.Config.RuntimeMode) == voiceRuntimeModeAlways && voiceLocalRuntimeStartReady(module.ID, provider, provider.Config) {
			updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "start"})
			if err != nil {
				return Result{}, err
			}
			provider = updated
		}
		return d.voiceLocalProviderPickerWithProvider(module, provider)
	case "delete-runtime":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceRuntimeDeleteConfirmMessage(module, provider),
			ConfirmLabel:   "Delete",
			CancelLabel:    "Cancel",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, "delete-runtime-confirm"),
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
			ConfirmDanger:  true,
		}}, nil
	case "delete-runtime-confirm":
		updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "delete-runtime"})
		if err != nil {
			return Result{}, err
		}
		if disabled, err := d.disableVoiceModuleIfActiveProvider(ctx, module, provider); err != nil {
			return Result{}, err
		} else if disabled {
			return d.voiceModulePicker(ctx, module.ID)
		}
		return d.voiceLocalProviderPickerWithProvider(module, updated)
	case "download":
		updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "download", ModelID: modelID})
		if err != nil {
			return Result{}, err
		}
		provider = updated
		if module.ID == setup.VoiceModuleTTS && strings.TrimSpace(modelID) != "" {
			cfg := provider.Config
			cfg.VoiceID = strings.TrimSpace(modelID)
			if provider.ID == "piper" {
				cfg.Language = voiceLanguageFromVoiceID(modelID)
			}
			modules, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
			if err != nil {
				return Result{}, err
			}
			if refreshed, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
				provider = refreshed
			}
			if provider.ID == "piper" && provider.RuntimeInstalled {
				modules, err := d.activateVoiceModuleProviderState(ctx, module, provider, false)
				if err != nil {
					return Result{}, err
				}
				if activated, ok := voiceProviderFromModules(modules, module.ID, provider.ID); ok {
					return d.voiceLocalProviderPickerWithProvider(module, activated)
				}
				return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
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
			if normalizeVoiceRunMode(cfg.RuntimeMode) == voiceRuntimeModeAlways && voiceLocalRuntimeStartReady(module.ID, provider, cfg) {
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
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
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
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
			ConfirmDanger:  action == "stop",
		}}, nil
	case "start-confirm", "stop-confirm", "restart":
		action = strings.TrimSuffix(action, "-confirm")
		updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: action})
		if err != nil {
			return Result{}, err
		}
		return d.voiceLocalProviderPickerWithProvider(module, updated)
	case "delete":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceDeleteConfirmMessage(module, provider, modelID),
			ConfirmLabel:   "Delete",
			CancelLabel:    "Cancel",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, "delete-confirm", modelID),
			CancelCommand:  voiceCancelCommandAfterDelete(module.ID, provider.ID),
			ConfirmDanger:  true,
		}}, nil
	case "delete-confirm":
		updated, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "delete", ModelID: modelID})
		if err != nil {
			return Result{}, err
		}
		if module.ID == setup.VoiceModuleTTS {
			if disabled, err := d.reselectTTSVoiceAfterDelete(ctx, module, updated, modelID); err != nil {
				return Result{}, err
			} else if disabled {
				return d.voiceModulePicker(ctx, module.ID)
			}
			return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
		}
		if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
			if disabled, err := d.reselectSTTModelAfterDelete(ctx, module, updated, modelID); err != nil {
				return Result{}, err
			} else if disabled {
				return d.voiceModulePicker(ctx, module.ID)
			}
			return d.voiceInstalledLocalPicker(ctx, module.ID, provider.ID)
		}
		return Result{Handled: true, Confirm: &ConfirmData{
			Title:          "Deleted",
			Message:        voiceDeletedMessage(module, provider, modelID),
			ConfirmLabel:   confirmLabelAfterDelete(module.ID),
			CancelLabel:    "Close",
			ConfirmCommand: voiceCommandAfterDelete(module.ID, provider.ID),
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
		}}, nil
	default:
		return Result{Handled: true, Text: provider.Name + " local runtime action `" + action + "` is not implemented yet."}, nil
	}
}

func (d *Dispatcher) disableVoiceModuleIfActiveProvider(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) (bool, error) {
	if !module.Enabled || !strings.EqualFold(strings.TrimSpace(module.ProviderID), strings.TrimSpace(provider.ID)) {
		return false, nil
	}
	enabled := false
	_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{Enabled: &enabled})
	return err == nil, err
}

func voiceLocalRuntimeStartReady(moduleID string, provider setup.VoiceProviderOption, cfg setup.VoiceProviderConfig) bool {
	if !provider.RuntimeInstalled {
		return false
	}
	provider.Config = cfg
	switch moduleID {
	case setup.VoiceModuleTTS:
		switch provider.ID {
		case "piper":
			_, ok := activeInstalledVoice(provider)
			return ok
		case "supertonic":
			return true
		}
	case setup.VoiceModuleSTT:
		if provider.ID == "whispercpp" {
			_, ok := activeInstalledModel(provider)
			return ok
		}
	}
	return voiceProviderDownloaded(provider)
}

func (d *Dispatcher) reselectTTSVoiceAfterDelete(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, deletedVoiceID string) (bool, error) {
	deletedVoiceID = strings.TrimSpace(deletedVoiceID)
	if deletedVoiceID == "" || !strings.EqualFold(provider.Config.VoiceID, deletedVoiceID) {
		return false, nil
	}
	cfg := provider.Config
	for _, model := range installedVoiceModels(provider) {
		if strings.EqualFold(model.ID, deletedVoiceID) {
			continue
		}
		cfg.VoiceID = model.ID
		cfg.Language = voiceLanguageFromVoiceID(model.ID)
		_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
		return false, err
	}
	return d.disableVoiceModuleIfActiveProvider(ctx, module, provider)
}

func (d *Dispatcher) ensureSupertonicDefaultVoice(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) error {
	cfg := provider.Config
	if strings.TrimSpace(cfg.VoiceID) == "" {
		cfg.VoiceID = defaultSupertonicVoiceID(provider)
	}
	if strings.TrimSpace(cfg.Language) == "" {
		cfg.Language = "auto"
	}
	if strings.TrimSpace(cfg.RuntimeMode) == "" {
		cfg.RuntimeMode = voiceRuntimeModePerTask
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = "http://127.0.0.1:7788"
	}
	_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
	return err
}

func defaultSupertonicVoiceID(provider setup.VoiceProviderOption) string {
	for _, model := range provider.Models {
		if model.Default && strings.TrimSpace(model.ID) != "" {
			return model.ID
		}
	}
	for _, model := range provider.Models {
		if strings.TrimSpace(model.ID) != "" {
			return model.ID
		}
	}
	return "M1"
}

func (d *Dispatcher) reselectSTTModelAfterDelete(ctx context.Context, module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption, deletedModelID string) (bool, error) {
	deletedModelID = strings.TrimSpace(deletedModelID)
	if deletedModelID == "" || !strings.EqualFold(provider.Config.ModelID, deletedModelID) {
		return false, nil
	}
	cfg := provider.Config
	for _, model := range installedVoiceModels(provider) {
		if strings.EqualFold(model.ID, deletedModelID) {
			continue
		}
		cfg.ModelID = model.ID
		_, err := d.voiceModules.UpdateVoiceModule(ctx, module.ID, setup.VoiceModuleUpdate{ProviderID: provider.ID, ProviderConfig: &cfg})
		return false, err
	}
	return d.disableVoiceModuleIfActiveProvider(ctx, module, provider)
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
