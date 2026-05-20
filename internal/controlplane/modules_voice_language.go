package controlplane

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

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
