package localruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (r *Runtime) supertonicOneShotTextToSpeech(ctx context.Context, provider setup.VoiceProviderOption, text string) ([]byte, error) {
	binaryPath, err := r.VoiceBinaryPath(provider)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "matrixclaw-supertonic-*.wav")
	if err != nil {
		return nil, err
	}
	outputPath := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(outputPath)
		return nil, err
	}
	defer os.Remove(outputPath)

	voiceID := strings.ToUpper(strings.TrimSpace(provider.Config.VoiceID))
	if voiceID == "" {
		voiceID = "M1"
	}
	args := []string{"tts", normalizeTTSInputText(text), "--voice", voiceID, "-o", outputPath}
	if language := supertonicLanguageArg(provider.Config.Language); language != "" {
		args = append(args, "--lang", language)
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = r.supertonicEnv(provider)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("supertonic failed: %s", message)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("supertonic returned empty audio")
	}
	return content, nil
}

func supertonicLanguageArg(language string) string {
	language = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(language, "_", "-")))
	if language == "" || language == "auto" {
		return ""
	}
	if before, _, ok := strings.Cut(language, "-"); ok {
		language = before
	}
	return language
}

func normalizeTTSInputText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
