package localruntime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/procsup"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func (r *Runtime) piperPersistentTextToSpeech(ctx context.Context, provider setup.VoiceProviderOption, text string) ([]byte, error) {
	process, err := r.server(ctx, setup.VoiceModuleTTS, provider)
	if err != nil {
		return nil, err
	}
	outputDir := r.piperOutputDir(provider)
	var content []byte
	err = process.Use(func(stdin io.Writer) error {
		before := piperOutputFiles(outputDir)
		if _, err := fmt.Fprintln(stdin, normalizeTTSInputText(text)); err != nil {
			return err
		}
		path, err := waitForPiperOutput(ctx, outputDir, before)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(path) }()
		content, err = os.ReadFile(path)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("piper returned empty audio")
	}
	return content, nil
}

func piperOutputFiles(dir string) map[string]struct{} {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string]struct{}{}
	}
	files := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".wav" {
			continue
		}
		files[entry.Name()] = struct{}{}
	}
	return files
}

func waitForPiperOutput(ctx context.Context, dir string, before map[string]struct{}) (string, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		if path := newestPiperOutput(dir, before); path != "" && piperOutputReady(path) {
			return path, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("piper did not produce audio")
		case <-ticker.C:
		}
	}
}

func newestPiperOutput(dir string, before map[string]struct{}) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newestPath string
	var newestMod time.Time
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".wav" {
			continue
		}
		if _, seen := before[entry.Name()]; seen {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() <= 0 {
			continue
		}
		if newestPath == "" || info.ModTime().After(newestMod) {
			newestPath = filepath.Join(dir, entry.Name())
			newestMod = info.ModTime()
		}
	}
	return newestPath
}

func piperOutputReady(path string) bool {
	first, err := os.Stat(path)
	if err != nil || first.IsDir() || first.Size() <= 0 {
		return false
	}
	time.Sleep(50 * time.Millisecond)
	second, err := os.Stat(path)
	if err != nil || second.IsDir() || second.Size() <= 0 {
		return false
	}
	return first.Size() == second.Size() && first.ModTime().Equal(second.ModTime())
}

func (r *Runtime) piperOutputDir(provider setup.VoiceProviderOption) string {
	return filepath.Join(r.runtimeDir(), "piper", strings.TrimSpace(provider.ID), "output")
}

func (r *Runtime) piperOneShotTextToSpeech(ctx context.Context, provider setup.VoiceProviderOption, text string) ([]byte, error) {
	modelPath := r.VoiceModelPath(setup.VoiceModuleTTS, provider)
	if modelPath == "" {
		return nil, fmt.Errorf("voice is not selected")
	}
	if installed, _ := r.VoiceModelInstalled(setup.VoiceModuleTTS, provider); !installed {
		return nil, fmt.Errorf("voice is not installed")
	}
	binaryPath, err := r.VoiceBinaryPath(provider)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "matrixclaw-piper-*.wav")
	if err != nil {
		return nil, err
	}
	outputPath := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(outputPath)
		return nil, err
	}
	defer func() { _ = os.Remove(outputPath) }()

	args := []string{"--model", modelPath, "--config", modelPath + ".json", "--output-file", outputPath}
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	procsup.Prepare(cmd)
	cmd.Stdin = strings.NewReader(normalizeTTSInputText(text))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("piper failed: %s", message)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("piper returned empty audio")
	}
	return content, nil
}
