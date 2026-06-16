package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

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
	return Result{Handled: true, Info: &InfoData{Title: voiceProviderTitle(module, provider), Rows: rows}}, nil
}

func (d *Dispatcher) voiceLocalProviderAction(ctx context.Context, moduleID string, args string) (Result, error) {
	providerID, rest := firstCommandToken(args)
	action, actionRest := firstCommandStep(rest)
	modelID := firstField(actionRest)
	module, provider, ok, err := d.voiceLocalProvider(ctx, moduleID, providerID)
	if err != nil || !ok {
		return Result{}, err
	}
	switch {
	case action == provider.ActionIDs.InstallRuntime:
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceRuntimeInstallConfirmMessage(provider),
			ConfirmLabel:   voiceRuntimeInstallConfirmLabel(provider),
			CancelLabel:    "Close",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, voiceActionConfirmID(provider.ActionIDs.InstallRuntime)),
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
		}}, nil
	case action == voiceActionConfirmID(provider.ActionIDs.InstallRuntime):
		return d.voicePostRuntimeInstallAction(ctx, module, provider)
	case action == provider.ActionIDs.DeleteRuntime:
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceRuntimeDeleteConfirmMessage(module, provider),
			ConfirmLabel:   "Delete",
			CancelLabel:    "Close",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, voiceActionConfirmID(provider.ActionIDs.DeleteRuntime)),
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
			ConfirmDanger:  true,
		}}, nil
	case action == voiceActionConfirmID(provider.ActionIDs.DeleteRuntime):
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
	case action == provider.ActionIDs.DownloadModelWithRuntime:
		return Result{Handled: true, Confirm: &ConfirmData{
			Title:          "Install " + provider.Name,
			Message:        voiceModelInstallWithRuntimeMessage(provider, modelID),
			ConfirmLabel:   "Install",
			CancelLabel:    "Close",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, provider.ActionIDs.DownloadModel, modelID),
			CancelCommand:  voiceModuleCommand(module.ID, "provider-model", provider.ID),
		}}, nil
	case action == provider.ActionIDs.DownloadModel:
		return d.voicePostDownloadAction(ctx, module, provider, modelID)
	case action == provider.ActionIDs.Start, action == provider.ActionIDs.Stop:
		if !voiceProviderDownloaded(provider) {
			return Result{Handled: true, Text: "Choose an installed voice before using runtime action `" + action + "`."}, nil
		}
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceRuntimeConfirmMessage(provider, action),
			ConfirmLabel:   voiceRuntimeConfirmLabel(provider, action),
			CancelLabel:    "Close",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, voiceActionConfirmID(action)),
			CancelCommand:  voiceProviderSettingsBackCommand(module.ID, provider.ID),
			ConfirmDanger:  action == provider.ActionIDs.Stop,
		}}, nil
	case action == voiceActionConfirmID(provider.ActionIDs.Start):
		return d.voicePostRuntimeAction(ctx, module, provider, "start")
	case action == voiceActionConfirmID(provider.ActionIDs.Stop):
		return d.voicePostRuntimeAction(ctx, module, provider, "stop")
	case action == provider.ActionIDs.DeleteModel:
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        voiceDeleteConfirmMessage(module, provider, modelID),
			ConfirmLabel:   "Delete",
			CancelLabel:    "Close",
			ConfirmCommand: voiceModuleCommand(module.ID, "provider-action", provider.ID, voiceActionConfirmID(provider.ActionIDs.DeleteModel), modelID),
			CancelCommand:  voiceCancelCommandAfterDelete(module.ID, provider.ID),
			ConfirmDanger:  true,
		}}, nil
	case action == voiceActionConfirmID(provider.ActionIDs.DeleteModel):
		return d.voicePostDeleteAction(ctx, module, provider, modelID)
	default:
		return Result{Handled: true, Text: provider.Name + " local runtime action `" + action + "` is not implemented yet."}, nil
	}
}

func voiceActionConfirmID(actionID string) string {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return ""
	}
	return actionID + "-confirm"
}
