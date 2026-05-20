package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

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
	case "install-runtime":
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: action}); err != nil {
			return Result{}, err
		}
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
	case "delete-runtime":
		if voiceRuntimeState(provider) == "running" {
			return Result{Handled: true, Text: "Stop the local runtime before deleting Piper runtime."}, nil
		}
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Delete Piper runtime?",
			ConfirmLabel:   "Delete",
			CancelLabel:    "Cancel",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, "delete-runtime-confirm"),
			CancelCommand:  voiceModuleCommand(module.ID, "provider", provider.ID),
			ConfirmDanger:  true,
		}}, nil
	case "delete-runtime-confirm":
		if _, err := d.voiceModules.VoiceProviderAction(ctx, module.ID, provider.ID, setup.VoiceProviderActionRequest{Action: "delete-runtime"}); err != nil {
			return Result{}, err
		}
		return d.voiceLocalProviderPicker(ctx, module.ID, provider.ID)
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
