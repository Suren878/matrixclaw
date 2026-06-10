package localruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/setup"
)

var piperProcesses = &localProcessManager[*piperProcess]{
	processes: map[string]*piperProcess{},
}

type piperProcess struct {
	mu sync.Mutex
	*managedProcess
	modelPath string
	outputDir string
}

func (r *Runtime) startPiperProcess(moduleID string, provider setup.VoiceProviderOption) error {
	if provider.ID != "piper" {
		return fmt.Errorf("local voice provider %q cannot be started", provider.ID)
	}
	installed, _ := r.VoiceModelInstalled(moduleID, provider)
	if !installed {
		return fmt.Errorf("voice is not installed")
	}
	modelPath := r.VoiceModelPath(moduleID, provider)
	if strings.TrimSpace(modelPath) == "" {
		return fmt.Errorf("voice is not selected")
	}
	binary, err := r.VoiceBinaryPath(provider)
	if err != nil {
		return err
	}
	key := r.localVoiceProcessKey(provider)
	outputDir := r.piperOutputDir(provider)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	piperProcesses.mu.Lock()
	defer piperProcesses.mu.Unlock()
	if process := piperProcesses.processes[key]; process != nil && process.running() && process.modelPath == modelPath {
		return nil
	}
	if process := piperProcesses.processes[key]; process != nil {
		_ = process.stop(managedProcessStopTimeout)
		delete(piperProcesses.processes, key)
	}

	cmd := exec.Command(binary,
		"--model", modelPath,
		"--config", modelPath+".json",
		"--output-dir", outputDir,
		"--output-dir-naming", "timestamp",
	)
	managed, err := r.startManagedProcess(managedProcessOptions{
		cmd:      cmd,
		logName:  "piper.log",
		waitName: "localruntime.piperWait",
		stdin:    true,
	})
	if err != nil {
		return err
	}
	process := &piperProcess{
		managedProcess: managed,
		modelPath:      modelPath,
		outputDir:      outputDir,
	}
	piperProcesses.processes[key] = process
	return nil
}

func (r *Runtime) stopPiperProcess(provider setup.VoiceProviderOption) error {
	key := r.localVoiceProcessKey(provider)
	piperProcesses.mu.Lock()
	process := piperProcesses.processes[key]
	delete(piperProcesses.processes, key)
	piperProcesses.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.stop(managedProcessStopTimeout)
}

func (r *Runtime) piperProcessRunning(provider setup.VoiceProviderOption) bool {
	key := r.localVoiceProcessKey(provider)
	piperProcesses.mu.Lock()
	process := piperProcesses.processes[key]
	piperProcesses.mu.Unlock()
	return process != nil && process.running()
}

func (r *Runtime) piperPersistentTextToSpeech(ctx context.Context, provider setup.VoiceProviderOption, text string) ([]byte, error) {
	modelPath := r.VoiceModelPath(setup.VoiceModuleTTS, provider)
	if strings.TrimSpace(modelPath) == "" {
		return nil, fmt.Errorf("voice is not selected")
	}
	key := r.localVoiceProcessKey(provider)
	piperProcesses.mu.Lock()
	process := piperProcesses.processes[key]
	piperProcesses.mu.Unlock()
	if process == nil || !process.running() || process.modelPath != modelPath {
		if err := r.startPiperProcess(setup.VoiceModuleTTS, provider); err != nil {
			return nil, err
		}
		piperProcesses.mu.Lock()
		process = piperProcesses.processes[key]
		piperProcesses.mu.Unlock()
	}
	if process == nil || !process.running() || process.modelPath != modelPath {
		return nil, fmt.Errorf("piper runtime is not running with the selected voice")
	}
	return process.synthesize(ctx, text)
}

func (p *piperProcess) synthesize(ctx context.Context, text string) ([]byte, error) {
	if p == nil || p.stdin == nil {
		return nil, fmt.Errorf("piper runtime is not running")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running() {
		return nil, fmt.Errorf("piper runtime is not running")
	}
	outputDir := strings.TrimSpace(p.outputDir)
	if outputDir == "" {
		return nil, fmt.Errorf("piper output directory is empty")
	}
	before := piperOutputFiles(outputDir)
	if _, err := fmt.Fprintln(p.stdin, normalizeTTSInputText(text)); err != nil {
		return nil, err
	}
	path, err := waitForPiperOutput(ctx, outputDir, before)
	if err != nil {
		return nil, err
	}
	defer os.Remove(path)
	content, err := os.ReadFile(path)
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
	defer os.Remove(outputPath)

	args := []string{"--model", modelPath, "--config", modelPath + ".json", "--output-file", outputPath}
	cmd := exec.CommandContext(ctx, binaryPath, args...)
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
