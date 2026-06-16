package controlplane

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

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
	return Result{Handled: true, Info: &InfoData{Title: provider.Name + " Status", Rows: rows}}
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
	return Result{Handled: true, Info: &InfoData{Title: provider.Name + " Status", Rows: rows}}
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
	return Result{Handled: true, Info: &InfoData{Title: "Whisper.cpp Status", Rows: rows}}
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
