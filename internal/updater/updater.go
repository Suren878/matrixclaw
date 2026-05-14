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
	"strconv"
	"strings"
	"time"
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
	if compareVersions(latest, current) <= 0 {
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
	defer resp.Body.Close()
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
	defer os.Remove(script)

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
	ref := strings.TrimSpace(tag)
	if ref == "" {
		ref = "main"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+repo+"/"+ref+"/scripts/install.sh", nil)
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
	defer resp.Body.Close()
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

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if before, _, ok := strings.Cut(value, " "); ok {
		value = before
	}
	if strings.HasPrefix(value, "matrixclaw ") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "matrixclaw "))
	}
	if value == "dev" {
		return "dev"
	}
	if strings.HasPrefix(value, "v") {
		return "v" + strings.TrimPrefix(value, "v")
	}
	if value == "" {
		return ""
	}
	return "v" + value
}

func compareVersions(left string, right string) int {
	l := versionParts(left)
	r := versionParts(right)
	for i := 0; i < len(l) || i < len(r); i++ {
		var lv, rv int
		if i < len(l) {
			lv = l[i]
		}
		if i < len(r) {
			rv = r[i]
		}
		if lv > rv {
			return 1
		}
		if lv < rv {
			return -1
		}
	}
	return 0
}

func versionParts(value string) []int {
	value = strings.TrimPrefix(normalizeVersion(value), "v")
	value, _, _ = strings.Cut(value, "-")
	fields := strings.Split(value, ".")
	out := make([]int, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, n)
	}
	return out
}
