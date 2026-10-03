package realtime

// AutoLanguage lets the provider detect the speaker's language.
var AutoLanguage = Language{Code: "auto", Name: "Auto"}

// RegionalLanguages are the BCP 47 languages Gemini Live and OpenAI Realtime
// speak; a bare language code selects its listed region.
var RegionalLanguages = []Language{
	AutoLanguage,
	{Code: "ar-EG", Name: "Arabic (Egyptian)", Aliases: []string{"ar"}},
	{Code: "bn-BD", Name: "Bengali (Bangladesh)", Aliases: []string{"bn"}},
	{Code: "nl-NL", Name: "Dutch (Netherlands)", Aliases: []string{"nl"}},
	{Code: "en-IN", Name: "English (India)"},
	{Code: "en-US", Name: "English (US)", Aliases: []string{"en"}},
	{Code: "fr-FR", Name: "French (France)", Aliases: []string{"fr"}},
	{Code: "de-DE", Name: "German (Germany)", Aliases: []string{"de"}},
	{Code: "hi-IN", Name: "Hindi (India)", Aliases: []string{"hi"}},
	{Code: "id-ID", Name: "Indonesian (Indonesia)", Aliases: []string{"id"}},
	{Code: "it-IT", Name: "Italian (Italy)", Aliases: []string{"it"}},
	{Code: "ja-JP", Name: "Japanese (Japan)", Aliases: []string{"ja"}},
	{Code: "ko-KR", Name: "Korean (Korea)", Aliases: []string{"ko"}},
	{Code: "mr-IN", Name: "Marathi (India)", Aliases: []string{"mr"}},
	{Code: "pl-PL", Name: "Polish (Poland)", Aliases: []string{"pl"}},
	{Code: "pt-BR", Name: "Portuguese (Brazil)", Aliases: []string{"pt"}},
	{Code: "ro-RO", Name: "Romanian (Romania)", Aliases: []string{"ro"}},
	{Code: "ru-RU", Name: "Russian (Russia)", Aliases: []string{"ru"}},
	{Code: "es-US", Name: "Spanish (US)", Aliases: []string{"es"}},
	{Code: "ta-IN", Name: "Tamil (India)", Aliases: []string{"ta"}},
	{Code: "te-IN", Name: "Telugu (India)", Aliases: []string{"te"}},
	{Code: "th-TH", Name: "Thai (Thailand)", Aliases: []string{"th"}},
	{Code: "tr-TR", Name: "Turkish (Turkey)", Aliases: []string{"tr"}},
	{Code: "uk-UA", Name: "Ukrainian (Ukraine)", Aliases: []string{"uk"}},
	{Code: "vi-VN", Name: "Vietnamese (Vietnam)", Aliases: []string{"vi"}},
}
