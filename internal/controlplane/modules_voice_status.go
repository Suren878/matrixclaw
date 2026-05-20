package controlplane

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

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

func piperRuntimeInstallAction(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return "delete-runtime"
	}
	return "install-runtime"
}

func piperRuntimeActionRole(action string) PickerItemRole {
	if action == "delete-runtime" {
		return PickerItemRoleDanger
	}
	return PickerItemRoleAction
}

func voiceRuntimeInstallInfo(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return "Installed"
	}
	return "Not installed"
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
	rows := []InfoRow{{Label: "Runtime", Value: voiceRuntimeInstallInfo(provider)}}
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
