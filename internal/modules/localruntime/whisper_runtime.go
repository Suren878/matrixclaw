package localruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

type WhisperSpeechInput struct {
	Content  []byte
	FileName string
	MIMEType string
	Language string
}

func (r *Runtime) WhisperSpeechToText(ctx context.Context, provider setup.VoiceProviderOption, input WhisperSpeechInput) (string, error) {
	if len(input.Content) == 0 {
		return "", errors.New("audio content is required")
	}
	if installed, _ := r.VoiceModelInstalled(setup.VoiceModuleSTT, provider); !installed {
		return "", errors.New("whisper.cpp model is not installed")
	}
	if !voiceProviderRunsPerTask(provider) {
		return r.whisperServerSpeechToText(ctx, provider, input)
	}
	binary, err := r.WhisperCLIPath(provider)
	if err != nil {
		return "", err
	}
	audioPath, cleanup, err := r.prepareWhisperAudio(ctx, input)
	if err != nil {
		return "", err
	}
	defer cleanup()

	outputBase := filepath.Join(os.TempDir(), "matrixclaw-whisper-"+tempFileSuffix())
	args := []string{"-m", r.VoiceModelPath(setup.VoiceModuleSTT, provider), "-f", audioPath, "-otxt", "-of", outputBase}
	if language := whisperLanguageArg(input.Language); language != "" {
		args = append(args, "-l", language)
	}
	if provider.Config.Threads > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", provider.Config.Threads))
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("whisper.cpp failed: %s", message)
	}
	textPath := outputBase + ".txt"
	defer os.Remove(textPath)
	content, err := os.ReadFile(textPath)
	if err != nil {
		if text := whisperTextFromStdout(stdout.String()); text != "" {
			return text, nil
		}
		return "", err
	}
	text := strings.TrimSpace(string(content))
	if text == "" {
		return "", errors.New("whisper.cpp returned empty text")
	}
	return text, nil
}

func whisperTextFromStdout(output string) string {
	lines := strings.Split(output, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "whisper_") || strings.HasPrefix(line, "system_info:") || strings.HasPrefix(line, "main:") || strings.Contains(line, " = ") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if _, after, ok := strings.Cut(line, "]"); ok {
				line = strings.TrimSpace(after)
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

func (r *Runtime) WhisperCLIPath(provider setup.VoiceProviderOption) (string, error) {
	configured := strings.TrimSpace(provider.Config.BinaryPath)
	if configured != "" && filepath.Base(configured) != "whisper-server" {
		if path, ok := executablePath(configured); ok {
			return path, nil
		}
	}
	for _, candidate := range []string{
		"whisper-cli",
		"main",
		filepath.Join(r.runtimeDir(), "whisper.cpp", "build", "bin", "whisper-cli"),
		filepath.Join(r.runtimeDir(), "whisper.cpp", "build", "bin", "main"),
		filepath.Join(r.runtimeDir(), "whisper.cpp", "main"),
	} {
		if path, ok := executablePath(candidate); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s runtime is not installed", provider.Name)
}

func (r *Runtime) WhisperServerPath(provider setup.VoiceProviderOption) (string, error) {
	configured := strings.TrimSpace(provider.Config.BinaryPath)
	if configured != "" && filepath.Base(configured) == "whisper-server" {
		if path, ok := executablePath(configured); ok {
			return path, nil
		}
	}
	for _, candidate := range []string{
		"whisper-server",
		filepath.Join(r.runtimeDir(), "whisper.cpp", "build", "bin", "whisper-server"),
	} {
		if path, ok := executablePath(candidate); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s server runtime is not installed", provider.Name)
}

func (r *Runtime) whisperServerSpeechToText(ctx context.Context, provider setup.VoiceProviderOption, input WhisperSpeechInput) (string, error) {
	if !r.whisperServerProcessRunning(provider) {
		if err := r.startWhisperServerProcess(ctx, setup.VoiceModuleSTT, provider); err != nil {
			return "", err
		}
	}
	audioPath, cleanup, err := r.prepareWhisperAudio(ctx, input)
	if err != nil {
		return "", err
	}
	defer cleanup()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	file, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		_ = file.Close()
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if language := whisperLanguageArg(input.Language); language != "" {
		_ = writer.WriteField("language", language)
	}
	_ = writer.WriteField("response_format", "text")
	if err := writer.Close(); err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(r.whisperServerEndpoint(provider), "/") + "/inference"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := r.httpClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("whisper.cpp server failed: status %s: %s", response.Status, strings.TrimSpace(string(responseBody)))
	}
	text := strings.TrimSpace(string(responseBody))
	if text == "" {
		return "", errors.New("whisper.cpp server returned empty text")
	}
	return text, nil
}

func executablePath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if filepath.IsAbs(value) || strings.Contains(value, string(os.PathSeparator)) {
		info, err := os.Stat(value)
		return value, err == nil && !info.IsDir() && info.Mode()&0o111 != 0
	}
	path, err := exec.LookPath(value)
	return path, err == nil
}

func (r *Runtime) prepareWhisperAudio(ctx context.Context, input WhisperSpeechInput) (string, func(), error) {
	extension := audioFileExtension(input.FileName, input.MIMEType)
	inputFile, err := os.CreateTemp("", "matrixclaw-whisper-input-*"+extension)
	if err != nil {
		return "", func() {}, err
	}
	inputPath := inputFile.Name()
	if _, err := inputFile.Write(input.Content); err != nil {
		_ = inputFile.Close()
		_ = os.Remove(inputPath)
		return "", func() {}, err
	}
	if err := inputFile.Close(); err != nil {
		_ = os.Remove(inputPath)
		return "", func() {}, err
	}
	cleanup := func() { _ = os.Remove(inputPath) }
	if whisperCLISupportsAudioFile(inputPath, input.Content) {
		return inputPath, cleanup, nil
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		cleanup()
		return "", func() {}, errors.New("ffmpeg is required to transcribe non-WAV audio with Whisper.cpp")
	}
	wavFile, err := os.CreateTemp("", "matrixclaw-whisper-audio-*.wav")
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	wavPath := wavFile.Name()
	if err := wavFile.Close(); err != nil {
		cleanup()
		_ = os.Remove(wavPath)
		return "", func() {}, err
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-y", "-i", inputPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wavPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		_ = os.Remove(wavPath)
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", func() {}, fmt.Errorf("ffmpeg failed: %s", message)
	}
	return wavPath, func() {
		cleanup()
		_ = os.Remove(wavPath)
	}, nil
}

func audioFileExtension(fileName string, mimeType string) string {
	if ext := strings.ToLower(strings.TrimSpace(filepath.Ext(fileName))); ext != "" {
		return ext
	}
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/flac":
		return ".flac"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/webm":
		return ".webm"
	default:
		return ".ogg"
	}
}

func whisperCLISupportsAudioFile(path string, content []byte) bool {
	if strings.ToLower(filepath.Ext(path)) != ".wav" {
		return false
	}
	return len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WAVE"
}

func whisperLanguageArg(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" || language == "auto" {
		return "auto"
	}
	language = strings.ReplaceAll(language, "_", "-")
	before, _, _ := strings.Cut(language, "-")
	if before == "" {
		return ""
	}
	return before
}

func tempFileSuffix() string {
	file, err := os.CreateTemp("", "matrixclaw-suffix-*")
	if err != nil {
		return "output"
	}
	name := filepath.Base(file.Name())
	_ = file.Close()
	_ = os.Remove(file.Name())
	return strings.TrimPrefix(name, "matrixclaw-suffix-")
}
