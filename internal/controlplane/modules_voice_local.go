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
		info := voiceModelPickerInfo(module.ID, provider, model)
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
				item.Command = voiceModuleCommand(module.ID, "provider-action", provider.ID, provider.ActionIDs.DownloadModel, model.ID)
			}
		} else if module.ID == setup.VoiceModuleSTT && provider.ID == "whispercpp" {
			if model.Installed {
				item.Command = voiceModuleCommand(module.ID, "provider-use", provider.ID, model.ID)
			} else if !provider.RuntimeInstalled {
				item.Command = voiceModuleCommand(module.ID, "provider-action", provider.ID, provider.ActionIDs.DownloadModelWithRuntime, model.ID)
			} else {
				item.Command = voiceModuleCommand(module.ID, "provider-action", provider.ID, provider.ActionIDs.DownloadModel, model.ID)
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
		Command: voiceModuleCommand(module.ID, "provider-action", provider.ID, provider.ActionIDs.DeleteModel, model.ID),
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
