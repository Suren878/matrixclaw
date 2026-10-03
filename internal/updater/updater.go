package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	DefaultRepo        = "Suren878/matrixclaw"
	defaultHTTPTimeout = 8 * time.Second
)

type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type Update struct {
	Current string
	Latest  string
	URL     string
}

type Checker struct {
	Repo       string
	HTTPClient *http.Client
	BaseURL    string
}

func (c Checker) Check(ctx context.Context, current string) (Update, bool, error) {
	current = normalizeVersion(current)
	if current == "" || current == "dev" {
		return Update{}, false, nil
	}
	release, err := c.LatestRelease(ctx)
	if err != nil {
		return Update{}, false, err
	}
	latest := normalizeVersion(release.TagName)
	if latest == "" {
		return Update{}, false, nil
	}
	if semver.Compare(latest, current) <= 0 {
		return Update{}, false, nil
	}
	return Update{
		Current: current,
		Latest:  latest,
		URL:     strings.TrimSpace(release.HTMLURL),
	}, true, nil
}

func (c Checker) LatestRelease(ctx context.Context) (Release, error) {
	repo := strings.TrimSpace(c.Repo)
	if repo == "" {
		repo = DefaultRepo
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("latest release request failed: %s", resp.Status)
	}
	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, err
	}
	return release, nil
}

type Installer struct {
	Repo       string
	HTTPClient *http.Client
	BaseURL    string
	InstallDir string
	Stdout     io.Writer
	Stderr     io.Writer
}

func (i Installer) Install(ctx context.Context, tag string) error {
	tag = normalizeVersion(tag)
	if tag == "" {
		return fmt.Errorf("update tag is required")
	}
	repo := strings.TrimSpace(i.Repo)
	if repo == "" {
		repo = DefaultRepo
	}
	script, err := i.downloadInstallScript(ctx, repo, tag)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(script) }()

	installDir, err := i.installDir()
	if err != nil {
		return err
	}
	args := []string{script, "--version", tag, "--install-dir", installDir, "--no-setup"}
	cmd := exec.CommandContext(ctx, "bash", args...)
	cmd.Stdout = i.Stdout
	cmd.Stderr = i.Stderr
	return cmd.Run()
}

func (i Installer) downloadInstallScript(ctx context.Context, repo string, tag string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(i.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://raw.githubusercontent.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+repo+"/"+tag+"/scripts/install.sh", nil)
	if err != nil {
		return "", err
	}
	client := i.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download install script failed: %s", resp.Status)
	}
	file, err := os.CreateTemp("", "matrixclaw-install-*.sh")
	if err != nil {
		return "", err
	}
	path := file.Name()
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	if err := os.Chmod(path, 0o700); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func (i Installer) installDir() (string, error) {
	if dir := strings.TrimSpace(i.InstallDir); dir != "" {
		return dir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

// normalizeVersion turns "0.1.19 (commit)" or "v0.1.19" into "v0.1.19".
func normalizeVersion(value string) string {
	value, _, _ = strings.Cut(strings.TrimSpace(value), " ")
	if value == "" || value == "dev" {
		return value
	}
	return "v" + strings.TrimPrefix(value, "v")
}
