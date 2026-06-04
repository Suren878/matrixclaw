package controlplane

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) voiceModuleInfo(ctx context.Context, moduleID string) (Result, error) {
	module, err := d.voiceModule(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	if module.ID == setup.VoiceModuleTTS || module.ID == setup.VoiceModuleSTT {
		return voiceModuleStatus(module), nil
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

func voiceModuleStatus(module setup.VoiceModuleDescriptor) Result {
	rows := []InfoRow{
		{Label: "Active provider", Value: "Disabled"},
		{Label: "Mode", Value: "Disabled"},
		{Label: "Used RAM", Value: "0 B"},
	}
	if module.Enabled {
		if provider, ok := selectedVoiceProvider(module); ok {
			rows = []InfoRow{
				{Label: "Active provider", Value: firstNonEmptyTrimmed(provider.Name, module.ProviderName, provider.ID)},
				{Label: "Mode", Value: voiceRunModeLabel(provider)},
				{Label: "Used RAM", Value: voiceRuntimeRAMStatus(provider)},
			}
		} else {
			rows[0].Value = firstNonEmptyTrimmed(module.ProviderName, module.ProviderID, "Unknown")
			rows[1].Value = "Unknown"
		}
	}
	return Result{Handled: true, Info: &InfoData{Title: module.Title + " Status", Rows: rows, CloseCommand: voiceModuleCommand(module.ID)}}
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

func voiceProviderPickerTitle(provider setup.VoiceProviderOption) string {
	return provider.Name
}

func voiceProviderPickerInfo(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	if provider.ID != module.ProviderID || !module.Enabled {
		return ""
	}
	if provider.Local {
		if provider.ID == "supertonic" {
			if provider.RuntimeInstalled && (voiceRunModePerTaskSelected(provider) || voiceRuntimeState(provider) == "running") {
				return "Active"
			}
			return ""
		}
		if provider.ID == "piper" && (voiceRunModePerTaskSelected(provider) || voiceRuntimeState(provider) == "running") {
			if _, ok := activeInstalledVoice(provider); ok {
				return "Active"
			}
		}
		if provider.ID == "whispercpp" && (voiceRunModePerTaskSelected(provider) || voiceRuntimeState(provider) == "running") {
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
	return "Not Installed"
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
		return "Not running"
	case "not_implemented", "unsupported":
		return "Not implemented yet"
	default:
		return "Not available"
	}
}

func voiceRuntimeManagerInfo(provider setup.VoiceProviderOption) string {
	if voiceRunModePerTaskSelected(provider) {
		return "Run per task"
	}
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

func voiceRunPerTaskTitle(provider setup.VoiceProviderOption) string {
	switch provider.ID {
	case "piper":
		return "Run Per Task (~1.4s)"
	case "supertonic":
		return "Run Per Task (~1.2s)"
	default:
		return "Run Per Task"
	}
}

func voiceRunModeAlways(provider setup.VoiceProviderOption) bool {
	return normalizeVoiceRunMode(provider.Config.RuntimeMode) == voiceRuntimeModeAlways && voicePersistentRuntimeAvailable(provider)
}

func voiceRunModePerTaskSelected(provider setup.VoiceProviderOption) bool {
	return !voiceRunModeAlways(provider)
}

func voicePersistentRuntimeAvailable(provider setup.VoiceProviderOption) bool {
	return provider.ID == "piper" || provider.ID == "whispercpp" || provider.ID == "supertonic"
}

func voicePersistentProvider(moduleID string, providerID string) bool {
	switch moduleID {
	case setup.VoiceModuleTTS:
		return providerID == "piper" || providerID == "supertonic"
	case setup.VoiceModuleSTT:
		return providerID == "whispercpp"
	default:
		return false
	}
}

func persistentRuntimeRAMEstimate(provider setup.VoiceProviderOption) string {
	switch provider.ID {
	case "piper":
		return "≈130 MB RAM"
	case "supertonic":
		return "≈550 MB RAM"
	case "whispercpp":
		return "Model RAM"
	default:
		return ""
	}
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
		return "Not Installed"
	}
	return "Remove local files"
}

func voiceRuntimeInstallAction(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return provider.ActionIDs.DeleteRuntime
	}
	return provider.ActionIDs.InstallRuntime
}

func piperRuntimeActionRole(provider setup.VoiceProviderOption, action string) PickerItemRole {
	if action == provider.ActionIDs.DeleteRuntime {
		return PickerItemRoleDanger
	}
	return PickerItemRoleAction
}

func voiceRuntimeInstallInfo(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return "Installed"
	}
	if provider.ID == "whispercpp" {
		return "Not Installed · Builds Locally"
	}
	return "Not Installed"
}

func voiceRuntimeInstallConfirmMessage(provider setup.VoiceProviderOption) string {
	if provider.ID == "whispercpp" {
		return "Build " + provider.Name + " engine?"
	}
	return "Download " + provider.Name + " engine?"
}

func voiceRuntimeInstallConfirmLabel(provider setup.VoiceProviderOption) string {
	if provider.ID == "whispercpp" {
		return "Build"
	}
	return "Download"
}

func voiceModelInstallWithRuntimeMessage(provider setup.VoiceProviderOption, modelID string) string {
	name := voiceModelName(provider, modelID)
	if provider.ID == "whispercpp" {
		return "Build Whisper.cpp engine and download `" + name + "` model?"
	}
	return "Download `" + name + "`?"
}

func supertonicRuntimeInstallInfo(provider setup.VoiceProviderOption) string {
	if provider.RuntimeInstalled {
		return "Installed"
	}
	return "Not Installed"
}

func voiceRuntimeConfirmMessage(provider setup.VoiceProviderOption, action string) string {
	if strings.TrimSpace(action) == strings.TrimSpace(provider.ActionIDs.Stop) {
		return "Stop " + provider.Name + " runtime?"
	}
	return "Start " + provider.Name + " runtime?"
}

func voiceRuntimeDeleteConfirmMessage(module setup.VoiceModuleDescriptor, provider setup.VoiceProviderOption) string {
	if module.ID == setup.VoiceModuleTTS && provider.ID == "piper" {
		return "Delete Piper engine and installed voices?"
	}
	return "Delete " + provider.Name + " engine?"
}

func voiceRuntimeConfirmLabel(provider setup.VoiceProviderOption, action string) string {
	if strings.TrimSpace(action) == strings.TrimSpace(provider.ActionIDs.Stop) {
		return "Stop"
	}
	return "Start"
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
		if providerID == "supertonic" {
			return voiceModuleCommand(moduleID, "provider-model", providerID)
		}
		return voiceModuleCommand(moduleID, "provider-language", providerID)
	}
	return voiceModuleCommand(moduleID, "provider-model", providerID)
}

func localModelActionMeta(moduleID string, provider setup.VoiceProviderOption, model setup.VoiceModelOption) string {
	parts := nonEmptyStrings(installedVoiceState(model.ID, activeLocalModelID(moduleID, provider)), model.Size)
	if moduleID == setup.VoiceModuleTTS && provider.ID != "supertonic" {
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

func voiceModelPickerInfo(moduleID string, provider setup.VoiceProviderOption, model setup.VoiceModelOption) string {
	state := "Download"
	if model.Installed {
		state = "Installed"
	}
	if moduleID == setup.VoiceModuleSTT && provider.ID == "whispercpp" && !provider.RuntimeInstalled && !model.Installed {
		state = "Download Engine + Model"
	}
	return strings.TrimSpace(strings.Join(nonEmptyStrings(state, model.Size, model.RAM), " · "))
}

func voiceLocalTTSStatus(provider setup.VoiceProviderOption) Result {
	if provider.ID == "supertonic" {
		return voiceLocalSupertonicStatus(provider)
	}
	model, hasActive := activeInstalledVoice(provider)
	rows := []InfoRow{{Label: "Runtime", Value: voiceRuntimeInstallInfo(provider)}}
	if hasActive {
		storage := voiceModelStorageStatus(model)
		if provider.ID == "supertonic" {
			storage = firstNonEmptyTrimmed(model.Size, storage)
		}
		rows = append(rows,
			InfoRow{Label: "Storage", Value: storage},
			InfoRow{Label: "Used RAM", Value: voiceRuntimeRAMStatus(provider)},
		)
	} else {
		rows = append(rows,
			InfoRow{Label: "Storage", Value: "Not Installed"},
			InfoRow{Label: "Used RAM", Value: "0 B"},
		)
	}
	return Result{Handled: true, Info: &InfoData{Title: provider.Name + " Status", Rows: rows, CloseCommand: voiceProviderSettingsBackCommand(setup.VoiceModuleTTS, provider.ID)}}
}

func voiceLocalSupertonicStatus(provider setup.VoiceProviderOption) Result {
	rows := []InfoRow{
		{Label: "Runtime", Value: supertonicRuntimeInstallInfo(provider)},
		{Label: "Model storage", Value: supertonicStorageStatus(provider)},
		{Label: "Used RAM", Value: voiceRuntimeRAMStatus(provider)},
		{Label: "Voice style", Value: voiceModelName(provider, firstNonEmptyTrimmed(provider.Config.VoiceID, "M1"))},
		{Label: "Language", Value: voiceLanguageStatus(provider.Config.Language)},
	}
	if detail := strings.TrimSpace(provider.RuntimeDetail); detail != "" {
		rows = append(rows, InfoRow{Label: "Detail", Value: detail})
	}
	return Result{Handled: true, Info: &InfoData{Title: provider.Name + " Status", Rows: rows, CloseCommand: voiceProviderSettingsBackCommand(setup.VoiceModuleTTS, provider.ID)}}
}

func supertonicStorageStatus(provider setup.VoiceProviderOption) string {
	if !provider.RuntimeInstalled {
		return "Not Installed"
	}
	total := supertonicStorageBytes(provider)
	if total == 0 {
		return "Installed"
	}
	return formatBytes(total)
}

func supertonicStorageBytes(provider setup.VoiceProviderOption) uint64 {
	var total uint64
	if runtimePath := strings.TrimSpace(provider.RuntimePath); runtimePath != "" {
		total += directoryStorageBytes(filepath.Dir(filepath.Dir(runtimePath)))
	}
	if cacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheDir) != "" {
		for _, name := range []string{"supertonic", "supertonic2", "supertonic3"} {
			total += directoryStorageBytes(filepath.Join(cacheDir, name))
		}
		total += directoryStorageBytes(filepath.Join(cacheDir, "huggingface", "hub", "models--Supertone--supertonic-3"))
	}
	return total
}

func directoryStorageBytes(root string) uint64 {
	root = strings.TrimSpace(root)
	if root == "" {
		return 0
	}
	var total uint64
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.Size() > 0 {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

func voiceLocalSTTStatus(provider setup.VoiceProviderOption) Result {
	model, hasActive := activeInstalledModel(provider)
	rows := []InfoRow{}
	if hasActive {
		rows = append(rows,
			InfoRow{Label: "Storage", Value: voiceModelStorageStatus(model)},
			InfoRow{Label: "Used RAM", Value: voiceRuntimeRAMStatus(provider)},
		)
	} else {
		rows = append(rows,
			InfoRow{Label: "Storage", Value: "Not Installed"},
			InfoRow{Label: "Used RAM", Value: "0 B"},
		)
	}
	return Result{Handled: true, Info: &InfoData{Title: "Whisper.cpp Status", Rows: rows, CloseCommand: voiceProviderSettingsBackCommand(setup.VoiceModuleSTT, provider.ID)}}
}

func voiceModelStorageStatus(model setup.VoiceModelOption) string {
	if bytes := voiceModelStorageBytes(model); bytes > 0 {
		return formatBytes(bytes)
	}
	if strings.TrimSpace(model.Path) == "" {
		return "Not Installed"
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

func ttsLanguageCode(provider setup.VoiceProviderOption, cfg setup.VoiceProviderConfig) string {
	if provider.ID == "supertonic" {
		return normalizeSupertonicLanguageCode(cfg.Language)
	}
	if language := normalizeVoiceLanguageCode(cfg.Language); language != "" {
		if len(voiceModelsForLanguage(provider.Models, language)) > 0 {
			return language
		}
	}
	return normalizeVoiceLanguageCode(voiceLanguageFromVoiceID(cfg.VoiceID))
}
